package main

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestExactVPNConnectUXIsSingleExplicitAction(t *testing.T) {
	data, err := webFS.ReadFile("web/vpn-ux-fix.js")
	if err != nil {
		t.Fatal(err)
	}
	ux := string(data)
	for _, required := range []string{
		"selectProviderProfile = selectExactProfile",
		".action-row:not(#exactConnectRow)",
		"Подключиться",
		"Сбросить выбор",
		"Обновить текущий VPN-профиль",
		"Сменить сервер",
		"Проверка выполнена автоматически",
		"operation: 'provider'",
		"waitExactState",
		"showExactMode(true)",
		"controls.routine.hidden = enabled",
	} {
		if !strings.Contains(ux, required) {
			t.Fatalf("exact VPN UX contract missing %q", required)
		}
	}
	if strings.Contains(ux, "quick.querySelector('.action-row')") {
		t.Fatal("routine VPN action row must not be rediscovered positionally after exact row is inserted")
	}
	if strings.Contains(ux, "confirm(") || strings.Contains(ux, "openModal(") {
		t.Fatal("routine exact VPN connect must not require an extra confirmation dialog")
	}
}

func TestIndexUsesReleaseVersionedUIAssets(t *testing.T) {
	a := &app{}
	req := httptest.NewRequest("GET", "http://router.local/", nil)
	rr := httptest.NewRecorder()
	a.handleIndex(rr, req)
	if rr.Code != 200 {
		t.Fatalf("index status=%d", rr.Code)
	}
	body := rr.Body.String()
	for _, required := range []string{
		`/self-update.js?v=v` + version,
		`/vpn-ux-fix.js?v=v` + version,
	} {
		if !strings.Contains(body, required) {
			t.Fatalf("versioned UI asset missing %q", required)
		}
	}
}

func TestVersionedVPNFixAssetIsServed(t *testing.T) {
	a := &app{}
	req := httptest.NewRequest("GET", "http://router.local/vpn-ux-fix.js?v=v"+version, nil)
	rr := httptest.NewRecorder()
	a.handleIndex(rr, req)
	if rr.Code != 200 {
		t.Fatalf("asset status=%d", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); !strings.Contains(ct, "application/javascript") {
		t.Fatalf("asset content-type=%q", ct)
	}
	if !strings.Contains(rr.Body.String(), "Подключиться") {
		t.Fatal("served asset is not the exact VPN connect layer")
	}
}
