package main

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestBridgeSplitResolverSelectionExcludesLocalPointer(t *testing.T) {
	config := strings.Join([]string{
		"ip name-server 77.88.8.8",
		"ip name-server 77.88.8.1",
		"ip name-server 192.168.50.1:53",
		"dns-proxy",
		"    tls upstream common.dot.dns.yandex.net",
		"!",
		"interface Vladlink",
		"    ip dhcp client dns-routes",
		"!",
	}, "\n")
	got, err := networkBridgeNativeResolverSelectionLines(config, "192.168.50.1")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"ip name-server 77.88.8.8", "ip name-server 77.88.8.1"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("selection=%v want=%v", got, want)
	}
}

func TestBridgeSplitResolverSelectionUnsupportedSyntaxStops(t *testing.T) {
	_, err := networkBridgeNativeResolverSelectionLines("ip name-server 8.8.8.8 on Vladlink\n", "192.168.50.1")
	if err == nil {
		t.Fatal("unsupported global resolver syntax must fail closed")
	}
}

func TestBridgeSplitResolverPlanMarksParallelNativeSelectionAsRepair(t *testing.T) {
	input := augmentNetworkBridgePlan(bridgePlanFixture("xkeen", "on", "opkg", "xray", "split"), "xkeen", "192.168.50.1", true)
	got := augmentNetworkBridgeSplitResolverPlan(input, 2)
	values := parseNetworkBridgeValues(got)
	if values["DNS_ROUTING_MODE"] != "split-native-resolver-selection-present" {
		t.Fatalf("routing marker=%q", values["DNS_ROUTING_MODE"])
	}
	if values["NATIVE_RESOLVER_SELECTION"] != "detach-for-split:2" {
		t.Fatalf("selection marker=%q", values["NATIVE_RESOLVER_SELECTION"])
	}
	if !strings.Contains(values["EXPECTED_DELTA"], "temporarily detach 2 active native resolver selection") {
		t.Fatalf("expected delta=%q", values["EXPECTED_DELTA"])
	}
}

func TestBridgeNativeResolverSnapshotRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("FREENET_NATIVE_DNS_STATE_DIR", dir)
	want := []string{"ip name-server 77.88.8.8", "ip name-server 77.88.8.1"}
	if err := networkBridgeWriteNativeResolverSelection(want); err != nil {
		t.Fatal(err)
	}
	got, err := networkBridgeLoadNativeResolverSelection()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("snapshot=%v want=%v", got, want)
	}
}

func TestBridgeSplitStagesLocalPointerBeforeDetachingNativeSelection(t *testing.T) {
	data, err := os.ReadFile("network_helper_bridge.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	start := strings.Index(text, "func applyNetworkBridgeSplit(")
	if start < 0 {
		t.Fatal("applyNetworkBridgeSplit not found")
	}
	body := text[start:]
	add := strings.Index(body, "networkBridgeAddLocalPointer(lanIP)")
	remove := strings.Index(body, "networkBridgeRemoveResolverSelectionLines(nativeSelection)")
	if add < 0 || remove < 0 || add >= remove {
		t.Fatalf("Split resolver ordering invalid: add=%d remove=%d", add, remove)
	}
}
