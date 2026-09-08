package main

import (
	"os"
	"strings"
	"testing"
)

func TestBestServerUIUsesExplicitIndependentScans(t *testing.T) {
	data, err := os.ReadFile("web/operation-coordinator.js")
	if err != nil {
		t.Fatal(err)
	}
	js := string(data)
	for _, want := range []string{
		"Ничего не проверяется автоматически",
		"/api/vpn/current-quality",
		"/api/vpn/best-foreign",
		"Проверить текущий VPN",
		"Найти лучший VPN",
		"Переключиться на лучший",
		"Почему рекомендуем",
		"countries.remove()",
		"operation: 'provider'",
		"profile_id: recommendation.id",
		"MUTATION NONE",
		"Мбит/с",
	} {
		if !strings.Contains(js, want) {
			t.Fatalf("Best Server UI contract missing %q", want)
		}
	}
	if strings.Contains(js, "setTimeout(() => scanBestServer") {
		t.Fatal("Overview must not auto-start Best Server scan")
	}
	if strings.Contains(js, "current.addEventListener('click', () => scanBestServer") {
		t.Fatal("current VPN button must not trigger full Best Server scan")
	}
}

func TestBestServerActionsHaveLifecycleSafeDelegation(t *testing.T) {
	data, err := os.ReadFile("web/operation-coordinator.js")
	if err != nil {
		t.Fatal(err)
	}
	js := string(data)
	for _, want := range []string{
		"installBestServerActionDelegation",
		"freenetBestServerActions",
		"document.addEventListener('click'",
		"#bestServerCheckCurrent, #bestServerRefresh, #bestServerApply",
		"void scanCurrentVPN()",
		"void scanBestServer()",
		"void applyBestServer()",
		"}, true)",
	} {
		if !strings.Contains(js, want) {
			t.Fatalf("lifecycle-safe Best Server action contract missing %q", want)
		}
	}
	if !strings.Contains(js, "installBestServerActionDelegation();") {
		t.Fatal("delegated Best Server action binding must be installed at startup")
	}
}

func TestBestServerUIExcludesRussiaFromSuggestions(t *testing.T) {
	data, err := os.ReadFile("web/operation-coordinator.js")
	if err != nil {
		t.Fatal(err)
	}
	js := string(data)
	for _, want := range []string{
		"isRussianProfile",
		"profile.country_code",
		"extra_profiles.filter(profile => !isRussianProfile(profile))",
		"Российские серверы исключены",
	} {
		if !strings.Contains(js, want) {
			t.Fatalf("foreign-only UI contract missing %q", want)
		}
	}
}

func TestBestServerUXRoutesAreRegistered(t *testing.T) {
	data, err := os.ReadFile("geodata_api.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "registerBestServerUXAPI(mux, a)") {
		t.Fatal("explicit current/best VPN quality API must be registered at startup")
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
		t.Fatal("manual exact selector must remain as advanced fallback")
	}
}

func TestOverviewMovesCurrentVPNIntoProfessionalTopbar(t *testing.T) {
	data, err := os.ReadFile("web/operation-coordinator.js")
	if err != nil {
		t.Fatal(err)
	}
	js := string(data)
	for _, want := range []string{
		"FreeNetOverviewV4",
		"topVpnSummary",
		"overview-v4-chip-label",
		"Текущий VPN",
		"ISP",
		"DNS",
		"overview-hero-source",
		"overview-compact-grid",
		"renderOverviewTopbarFromStatus",
		"installOverviewTopbarStatusHook",
		"queueMicrotask",
		"typeof lastStatus !== 'undefined'",
		"VPN + DNS OK",
		"VPN OK · DNS напрямую",
		"VPN не работает",
		"DNS требует внимания",
	} {
		if !strings.Contains(js, want) {
			t.Fatalf("Overview v4/topbar contract missing %q", want)
		}
	}
}

func TestOverviewTopbarWatcherDoesNotPassArrayIndexAsQueryRoot(t *testing.T) {
	data, err := os.ReadFile("web/operation-coordinator.js")
	if err != nil {
		t.Fatal(err)
	}
	js := string(data)
	if strings.Contains(js, ".map(qs)") {
		t.Fatal("Array.map(qs) passes numeric array index as qs root and breaks topbar synchronization")
	}
	if !strings.Contains(js, ".map(selector => qs(selector))") {
		t.Fatal("topbar watcher must resolve selectors explicitly")
	}
}
