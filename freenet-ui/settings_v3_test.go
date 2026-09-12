package main

import (
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
		"Текущая страна",
		"Ближайшие страны",
		"Выбранные страны",
		"Резервное копирование",
		"Создать копию",
		"Восстановить",
		"GeoData / GeoIP",
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
	end := strings.Index(text[start:], "func v3CopyFile")
	if start < 0 || end < 0 {
		t.Fatal("FreeNet update check implementation not found")
	}
	segment := text[start : start+end]
	if !strings.Contains(segment, `a.cfg.SelfUpdatePath, "plan"`) {
		t.Fatal("automatic FreeNet update must use read-only plan mode")
	}
	if strings.Contains(segment, `"apply"`) {
		t.Fatal("automatic FreeNet update must never install without explicit confirmation")
	}
}
