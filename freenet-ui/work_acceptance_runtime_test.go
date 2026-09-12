package main

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestCurrentQualityDisplayDoesNotRelaxDecisionTTL(t *testing.T) {
	const endpoint = "203.0.113.10:443"
	const filter = "Warsaw, Poland, Extra"

	bestServerCurrentQualityCache.Lock()
	previous := bestServerCurrentQualityCache.Entry
	bestServerCurrentQualityCache.Entry = bestServerCurrentQualityCacheEntry{
		Key:      bestServerCurrentQualityKey(endpoint, filter),
		StoredAt: time.Now().Add(-bestServerCurrentQualityCacheTTL - time.Minute),
		Candidate: bestServerQualityCandidate{
			Current: true, Tested: true, Available: true, Eligible: true,
			ApplicationMS: 165, JitterMS: 7, DownloadMbps: 149,
		},
	}
	bestServerCurrentQualityCache.Unlock()
	defer func() {
		bestServerCurrentQualityCache.Lock()
		bestServerCurrentQualityCache.Entry = previous
		bestServerCurrentQualityCache.Unlock()
	}()

	if _, ok := loadBestServerCurrentQuality(endpoint, filter); ok {
		t.Fatal("stale display metric must never remain decision-fresh")
	}

	got, checkedAt, ok := loadBestServerCurrentQualityForDisplay(endpoint, filter)
	if !ok || checkedAt.IsZero() {
		t.Fatal("last complete current-VPN measurement must remain available for display")
	}
	if got.DownloadMbps != 149 || got.ApplicationMS != 165 || got.JitterMS != 7 || !got.Eligible {
		t.Fatalf("unexpected display measurement: %+v", got)
	}
}

func TestCurrentQualityDisplayRequiresExactLogicalIdentity(t *testing.T) {
	bestServerCurrentQualityCache.Lock()
	previous := bestServerCurrentQualityCache.Entry
	bestServerCurrentQualityCache.Entry = bestServerCurrentQualityCacheEntry{
		Key:      bestServerCurrentQualityKey("203.0.113.10:443", "Warsaw, Poland, Extra"),
		StoredAt: time.Now(),
		Candidate: bestServerQualityCandidate{
			Current: true, Tested: true, Available: true, Eligible: true, DownloadMbps: 149,
		},
	}
	bestServerCurrentQualityCache.Unlock()
	defer func() {
		bestServerCurrentQualityCache.Lock()
		bestServerCurrentQualityCache.Entry = previous
		bestServerCurrentQualityCache.Unlock()
	}()

	if _, _, ok := loadBestServerCurrentQualityForDisplay("203.0.113.10:443", "Frankfurt, Germany, Extra"); ok {
		t.Fatal("same endpoint with a different logical profile must not reuse display metrics")
	}
	if _, _, ok := loadBestServerCurrentQualityForDisplay("203.0.113.11:443", "Warsaw, Poland, Extra"); ok {
		t.Fatal("changed endpoint must not reuse previous display metrics")
	}
}

func TestSettingsWorkAcceptanceRuntimeAsset(t *testing.T) {
	data, err := automationWebFS.ReadFile("web/runtime-acceptance.js")
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	for _, required := range []string{
		".footer",
		"current_quality_checked_at",
		"current_quality_fresh",
		"DNS через роутер",
		"Раздельный DNS",
		"normalizeLegacyDNSLabels",
		"dnsLabels.xkeen = 'Раздельный DNS'",
		"canonicalizeDNSCopy",
		"protectTopbarDNSBoundary",
		"Object.getOwnPropertyDescriptor(Node.prototype, 'textContent')",
		"Object.defineProperty(node, 'textContent'",
		"freenetDNSBoundary",
		"fnLastRun",
		"fnCountriesBtn",
		"fnAutoMode",
		"fnCountryScope",
		"mode !== 'best' || scope !== 'allowlist'",
		"latestAutomationPayload",
		"latestStatusPayload",
		"reconcileRuntimePresentation",
		"scheduleRuntimePresentation",
		"window.fetch",
		"const originalJSON = response.json.bind(response)",
		"response.json = async",
		"requestAnimationFrame",
	} {
		if !strings.Contains(s, required) {
			t.Fatalf("runtime acceptance asset missing %q", required)
		}
	}
	if strings.Contains(s, "if (!auto || !auto.current_quality_known) return") {
		t.Fatal("current-check timestamp must be rendered independently from scheduler last_run")
	}
	for _, forbidden := range []string{"MutationObserver", "XKeen/Xray DNS", "response.clone()", "setInterval("} {
		if strings.Contains(s, forbidden) {
			t.Fatalf("runtime acceptance asset contains forbidden/racy surface %q", forbidden)
		}
	}

	jsonHook := strings.Index(s, "response.json = async")
	if jsonHook < 0 {
		t.Fatal("consumer response.json hook is required")
	}
	hook := s[jsonHook:]
	statusNormalize := strings.Index(hook, "normalizeLegacyDNSLabels();")
	schedule := strings.Index(hook, "scheduleRuntimePresentation();")
	returnPayload := strings.Index(hook, "return payload;")
	if statusNormalize < 0 || schedule < 0 || returnPayload < 0 || statusNormalize > returnPayload || schedule > returnPayload {
		t.Fatal("DNS normalization and presentation scheduling must happen from consumer response.json before payload return")
	}
}

func TestSettingsDNSBoundaryCoversIndependentTopbarWriter(t *testing.T) {
	patch, err := automationWebFS.ReadFile("web/runtime-acceptance.js")
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := webFS.ReadFile("web/operation-coordinator.js")
	if err != nil {
		t.Fatal(err)
	}
	legacySource := string(legacy)
	if !strings.Contains(legacySource, "setInterval(syncOverviewTopbar, 2500)") || !strings.Contains(legacySource, "setText(qs('#topDNSValue'), dnsLabel(status))") {
		t.Fatal("expected independent legacy topbar writer contract changed; review DNS boundary protection")
	}
	patchSource := string(patch)
	if !strings.Contains(patchSource, "protectTopbarDNSBoundary();") || !strings.Contains(patchSource, "canonicalizeDNSCopy(value)") {
		t.Fatal("accepted DNS write boundary must protect topbar from independent legacy writes")
	}
}

func TestAutomationAssetLoaderKeepsPatchFailOpen(t *testing.T) {
	data, err := automationWebFS.ReadFile("web/runtime-acceptance.js")
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 {
		t.Fatal("runtime acceptance asset must be embedded")
	}

	// The bootstrap intentionally loads automation.js on both patch success and
	// patch failure, so a cosmetic/runtime-acceptance fix cannot block Settings.
	// Keep this source contract explicit because the two scripts are embedded.
	const bootstrapNeedle = "patch.onerror = loadAutomation"
	source, err := os.ReadFile("automation_api.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(source), bootstrapNeedle) || !strings.Contains(string(source), "patch.onload = loadAutomation") {
		t.Fatal("runtime patch must fail open to the canonical automation UI")
	}
}
