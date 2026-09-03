package main

import (
	"strings"
	"testing"
)

func TestDashboardReservesExtraRegionBeforeAsyncProfiles(t *testing.T) {
	indexData, err := webFS.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	ui := string(indexData)
	for _, required := range []string{
		`id="quickActionsSection"`,
		`id="profilesList"`,
		`id="selectedProfileCard"`,
	} {
		if !strings.Contains(ui, required) {
			t.Fatalf("dashboard structure missing %q", required)
		}
	}

	assetData, err := webFS.ReadFile("web/self-update.js")
	if err != nil {
		t.Fatal(err)
	}
	asset := string(assetData)
	for _, required := range []string{
		"mountDashboardStability",
		"#profilesList.profiles{display:block}",
		"#profilesList{min-height:178px}",
		"#selectedProfileCard{min-height:78px}",
		"#quickActionsSection{min-height:472px}",
		"@media(max-width:980px)",
		"Текущий VPN и список профилей появятся здесь без изменения размеров Dashboard.",
	} {
		if !strings.Contains(asset, required) {
			t.Fatalf("dashboard stability contract missing %q", required)
		}
	}
}
