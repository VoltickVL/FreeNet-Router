package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRollbackGuardJournalThrottlesIdenticalPendingWithoutClearingSTOP(t *testing.T) {
	automationRollbackGuardJournal.Lock()
	previousTime := automationRollbackGuardJournal.lastTime
	previousKind := automationRollbackGuardJournal.lastKind
	automationRollbackGuardJournal.lastTime = time.Time{}
	automationRollbackGuardJournal.lastKind = ""
	automationRollbackGuardJournal.Unlock()
	t.Cleanup(func() {
		automationRollbackGuardJournal.Lock()
		automationRollbackGuardJournal.lastTime = previousTime
		automationRollbackGuardJournal.lastKind = previousKind
		automationRollbackGuardJournal.Unlock()
	})

	now := time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC)
	if !automationRollbackGuardShouldJournal("blocked_pending", now) {
		t.Fatal("first pending guard was suppressed")
	}
	if automationRollbackGuardShouldJournal("blocked_pending", now.Add(30*time.Second)) {
		t.Fatal("second 30-second watchdog pass would flood the incident journal")
	}
	if !automationRollbackGuardShouldJournal("blocked", now.Add(time.Minute)) {
		t.Fatal("different guard state was not reported")
	}
	if !automationRollbackGuardShouldJournal("blocked_pending", now.Add(2*time.Minute)) {
		t.Fatal("transition back to pending was suppressed")
	}
	if automationRollbackGuardShouldJournal("blocked_pending", now.Add(14*time.Minute)) {
		t.Fatal("unchanged pending repeated before journal interval")
	}
	if !automationRollbackGuardShouldJournal("blocked_pending", now.Add(17*time.Minute)) {
		t.Fatal("pending heartbeat not reported after fifteen minutes")
	}

	root := t.TempDir()
	checkpoint := filepath.Join(root, "vpn-transaction.pending")
	history := filepath.Join(root, "auto.history")
	t.Setenv("FREENET_VPN_TRANSACTION_DIR", checkpoint)
	t.Setenv("FREENET_AUTOMATION_STATE", filepath.Join(root, "auto.state"))
	t.Setenv("FREENET_AUTOMATION_HISTORY", history)
	if err := os.Mkdir(checkpoint, 0700); err != nil {
		t.Fatal(err)
	}
	automationRollbackGuardJournal.Lock()
	automationRollbackGuardJournal.lastTime = time.Time{}
	automationRollbackGuardJournal.lastKind = ""
	automationRollbackGuardJournal.Unlock()
	for i := 0; i < 2; i++ {
		observed, guarded := automationRollbackGuardResult(nil, automationHealthProbe{State: automationHealthFailed})
		if !guarded || observed.State != automationHealthUncertain || !automationMutationBlockedState() {
			t.Fatalf("pending checkpoint escaped STOP: %+v guarded=%v", observed, guarded)
		}
	}
	logged, err := os.ReadFile(history)
	if err != nil {
		t.Fatal(err)
	}
	if count := strings.Count(string(logged), "rollback_guard:blocked_pending"); count != 1 {
		t.Fatalf("watchdog duplicates were not throttled: %d: %s", count, logged)
	}
	if _, err := os.Lstat(checkpoint); err != nil {
		t.Fatal("throttled guard must never clear checkpoint")
	}
}
