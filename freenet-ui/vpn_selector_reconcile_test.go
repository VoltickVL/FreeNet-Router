package main

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestVPNSelectorReconcileContract(t *testing.T) {
	data, err := webFS.ReadFile("web/vpn-selector-reconcile.js")
	if err != nil {
		t.Fatal(err)
	}
	ux := string(data)
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

func TestCanonicalIndexLoadsVersionedVPNSelectorReconcile(t *testing.T) {
	a := &app{}
	req := httptest.NewRequest("GET", "http://router.local/", nil)
	rr := httptest.NewRecorder()
	registerTestMux := func() *httptest.ResponseRecorder { return rr }
	_ = registerTestMux
	a.handleIndex(rr, req)
	if rr.Code != 200 {
		t.Fatalf("base index status=%d", rr.Code)
	}
	body, err := canonicalizeControlCenterIndex(rr.Body.String())
	if err != nil {
		t.Fatal(err)
	}
	want := `/vpn-selector-reconcile.js?v=v` + version
	if !strings.Contains(body, want) {
		t.Fatalf("canonical index missing %q", want)
	}
}

func TestVersionedVPNSelectorReconcileAssetIsServed(t *testing.T) {
	a := &app{}
	req := httptest.NewRequest("GET", "http://router.local/vpn-selector-reconcile.js?v=v"+version, nil)
	rr := httptest.NewRecorder()
	a.handleIndex(rr, req)
	if rr.Code != 200 {
		t.Fatalf("asset status=%d", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); !strings.Contains(ct, "application/javascript") {
		t.Fatalf("asset content-type=%q", ct)
	}
	if !strings.Contains(rr.Body.String(), "reconcilePendingManualSwitch") {
		t.Fatal("served asset is not the delayed VPN reconciliation layer")
	}
}
