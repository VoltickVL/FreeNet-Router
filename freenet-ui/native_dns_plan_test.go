package main

import (
	"strings"
	"testing"
)

func TestParseNetworkPlanMarksNativeKeeneticDNSActive(t *testing.T) {
	out := strings.Join([]string{
		"ISP_ID=rostelecom",
		"DNS_MODE=firmware",
		"EFFECTIVE_DNS_MODE=firmware",
		"SUPPORTED=yes",
		"REASON=нативный DNS Keenetic без VPN-проксирования",
		"PROXY_DNS=off",
		"PORT53_OWNER=ndnproxy",
		"NDM_DNS_OVERRIDE=off",
		"XRAY_GID=11111",
		"DNS_ROUTING_MODE=native",
		"DNS_OUT=no",
		"VLESS_PROFILE=yes",
		"EXPECTED_DELTA=Keenetic native DNS owns :53",
		"EXPECTED_NO_DELTA=no non-DNS routing change",
		"MUTATION=NONE",
	}, "\n")
	plan, err := parseNetworkPlan(out)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Active {
		t.Fatalf("true native Keenetic DNS must be active without dns-out: %+v", plan)
	}
	if plan.DNSOut {
		t.Fatalf("native Keenetic DNS must not report dns-out: %+v", plan)
	}
	if plan.DNSRoutingMode != "native" {
		t.Fatalf("native routing mode not exposed: %+v", plan)
	}
}

func TestParseNetworkPlanRejectsLegacyStandardAsNative(t *testing.T) {
	out := strings.Join([]string{
		"ISP_ID=rostelecom",
		"DNS_MODE=firmware",
		"EFFECTIVE_DNS_MODE=firmware",
		"SUPPORTED=yes",
		"REASON=legacy partial DNS",
		"PROXY_DNS=off",
		"PORT53_OWNER=xray",
		"XRAY_GID=11111",
		"DNS_ROUTING_MODE=standard",
		"DNS_OUT=yes",
		"VLESS_PROFILE=yes",
		"EXPECTED_DELTA=restore native",
		"EXPECTED_NO_DELTA=preserve routing",
		"MUTATION=NONE",
	}, "\n")
	plan, err := parseNetworkPlan(out)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Active {
		t.Fatalf("legacy Xray-backed standard DNS must not be accepted as true native: %+v", plan)
	}
}
