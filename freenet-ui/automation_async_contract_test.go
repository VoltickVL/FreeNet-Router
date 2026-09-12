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

func TestAutomationAsyncBrowserAssetPollsAndRejectsHTML(t *testing.T) {
	asset, err := automationWebFS.ReadFile("web/automation-async.js")
	if err != nil {
		t.Fatal(err)
	}
	source := string(asset)
	for _, required := range []string{
		`#fnCheckNow`,
		`/api/automation/check`,
		`response.headers.get('content-type')`,
		`application/json`,
		`event.stopImmediatePropagation()`,
		`setTimeout`,
	} {
		if !strings.Contains(source, required) {
			t.Fatalf("async browser contract missing %q", required)
		}
	}
	if strings.Contains(source, `Unexpected token`) {
		t.Fatal("raw JSON parser errors must not be user-facing")
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
