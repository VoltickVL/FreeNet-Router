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
	memoryAt := strings.Index(html, `id="freenetOverviewCurrentQualityMemory"`)
	releaseAt := strings.Index(html, `id="freenetCanonicalBootRelease"`)
	if memoryAt < 0 || releaseAt < 0 || releaseAt < memoryAt {
		t.Fatalf("quality memory must be delivered before canonical boot release: memory=%d release=%d", memoryAt, releaseAt)
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


func TestOverviewCurrentQualityMemoryBridgesCoordinatorState(t *testing.T) {
	if !strings.Contains(overviewCurrentQualityMemoryScript, "freenet:current-quality-display") {
		t.Fatal("persisted current-quality renderer must publish the exact measured candidate to the Overview coordinator")
	}
	data, err := webFS.ReadFile("web/operation-coordinator.js")
	if err != nil {
		t.Fatal(err)
	}
	src := string(data)
	for _, want := range []string{
		"installCurrentQualityMemoryBridge",
		"freenet:current-quality-display",
		"currentQuality = Object.assign({}, candidate, {current:true})",
		"currentMetrics.children.length === 0",
	} {
		if !strings.Contains(src, want) {
			t.Fatalf("Overview current-quality bridge contract missing %q", want)
		}
	}
}
