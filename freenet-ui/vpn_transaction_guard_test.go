package main

import (
    "context"
    "net/http"
    "net/http/httptest"
    "os"
    "path/filepath"
    "strings"
    "testing"
)

func TestPendingVPNTransactionBlocksGoMutations(t *testing.T) {
    root := t.TempDir()
    pending := filepath.Join(root, "vpn-transaction.pending")
    t.Setenv("FREENET_VPN_TRANSACTION_DIR", pending)
    t.Setenv("FREENET_AUTOMATION_STATE", filepath.Join(root, "auto.state"))
    if automationMutationBlockedState() { t.Fatal("fresh empty runtime falsely blocked") }
    if err := os.Mkdir(pending, 0700); err != nil { t.Fatal(err) }
    if !vpnTransactionPending() || !automationMutationBlockedState() {
        t.Fatal("persistent checkpoint was ignored")
    }
    a := &app{sem: make(chan struct{}, 1)}
    if !a.xrayRuntimeMutationBusy() { t.Fatal("watchdog bypassed PENDING") }
    for _, action := range []string{"start", "stop", "restart"} {
        if _, err := a.controlXrayService(context.Background(), action); err == nil {
            t.Fatalf("Xray action %s bypassed PENDING", action)
        }
    }
    w := httptest.NewRecorder()
    if !a.mutationBlockedBySelfUpdate(w) || w.Code != http.StatusConflict {
        t.Fatalf("VPN API was not blocked: HTTP %d", w.Code)
    }
    if strings.Contains(w.Body.String(), root) {
        t.Fatal("private checkpoint path disclosed")
    }
    if err := os.Remove(pending); err != nil { t.Fatal(err) }
    if automationMutationBlockedState() { t.Fatal("verified marker removal stayed blocked") }
}

func TestLegacyUnknownBlocksWithoutCheckpoint(t *testing.T) {
    root := t.TempDir()
    t.Setenv("FREENET_VPN_TRANSACTION_DIR", filepath.Join(root, "missing"))
    state := filepath.Join(root, "auto.state")
    t.Setenv("FREENET_AUTOMATION_STATE", state)
    if err := os.WriteFile(state, []byte("MUTATION_BLOCKED=yes\n"), 0600); err != nil { t.Fatal(err) }
    if !automationMutationBlockedState() {
        t.Fatal("legacy UNKNOWN marker was not respected")
    }
}

func TestPendingCheckpointSurvivesHealthyOrFailedVPNProbe(t *testing.T) {
	dir := t.TempDir()
	pending := filepath.Join(dir, "vpn-transaction.pending")
	t.Setenv("FREENET_VPN_TRANSACTION_DIR", pending)
	t.Setenv("FREENET_AUTOMATION_STATE", filepath.Join(dir, "automation.state"))
	if err := os.Mkdir(pending, 0700); err != nil {
		t.Fatal(err)
	}
	for _, probe := range []automationHealthProbe{
		{State: automationHealthHealthy, Reason: "HTTPS passes"},
		{State: automationHealthFailed, Reason: "HTTPS fails"},
	} {
		got, handled := automationRollbackGuardResult(nil, probe)
		if !handled || got.State != automationHealthUncertain {
			t.Fatalf("checkpoint must STOP even if single VPN probe is %q: %+v handled=%v", probe.State, got, handled)
		}
		if !automationMutationBlockedState() {
			t.Fatal("read-only probe improperly cleared persistent checkpoint")
		}
	}
}

func TestPendingCheckpointBlocksInternalAutoApply(t *testing.T) {
	dir := t.TempDir()
	pending := filepath.Join(dir, "vpn-transaction.pending")
	t.Setenv("FREENET_VPN_TRANSACTION_DIR", pending)
	t.Setenv("FREENET_AUTOMATION_STATE", filepath.Join(dir, "automation.state"))
	if err := os.Mkdir(pending, 0700); err != nil {
		t.Fatal(err)
	}
	a := &app{sem: make(chan struct{}, 1)}
	status, result := a.executeProviderProfileApply(networkApplyRequest{Operation: "provider", ProfileID: "0000000000000001"})
	if status != http.StatusConflict || result.Success || result.RollbackState != "FAILED/UNKNOWN" {
		t.Fatalf("internal AUTO apply bypassed durable STOP: HTTP %d result=%+v", status, result)
	}
}
