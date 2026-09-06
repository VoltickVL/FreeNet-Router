package main

import "testing"

func TestCompilePolicyDomainAndGeoSiteSharePayloadAndDNS(t *testing.T) {
	compiled, err := CompilePolicy([]PolicyRule{
		{Selector: PolicySelector{Kind: "domain", Value: "Plati.Market."}, Action: "direct"},
		{Selector: PolicySelector{Kind: "geosite", Value: "YouTube"}, Action: "vpn"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(compiled.Rules) != 2 || len(compiled.Payload) != 2 || len(compiled.DNS) != 2 {
		t.Fatalf("unexpected sizes rules=%d payload=%d dns=%d", len(compiled.Rules), len(compiled.Payload), len(compiled.DNS))
	}
	if got := compiled.Rules[0]; got.Order != 0 || got.Selector.Value != "plati.market" || got.Action != PolicyActionDirect || got.PayloadOutbound != "direct" || got.DNSLeg != "dns-direct" {
		t.Fatalf("unexpected first rule: %+v", got)
	}
	if got := compiled.Rules[1]; got.Order != 1 || got.Selector.Value != "youtube" || got.Action != PolicyActionVPN || got.PayloadOutbound != "vless-reality" || got.DNSLeg != "dns-vless" {
		t.Fatalf("unexpected second rule: %+v", got)
	}
}

func TestCompilePolicyIPKindsNeverEnterDNSLeg(t *testing.T) {
	compiled, err := CompilePolicy([]PolicyRule{
		{Selector: PolicySelector{Kind: "ip", Value: "1.1.1.1"}, Action: "DIRECT"},
		{Selector: PolicySelector{Kind: "cidr", Value: "10.10.10.42/24"}, Action: "VPN"},
		{Selector: PolicySelector{Kind: "geoip", Value: "RU"}, Action: "BLOCK"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(compiled.Payload) != 3 {
		t.Fatalf("payload size=%d want 3", len(compiled.Payload))
	}
	if len(compiled.DNS) != 0 {
		t.Fatalf("IP selectors leaked into DNS leg: %+v", compiled.DNS)
	}
	if compiled.Rules[1].Selector.Value != "10.10.10.0/24" {
		t.Fatalf("CIDR not canonicalized: %+v", compiled.Rules[1])
	}
	for _, rule := range compiled.Rules {
		if rule.DNSLeg != "" {
			t.Fatalf("IP-based rule has DNS leg: %+v", rule)
		}
	}
}

func TestCompilePolicyPreservesFirstMatchOrder(t *testing.T) {
	compiled, err := CompilePolicy([]PolicyRule{
		{Selector: PolicySelector{Kind: "domain", Value: "api.example.com"}, Action: "VPN"},
		{Selector: PolicySelector{Kind: "geosite", Value: "example"}, Action: "DIRECT"},
		{Selector: PolicySelector{Kind: "cidr", Value: "192.0.2.0/24"}, Action: "BLOCK"},
	})
	if err != nil {
		t.Fatal(err)
	}
	for i, rule := range compiled.Rules {
		if rule.Order != i {
			t.Fatalf("rule %d order=%d", i, rule.Order)
		}
		if compiled.Payload[i].Order != i {
			t.Fatalf("payload %d order=%d", i, compiled.Payload[i].Order)
		}
	}
	if len(compiled.DNS) != 2 || compiled.DNS[0].Order != 0 || compiled.DNS[1].Order != 1 {
		t.Fatalf("DNS order changed: %+v", compiled.DNS)
	}
}

func TestCompilePolicyRejectsInvalidAndDuplicateRules(t *testing.T) {
	cases := []struct {
		name  string
		rules []PolicyRule
	}{
		{name: "unknown kind", rules: []PolicyRule{{Selector: PolicySelector{Kind: "service", Value: "youtube"}, Action: "DIRECT"}}},
		{name: "empty selector", rules: []PolicyRule{{Selector: PolicySelector{Kind: "domain", Value: "  "}, Action: "DIRECT"}}},
		{name: "bad domain", rules: []PolicyRule{{Selector: PolicySelector{Kind: "domain", Value: "not-a-domain"}, Action: "DIRECT"}}},
		{name: "bad ip", rules: []PolicyRule{{Selector: PolicySelector{Kind: "ip", Value: "999.1.1.1"}, Action: "VPN"}}},
		{name: "bad cidr", rules: []PolicyRule{{Selector: PolicySelector{Kind: "cidr", Value: "10.0.0.0/99"}, Action: "VPN"}}},
		{name: "unknown action", rules: []PolicyRule{{Selector: PolicySelector{Kind: "domain", Value: "example.com"}, Action: "PROXY"}}},
		{name: "duplicate after normalization", rules: []PolicyRule{
			{Selector: PolicySelector{Kind: "domain", Value: "Example.COM"}, Action: "DIRECT"},
			{Selector: PolicySelector{Kind: "DOMAIN", Value: "example.com."}, Action: "VPN"},
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := CompilePolicy(tc.rules); err == nil {
				t.Fatalf("expected error for %+v", tc.rules)
			}
		})
	}
}

func TestCompilePolicyBlockIsExplicitInBothDomainLegs(t *testing.T) {
	compiled, err := CompilePolicy([]PolicyRule{{Selector: PolicySelector{Kind: "domain", Value: "ads.example"}, Action: "BLOCK"}})
	if err != nil {
		t.Fatal(err)
	}
	if compiled.Rules[0].PayloadOutbound != "block" || compiled.Rules[0].DNSLeg != "block" {
		t.Fatalf("block mapping=%+v", compiled.Rules[0])
	}
}
