package main

import (
	"os"
	"strings"
	"testing"
)

func TestBestServerUIReplacesManualQuickCountries(t *testing.T) {
	data, err := os.ReadFile("web/operation-coordinator.js")
	if err != nil {
		t.Fatal(err)
	}
	js := string(data)
	for _, want := range []string{
		"Лучший VPN",
		"/api/vpn/best",
		"Переключиться на лучший",
		"Проверить заново",
		"countries.remove()",
		"operation: 'provider'",
		"profile_id: recommendation.id",
		"MUTATION: NONE",
		"HTTP-отклик",
		"Мбит/с",
	} {
		if !strings.Contains(js, want) {
			t.Fatalf("Best Server UI contract missing %q", want)
		}
	}
}

func TestBestServerRouteIsRegistered(t *testing.T) {
	data, err := os.ReadFile("geodata_api.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "registerBestServerQualityAPI(mux, a)") {
		t.Fatal("Best Server quality API must be registered at startup")
	}
}

func TestBestServerUIKeepsExactProfileFallback(t *testing.T) {
	index, err := os.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	js, err := os.ReadFile("web/operation-coordinator.js")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(index), `id="profileSearch"`) || !strings.Contains(string(index), `id="profilesTrigger"`) {
		t.Fatal("manual exact profile selector disappeared")
	}
	if !strings.Contains(string(js), "Ручной выбор Extra-профиля") {
		t.Fatal("manual exact selector must be clearly demoted to fallback")
	}
}

func TestOverviewMovesCurrentVPNIntoProfessionalTopbar(t *testing.T) {
	data, err := os.ReadFile("web/operation-coordinator.js")
	if err != nil {
		t.Fatal(err)
	}
	js := string(data)
	for _, want := range []string{
		"topVpnSummary",
		"top-vpn-label",
		"Текущий VPN",
		"overview-hero-source",
		"overview-compact-grid",
		"renderOverviewTopbarFromStatus",
		"installOverviewTopbarStatusHook",
		"queueMicrotask",
		"typeof lastStatus !== 'undefined'",
		"VPN + DNS OK",
		"VPN OK · DNS напрямую",
		"Система требует внимания",
	} {
		if !strings.Contains(js, want) {
			t.Fatalf("compact Overview/topbar contract missing %q", want)
		}
	}
	if strings.Contains(js, "FreeNet доступен") {
		t.Fatal("topbar must report actual VPN/DNS health, not tautological FreeNet availability")
	}
}

func TestOverviewTopbarWatcherDoesNotPassArrayIndexAsQueryRoot(t *testing.T) {
	data, err := os.ReadFile("web/operation-coordinator.js")
	if err != nil {
		t.Fatal(err)
	}
	js := string(data)
	if strings.Contains(js, ".map(qs)") {
		t.Fatal("Array.map(qs) passes the numeric array index as qs root and breaks topbar synchronization at runtime")
	}
	if !strings.Contains(js, ".map(selector => qs(selector))") {
		t.Fatal("topbar watcher must resolve selectors explicitly")
	}
}
