package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func writeFakeProcCmdline(t *testing.T, root string, pid int, args ...string) {
	t.Helper()
	dir := filepath.Join(root, strconv.Itoa(pid))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	data := []byte(strings.Join(args, "\x00") + "\x00")
	if err := os.WriteFile(filepath.Join(dir, "cmdline"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestUpdateLockStatusMarksOldPreMutationLockStaleWhenNoUpdaterRuns(t *testing.T) {
	dir := t.TempDir()
	proc := filepath.Join(dir, "proc")
	if err := os.MkdirAll(proc, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FREENET_PROC_ROOT", proc)
	lock := filepath.Join(dir, "update.lock")
	if err := os.Mkdir(lock, 0o755); err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(dir, "update.state")
	if err := os.WriteFile(state, []byte("STATE=CHECKING\nTARGET_VERSION=v0.3.82\nROLLBACK_STATE=NOT_NEEDED\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	a := &app{cfg: config{UpdateLock: lock, UpdateState: state, SelfUpdatePath: "/opt/lib/freenet/self_update.sh"}}
	held, stale := a.updateLockStatus()
	if !held || !stale {
		t.Fatalf("old dead pre-mutation lock held=%v stale=%v", held, stale)
	}
}

func TestUpdateLockStatusDetectsOwnerAndOldProcessScan(t *testing.T) {
	dir := t.TempDir()
	proc := filepath.Join(dir, "proc")
	if err := os.MkdirAll(proc, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FREENET_PROC_ROOT", proc)
	lock := filepath.Join(dir, "update.lock")
	if err := os.Mkdir(lock, 0o755); err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(dir, "update.state")
	if err := os.WriteFile(state, []byte("STATE=CHECKING\nROLLBACK_STATE=NOT_NEEDED\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	helper := "/opt/lib/freenet/self_update.sh"
	writeFakeProcCmdline(t, proc, 111, "/bin/sh", helper, "apply", "v0.3.83")
	if err := os.WriteFile(filepath.Join(lock, "owner.pid"), []byte("111\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	a := &app{cfg: config{UpdateLock: lock, UpdateState: state, SelfUpdatePath: helper}}
	held, stale := a.updateLockStatus()
	if !held || stale {
		t.Fatalf("live owner lock held=%v stale=%v", held, stale)
	}

	if err := os.Remove(filepath.Join(lock, "owner.pid")); err != nil {
		t.Fatal(err)
	}
	held, stale = a.updateLockStatus()
	if !held || stale {
		t.Fatalf("old lock must still detect live updater by proc scan held=%v stale=%v", held, stale)
	}
}

func TestUnlockStaleUpdateLockPreservesHardStops(t *testing.T) {
	dir := t.TempDir()
	proc := filepath.Join(dir, "proc")
	if err := os.MkdirAll(proc, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FREENET_PROC_ROOT", proc)
	lock := filepath.Join(dir, "update.lock")
	state := filepath.Join(dir, "update.state")
	helper := "/opt/lib/freenet/self_update.sh"

	if err := os.Mkdir(lock, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(state, []byte("STATE=ROLLBACK_FAILED\nROLLBACK_STATE=FAILED_UNKNOWN\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	a := &app{cfg: config{UpdateLock: lock, UpdateState: state, SelfUpdatePath: helper}}
	if err := a.unlockStaleUpdateLock(); err == nil {
		t.Fatal("ROLLBACK_FAILED lock must never be auto-unlocked")
	}
	if _, err := os.Stat(lock); err != nil {
		t.Fatalf("hard-stop lock was removed: %v", err)
	}
}

func TestUnlockStaleUpdateLockClearsOnlySafeDeadLockAndNormalizesState(t *testing.T) {
	dir := t.TempDir()
	proc := filepath.Join(dir, "proc")
	if err := os.MkdirAll(proc, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FREENET_PROC_ROOT", proc)
	lock := filepath.Join(dir, "update.lock")
	state := filepath.Join(dir, "update.state")
	if err := os.Mkdir(lock, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(state, []byte("STATE=CHECKING\nFROM_VERSION=v0.3.79\nTARGET_VERSION=v0.3.82\nROLLBACK_STATE=NOT_NEEDED\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	a := &app{cfg: config{UpdateLock: lock, UpdateState: state, SelfUpdatePath: "/opt/lib/freenet/self_update.sh"}}
	if err := a.unlockStaleUpdateLock(); err != nil {
		t.Fatalf("safe stale unlock failed: %v", err)
	}
	if _, err := os.Stat(lock); !os.IsNotExist(err) {
		t.Fatalf("stale lock still exists: %v", err)
	}
	got, err := os.ReadFile(state)
	if err != nil {
		t.Fatal(err)
	}
	text := string(got)
	for _, want := range []string{"STATE=IDLE", "FROM_VERSION=v0.3.79", "TARGET_VERSION=v0.3.82", "ROLLBACK_STATE=NOT_NEEDED"} {
		if !strings.Contains(text, want) {
			t.Fatalf("normalized state missing %q:\n%s", want, text)
		}
	}
}

func TestUnknownUpdaterReceiptBlocksVPNWithoutTmpFenceAfterReboot(t *testing.T) {
	for _, tc := range []struct {
		name  string
		state string
	}{
		{"rollback-failed", "STATE=ROLLBACK_FAILED\nROLLBACK_STATE=FAILED_UNKNOWN\n"},
		{"reconnecting", "STATE=RECONNECTING\nROLLBACK_STATE=PENDING\n"},
		{"updating", "STATE=UPDATING\nROLLBACK_STATE=PENDING\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("FREENET_VPN_TRANSACTION_DIR", filepath.Join(root, "no-checkpoint"))
			state := filepath.Join(root, "self-update.state")
			if err := os.WriteFile(state, []byte(tc.state), 0o600); err != nil {
				t.Fatal(err)
			}
			lock := filepath.Join(root, "erased-by-reboot.lock")
			a := &app{cfg: config{UpdateState: state, UpdateLock: lock}}
			if !a.updateLockHeld() {
				t.Fatal("durable UNKNOWN receipt must hard-stop after /tmp lock vanished")
			}
			response := httptest.NewRecorder()
			if !a.mutationBlockedBySelfUpdate(response) || response.Code != http.StatusConflict {
				t.Fatalf("unsafe mutation allowed without owner lock: %d %s", response.Code, response.Body.String())
			}
			if _, err := os.Stat(lock); !os.IsNotExist(err) {
				t.Fatalf("hard-stop fabricated a new lock: %v", err)
			}
			receipt, err := os.ReadFile(state)
			if err != nil || string(receipt) != tc.state {
				t.Fatal("read-only hard stop must preserve the original receipt")
			}
		})
	}
}
