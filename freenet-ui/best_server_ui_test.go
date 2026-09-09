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
	markup, err := os.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	js := string(data) + string(markup)
	for _, want := range []string{
		"/api/vpn/current-quality",
		"/api/vpn/best-foreign",
		"Проверить текущий VPN",
		"Подобрать серверы",
		"Топ-3 варианта на основе реальных измерений",
		"Лучший вариант",
		"Для сравнения",
		"Не прошёл проверку",
		"countries.remove()",
		"operation: 'provider'",
		"profile_id:candidate.id",
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
	markup, err := os.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	js := string(data) + string(markup)
	for _, want := range []string{
		"installBestServerActionDelegation",
		"freenetBestServerActions",
		"document.addEventListener('click'",
		"#bestServerCheckCurrent,#bestServerRefresh,.vpn-option-apply,.vpn-option-retry",
		"void scanCurrentVPN()",
		"void scanBestServer()",
		"void applyCandidate(candidate)",
		"},true)",
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
	markup, err := os.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	js := string(data) + string(markup)
	for _, want := range []string{
		"isRussianProfile",
		"country_code",
		"extra_profiles.filter(profile => !isRussianProfile(profile))",
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
	markup := string(index)
	script := string(js)
	if !strings.Contains(markup, `id="profileSearch"`) || !strings.Contains(markup, `id="profilesTrigger"`) {
		t.Fatal("manual exact profile selector disappeared")
	}
	for _, want := range []string{"fn-topbar-vpn-picker", "topbar.insertBefore(manual, topSummary || topActions || null)"} {
		if !strings.Contains(script, want) {
			t.Fatalf("manual exact selector must remain as topbar fallback: missing %q", want)
		}
	}
	if !strings.Contains(markup, "extraProfiles.length?'Выбрать VPN':'Профили не загружены'") {
		t.Fatal("topbar exact selector must use user-facing label 'Выбрать VPN'")
	}
}

func TestOverviewKeepsSystemSummaryAndSingleVPNCard(t *testing.T) {
	data, err := os.ReadFile("web/operation-coordinator.js")
	if err != nil {
		t.Fatal(err)
	}
	markup, err := os.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	js := string(data) + string(markup)
	for _, want := range []string{
		"FreeNetApprovedOverview",
		"overviewApprovedTop",
		"overview-approved-fact",
		"Текущий VPN",
		"Провайдер",
		"DNS",
		"overview-hero-source",
		"overview-compact-grid",
		"renderOverviewTopbarFromStatus",
		"installOverviewStatusHook",
		"queueMicrotask",
		"typeof lastStatus !== 'undefined'",
		"VPN + DNS OK",
		"VPN OK · DNS напрямую",
		"VPN не работает",
		"DNS требует внимания",
	} {
		if !strings.Contains(js, want) {
			t.Fatalf("approved Overview/topbar contract missing %q", want)
		}
	}
}

func TestOverviewTopbarSyncDoesNotUseArrayIndexAsQueryRoot(t *testing.T) {
	data, err := os.ReadFile("web/operation-coordinator.js")
	if err != nil {
		t.Fatal(err)
	}
	js := string(data)
	if strings.Contains(js, ".map(qs)") {
		t.Fatal("Array.map(qs) passes numeric array index as qs root and breaks topbar synchronization")
	}
	for _, want := range []string{"syncOverviewTopbar", "#topISPValue", "#topDNSValue"} {
		if !strings.Contains(js, want) {
			t.Fatalf("topbar direct synchronization missing %q", want)
		}
	}
}
