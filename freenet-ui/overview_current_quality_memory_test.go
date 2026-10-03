package main

import (
	"strings"
	"testing"
)

func TestOverviewCurrentQualityMemoryDeliveredBeforeBootRelease(t *testing.T) {
	rawBytes, err := webFS.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	html, err := canonicalizeControlCenterIndex(string(rawBytes))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`id="freenetOverviewCurrentQualityMemory"`,
		`/api/status`,
		`/api/vpn/current-quality?job=cache`,
		`seedMissingMeasurement`,
		`/api/vpn/current-quality?job=start&id=`,
		`window.__freenetCurrentQualityHydration`,
		`freenet:current-quality-display`,
		`liveEndpoint !== String(candidate.endpoint).trim()`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("overview quality hydration contract missing %q", want)
		}
	}
	memoryAt := strings.Index(html, `id="freenetOverviewCurrentQualityMemory"`)
	releaseAt := strings.Index(html, `id="freenetCanonicalBootRelease"`)
	if memoryAt < 0 || releaseAt < 0 || releaseAt < memoryAt {
		t.Fatalf("quality hydration must be delivered before canonical boot release: memory=%d release=%d", memoryAt, releaseAt)
	}
}

func TestOverviewCurrentQualityMemoryIsHydrateOnly(t *testing.T) {
	for _, forbidden := range []string{
		`/api/action`,
		`/api/network-profile/apply`,
		`/api/vpn/current-refresh`,
		`/api/vpn/best-foreign`,
		`renderMetrics`,
		`bestCurrentMetrics`,
		`bestCurrentQuality`,
		`bestCurrentHealth`,
		`latencyOnlyWarning`,
		`Быстрый замер`,
		`Скорость VPN`,
	} {
		if strings.Contains(overviewCurrentQualityMemoryScript, forbidden) {
			t.Fatalf("overview first-paint hydration must not own UI/mutation surface %q", forbidden)
		}
	}
	for _, want := range []string{
		`if (publish(data, status)) return;`,
		`void seedMissingMeasurement(status);`,
		`candidate.endpoint`,
		`status.endpoint`,
	} {
		if !strings.Contains(overviewCurrentQualityMemoryScript, want) {
			t.Fatalf("hydrate-only memory missing %q", want)
		}
	}
}

func TestOverviewCurrentQualityMemoryUsesHTTPCompatibleJobID(t *testing.T) {
	if strings.Contains(overviewCurrentQualityMemoryScript, "crypto.randomUUID()") {
		t.Fatal("overview quality seed must not depend on secure-context-only crypto.randomUUID")
	}
	for _, want := range []string{
		"window.freenetQualityJobID",
		"^[a-zA-Z0-9_-]{16,64}$",
	} {
		if !strings.Contains(overviewCurrentQualityMemoryScript, want) {
			t.Fatalf("overview quality seed missing %q", want)
		}
	}
	data, err := webFS.ReadFile("web/operation-coordinator.js")
	if err != nil {
		t.Fatal(err)
	}
	src := string(data)
	for _, want := range []string{"window.freenetQualityJobID = qualityJobID", "cryptoAPI.getRandomValues(bytes)"} {
		if !strings.Contains(src, want) {
			t.Fatalf("shared quality job ID generator missing %q", want)
		}
	}
}

func TestOverviewCurrentQualityMemoryBridgesSingleCoordinatorRenderer(t *testing.T) {
	data, err := webFS.ReadFile("web/operation-coordinator.js")
	if err != nil {
		t.Fatal(err)
	}
	src := string(data)
	for _, want := range []string{
		"installCurrentQualityMemoryBridge",
		"freenet:current-quality-display",
		"currentQuality = Object.assign({}, candidate, {current:true})",
		"renderCurrentQuality({scanned_at: detail.scanned_at || '', candidates:[currentQuality]})",
		"window.__freenetCurrentQualityHydration",
		"fallback_download_mbps",
		"current_fallback",
		"strict_aggregate",
		"не для сравнения",
	} {
		if !strings.Contains(src, want) {
			t.Fatalf("Overview coordinator ownership contract missing %q", want)
		}
	}
}
