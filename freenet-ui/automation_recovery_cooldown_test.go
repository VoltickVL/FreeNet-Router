package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestEmergencyCooldownPersistsAndBacksOffAfterRepeatedNoCandidate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cooldown.json")
	t.Setenv("FREENET_AUTO_EMERGENCY_COOLDOWN_STATE", path)
	filter := filepath.Join(dir, "profile.filter")
	if err := os.WriteFile(filter, []byte("AT Vienna Extra\n"), 0600); err != nil {
		t.Fatal(err)
	}
	a := &app{cfg: config{FilterPath: filter}}
	start := time.Date(2026, 10, 15, 1, 30, 0, 0, time.UTC)
	delays := []time.Duration{
		30 * time.Second, time.Minute, 2 * time.Minute,
		4 * time.Minute, 5 * time.Minute, 5 * time.Minute,
	}
	for i, delay := range delays {
		now := start.Add(time.Duration(i) * 2 * time.Minute)
		retryAt, err := recordAutomationEmergencyFailure(a, now)
		if err != nil {
			t.Fatal(err)
		}
		if got := retryAt.Sub(now); got != delay {
			t.Fatalf("failure %d: delay %v want %v", i+1, got, delay)
		}
		until, active, err := automationEmergencyCooldownDue(a, now.Add(time.Second))
		if err != nil || !active || !until.Equal(retryAt) {
			t.Fatalf("failure %d: active=%v until=%v err=%v", i+1, active, until, err)
		}
		if _, active, err := automationEmergencyCooldownDue(a, retryAt); err != nil || active {
			t.Fatalf("failure %d: retry still held at deadline, active=%v err=%v", i+1, active, err)
		}
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("cooldown state is not mode 0600: info=%v err=%v", info, err)
	}
	if err := clearAutomationEmergencyCooldown(); err != nil {
		t.Fatal(err)
	}
	if _, active, err := automationEmergencyCooldownDue(a, start.Add(time.Minute)); err != nil || active {
		t.Fatalf("confirmed healthy reset must clear cooldown, active=%v err=%v", active, err)
	}
}

func TestEmergencyCooldownCannotCarryOverToDifferentProfile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("FREENET_AUTO_EMERGENCY_COOLDOWN_STATE", filepath.Join(dir, "cooldown.json"))
	filter := filepath.Join(dir, "profile.filter")
	a := &app{cfg: config{FilterPath: filter}}
	if err := os.WriteFile(filter, []byte("BE Brussels Extra\n"), 0600); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 15, 2, 0, 0, 0, time.UTC)
	if _, err := recordAutomationEmergencyFailure(a, now); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filter, []byte("IT Milan Extra\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, active, err := automationEmergencyCooldownDue(a, now.Add(time.Second)); err != nil || active {
		t.Fatalf("old profile must not block current VPN: active=%v err=%v", active, err)
	}
	retryAt, err := recordAutomationEmergencyFailure(a, now.Add(5*time.Second))
	if err != nil || retryAt.Sub(now.Add(5*time.Second)) != 30*time.Second {
		t.Fatalf("new profile must start from first failure: retryAt=%v err=%v", retryAt, err)
	}
}

func TestEmergencyCooldownBrokenPersistenceFailsClosed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cooldown.json")
	t.Setenv("FREENET_AUTO_EMERGENCY_COOLDOWN_STATE", path)
	if err := os.WriteFile(path, []byte("{not json"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := automationEmergencyCooldownDue(nil, time.Now()); err == nil {
		t.Fatal("corrupt cooldown state must stop AUTO mutation")
	}
	if _, err := recordAutomationEmergencyFailure(nil, time.Now()); err == nil {
		t.Fatal("corrupt state must not be overwritten")
	}
	if err := clearAutomationEmergencyCooldown(); err != nil {
		t.Fatal(err)
	}
	if _, active, err := automationEmergencyCooldownDue(nil, time.Now()); err != nil || active {
		t.Fatalf("failed to clear corrupt state: active=%v err=%v", active, err)
	}
}

func TestEmergencyCooldownFreshOutageResetsOldFailureCount(t *testing.T) {
	t.Setenv("FREENET_AUTO_EMERGENCY_COOLDOWN_STATE", filepath.Join(t.TempDir(), "cooldown.json"))
	start := time.Date(2026, 10, 15, 3, 0, 0, 0, time.UTC)
	if _, err := recordAutomationEmergencyFailure(nil, start); err != nil {
		t.Fatal(err)
	}
	now := start.Add(time.Hour)
	until, err := recordAutomationEmergencyFailure(nil, now)
	if err != nil || until.Sub(now) != 30*time.Second {
		t.Fatalf("new outage after an hour must restart delay: until=%v err=%v", until, err)
	}
}

func TestHealthyVPNAt220msDoesNotTriggerEmergencyQualityScan(t *testing.T) {
	got := classifyAutomationReachableQuality(220, 4, 4)
	if got.State != automationHealthHealthy || got.QualityDegraded {
		t.Fatalf("4/4 sites at 220ms must remain available: %+v", got)
	}
	if got := classifyAutomationReachableQuality(235, 4, 4); got.State != automationHealthHealthy {
		t.Fatalf("normal 235ms jitter with 4/4 services must remain healthy: %+v", got)
	}
	if got := classifyAutomationReachableQuality(300, 4, 4); got.State != automationHealthUncertain {
		t.Fatalf("quality degradation at 300ms must be observable without recovery: %+v", got)
	}
	if got := classifyAutomationReachableQuality(180, 0, 4); got.State != automationHealthFailed {
		t.Fatalf("real service outage must not be hidden: %+v", got)
	}
}

func TestEmergencyTerminalEventPresentWhenScanReturnsFailedNilError(t *testing.T) {
	data, err := os.ReadFile("automation_health.go")
	if err != nil {
		t.Fatal(err)
	}
	code := string(data)
	start := strings.Index(code, "incident := newAutomationRecoveryIncident")
	end := strings.Index(code, "\nfunc init()")
	if start < 0 || end <= start {
		t.Fatal("emergency recovery state machine not found")
	}
	body := code[start:end]
	for _, expected := range []string{
		"appendAutomationRecoveryIncidentStage(incident, \"incident\", \"uncertain\"",
		"appendAutomationRecoveryIncidentStage(incident, \"incident\", \"stopped\"",
		"appendAutomationRecoveryIncidentStage(incident, \"incident\", \"failed\"",
		"terminal := \"failed\"",
		"incident, \"incident\", terminal",
		"if best.Result == \"failed\" && !best.Mutated && !automationMutationBlockedState()",
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("missing emergency terminal/fencing contract: %q", expected)
		}
	}
}
