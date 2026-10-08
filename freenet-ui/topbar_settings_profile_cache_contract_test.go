package main

import (
	"strings"
	"testing"
)

func TestTopbarSettingsProfileCacheContract(t *testing.T) {
	ux := string(vpnSelectorReconcileAsset)
	for _, required := range []string{
		"freenet-extra-profiles-last-good-v1",
		"renderExtraProfiles",
		"profiles_stale",
		"Используется последний успешный список Extra-профилей",
		"currentProfilesWithCache",
		"catalogStale",
		"freenetProfileCatalogState",
		"freenetPublishMeasuredProfileCatalog",
		".fn-xray-topbar",
		"sidebar>.brand",
		"freenet:settings-v3-updated",
		"fn-sub-next",
		"fn3-grid{align-items:start!important",
		"fn3-left{display:grid!important",
		"fn3-left>.fn3-card{flex:none!important",
	} {
		if !strings.Contains(ux, required) {
			t.Fatalf("topbar/settings profile cache contract missing %q", required)
		}
	}
	for _, forbidden := range []string{
		"/api/network-profile/apply",
		"method: 'POST'",
		"method:\"POST\"",
		"setTimeout(",
		"fnVpnPickerPopover",
		"fnVpnPickerHost",
		"fnVpnPickerToggle",
		"freenetIssue601Styles",
		"fn3-left>.fn3-card{flex:1 1 auto!important",
		"min-height:100%!important",
		"fn3-extra-grid{align-items:stretch!important",
	} {
		if strings.Contains(ux, forbidden) {
			t.Fatalf("topbar/settings cache patch must stay data/presentation-only without legacy picker: found %q", forbidden)
		}
	}
}
