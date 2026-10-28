package main

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A concurrent FreeNet self-update or other writer can own the application
// semaphore while AUTO is evaluating an already measured VPN alternative.
// This rejection happens BEFORE any provider action. It must never be
// persisted as an unknown/failed rollback that strands all future AutoVPN.
func TestAutoProviderBusyBeforeMutationDoesNotLatchRollbackUnknown(t *testing.T) {
	root := t.TempDir()
	statePath := filepath.Join(root, "auto.state")
	checkpoint := filepath.Join(root, "vpn-transaction.pending")
	t.Setenv("FREENET_AUTOMATION_STATE", statePath)
	t.Setenv("FREENET_VPN_TRANSACTION_DIR", checkpoint)

	a := &app{sem: make(chan struct{}, 1)}
	a.sem <- struct{}{} // another writer owns the gate
	guardRan := false
	status, response := a.executeProviderProfileApplyWithAdmission(
		networkApplyRequest{Operation: "provider", ProfileID: "test-logical-id"},
		nil,
		&providerApplyAdmission{beforeQuiesce: func(bestServerInternalCandidate) error {
			guardRan = true
			return nil
		}},
	)
	if status != http.StatusConflict || response.Success || response.Applied || guardRan {
		t.Fatalf("busy writer was allowed into provider admission: status=%d response=%+v guard=%v", status, response, guardRan)
	}
	if response.RollbackState != "NOT_APPLIED" || !strings.Contains(response.Error, "another FreeNet operation") {
		t.Fatalf("pre-mutation busy must be an explicit NOT_APPLIED outcome: %+v", response)
	}
	if _, err := os.Lstat(checkpoint); !os.IsNotExist(err) {
		t.Fatalf("busy writer must not create a VPN checkpoint: %v", err)
	}

	// AUTO's existing failure persistence must preserve this exact, proven
	// pre-mutation status without synthesizing a durable rollback incident.
	writeAutomationStateV2("failed", "provider writer busy", response.RollbackState, false)
	if automationMutationBlockedState() {
		t.Fatal("pre-mutation busy incorrectly latched MUTATION_BLOCKED=yes")
	}
	recorded := parseAutomationState(statePath)
	if recorded["ROLLBACK_READY"] != "NOT_APPLIED" || recorded["MUTATION_BLOCKED"] != "no" {
		t.Fatalf("unexpected durable state after a busy writer: %+v", recorded)
	}

	// Genuine UNKNOWN remains an immutable STOP. Do not globally convert
	// unknown rollbacks into NOT_APPLIED to fix the busy case.
	writeAutomationStateV2("failed", "real rollback unknown", "UNKNOWN", false)
	if !automationMutationBlockedState() || parseAutomationState(statePath)["MUTATION_BLOCKED"] != "yes" {
		t.Fatal("actual unknown rollback no longer blocks future mutations")
	}
}
