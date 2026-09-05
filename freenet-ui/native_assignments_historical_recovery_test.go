package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func nativeAssignmentsTestConfig(assignments ...string) string {
	lines := []string{
		"dns-proxy",
		"    rebind-protect auto",
		"    intercept enable",
		"    tls upstream common.dot.dns.yandex.net",
		"    https upstream https://common.dot.dns.yandex.net/dns-query",
		"    filter profile xbox-dns.ru",
		"    filter profile xbox-dns.ru description preserved",
		"    filter profile xbox-dns.ru tls upstream xbox-dns.ru",
	}
	for _, assignment := range assignments {
		lines = append(lines, "    "+assignment)
	}
	lines = append(lines,
		"    filter engine public",
		"!",
		"interface GigabitEthernet0/0",
		"    ip dhcp client dns-routes",
		"    ip dhcp client no name-servers",
		"!",
	)
	return strings.Join(lines, "\n") + "\n"
}

func splitAssignmentsTestConfig() string {
	return strings.Join([]string{
		"dns-proxy",
		"    rebind-protect auto",
		"    tls upstream common.dot.dns.yandex.net",
		"    https upstream https://common.dot.dns.yandex.net/dns-query",
		"    filter profile xbox-dns.ru",
		"    filter profile xbox-dns.ru description preserved",
		"    filter profile xbox-dns.ru tls upstream xbox-dns.ru",
		"    filter engine opkg",
		"!",
		"interface GigabitEthernet0/0",
		"    ip dhcp client dns-routes",
		"    ip dhcp client no name-servers",
		"!",
		"opkg dns-override",
		"!",
	}, "\n") + "\n"
}

func writeNativeAssignmentsRecoveryBaseline(t *testing.T, nativeDir string) {
	t.Helper()
	if err := os.MkdirAll(nativeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nativeDir, "filter-engine.native"), []byte("public\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nativeDir, "intercept.native"), []byte("on\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeNativeAssignmentsRecoveryBackup(t *testing.T, backupRoot, name, nativeConfig string) {
	t.Helper()
	dir := filepath.Join(backupRoot, name)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"ndm-override.before":      "off\n",
		"ndm-filter-engine.before": "public\n",
		"ndm-intercept.before":     "on\n",
		"ndm-protected.before":     networkBridgeProtectedStateText(nativeConfig) + "\n",
		"ndm-running.before":       nativeConfig,
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestBridgeRecoversNativeAssignmentsFromV052HistoricalBackup(t *testing.T) {
	root := t.TempDir()
	nativeDir := filepath.Join(root, "native-dns")
	backupRoot := filepath.Join(root, "backups")
	writeNativeAssignmentsRecoveryBaseline(t, nativeDir)

	nativeConfig := nativeAssignmentsTestConfig(
		"filter assign host profile aa:bb:cc:dd:ee:ff xbox-dns.ru",
		"filter assign interface preset Home cloudflare-unfiltered",
	)
	writeNativeAssignmentsRecoveryBackup(t, backupRoot, "freenet-network-split-legacy", nativeConfig)

	recovered, err := networkBridgeRecoverNativeAssignmentsSnapshot(nativeDir, backupRoot, splitAssignmentsTestConfig())
	if err != nil {
		t.Fatal(err)
	}
	if !recovered {
		t.Fatal("expected historical native assignments recovery")
	}
	data, err := os.ReadFile(filepath.Join(nativeDir, "assignments.native"))
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	for _, want := range []string{
		"filter assign host profile aa:bb:cc:dd:ee:ff xbox-dns.ru",
		"filter assign interface preset Home cloudflare-unfiltered",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("recovered assignments %q missing %q", got, want)
		}
	}

	recovered, err = networkBridgeRecoverNativeAssignmentsSnapshot(nativeDir, backupRoot, splitAssignmentsTestConfig())
	if err != nil {
		t.Fatal(err)
	}
	if recovered {
		t.Fatal("valid existing assignments snapshot must be reused, not rewritten")
	}
}

func TestBridgeRejectsAmbiguousHistoricalNativeAssignments(t *testing.T) {
	root := t.TempDir()
	nativeDir := filepath.Join(root, "native-dns")
	backupRoot := filepath.Join(root, "backups")
	writeNativeAssignmentsRecoveryBaseline(t, nativeDir)

	first := nativeAssignmentsTestConfig("filter assign host profile aa:bb:cc:dd:ee:ff xbox-dns.ru")
	second := nativeAssignmentsTestConfig("filter assign interface preset Home cloudflare-unfiltered")
	writeNativeAssignmentsRecoveryBackup(t, backupRoot, "freenet-network-split-a", first)
	writeNativeAssignmentsRecoveryBackup(t, backupRoot, "freenet-network-split-b", second)

	_, err := networkBridgeRecoverNativeAssignmentsSnapshot(nativeDir, backupRoot, splitAssignmentsTestConfig())
	if err == nil || !strings.Contains(err.Error(), "different DNS filter assignments") {
		t.Fatalf("expected ambiguity error, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(nativeDir, "assignments.native")); !os.IsNotExist(statErr) {
		t.Fatalf("ambiguous recovery must not persist snapshot, stat=%v", statErr)
	}
}

func TestBridgeRecoversAssignmentsDespiteProtectedResolverStateChange(t *testing.T) {
	root := t.TempDir()
	nativeDir := filepath.Join(root, "native-dns")
	backupRoot := filepath.Join(root, "backups")
	writeNativeAssignmentsRecoveryBaseline(t, nativeDir)

	nativeConfig := strings.ReplaceAll(
		nativeAssignmentsTestConfig("filter assign host profile aa:bb:cc:dd:ee:ff xbox-dns.ru"),
		"common.dot.dns.yandex.net",
		"old-resolver.example.net",
	)
	writeNativeAssignmentsRecoveryBackup(t, backupRoot, "freenet-network-split-old-resolver", nativeConfig)

	recovered, err := networkBridgeRecoverNativeAssignmentsSnapshot(nativeDir, backupRoot, splitAssignmentsTestConfig())
	if err != nil {
		t.Fatal(err)
	}
	if !recovered {
		t.Fatal("expected assignment recovery to ignore unrelated resolver profile drift")
	}
	data, err := os.ReadFile(filepath.Join(nativeDir, "assignments.native"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "filter assign host profile aa:bb:cc:dd:ee:ff xbox-dns.ru") {
		t.Fatalf("unexpected recovered assignments: %q", string(data))
	}
}

func TestBridgeStrictRuntimeSplitRequiresNoNativeAssignments(t *testing.T) {
	values := parseNetworkBridgeValues(bridgePlanFixture("xkeen", "on", "opkg", "xray", "split"))
	if !networkBridgeStrictRuntimeSplit(values) {
		t.Fatal("expected strict Split fixture")
	}
	values["NDM_DNS_ASSIGNMENTS"] = "present"
	if networkBridgeStrictRuntimeSplit(values) {
		t.Fatal("Split with active native assignments must not qualify for historical recovery")
	}
}
