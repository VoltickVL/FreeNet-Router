package main

import (
	"strings"
	"testing"
)

func TestCanonicalRoutingV2UsesRowBoardPolish(t *testing.T) {
	script := canonicalRoutingV2Script()
	checks := []string{
		".rv4-board-grid{grid-template-columns:1fr!important",
		".rv4-board{display:block!important;border-left:4px solid var(--accent)!important;overflow:hidden!important",
		".rv4-board-title-line strong{white-space:normal!important;overflow:visible!important;text-overflow:clip!important",
		".rv4-board-head{min-height:auto!important;border-right:0!important;border-bottom:1px solid #203650!important",
		"collapsed: {DIRECT:true, VPN:true, BLOCK:true}",
		`data-board-toggle="DIRECT" aria-expanded="false"`,
		`data-board-toggle="VPN" aria-expanded="false"`,
		`data-board-toggle="BLOCK" aria-expanded="false"`,
	}
	for _, want := range checks {
		if !strings.Contains(script, want) && !strings.Contains(freenetUnifiedDesignCSS, want) {
			t.Fatalf("canonical routing script missing row-board polish %q", want)
		}
	}

	if strings.Contains(script, "routingV2RowPolishStyles") || strings.Contains(script, "createElement('style')") {
		t.Fatal("Routing must not append a legacy column override stylesheet")
	}

}

func TestRoutingPolicyDirectionMappingsRemainCanonical(t *testing.T) {
	cases := []struct {
		action  PolicyAction
		payload string
		dns     string
	}{
		{PolicyActionDirect, "direct", "dns-direct"},
		{PolicyActionVPN, "vless-reality", "dns-vless"},
		{PolicyActionBlock, "block", "block"},
	}
	for _, tc := range cases {
		if got := payloadOutboundForAction(tc.action); got != tc.payload {
			t.Fatalf("%s payload outbound=%q want %q", tc.action, got, tc.payload)
		}
		if got := dnsLegForAction(tc.action); got != tc.dns {
			t.Fatalf("%s DNS leg=%q want %q", tc.action, got, tc.dns)
		}
	}
}
