package main

import (
	"strings"
	"testing"
)

func TestVPNSelectorModalSearchContract(t *testing.T) {
	compat := string(vpnSelectorReconcileAsset)
	for _, required := range []string{
		"renderProfileOptionsHotfix",
		"германия",
		"герман",
		"Extra-профили не загружены",
		"По запросу",
		"selectProviderProfile(profile)",
	} {
		if !strings.Contains(compat, required) {
			t.Fatalf("hidden exact-engine search compatibility missing %q", required)
		}
	}
	for _, forbidden := range []string{
		"/api/network-profile/apply",
		"method: 'POST'",
		"fnVpnPickerPopover",
		"fnVpnPickerHost",
		"fnVpnPickerToggle",
		"fnVpnPickerTrigger",
		"freenetIssue601Styles",
		"fn-topbar-vpn-picker{",
	} {
		if strings.Contains(compat, forbidden) {
			t.Fatalf("compatibility layer must not own visible picker surface: found %q", forbidden)
		}
	}

	v2Data, err := webFS.ReadFile("web/vpn-picker-v2.js")
	if err != nil {
		t.Fatal(err)
	}
	v2 := string(v2Data)
	for _, required := range []string{
		"fnVpnPickerV2Panel",
		"fnVpnPickerV2Search",
		"fnVpnPickerV2Results",
		"selectProviderProfile(profile)",
		"refreshStaleCatalogOnOpen",
		"xray-vpn-dns-freenet",
	} {
		if !strings.Contains(v2, required) {
			t.Fatalf("visible picker v2 search/modal contract missing %q", required)
		}
	}
}
