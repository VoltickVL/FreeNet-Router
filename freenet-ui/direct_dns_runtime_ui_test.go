package main

import (
	"strings"
	"testing"
)

func TestDirectDNSRuntimeStatusDoesNotRequireDNSOut(t *testing.T) {
	uxData, err := webFS.ReadFile("web/vpn-ux-fix.js")
	if err != nil {
		t.Fatal(err)
	}
	ux := string(uxData)
	for _, required := range []string{
		`function ensureLegacyVPNStatusNodes()`,
		`ensureLegacyVPNStatusNodes();`,
		`const xrayDNS = s.dns_mode === 'xkeen';`,
		`const dnsHealthy = xrayDNS ? !!s.dns_out_present : true;`,
		`dnsState.textContent = xrayDNS ? (s.dns_out_present ? 'DNS через XKeen/Xray' : 'DNS требует внимания') : 'DNS напрямую';`,
		`topStatus.textContent = 'FreeNet доступен';`,
		`VPN-действия не меняют ISP и DNS.`,
	} {
		if !strings.Contains(ux, required) {
			t.Fatalf("direct-DNS runtime UI contract missing %q", required)
		}
	}
}
