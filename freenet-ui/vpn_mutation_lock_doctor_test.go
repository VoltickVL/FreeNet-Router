package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVPNMutationLockReadOnlyClassification(t *testing.T) {
	root := t.TempDir()
	lock := filepath.Join(root, "lock")
	proc := filepath.Join(root, "proc")
	if err := os.Mkdir(proc, 0700); err != nil {
		t.Fatal(err)
	}
	if got := vpnMutationLockState(lock, proc); got != "absent" {
		t.Fatalf("absent lock: %q", got)
	}
	if err := os.Mkdir(lock, 0700); err != nil {
		t.Fatal(err)
	}
	if got := vpnMutationLockState(lock, proc); got != "unknown" {
		t.Fatalf("missing owner marker accepted: %q", got)
	}
	pidFile := filepath.Join(lock, "pid")
	if err := os.WriteFile(pidFile, []byte("24369\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if got := vpnMutationLockState(lock, proc); got != "owner_pid_absent" {
		t.Fatalf("missing PID in proc not detected: %q", got)
	}
	if b, err := os.ReadFile(pidFile); err != nil || string(b) != "24369\n" {
		t.Fatal("read-only classification mutated PID file")
	}
	if err := os.Mkdir(filepath.Join(proc, "24369"), 0700); err != nil {
		t.Fatal(err)
	}
	if got := vpnMutationLockState(lock, proc); got != "pid_present_unverified" {
		t.Fatalf("possible PID reuse treated as known owner: %q", got)
	}
	// The command line, process name, or PID alone cannot establish that the
	// live process is the original lock owner.
	if err := os.WriteFile(filepath.Join(proc, "24369", "comm"), []byte("foreign-process\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if got := vpnMutationLockState(lock, proc); got != "pid_present_unverified" {
		t.Fatalf("foreign process identity was trusted: %q", got)
	}
	marker := filepath.Join(lock, "freenet-update-owner")
	if err := os.WriteFile(marker, []byte("/tmp/freenet-self-update.lock\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if got := vpnMutationLockState(lock, proc); got != "update_owner_unverified" {
		t.Fatalf("updater-owned lock mistaken for VPN lock: %q", got)
	}
	if err := os.Remove(marker); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"", "a123", "0", "-1", "24369 extra", strings.Repeat("7", 40)} {
		if err := os.WriteFile(pidFile, []byte(bad), 0600); err != nil {
			t.Fatal(err)
		}
		if got := vpnMutationLockState(lock, proc); got != "unknown" {
			t.Fatalf("malformed PID %q accepted: %q", bad, got)
		}
	}
	if err := os.Remove(pidFile); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(proc, "24369", "comm"), pidFile); err == nil {
		if got := vpnMutationLockState(lock, proc); got != "unknown" {
			t.Fatalf("symlinked PID file accepted: %q", got)
		}
		if err := os.Remove(pidFile); err != nil {
			t.Fatal(err)
		}
	}
	link := filepath.Join(root, "linked-lock")
	if err := os.Symlink(lock, link); err == nil {
		if got := vpnMutationLockState(link, proc); got != "unknown" {
			t.Fatalf("symlinked lock directory accepted: %q", got)
		}
	}
}

func TestVPNDoctorNestedRecoveryAndOrphanedLockRemainSTOP(t *testing.T) {
	a, checkpoint, live := newVPNTransactionDoctorFixture(t)
	root := t.TempDir()
	proc := filepath.Join(root, "proc")
	if err := os.Mkdir(proc, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FREENET_PROC_ROOT", proc)
	a.cfg.LockPath = filepath.Join(root, "lock")
	if err := os.Mkdir(a.cfg.LockPath, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(a.cfg.LockPath, "pid"), []byte("24369\n"), 0600); err != nil {
		t.Fatal(err)
	}
	attempt := filepath.Join(checkpoint, "recovery.pending")
	if err := os.Mkdir(attempt, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(attempt, "state"), []byte("PENDING\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(attempt, "core.before"), []byte("running\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for kind, file := range live {
		snapshot, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(attempt, kind+".before"), snapshot, 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, append(snapshot, []byte("-CHANGED-CANDIDATE")...), 0600); err != nil {
			t.Fatal(err)
		}
	}
	result := a.vpnTransactionDoctor()
	if result.LockState != "owner_pid_absent" || result.RecoveryAttempt != "complete-readable" ||
		result.RecoveryCoreBefore != "running" || result.RecoveryOutbound != "different" ||
		result.RecoveryProfile != "different" || result.RecoveryFilter != "different" ||
		result.Outbound != "different" || result.Profile != "different" || result.Filter != "different" ||
		result.Checkpoint != "pending" || result.Decision != "STOP_FILES_UNRESOLVED" ||
		result.FilesRestored || !result.MutationBlocked || result.Mutation != "NONE" {
		t.Fatalf("unknown nested rollback was improperly accepted: %+v", result)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	for _, private := range []string{
		"TEST-SENSITIVE-OUTBOUND", "TEST-SENSITIVE-PROFILE", "TEST-SENSITIVE-FILTER",
		"CHANGED-CANDIDATE", checkpoint, root, "24369",
	} {
		if strings.Contains(string(encoded), private) {
			t.Fatalf("diagnostic leaked private or implementation data: %q", private)
		}
	}
	for kind, livePath := range live {
		snapshot, err := os.ReadFile(filepath.Join(attempt, kind+".before"))
		if err != nil {
			t.Fatal(err)
		}
		current, err := os.ReadFile(livePath)
		if err != nil || !strings.HasPrefix(string(current), string(snapshot)) {
			t.Fatalf("read-only doctor unexpectedly changed %s", kind)
		}
	}
	// Corrupt nested state must fail closed while the original checkpoint and
	// all live files remain untouched.
	if err := os.WriteFile(filepath.Join(attempt, "core.before"), []byte("UNKNOWN-PRIVATE-CORE\n"), 0600); err != nil {
		t.Fatal(err)
	}
	result = a.vpnTransactionDoctor()
	if result.RecoveryAttempt != "unresolved" || result.RecoveryCoreBefore != "unknown" ||
		result.Decision != "STOP_RECOVERY_INTERRUPTED" || result.LockState != "owner_pid_absent" {
		t.Fatalf("corrupt nested recovery not fenced: %+v", result)
	}
	if data, err := json.Marshal(result); err != nil || strings.Contains(string(data), "UNKNOWN-PRIVATE-CORE") {
		t.Fatal("unsafe nested recovery marker leaked")
	}
}
