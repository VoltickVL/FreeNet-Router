package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func xrayInventoryFixture(t *testing.T, root, binary, pid, comm, state, env, group string) {
	t.Helper()
	path := filepath.Join(root, pid)
	if err := os.MkdirAll(path, 0700); err != nil { t.Fatal(err) }
	write := func(name, data string) {
		if err := os.WriteFile(filepath.Join(path, name), []byte(data), 0600); err != nil { t.Fatal(err) }
	}
	write("comm", comm+"\n")
	write("status", "Name:\t"+comm+"\nState:\t"+state+"\nGid:\t"+group+"\n")
	write("environ", strings.ReplaceAll(env, "|", "\x00"))
	if err := os.Link(binary, filepath.Join(path, "exe")); err != nil { t.Fatal(err) }
}

func TestXrayProcessInventoryClassifiesProductionAndOwnedProbes(t *testing.T) {
	root := t.TempDir()
	binary := filepath.Join(t.TempDir(), "xray")
	if err := os.WriteFile(binary, []byte("fixture"), 0700); err != nil { t.Fatal(err) }
	group := "11111\t11111\t11111\t11111"
	conf := "/opt/etc/xray/configs"
	assert := func(want string, prod, probe, unknown, zombie int) {
		t.Helper()
		got := scanXrayProcessInventory(root, conf, binary)
		if got.State != want || got.ProductionCount != prod || got.ProbeCount != probe ||
			got.UnknownCount != unknown || got.ZombieCount != zombie || !got.Success || got.Mutation != "NONE" ||
			got.RestartReady != (want == "ready") {
			t.Fatalf("inventory=%+v want %s (%d,%d,%d,%d)", got,want,prod,probe,unknown,zombie)
		}
	}
	assert("production-xray-absent",0,0,0,0)
	xrayInventoryFixture(t, root, binary, "101", "xray", "S", "XRAY_LOCATION_CONFDIR="+conf+"|", group)
	assert("ready",1,0,0,0)
	xrayInventoryFixture(t, root, binary, "102", "fn-xray-probe", "S", "FREENET_XRAY_PROBE=1|", group)
	assert("isolated-probe-active",1,1,0,0)
	if err := os.RemoveAll(filepath.Join(root, "102")); err != nil { t.Fatal(err) }
	xrayInventoryFixture(t, root, binary, "103", "xray", "S", "XRAY_LOCATION_CONFDIR="+conf+"|", group)
	assert("multiple-production-xray",2,0,0,0)
	if err := os.RemoveAll(filepath.Join(root, "103")); err != nil { t.Fatal(err) }
	xrayInventoryFixture(t, root, binary, "104", "fn-xray-probe", "S", "UNTRUSTED=1|", group)
	assert("unknown-xray-process",1,0,1,0)
	if err := os.RemoveAll(filepath.Join(root, "104")); err != nil { t.Fatal(err) }
	xrayInventoryFixture(t, root, binary, "105", "xray", "Z", "XRAY_LOCATION_CONFDIR="+conf+"|", group)
	assert("unreaped-xray-process",1,0,0,1)
	if err := os.RemoveAll(filepath.Join(root, "105")); err != nil { t.Fatal(err) }
	assert("ready",1,0,0,0)
}

func TestXrayProcessInventoryRejectsDifferentOwnerAndUntrustedBinary(t *testing.T) {
	root := t.TempDir()
	binary := filepath.Join(t.TempDir(), "xray")
	if err := os.WriteFile(binary, []byte("fixture"), 0700); err != nil { t.Fatal(err) }
	xrayInventoryFixture(t, root, binary, "121", "xray", "S", "XRAY_LOCATION_CONFDIR=/tmp/wrong|", "11111\t11111\t11111\t11111")
	got := scanXrayProcessInventory(root, "/opt/etc/xray/configs", binary)
	if got.State != "unknown-xray-process" || got.RestartReady || got.ProductionCount != 0 { t.Fatalf("wrong config was trusted: %+v",got) }
	if err := os.RemoveAll(filepath.Join(root,"121")); err != nil { t.Fatal(err) }
	xrayInventoryFixture(t, root, binary, "122", "xray", "S", "XRAY_LOCATION_CONFDIR=/opt/etc/xray/configs|", "0\t0\t0\t0")
	got = scanXrayProcessInventory(root,"/opt/etc/xray/configs",binary)
	if got.State != "unknown-xray-process" || got.RestartReady { t.Fatalf("wrong routing group trusted: %+v",got) }
	if result := scanXrayProcessInventory(filepath.Join(root,"missing"),"/opt/etc/xray/configs",binary); result.Success || result.State != "proc-unavailable" { t.Fatalf("unreadable proc was trusted: %+v",result) }
}
