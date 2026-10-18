package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestManualRecoveryDistinguishesStoppedFromUnknownCore(t *testing.T) {
	root := t.TempDir()
	if got := manualRecoveryCoreStateInProc(root, "/opt/etc/xray/configs"); got != "stopped" {
		t.Fatalf("empty inventory=%s", got)
	}
	if got := manualRecoveryCoreStateInProc(filepath.Join(root, "missing"), "/opt/etc/xray/configs"); got != "unknown" {
		t.Fatalf("unreadable inventory=%s", got)
	}
	writeFakeProc(t, root, "100", "xray", isolatedXrayProbeFlag+"\x00", "Gid:\t0\t0\t0\t0\n")
	if got := manualRecoveryCoreStateInProc(root, "/opt/etc/xray/configs"); got != "stopped" {
		t.Fatalf("isolated probe mistaken for production: %s", got)
	}
	writeFakeProc(t, root, "101", "xray", "XRAY_LOCATION_CONFDIR=/opt/etc/xray/configs\x00", "Gid:\t0\t0\t0\t0\n")
	if got := manualRecoveryCoreStateInProc(root, "/opt/etc/xray/configs"); got != "running" {
		t.Fatalf("exact production=%s", got)
	}
	writeFakeProc(t, root, "102", "xray", "XRAY_LOCATION_CONFDIR=/other/configs\x00", "Gid:\t0\t0\t0\t0\n")
	if got := manualRecoveryCoreStateInProc(root, "/opt/etc/xray/configs"); got != "unknown" {
		t.Fatalf("ambiguous second core accepted: %s", got)
	}
	os.RemoveAll(filepath.Join(root, "102"))
	os.Remove(filepath.Join(root, "101", "environ"))
	if got := manualRecoveryCoreStateInProc(root, "/opt/etc/xray/configs"); got != "unknown" {
		t.Fatalf("missing process facts accepted: %s", got)
	}
}

func TestManualRecoveryStoppedAndCompleteInterruptedAttempt(t *testing.T) {
	a, pending, live := newVPNTransactionDoctorFixture(t)
	t.Setenv("FREENET_XRAY_PROC_ROOT", t.TempDir())
	initial := a.vpnTransactionDoctor()
	if initial.XrayCore != "stopped" || !vpnManualRecoveryEligible(initial) {
		t.Fatalf("known stopped core still blocks manual recovery: %+v", initial)
	}
	attempt := filepath.Join(pending, "recovery.pending")
	if err := os.Mkdir(attempt, 0700); err != nil {
		t.Fatal(err)
	}
	for kind, path := range live {
		before, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(attempt, kind+".before"), before, 0600); err != nil {
			t.Fatal(err)
		}
	}
	os.WriteFile(filepath.Join(attempt, "state"), []byte("PENDING\n"), 0600)
	os.WriteFile(filepath.Join(attempt, "core.before"), []byte("running\n"), 0600)
	os.WriteFile(live["out"], []byte(`{"outbounds":[],"partial":"readable"}`), 0600)
	result := a.vpnTransactionDoctor()
	if result.RecoveryAttempt != "complete-readable" || !vpnManualRecoveryEligible(result) {
		t.Fatalf("complete interrupted state cannot be reconciled by a new explicit choice: %+v", result)
	}
	if _, err := os.Stat(attempt); err != nil {
		t.Fatal("read-only doctor moved evidence")
	}
	if _, err := os.Stat(filepath.Join(attempt, "result")); !os.IsNotExist(err) {
		t.Fatal("read-only doctor committed a reconciliation")
	}
	os.Remove(filepath.Join(attempt, "profile.before"))
	result = a.vpnTransactionDoctor()
	if result.Decision != "STOP_RECOVERY_INTERRUPTED" || vpnManualRecoveryEligible(result) {
		t.Fatalf("incomplete before generation accepted: %+v", result)
	}
}

func TestManualRecoveryRejectsUnsafeNewFileAfterAbsenceSnapshot(t *testing.T) {
	a, pending, live := newVPNTransactionDoctorFixture(t)
	os.Remove(filepath.Join(pending, "profile.before"))
	os.WriteFile(filepath.Join(pending, "profile.absent"), nil, 0600)
	os.Remove(live["profile"])
	if err := os.Symlink(live["out"], live["profile"]); err != nil {
		t.Fatal(err)
	}
	if got := a.vpnTransactionDoctor().Profile; got != "unknown" {
		t.Fatalf("unsafe absent-to-symlink transition=%s", got)
	}
}

func TestManualVPNRecoveryShellMatrix(t *testing.T) {
	out, err := exec.Command("sh", "../tests/test_manual_vpn_recovery.sh").CombinedOutput()
	if err != nil {
		t.Fatalf("manual recovery lifecycle: %v\n%s", err, out)
	}
}
