package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	settingsV3StatePathDefault   = "/opt/var/run/freenet-settings-v3.state"
	settingsV3HistoryPathDefault = "/opt/var/log/freenet-settings-v3.history"
	settingsV3BackupRootDefault  = "/opt/backups/freenet-settings"
	settingsV3UpdaterDefault     = "/opt/bin/blanc_xkeen_update_outbounds.sh"
)

type settingsV3Schedule struct {
	Enabled  bool   `json:"enabled"`
	Interval string `json:"interval"`
	LastRun  string `json:"last_run,omitempty"`
	NextRun  string `json:"next_run,omitempty"`
	Result   string `json:"result,omitempty"`
	Message  string `json:"message,omitempty"`
}

type settingsV3AutoVPN struct {
	Enabled          bool     `json:"enabled"`
	Mode             string   `json:"mode"`
	EndpointInterval string   `json:"endpoint_interval"`
	CountryScope     string   `json:"country_scope"`
	Countries        []string `json:"countries"`
	LastHealth       string   `json:"last_health,omitempty"`
	NextHealth       string   `json:"next_health,omitempty"`
}

type settingsV3BackupInfo struct {
	Root       string `json:"root"`
	Latest     string `json:"latest,omitempty"`
	LatestPath string `json:"latest_path,omitempty"`
	Tracked    int    `json:"tracked"`
}

type settingsV3Response struct {
	Success      bool                 `json:"success"`
	AutoVPN      settingsV3AutoVPN    `json:"auto_vpn"`
	Automation   automationResponse   `json:"automation"`
	Subscription settingsV3Schedule   `json:"subscription"`
	GeoData      settingsV3Schedule   `json:"geodata"`
	FreeNet      settingsV3Schedule   `json:"freenet"`
	Backup       settingsV3Schedule   `json:"backup"`
	BackupInfo   settingsV3BackupInfo `json:"backup_info"`
	Events       []automationEvent    `json:"events"`
	Error        string               `json:"error,omitempty"`
}

type settingsV3SaveRequest struct {
	Action                   string   `json:"action"`
	AutoVPNEnabled           *bool    `json:"auto_vpn_enabled,omitempty"`
	AutoVPNMode              string   `json:"auto_vpn_mode,omitempty"`
	AutoVPNEndpointInterval  string   `json:"auto_vpn_endpoint_interval,omitempty"`
	CountryScope             string   `json:"country_scope,omitempty"`
	Countries            []string `json:"countries,omitempty"`
	SubscriptionEnabled  *bool    `json:"subscription_enabled,omitempty"`
	SubscriptionInterval string   `json:"subscription_interval,omitempty"`
	GeoDataEnabled       *bool    `json:"geodata_enabled,omitempty"`
	GeoDataInterval      string   `json:"geodata_interval,omitempty"`
	FreeNetEnabled       *bool    `json:"freenet_enabled,omitempty"`
	FreeNetInterval      string   `json:"freenet_interval,omitempty"`
	BackupEnabled        *bool    `json:"backup_enabled,omitempty"`
	BackupInterval       string   `json:"backup_interval,omitempty"`
}

type settingsV3ActionRequest struct {
	Action string `json:"action"`
}

type settingsV3ActionResponse struct {
	Success           bool                  `json:"success"`
	Message           string                `json:"message,omitempty"`
	Profiles          []subscriptionProfile `json:"profiles,omitempty"`
	ProfilesAvailable int                   `json:"profiles_available,omitempty"`
	ProfilesStale     bool                  `json:"profiles_stale,omitempty"`
	ProfilesUpdatedAt string                `json:"profiles_updated_at,omitempty"`
	BackupInfo        *settingsV3BackupInfo `json:"backup_info,omitempty"`
	Error             string                `json:"error,omitempty"`
}

func registerSettingsV3API(mux *http.ServeMux, a *app) {
	mux.HandleFunc("GET /api/settings-v3", a.requireAuth(a.handleSettingsV3Get))
	mux.HandleFunc("POST /api/settings-v3", a.requireAuth(a.handleSettingsV3Save))
	mux.HandleFunc("POST /api/settings-v3/action", a.requireAuth(a.handleSettingsV3Action))
}

func settingsV3StatePath() string {
	if value := strings.TrimSpace(os.Getenv("FREENET_SETTINGS_V3_STATE")); value != "" {
		return value
	}
	return settingsV3StatePathDefault
}

func settingsV3HistoryPath() string {
	if value := strings.TrimSpace(os.Getenv("FREENET_SETTINGS_V3_HISTORY")); value != "" {
		return value
	}
	return settingsV3HistoryPathDefault
}

func settingsV3BackupRoot() string {
	if value := strings.TrimSpace(os.Getenv("FREENET_SETTINGS_BACKUP_ROOT")); value != "" {
		return value
	}
	return settingsV3BackupRootDefault
}

func latestV3BackupSnapshot() (string, string, error) {
	root := settingsV3BackupRoot()
	latestRaw, err := os.ReadFile(filepath.Join(root, "latest"))
	if err != nil {
		return "", "", errors.New("backup is unavailable")
	}
	name := strings.TrimSpace(string(latestRaw))
	if name == "" || filepath.Base(name) != name || !strings.HasPrefix(name, "backup-") {
		return "", "", errors.New("backup reference is invalid")
	}
	source := filepath.Join(root, name)
	info, err := os.Stat(source)
	if err != nil || !info.IsDir() {
		return "", "", errors.New("backup directory is unavailable")
	}
	return name, source, nil
}

func (a *app) v3BackupInfo() settingsV3BackupInfo {
	info := settingsV3BackupInfo{
		Root:    settingsV3BackupRoot(),
		Tracked: len(a.v3BackupTrackedFiles()),
	}
	name, path, err := latestV3BackupSnapshot()
	if err == nil {
		info.Latest = name
		info.LatestPath = path
	}
	return info
}

func settingsV3UpdaterPath() string {
	if value := strings.TrimSpace(os.Getenv("FREENET_SUBSCRIPTION_UPDATER")); value != "" {
		return value
	}
	return settingsV3UpdaterDefault
}

func v3Bool(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}

func v3IntervalDuration(interval string) time.Duration {
	switch strings.TrimSpace(interval) {
	case "30m":
		return 30 * time.Minute
	case "1h":
		return time.Hour
	case "3h":
		return 3 * time.Hour
	case "6h":
		return 6 * time.Hour
	case "12h":
		return 12 * time.Hour
	case "24h":
		return 24 * time.Hour
	default:
		return 0
	}
}

func v3IntervalCron(interval string) (string, bool) {
	switch strings.TrimSpace(interval) {
	case "30m":
		return "*/30 * * * *", true
	case "1h":
		return "0 * * * *", true
	case "3h":
		return "0 */3 * * *", true
	case "6h":
		return "0 */6 * * *", true
	case "12h":
		return "0 */12 * * *", true
	case "24h":
		return "17 4 * * *", true
	default:
		return "", false
	}
}

func v3DefaultInterval(key string) string {
	switch key {
	case "auto_vpn_endpoint":
		return "1h"
	case "subscription":
		return "6h"
	case "geodata":
		return "3h"
	case "freenet":
		return "12h"
	case "backup":
		return "24h"
	default:
		return "6h"
	}
}

func v3NormalizeInterval(value, key string) string {
	if v3IntervalDuration(value) > 0 {
		return strings.TrimSpace(value)
	}
	return v3DefaultInterval(key)
}

func v3ParseState(path string) map[string]string {
	out := map[string]string{}
	data, err := os.ReadFile(path)
	if err != nil {
		return out
	}
	for _, raw := range strings.Split(strings.ReplaceAll(string(data), "\r", ""), "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(raw), "=")
		if ok && key != "" {
			out[key] = strings.TrimSpace(value)
		}
	}
	return out
}

func v3WriteState(values map[string]string) error {
	path := settingsV3StatePath()
	current := v3ParseState(path)
	for key, value := range values {
		current[key] = sanitizeAutomationReason(value)
	}
	keys := make([]string, 0, len(current))
	for key := range current {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, key := range keys {
		fmt.Fprintf(&b, "%s=%s\n", key, strings.ReplaceAll(current[key], "\n", " "))
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	tmp := path + ".new"
	if err := os.WriteFile(tmp, []byte(b.String()), 0600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func v3AppendEvent(kind, result, message string) {
	path := settingsV3HistoryPath()
	_ = os.MkdirAll(filepath.Dir(path), 0755)
	line := time.Now().UTC().Format(time.RFC3339) + "\t" + sanitizeAutomationReason(kind) + "\t" + sanitizeAutomationReason(result) + "\t" + sanitizeAutomationReason(message) + "\n"
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err == nil {
		_, _ = file.WriteString(line)
		_ = file.Close()
	}
}

func v3Next(last, interval string) string {
	if last == "" {
		return ""
	}
	t, err := time.Parse(time.RFC3339, last)
	if err != nil {
		return ""
	}
	d := v3IntervalDuration(interval)
	if d <= 0 {
		return ""
	}
	return t.Add(d).Format(time.RFC3339)
}

func v3ScheduleFromConfig(configPath, prefix, key string, defaultEnabled bool) settingsV3Schedule {
	state := v3ParseState(settingsV3StatePath())
	enabledDefault := "no"
	if defaultEnabled {
		enabledDefault = "yes"
	}
	enabled := automationConfigValue(configPath, prefix+"_ENABLED", enabledDefault) == "yes"
	interval := v3NormalizeInterval(automationConfigValue(configPath, prefix+"_INTERVAL", v3DefaultInterval(key)), key)
	upper := strings.ToUpper(key)
	last := state[upper+"_LAST"]
	return settingsV3Schedule{
		Enabled: enabled, Interval: interval, LastRun: last, NextRun: v3Next(last, interval),
		Result: state[upper+"_RESULT"], Message: state[upper+"_MESSAGE"],
	}
}

func v3HealthTimes(enabled bool) (string, string) {
	if !enabled {
		return "", ""
	}
	state := v3ParseState(settingsV3StatePath())
	last := state["HEALTH_LAST"]
	if last == "" {
		return "", time.Now().UTC().Add(5 * time.Minute).Format(time.RFC3339)
	}
	t, err := time.Parse(time.RFC3339, last)
	if err != nil {
		return last, ""
	}
	return last, t.Add(5 * time.Minute).Format(time.RFC3339)
}

func v3MergeEvents(limit int, groups ...[]automationEvent) []automationEvent {
	total := 0
	for _, group := range groups {
		total += len(group)
	}
	events := make([]automationEvent, 0, total)
	for _, group := range groups {
		events = append(events, group...)
	}
	sort.SliceStable(events, func(i, j int) bool {
		left, leftErr := time.Parse(time.RFC3339, events[i].At)
		right, rightErr := time.Parse(time.RFC3339, events[j].At)
		switch {
		case leftErr == nil && rightErr == nil:
			return left.After(right)
		case leftErr == nil:
			return true
		case rightErr == nil:
			return false
		default:
			return false
		}
	})
	if limit > 0 && len(events) > limit {
		events = events[:limit]
	}
	return events
}

func canonicalJournalEvents(limit int) []automationEvent {
	if limit <= 0 {
		limit = 50
	}
	automationEvents := readAutomationEvents(automationHistoryPath(), limit)
	filteredAutomation := make([]automationEvent, 0, len(automationEvents))
	for _, event := range automationEvents {
		// Provider helper writes legacy generic switch rows into the automation
		// history. Canonical manual VPN events are recorded explicitly by the
		// HTTP handlers, so keep the legacy row out of the user-facing journal
		// to avoid duplicate/misclassified AUTO VPN entries.
		if strings.EqualFold(strings.TrimSpace(event.Kind), "VPN switch") {
			continue
		}
		filteredAutomation = append(filteredAutomation, event)
	}
	settingsEvents := readAutomationEvents(settingsV3HistoryPath(), limit)
	return v3MergeEvents(limit, filteredAutomation, settingsEvents)
}

func (a *app) settingsV3Snapshot() settingsV3Response {
	auto := a.automationSnapshot()
	scope := normalizeAutomationCountryScope(auto.Settings.CountryScope)
	if scope == "" {
		scope = automationCountryRegion
	}
	lastHealth, nextHealth := v3HealthTimes(auto.Settings.Enabled)
	events := canonicalJournalEvents(50)
	subscription := v3ScheduleFromConfig(a.cfg.ConfigPath, "AUTO_SUBSCRIPTION_REFRESH", "subscription", true)
	if subscription.Enabled {
		subscription.NextRun = subscriptionNextCronRun(subscription.Interval, time.Now())
	}
	mode := normalizeAutomationMode(auto.Settings.Mode)
	endpointInterval := auto.Settings.Interval
	if v3IntervalDuration(endpointInterval) <= 0 {
		endpointInterval = v3DefaultInterval("auto_vpn_endpoint")
	}
	return settingsV3Response{
		Success: true,
		AutoVPN: settingsV3AutoVPN{
			Enabled: auto.Settings.Enabled, Mode: mode, EndpointInterval: endpointInterval,
			CountryScope: scope, Countries: auto.Settings.Countries, LastHealth: lastHealth, NextHealth: nextHealth,
		},
		Automation: auto,
		Subscription: subscription,
		GeoData: v3ScheduleFromConfig(a.cfg.ConfigPath, "AUTO_GEODATA", "geodata", automationConfigValue(a.cfg.ConfigPath, "AUTO_XKEEN_GEODATA", "yes") == "yes"),
		FreeNet:    v3ScheduleFromConfig(a.cfg.ConfigPath, "AUTO_FREENET_CHECK", "freenet", false),
		Backup:     v3ScheduleFromConfig(a.cfg.ConfigPath, "AUTO_BACKUP", "backup", false),
		BackupInfo: a.v3BackupInfo(),
		Events:     events,
	}
}

func (a *app) handleSettingsV3Get(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, a.settingsV3Snapshot())
}

func v3ShellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "") + "'"
}

func buildManagedAutomationCronV3(a *app, existing []byte, values map[string]string) ([]byte, error) {
	lines := stripManagedAutomationCron(existing)
	lines = append(lines, "# BEGIN FREENET")
	bin := v3ShellQuote(automationUIBinary())
	configArg := " --config " + v3ShellQuote(a.cfg.ConfigPath)

	if values["AUTO_VPN_V1"] == "yes" {
		lines = append(lines, "*/5 * * * * "+bin+" automation-health-watch"+configArg)
		if normalizeAutomationMode(values["AUTO_VPN_MODE"]) == automationModeEndpoint {
			cron, ok := v3IntervalCron(values["AUTO_VPN_V1_INTERVAL"])
			if !ok {
				return nil, errors.New("unsupported AUTO VPN endpoint interval")
			}
			lines = append(lines, cron+" "+bin+" settings-v3-endpoint-refresh"+configArg)
		}
	}
	appendJob := func(enabledKey, intervalKey, command string) error {
		if values[enabledKey] != "yes" {
			return nil
		}
		cron, ok := v3IntervalCron(values[intervalKey])
		if !ok {
			return fmt.Errorf("unsupported interval for %s", enabledKey)
		}
		lines = append(lines, cron+" "+bin+" "+command+configArg)
		return nil
	}
	if err := appendJob("AUTO_SUBSCRIPTION_REFRESH_ENABLED", "AUTO_SUBSCRIPTION_REFRESH_INTERVAL", "settings-v3-subscription"); err != nil {
		return nil, err
	}
	if err := appendJob("AUTO_GEODATA_ENABLED", "AUTO_GEODATA_INTERVAL", "settings-v3-geodata"); err != nil {
		return nil, err
	}
	if err := appendJob("AUTO_FREENET_CHECK_ENABLED", "AUTO_FREENET_CHECK_INTERVAL", "settings-v3-freenet-check"); err != nil {
		return nil, err
	}
	if err := appendJob("AUTO_BACKUP_ENABLED", "AUTO_BACKUP_INTERVAL", "settings-v3-backup"); err != nil {
		return nil, err
	}
	lines = append(lines, "# END FREENET")
	return []byte(strings.TrimRight(strings.Join(lines, "\n"), "\n") + "\n"), nil
}

func settingsV3ManagedCronValuesFromConfig(configPath string) map[string]string {
	geodataDefault := automationConfigValue(configPath, "AUTO_XKEEN_GEODATA", "yes")
	return map[string]string{
		"AUTO_VPN_V1": automationConfigValue(configPath, "AUTO_VPN_V1", "no"),
		"AUTO_VPN_MODE": normalizeAutomationMode(automationConfigValue(configPath, "AUTO_VPN_MODE", automationModeBest)),
		"AUTO_VPN_V1_INTERVAL": v3NormalizeInterval(
			automationConfigValue(configPath, "AUTO_VPN_V1_INTERVAL", v3DefaultInterval("auto_vpn_endpoint")),
			"auto_vpn_endpoint",
		),
		"AUTO_SUBSCRIPTION_REFRESH_ENABLED": automationConfigValue(configPath, "AUTO_SUBSCRIPTION_REFRESH_ENABLED", "yes"),
		"AUTO_SUBSCRIPTION_REFRESH_INTERVAL": v3NormalizeInterval(automationConfigValue(configPath, "AUTO_SUBSCRIPTION_REFRESH_INTERVAL", v3DefaultInterval("subscription")), "subscription"),
		"AUTO_GEODATA_ENABLED": automationConfigValue(configPath, "AUTO_GEODATA_ENABLED", geodataDefault),
		"AUTO_GEODATA_INTERVAL": v3NormalizeInterval(automationConfigValue(configPath, "AUTO_GEODATA_INTERVAL", v3DefaultInterval("geodata")), "geodata"),
		"AUTO_FREENET_CHECK_ENABLED": automationConfigValue(configPath, "AUTO_FREENET_CHECK_ENABLED", "no"),
		"AUTO_FREENET_CHECK_INTERVAL": v3NormalizeInterval(automationConfigValue(configPath, "AUTO_FREENET_CHECK_INTERVAL", v3DefaultInterval("freenet")), "freenet"),
		"AUTO_BACKUP_ENABLED": automationConfigValue(configPath, "AUTO_BACKUP_ENABLED", "no"),
		"AUTO_BACKUP_INTERVAL": v3NormalizeInterval(automationConfigValue(configPath, "AUTO_BACKUP_INTERVAL", v3DefaultInterval("backup")), "backup"),
	}
}

func (a *app) saveSettingsV3(req settingsV3SaveRequest) error {
	if req.AutoVPNEnabled == nil || req.SubscriptionEnabled == nil || req.GeoDataEnabled == nil || req.FreeNetEnabled == nil || req.BackupEnabled == nil {
		return errors.New("all automation switches are required")
	}
	currentAuto := readAutomationSettings(a.cfg.ConfigPath)
	mode := currentAuto.Mode
	if strings.TrimSpace(req.AutoVPNMode) != "" {
		mode = normalizeAutomationMode(req.AutoVPNMode)
	}
	endpointInterval := strings.TrimSpace(req.AutoVPNEndpointInterval)
	if v3IntervalDuration(endpointInterval) <= 0 {
		if mode == automationModeEndpoint && v3IntervalDuration(currentAuto.Interval) > 0 {
			endpointInterval = currentAuto.Interval
		} else {
			endpointInterval = v3DefaultInterval("auto_vpn_endpoint")
		}
	}
	scope := normalizeAutomationCountryScope(req.CountryScope)
	if scope != automationCountryCurrent && scope != automationCountryRegion && scope != automationCountryAllowlist {
		return errors.New("unsupported replacement geography")
	}
	countries := normalizeAutomationCountries(req.Countries)
	if mode == automationModeBest && scope == automationCountryAllowlist && len(countries) == 0 {
		return errors.New("selected countries list is empty")
	}
	if *req.AutoVPNEnabled && mode == automationModeEndpoint {
		if _, err := ensureAutomationHelper(); err != nil {
			return err
		}
	}
	autoInterval := "manual"
	autoEndpointUpdate := "no"
	autoEndpointCron := ""
	if mode == automationModeEndpoint {
		autoInterval = endpointInterval
		autoEndpointUpdate = v3Bool(*req.AutoVPNEnabled)
		if cron, ok := v3IntervalCron(endpointInterval); ok {
			autoEndpointCron = cron
		}
	}
	values := map[string]string{
		"AUTO_VPN_V1": v3Bool(*req.AutoVPNEnabled),
		"AUTO_VPN_V1_INTERVAL": autoInterval,
		"AUTO_VPN_MODE": mode,
		"AUTO_VPN_POLICY": automationPolicyDegraded,
		"AUTO_VPN_COUNTRY_SCOPE": scope,
		"AUTO_VPN_COUNTRIES": strings.Join(countries, ","),
		"AUTO_VPN_AUTO_APPLY": "yes",
		"AUTO_VPN_FAILOVER": "no",
		"AUTO_ENDPOINT_UPDATE": autoEndpointUpdate,
		"AUTO_ENDPOINT_CRON": autoEndpointCron,
		"AUTO_SUBSCRIPTION_REFRESH_ENABLED": v3Bool(*req.SubscriptionEnabled),
		"AUTO_SUBSCRIPTION_REFRESH_INTERVAL": v3NormalizeInterval(req.SubscriptionInterval, "subscription"),
		"AUTO_GEODATA_ENABLED": v3Bool(*req.GeoDataEnabled),
		"AUTO_GEODATA_INTERVAL": v3NormalizeInterval(req.GeoDataInterval, "geodata"),
		"AUTO_XKEEN_GEODATA": v3Bool(*req.GeoDataEnabled),
		"AUTO_FREENET_CHECK_ENABLED": v3Bool(*req.FreeNetEnabled),
		"AUTO_FREENET_CHECK_INTERVAL": v3NormalizeInterval(req.FreeNetInterval, "freenet"),
		"AUTO_BACKUP_ENABLED": v3Bool(*req.BackupEnabled),
		"AUTO_BACKUP_INTERVAL": v3NormalizeInterval(req.BackupInterval, "backup"),
	}
	if cron, ok := v3IntervalCron(values["AUTO_GEODATA_INTERVAL"]); ok {
		values["AUTO_XKEEN_GEODATA_CRON"] = cron
	}
	beforeConfig, err := os.ReadFile(a.cfg.ConfigPath)
	if err != nil {
		return errors.New("FreeNet config is unavailable")
	}
	beforeCron := readAutomationCrontab()
	if err := writeAutomationConfigValues(a.cfg.ConfigPath, values); err != nil {
		return errors.New("cannot stage settings")
	}
	managed, err := buildManagedAutomationCronV3(a, beforeCron, values)
	if err == nil {
		err = installAutomationCrontab(managed)
	}
	if err == nil {
		return nil
	}
	rollbackConfigErr := os.WriteFile(a.cfg.ConfigPath, beforeConfig, 0600)
	rollbackCronErr := installAutomationCrontab(beforeCron)
	if rollbackConfigErr != nil || rollbackCronErr != nil {
		return errors.New("settings apply failed; rollback failed or is unknown")
	}
	return errors.New("settings apply failed; previous config and scheduler restored")
}

func (a *app) handleSettingsV3Save(w http.ResponseWriter, r *http.Request) {
	if a.mutationBlockedBySelfUpdate(w) {
		return
	}
	var req settingsV3SaveRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil || strings.TrimSpace(req.Action) != "save" {
		writeJSON(w, http.StatusBadRequest, settingsV3Response{Success: false, Error: "invalid settings request"})
		return
	}
	if err := a.saveSettingsV3(req); err != nil {
		writeJSON(w, http.StatusBadGateway, settingsV3Response{Success: false, Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, a.settingsV3Snapshot())
}

func v3Mark(kind, result, message string) {
	now := time.Now().UTC().Format(time.RFC3339)
	upper := strings.ToUpper(kind)
	_ = v3WriteState(map[string]string{upper + "_LAST": now, upper + "_RESULT": result, upper + "_MESSAGE": message})
	v3AppendEvent(kind, result, message)
}

func (a *app) runV3SubscriptionResult(ctx context.Context) (subscriptionRefreshResult, error) {
	result, err := a.refreshSubscriptionProfiles(ctx)
	visible := selectableSubscriptionProfiles(result.Profiles)
	if err != nil {
		if len(visible) > 0 && result.Stale {
			v3Mark("subscription", "failed", fmt.Sprintf("Свежий список VPN не получен. Используется последний успешный зарубежный Extra-каталог: %d.", len(visible)))
		} else {
			v3Mark("subscription", "failed", "Не удалось получить безопасный список VPN из подписки.")
		}
		return result, err
	}
	v3Mark("subscription", "success", fmt.Sprintf("Подписка проверена. Доступно зарубежных Extra-профилей: %d.", len(visible)))
	return result, nil
}

func (a *app) runV3Subscription(ctx context.Context) error {
	_, err := a.runV3SubscriptionResult(ctx)
	return err
}

func subscriptionHasFreshEndpointForCurrent(profiles []subscriptionProfile, currentEndpoint, currentFilter, exactLabel string) bool {
	currentEndpoint = strings.TrimSpace(currentEndpoint)
	currentFilter = strings.TrimSpace(currentFilter)
	if currentEndpoint == "" || currentFilter == "" {
		return false
	}
	matcher, err := regexp.Compile(currentFilter)
	if err != nil {
		return false
	}
	candidates := make([]bestServerInternalCandidate, 0, len(profiles))
	for _, profile := range profiles {
		candidates = append(candidates, bestServerInternalCandidate{Profile: profile})
	}
	_, ok := bestServerFreshCandidateForCurrent(candidates, matcher, exactLabel, currentEndpoint)
	return ok
}

var settingsV3ScheduledCurrentEndpointChanged = func(a *app, profiles []subscriptionProfile) bool {
	return subscriptionHasFreshEndpointForCurrent(
		profiles,
		readBestServerCurrentEndpoint(a.cfg.OutPath),
		readBestServerCurrentFilter(a.cfg.FilterPath),
		currentExactProfileLabel(a.cfg.FilterPath),
	)
}

var settingsV3ScheduledCurrentRefresh = func(a *app, ctx context.Context) (int, bestServerRefreshResponse) {
	return a.executeBestServerCurrentRefresh(ctx)
}

var settingsV3ScheduledEndpointRefresh = func(a *app, ctx context.Context) error {
	helper, err := ensureAutomationHelper()
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, helper, "run")
	cmd.Env = append(os.Environ(),
		"FREENET_CONFIG_FILE="+a.cfg.ConfigPath,
		"FREENET_SUB_FILE="+a.cfg.SubPath,
		"FREENET_FILTER_FILE="+a.cfg.FilterPath,
		"FREENET_OUT_FILE="+a.cfg.OutPath,
		"FREENET_XKEEN_BIN="+a.cfg.XKeenPath,
	)
	out, err := cmd.CombinedOutput()
	if err == nil {
		return nil
	}
	lower := strings.ToLower(sanitizeOutput(string(out)))
	if strings.Contains(lower, "another auto vpn operation") || automationEndpointUpdateBusy(out) {
		return errAutomationBusy
	}
	if automationEndpointRollbackUnknown(out) {
		return errors.New("scheduled endpoint refresh rollback failed or is unknown")
	}
	if reason := safeAutomationHelperError(out); reason != "" {
		return errors.New(reason)
	}
	return err
}

func (a *app) runV3ScheduledEndpointRefresh(ctx context.Context) error {
	settings := readAutomationSettings(a.cfg.ConfigPath)
	if !settings.Enabled || !settings.AutoApply || settings.Mode != automationModeEndpoint {
		return nil
	}
	release, lockErr := acquireAutomationHealthLock()
	if lockErr != nil {
		appendAutomationHistoryV2("busy", "Плановое обновление endpoint пропущено: другая AUTO VPN операция уже выполняется.")
		return nil
	}
	defer release()
	err := settingsV3ScheduledEndpointRefresh(a, ctx)
	if errors.Is(err, errAutomationBusy) {
		appendAutomationHistoryV2("busy", "Плановое обновление endpoint пропущено: другой безопасный updater уже выполняется.")
		return nil
	}
	return err
}

func (a *app) runV3ScheduledSubscription(ctx context.Context) error {
	subscription, err := a.runV3SubscriptionResult(ctx)
	if err != nil {
		return err
	}
	settings := readAutomationSettings(a.cfg.ConfigPath)
	if !settings.Enabled || !settings.AutoApply || !settingsV3ScheduledCurrentEndpointChanged(a, subscription.Profiles) {
		return nil
	}

	release, lockErr := acquireAutomationHealthLock()
	if lockErr != nil {
		message := "Свежий endpoint найден после обновления подписки, но AUTO VPN уже выполняет другую проверку. Текущий VPN не изменён."
		appendAutomationHistoryV2("busy", message)
		return nil
	}
	defer release()

	status, refresh := settingsV3ScheduledCurrentRefresh(a, ctx)
	message := strings.TrimSpace(refresh.Message)
	if message == "" {
		message = strings.TrimSpace(refresh.Error)
	}
	if refresh.Applied {
		if message == "" {
			message = "AUTO VPN обновил endpoint текущего профиля после обновления подписки."
		}
		writeAutomationStateV2("updated", message, refresh.RollbackState, false)
		appendAutomationHistoryV2("updated", message)
		return nil
	}
	if status >= 200 && status < 300 && refresh.Success {
		if refresh.Outcome == "no_new" {
			return nil
		}
		if message == "" {
			message = "Свежий endpoint текущего профиля не применён; рабочий VPN сохранён."
		}
		appendAutomationHistoryV2("same", message)
		return nil
	}

	rollback := strings.TrimSpace(refresh.RollbackState)
	if rollback == "" {
		rollback = "NOT_APPLIED"
	}
	if message == "" {
		message = "Проверка свежего endpoint после обновления подписки не завершена."
	}
	result := "uncertain"
	if refresh.Mutation != "NONE" || rollback == "FAILED/UNKNOWN" {
		result = "failed"
	}
	writeAutomationStateV2(result, message, rollback, false)
	appendAutomationHistoryV2(result, message+"; rollback="+rollback)
	if rollback == "FAILED/UNKNOWN" {
		return errors.New("scheduled current endpoint reconcile rollback failed or is unknown")
	}
	return nil
}

func (a *app) runV3GeoData(ctx context.Context) error {
	if _, err := os.Stat(a.cfg.XKeenPath); err != nil {
		v3Mark("geodata", "failed", "XKeen updater недоступен.")
		return errors.New("xkeen updater is unavailable")
	}
	cmd := exec.CommandContext(ctx, a.cfg.XKeenPath, "-ug")
	if out, err := cmd.CombinedOutput(); err != nil {
		_ = out
		v3Mark("geodata", "failed", "GeoData / GeoIP не обновлены.")
		return err
	}
	v3Mark("geodata", "success", "GeoData / GeoIP обновлены.")
	return nil
}

func (a *app) runV3FreeNetCheck(ctx context.Context) error {
	if _, err := os.Stat(a.cfg.SelfUpdatePath); err != nil {
		v3Mark("freenet", "failed", "Механизм проверки обновления FreeNet недоступен.")
		return err
	}
	cmd := exec.CommandContext(ctx, a.cfg.SelfUpdatePath, "plan")
	cmd.Env = append(os.Environ(),
		"FREENET_CURRENT_VERSION=v"+version,
		"FREENET_UPDATE_STATE_FILE="+a.cfg.UpdateState,
		"FREENET_UPDATE_LOCK_DIR="+a.cfg.UpdateLock,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		v3Mark("freenet", "failed", "Проверка обновления FreeNet не завершена.")
		return err
	}
	values := parseKVOutput(string(out))
	if strings.EqualFold(values["UPDATE_AVAILABLE"], "yes") {
		latest := sanitizeAutomationReason(values["LATEST_VERSION"])
		v3Mark("freenet", "available", "Доступно обновление FreeNet "+latest+". Установка требует подтверждения.")
	} else {
		v3Mark("freenet", "success", "Установлена актуальная версия FreeNet.")
	}
	return nil
}

func (a *app) createV3Backup(updateLatest bool) (string, error) {
	root := settingsV3BackupRoot()
	if err := os.MkdirAll(root, 0700); err != nil {
		return "", errors.New("cannot prepare backup storage")
	}
	name := "backup-" + time.Now().Format("20060102-150405.000000000")
	dir := filepath.Join(root, name)
	if err := os.Mkdir(dir, 0700); err != nil {
		return "", errors.New("cannot create backup directory")
	}
	if err := v3CaptureBackupSnapshot(dir, a.v3BackupTrackedFiles()); err != nil {
		_ = os.RemoveAll(dir)
		return "", err
	}
	if updateLatest {
		if err := atomicWrite(filepath.Join(root, "latest"), []byte(name+"\n"), 0600); err != nil {
			_ = os.RemoveAll(dir)
			return "", errors.New("cannot commit backup restore reference")
		}
	}
	return dir, nil
}

func (a *app) createAndMarkV3Backup() (string, error) {
	dir, err := a.createV3Backup(true)
	if err != nil {
		v3Mark("backup", "failed", "Не удалось создать резервную копию настроек FreeNet.")
		return "", err
	}
	v3Mark("backup", "success", "Снимок настроек FreeNet создан: "+filepath.Base(dir)+".")
	return dir, nil
}

func (a *app) runV3Backup() error {
	_, err := a.createAndMarkV3Backup()
	return err
}

func (a *app) restoreV3Backup() error {
	_, source, err := latestV3BackupSnapshot()
	if err != nil {
		return err
	}

	rollback, err := a.createV3Backup(false)
	if err != nil {
		return errors.New("cannot create pre-restore snapshot")
	}
	files := a.v3BackupTrackedFiles()

	restoreErr := v3ApplyBackupSnapshotForRestore(source, files, false)
	if restoreErr == nil {
		restoreErr = v3VerifyBackupSnapshotForRestore(source, files, false)
	}
	if restoreErr == nil {
		return nil
	}

	if rollbackErr := v3RollbackBackupSnapshot(rollback, files); rollbackErr != nil {
		return errors.New("backup restore failed; rollback failed or is unknown")
	}
	return errors.New("backup restore failed; previous files restored and verified")
}

func (a *app) handleSettingsV3Action(w http.ResponseWriter, r *http.Request) {
	var req settingsV3ActionRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, settingsV3ActionResponse{Success: false, Error: "invalid action request"})
		return
	}
	if a.mutationBlockedBySelfUpdate(w) && req.Action != "freenet_check" {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 4*time.Minute)
	defer cancel()
	var err error
	var message string
	var backupInfo *settingsV3BackupInfo
	switch strings.TrimSpace(req.Action) {
	case "subscription_check":
		result, subscriptionErr := a.runV3SubscriptionResult(ctx)
		visible := selectableSubscriptionProfiles(result.Profiles)
		response := settingsV3ActionResponse{
			Success: subscriptionErr == nil, Message: "Подписка проверена и каталог Extra-профилей обновлён.",
			Profiles: visible, ProfilesAvailable: len(visible), ProfilesStale: result.Stale, ProfilesUpdatedAt: result.UpdatedAt,
		}
		if subscriptionErr != nil {
			response.Error = sanitizeAutomationReason(subscriptionErr.Error())
			writeJSON(w, http.StatusBadGateway, response)
			return
		}
		writeJSON(w, http.StatusOK, response)
		return
	case "geodata_update":
		err = a.runV3GeoData(ctx); message = "GeoData / GeoIP обновлены."
	case "freenet_check":
		err = a.runV3FreeNetCheck(ctx); message = "Проверка версии FreeNet завершена."
	case "backup_create":
		var dir string
		dir, err = a.createAndMarkV3Backup()
		if err == nil {
			info := a.v3BackupInfo()
			info.Latest = filepath.Base(dir)
			info.LatestPath = dir
			backupInfo = &info
			message = "Снимок настроек FreeNet создан."
		}
	case "backup_restore":
		err = a.restoreV3Backup()
		if err == nil {
			info := a.v3BackupInfo()
			backupInfo = &info
			message = "Последний снимок восстановлен и проверен."
		}
	default:
		writeJSON(w, http.StatusBadRequest, settingsV3ActionResponse{Success: false, Error: "unsupported action"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusBadGateway, settingsV3ActionResponse{Success: false, Error: sanitizeAutomationReason(err.Error())})
		return
	}
	writeJSON(w, http.StatusOK, settingsV3ActionResponse{Success: true, Message: message, BackupInfo: backupInfo})
}

func recordSettingsV3Health(result automationHealthResult) {
	now := time.Now().UTC().Format(time.RFC3339)
	_ = v3WriteState(map[string]string{"HEALTH_LAST": now, "HEALTH_RESULT": result.State, "HEALTH_MESSAGE": result.Reason})
	resultCode := result.State
	if result.State == automationHealthHealthy {
		resultCode = "success"
	}
	v3AppendEvent("auto_vpn", resultCode, result.Reason)
}

func runSettingsV3CLI(command string) int {
	a := &app{cfg: automationCLIConfig(), sem: make(chan struct{}, 1)}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	var err error
	switch command {
	case "settings-v3-endpoint-refresh":
		err = a.runV3ScheduledEndpointRefresh(ctx)
	case "settings-v3-subscription":
		err = a.runV3ScheduledSubscription(ctx)
	case "settings-v3-geodata":
		err = a.runV3GeoData(ctx)
	case "settings-v3-freenet-check":
		err = a.runV3FreeNetCheck(ctx)
	case "settings-v3-backup":
		err = a.runV3Backup()
	default:
		return -1
	}
	if err != nil {
		fmt.Printf("RESULT=failed\nREASON=%s\n", sanitizeAutomationReason(err.Error()))
		return 1
	}
	fmt.Println("RESULT=success")
	return 0
}

func init() {
	if len(os.Args) < 2 {
		return
	}
	if rc := runSettingsV3CLI(os.Args[1]); rc >= 0 {
		os.Exit(rc)
	}
}
