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
	Enabled      bool     `json:"enabled"`
	CountryScope string   `json:"country_scope"`
	Countries    []string `json:"countries"`
	LastHealth   string   `json:"last_health,omitempty"`
	NextHealth   string   `json:"next_health,omitempty"`
}

type settingsV3Response struct {
	Success      bool               `json:"success"`
	AutoVPN      settingsV3AutoVPN  `json:"auto_vpn"`
	Automation   automationResponse `json:"automation"`
	Subscription settingsV3Schedule `json:"subscription"`
	GeoData      settingsV3Schedule `json:"geodata"`
	FreeNet      settingsV3Schedule `json:"freenet"`
	Backup       settingsV3Schedule `json:"backup"`
	Events       []automationEvent  `json:"events"`
	Error        string             `json:"error,omitempty"`
}

type settingsV3SaveRequest struct {
	Action               string   `json:"action"`
	AutoVPNEnabled       *bool    `json:"auto_vpn_enabled,omitempty"`
	CountryScope         string   `json:"country_scope,omitempty"`
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
	Success bool   `json:"success"`
	Message string `json:"message,omitempty"`
	Error   string `json:"error,omitempty"`
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

func (a *app) settingsV3Snapshot() settingsV3Response {
	auto := a.automationSnapshot()
	scope := normalizeAutomationCountryScope(auto.Settings.CountryScope)
	if scope == "" {
		scope = automationCountryRegion
	}
	lastHealth, nextHealth := v3HealthTimes(auto.Settings.Enabled)
	events := append([]automationEvent{}, auto.Events...)
	events = append(events, readAutomationEvents(settingsV3HistoryPath(), 8)...)
	if len(events) > 12 {
		events = events[:12]
	}
	return settingsV3Response{
		Success: true,
		AutoVPN: settingsV3AutoVPN{Enabled: auto.Settings.Enabled, CountryScope: scope, Countries: auto.Settings.Countries, LastHealth: lastHealth, NextHealth: nextHealth},
		Automation: auto,
		Subscription: v3ScheduleFromConfig(a.cfg.ConfigPath, "AUTO_SUBSCRIPTION_REFRESH", "subscription", false),
		GeoData: v3ScheduleFromConfig(a.cfg.ConfigPath, "AUTO_GEODATA", "geodata", automationConfigValue(a.cfg.ConfigPath, "AUTO_XKEEN_GEODATA", "yes") == "yes"),
		FreeNet: v3ScheduleFromConfig(a.cfg.ConfigPath, "AUTO_FREENET_CHECK", "freenet", false),
		Backup: v3ScheduleFromConfig(a.cfg.ConfigPath, "AUTO_BACKUP", "backup", false),
		Events: events,
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

func (a *app) saveSettingsV3(req settingsV3SaveRequest) error {
	if req.AutoVPNEnabled == nil || req.SubscriptionEnabled == nil || req.GeoDataEnabled == nil || req.FreeNetEnabled == nil || req.BackupEnabled == nil {
		return errors.New("all automation switches are required")
	}
	scope := normalizeAutomationCountryScope(req.CountryScope)
	if scope != automationCountryCurrent && scope != automationCountryRegion && scope != automationCountryAllowlist {
		return errors.New("unsupported replacement geography")
	}
	countries := normalizeAutomationCountries(req.Countries)
	if scope == automationCountryAllowlist && len(countries) == 0 {
		return errors.New("selected countries list is empty")
	}
	values := map[string]string{
		"AUTO_VPN_V1": v3Bool(*req.AutoVPNEnabled),
		"AUTO_VPN_V1_INTERVAL": "manual",
		"AUTO_VPN_MODE": automationModeBest,
		"AUTO_VPN_POLICY": automationPolicyDegraded,
		"AUTO_VPN_COUNTRY_SCOPE": scope,
		"AUTO_VPN_COUNTRIES": strings.Join(countries, ","),
		"AUTO_VPN_AUTO_APPLY": "yes",
		"AUTO_VPN_FAILOVER": "no",
		"AUTO_ENDPOINT_UPDATE": "no",
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

func (a *app) runV3Subscription(ctx context.Context) error {
	path := settingsV3UpdaterPath()
	if _, err := os.Stat(path); err != nil {
		v3Mark("subscription", "failed", "Штатный updater подписки недоступен.")
		return errors.New("subscription updater is unavailable")
	}
	cmd := exec.CommandContext(ctx, path)
	if out, err := cmd.CombinedOutput(); err != nil {
		_ = out
		v3Mark("subscription", "failed", "Не удалось обновить список VPN из подписки.")
		return err
	}
	v3Mark("subscription", "success", "Список VPN из подписки обновлён.")
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

func v3CopyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0700); err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0600)
}

func (a *app) createV3Backup(updateLatest bool) (string, error) {
	root := settingsV3BackupRoot()
	if err := os.MkdirAll(root, 0700); err != nil {
		return "", err
	}
	name := "backup-" + time.Now().Format("20060102-150405")
	dir := filepath.Join(root, name)
	if err := os.Mkdir(dir, 0700); err != nil {
		return "", err
	}
	files := []struct{ src, name string }{
		{a.cfg.ConfigPath, "freenet.conf"},
		{a.cfg.SubPath, "subscription.url"},
		{a.cfg.FilterPath, "profile_filter.regex"},
		{a.cfg.OutPath, "04_outbounds.json"},
	}
	for _, file := range files {
		if err := v3CopyFile(file.src, filepath.Join(dir, file.name)); err != nil {
			_ = os.RemoveAll(dir)
			return "", err
		}
	}
	if updateLatest {
		_ = os.WriteFile(filepath.Join(root, "latest"), []byte(name+"\n"), 0600)
	}
	return dir, nil
}

func (a *app) runV3Backup() error {
	_, err := a.createV3Backup(true)
	if err != nil {
		v3Mark("backup", "failed", "Не удалось создать резервную копию настроек FreeNet.")
		return err
	}
	v3Mark("backup", "success", "Резервная копия настроек FreeNet создана.")
	return nil
}

func v3AtomicRestore(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	tmp := dst + ".restore-new"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}

func (a *app) restoreV3Backup() error {
	root := settingsV3BackupRoot()
	latestRaw, err := os.ReadFile(filepath.Join(root, "latest"))
	if err != nil {
		return errors.New("backup is unavailable")
	}
	name := strings.TrimSpace(string(latestRaw))
	if name == "" || filepath.Base(name) != name || !strings.HasPrefix(name, "backup-") {
		return errors.New("backup reference is invalid")
	}
	source := filepath.Join(root, name)
	info, err := os.Stat(source)
	if err != nil || !info.IsDir() {
		return errors.New("backup directory is unavailable")
	}
	rollback, err := a.createV3Backup(false)
	if err != nil {
		return errors.New("cannot create pre-restore snapshot")
	}
	pairs := []struct{ name, dst string }{
		{"freenet.conf", a.cfg.ConfigPath},
		{"subscription.url", a.cfg.SubPath},
		{"profile_filter.regex", a.cfg.FilterPath},
		{"04_outbounds.json", a.cfg.OutPath},
	}
	for _, pair := range pairs {
		if err := v3AtomicRestore(filepath.Join(source, pair.name), pair.dst); err != nil {
			for _, rb := range pairs {
				_ = v3AtomicRestore(filepath.Join(rollback, rb.name), rb.dst)
			}
			return errors.New("backup restore failed; previous files restored")
		}
	}
	return nil
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
	switch strings.TrimSpace(req.Action) {
	case "subscription_check":
		err = a.runV3Subscription(ctx); message = "Подписка проверена и обновлена."
	case "geodata_update":
		err = a.runV3GeoData(ctx); message = "GeoData / GeoIP обновлены."
	case "freenet_check":
		err = a.runV3FreeNetCheck(ctx); message = "Проверка версии FreeNet завершена."
	case "backup_create":
		err = a.runV3Backup(); message = "Резервная копия создана."
	case "backup_restore":
		err = a.restoreV3Backup(); message = "Последняя резервная копия восстановлена. Перезагрузите страницу и проверьте настройки."
	default:
		writeJSON(w, http.StatusBadRequest, settingsV3ActionResponse{Success: false, Error: "unsupported action"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusBadGateway, settingsV3ActionResponse{Success: false, Error: sanitizeAutomationReason(err.Error())})
		return
	}
	writeJSON(w, http.StatusOK, settingsV3ActionResponse{Success: true, Message: message})
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
	case "settings-v3-subscription":
		err = a.runV3Subscription(ctx)
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
