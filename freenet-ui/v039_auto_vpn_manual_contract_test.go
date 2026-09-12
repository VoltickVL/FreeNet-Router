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

func TestAsyncManualCheckShowsLiveElapsedActivity(t *testing.T) {
	asset, err := automationWebFS.ReadFile("web/automation-async.js")
	if err != nil {
		t.Fatal(err)
	}
	text := string(asset)
	for _, required := range []string{
		`renderProgress(button, startedAt)`,
		`Проверяем… ${elapsed} с`,
		`aria-busy`,
		`/api/automation/check`,
		`повторно запускать её не нужно`,
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("manual AUTO VPN progress contract missing %q", required)
		}
	}
	if strings.Contains(text, `setInterval(`) {
		t.Fatal("progress UX must reuse the existing status polling loop, not add another timer loop")
	}
}
