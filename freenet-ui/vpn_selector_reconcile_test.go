package main

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestVPNSelectorReconcileContract(t *testing.T) {
	ux := string(vpnSelectorReconcileAsset)
	if ux == "" {
		t.Fatal("embedded VPN selector reconcile asset is empty")
	}
	for _, required := range []string{
		"Требуется проверка состояния",
		"Связь прервалась",
		"status.endpoint === expectedEndpoint",
		"!status.busy",
		"!status.updater_busy",
		"status.xray_online === true",
		"renderSelectedProfile(null)",
		"exactRow.hidden = true",
		"routine.hidden = false",
		"closeProfileMenu",
		"previousUpdateStatusViews.apply",
	} {
		if !strings.Contains(ux, required) {
			t.Fatalf("VPN selector reconcile contract missing %q", required)
		}
	}
	for _, forbidden := range []string{
		"/api/network-profile/apply",
		"method: 'POST'",
		"setTimeout(",
	} {
		if strings.Contains(ux, forbidden) {
			t.Fatalf("VPN selector reconcile must not retry or time-hide state: found %q", forbidden)
		}
	}
}

func TestCanonicalIndexEmbedsVPNSelectorReconcile(t *testing.T) {
	a := &app{}
	req := httptest.NewRequest("GET", "http://router.local/", nil)
	rr := httptest.NewRecorder()
	a.handleIndex(rr, req)
	if rr.Code != 200 {
		t.Fatalf("base index status=%d", rr.Code)
	}
	body, err := canonicalizeControlCenterIndex(rr.Body.String())
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{
		`<script id="freenetVPNSelectorReconcile">`,
		"reconcilePendingManualSwitch",
		"Требуется проверка состояния",
	} {
		if !strings.Contains(body, required) {
			t.Fatalf("canonical index missing embedded reconcile marker %q", required)
		}
	}
	if strings.Contains(body, `/vpn-selector-reconcile.js?v=`) {
		t.Fatal("canonical reconcile layer must not depend on a separately cached asset request")
	}
}
