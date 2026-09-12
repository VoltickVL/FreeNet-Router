package main

import (
	"os"
	"strings"
	"testing"
)

func TestManualBestAutoApplyIsIndependentFromScheduler(t *testing.T) {
	source, err := os.ReadFile("automation_v2.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	if !strings.Contains(text, `if !manual && !settings.Enabled {`) {
		t.Fatal("scheduled AUTO VPN runs must still require the scheduler to be enabled")
	}
	if !strings.Contains(text, `if !settings.AutoApply {`) {
		t.Fatal("manual Best AUTO VPN must honor the independent auto-apply policy")
	}
	if strings.Contains(text, `!settings.AutoApply || (!settings.Enabled && manual)`) {
		t.Fatal("scheduler disabled must not suppress a manual confirmed AUTO VPN apply")
	}
}

func TestSettingsV3ManualCheckShowsLiveElapsedActivity(t *testing.T) {
	asset, err := automationWebFS.ReadFile("web/settings-v3.js")
	if err != nil {
		t.Fatal(err)
	}
	text := string(asset)
	for _, required := range []string{
		`const btn = q('#fn3Check'); const started = Date.now();`,
		`Проверяем… ${sec} с`,
		`/api/automation/check`,
		`await new Promise(r => setTimeout(r, 1200));`,
		`state.checking = false`,
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("Settings v3 manual AUTO VPN progress contract missing %q", required)
		}
	}
	for _, forbidden := range []string{
		`#fnCheckNow`,
		`AUTO VPN: идёт проверка`,
		`повторно запускать её не нужно`,
		`setInterval(`,
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("Settings v3 manual AUTO VPN progress retained legacy behavior: %q", forbidden)
		}
	}
}

func TestSettingsV3ManualCheckReloadsCanonicalCurrentQuality(t *testing.T) {
	asset, err := automationWebFS.ReadFile("web/settings-v3.js")
	if err != nil {
		t.Fatal(err)
	}
	text := string(asset)
	for _, required := range []string{
		`await load();`,
		`fetchJSON('/api/settings-v3', {cache:'no-store'})`,
		`current_quality_known`,
		`current_latency_ms`,
		`current_download_mbps`,
		`current_jitter_ms`,
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("Settings v3 post-check quality refresh contract missing %q", required)
		}
	}
}
