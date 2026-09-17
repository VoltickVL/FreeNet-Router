package main

import (
	"strings"
	"testing"
)

func TestOverviewCurrentQualityMemoryDeliveredAfterAcceptedUX(t *testing.T) {
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
		`/api/vpn/current-quality?job=cache`,
		`Последний замер: `,
		`seedMissingMeasurement`,
		`/api/vpn/current-quality?job=start&id=`,
		`renderMetrics(candidate)`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("overview quality memory contract missing %q", want)
		}
	}
	acceptedAt := strings.Index(html, `/accepted-ux.js?v=v`)
	memoryAt := strings.Index(html, `id="freenetOverviewCurrentQualityMemory"`)
	releaseAt := strings.Index(html, `id="freenetCanonicalBootRelease"`)
	if acceptedAt < 0 || memoryAt < acceptedAt || releaseAt < memoryAt {
		t.Fatalf("quality memory delivery order invalid: accepted=%d memory=%d release=%d", acceptedAt, memoryAt, releaseAt)
	}
}

func TestOverviewCurrentQualityMemoryDoesNotHideMutation(t *testing.T) {
	for _, forbidden := range []string{
		`/api/action`,
		`/api/network-profile/apply`,
		`/api/vpn/current-refresh`,
		`/api/vpn/best-foreign`,
	} {
		if strings.Contains(overviewCurrentQualityMemoryScript, forbidden) {
			t.Fatalf("overview first-paint memory must not contain mutation/broad-scan endpoint %q", forbidden)
		}
	}
	if !strings.Contains(overviewCurrentQualityMemoryScript, `if (!renderQuality(data)) void seedMissingMeasurement();`) {
		t.Fatal("silent current-VPN seed must run only when exact cached display data is missing")
	}
}
