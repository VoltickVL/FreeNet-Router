package main

import (
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

func TestCanonicalNativeResolverTargetIsExactYandexBasic(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("FREENET_NATIVE_DNS_STATE_DIR", dir)

	got, source, err := networkBridgeCanonicalNativeResolverTarget()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"ip name-server 77.88.8.8", "ip name-server 77.88.8.1"}
	if source != "yandex-basic" || !networkBridgeResolverSelectionsEqual(got, want) {
		t.Fatalf("target=%v source=%q", got, source)
	}
}

func TestCanonicalNativeResolverTargetIgnoresHistoricalSnapshot(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("FREENET_NATIVE_DNS_STATE_DIR", dir)
	if err := networkBridgeWriteNativeResolverSelection([]string{"ip name-server 1.1.1.1", "ip name-server 9.9.9.9"}); err != nil {
		t.Fatal(err)
	}

	got, source, err := networkBridgeCanonicalNativeResolverTarget()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"ip name-server 77.88.8.8", "ip name-server 77.88.8.1"}
	if source != "yandex-basic" || !networkBridgeResolverSelectionsEqual(got, want) {
		t.Fatalf("historical snapshot became target: target=%v source=%q", got, source)
	}
}

func TestNativeResolverPlanTreatsInheritedActiveResolversAsRealDelta(t *testing.T) {
	input := canonicalNativePlanFixture()
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
