package main

import (
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPendingVPNDoesNotBlockIndependentApplicationUpdate(t *testing.T) {
	root := t.TempDir()
	pending := filepath.Join(root, "vpn-transaction.pending")
	if err := os.Mkdir(pending, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FREENET_VPN_TRANSACTION_DIR", pending)
	t.Setenv("FREENET_AUTOMATION_STATE", filepath.Join(root, "auto.state"))
	marker := filepath.Join(root, "started")
	helper := filepath.Join(root, "self_update.sh")
	if err := os.WriteFile(helper, []byte("#!/bin/sh\nprintf started > '"+marker+"'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	a := &app{cfg: config{SelfUpdatePath: helper, UpdateLock: filepath.Join(root, "update.lock"), UpdateState: filepath.Join(root, "update.state")}}
	rr, result := performSelfUpdateApplyRequest(t, a, "v9.9.9")
	if rr.Code != http.StatusAccepted || !result.Success {
		t.Fatalf("VPN incorrectly blocks app repair: %d %s", rr.Code, rr.Body.String())
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(marker); err == nil {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatal("independent updater not launched")
	}
	if _, err := os.Stat(pending); err != nil {
		t.Fatal("application update removed VPN checkpoint")
	}
}

func TestUpdateAdmissionDoesNotTrustMissingLockOrUnreadableReceipt(t *testing.T) {
	for _, state := range []string{"STATE=ROLLBACK_FAILED\n", "STATE=FAILED\nROLLBACK_STATE=FAILED_UNKNOWN\n", "STATE=RECONNECTING\nROLLBACK_STATE=PENDING\n", "STATE=unrecognized\n"} {
		t.Run(strings.Split(state, "\n")[0], func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "update.state")
			if err := os.WriteFile(path, []byte(state), 0600); err != nil {
				t.Fatal(err)
			}
			a := &app{cfg: config{UpdateState: path, UpdateLock: filepath.Join(root, "absent.lock")}}
			if a.selfUpdateStateAdmission() == nil {
				t.Fatal("unsafe previous update accepted without lock")
			}
		})
	}
}

func TestTemporaryVerifiedUpdaterIsNotMisclassifiedAsStale(t *testing.T) {
	root := t.TempDir()
	proc := filepath.Join(root, "proc")
	lock := filepath.Join(root, "lock")
	if err := os.MkdirAll(proc, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(lock, 0700); err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(root, "state")
	if err := os.WriteFile(state, []byte("STATE=CHECKING\nROLLBACK_STATE=NOT_NEEDED\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FREENET_PROC_ROOT", proc)
	helper := filepath.Join(os.TempDir(), "freenet-browser-recovery.fixture", "self_update.sh")
	writeFakeProcCmdline(t, proc, 777, "/bin/sh", helper, "apply", "v0.8.0-beta.6")
	if err := os.WriteFile(filepath.Join(lock, "owner.pid"), []byte("777\n"), 0600); err != nil {
		t.Fatal(err)
	}
	a := &app{cfg: config{UpdateLock: lock, UpdateState: state, SelfUpdatePath: "/opt/lib/freenet/self_update.sh"}}
	held, stale := a.updateLockStatus()
	if !held || stale {
		t.Fatalf("temporary live updater mistaken for stale: %v %v", held, stale)
	}
	if err := a.unlockStaleUpdateLock(); err == nil {
		t.Fatal("live temporary updater unlocked")
	}
	if temporaryRecoveryUpdaterPath("/tmp/unrelated/self_update.sh") {
		t.Fatal("untrusted helper accepted")
	}
}

func TestStaleUpdaterCanReleaseOnlyItsOwnNetworkFence(t *testing.T) {
	root := t.TempDir()
	proc := filepath.Join(root, "proc")
	lock := filepath.Join(root, "updater")
	fence := filepath.Join(root, "network")
	for _, p := range []string{proc, lock, fence} {
		if err := os.Mkdir(p, 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("FREENET_PROC_ROOT", proc)
	state := filepath.Join(root, "state")
	os.WriteFile(state, []byte("STATE=CHECKING\nROLLBACK_STATE=NOT_NEEDED\n"), 0600)
	os.WriteFile(filepath.Join(lock, "owner.pid"), []byte("12345\n"), 0600)
	os.WriteFile(filepath.Join(fence, "pid"), []byte("12345\n"), 0600)
	os.WriteFile(filepath.Join(fence, "freenet-update-owner"), []byte(lock+"\n"), 0600)
	a := &app{cfg: config{UpdateLock: lock, UpdateState: state, LockPath: fence}}
	if err := a.unlockStaleUpdateLock(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(fence); !os.IsNotExist(err) {
		t.Fatal("dead updater's network fence still present")
	}
}

func TestSelfUpdateLifecycleShellMatrix(t *testing.T) {
	cmd := exec.Command("sh", "../tests/test_self_update_lifecycle.sh")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("lifecycle matrix: %v\n%s", err, out)
	}
}
