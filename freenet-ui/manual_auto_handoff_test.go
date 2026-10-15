package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A manual server selection is a current-VPN change, never an AUTO disable.
// Preserve the scheduler's LAST_RUN so the selection algorithm still runs
// when it was due, and preserve the user's mode, policy and auto-apply choice.
func TestManualVPNHandoffPreservesAutoAndScheduledSelection(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "automation.state")
	configPath := filepath.Join(dir, "freenet.conf")
	t.Setenv("FREENET_AUTOMATION_STATE", statePath)
	t.Setenv("FREENET_VPN_TRANSACTION_DIR", filepath.Join(dir, "no-pending-transaction"))
	const config = "AUTO_VPN_V1=yes\nAUTO_VPN_V1_MODE=best\nAUTO_VPN_V1_POLICY=better\nAUTO_VPN_V1_AUTO_APPLY=yes\n"
	if err := os.WriteFile(configPath, []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	const state = "LAST_RUN=2026-10-15T02:00:00Z\nLAST_RESULT=same\nLAST_REASON=healthy\nROLLBACK_READY=yes\nLAST_SWITCH=2026-10-15T01:00:00Z\nLAST_SWITCH_ORIGIN=auto\nMUTATION_BLOCKED=no\nPOST_UPDATE_ACK=v0.7.4\n"
	if err := os.WriteFile(statePath, []byte(state), 0600); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 15, 3, 0, 0, 0, time.UTC)
	if err := recordAutomationManualSwitch(now); err != nil {
		t.Fatalf("manual-to-AUTO handoff: %v", err)
	}
	after := parseAutomationState(statePath)
	for key, expected := range map[string]string{
		"LAST_RUN": "2026-10-15T02:00:00Z",
		"LAST_RESULT": "same",
		"LAST_REASON": "healthy",
		"ROLLBACK_READY": "yes",
		"LAST_SWITCH": now.Format(time.RFC3339),
		"LAST_SWITCH_ORIGIN": "manual",
		"MUTATION_BLOCKED": "no",
		"POST_UPDATE_ACK": "v0.7.4",
	} {
		if after[key] != expected {
			t.Fatalf("%s changed unexpectedly: actual %q want %q", key, after[key], expected)
		}
	}
	actualConfig, err := os.ReadFile(configPath)
	if err != nil || string(actualConfig) != config {
		t.Fatalf("manual handoff must not change AUTO settings: %q err=%v", actualConfig, err)
	}
	if !readAutomationSettings(configPath).Enabled {
		t.Fatal("manual selection disabled AUTO VPN")
	}
	if !automationOptimizationCooldownActive(statePath, now.Add(4*time.Minute)) {
		t.Fatal("normal optimization can instantly reverse healthy manual choice")
	}
	if automationOptimizationCooldownActive(statePath, now.Add(5*time.Minute)) {
		t.Fatal("manual choice permanently suppresses AUTO Best selection")
	}
	if automationCooldownActive(now, now.Add(5*time.Minute)) == false {
		t.Fatal("regular AUTO-origin switch cooldown unexpectedly changed")
	}
	// Background health status updates must keep the manual switch marker.
	writeAutomationStateV2("same", "VPN remains usable", "yes", false)
	later := parseAutomationState(statePath)
	if later["LAST_SWITCH_ORIGIN"] != "manual" || later["LAST_SWITCH"] != now.Format(time.RFC3339) {
		t.Fatalf("read-only health update discarded manual handoff: %+v", later)
	}
	// After AUTO itself changes the active server, its usual switch-policy
	// cooldown resumes and the manual marker no longer applies.
	writeAutomationStateV2("switched", "AUTO recovered current VPN", "yes", true)
	automatic := parseAutomationState(statePath)
	if automatic["LAST_SWITCH_ORIGIN"] != "auto" {
		t.Fatalf("AUTO selection did not resume control: %+v", automatic)
	}
	if !automationOptimizationCooldownActive(statePath, time.Now().UTC()) {
		t.Fatal("AUTO switch should retain the ordinary anti-flap period")
	}
}

func TestManualVPNHandoffCannotEraseUnknownRollback(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("FREENET_AUTOMATION_STATE", filepath.Join(dir, "automation.state"))
	t.Setenv("FREENET_VPN_TRANSACTION_DIR", filepath.Join(dir, "checkpoint.pending"))
	state := automationStatePath()
	if err := os.WriteFile(state, []byte("LAST_SWITCH=2026-10-15T01:00:00Z\nLAST_SWITCH_ORIGIN=auto\nMUTATION_BLOCKED=yes\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := recordAutomationManualSwitch(time.Date(2026, 10, 15, 2, 0, 0, 0, time.UTC)); err == nil {
		t.Fatal("manual switch may not clear unresolved rollback by recording a handoff")
	}
	if got := parseAutomationState(state); got["LAST_SWITCH_ORIGIN"] != "auto" || got["MUTATION_BLOCKED"] != "yes" {
		t.Fatalf("unsafe manual handoff modified STOP state: %+v", got)
	}
	if err := os.Mkdir(filepath.Join(dir, "checkpoint.pending"), 0700); err != nil {
		t.Fatal(err)
	}
	setAutomationMutationBlocked(false) // legacy state alone does not remove the durable checkpoint.
	if err := recordAutomationManualSwitch(time.Now().UTC()); err == nil {
		t.Fatal("manual handoff may not bypass persistent pending transaction")
	}
	if !automationMutationBlockedState() {
		t.Fatal("persistent rollback checkpoint improperly cleared")
	}
}

func TestManualRecoveryNeverDisablesEmergencyWatch(t *testing.T) {
	health, err := os.ReadFile("automation_health.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(health)
	start := strings.Index(src, "func (a *app) runAutomationHealthWatch(")
	if start < 0 {
		t.Fatal("missing health watch")
	}
	watch := src[start:]
	if strings.Contains(watch, "automationOptimizationCooldownActive(") {
		t.Fatal("critical VPN recovery waits for cosmetic manual switch cooldown")
	}
	if !strings.Contains(watch, "classifyAutomationHealth(initialFailure, wanHealthy, confirm)") {
		t.Fatal("AUTO must continue confirming failures after manual choice")
	}
}
