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
	automationEmergencyCandidateLimit       = 8
	automationEmergencyRTTCandidateTimeout  = 3 * time.Second
	automationEmergencyRTTSweepTimeout      = 12 * time.Second
	automationEmergencyCandidateTimeout     = 7 * time.Second
	automationEmergencyScanTimeout          = 32 * time.Second
	automationEmergencyProbeWorkers         = 2
)

type automationEmergencyScanResult struct {
	Candidate            bestServerInternalCandidate
	CurrentEndpoint      string
	CurrentFilter        string
	Total                int
	RTTChecked           int
	RTTReachable         int
	BestRTTMS            int
	RTTPrefilterDuration time.Duration
	Checked              int
	Reachable            int
	ApplicationMS        int
	ScanDuration         time.Duration
	ApplyDuration        time.Duration
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

var automationEmergencyRTTProbe = func(a *app, ctx context.Context, candidate bestServerInternalCandidate) bestServerProbeResult {
	if a == nil {
		return bestServerProbeResult{}
	}
	return a.probeBestServerProfilePing(ctx, candidate)
}

var automationEmergencyDiscoverCandidates = func(a *app, ctx context.Context) ([]bestServerInternalCandidate, bool, error) {
	if a == nil {
		return nil, false, errors.New("FreeNet app is unavailable")
	}
	all, _, truncated, err := a.discoverBestServerCandidates(ctx)
	return all, truncated, err
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

func automationEmergencyFreshRTTOrder(parent context.Context, a *app, candidates []bestServerInternalCandidate) ([]bestServerInternalCandidate, int, int, int) {
	if len(candidates) == 0 {
		return nil, 0, 0, 0
	}
	rttCtx, cancel := context.WithTimeout(parent, automationEmergencyRTTSweepTimeout)
	items := measureProviderProfileRTTWithTimeout(
		rttCtx,
		candidates,
		func(ctx context.Context, candidate bestServerInternalCandidate) bestServerProbeResult {
			return automationEmergencyRTTProbe(a, ctx, candidate)
		},
		automationEmergencyRTTCandidateTimeout,
	)
	cancel()
	storeProviderProfileRTTCache(candidates, items)

	byID := make(map[string]bestServerInternalCandidate, len(candidates))
	for _, candidate := range candidates {
		if id := strings.TrimSpace(candidate.Profile.ID); id != "" {
			byID[id] = candidate
		}
	}
	ordered := make([]bestServerInternalCandidate, 0, len(candidates))
	seen := make(map[string]bool, len(candidates))
	checked, reachable, bestRTT := 0, 0, 0
	for _, item := range items {
		id := strings.TrimSpace(item.ProfileID)
		candidate, ok := byID[id]
		if !ok || seen[id] {
			continue
		}
		if item.Attempted {
			checked++
		}
		if !item.Reachable || item.RTTMS <= 0 {
			continue
		}
		reachable++
		if bestRTT == 0 || item.RTTMS < bestRTT {
			bestRTT = item.RTTMS
		}
		candidate.VPNRTTMS = item.RTTMS
		candidate.VPNJitterMS = item.JitterMS
		ordered = append(ordered, candidate)
		seen[id] = true
	}

	// Fresh canonical VPN RTT decides the priority. UNKNOWN/unreachable rows are
	// fallback only: a quick RTT miss is not enough evidence to discard a
	// candidate that may still pass the named-HTTPS application gate.
	for _, candidate := range candidates {
		id := strings.TrimSpace(candidate.Profile.ID)
		if id == "" || seen[id] {
			continue
		}
		ordered = append(ordered, candidate)
		seen[id] = true
	}
	return ordered, checked, reachable, bestRTT
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
	all, _, err := automationEmergencyDiscoverCandidates(a, parent)
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

	// Cache/country diversity only builds the bounded cohort. Every emergency
	// cycle then refreshes canonical logical-VPN RTT for that cohort before any
	// named-HTTPS acceptance. Fresh VPN ping, not country ordering, determines
	// which candidate receives the application gate first.
	cohort := automationEmergencyOrderedCandidates(filtered)
	cursor := automationEmergencyScanCursor(len(cohort))
	cohort = rotateAutomationEmergencyCandidates(cohort, cursor)
	if len(cohort) > automationEmergencyCandidateLimit {
		cohort = cohort[:automationEmergencyCandidateLimit]
	}

	scanCtx, cancelScan := context.WithTimeout(parent, automationEmergencyScanTimeout)
	defer cancelScan()

	rttStarted := time.Now()
	ordered, rttChecked, rttReachable, bestRTT := automationEmergencyFreshRTTOrder(scanCtx, a, cohort)
	result.RTTPrefilterDuration = time.Since(rttStarted).Round(time.Millisecond)
	result.RTTChecked = rttChecked
	result.RTTReachable = rttReachable
	result.BestRTTMS = bestRTT
	if len(ordered) == 0 {
		advance := len(cohort)
		if advance < 1 {
			advance = 1
		}
		setAutomationEmergencyScanCursor(cursor+advance, len(filtered))
		return result, nil
	}

	// Application acceptance runs in RTT-priority batches. Two candidates may
	// be probed in parallel for latency, but a higher-RTT candidate cannot beat
	// a lower-RTT successful candidate from the same batch.
	workers := automationEmergencyProbeWorkers
	if workers < 1 {
		workers = 1
	}
	for offset := 0; offset < len(ordered) && scanCtx.Err() == nil; offset += workers {
		end := offset + workers
		if end > len(ordered) {
			end = len(ordered)
		}
		batch := ordered[offset:end]
		probes := make([]bestServerProbeResult, len(batch))
		var wg sync.WaitGroup
		for index := range batch {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				if scanCtx.Err() != nil {
					return
				}
				probeCtx, cancel := context.WithTimeout(scanCtx, automationEmergencyCandidateTimeout)
				probes[i] = automationEmergencyCandidateProbe(a, probeCtx, batch[i])
				cancel()
			}(index)
		}
		wg.Wait()

		for index, probe := range probes {
			result.Checked++
			if !probe.OK {
				continue
			}
			result.Reachable++
			result.Candidate = batch[index]
			result.ApplicationMS = probe.Median
			setAutomationEmergencyScanCursor(0, len(filtered))
			return result, nil
		}
	}

	advance := len(cohort)
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
		Endpoint: profileEndpoint(candidate.Profile),
		VPNRTTMS: candidate.VPNRTTMS, VPNJitterMS: candidate.VPNJitterMS,
		ApplicationMS: scan.ApplicationMS,
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
		"AUTO VPN emergency fast-path: fresh VPN-ping → application-ready replacement — %s [VPN %d мс; сайты %d мс]; pool=%d; rtt_checked=%d; rtt_reachable=%d; app_checked=%d; app_reachable=%d; full quality scan отложен до восстановления интернета.",
		name, scan.Candidate.VPNRTTMS, scan.ApplicationMS, scan.Total, scan.RTTChecked, scan.RTTReachable, scan.Checked, scan.Reachable,
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

	scanStarted := time.Now()
	scan, err := a.scanAutomationEmergencyReplacement(parent, settings, currentCountry)
	scan.ScanDuration = time.Since(scanStarted).Round(time.Millisecond)
	if err != nil {
		reason := "Аварийный fast-path не завершил безопасную проверку replacement; текущие настройки сохранены."
		writeAutomationStateV2("failed", reason, "no", false)
		appendAutomationHistoryV2("failed", reason)
		return automationBestCycleResult{Result: "failed", Reason: reason}, scan, err
	}
	if strings.TrimSpace(scan.Candidate.Profile.ID) == "" {
		reason := fmt.Sprintf(
			"В аварийном fast-path пока нет application-ready replacement: pool=%d; rtt_checked=%d; rtt_reachable=%d; app_checked=%d; app_reachable=%d. Следующий цикл продолжит с другого участка пула.",
			scan.Total, scan.RTTChecked, scan.RTTReachable, scan.Checked, scan.Reachable,
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
	applyStarted := time.Now()
	status, applied := a.executeProviderProfileApply(networkApplyRequest{
		Operation: "provider", ProfileID: scan.Candidate.Profile.ID, SelectionToken: token, Confirm: true,
	})
	scan.ApplyDuration = time.Since(applyStarted).Round(time.Millisecond)
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
