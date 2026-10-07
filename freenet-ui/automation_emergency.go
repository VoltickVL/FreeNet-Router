package main

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	automationEmergencyCandidateLimit   = 8
	automationEmergencyCandidateTimeout = 7 * time.Second
	automationEmergencyScanTimeout      = 32 * time.Second
	automationEmergencyProbeWorkers     = 2
)

type automationEmergencyScanResult struct {
	Candidate       bestServerInternalCandidate
	CurrentEndpoint string
	CurrentFilter   string
	Total           int
	Checked         int
	Reachable       int
	ApplicationMS   int
}

type automationRecoveryIncident struct {
	ID        string
	StartedAt time.Time
	Profile   string
	Endpoint  string
	Xray      string
}

var automationEmergencyCandidateProbe = func(a *app, ctx context.Context, candidate bestServerInternalCandidate) bestServerProbeResult {
	if a == nil {
		return bestServerProbeResult{}
	}
	return a.probeBestServerApplicationPreflight(ctx, candidate)
}

func automationEmergencyRoundRobinByCountry(candidates []bestServerInternalCandidate) []bestServerInternalCandidate {
	if len(candidates) <= 1 {
		return append([]bestServerInternalCandidate(nil), candidates...)
	}
	groups := map[string][]bestServerInternalCandidate{}
	countries := make([]string, 0)
	for _, candidate := range candidates {
		country := strings.ToLower(strings.TrimSpace(candidate.Profile.CountryCode))
		if country == "" {
			country = "zz"
		}
		if _, ok := groups[country]; !ok {
			countries = append(countries, country)
		}
		groups[country] = append(groups[country], candidate)
	}
	sort.Strings(countries)
	for _, country := range countries {
		sort.SliceStable(groups[country], func(i, j int) bool {
			left := strings.TrimSpace(groups[country][i].Profile.ID)
			right := strings.TrimSpace(groups[country][j].Profile.ID)
			if left != right {
				return left < right
			}
			return profileEndpoint(groups[country][i].Profile) < profileEndpoint(groups[country][j].Profile)
		})
	}
	out := make([]bestServerInternalCandidate, 0, len(candidates))
	for round := 0; len(out) < len(candidates); round++ {
		added := false
		for _, country := range countries {
			if round >= len(groups[country]) {
				continue
			}
			out = append(out, groups[country][round])
			added = true
		}
		if !added {
			break
		}
	}
	return out
}

func automationEmergencyOrderedCandidates(candidates []bestServerInternalCandidate) []bestServerInternalCandidate {
	if len(candidates) <= 1 {
		return append([]bestServerInternalCandidate(nil), candidates...)
	}
	cached, missing, _ := splitProviderProfileRTTCache(candidates)
	reachable := make([]bestServerInternalCandidate, 0, len(cached))
	unreachable := make([]bestServerInternalCandidate, 0, len(cached))
	for _, candidate := range candidates {
		item, ok := cached[candidate.Profile.ID]
		if !ok {
			continue
		}
		if item.Reachable && item.RTTMS > 0 {
			candidate.VPNRTTMS = item.RTTMS
			candidate.VPNJitterMS = item.JitterMS
			reachable = append(reachable, candidate)
		} else {
			unreachable = append(unreachable, candidate)
		}
	}
	sort.SliceStable(reachable, func(i, j int) bool {
		if reachable[i].VPNRTTMS != reachable[j].VPNRTTMS {
			return reachable[i].VPNRTTMS < reachable[j].VPNRTTMS
		}
		if reachable[i].VPNJitterMS != reachable[j].VPNJitterMS {
			return reachable[i].VPNJitterMS < reachable[j].VPNJitterMS
		}
		return reachable[i].Profile.ID < reachable[j].Profile.ID
	})
	out := make([]bestServerInternalCandidate, 0, len(candidates))
	out = append(out, reachable...)
	out = append(out, automationEmergencyRoundRobinByCountry(missing)...)
	out = append(out, automationEmergencyRoundRobinByCountry(unreachable)...)
	return out
}

func automationEmergencyScanCursor(total int) int {
	if total <= 0 {
		return 0
	}
	state := v3ParseState(settingsV3StatePath())
	value, err := strconv.Atoi(strings.TrimSpace(state["EMERGENCY_SCAN_CURSOR"]))
	if err != nil || value < 0 {
		return 0
	}
	return value % total
}

func setAutomationEmergencyScanCursor(value, total int) {
	if total <= 0 {
		value = 0
	} else {
		value %= total
		if value < 0 {
			value = 0
		}
	}
	_ = v3WriteState(map[string]string{"EMERGENCY_SCAN_CURSOR": strconv.Itoa(value)})
}

func rotateAutomationEmergencyCandidates(candidates []bestServerInternalCandidate, offset int) []bestServerInternalCandidate {
	if len(candidates) == 0 {
		return nil
	}
	offset %= len(candidates)
	if offset < 0 {
		offset = 0
	}
	out := make([]bestServerInternalCandidate, 0, len(candidates))
	out = append(out, candidates[offset:]...)
	out = append(out, candidates[:offset]...)
	return out
}

func (a *app) scanAutomationEmergencyReplacement(parent context.Context, settings automationSettings, currentCountry string) (automationEmergencyScanResult, error) {
	result := automationEmergencyScanResult{
		CurrentEndpoint: readBestServerCurrentEndpoint(a.cfg.OutPath),
		CurrentFilter:   readBestServerCurrentFilter(a.cfg.FilterPath),
	}
	all, _, _, err := a.discoverBestServerCandidates(parent)
	if err != nil {
		return result, err
	}
	all = withoutBestServerCurrentLogicalAlternatives(all, result.CurrentEndpoint, result.CurrentFilter, currentExactProfileLabel(a.cfg.FilterPath))
	foreign := filterForeignBestServerCandidates(all)
	filtered := make([]bestServerInternalCandidate, 0, len(foreign))
	for _, candidate := range foreign {
		if automationCountryAllowed(settings, currentCountry, candidate.Profile.CountryCode) {
			filtered = append(filtered, candidate)
		}
	}
	result.Total = len(filtered)
	if len(filtered) == 0 {
		return result, nil
	}

	ordered := automationEmergencyOrderedCandidates(filtered)
	cursor := automationEmergencyScanCursor(len(ordered))
	ordered = rotateAutomationEmergencyCandidates(ordered, cursor)
	if len(ordered) > automationEmergencyCandidateLimit {
		ordered = ordered[:automationEmergencyCandidateLimit]
	}

	scanCtx, cancelScan := context.WithTimeout(parent, automationEmergencyScanTimeout)
	defer cancelScan()
	type probeResult struct {
		candidate bestServerInternalCandidate
		probe     bestServerProbeResult
	}
	success := make(chan probeResult, 1)
	jobs := make(chan bestServerInternalCandidate)
	workers := automationEmergencyProbeWorkers
	if workers > len(ordered) {
		workers = len(ordered)
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	checked := 0
	reachable := 0
	for worker := 0; worker < workers; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for candidate := range jobs {
				if scanCtx.Err() != nil {
					return
				}
				probeCtx, cancel := context.WithTimeout(scanCtx, automationEmergencyCandidateTimeout)
				probe := automationEmergencyCandidateProbe(a, probeCtx, candidate)
				cancel()

				mu.Lock()
				checked++
				if probe.OK {
					reachable++
				}
				mu.Unlock()

				if !probe.OK {
					continue
				}
				select {
				case success <- probeResult{candidate: candidate, probe: probe}:
					cancelScan()
					return
				default:
					return
				}
			}
		}()
	}
	go func() {
		defer close(jobs)
		for _, candidate := range ordered {
			select {
			case jobs <- candidate:
			case <-scanCtx.Done():
				return
			}
		}
	}()

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	var winner probeResult
	found := false
	select {
	case winner = <-success:
		found = true
		<-done
	case <-done:
	case <-parent.Done():
		cancelScan()
		<-done
	}
	mu.Lock()
	result.Checked = checked
	result.Reachable = reachable
	mu.Unlock()

	if found {
		result.Candidate = winner.candidate
		result.ApplicationMS = winner.probe.Median
		setAutomationEmergencyScanCursor(0, len(filtered))
		return result, nil
	}

	advance := len(ordered)
	if advance < 1 {
		advance = 1
	}
	setAutomationEmergencyScanCursor(cursor+advance, len(filtered))
	if parent.Err() != nil {
		return result, parent.Err()
	}
	return result, nil
}

func (a *app) storeAutomationEmergencySelectionSnapshot(scan automationEmergencyScanResult) (string, error) {
	candidate := scan.Candidate
	if strings.TrimSpace(candidate.Profile.ID) == "" {
		return "", errors.New("emergency VPN candidate is unavailable")
	}
	measured := []bestServerQualityCandidate{{
		Tested: true, Available: true, Eligible: true,
		ID: candidate.Profile.ID, Name: candidate.Profile.Name, CountryCode: candidate.Profile.CountryCode,
		Endpoint: profileEndpoint(candidate.Profile), ApplicationMS: scan.ApplicationMS,
	}}
	return a.storeBestServerSelectionSnapshot(
		scan.CurrentEndpoint,
		scan.CurrentFilter,
		[]bestServerInternalCandidate{candidate},
		measured,
	)
}

func automationEmergencySelectionSummary(scan automationEmergencyScanResult) string {
	name := profileDisplayName(scan.Candidate.Profile.Name)
	if name == "" {
		name = "VPN"
	}
	return fmt.Sprintf(
		"AUTO VPN emergency fast-path: application-ready replacement — %s [сайты %d мс]; pool=%d; checked=%d; reachable=%d; full quality scan отложен до восстановления интернета.",
		name, scan.ApplicationMS, scan.Total, scan.Checked, scan.Reachable,
	)
}

func (a *app) runAutomationBestEmergencyCycleLocked(parent context.Context, settings automationSettings) (automationBestCycleResult, automationEmergencyScanResult, error) {
	if settings.Mode != automationModeBest {
		return automationBestCycleResult{Result: "same", Reason: "Автоматический поиск замены не применим к текущему режиму."}, automationEmergencyScanResult{}, nil
	}
	currentCountry := automationCurrentCountry(a)
	if currentCountry == "" {
		reason := "Страна текущего VPN не подтверждена; автоматическая замена отменена."
		writeAutomationStateV2("uncertain", reason, "no", false)
		appendAutomationHistoryV2("uncertain", reason)
		return automationBestCycleResult{Result: "uncertain", Reason: reason}, automationEmergencyScanResult{}, nil
	}

	scan, err := a.scanAutomationEmergencyReplacement(parent, settings, currentCountry)
	if err != nil {
		reason := "Аварийный fast-path не завершил безопасную проверку replacement; текущие настройки сохранены."
		writeAutomationStateV2("failed", reason, "no", false)
		appendAutomationHistoryV2("failed", reason)
		return automationBestCycleResult{Result: "failed", Reason: reason}, scan, err
	}
	if strings.TrimSpace(scan.Candidate.Profile.ID) == "" {
		reason := fmt.Sprintf(
			"В аварийном fast-path пока нет application-ready replacement: pool=%d; checked=%d; reachable=%d. Следующий цикл продолжит с другого участка пула.",
			scan.Total, scan.Checked, scan.Reachable,
		)
		writeAutomationStateV2("failed", reason, "no", false)
		appendAutomationHistoryV2("failed", reason)
		return automationBestCycleResult{Result: "failed", Reason: reason}, scan, nil
	}
	if !settings.AutoApply {
		reason := "Найдена безопасно проверенная аварийная замена, но автоматическое применение выключено."
		writeAutomationStateV2("candidate", reason, "no", false)
		appendAutomationHistoryV2("candidate", reason)
		return automationBestCycleResult{Result: "candidate", Reason: reason, ProfileID: scan.Candidate.Profile.ID}, scan, nil
	}

	token, err := a.storeAutomationEmergencySelectionSnapshot(scan)
	if err != nil || !validBestServerSelectionToken(token) {
		reason := "Аварийный replacement проверен, но точный local selection snapshot не сохранён; mutation не выполняется."
		writeAutomationStateV2("failed", reason, "no", false)
		appendAutomationHistoryV2("failed", reason)
		if err == nil {
			err = errors.New("AUTO VPN emergency selection snapshot unavailable")
		}
		return automationBestCycleResult{Result: "failed", Reason: reason, ProfileID: scan.Candidate.Profile.ID}, scan, err
	}

	appendAutomationHistoryV2("selection", automationEmergencySelectionSummary(scan))
	status, applied := a.executeProviderProfileApply(networkApplyRequest{
		Operation: "provider", ProfileID: scan.Candidate.Profile.ID, SelectionToken: token, Confirm: true,
	})
	if status < 200 || status >= 300 || !applied.Success {
		reason := "Аварийный replacement не применён: " + strings.TrimSpace(applied.Error)
		rollback := strings.TrimSpace(applied.RollbackState)
		if rollback == "" {
			rollback = "unknown"
		}
		writeAutomationStateV2("failed", reason, rollback, false)
		appendAutomationHistoryV2("failed", reason+"; rollback="+rollback)
		return automationBestCycleResult{
			Result: "failed", Reason: reason, RollbackState: rollback, ProfileID: scan.Candidate.Profile.ID,
		}, scan, errors.New("AUTO VPN emergency apply failed")
	}
	name := profileDisplayName(scan.Candidate.Profile.Name)
	if name == "" {
		name = "VPN"
	}
	reason := "VPN-интернет восстановлен через аварийный fast-path: " + name
	writeAutomationStateV2("switched", reason, "yes", true)
	appendAutomationHistoryV2("success", reason)
	return automationBestCycleResult{
		Result: "switched", Reason: reason, Mutated: true, RollbackState: applied.RollbackState, ProfileID: scan.Candidate.Profile.ID,
	}, scan, nil
}

func newAutomationRecoveryIncident(a *app, now time.Time) automationRecoveryIncident {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	id := now.UTC().Format("20060102T150405Z")
	if token, err := newBestServerSelectionToken(); err == nil && len(token) >= 8 {
		id += "-" + token[:8]
	}
	profile := strings.TrimSpace(currentExactProfileLabel(a.cfg.FilterPath))
	if profile == "" {
		profile = "unknown"
	}
	endpoint := strings.TrimSpace(readBestServerCurrentEndpoint(a.cfg.OutPath))
	if endpoint == "" {
		endpoint = "unknown"
	}
	xray := "stopped"
	if xrayRuntimeProcessRunning(a) {
		xray = "running"
	}
	return automationRecoveryIncident{ID: id, StartedAt: now.UTC(), Profile: profile, Endpoint: endpoint, Xray: xray}
}

func (incident automationRecoveryIncident) context(now time.Time) string {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	elapsed := time.Duration(0)
	if !incident.StartedAt.IsZero() && !now.Before(incident.StartedAt) {
		elapsed = now.Sub(incident.StartedAt).Round(time.Second)
	}
	return fmt.Sprintf(
		"incident=%s; elapsed=%s; profile=%s; endpoint=%s; xray=%s",
		incident.ID, elapsed, incident.Profile, incident.Endpoint, incident.Xray,
	)
}

func appendAutomationRecoveryIncidentStage(incident automationRecoveryIncident, stage, result, reason string, now time.Time) {
	contextText := incident.context(now)
	reason = strings.TrimSpace(reason)
	if reason != "" {
		contextText += "; " + reason
	}
	appendAutomationRecoveryStage(stage, result, contextText)
}
