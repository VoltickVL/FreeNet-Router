package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Both launch models must pass only when all other runtime identity proofs
// match. Group 0 without exact live config or executable inode is NEVER trusted.
func TestXrayCoreNativeRuntimeIdentityRejectsWrongGroupAndProcess(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "xray")
	config := filepath.Join(dir, "configs")
	if err := os.WriteFile(binary, []byte("test"), 0755); err != nil { t.Fatal(err) }
	if err := os.MkdirAll(config, 0700); err != nil { t.Fatal(err) }
	proc := filepath.Join(dir, "proc")
	add := func(pid, gid, conf string) {
		t.Helper()
		dest := filepath.Join(proc, pid)
		if err := os.MkdirAll(dest, 0700); err != nil { t.Fatal(err) }
		if err := os.WriteFile(filepath.Join(dest, "comm"), []byte("xray\n"), 0600); err != nil { t.Fatal(err) }
		if err := os.WriteFile(filepath.Join(dest, "status"),
			[]byte("Name:\txray\nState:\tS\nGid:\t"+gid+"\n"), 0600); err != nil { t.Fatal(err) }
		if err := os.WriteFile(filepath.Join(dest, "environ"),
			[]byte("XRAY_LOCATION_CONFDIR="+conf+"\x00"), 0600); err != nil { t.Fatal(err) }
		if err := os.Symlink(binary, filepath.Join(dest, "exe")); err != nil { t.Fatal(err) }
	}
	const freeNetGID = "11111\t11111\t11111\t11111"
	const xkeenGID = "0\t0\t0\t0"
	add("1234", freeNetGID, config)
	if !xrayCoreNativeProcessMatchesInProc(proc, binary, config) {
		t.Fatal("healthy FreeNet-native group and owned process rejected")
	}
	if err := os.WriteFile(filepath.Join(proc, "1234", "status"),
		[]byte("Name:\txray\nState:\tS\nGid:\t"+xkeenGID+"\n"), 0600); err != nil { t.Fatal(err) }
	if !xrayCoreNativeProcessMatchesInProc(proc, binary, config) {
		t.Fatal("official XKeen root-launch with exact config and inode rejected")
	}
	for _, gid := range []string{"0\t0\t11111\t0", "11111\t0\t11111\t11111", "7\t7\t7\t7"} {
		if err := os.WriteFile(filepath.Join(proc, "1234", "status"),
			[]byte("Name:\txray\nState:\tS\nGid:\t"+gid+"\n"), 0600); err != nil { t.Fatal(err) }
		if xrayCoreNativeProcessMatchesInProc(proc, binary, config) {
			t.Fatalf("unknown/mixed launch GID accepted: %s", gid)
		}
	}
	if err := os.WriteFile(filepath.Join(proc, "1234", "status"),
		[]byte("Name:\txray\nState:\tS\nGid:\t"+xkeenGID+"\n"), 0600); err != nil { t.Fatal(err) }
	if err := os.WriteFile(filepath.Join(proc, "1234", "environ"),
		[]byte("XRAY_LOCATION_CONFDIR=/other\x00"), 0600); err != nil { t.Fatal(err) }
	if xrayCoreNativeProcessMatchesInProc(proc, binary, config) {
		t.Fatal("root launch using another config was trusted")
	}
	if err := os.WriteFile(filepath.Join(proc, "1234", "environ"),
		[]byte("XRAY_LOCATION_CONFDIR="+config+"\x00"), 0600); err != nil { t.Fatal(err) }
	add("1235", xkeenGID, config)
	if xrayCoreNativeProcessMatchesInProc(proc, binary, config) {
		t.Fatal("two Xray processes are ambiguous")
	}
	if err := os.RemoveAll(filepath.Join(proc, "1235")); err != nil { t.Fatal(err) }
	if err := os.Remove(filepath.Join(proc, "1234", "exe")); err != nil { t.Fatal(err) }
	old := filepath.Join(dir, "old-xray")
	if err := os.WriteFile(old, []byte("test"), 0755); err != nil { t.Fatal(err) }
	if err := os.Symlink(old, filepath.Join(proc, "1234", "exe")); err != nil { t.Fatal(err) }
	if xrayCoreNativeProcessMatchesInProc(proc, binary, config) {
		t.Fatal("old/untrusted executable inode accepted")
	}
}

func fakeFreeNetXrayProcess(t *testing.T, root, pid, config, gid, binary string) {
	t.Helper()
	dir := filepath.Join(root, pid)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	write := func(file, value string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, file), []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("comm", "xray\n")
	write("status", "Gid:\t"+gid+"\n")
	write("environ", "XRAY_LOCATION_CONFDIR="+config+"\x00")
	if err := os.Symlink(binary, filepath.Join(dir, "exe")); err != nil { t.Fatal(err) }
	// /proc/<pid>/stat field 22 (index 19 after the final ')') pins
	// a PID to its original process generation.
	write("stat", fmt.Sprintf("%s (xray) S %s 92765\n", pid, strings.Repeat("0 ", 18)))
}

func TestXrayCoreNativeInventoryRequiresSingleOwnedRuntime(t *testing.T) {
	proc := filepath.Join(t.TempDir(), "proc")
	config := "/opt/etc/xray/configs"
	binary := filepath.Join(t.TempDir(), "xray")
	if err := os.WriteFile(binary, []byte("test"), 0755); err != nil { t.Fatal(err) }
	fakeFreeNetXrayProcess(t, proc, "2345", config, "11111\t11111\t11111\t11111", binary)
	one, err := xrayCoreNativeInventory(proc, config, binary)
	if err != nil || one.pid != 2345 || one.startTime != "92765" {
		t.Fatalf("expected one pinned owned Xray, got %+v: %v", one, err)
	}
	alive, err := xrayCoreNativeStillAlive(proc, one)
	if err != nil || !alive {
		t.Fatalf("pinned process should be alive: %v, %v", alive, err)
	}
	fakeFreeNetXrayProcess(t, proc, "2346", config, "11111\t11111\t11111\t11111", binary)
	if _, err := xrayCoreNativeInventory(proc, config, binary); err == nil {
		t.Fatal("multiple production processes must STOP before any signal")
	}
	if err := os.RemoveAll(filepath.Join(proc, "2346")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proc, "2345", "environ"), []byte("XRAY_LOCATION_CONFDIR=/wrong\x00"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := xrayCoreNativeInventory(proc, config, binary); err == nil {
		t.Fatal("unrelated Xray config must STOP before any signal")
	}
	if err := os.WriteFile(filepath.Join(proc, "2345", "environ"), []byte("XRAY_LOCATION_CONFDIR="+config+"\x00"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proc, "2345", "status"), []byte("Gid:\t0\t0\t0\t0\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := xrayCoreNativeInventory(proc, config, binary); err != nil {
		t.Fatalf("trusted XKeen root launch must be restart-ready: %v", err)
	}
	if err := os.WriteFile(filepath.Join(proc, "2345", "status"), []byte("Gid:\t0\t11111\t0\t0\n"), 0600); err != nil { t.Fatal(err) }
	if _, err := xrayCoreNativeInventory(proc, config, binary); err == nil {
		t.Fatal("mixed Xray GID must STOP")
	}
	if err := os.WriteFile(filepath.Join(proc, "2345", "status"), []byte("Gid:\t0\t0\t0\t0\n"), 0600); err != nil { t.Fatal(err) }
	if err := os.Remove(filepath.Join(proc, "2345", "exe")); err != nil { t.Fatal(err) }
	other := filepath.Join(t.TempDir(), "other-xray")
	if err := os.WriteFile(other, []byte("bad"), 0755); err != nil { t.Fatal(err) }
	if err := os.Symlink(other, filepath.Join(proc, "2345", "exe")); err != nil { t.Fatal(err) }
	if _, err := xrayCoreNativeInventory(proc, config, binary); err == nil {
		t.Fatal("other executable path must STOP before any signal")
	}
}

func TestXrayCoreNativeEnvironmentNoXKeenUI(t *testing.T) {
	t.Setenv("XRAY_LOCATION_CONFDIR", "/wrong")
	t.Setenv("XRAY_LOCATION_ASSET", "/old")
	t.Setenv("FREENET_XRAY_PROBE", "1")
	env := xrayCoreNativeEnvironment("/opt/etc/xray/configs", "/opt/etc/xray/dat")
	foundConfig, foundAsset := 0, 0
	for _, value := range env {
		if strings.HasPrefix(value, "XRAY_LOCATION_CONFDIR=") {
			foundConfig++
			if value != "XRAY_LOCATION_CONFDIR=/opt/etc/xray/configs" {
				t.Fatal("native runtime inherited wrong config directory")
			}
		}
		if strings.HasPrefix(value, "XRAY_LOCATION_ASSET=") {
			foundAsset++
			if value != "XRAY_LOCATION_ASSET=/opt/etc/xray/dat" {
				t.Fatal("native runtime inherited wrong asset directory")
			}
		}
		if strings.HasPrefix(value, "FREENET_XRAY_PROBE=") {
			t.Fatal("production process must not inherit isolated diagnostic identity")
		}
	}
	if foundConfig != 1 || foundAsset != 1 {
		t.Fatalf("missing or repeated native Xray environment: config=%d asset=%d", foundConfig, foundAsset)
	}
}

func TestXrayCoreNativePreflightFailsWithoutRuntime(t *testing.T) {
	old := xrayCoreNativeRuntimeProof
	xrayCoreNativeRuntimeProof = func(string, string) bool { return false }
	defer func() { xrayCoreNativeRuntimeProof = old }()
	if err := xrayCoreNativePreflight(context.Background(), "/opt/sbin/xray", "/opt/etc/xray/configs"); err == nil {
		t.Fatal("unknown runtime must STOP before any binary mutation")
	}
}
