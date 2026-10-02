package main

import (
	"reflect"
	"strings"
	"testing"
)

func traceTestRouting() map[string]any {
	return map[string]any{
		"routing": map[string]any{
			"domainStrategy": "AsIs",
			"rules": []any{
				map[string]any{"type": "field", "inboundTag": []any{"dns-vless"}, "outboundTag": "vless-reality"},
				map[string]any{"type": "field", "inboundTag": []any{"dns-direct"}, "outboundTag": "direct"},
				map[string]any{"type": "field", "port": float64(53), "outboundTag": "dns-out"},
				map[string]any{"type": "field", "domain": []any{"domain:direct.example"}, "outboundTag": "direct"},
				map[string]any{"type": "field", "domain": []any{"domain:vpn.example"}, "outboundTag": "vless-reality"},
			},
		},
	}
}

func traceTestDNS() map[string]any {
	expected, err := expectedFreeNetManagedSplitDNS(traceTestRouting())
	if err != nil {
		panic(err)
	}
	return expected
}

func traceTestModernDNS() map[string]any {
	expected, err := expectedFreeNetManagedSplitDNSWithResolvers(traceTestRouting(), settingsDNSYandexDoH, settingsDNSGoogleDoH)
	if err != nil {
		panic(err)
	}
	return expected
}

func TestManagedSplitMirrorAcceptsSupportedCurrentDoHProviders(t *testing.T) {
	current := traceTestModernDNS()
	expected, err := expectedFreeNetManagedSplitDNSForCurrent(traceTestRouting(), current)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(expected, current) {
		t.Fatalf("managed modern DNS mirror mismatch: expected=%v current=%v", expected, current)
	}
}

func TestManagedSplitMirrorRejectsUnknownCurrentResolver(t *testing.T) {
	current := traceTestModernDNS()
	dns := current["dns"].(map[string]any)
	servers := dns["servers"].([]any)
	for _, raw := range servers {
		server := raw.(map[string]any)
		if legacyNativeString(server["tag"]) == "dns-direct" {
			server["address"] = "https://resolver.invalid/dns-query"
		}
	}
	if _, err := expectedFreeNetManagedSplitDNSForCurrent(traceTestRouting(), current); err == nil {
		t.Fatal("unknown resolver must fail closed")
	}
}

func TestNormalizeDNSPathTraceHost(t *testing.T) {
	got, err := normalizeDNSPathTraceHost(" Ipleak.NET. ")
	if err != nil || got != "ipleak.net" {
		t.Fatalf("got=%q err=%v", got, err)
	}
	for _, bad := range []string{"", "localhost", "https://ipleak.net", "192.0.2.1", "bad_name.example", "-bad.example"} {
		if _, err := normalizeDNSPathTraceHost(bad); err == nil {
			t.Fatalf("expected invalid hostname: %q", bad)
		}
	}
}

func TestBuildReadOnlyTraceRoutingDropsLiveBalancerMetadata(t *testing.T) {
	routing := traceTestRouting()
	routingObj := routing["routing"].(map[string]any)
	routingObj["balancers"] = []any{
		map[string]any{
			"tag":      "best",
			"selector": []any{"vless"},
			"strategy": map[string]any{"type": "leastPing"},
		},
	}
	routingObj["domainMatcher"] = "hybrid"

	minimal, err := buildReadOnlyTraceRouting(routing)
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := minimal["balancers"]; exists {
		t.Fatal("trace routing must not copy live balancers")
	}
	if _, exists := minimal["domainMatcher"]; exists {
		t.Fatal("trace routing must not copy unsupported live routing metadata")
	}
	if got := legacyNativeString(minimal["domainStrategy"]); got != "AsIs" {
		t.Fatalf("domainStrategy=%q", got)
	}
	rules, ok := minimal["rules"].([]any)
	if !ok || len(rules) == 0 {
		t.Fatal("ordered routing rules missing")
	}
}

func TestBuildReadOnlyTraceRoutingRejectsBalancerRule(t *testing.T) {
	routing := traceTestRouting()
	routingObj := routing["routing"].(map[string]any)
	rules := routingObj["rules"].([]any)
	rules = append([]any{
		map[string]any{"type": "field", "domain": []any{"domain:balanced.example"}, "balancerTag": "best"},
	}, rules...)
	routingObj["rules"] = rules
	if _, err := buildReadOnlyTraceRouting(routing); err == nil || !strings.Contains(err.Error(), "balancerTag") {
		t.Fatalf("expected fail-closed balancer STOP, got %v", err)
	}
}

func TestSanitizeDNSPathTraceValidationDetail(t *testing.T) {
	raw := "failed\n  to load /tmp/freenet-dns-trace-123/trace.json\r\nreason"
	got := sanitizeDNSPathTraceValidationDetail(raw, "/tmp/freenet-dns-trace-123")
	if strings.Contains(got, "/tmp/freenet-dns-trace-123") {
		t.Fatalf("temporary path leaked: %q", got)
	}
	if got != "failed to load <trace>/trace.json reason" {
		t.Fatalf("unexpected sanitized detail: %q", got)
	}

	long := strings.Repeat("x", 600)
	if got := sanitizeDNSPathTraceValidationDetail(long, ""); len(got) > 483 || !strings.HasSuffix(got, "...") {
		t.Fatalf("validation detail was not bounded: len=%d", len(got))
	}
}

func TestBuildReadOnlyXrayTraceConfigHasNoLiveCredentials(t *testing.T) {
	cfg, err := buildReadOnlyXrayTraceConfig(18080, 18081, traceTestDNS(), traceTestRouting())
	if err != nil {
		t.Fatal(err)
	}
	outbounds, ok := cfg["outbounds"].([]any)
	if !ok || len(outbounds) == 0 {
		t.Fatal("trace outbounds missing")
	}
	for _, raw := range outbounds {
		ob := raw.(map[string]any)
		protocol := legacyNativeString(ob["protocol"])
		if protocol != "blackhole" && protocol != "freedom" {
			t.Fatalf("trace copied a live outbound protocol: %q", protocol)
		}
		if _, exists := ob["streamSettings"]; exists {
			t.Fatal("trace must not copy live stream credentials")
		}
	}
}

func TestParseDNSPathTraceVPNParity(t *testing.T) {
	logs := strings.Join([]string{
		"[Debug] app/dispatcher: taking detour [vless-reality] for [tcp:vpn.example:80]",
		"[Debug] app/dns: domain vpn.example matches following rules: [domain:vpn.example(DNS idx:1)]",
		"[Debug] app/dns: domain vpn.example will use DNS in order: [DOH//8.8.8.8]",
		"[Debug] app/dispatcher: taking detour [vless-reality] for [tcp:8.8.8.8:443]",
		"[Debug] app/dispatcher: taking detour [freenet-trace-resolve] for [tcp:vpn.example:80]",
	}, "\n")
	trace, err := parseDNSPathTrace("vpn.example", logs, traceTestDNS(), traceTestRouting())
	if err != nil {
		t.Fatal(err)
	}
	if trace.PayloadAction != "VPN" || trace.PayloadOutbound != "vless-reality" || trace.DNSSelector != "dns-vless" || trace.DNSExpectedOutbound != "vless-reality" || trace.DNSObservedOutbound != "vless-reality" || !trace.PolicyParity {
		t.Fatalf("unexpected trace: %+v", trace)
	}
}

func TestParseDNSPathTraceDirectParity(t *testing.T) {
	logs := strings.Join([]string{
		"[Debug] app/dispatcher: taking detour [direct] for [tcp:direct.example:80]",
		"[Debug] app/dns: domain direct.example matches following rules: [domain:direct.example(DNS idx:0)]",
		"[Debug] app/dns: domain direct.example will use DNS in order: [UDP:77.88.8.8:53]",
		"[Debug] app/dispatcher: taking detour [direct] for [udp:77.88.8.8:53]",
	}, "\n")
	trace, err := parseDNSPathTrace("direct.example", logs, traceTestDNS(), traceTestRouting())
	if err != nil {
		t.Fatal(err)
	}
	if trace.PayloadAction != "DIRECT" || trace.DNSSelector != "dns-direct" || trace.DNSObservedOutbound != "direct" || !trace.PolicyParity {
		t.Fatalf("unexpected trace: %+v", trace)
	}
}

func TestParseDNSPathTraceDetectsSelectorMismatch(t *testing.T) {
	logs := strings.Join([]string{
		"[Debug] app/dispatcher: taking detour [vless-reality] for [tcp:vpn.example:80]",
		"[Debug] app/dns: domain vpn.example will use DNS in order: [UDP:77.88.8.8:53]",
		"[Debug] app/dispatcher: taking detour [direct] for [udp:77.88.8.8:53]",
	}, "\n")
	trace, err := parseDNSPathTrace("vpn.example", logs, traceTestDNS(), traceTestRouting())
	if err != nil {
		t.Fatal(err)
	}
	if trace.PolicyParity || trace.DNSSelector != "dns-direct" || trace.PayloadAction != "VPN" {
		t.Fatalf("mismatch was not detected: %+v", trace)
	}
}

func TestParseDNSPathTraceDirectParityWithSupportedDoHProvider(t *testing.T) {
	logs := strings.Join([]string{
		"[Debug] app/dispatcher: taking detour [direct] for [tcp:direct.example:80]",
		"[Debug] app/dns: domain direct.example matches following rules: [domain:direct.example(DNS idx:0)]",
		"[Debug] app/dns: domain direct.example will use DNS in order: [DOH//dns.yandex.ru]",
		"[Debug] app/dispatcher: taking detour [direct] for [tcp:dns.yandex.ru:443]",
	}, "\n")
	trace, err := parseDNSPathTrace("direct.example", logs, traceTestModernDNS(), traceTestRouting())
	if err != nil {
		t.Fatal(err)
	}
	if trace.PayloadAction != "DIRECT" || trace.DNSSelector != "dns-direct" || trace.DNSUpstream != settingsDNSYandexDoH || trace.DNSObservedOutbound != "direct" || !trace.PolicyParity {
		t.Fatalf("unexpected modern-provider trace: %+v", trace)
	}
}
