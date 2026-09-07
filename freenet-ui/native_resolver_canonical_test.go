package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func canonicalNativePlanFixture() string {
	fixture := bridgePlanFixture("firmware", "off", "public", "ndnproxy", "native")
	fixture = strings.Replace(fixture, "NDM_DNS_INTERCEPT=off", "NDM_DNS_INTERCEPT=on", 1)
	fixture = strings.Replace(fixture, "XRAY_DNS_INBOUND_COUNT=1", "XRAY_DNS_INBOUND_COUNT=0", 1)
	fixture = strings.Replace(fixture, "DNS_OUT=yes", "DNS_OUT=no", 1)
	return fixture
}

func writeNativeProviderTestConfig(t *testing.T, provider string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "freenet.conf")
	content := "ISP_ID=rostelecom\nDNS_MODE=firmware\nNATIVE_DNS_PROVIDER=" + provider + "\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FREENET_CONFIG_FILE", path)
	return path
}

func installFakeNDMC(t *testing.T, runningConfig string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "ndmc")
	script := "#!/bin/sh\nif [ \"$1\" = \"-c\" ] && [ \"$2\" = \"show running-config\" ]; then\ncat <<'EOF'\n" + runningConfig + "\nEOF\nexit 0\nfi\nexit 0\n"
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
}

func TestCanonicalNativeResolverTargetIsExactYandexBasic(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("FREENET_NATIVE_DNS_STATE_DIR", dir)
	writeNativeProviderTestConfig(t, nativeDNSProviderYandexBasic)

	got, source, err := networkBridgeCanonicalNativeResolverTarget("192.168.1.1")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"ip name-server 77.88.8.8", "ip name-server 77.88.8.1"}
	if source != nativeDNSProviderYandexBasic || !networkBridgeResolverSelectionsEqual(got, want) {
		t.Fatalf("target=%v source=%q", got, source)
	}
}

func TestYandexProviderIgnoresHistoricalSnapshot(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("FREENET_NATIVE_DNS_STATE_DIR", dir)
	writeNativeProviderTestConfig(t, nativeDNSProviderYandexBasic)
	if err := networkBridgeWriteNativeResolverSelection([]string{"ip name-server 1.1.1.1", "ip name-server 9.9.9.9"}); err != nil {
		t.Fatal(err)
	}

	got, source, err := networkBridgeCanonicalNativeResolverTarget("192.168.1.1")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"ip name-server 77.88.8.8", "ip name-server 77.88.8.1"}
	if source != nativeDNSProviderYandexBasic || !networkBridgeResolverSelectionsEqual(got, want) {
		t.Fatalf("historical snapshot became Yandex target: target=%v source=%q", got, source)
	}
}

func TestRouterCurrentProviderUsesActiveRouterSelection(t *testing.T) {
	t.Setenv("FREENET_NATIVE_DNS_STATE_DIR", t.TempDir())
	writeNativeProviderTestConfig(t, nativeDNSProviderRouterCurrent)
	installFakeNDMC(t, "ip name-server 1.1.1.1\nip name-server 9.9.9.9")

	got, source, err := networkBridgeCanonicalNativeResolverTarget("192.168.1.1")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"ip name-server 1.1.1.1", "ip name-server 9.9.9.9"}
	if source != nativeDNSProviderRouterCurrent || !networkBridgeResolverSelectionsEqual(got, want) {
		t.Fatalf("router target=%v source=%q", got, source)
	}
}

func TestRouterCurrentProviderRestoresSignedSnapshotWhileSplit(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("FREENET_NATIVE_DNS_STATE_DIR", dir)
	writeNativeProviderTestConfig(t, nativeDNSProviderRouterCurrent)
	installFakeNDMC(t, "ip name-server 192.168.1.1:53")
	want := []string{"ip name-server 77.88.8.8", "ip name-server 77.88.8.1"}
	if err := networkBridgeWriteNativeResolverSelection(want); err != nil {
		t.Fatal(err)
	}

	got, source, err := networkBridgeCanonicalNativeResolverTarget("192.168.1.1")
	if err != nil {
		t.Fatal(err)
	}
	if source != nativeDNSProviderRouterCurrent || !networkBridgeResolverSelectionsEqual(got, want) {
		t.Fatalf("snapshot target=%v source=%q", got, source)
	}
}

func TestNativeResolverPlanTreatsInheritedActiveResolversAsRealDelta(t *testing.T) {
	input := canonicalNativePlanFixture()
	got := augmentNetworkBridgeNativeResolverPlan(input, nativeDNSProviderYandexBasic+"-replace-needed")
	values := parseNetworkBridgeValues(got)

	if values["DNS_ROUTING_MODE"] != "native-resolver-selection-replace" {
		t.Fatalf("routing marker=%q", values["DNS_ROUTING_MODE"])
	}
	if !strings.Contains(values["EXPECTED_DELTA"], "Yandex Basic") ||
		!strings.Contains(values["EXPECTED_DELTA"], "77.88.8.8/77.88.8.1") {
		t.Fatalf("expected delta=%q", values["EXPECTED_DELTA"])
	}
	plan, err := parseNetworkPlan(got)
	if err != nil {
		t.Fatal(err)
	}
	if networkPlanIsActive(plan) {
		t.Fatal("structurally native router with unmanaged active resolver set must require canonicalization")
	}
}

func TestNativeResolverPlanKeepsCanonicalNativeActive(t *testing.T) {
	input := canonicalNativePlanFixture()
	got := augmentNetworkBridgeNativeResolverPlan(input, "existing-native-resolver-ready")
	values := parseNetworkBridgeValues(got)
	if values["DNS_ROUTING_MODE"] != "native" {
		t.Fatalf("canonical native routing marker=%q", values["DNS_ROUTING_MODE"])
	}
	plan, err := parseNetworkPlan(got)
	if err != nil {
		t.Fatal(err)
	}
	if !networkPlanIsActive(plan) {
		t.Fatalf("canonical native unexpectedly inactive: %s", networkPlanActiveMismatch(plan))
	}
}

func TestResolverSelectionComparisonIsOrderIndependentButExact(t *testing.T) {
	left := []string{"ip name-server 77.88.8.8", "ip name-server 77.88.8.1"}
	right := []string{"ip name-server 77.88.8.1", "ip name-server 77.88.8.8"}
	if !networkBridgeResolverSelectionsEqual(left, right) {
		t.Fatal("same active resolver set in different order must compare equal")
	}
	if networkBridgeResolverSelectionsEqual(left, []string{"ip name-server 77.88.8.8"}) {
		t.Fatal("missing resolver must not compare equal")
	}
	if networkBridgeResolverSelectionsEqual(left, []string{"ip name-server 77.88.8.8", "ip name-server 1.1.1.1"}) {
		t.Fatal("inherited conflicting resolver must not compare equal")
	}
}
