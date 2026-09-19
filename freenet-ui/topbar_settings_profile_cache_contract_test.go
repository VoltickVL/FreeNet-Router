package main

import (
	"strings"
	"testing"
)

func TestTopbarSettingsProfileCacheContract(t *testing.T) {
	ux := string(vpnSelectorReconcileAsset)
	for _, required := range []string{
		"freenetTopbarSettingsProfileCacheStyles",
		"freenet-extra-profiles-last-good-v1",
		"renderExtraProfiles",
		"profiles_stale",
		"Используется последний успешный список Extra-профилей",
		"currentProfilesWithCache",
		"topbar #bestServerAdvanced.fn-topbar-vpn-picker",
		"height:50px!important",
		"fn3-left>.fn3-card",
		"fn3-backup-actions .btn",
		"height:56px!important",
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
	} {
		if strings.Contains(ux, forbidden) {
			t.Fatalf("topbar/settings cache patch must stay presentation-only: found %q", forbidden)
		}
	}
}
