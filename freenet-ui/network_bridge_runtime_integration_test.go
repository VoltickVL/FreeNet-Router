package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBridgeRuntimeForcesCanonicalHelper(t *testing.T) {
	t.Setenv("FREENET_NETWORK_HELPER", "/tmp/legacy-or-custom-bypass")
	if err := networkBridgeInstallRequiredHelper(); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("FREENET_NETWORK_HELPER"); got != defaultNetworkBridgeExecutable {
		t.Fatalf("network helper=%q, want mandatory bridge %q", got, defaultNetworkBridgeExecutable)
	}
}

func TestBridgeRecognizesGoTestProcess(t *testing.T) {
	if !networkBridgeIsTestProcess() {
		t.Fatalf("test process %q must not receive production init env mutation", os.Args[0])
	}
}

func TestBridgeRecoveryPreflightStripsOnlyLocalSplitPointer(t *testing.T) {
	config := strings.Join([]string{
		"ip name-server 77.88.8.8",
		"ip name-server 192.168.50.1:53",
		"ip name-server 192.168.50.1 \"\" on Vladlink",
		"ip name-server 192.168.5.1",
		"dns-proxy",
		"    filter engine opkg",
		"!",
	}, "\n")
	got := networkBridgeConfigWithoutLocalPointer(config, "192.168.50.1")
	if strings.Contains(got, "192.168.50.1") {
		t.Fatalf("local Split pointer remained in read-only recovery view: %q", got)
	}
	for _, preserved := range []string{"ip name-server 77.88.8.8", "ip name-server 192.168.5.1", "filter engine opkg"} {
		if !strings.Contains(got, preserved) {
			t.Fatalf("recovery view lost %q: %q", preserved, got)
		}
	}
}

func TestBridgeRecoveryPreflightUsesSameHistoricalEvidenceAsApply(t *testing.T) {
	root := t.TempDir()
	nativeDir := filepath.Join(root, "native-dns")
	backupRoot := filepath.Join(root, "backups")
	writeNativeAssignmentsRecoveryBaseline(t, nativeDir)

	nativeConfig := nativeAssignmentsTestConfig(
		"filter assign host profile aa:bb:cc:dd:ee:ff xbox-dns.ru",
		"filter assign interface preset Home cloudflare-unfiltered",
	)
	writeNativeAssignmentsRecoveryBackup(t, backupRoot, "freenet-network-split-legacy", nativeConfig)

	currentSplit := splitAssignmentsTestConfig() + "ip name-server 192.168.50.1:53\n"
	readOnlyView := networkBridgeConfigWithoutLocalPointer(currentSplit, "192.168.50.1")
	candidate, err := networkBridgeFindNativeAssignmentsCandidate(nativeDir, backupRoot, readOnlyView)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"filter assign host profile aa:bb:cc:dd:ee:ff xbox-dns.ru",
		"filter assign interface preset Home cloudflare-unfiltered",
	} {
		if !strings.Contains(candidate, want) {
			t.Fatalf("candidate %q missing %q", candidate, want)
		}
	}
}
