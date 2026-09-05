package main

import (
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
