package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDNSPathTraceResponseHasNoCredentialFields(t *testing.T) {
	data, err := json.Marshal(dnsPathTraceResponse{
		Success:             true,
		Host:                "ipleak.net",
		DNSMode:             "xkeen",
		PayloadAction:       "VPN",
		PayloadOutbound:     "vless-reality",
		DNSSelector:         "dns-vless",
		DNSUpstream:         "https://8.8.8.8/dns-query",
		DNSExpectedOutbound: "vless-reality",
		DNSObservedOutbound: "vless-reality",
		PolicyParity:        true,
		Mutation:            "NONE",
	})
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ToLower(string(data))
	for _, forbidden := range []string{"uuid", "public_key", "shortid", "subscription", "password", "token"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("DNS trace response exposes forbidden credential field %q: %s", forbidden, data)
		}
	}
}
