package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSettingsV3HumanIntervals(t *testing.T) {
	cases := map[string]string{
		"30m": "*/30 * * * *",
		"1h": "0 * * * *",
		"3h": "0 */3 * * *",
		"6h": "0 */6 * * *",
		"12h": "0 */12 * * *",
		"24h": "17 4 * * *",
	}
	for interval, want := range cases {
		got, ok := v3IntervalCron(interval)
		if !ok || got != want {
			t.Fatalf("interval %s cron=%q ok=%v want=%q", interval, got, ok, want)
		}
	}
}

func TestSettingsV3ManagedSchedulerUsesHealthWatchdogNotPeriodicBest(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "freenet.conf")
	if err := os.WriteFile(configPath, []byte("UI_PORT=1001\n"), 0600); err != nil {
		t.Fatal(err)
	}
	a := &app{cfg: config{ConfigPath: configPath}}
	values := map[string]string{
		"AUTO_VPN_V1": "yes",
		"AUTO_SUBSCRIPTION_REFRESH_ENABLED": "yes", "AUTO_SUBSCRIPTION_REFRESH_INTERVAL": "6h",
		"AUTO_GEODATA_ENABLED": "yes", "AUTO_GEODATA_INTERVAL": "3h",
		"AUTO_FREENET_CHECK_ENABLED": "yes", "AUTO_FREENET_CHECK_INTERVAL": "12h",
		"AUTO_BACKUP_ENABLED": "yes", "AUTO_BACKUP_INTERVAL": "24h",
	}
	got, err := buildManagedAutomationCronV3(a, []byte("*/5 * * * * /opt/bin/vpn failover\n"), values)
	if err != nil {
		t.Fatal(err)
	}
	text := string(got)
	checks := []string{
		"*/5 * * * * ", " automation-health-watch",
		"0 */6 * * * ", " settings-v3-subscription",
		"0 */3 * * * ", " settings-v3-geodata",
		"0 */12 * * * ", " settings-v3-freenet-check",
		"17 4 * * * ", " settings-v3-backup",
	}
	for _, check := range checks {
		if !strings.Contains(text, check) {
			t.Fatalf("managed v3 cron missing %q:\n%s", check, text)
		}
	}
	if strings.Contains(text, "automation-best-run") || strings.Contains(text, "/opt/bin/vpn failover") {
		t.Fatalf("Settings v3 must not schedule periodic Best optimization or legacy failover:\n%s", text)
	}
}

func TestSettingsV3MagicRecoveryIsEndpointFirst(t *testing.T) {
	data, err := os.ReadFile("automation_health.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	start := strings.Index(text, "func (a *app) runAutomationHealthWatch")
	end := strings.Index(text[start:], "func init()")
	if start < 0 || end < 0 {
		t.Fatal("health-watch implementation not found")
	}
	segment := text[start : start+end]
	endpoint := strings.Index(segment, "runAutomationEndpointEmergency")
	replacement := strings.Index(segment, "runAutomationBestEmergencyCycle")
	if endpoint < 0 || replacement < 0 || endpoint >= replacement {
		t.Fatal("AUTO VPN outage recovery must try the current VPN endpoint before searching a replacement")
	}
	if strings.Contains(segment, "settings.Interval == \"manual\"") {
		t.Fatal("5-minute health watchdog must be independent from the removed user-facing deep interval")
	}
}

func TestSettingsV3AcceptedUserSurface(t *testing.T) {
	data, err := os.ReadFile("web/settings-v3.js")
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{
		"Настройки / Система",
		"FreeNet каждые 5 минут проверяет доступность текущего VPN",
		"Режим работы",
		"Только текущий VPN",
		"Полный AUTO VPN",
		"Плановое обновление endpoint",
		"Текущая страна",
		"Ближайшие страны",
		"Выбранные страны",
		"Резервное копирование",
		"Создать снимок",
		"Восстановить последний",
		"GeoData / GeoIP",
		"Системное обслуживание",
		"Сохранить изменения",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("Settings v3 accepted surface missing %q", want)
		}
	}
	for _, forbidden := range []string{"Интернет-провайдер", "Только endpoint", "Лучший VPN автоматически", "hysteresis ≥10%", "6-часовой cooldown"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("Settings v3 user surface still contains obsolete wording %q", forbidden)
		}
	}
}

func TestSettingsV3FreeNetAutomationIsCheckOnly(t *testing.T) {
	data, err := os.ReadFile("settings_v3.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	start := strings.Index(text, "func (a *app) runV3FreeNetCheck")
	if start < 0 {
		t.Fatal("FreeNet update check implementation not found")
	}
	rest := text[start+len("func (a *app) runV3FreeNetCheck"):]
	next := strings.Index(rest, "\nfunc ")
	if next < 0 {
		t.Fatal("FreeNet update check implementation end not found")
	}
	segment := text[start : start+len("func (a *app) runV3FreeNetCheck")+next]
	if !strings.Contains(segment, `a.cfg.SelfUpdatePath, "plan"`) {
		t.Fatal("automatic FreeNet update must use read-only plan mode")
	}
	if strings.Contains(segment, `"apply"`) {
		t.Fatal("automatic FreeNet update must never install without explicit confirmation")
	}
}


func TestSettingsV3SubscriptionActionReturnsSelectableSafeCatalog(t *testing.T) {
	resetSubscriptionRefreshGroupForTest()
	t.Setenv("FREENET_SUBSCRIPTION_PROFILES_CACHE", filepath.Join(t.TempDir(), "profiles.json"))
	t.Setenv("FREENET_SETTINGS_V3_STATE", filepath.Join(t.TempDir(), "settings.state"))
	t.Setenv("FREENET_SETTINGS_V3_HISTORY", filepath.Join(t.TempDir(), "settings.history"))
	oldDiscovery := subscriptionProfileDiscovery
	t.Cleanup(func() {
		subscriptionProfileDiscovery = oldDiscovery
		resetSubscriptionRefreshGroupForTest()
	})
	subscriptionProfileDiscovery = func(_ *app, _ context.Context) ([]subscriptionProfile, error) {
		return []subscriptionProfile{
			{ID: "0123456789abcdef", Name: "DE Frankfurt, Germany, Extra", CountryCode: "de", Address: "203.0.113.10", Port: 443},
			{ID: "fedcba9876543210", Name: "RU Moscow, Russia, Extra", CountryCode: "ru", Address: "203.0.113.20", Port: 443},
		}, nil
	}
	a := &app{cfg: config{UpdateLock: filepath.Join(t.TempDir(), "no-update-lock")}}
	req := httptest.NewRequest(http.MethodPost, "/api/settings-v3/action", strings.NewReader(`{"action":"subscription_check"}`))
	rec := httptest.NewRecorder()
	a.handleSettingsV3Action(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var response settingsV3ActionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if !response.Success || response.ProfilesAvailable != 1 || len(response.Profiles) != 1 || response.Profiles[0].CountryCode != "de" {
		t.Fatalf("unexpected subscription action response: %+v", response)
	}
	for _, forbidden := range []string{"vless://", "uuid", "pbk=", "sid=", "https://"} {
		if strings.Contains(strings.ToLower(rec.Body.String()), forbidden) {
			t.Fatalf("subscription action leaked forbidden material %q: %s", forbidden, rec.Body.String())
		}
	}
}


func TestSettingsV3EndpointOnlySchedulerAddsPeriodicRefresh(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "freenet.conf")
	if err := os.WriteFile(configPath, []byte("UI_PORT=1001\n"), 0600); err != nil {
		t.Fatal(err)
	}
	a := &app{cfg: config{ConfigPath: configPath}}
	values := map[string]string{
		"AUTO_VPN_V1": "yes",
		"AUTO_VPN_MODE": automationModeEndpoint,
		"AUTO_VPN_V1_INTERVAL": "30m",
		"AUTO_SUBSCRIPTION_REFRESH_ENABLED": "no",
		"AUTO_GEODATA_ENABLED": "no",
		"AUTO_FREENET_CHECK_ENABLED": "no",
		"AUTO_BACKUP_ENABLED": "no",
	}
	got, err := buildManagedAutomationCronV3(a, nil, values)
	if err != nil {
		t.Fatal(err)
	}
	text := string(got)
	if !strings.Contains(text, "*/5 * * * * "+v3ShellQuote(automationUIBinary())+" automation-health-watch") {
		t.Fatalf("endpoint-only mode lost the 5-minute health watchdog:\n%s", text)
	}
	if !strings.Contains(text, "*/30 * * * * "+v3ShellQuote(automationUIBinary())+" settings-v3-endpoint-refresh") {
		t.Fatalf("endpoint-only mode did not schedule the selected endpoint interval:\n%s", text)
	}
	if strings.Contains(text, "automation-best-run") {
		t.Fatalf("endpoint-only mode must never schedule Best optimization:\n%s", text)
	}
}

func TestSettingsV3FullModeDoesNotScheduleEndpointRefresh(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "freenet.conf")
	if err := os.WriteFile(configPath, []byte("UI_PORT=1001\n"), 0600); err != nil {
		t.Fatal(err)
	}
	a := &app{cfg: config{ConfigPath: configPath}}
	values := map[string]string{
		"AUTO_VPN_V1": "yes",
		"AUTO_VPN_MODE": automationModeBest,
		"AUTO_VPN_V1_INTERVAL": "1h",
		"AUTO_SUBSCRIPTION_REFRESH_ENABLED": "no",
		"AUTO_GEODATA_ENABLED": "no",
		"AUTO_FREENET_CHECK_ENABLED": "no",
		"AUTO_BACKUP_ENABLED": "no",
	}
	got, err := buildManagedAutomationCronV3(a, nil, values)
	if err != nil {
		t.Fatal(err)
	}
	text := string(got)
	if !strings.Contains(text, "automation-health-watch") {
		t.Fatalf("full AUTO VPN lost the health watchdog:\n%s", text)
	}
	if strings.Contains(text, "settings-v3-endpoint-refresh") {
		t.Fatalf("full AUTO VPN must not schedule the periodic endpoint-only job:\n%s", text)
	}
}

func TestSettingsV3SaveSwitchesEndpointAndFullModesTransactionally(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "freenet.conf")
	cronState := filepath.Join(dir, "crontab.state")
	cronCount := filepath.Join(dir, "crontab.count")
	helper := filepath.Join(dir, "auto_vpn.sh")
	if err := os.WriteFile(configPath, []byte(strings.Join([]string{
		"AUTO_VPN_V1=yes",
		"AUTO_VPN_MODE=best",
		"AUTO_VPN_V1_INTERVAL=manual",
		"AUTO_XKEEN_GEODATA=no",
	}, "\n")+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cronState, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(helper, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}
	writeSettingsV3FakeCrontab(t, cronState, cronCount)
	t.Setenv("FREENET_AUTO_VPN_HELPER", helper)

	enabled, disabled := true, false
	req := settingsV3SaveRequest{
		Action: "save", AutoVPNEnabled: &enabled, AutoVPNMode: automationModeEndpoint, AutoVPNEndpointInterval: "30m",
		CountryScope: automationCountryRegion, Countries: []string{"de"},
		SubscriptionEnabled: &disabled, GeoDataEnabled: &disabled, FreeNetEnabled: &disabled, BackupEnabled: &disabled,
	}
	a := &app{cfg: config{ConfigPath: configPath}}
	if err := a.saveSettingsV3(req); err != nil {
		t.Fatal(err)
	}
	configData, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	configText := string(configData)
	for _, want := range []string{
		"AUTO_VPN_MODE=endpoint",
		"AUTO_VPN_V1_INTERVAL=30m",
		"AUTO_ENDPOINT_UPDATE=yes",
		"AUTO_ENDPOINT_CRON=*/30 * * * *",
	} {
		if !strings.Contains(configText, want) {
			t.Fatalf("endpoint-only save missing %q:\n%s", want, configText)
		}
	}
	cronData, err := os.ReadFile(cronState)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(cronData), "settings-v3-endpoint-refresh") {
		t.Fatalf("endpoint-only scheduler missing after save:\n%s", cronData)
	}

	req.AutoVPNMode = automationModeBest
	req.AutoVPNEndpointInterval = "3h"
	if err := a.saveSettingsV3(req); err != nil {
		t.Fatal(err)
	}
	configData, err = os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	configText = string(configData)
	for _, want := range []string{"AUTO_VPN_MODE=best", "AUTO_VPN_V1_INTERVAL=manual", "AUTO_ENDPOINT_UPDATE=no"} {
		if !strings.Contains(configText, want) {
			t.Fatalf("full-mode save missing %q:\n%s", want, configText)
		}
	}
	cronData, err = os.ReadFile(cronState)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(cronData), "settings-v3-endpoint-refresh") {
		t.Fatalf("full AUTO VPN retained endpoint-only scheduler:\n%s", cronData)
	}
}

func TestSettingsV3ScheduledEndpointRefreshRespectsModeAndHealthLock(t *testing.T) {
	old := settingsV3ScheduledEndpointRefresh
	t.Cleanup(func() { settingsV3ScheduledEndpointRefresh = old })
	dir := t.TempDir()
	configPath := filepath.Join(dir, "freenet.conf")
	lockPath := filepath.Join(dir, "health.lock")
	t.Setenv("FREENET_AUTO_HEALTH_LOCK", lockPath)
	writeConfig := func(mode string) {
		if err := os.WriteFile(configPath, []byte(strings.Join([]string{
			"AUTO_VPN_V1=yes",
			"AUTO_VPN_MODE="+mode,
			"AUTO_VPN_V1_INTERVAL=1h",
			"AUTO_VPN_AUTO_APPLY=yes",
		}, "\n")+"\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	calls := 0
	settingsV3ScheduledEndpointRefresh = func(_ *app, _ context.Context) error {
		calls++
		return nil
	}
	a := &app{cfg: config{ConfigPath: configPath}}

	writeConfig(automationModeEndpoint)
	if err := a.runV3ScheduledEndpointRefresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("endpoint-only scheduled refresh calls=%d want=1", calls)
	}

	writeConfig(automationModeBest)
	if err := a.runV3ScheduledEndpointRefresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("full mode must not run endpoint-only scheduler; calls=%d", calls)
	}

	writeConfig(automationModeEndpoint)
	if err := os.Mkdir(lockPath, 0700); err != nil {
		t.Fatal(err)
	}
	if err := a.runV3ScheduledEndpointRefresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("held health lock must skip endpoint refresh before mutation; calls=%d", calls)
	}
}

func TestSettingsV3EndpointOnlyHealthPathBlocksBestFallback(t *testing.T) {
	data, err := os.ReadFile("automation_health.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	start := strings.Index(text, "func (a *app) runAutomationHealthWatch")
	end := strings.Index(text[start:], "func init()")
	if start < 0 || end < 0 {
		t.Fatal("health-watch implementation not found")
	}
	segment := text[start : start+end]
	gate := strings.Index(segment, "if settings.Mode == automationModeEndpoint")
	best := strings.Index(segment, "runAutomationBestEmergencyCycle")
	if gate < 0 || best < 0 || gate >= best {
		t.Fatal("endpoint-only mode must STOP before the Best replacement path")
	}
	if !strings.Contains(segment[gate:best], "candidate_selection\", \"blocked") {
		t.Fatal("endpoint-only STOP must be explicit in the AUTO VPN recovery journal")
	}
}

func TestSettingsV3CoreRendersTwoAutoVPNModes(t *testing.T) {
	data, err := os.ReadFile("web/settings-v3-core.js")
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{
		`name="fn3Mode" value="endpoint"`,
		`name="fn3Mode" value="best"`,
		"Только текущий VPN",
		"Полный AUTO VPN",
		"fn3EndpointInterval",
		"fn3ReplacementSettings",
		"auto_vpn_mode:form.mode",
		"auto_vpn_endpoint_interval:form.endpointInterval",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("Settings v3 core missing two-mode contract %q", want)
		}
	}
}
