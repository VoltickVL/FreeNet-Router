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
		`Краткая потеря связи/502 во время перезапуска ожидаема`,
		`for (let i = 0; i < 90; i++)`,
		`[hidden]{display:none!important}`,
		`.auth-wrap{position:fixed!important`,
	} {
		if !strings.Contains(ux, required) {
			t.Fatalf("modal/reconnect contract missing %q", required)
		}
	}
}
