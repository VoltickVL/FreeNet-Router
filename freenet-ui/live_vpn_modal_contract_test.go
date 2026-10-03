package main

import (
	"strings"
	"testing"
)

func TestOverviewOwnsRoutineVPNFlow(t *testing.T) {
	indexData, err := webFS.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	uxData, err := webFS.ReadFile("web/self-update.js")
	if err != nil {
		t.Fatal(err)
	}
	index := string(indexData)
	ux := string(uxData)

	for _, required := range []string{
		`vpnNav.remove()`,
		`vpnPage.remove()`,
		`delete pageLabels.vpn`,
		`if (location.hash === '#vpn') setPage('overview')`,
		`selectProviderProfile = async function(p)`,
		`applyExactProfileFromOverview`,
		`waitForVPNState`,
		`renderProfileOptions = function()`,
		`makeCountryMarker(profileCountryCode(p))`,
		`profile-option-endpoint{font-size:12.5px`,
	} {
		if !strings.Contains(ux, required) {
			t.Fatalf("overview VPN UX contract missing %q", required)
		}
	}

	if !strings.Contains(index, `data-page-view="overview"`) {
		t.Fatal("overview page missing")
	}
}

func TestUnifiedModalAndReconnectUX(t *testing.T) {
	uxData, err := webFS.ReadFile("web/self-update.js")
	if err != nil {
		t.Fatal(err)
	}
	ux := string(uxData)
	for _, required := range []string{
		`fn-modal-root`,
		`openModal({`,
		`modalProgress(`,
		`modalResult(`,
		`Подготавливаем обновление и создаём резервную копию. После перезапуска FreeNet автоматически проверит результат.`,
		`for (let i = 0; i < 90; i++)`,
		`[hidden]{display:none!important}`,
		`.auth-wrap{position:fixed!important`,
	} {
		if !strings.Contains(ux, required) {
			t.Fatalf("modal/reconnect contract missing %q", required)
		}
	}
}


func TestVPNPickerV2OwnsTopbarAndBodyPanel(t *testing.T) {
	data, err := webFS.ReadFile("web/vpn-picker-v2.js")
	if err != nil {
		t.Fatal(err)
	}
	ux := string(data)
	for _, required := range []string{
		`fnVpnPickerV2Country`,
		`document.body.appendChild(panel)`,
		`xray-vpn-dns-freenet`,
		`[xray,host,dns,freenet]`,
		`refreshStaleCatalogOnOpen`,
		`selectProviderProfile(profile)`,
		`fnVpnPickerV2Resize`,
	} {
		if !strings.Contains(ux, required) {
			t.Fatalf("VPN picker v2 topbar/body contract missing %q", required)
		}
	}
}

func TestVPNLegacyVisualOwnersAreRetired(t *testing.T) {
	for _, path := range []string{"web/operation-coordinator.js", "web/topbar-settings-profile-cache.js", "web/accepted-ux.js"} {
		data, err := webFS.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		ux := string(data)
		for _, forbidden := range []string{
			"fnVpnPickerPopover",
			"fnVpnPickerHost",
			"fnVpnPickerToggle",
			"freenetIssue609VpnPolishStyles",
			"freenetIssue601Styles",
		} {
			if strings.Contains(ux, forbidden) {
				t.Fatalf("%s still contains legacy picker visual owner %q", path, forbidden)
			}
		}
	}
}
