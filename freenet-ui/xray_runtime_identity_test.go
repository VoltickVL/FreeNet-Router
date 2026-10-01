package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func writeFakeProc(t *testing.T, root, pid, comm, environ, status string) {
	t.Helper()
	dir := filepath.Join(root, pid)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "comm"), []byte(comm+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if environ != "" {
		if err := os.WriteFile(filepath.Join(dir, "environ"), []byte(environ), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if status != "" {
		if err := os.WriteFile(filepath.Join(dir, "status"), []byte(status), 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestLiveXrayProcessRunningIgnoresProbeAndUsesConfigDir(t *testing.T) {
	root := t.TempDir()
	writeFakeProc(t, root, "100", "xray", isolatedXrayProbeFlag+"\x00XRAY_LOCATION_CONFDIR=/tmp/probe\x00", "Gid:\t0\t0\t0\t0\n")
	writeFakeProc(t, root, "101", "xray", "XRAY_LOCATION_CONFDIR=/opt/etc/xray/configs\x00", "Gid:\t0\t0\t0\t0\n")
	if !liveXrayProcessRunningInProc(root, "/opt/etc/xray/configs") {
		t.Fatal("live XKeen Xray with exact config dir was not detected")
	}
}

func TestLiveXrayProcessRunningFallsBackToExpectedGID(t *testing.T) {
	root := t.TempDir()
	writeFakeProc(t, root, "200", "xray", "", "Gid:\t11111\t11111\t11111\t11111\n")
	if !liveXrayProcessRunningInProc(root, "/custom/configs") {
		t.Fatal("expected GID fallback did not detect live Xray")
	}
}

func TestLiveXrayProcessRunningRejectsProbeOnly(t *testing.T) {
	root := t.TempDir()
	writeFakeProc(t, root, "300", "xray", isolatedXrayProbeFlag+"\x00", "Gid:\t11111\t11111\t11111\t11111\n")
	if liveXrayProcessRunningInProc(root, "/opt/etc/xray/configs") {
		t.Fatal("probe Xray must never satisfy live runtime identity")
	}
}

func TestIsolatedXrayProbeAliasAndMarker(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "xray")
	if err := os.WriteFile(target, []byte("x"), 0700); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "probe")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path, err := isolatedXrayProbePath(dir, target)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(path) == "xray" {
		t.Fatal("isolated probe executable must not be named xray")
	}
	got, err := os.Readlink(path)
	if err != nil || got != target {
		t.Fatalf("probe alias target=%q err=%v", got, err)
	}
	env := isolatedXrayProbeEnv([]string{"A=B"})
	found := false
	for _, value := range env {
		if value == isolatedXrayProbeFlag {
			found = true
		}
	}
	if !found {
		t.Fatal("probe marker missing from environment")
	}
}


func TestLiveXrayProcessRunningRejectsAmbiguousLiveProcesses(t *testing.T) {
	root := t.TempDir()
	writeFakeProc(t, root, "401", "xray", "XRAY_LOCATION_CONFDIR=/opt/etc/xray/configs\x00", "Gid:\t11111\t11111\t11111\t11111\n")
	writeFakeProc(t, root, "402", "xray", "XRAY_LOCATION_CONFDIR=/opt/etc/xray/configs\x00", "Gid:\t11111\t11111\t11111\t11111\n")
	if liveXrayProcessRunningInProc(root, "/opt/etc/xray/configs") {
		t.Fatal("multiple exact live Xray processes must fail closed")
	}

	root2 := t.TempDir()
	writeFakeProc(t, root2, "501", "xray", "", "Gid:\t11111\t11111\t11111\t11111\n")
	writeFakeProc(t, root2, "502", "xray", "", "Gid:\t11111\t11111\t11111\t11111\n")
	if liveXrayProcessRunningInProc(root2, "/custom/configs") {
		t.Fatal("multiple GID fallback Xray processes must fail closed")
	}
}

func TestIsolatedXrayProbeAliasChangesLinuxProcessIdentity(t *testing.T) {
	sleepPath, err := exec.LookPath("sleep")
	if err != nil {
		t.Skip("sleep binary unavailable")
	}
	dir := t.TempDir()
	alias, err := isolatedXrayProbePath(dir, sleepPath)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(alias, "2")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()
	comm, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(cmd.Process.Pid), "comm"))
	if err != nil {
		t.Skipf("/proc comm unavailable: %v", err)
	}
	if got := strings.TrimSpace(string(comm)); got != isolatedXrayProbeName {
		t.Fatalf("probe process comm=%q want %q; XKeen pidof isolation would be unsafe", got, isolatedXrayProbeName)
	}
}
