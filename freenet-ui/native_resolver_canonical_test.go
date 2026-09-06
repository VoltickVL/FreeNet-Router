package main

import (
	"os"
	"strings"
	"testing"
)

func TestCanonicalNativeResolverFallbackIsExactYandexBasic(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("FREENET_NATIVE_DNS_STATE_DIR", dir)

	got, source, err := networkBridgeCanonicalNativeResolverTarget()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"ip name-server 77.88.8.8", "ip name-server 77.88.8.1"}
	if source != "yandex-basic-fallback" || !networkBridgeResolverSelectionsEqual(got, want) {
		t.Fatalf("target=%v source=%q", got, source)
	}
}

func TestCanonicalNativeResolverPrefersVerifiedSnapshot(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("FREENET_NATIVE_DNS_STATE_DIR", dir)
	want := []string{"ip name-server 1.1.1.1", "ip name-server 9.9.9.9"}
	if err := networkBridgeWriteNativeResolverSelection(want); err != nil {
		t.Fatal(err)
	}

	got, source, err := networkBridgeCanonicalNativeResolverTarget()
	if err != nil {
		t.Fatal(err)
	}
	if source != "native-resolver-snapshot" || !networkBridgeResolverSelectionsEqual(got, want) {
		t.Fatalf("target=%v source=%q", got, source)
	}
}

func TestCanonicalNativeResolverRejectsBrokenSnapshotInsteadOfGuessing(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("FREENET_NATIVE_DNS_STATE_DIR", dir)
	if err := networkBridgeWriteNativeResolverSelection([]string{"ip name-server 1.1.1.1"}); err != nil {
		t.Fatal(err)
	}
	_, hashPath := networkBridgeNativeResolverSnapshotPaths()
	if err := os.WriteFile(hashPath, []byte("broken\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := networkBridgeCanonicalNativeResolverTarget(); err == nil {
		t.Fatal("broken managed snapshot must STOP instead of falling back")
	}
}

func TestNativeResolverPlanTreatsInheritedActiveResolversAsRealDelta(t *testing.T) {
	input := bridgePlanFixture("firmware", "off", "public", "ndnproxy", "native")
	got := augmentNetworkBridgeNativeResolverPlan(input, "yandex-basic-replace-needed")
	values := parseNetworkBridgeValues(got)

	if values["DNS_ROUTING_MODE"] != "native-resolver-selection-replace" {
		t.Fatalf("routing marker=%q", values["DNS_ROUTING_MODE"])
	}
	if !strings.Contains(values["EXPECTED_DELTA"], "remove inherited active System resolvers") ||
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
	input := bridgePlanFixture("firmware", "off", "public", "ndnproxy", "native")
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
