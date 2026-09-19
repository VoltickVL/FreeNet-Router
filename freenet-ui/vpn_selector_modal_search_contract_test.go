package main

import (
	"strings"
	"testing"
)

func TestVPNSelectorModalSearchContract(t *testing.T) {
	ux := string(vpnSelectorReconcileAsset)
	for _, required := range []string{
		"freenetVpnSelectorHotfix",
		"renderProfileOptionsHotfix",
		"германия",
		"герман",
		"Extra-профили не загружены",
		"По запросу",
		"#fnVpnPickerTrigger",
		"#fnVpnPickerBody #profilesMenu",
		".fn-topbar-vpn-picker",
		"selectProviderProfile(profile)",
	} {
		if !strings.Contains(ux, required) {
			t.Fatalf("VPN selector modal/search contract missing %q", required)
		}
	}
	for _, forbidden := range []string{
		"/api/network-profile/apply",
		"method: 'POST'",
		"setTimeout(",
	} {
		if strings.Contains(ux, forbidden) {
			t.Fatalf("VPN selector modal/search patch must stay presentation-only: found %q", forbidden)
		}
	}
}
