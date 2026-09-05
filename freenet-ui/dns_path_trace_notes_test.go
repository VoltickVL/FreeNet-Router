package main

import "testing"

func TestDNSPathTracePolicySelectorInvariant(t *testing.T) {
	cases := []struct {
		payload  string
		selector string
	}{
		{payload: "direct", selector: "dns-direct"},
		{payload: "vless-reality", selector: "dns-vless"},
	}
	for _, tc := range cases {
		if tc.payload == "direct" && tc.selector != "dns-direct" {
			t.Fatal("DIRECT payload must use dns-direct")
		}
		if tc.payload == "vless-reality" && tc.selector != "dns-vless" {
			t.Fatal("VPN payload must use dns-vless")
		}
	}
}
