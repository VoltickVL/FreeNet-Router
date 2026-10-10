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

	automationBestCooldown   = 6 * time.Hour
	automationBestBudgetSlack = 10 * time.Second
)

func automationBestForeignTimeout(policy string) time.Duration {
	target := automationBestEligibleTarget(policy)
	return bestServerRTTSweepTimeout(bestServerMaxCandidates) +
		bestServerConfirmedRTTSweepTimeout +
		time.Duration(target)*bestServerQualityCandidateTimeout +
		automationBestBudgetSlack
}

func automationBestQualityCycleTimeout(policy string) time.Duration {
	return bestServerCurrentScanTimeout + automationBestForeignTimeout(policy) + automationBestBudgetSlack
}

var errAutomationBusy = errors.New("AUTO VPN operation is already active")

var automationBestCurrentRefresh = func(a *app, ctx context.Context) (int, bestServerRefreshResponse) {
	return a.executeBestServerCurrentRefresh(ctx)
}

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

// Emergency recovery values restoration latency above decorative comparison:
// after a confirmed outage one fully measured Eligible replacement is enough
// to restore service. Quality optimization still compares two alternatives
// against the freshly measured current VPN (measured Top-3 = current + 2).
func automationBestEligibleTarget(policy string) int {
	if normalizeAutomationPolicy(policy) == automationPolicyDegraded {
		return 1
	}
	return 2
}

func automationNeedsForeignScan(policy, currentState string) bool {
	return normalizeAutomationPolicy(policy) != automationPolicyDegraded || currentState == "degraded"
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
			if len(code) != 2 || isUserExcludedVPNCountry(code) {
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
			strings.Contains(line, "freenet-ui automation-health-watch") ||
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
	if _, ok := automationCron(settings.Interval); !ok {
		return nil, errors.New("unsupported automation interval")
	}
	values := settingsV3ManagedCronValuesFromConfig(configPath)
	values["AUTO_VPN_V1"] = map[bool]string{true: "yes", false: "no"}[settings.Enabled]
	values["AUTO_VPN_MODE"] = normalizeAutomationMode(settings.Mode)
	values["AUTO_VPN_V1_INTERVAL"] = settings.Interval
	compat := &app{cfg: config{ConfigPath: configPath}}
	return buildManagedAutomationCronV3(compat, existing, values)
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
		"AUTO_VPN_FAILOVER":      "no",
	}
	if geoDataEnabled != nil {
		geo := map[bool]string{true: "yes", false: "no"}[*geoDataEnabled]
		values["AUTO_XKEEN_GEODATA"] = geo
		values["AUTO_GEODATA_ENABLED"] = geo
	}
	if err := writeAutomationConfigValues(a.cfg.ConfigPath, values); err != nil {
		return errors.New("cannot stage AUTO VPN settings")
	}
	_, err = a.reconcileSettingsV3Scheduler()
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

func automationCountryAllowed(settings automationSettings, currentCountry, candidateCountry string) bool {
	candidateCountry = strings.ToLower(strings.TrimSpace(candidateCountry))
	currentCountry = strings.ToLower(strings.TrimSpace(currentCountry))
	if candidateCountry == "" || isUserExcludedVPNCountry(candidateCountry) {
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
		// Backward-compatible stored value "region" now means the recommended
		// measured-response pool: every selectable foreign profile may compete.
		// Actual choice remains gated by measured VPN-path quality/RTT and strict
		// eligibility; geography itself is not a proxy for network distance.
		return true
	}
}

func automationClampedRelativeGain(current, candidate, floor float64, lowerIsBetter bool) float64 {
	if current <= 0 || candidate <= 0 {
		return 0
	}
	denom := current
	if denom < floor {
		denom = floor
	}
	gain := (candidate - current) / denom
	if lowerIsBetter {
		gain = -gain
	}
	if gain > 1 {
		return 1
	}
	if gain < -1 {
		return -1
	}
	return gain
}

func automationNoiseAwareLatencyGain(currentMS, candidateMS, currentSpread, candidateSpread int, floor float64) float64 {
	delta := bestServerNoiseAwareLatencyDelta(currentMS, candidateMS, currentSpread, candidateSpread)
	if delta == 0 || currentMS <= 0 {
		return 0
	}
	denom := float64(currentMS)
	if denom < floor {
		denom = floor
	}
	gain := float64(delta) / denom
	if gain > 1 {
		return 1
	}
	if gain < -1 {
		return -1
	}
	return gain
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
	// Stability-first comparison. Once both VPNs already pass strict service,
	// stall and minimum-throughput gates, extra Mbps is secondary. Application
	// latency, jitter and confirmed VPN RTT describe the user's "laggy vs smooth"
	// experience much better and therefore dominate AUTO optimization.
	if current.DownloadMbps <= 0 || candidate.DownloadMbps <= 0 ||
		current.ApplicationMS <= 0 || candidate.ApplicationMS <= 0 ||
		current.JitterMS <= 0 || candidate.JitterMS <= 0 ||
		current.VPNRTTMS <= 0 || candidate.VPNRTTMS <= 0 {
		return false
	}

	gain := 0.10 * automationClampedRelativeGain(current.DownloadMbps, candidate.DownloadMbps, 50, false)
	gain += 0.35 * automationNoiseAwareLatencyGain(
		current.ApplicationMS, candidate.ApplicationMS,
		current.JitterMS, candidate.JitterMS, 100,
	)
	gain += 0.30 * automationClampedRelativeGain(float64(current.JitterMS), float64(candidate.JitterMS), 20, true)
	gain += 0.25 * automationNoiseAwareLatencyGain(
		current.VPNRTTMS, candidate.VPNRTTMS,
		current.VPNJitterMS, candidate.VPNJitterMS, 100,
	)
	return gain >= 0.10
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

// Do not echo arbitrary subprocess errors into permanent Journal history.
// Only these fixed provider-helper reasons are known not to contain secrets.
func safeAutomationProviderPrimaryError(result networkApplyResponse) string {
	primary := strings.TrimSpace(result.PrimaryError)
	switch primary {
	case "another VPN mutation is already running",
		"cannot snapshot current provider state",
		"safe core-only Xray restart requires exactly one running Xray process",
		"safe core-only Xray restart is unavailable or runtime state is ambiguous",
		"cannot create FreeNet config directory",
		"cannot create profile filter directory",
		"cannot stage outbound candidate",
		"cannot commit outbound candidate",
		"cannot stage preferred profile",
		"cannot commit preferred profile",
		"cannot stage exact active profile filter",
		"cannot commit exact active profile filter",
		"Xray/XKeen runtime acceptance failed after provider apply",
		"live Xray configuration validation failed after provider apply",
		"live VPN application route validation failed after provider apply",
		"provider profile apply timed out":
		return primary
	}
	// Before an apply, only allow an exact fixed failure code; never leak
	// untrusted subscription/credential-bearing errors to the Journal.
	switch strings.TrimSpace(result.Error) {
	case "provider plan is not a validated application-ready candidate":
		return "provider plan rejected before mutation"
	case "another FreeNet operation is already running":
		return "concurrent VPN operation blocked before mutation"
	}
	return "не классифицирована (подробности скрыты для безопасности)"
}

func automationRollbackBlocksMutation(value string) bool {
	normalized := strings.ToUpper(strings.TrimSpace(value))
	if normalized == "" {
		return false
	}
	switch normalized {
	case "UNKNOWN", "FAILED/UNKNOWN", "FAILED_UNKNOWN":
		return true
	}
	return strings.Contains(normalized, "ROLLBACK FAILED") || strings.Contains(normalized, "ROLLBACK UNKNOWN")
}

// The persistent directory is a marker as well as a private rollback snapshot.
// A missing or unreadable state must never be treated as an accepted VPN switch.
func vpnTransactionPending() bool {
    path := strings.TrimSpace(os.Getenv("FREENET_VPN_TRANSACTION_DIR"))
    if path == "" { path = "/opt/var/lib/freenet/vpn-transaction.pending" }
    _, err := os.Lstat(path)
    return err == nil || !os.IsNotExist(err)
}

func automationMutationBlockedState() bool {
    return vpnTransactionPending() ||
        strings.EqualFold(strings.TrimSpace(parseAutomationState(automationStatePath())["MUTATION_BLOCKED"]), "yes")
}

func writeAutomationStatePayload(path string, values map[string]string) {
	payload := strings.Join([]string{
		"LAST_RUN=" + sanitizeAutomationReason(values["LAST_RUN"]),
		"LAST_RESULT=" + sanitizeAutomationReason(values["LAST_RESULT"]),
		"LAST_REASON=" + sanitizeAutomationReason(values["LAST_REASON"]),
		"ROLLBACK_READY=" + sanitizeAutomationReason(values["ROLLBACK_READY"]),
		"LAST_SWITCH=" + sanitizeAutomationReason(values["LAST_SWITCH"]),
		"MUTATION_BLOCKED=" + sanitizeAutomationReason(values["MUTATION_BLOCKED"]),
		"POST_UPDATE_ACK=" + sanitizeAutomationReason(values["POST_UPDATE_ACK"]),
	}, "\n") + "\n"
	_ = os.MkdirAll(filepath.Dir(path), 0755)
	tmp := path + ".v2.new"
	if err := os.WriteFile(tmp, []byte(payload), 0600); err == nil {
		_ = os.Rename(tmp, path)
	}
}

func setAutomationMutationBlocked(blocked bool) {
	path := automationStatePath()
	values := parseAutomationState(path)
	if values["LAST_RUN"] == "" {
		values["LAST_RUN"] = time.Now().UTC().Format(time.RFC3339)
	}
	if values["ROLLBACK_READY"] == "" {
		values["ROLLBACK_READY"] = "no"
	}
	if blocked {
		values["MUTATION_BLOCKED"] = "yes"
	} else {
		values["MUTATION_BLOCKED"] = "no"
	}
	writeAutomationStatePayload(path, values)
}

func automationPendingPostUpdateTarget(a *app) string {
	if a == nil || strings.TrimSpace(a.cfg.UpdateState) == "" {
		return ""
	}
	kv := readStateFile(a.cfg.UpdateState)
	if !strings.EqualFold(strings.TrimSpace(kv["STATE"]), "SUCCESS") {
		return ""
	}
	target := strings.TrimSpace(kv["TARGET_VERSION"])
	if target == "" || target != "v"+version {
		return ""
	}
	state := parseAutomationState(automationStatePath())
	if strings.TrimSpace(state["POST_UPDATE_ACK"]) == target {
		return ""
	}
	return target
}

func setAutomationPostUpdateAck(target string) {
	target = strings.TrimSpace(target)
	if target == "" {
		return
	}
	path := automationStatePath()
	values := parseAutomationState(path)
	values["POST_UPDATE_ACK"] = target
	if values["LAST_RUN"] == "" {
		values["LAST_RUN"] = time.Now().UTC().Format(time.RFC3339)
	}
	if values["ROLLBACK_READY"] == "" {
		values["ROLLBACK_READY"] = "no"
	}
	if values["MUTATION_BLOCKED"] == "" {
		values["MUTATION_BLOCKED"] = "no"
	}
	writeAutomationStatePayload(path, values)
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
	blocked := strings.EqualFold(strings.TrimSpace(previous["MUTATION_BLOCKED"]), "yes") || automationRollbackBlocksMutation(rollback)
	values := map[string]string{
		"LAST_RUN": time.Now().UTC().Format(time.RFC3339),
		"LAST_RESULT": result,
		"LAST_REASON": reason,
		"ROLLBACK_READY": rollback,
		"LAST_SWITCH": lastSwitch,
		"MUTATION_BLOCKED": "no",
		"POST_UPDATE_ACK": strings.TrimSpace(previous["POST_UPDATE_ACK"]),
	}
	if blocked {
		values["MUTATION_BLOCKED"] = "yes"
	}
	writeAutomationStatePayload(path, values)
}

func appendAutomationHistoryV2(result, reason string) {
	line := fmt.Sprintf("%s\tAUTO VPN\t%s\t%s\n", time.Now().UTC().Format(time.RFC3339), sanitizeAutomationReason(result), sanitizeAutomationReason(reason))
	appendBoundedJournalLine(automationHistoryPath(), line)
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
	all = withoutBestServerCurrentLogicalAlternatives(all, currentEndpoint, currentFilter, currentExactProfileLabel(a.cfg.FilterPath))
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
	targetEligible := automationBestEligibleTarget(settings.Policy)
	if normalizeAutomationPolicy(settings.Policy) == automationPolicyBetter && !currentBaselineOK {
		// Quality optimization normally has a fresh current baseline from the
		// immediately preceding current scan. If that evidence disappeared,
		// fail toward a full three-alternative comparison rather than silently
		// presenting an incomplete Top-3.
		targetEligible = bestServerVisibleAlternatives
	}
	response := a.rankMeasuredBestServerBatches(ctx, filtered, profilesScanned, truncated, currentEndpoint, currentFilter, targetEligible)
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
	if err := a.attachBestServerSelectionSnapshot(&response, filtered, currentEndpoint, currentFilter); err != nil {
		return bestServerQualityResponse{}, err
	}
	return response, nil
}

func bestAutomationCandidate(response bestServerQualityResponse) (bestServerQualityCandidate, bool) {
	for _, candidate := range response.Candidates {
		if candidate.Current || !candidate.Eligible || !candidate.Available || !validProfileID(candidate.ID) {
			continue
		}
		if display := profileDisplayName(candidate.Name); display != "" {
			candidate.Name = display
		}
		return candidate, true
	}
	return bestServerQualityCandidate{}, false
}

func automationCooldownReason(candidate bestServerQualityCandidate) string {
	name := profileDisplayName(candidate.Name)
	if name == "" {
		name = "VPN"
	}
	return fmt.Sprintf("Новый VPN найден: %s [VPN %d мс, сайты %d мс, скорость %.0f Мбит/с, стабильность %d мс], но текущий VPN всё ещё исправен и действует 6-часовая защита от лишних переключений.",
		name, candidate.VPNRTTMS, candidate.ApplicationMS, candidate.DownloadMbps, candidate.JitterMS)
}

func automationBestEmergencySelectionSummary(selected bestServerQualityCandidate) string {
	name := profileDisplayName(selected.Name)
	if name == "" {
		name = "VPN"
	}
	return fmt.Sprintf("AUTO VPN emergency: первый fully measured Eligible replacement — %s [VPN %d мс, сайты %d мс, скорость %.0f Мбит/с, стабильность %d мс].",
		name, selected.VPNRTTMS, selected.ApplicationMS, selected.DownloadMbps, selected.JitterMS)
}

func automationBestSelectionSummary(response bestServerQualityResponse, selected bestServerQualityCandidate) string {
	parts := make([]string, 0, bestServerVisibleAlternatives)
	for _, candidate := range response.Candidates {
		if candidate.Current || !candidate.Eligible || !candidate.Available {
			continue
		}
		name := profileDisplayName(candidate.Name)
		if name == "" {
			name = "VPN"
		}
		parts = append(parts, fmt.Sprintf("%s [VPN %d мс, сайты %d мс, скорость %.0f Мбит/с, стабильность %d мс]",
			name, candidate.VPNRTTMS, candidate.ApplicationMS, candidate.DownloadMbps, candidate.JitterMS))
		if len(parts) >= bestServerVisibleAlternatives {
			break
		}
	}
	selectedName := profileDisplayName(selected.Name)
	if selectedName == "" {
		selectedName = "VPN"
	}
	if len(parts) == 0 {
		return "AUTO VPN выбрал " + selectedName + "; сравнимый Top-3 отсутствует."
	}
	return "AUTO VPN Top-3: " + strings.Join(parts, "; ") + ". Выбран: " + selectedName + "."
}

func (a *app) runAutomationBestCycle(parent context.Context, manual bool) (automationBestCycleResult, error) {
	return a.runAutomationBestCycleWithSettings(parent, readAutomationSettings(a.cfg.ConfigPath), manual)
}

func (a *app) runAutomationBestCycleWithSettings(parent context.Context, settings automationSettings, manual bool) (automationBestCycleResult, error) {
	settings.Mode = normalizeAutomationMode(settings.Mode)
	settings.Policy = normalizeAutomationPolicy(settings.Policy)
	settings.CountryScope = normalizeAutomationCountryScope(settings.CountryScope)
	settings.Countries = normalizeAutomationCountries(settings.Countries)
	if settings.Mode != automationModeBest {
		return automationBestCycleResult{Result: "same", Reason: "Режим «Лучший VPN автоматически» не выбран."}, nil
	}
	if !manual && !settings.Enabled {
		return automationBestCycleResult{Result: "disabled", Reason: "AUTO VPN выключен."}, nil
	}
	releaseHealth, err := acquireAutomationHealthLock()
	if err != nil {
		return automationBestCycleResult{Result: "busy", Reason: "Проверка пропущена: другая AUTO VPN health/recovery операция уже выполняется."}, nil
	}
	defer releaseHealth()
	if automationMutationBlockedState() {
		reason := "AUTO VPN mutation заблокирована после неподтверждённого rollback. Сначала требуется успешная read-only проверка фактического текущего VPN."
		appendAutomationHistoryV2("blocked", reason)
		return automationBestCycleResult{Result: "uncertain", Reason: reason, RollbackState: "FAILED/UNKNOWN"}, nil
	}

	release, err := acquireAutomationBestLock()
	if err != nil {
		return automationBestCycleResult{Result: "busy", Reason: "Проверка пропущена: другая AUTO VPN операция уже выполняется."}, nil
	}
	defer release()

	if settings.AutoEndpointUpdate && settings.AutoApply {
		refreshCtx, cancelRefresh := context.WithTimeout(parent, bestServerRefreshTimeout)
		refreshStatus, refresh := automationBestCurrentRefresh(a, refreshCtx)
		cancelRefresh()
		if refresh.Applied {
			reason := "AUTO VPN обновил endpoint текущего логического VPN и подтвердил доступ через обновлённое подключение."
			writeAutomationStateV2("updated", reason, refresh.RollbackState, false)
			appendAutomationHistoryV2("updated", reason)
			return automationBestCycleResult{Result: "updated", Reason: reason, Mutated: true, RollbackState: refresh.RollbackState}, nil
		}
		if refreshStatus < 200 || refreshStatus >= 300 || !refresh.Success {
			rollback := strings.TrimSpace(refresh.RollbackState)
			if rollback == "" {
				rollback = "NOT_APPLIED"
			}
			reason := "Проверка свежего endpoint текущего VPN не завершена безопасно; дальнейшее переключение в этом цикле отменено."
			if strings.TrimSpace(refresh.Error) != "" {
				reason += " " + strings.TrimSpace(refresh.Error)
			}
			result := "uncertain"
			if refresh.Mutation != "NONE" || rollback == "FAILED/UNKNOWN" {
				result = "failed"
			}
			writeAutomationStateV2(result, reason, rollback, false)
			appendAutomationHistoryV2(result, reason+"; rollback="+rollback)
			return automationBestCycleResult{Result: result, Reason: reason, RollbackState: rollback}, errors.New("AUTO VPN current endpoint refresh did not complete safely")
		}
	}

	ctx, cancel := context.WithTimeout(parent, automationBestQualityCycleTimeout(settings.Policy))
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
	if !automationNeedsForeignScan(settings.Policy, currentState) {
		reason := "Текущий VPN подтверждён как рабочий; policy «только при деградации» не требует поиска замены."
		writeAutomationStateV2("same", reason, "no", false)
		appendAutomationHistoryV2("same", reason)
		return automationBestCycleResult{Result: "same", Reason: reason}, nil
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
	if currentState == "healthy" && automationCooldownActive(parseAutomationLastSwitch(automationStatePath()), time.Now().UTC()) {
		reason := automationCooldownReason(candidate)
		writeAutomationStateV2("cooldown", reason, "no", false)
		appendAutomationHistoryV2("cooldown", reason)
		return automationBestCycleResult{Result: "cooldown", Reason: reason, ProfileID: candidate.ID}, nil
	}
	if !validBestServerSelectionToken(candidates.SelectionToken) {
		reason := "Найден подтверждённый VPN, но точный измеренный snapshot не сохранён; AUTO VPN не выполняет mutation."
		writeAutomationStateV2("failed", reason, "no", false)
		appendAutomationHistoryV2("failed", reason)
		return automationBestCycleResult{Result: "failed", Reason: reason, ProfileID: candidate.ID}, errors.New("AUTO VPN measured selection snapshot unavailable")
	}
	if !settings.AutoApply {
		reason := "Найден подтверждённый лучший VPN; автоматическое применение выключено."
		writeAutomationStateV2("candidate", reason, "no", false)
		appendAutomationHistoryV2("candidate", reason)
		return automationBestCycleResult{Result: "candidate", Reason: reason, ProfileID: candidate.ID}, nil
	}

	appendAutomationHistoryV2("selection", automationBestSelectionSummary(candidates, candidate))
	status, applied := a.executeProviderProfileApply(networkApplyRequest{
		Operation: "provider", ProfileID: candidate.ID, SelectionToken: candidates.SelectionToken, Confirm: true,
	})
	if status < 200 || status >= 300 || !applied.Success {
		reason := "Подтверждённый VPN не применён; PRIMARY ERROR: " + safeAutomationProviderPrimaryError(applied)
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

func automationCLIConfigPath(args []string) string {
	configPath := defaultConfigPath
	for i := 2; i+1 < len(args); i++ {
		if args[i] != "--config" {
			continue
		}
		candidate := strings.TrimSpace(args[i+1])
		if candidate != "" && filepath.IsAbs(candidate) {
			configPath = candidate
		}
		break
	}
	return configPath
}

func automationCLIConfig() config {
	configPath := automationCLIConfigPath(os.Args)
	return config{
		Listen: defaultListen, VPNPath: defaultVPNPath, FilterPath: defaultFilterPath, OutPath: defaultOutPath,
		GeoDataDir: defaultGeoDataAssetDir, XKeenPath: defaultXKeenPath, LockPath: defaultLockPath,
		ConfigPath: configPath, SubPath: defaultSubPath, SelfUpdatePath: defaultSelfUpdatePath,
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
