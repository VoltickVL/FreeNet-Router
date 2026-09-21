package main

import (
	"strings"
	"testing"
)

func TestCanonicalRoutingV2UsesRowBoardPolish(t *testing.T) {
	script := canonicalRoutingV2Script()
	checks := []string{
		"routingV2RowPolishStyles",
		".rv4-board-grid{grid-template-columns:1fr!important",
		".rv4-board{display:grid!important;grid-template-columns:minmax(250px,320px) minmax(0,1fr)!important",
		".rv4-board-title-line strong{white-space:normal!important;overflow:visible!important;text-overflow:clip!important",
		".rv4-board-head{border-right:0!important;border-bottom:1px solid #203650!important",
	}
	for _, want := range checks {
		if !strings.Contains(script, want) {
			t.Fatalf("canonical routing script missing row-board polish %q", want)
		}
	}

	legacyColumns := ".rv4-board-grid{display:grid;grid-template-columns:minmax(0,1.55fr) minmax(270px,1fr) minmax(240px,.85fr)"
	legacyIndex := strings.LastIndex(script, legacyColumns)
	polishIndex := strings.LastIndex(script, "routingV2RowPolishStyles")
	if legacyIndex < 0 {
		t.Fatalf("legacy Routing v2 column grid was not found; row-board override may no longer target the delivered surface")
	}
	if polishIndex <= legacyIndex {
		t.Fatalf("row-board polish must be appended after legacy column CSS so it wins in production delivery")
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
