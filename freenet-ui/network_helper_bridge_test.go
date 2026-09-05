package main

import (
	"os"
	"strings"
	"testing"
)

func bridgePlanFixture(effective, override, engine, owner, routing string) string {
	return strings.Join([]string{
		"ISP_ID=vladlink",
		"DNS_MODE=" + effective,
		"EFFECTIVE_DNS_MODE=" + effective,
		"SUPPORTED=yes",
		"PROXY_DNS=off",
		"NDM_DNS_OVERRIDE=" + override,
		"NDM_FILTER_ENGINE=" + engine,
		"NDM_DNS_INTERCEPT=off",
		"NDM_DNS_ASSIGNMENTS=none",
		"PORT53_OWNER=" + owner,
		"XRAY_DNS_INBOUND_COUNT=1",
		"XRAY_RUNNING=yes",
		"XRAY_GID=11111",
		"DNS_ROUTING_MODE=" + routing,
		"DNS_OUT=yes",
		"VLESS_PROFILE=yes",
		"EXPECTED_DELTA=base",
		"MUTATION=NONE",
		"",
	}, "\n")
}

func TestBridgePlanMarksSplitWithoutLocalResolverAsRepairNeeded(t *testing.T) {
	input := bridgePlanFixture("xkeen", "on", "opkg", "xray", "split")
	got := augmentNetworkBridgePlan(input, "xkeen", "192.168.50.1", false)
	values := parseNetworkBridgeValues(got)
	if values["DNS_ROUTING_MODE"] != "split-local-upstream-missing" {
		t.Fatalf("routing marker=%q", values["DNS_ROUTING_MODE"])
	}
	if !strings.Contains(values["EXPECTED_DELTA"], "local Xray 192.168.50.1:53") {
		t.Fatalf("expected delta does not describe local resolver: %q", values["EXPECTED_DELTA"])
	}
}

func TestBridgePlanKeepsHealthySplitActiveWhenLocalResolverExists(t *testing.T) {
	input := bridgePlanFixture("xkeen", "on", "opkg", "xray", "split")
	got := augmentNetworkBridgePlan(input, "xkeen", "192.168.50.1", true)
	values := parseNetworkBridgeValues(got)
	if values["DNS_ROUTING_MODE"] != "split" {
		t.Fatalf("routing marker=%q", values["DNS_ROUTING_MODE"])
	}
	if values["LOCAL_XRAY_DNS_UPSTREAM"] != "present" {
		t.Fatalf("local upstream=%q", values["LOCAL_XRAY_DNS_UPSTREAM"])
	}
}

func TestBridgePlanMarksNativeSelfReferenceAsRepairNeeded(t *testing.T) {
	input := bridgePlanFixture("firmware", "off", "public", "ndnproxy", "native")
	got := augmentNetworkBridgePlan(input, "firmware", "192.168.50.1", true)
	values := parseNetworkBridgeValues(got)
	if values["DNS_ROUTING_MODE"] != "native-local-upstream-present" {
		t.Fatalf("routing marker=%q", values["DNS_ROUTING_MODE"])
	}
}

func TestBridgeRecognizesOnlyRouterLocalNameServerLines(t *testing.T) {
	config := strings.Join([]string{
		"ip name-server 77.88.8.8",
		"ip name-server 192.168.50.1:53",
		"ip name-server 192.168.50.1 \"\" on Vladlink",
		"ip name-server 192.168.5.1",
	}, "\n")
	got := networkBridgeLocalPointerLines(config, "192.168.50.1")
	if len(got) != 2 {
		t.Fatalf("local pointer lines=%v", got)
	}
}

func TestBridgeNativeResolverIgnoresSplitOwnedLocalPointer(t *testing.T) {
	config := "ip name-server 192.168.50.1:53\n"
	if networkBridgeHasNativeResolverDefinition(config, "192.168.50.1") {
		t.Fatal("Split-owned local pointer must not count as a native resolver")
	}
}

func TestBridgeNativeResolverRecognizesPreservedNativeDefinitions(t *testing.T) {
	cases := []string{
		"ip name-server 77.88.8.8\n",
		"dns-proxy\n    tls upstream 77.88.8.8 853 sni common.dot.dns.yandex.net\n!\n",
		"dns-proxy https upstream https://example.invalid/dns-query\n",
		"interface Vladlink\n    ip dhcp client dns-routes\n!\n",
	}
	for _, config := range cases {
		if !networkBridgeHasNativeResolverDefinition(config, "192.168.50.1") {
			t.Fatalf("native resolver definition not recognized: %q", config)
		}
	}
}

func TestBridgeNativeResolverPlanMakesFallbackExplicit(t *testing.T) {
	input := augmentNetworkBridgePlan(bridgePlanFixture("firmware", "on", "opkg", "xray", "split"), "firmware", "192.168.50.1", true)
	got := augmentNetworkBridgeNativeResolverPlan(input, "yandex-basic-fallback-needed")
	values := parseNetworkBridgeValues(got)
	if values["NATIVE_RESOLVER_SELECTION"] != "yandex-basic-fallback-needed" {
		t.Fatalf("native resolver status=%q", values["NATIVE_RESOLVER_SELECTION"])
	}
	if !strings.Contains(values["EXPECTED_DELTA"], "77.88.8.8/77.88.8.1") {
		t.Fatalf("fallback is absent from expected delta: %q", values["EXPECTED_DELTA"])
	}
}

func TestBridgeRuntimeClassification(t *testing.T) {
	values := parseNetworkBridgeValues(bridgePlanFixture("xkeen", "on", "opkg", "xray", "split"))
	if !networkBridgeRuntimeSplit(values) {
		t.Fatal("expected split runtime")
	}
	values = parseNetworkBridgeValues(bridgePlanFixture("firmware", "off", "public", "ndnproxy", "native"))
	if !networkBridgeRuntimeNative(values) {
		t.Fatal("expected native runtime")
	}
}

func TestBridgeFirmwareDraftReplacesOnlyDNSMode(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/freenet.conf"
	original := "UI_PORT=1001\nISP_ID=vladlink\nDNS_MODE=xkeen\nAUTO_XKEEN_GEODATA=yes\n"
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	draft, err := networkBridgeFirmwareDraft(path)
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(draft)
	data, err := os.ReadFile(draft)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, "DNS_MODE=firmware") || strings.Contains(text, "DNS_MODE=xkeen") {
		t.Fatalf("draft=%q", text)
	}
	for _, preserved := range []string{"ISP_ID=vladlink", "AUTO_XKEEN_GEODATA=yes"} {
		if !strings.Contains(text, preserved) {
			t.Fatalf("draft lost %q: %q", preserved, text)
		}
	}
}

func TestBridgeDoesNotPersistForwardPointerMutationBeforeCoreAcceptance(t *testing.T) {
	data, err := os.ReadFile("network_helper_bridge.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)

	splitStart := strings.Index(text, "func applyNetworkBridgeSplit(")
	if splitStart < 0 {
		t.Fatal("applyNetworkBridgeSplit not found")
	}
	splitCore := strings.Index(text[splitStart:], `runNetworkBridgeCore(configPath, "apply")`)
	if splitCore < 0 {
		t.Fatal("Split core apply call not found")
	}
	if strings.Contains(text[splitStart:splitStart+splitCore], "networkBridgeSave()") {
		t.Fatal("Split forward pointer staging persists before core acceptance")
	}

	nativeStart := strings.Index(text, "func applyNetworkBridgeNative(")
	if nativeStart < 0 {
		t.Fatal("applyNetworkBridgeNative not found")
	}
	nativeRecovery := strings.Index(text[nativeStart:], "if networkBridgeStrictRuntimeSplit(prePlan) {")
	if nativeRecovery < 0 {
		t.Fatal("native recovery boundary not found")
	}
	if strings.Contains(text[nativeStart:nativeStart+nativeRecovery], "networkBridgeSave()") {
		t.Fatal("native forward resolver staging persists before recovery/core acceptance")
	}
	if !strings.Contains(text[nativeStart+nativeRecovery:], "networkBridgeRollbackNativeResolverStage") {
		t.Fatal("native resolver rollback contract disappeared")
	}
}

func TestBridgeStagesNativeResolverBeforeRemovingSplitPointer(t *testing.T) {
	data, err := os.ReadFile("network_helper_bridge.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	start := strings.Index(text, "func applyNetworkBridgeNative(")
	if start < 0 {
		t.Fatal("applyNetworkBridgeNative not found")
	}
	body := text[start:]
	stage := strings.Index(body, "networkBridgeEnsureNativeResolverReady(lanIP)")
	remove := strings.Index(body, "networkBridgeRemoveLocalPointer(lanIP)")
	core := strings.Index(body, `runNetworkBridgeCore(configPath, "apply")`)
	if stage < 0 || remove < 0 || core < 0 || !(stage < remove && remove < core) {
		t.Fatalf("native resolver ordering invalid: stage=%d remove=%d core=%d", stage, remove, core)
	}
}
