package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	automationModeEndpoint = "endpoint"
	automationModeBest     = "best"

	automationPolicyDegraded = "degraded"
	automationPolicyBetter   = "better"

	automationCountryCurrent   = "current"
	automationCountryRegion    = "region"
	automationCountryAllowlist = "allowlist"

	automationBestCooldown = 6 * time.Hour
	automationBestTimeout  = 250 * time.Second
)

var errAutomationBusy = errors.New("AUTO VPN operation is already active")

type automationBestCycleResult struct {
	Result        string
	Reason        string
	Mutated       bool
	RollbackState string
	ProfileID     string
}

func normalizeAutomationMode(value string) string {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case automationModeBest:
		return automationModeBest
	default:
		return automationModeEndpoint
	}
}

func normalizeAutomationPolicy(value string) string {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case automationPolicyBetter:
		return automationPolicyBetter
	default:
		return automationPolicyDegraded
	}
}

func normalizeAutomationCountryScope(value string) string {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case automationCountryCurrent, automationCountryAllowlist:
		return strings.TrimSpace(strings.ToLower(value))
	default:
		return automationCountryRegion
	}
}

func normalizeAutomationCountries(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, raw := range values {
		for _, part := range strings.Split(raw, ",") {
			code := strings.ToLower(strings.TrimSpace(part))
			if len(code) != 2 || code == "ru" {
				continue
			}
			valid := true
			for _, r := range code {
				if r < 'a' || r > 'z' {
					valid = false
					break
				}
			}
			if !valid {
				continue
			}
			if _, ok := seen[code]; ok {
				continue
			}
			seen[code] = struct{}{}
			out = append(out, code)
		}
	}
	sort.Strings(out)
	return out
}

func automationCountriesFromConfig(value string) []string {
	if strings.TrimSpace(value) == "" {
		return []string{}
	}
	return normalizeAutomationCountries(strings.Split(value, ","))
}

func readAutomationSettings(path string) automationSettings {
	interval := automationConfigValue(path, "AUTO_VPN_V1_INTERVAL", "manual")
	if _, ok := automationCron(interval); !ok {
		interval = "manual"
	}
	return automationSettings{
		Enabled:                automationConfigValue(path, "AUTO_VPN_V1", "no") == "yes",
		Interval:               interval,
		CurrentProfileOnly:     normalizeAutomationMode(automationConfigValue(path, "AUTO_VPN_MODE", automationModeEndpoint)) == automationModeEndpoint,
		AutoEndpointUpdate:     true,
		AmbiguousNeedsApproval: true,
		Mode:                   normalizeAutomationMode(automationConfigValue(path, "AUTO_VPN_MODE", automationModeEndpoint)),
		Policy:                 normalizeAutomationPolicy(automationConfigValue(path, "AUTO_VPN_POLICY", automationPolicyDegraded)),
		CountryScope:           normalizeAutomationCountryScope(automationConfigValue(path, "AUTO_VPN_COUNTRY_SCOPE", automationCountryRegion)),
		Countries:              automationCountriesFromConfig(automationConfigValue(path, "AUTO_VPN_COUNTRIES", "")),
		AutoApply:              automationConfigValue(path, "AUTO_VPN_AUTO_APPLY", "yes") != "no",
	}
}

func validateAutomationSettings(settings automationSettings) error {
	if _, ok := automationCron(settings.Interval); !ok {
		return errors.New("unsupported automation interval")
	}
	if settings.Mode != automationModeEndpoint && settings.Mode != automationModeBest {
		return errors.New("unsupported AUTO VPN mode")
	}
	if settings.Policy != automationPolicyDegraded && settings.Policy != automationPolicyBetter {
		return errors.New("unsupported AUTO VPN policy")
	}
	if settings.CountryScope != automationCountryCurrent && settings.CountryScope != automationCountryRegion && settings.CountryScope != automationCountryAllowlist {
		return errors.New("unsupported country scope")
	}
	if settings.CountryScope == automationCountryAllowlist && len(settings.Countries) == 0 {
		return errors.New("country allow-list is empty")
	}
	return nil
}

func configAssignmentValue(value string) string {
	if strings.ContainsAny(value, " \t") {
		return "'" + strings.ReplaceAll(value, "'", "") + "'"
	}
	return strings.ReplaceAll(value, "'", "")
}

func writeAutomationConfigValues(path string, values map[string]string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	lines := strings.Split(strings.ReplaceAll(string(data), "\r", ""), "\n")
	written := map[string]bool{}
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		key, _, ok := strings.Cut(trimmed, "=")
		if !ok {
			continue
		}
		value, exists := values[key]
		if !exists {
			continue
		}
		lines[i] = key + "=" + configAssignmentValue(value)
		written[key] = true
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if !written[key] {
			lines = append(lines, key+"="+configAssignmentValue(values[key]))
		}
	}
	payload := []byte(strings.TrimRight(strings.Join(lines, "\n"), "\n") + "\n")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	tmp := path + ".settings-v2.new"
	if err := os.WriteFile(tmp, payload, 0600); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0600); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func automationCrontabBin() string {
	if value := strings.TrimSpace(os.Getenv("FREENET_CRONTAB_BIN")); value != "" {
		return value
	}
	return "crontab"
}

func automationRunnerPath() string {
	if value := strings.TrimSpace(os.Getenv("FREENET_UI_BIN")); value != "" {
		return value
	}
	return "/opt/sbin/freenet-ui"
}

func readAutomationCrontab() []byte {
	out, err := exec.Command(automationCrontabBin(), "-l").Output()
	if err != nil {
		return []byte{}
	}
	return out
}

func installAutomationCrontab(data []byte) error {
	file, err := os.CreateTemp("", "freenet-crontab-*.txt")
	if err != nil {
		return err
	}
	name := file.Name()
	defer os.Remove(name)
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	out, err := exec.Command(automationCrontabBin(), name).CombinedOutput()
	if err != nil {
		return fmt.Errorf("cannot install managed cron: %s", strings.TrimSpace(sanitizeOutput(string(out))))
	}
	return nil
}

func stripManagedAutomationCron(data []byte) []string {
	lines := strings.Split(strings.ReplaceAll(string(data), "\r", ""), "\n")
	out := make([]string, 0, len(lines))
	skip := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "# BEGIN FREENET" {
			skip = true
			continue
		}
		if trimmed == "# END FREENET" {
			skip = false
			continue
		}
		if skip {
			continue
		}
		if strings.Contains(line, "/opt/lib/freenet/auto_vpn.sh run") ||
			strings.Contains(line, "freenet-ui automation-best-run") ||
			strings.Contains(line, "/opt/bin/blanc_xkeen_update_outbounds.sh") ||
			strings.Contains(line, "/opt/bin/vpn failover") ||
			strings.Contains(line, "/opt/sbin/xkeen -ug") {
			continue
		}
		if trimmed != "" {
			out = append(out, line)
		}
	}
	return out
}

func buildManagedAutomationCron(configPath string, settings automationSettings, existing []byte) ([]byte, error) {
	cron, ok := automationCron(settings.Interval)
	if !ok {
		return nil, errors.New("unsupported automation interval")
	}
	lines := stripManagedAutomationCron(existing)
	lines = append(lines, "# BEGIN FREENET")
	if automationConfigValue(configPath, "AUTO_XKEEN_GEODATA", "yes") == "yes" {
		geoCron := strings.TrimSpace(automationConfigValue(configPath, "AUTO_XKEEN_GEODATA_CRON", "30 6 * * *"))
		if geoCron != "" {
			lines = append(lines, geoCron+" /opt/sbin/xkeen -ug")
		}
	}
	if settings.Enabled && settings.Interval != "manual" && cron != "" {
		if settings.Mode == automationModeBest {
			lines = append(lines, cron+" "+automationRunnerPath()+" automation-best-run >> /opt/var/log/freenet-auto-vpn.log 2>&1")
		} else {
			lines = append(lines, cron+" /opt/lib/freenet/auto_vpn.sh run >> /opt/var/log/freenet-auto-vpn.log 2>&1")
		}
	} else {
		lines = append(lines, "# AUTO VPN scheduler disabled by FreeNet settings")
	}
	if automationConfigValue(configPath, "AUTO_VPN_FAILOVER", "no") == "yes" {
		failoverCron := strings.TrimSpace(automationConfigValue(configPath, "AUTO_VPN_FAILOVER_CRON", "*/5 * * * *"))
		if failoverCron != "" {
			lines = append(lines, failoverCron+" /opt/bin/vpn failover >> /opt/var/log/freenet-vpn-failover.log 2>&1")
		}
	} else {
		lines = append(lines, "# vpn failover disabled by FreeNet settings")
	}
	lines = append(lines, "# END FREENET")
	return []byte(strings.Join(lines, "\n") + "\n"), nil
}

func (a *app) saveAutomationSettingsV2(settings automationSettings, geoDataEnabled *bool) error {
	settings.Mode = normalizeAutomationMode(settings.Mode)
	settings.Policy = normalizeAutomationPolicy(settings.Policy)
	settings.CountryScope = normalizeAutomationCountryScope(settings.CountryScope)
	settings.Countries = normalizeAutomationCountries(settings.Countries)
	if err := validateAutomationSettings(settings); err != nil {
		return err
	}
	beforeConfig, err := os.ReadFile(a.cfg.ConfigPath)
	if err != nil {
		return errors.New("FreeNet config is unavailable")
	}
	beforeCron := readAutomationCrontab()
	cron, _ := automationCron(settings.Interval)
	values := map[string]string{
		"AUTO_VPN_V1":            map[bool]string{true: "yes", false: "no"}[settings.Enabled],
		"AUTO_VPN_V1_INTERVAL":   settings.Interval,
		"AUTO_VPN_MODE":          settings.Mode,
		"AUTO_VPN_POLICY":        settings.Policy,
		"AUTO_VPN_COUNTRY_SCOPE": settings.CountryScope,
		"AUTO_VPN_COUNTRIES":     strings.Join(settings.Countries, ","),
		"AUTO_VPN_AUTO_APPLY":    map[bool]string{true: "yes", false: "no"}[settings.AutoApply],
		"AUTO_ENDPOINT_UPDATE":   map[bool]string{true: "yes", false: "no"}[settings.Enabled && settings.Mode == automationModeEndpoint && settings.Interval != "manual"],
		"AUTO_ENDPOINT_CRON":     cron,
	}
	if geoDataEnabled != nil {
		values["AUTO_XKEEN_GEODATA"] = map[bool]string{true: "yes", false: "no"}[*geoDataEnabled]
	}
	if err := writeAutomationConfigValues(a.cfg.ConfigPath, values); err != nil {
		return errors.New("cannot stage AUTO VPN settings")
	}
	managed, err := buildManagedAutomationCron(a.cfg.ConfigPath, settings, beforeCron)
	if err == nil {
		err = installAutomationCrontab(managed)
	}
	if err == nil {
		return nil
	}
	rollbackConfigErr := os.WriteFile(a.cfg.ConfigPath, beforeConfig, 0600)
	rollbackCronErr := installAutomationCrontab(beforeCron)
	if rollbackConfigErr != nil || rollbackCronErr != nil {
		return errors.New("AUTO VPN settings apply failed; rollback failed or is unknown")
	}
	return errors.New("AUTO VPN settings apply failed; previous config and cron restored")
}

func automationRegion(code string) string {
	code = strings.ToLower(strings.TrimSpace(code))
	sets := map[string]string{
		"al":"eu","ad":"eu","at":"eu","be":"eu","bg":"eu","ba":"eu","by":"eu","ch":"eu","cy":"eu","cz":"eu","de":"eu","dk":"eu","ee":"eu","es":"eu","fi":"eu","fr":"eu","gb":"eu","gr":"eu","hr":"eu","hu":"eu","ie":"eu","is":"eu","it":"eu","li":"eu","lt":"eu","lu":"eu","lv":"eu","mc":"eu","md":"eu","me":"eu","mk":"eu","mt":"eu","nl":"eu","no":"eu","pl":"eu","pt":"eu","ro":"eu","rs":"eu","se":"eu","si":"eu","sk":"eu","ua":"eu",
		"ae":"asia","am":"asia","az":"asia","ge":"asia","hk":"asia","id":"asia","il":"asia","in":"asia","jp":"asia","kr":"asia","kz":"asia","my":"asia","ph":"asia","sg":"asia","th":"asia","tr":"asia","tw":"asia","vn":"asia",
		"ar":"americas","br":"americas","ca":"americas","cl":"americas","co":"americas","mx":"americas","pe":"americas","us":"americas","uy":"americas",
		"au":"oceania","nz":"oceania",
		"eg":"africa","ma":"africa","za":"africa",
	}
	return sets[code]
}

func automationCountryAllowed(settings automationSettings, currentCountry, candidateCountry string) bool {
	candidateCountry = strings.ToLower(strings.TrimSpace(candidateCountry))
	currentCountry = strings.ToLower(strings.TrimSpace(currentCountry))
	if candidateCountry == "" || candidateCountry == "ru" {
		return false
	}
	switch settings.CountryScope {
	case automationCountryCurrent:
		return currentCountry != "" && candidateCountry == currentCountry
	case automationCountryAllowlist:
		for _, code := range settings.Countries {
			if candidateCountry == code {
				return true
			}
		}
		return false
	default:
		region := automationRegion(currentCountry)
		return region != "" && automationRegion(candidateCountry) == region
	}
}

func automationMeaningfullyBetter(current, candidate bestServerQualityCandidate) bool {
	if !candidate.Eligible || !candidate.Available {
		return false
	}
	if current.Available && !current.Eligible {
		return true
	}
	if !current.Eligible || !current.Available {
		return false
	}
	gain := 0.0
	weight := 0.0
	if current.DownloadMbps > 0 && candidate.DownloadMbps > 0 {
		gain += 0.45 * ((candidate.DownloadMbps - current.DownloadMbps) / current.DownloadMbps)
		weight += 0.45
	}
	if current.ApplicationMS > 0 && candidate.ApplicationMS > 0 {
		gain += 0.35 * (float64(current.ApplicationMS-candidate.ApplicationMS) / float64(current.ApplicationMS))
		weight += 0.35
	}
	if current.JitterMS > 0 && candidate.JitterMS > 0 {
		gain += 0.20 * (float64(current.JitterMS-candidate.JitterMS) / float64(current.JitterMS))
		weight += 0.20
	}
	if weight < 0.79 {
		return false
	}
	return gain/weight >= 0.10
}

func automationCurrentQualityState(response bestServerQualityResponse) (bestServerQualityCandidate, string) {
	if len(response.Candidates) != 1 {
		return bestServerQualityCandidate{}, "uncertain"
	}
	candidate := response.Candidates[0]
	if !candidate.Tested || !candidate.Available {
		return candidate, "uncertain"
	}
	if candidate.Eligible {
		return candidate, "healthy"
	}
	return candidate, "degraded"
}

func parseAutomationLastSwitch(path string) time.Time {
	state := parseAutomationState(path)
	value := strings.TrimSpace(state["LAST_SWITCH"])
	if value == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}
	}
	return t
}

func automationCooldownActive(lastSwitch, now time.Time) bool {
	return !lastSwitch.IsZero() && now.Before(lastSwitch.Add(automationBestCooldown))
}

func sanitizeAutomationReason(reason string) string {
	reason = sanitizeOutput(reason)
	reason = strings.ReplaceAll(reason, "\t", " ")
	reason = strings.ReplaceAll(reason, "\r", " ")
	reason = strings.ReplaceAll(reason, "\n", " ")
	return strings.TrimSpace(reason)
}

func writeAutomationStateV2(result, reason, rollback string, switched bool) {
	path := automationStatePath()
	previous := parseAutomationState(path)
	lastSwitch := previous["LAST_SWITCH"]
	if switched {
		lastSwitch = time.Now().UTC().Format(time.RFC3339)
	}
	if rollback == "" {
		rollback = "no"
	}
	payload := strings.Join([]string{
		"LAST_RUN=" + time.Now().UTC().Format(time.RFC3339),
		"LAST_RESULT=" + sanitizeAutomationReason(result),
		"LAST_REASON=" + sanitizeAutomationReason(reason),
		"ROLLBACK_READY=" + sanitizeAutomationReason(rollback),
		"LAST_SWITCH=" + sanitizeAutomationReason(lastSwitch),
	}, "\n") + "\n"
	_ = os.MkdirAll(filepath.Dir(path), 0755)
	tmp := path + ".v2.new"
	if err := os.WriteFile(tmp, []byte(payload), 0600); err == nil {
		_ = os.Rename(tmp, path)
	}
}

func appendAutomationHistoryV2(result, reason string) {
	path := automationHistoryPath()
	_ = os.MkdirAll(filepath.Dir(path), 0755)
	line := fmt.Sprintf("%s\tAUTO VPN\t%s\t%s\n", time.Now().UTC().Format(time.RFC3339), sanitizeAutomationReason(result), sanitizeAutomationReason(reason))
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return
	}
	_, _ = file.WriteString(line)
	_ = file.Close()
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) > 50 {
		lines = lines[len(lines)-50:]
		_ = os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0600)
	}
}

func automationBestLockPath() string {
	if value := strings.TrimSpace(os.Getenv("FREENET_AUTO_BEST_LOCK")); value != "" {
		return value
	}
	return "/tmp/freenet-auto-best.lock"
}

func acquireAutomationBestLock() (func(), error) {
	path := automationBestLockPath()
	try := func() error {
		if err := os.Mkdir(path, 0700); err != nil {
			return err
		}
		_ = os.WriteFile(filepath.Join(path, "pid"), []byte(strconv.Itoa(os.Getpid())+"\n"), 0600)
		return nil
	}
	if err := try(); err == nil {
		return func() { _ = os.RemoveAll(path) }, nil
	}
	if info, err := os.Stat(path); err == nil && time.Since(info.ModTime()) > 15*time.Minute {
		_ = os.RemoveAll(path)
		if err := try(); err == nil {
			return func() { _ = os.RemoveAll(path) }, nil
		}
	}
	return nil, errAutomationBusy
}

func (a *app) scanBestServerForeignForAutomation(ctx context.Context, settings automationSettings, currentCountry string) (bestServerQualityResponse, error) {
	currentEndpoint := readBestServerCurrentEndpoint(a.cfg.OutPath)
	currentFilter := readBestServerCurrentFilter(a.cfg.FilterPath)
	all, _, truncated, err := a.discoverBestServerCandidates(ctx)
	if err != nil {
		return bestServerQualityResponse{}, err
	}
	foreign := filterForeignBestServerCandidates(all)
	filtered := make([]bestServerInternalCandidate, 0, len(foreign))
	for _, candidate := range foreign {
		if automationCountryAllowed(settings, currentCountry, candidate.Profile.CountryCode) {
			filtered = append(filtered, candidate)
		}
	}
	profilesScanned := len(filtered)
	currentBaseline, currentBaselineOK := loadBestServerCurrentQuality(currentEndpoint, currentFilter)
	if currentIndex := bestServerCurrentCandidateIndex(filtered, currentEndpoint, currentFilter); currentIndex >= 0 {
		filtered = withoutBestServerCandidate(filtered, currentIndex)
	}
	if len(filtered) == 0 {
		return bestServerQualityResponse{
			Success: true, Available: false, Candidates: []bestServerQualityCandidate{}, ProfilesScanned: profilesScanned, ProfilesTotal: profilesScanned,
			ProfilesTruncated: truncated, Mutation: "NONE", ScannedAt: time.Now().UTC().Format(time.RFC3339), CurrentEndpoint: currentEndpoint,
			Message: "Нет разрешённых кандидатов для автоматического переключения.",
		}, nil
	}
	filtered = a.applicationAwareBestServerShortlist(ctx, filtered, currentEndpoint, currentFilter)
	response := a.rankMeasuredBestServerBatches(ctx, filtered, profilesScanned, truncated, currentEndpoint, currentFilter)
	if ctx.Err() != nil && len(response.Candidates) == 0 {
		return bestServerQualityResponse{}, ctx.Err()
	}
	if currentBaselineOK {
		response.Candidates = append(response.Candidates, currentBaseline)
	}
	response.Success = true
	response.Mutation = "NONE"
	response.ScannedAt = time.Now().UTC().Format(time.RFC3339)
	response.CurrentEndpoint = currentEndpoint
	response.ProfilesScanned = profilesScanned
	if after := readBestServerCurrentEndpoint(a.cfg.OutPath); after != currentEndpoint {
		return bestServerQualityResponse{}, errors.New("VPN endpoint changed during AUTO VPN scan")
	}
	if afterFilter := readBestServerCurrentFilter(a.cfg.FilterPath); afterFilter != currentFilter {
		return bestServerQualityResponse{}, errors.New("VPN profile identity changed during AUTO VPN scan")
	}
	return response, nil
}

func bestAutomationCandidate(response bestServerQualityResponse) (bestServerQualityCandidate, bool) {
	for _, candidate := range response.Candidates {
		if candidate.Current || !candidate.Eligible || !candidate.Available || !validProfileID(candidate.ID) {
			continue
		}
		return candidate, true
	}
	return bestServerQualityCandidate{}, false
}

func (a *app) runAutomationBestCycle(parent context.Context, manual bool) (automationBestCycleResult, error) {
	settings := readAutomationSettings(a.cfg.ConfigPath)
	if settings.Mode != automationModeBest {
		return automationBestCycleResult{Result: "same", Reason: "Режим «Лучший VPN автоматически» не выбран."}, nil
	}
	if !manual && !settings.Enabled {
		return automationBestCycleResult{Result: "disabled", Reason: "AUTO VPN выключен."}, nil
	}
	release, err := acquireAutomationBestLock()
	if err != nil {
		return automationBestCycleResult{Result: "busy", Reason: "Проверка пропущена: другая AUTO VPN операция уже выполняется."}, nil
	}
	defer release()

	ctx, cancel := context.WithTimeout(parent, automationBestTimeout)
	defer cancel()
	currentResponse, err := a.scanCurrentVPNQuality(ctx)
	if err != nil {
		reason := "Не удалось подтвердить качество текущего VPN; переключение не выполнялось."
		writeAutomationStateV2("failed", reason, "no", false)
		appendAutomationHistoryV2("failed", reason)
		return automationBestCycleResult{Result: "failed", Reason: reason}, err
	}
	current, currentState := automationCurrentQualityState(currentResponse)
	if currentState == "uncertain" {
		reason := "Качество текущего VPN подтверждено не полностью; неоднозначность = без изменений."
		writeAutomationStateV2("uncertain", reason, "no", false)
		appendAutomationHistoryV2("uncertain", reason)
		return automationBestCycleResult{Result: "uncertain", Reason: reason}, nil
	}
	currentCountry := current.CountryCode
	if currentCountry == "" {
		currentCountry = a.status().CountryCode
	}
	candidates, err := a.scanBestServerForeignForAutomation(ctx, settings, currentCountry)
	if err != nil {
		reason := "Подбор разрешённых VPN-кандидатов не завершён; текущий VPN сохранён."
		writeAutomationStateV2("failed", reason, "no", false)
		appendAutomationHistoryV2("failed", reason)
		return automationBestCycleResult{Result: "failed", Reason: reason}, err
	}
	candidate, ok := bestAutomationCandidate(candidates)
	if !ok {
		reason := "Подтверждённого Eligible-кандидата для автоматического переключения нет."
		writeAutomationStateV2("same", reason, "no", false)
		appendAutomationHistoryV2("same", reason)
		return automationBestCycleResult{Result: "same", Reason: reason}, nil
	}

	shouldSwitch := false
	if settings.Policy == automationPolicyDegraded {
		shouldSwitch = currentState == "degraded"
	} else {
		shouldSwitch = currentState == "degraded" || automationMeaningfullyBetter(current, candidate)
	}
	if !shouldSwitch {
		reason := "Текущий VPN не требует смены по выбранной политике."
		writeAutomationStateV2("same", reason, "no", false)
		appendAutomationHistoryV2("same", reason)
		return automationBestCycleResult{Result: "same", Reason: reason, ProfileID: candidate.ID}, nil
	}
	if automationCooldownActive(parseAutomationLastSwitch(automationStatePath()), time.Now().UTC()) {
		reason := "Новый VPN найден, но действует 6-часовая защита от частых переключений."
		writeAutomationStateV2("cooldown", reason, "no", false)
		appendAutomationHistoryV2("cooldown", reason)
		return automationBestCycleResult{Result: "cooldown", Reason: reason, ProfileID: candidate.ID}, nil
	}
	if !settings.AutoApply || (!settings.Enabled && manual) {
		reason := "Найден подтверждённый лучший VPN; автоматическое применение выключено."
		writeAutomationStateV2("candidate", reason, "no", false)
		appendAutomationHistoryV2("candidate", reason)
		return automationBestCycleResult{Result: "candidate", Reason: reason, ProfileID: candidate.ID}, nil
	}

	status, applied := a.executeProviderProfileApply(networkApplyRequest{Operation: "provider", ProfileID: candidate.ID, Confirm: true})
	if status < 200 || status >= 300 || !applied.Success {
		reason := "Подтверждённый VPN не применён: " + strings.TrimSpace(applied.Error)
		rollback := applied.RollbackState
		if rollback == "" {
			rollback = "unknown"
		}
		writeAutomationStateV2("failed", reason, rollback, false)
		appendAutomationHistoryV2("failed", reason+"; rollback="+rollback)
		return automationBestCycleResult{Result: "failed", Reason: reason, RollbackState: rollback, ProfileID: candidate.ID}, errors.New("AUTO VPN apply failed")
	}
	reason := "Подтверждённый лучший VPN применён: " + candidate.Name
	writeAutomationStateV2("switched", reason, "yes", true)
	appendAutomationHistoryV2("success", reason)
	return automationBestCycleResult{Result: "switched", Reason: reason, Mutated: true, RollbackState: applied.RollbackState, ProfileID: candidate.ID}, nil
}

func automationCLIConfig() config {
	return config{
		Listen: defaultListen, VPNPath: defaultVPNPath, FilterPath: defaultFilterPath, OutPath: defaultOutPath,
		GeoDataDir: defaultGeoDataAssetDir, XKeenPath: defaultXKeenPath, LockPath: defaultLockPath,
		ConfigPath: defaultConfigPath, SubPath: defaultSubPath, SelfUpdatePath: defaultSelfUpdatePath,
		UpdateState: defaultUpdateState, UpdateLock: defaultUpdateLock, Timeout: 95 * time.Second,
	}
}

func init() {
	if len(os.Args) < 2 || os.Args[1] != "automation-best-run" {
		return
	}
	a := &app{cfg: automationCLIConfig(), sem: make(chan struct{}, 1)}
	result, err := a.runAutomationBestCycle(context.Background(), false)
	fmt.Printf("RESULT=%s\n", sanitizeAutomationReason(result.Result))
	fmt.Printf("REASON=%s\n", sanitizeAutomationReason(result.Reason))
	if result.Mutated {
		fmt.Println("MUTATION=APPLIED")
	} else {
		fmt.Println("MUTATION=NONE")
	}
	if err != nil {
		os.Exit(1)
	}
	os.Exit(0)
}
