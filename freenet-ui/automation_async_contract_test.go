package main

import (
	"os"
	"strings"
	"testing"
)

func TestAutomationManualCheckUsesAsyncTransport(t *testing.T) {
	api, err := os.ReadFile("automation_api.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(api)
	for _, required := range []string{
		`GET /api/automation/check`,
		`a.handleAutomationCheckStart(w, r)`,
		`web/automation-async.js`,
	} {
		if !strings.Contains(source, required) {
			t.Fatalf("async AUTO VPN transport missing %q", required)
		}
	}
	if strings.Contains(source, `runAutomationBestCycle(r.Context(), true)`) {
		t.Fatal("manual Best AUTO VPN check must not stay attached to the HTTP request context")
	}
}

func TestLegacyAutomationAsyncBrowserRendererIsRetired(t *testing.T) {
	asset, err := automationWebFS.ReadFile("web/automation-async.js")
	if err != nil {
		t.Fatal(err)
	}
	source := string(asset)
	if !strings.Contains(source, `__freenetLegacyAutomationAsyncRetired = true`) {
		t.Fatal("legacy automation async asset must remain only as an explicit retired compatibility boundary")
	}
	for _, forbidden := range []string{
		`#fnCheckNow`,
		`/api/automation/check`,
		`stopImmediatePropagation`,
		`pollCheck`,
		`bridgeSettingsNavigation`,
	} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("legacy Settings async behavior survived retirement: %q", forbidden)
		}
	}
}

func TestSettingsPresentationRefreshesLiveRuntimeOnActivation(t *testing.T) {
	asset, err := automationWebFS.ReadFile("web/automation-async.js")
	if err != nil {
		t.Fatal(err)
	}
	source := string(asset)
	for _, required := range []string{
		`refreshSettingsRuntime`,
		`fetch('/api/settings-v3', {cache:'no-store'})`,
		`fetch('/api/status', {cache:'no-store'})`,
		`if (active && !settingsWasActive) refreshSettingsRuntime()`,
		`replace(/^[A-Za-z]{2}\s+/, '')`,
		`year:'numeric'`,
		`Системное обслуживание`,
		`Создать внутренний снимок настроек на роутере`,
	} {
		if !strings.Contains(source, required) {
			t.Fatalf("Settings runtime acceptance polish missing %q", required)
		}
	}
	if strings.Contains(source, `location.reload`) {
		t.Fatal("Settings runtime refresh must not require a full page reload")
	}
}

func TestAutomationCheckCoordinatorDeduplicatesSameTarget(t *testing.T) {
	var coordinator operationCoordinator
	first, leader, conflict := coordinator.begin("auto-vpn-check", "best")
	if first == nil || !leader || conflict != nil {
		t.Fatalf("first operation should become leader: leader=%v conflict=%v", leader, conflict)
	}
	second, secondLeader, secondConflict := coordinator.begin("auto-vpn-check", "best")
	if second != first || secondLeader || secondConflict != nil {
		t.Fatalf("same manual check should join existing operation without starting a duplicate")
	}
	_, otherLeader, otherConflict := coordinator.begin("auto-vpn-check", "endpoint")
	if otherLeader || otherConflict == nil {
		t.Fatal("different AUTO VPN target must conflict while a check is active")
	}
}
