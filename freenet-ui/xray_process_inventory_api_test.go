package main

import (
	"encoding/json"
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
			got.RestartReady != (want == "ready") || (unknown == 0 && len(got.Reasons) != 0) {
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


func TestXrayProcessInventoryReadOnlyReasonsStayFiniteAndDoNotWeakenAdmission(t *testing.T) {
	const conf = "/opt/etc/xray/configs"
	const correctGID = "11111\t11111\t11111\t11111"
	cases := []struct {
		name      string
		comm      string
		env       string
		group     string
		sabotage  string
		wantCode  string
	}{
		{"config-mismatch", "xray", "XRAY_LOCATION_CONFDIR=/opt/other|", correctGID, "", "live-config-environment-mismatch"},
		{"gid-mismatch", "xray", "XRAY_LOCATION_CONFDIR=" + conf + "|", "0\t0\t0\t0", "", "routing-gid-mismatch"},
		{"alias-untrusted", "fn-xray-probe", "OTHER=1|", correctGID, "", "untrusted-xray-alias"},
		{"process-environment-unreadable", "xray", "XRAY_LOCATION_CONFDIR=" + conf + "|", correctGID, "remove-environ", "process-environment-unavailable"},
		{"process-state-unreadable", "xray", "XRAY_LOCATION_CONFDIR=" + conf + "|", correctGID, "remove-status", "process-state-unavailable"},
		{"process-executable-unreadable", "xray", "XRAY_LOCATION_CONFDIR=" + conf + "|", correctGID, "remove-exe", "process-executable-unavailable"},
		{"binary-inode-mismatch", "xray", "XRAY_LOCATION_CONFDIR=" + conf + "|", correctGID, "other-exe", "binary-inode-mismatch"},
		{"expected-binary-missing", "xray", "XRAY_LOCATION_CONFDIR=" + conf + "|", correctGID, "missing-expected-binary", "expected-binary-unavailable"},
		{"live-config-identity-missing", "xray", "XRAY_LOCATION_CONFDIR=" + conf + "|", correctGID, "missing-config", "live-config-identity-unavailable"},
		{"comm-unreadable", "xray", "XRAY_LOCATION_CONFDIR=" + conf + "|", correctGID, "comm-directory", "process-comm-unreadable"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			binary := filepath.Join(t.TempDir(), "xray")
			if err := os.WriteFile(binary, []byte("expected-binary"), 0700); err != nil {
				t.Fatal(err)
			}
			xrayInventoryFixture(t, root, binary, "101", tc.comm, "S", tc.env, tc.group)
			task := filepath.Join(root, "101")
			expected := binary
			configDir := conf
			switch tc.sabotage {
			case "":
			case "remove-environ", "remove-status", "remove-exe":
				name := strings.TrimPrefix(tc.sabotage, "remove-")
				if err := os.Remove(filepath.Join(task, name)); err != nil { t.Fatal(err) }
			case "other-exe":
				if err := os.Remove(filepath.Join(task, "exe")); err != nil { t.Fatal(err) }
				if err := os.WriteFile(filepath.Join(task, "exe"), []byte("other-inode"), 0700); err != nil { t.Fatal(err) }
			case "missing-expected-binary":
				expected = filepath.Join(t.TempDir(), "no-such-binary")
			case "missing-config":
				configDir = ""
			case "comm-directory":
				if err := os.Remove(filepath.Join(task, "comm")); err != nil { t.Fatal(err) }
				if err := os.Mkdir(filepath.Join(task, "comm"), 0700); err != nil { t.Fatal(err) }
			default:
				t.Fatal("unsupported fixture")
			}
			got := scanXrayProcessInventory(root, configDir, expected)
			if !got.Success || got.Mutation != "NONE" || got.RestartReady ||
				got.State != "unknown-xray-process" || got.ProductionCount != 0 ||
				got.ProbeCount != 0 || got.ZombieCount != 0 || got.UnknownCount != 1 ||
				len(got.Reasons) != 1 || got.Reasons[0].Code != tc.wantCode || got.Reasons[0].Count != 1 {
				t.Fatalf("expected exact finite rejection reason %q, got %+v", tc.wantCode, got)
			}
			public, err := json.Marshal(got)
			if err != nil { t.Fatal(err) }
			for _, forbidden := range []string{`"pid":`, conf, "XRAY_LOCATION_CONFDIR=", "/opt/wrong", "/opt/other", "other-inode"} {
				if strings.Contains(string(public), forbidden) {
					t.Fatalf("private process identity leaked into inventory response: %s", public)
				}
			}
		})
	}
}

func TestXrayProcessInventoryAggregatesReasonsInStableOrder(t *testing.T) {
	root := t.TempDir()
	binary := filepath.Join(t.TempDir(), "xray")
	if err := os.WriteFile(binary, []byte("fixture"), 0700); err != nil { t.Fatal(err) }
	const conf = "/opt/etc/xray/configs"
	group := "11111\t11111\t11111\t11111"
	xrayInventoryFixture(t, root, binary, "201", "xray", "S", "XRAY_LOCATION_CONFDIR=/opt/wrong|", group)
	xrayInventoryFixture(t, root, binary, "202", "xray", "S", "XRAY_LOCATION_CONFDIR="+conf+"|", "0\t0\t0\t0")
	got := scanXrayProcessInventory(root, conf, binary)
	if got.Mutation != "NONE" || !got.Success || got.RestartReady || got.UnknownCount != 2 ||
		got.State != "unknown-xray-process" || len(got.Reasons) != 2 ||
		got.Reasons[0] != (xrayProcessInventoryReason{Code: "live-config-environment-mismatch", Count: 1}) ||
		got.Reasons[1] != (xrayProcessInventoryReason{Code: "routing-gid-mismatch", Count: 1}) {
		t.Fatalf("expected deterministic count-only reasons, got %+v", got)
	}
}
