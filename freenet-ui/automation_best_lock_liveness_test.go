package main

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestAutomationBestLockReclaimsDeadOwnerImmediatelyAfterUpdate(t *testing.T) {
	lock := filepath.Join(t.TempDir(), "best.lock")
	t.Setenv("FREENET_AUTO_BEST_LOCK", lock)
	if err := os.Mkdir(lock, 0700); err != nil {
		t.Fatal(err)
	}
	// The old FreeNet PID is gone after self-update. A fresh lock's age must
	// not postpone recovery of degraded video for another 15 minutes.
	deadPID := 99999999
	if _, err := os.Stat(filepath.Join("/proc", strconv.Itoa(deadPID))); !errors.Is(err, os.ErrNotExist) {
		t.Skip("test PID unexpectedly exists or procfs is unavailable")
	}
	if err := os.WriteFile(filepath.Join(lock, "pid"), []byte(strconv.Itoa(deadPID)+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	release, err := acquireAutomationBestLock()
	if err != nil {
		t.Fatalf("dead process retained an abandoned Best lock: %v", err)
	}
	defer release()
	pidBytes, err := os.ReadFile(filepath.Join(lock, "pid"))
	if err != nil || strings.TrimSpace(string(pidBytes)) != strconv.Itoa(os.Getpid()) {
		t.Fatalf("reclaimed Best lock was not owned by the live FreeNet PID: data=%q err=%v", pidBytes, err)
	}
	if nextRelease, err := acquireAutomationBestLock(); err != errAutomationBusy {
		if nextRelease != nil { nextRelease() }
		t.Fatalf("same live owner lock was stolen: %v", err)
	}
}

func TestAutomationBestLockNeverStealsLiveOwnerBasedOnAge(t *testing.T) {
	lock := filepath.Join(t.TempDir(), "best.lock")
	t.Setenv("FREENET_AUTO_BEST_LOCK", lock)
	if err := os.Mkdir(lock, 0700); err != nil { t.Fatal(err) }
	if err := os.WriteFile(filepath.Join(lock, "pid"), []byte(strconv.Itoa(os.Getpid())+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-18*time.Minute)
	if err := os.Chtimes(lock, old, old); err != nil { t.Fatal(err) }
	if release, err := acquireAutomationBestLock(); err != errAutomationBusy {
		if release != nil { release() }
		t.Fatalf("old but live Best scan lock must stay exclusive: %v", err)
	}
	if _, err := os.Stat(filepath.Join(lock, "pid")); err != nil {
		t.Fatalf("live owner lock was removed: %v", err)
	}
}

func TestAutomationBestLockUnpublishedOwnerUsesBoundedLegacyRecovery(t *testing.T) {
	lock := filepath.Join(t.TempDir(), "best.lock")
	t.Setenv("FREENET_AUTO_BEST_LOCK", lock)
	if err := os.Mkdir(lock, 0700); err != nil { t.Fatal(err) }
	// Never assume a freshly created directory without a PID means dead;
	// its owner could be between mkdir and WriteFile.
	if release, err := acquireAutomationBestLock(); err != errAutomationBusy {
		if release != nil { release() }
		t.Fatalf("fresh incomplete lock wrongly discarded: %v", err)
	}
	old := time.Now().Add(-16*time.Minute)
	if err := os.Chtimes(lock, old, old); err != nil { t.Fatal(err) }
	release, err := acquireAutomationBestLock()
	if err != nil { t.Fatalf("legacy abandoned lock did not expire: %v", err) }
	release()
	if _, err := os.Stat(lock); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("lock was left behind after release: %v", err)
	}
}
