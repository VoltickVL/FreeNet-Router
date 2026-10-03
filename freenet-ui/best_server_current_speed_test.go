package main

import (
	"os"
	"strings"
	"testing"
)

func TestCurrentQualityCacheStripsLegacyFallbackSpeed(t *testing.T) {
	resetBestServerCurrentQualityCacheForTest()
	defer resetBestServerCurrentQualityCacheForTest()

	legacyFallback := bestServerQualityCandidate{
		Current: true, Tested: true, Available: true,
		ApplicationMS: 120, JitterMS: 8,
		FallbackDownloadMbps: 42.5,
		ThroughputSource: bestServerThroughputCurrentFallback,
		Eligible: false,
	}
	storeBestServerCurrentQuality("203.0.113.10:443", "profile-a", legacyFallback)

	display, _, ok := loadBestServerCurrentQualityForDisplay("203.0.113.10:443", "profile-a")
	if !ok {
		t.Fatal("partial current measurement should remain displayable")
	}
	if display.DownloadMbps != 0 || display.FallbackDownloadMbps != 0 || display.ThroughputSource != "" {
		t.Fatalf("legacy fallback speed leaked into current display evidence: %#v", display)
	}
	if _, ok := loadBestServerCurrentQuality("203.0.113.10:443", "profile-a"); ok {
		t.Fatal("partial measurement without canonical strict speed must not become decision evidence")
	}
}

func TestCurrentStrictSpeedRemainsCanonicalDecisionEvidence(t *testing.T) {
	resetBestServerCurrentQualityCacheForTest()
	defer resetBestServerCurrentQualityCacheForTest()

	complete := bestServerQualityCandidate{
		Current: true, Tested: true, Available: true, Eligible: true,
		ApplicationMS: 120, JitterMS: 8,
		DownloadMbps: 42.5,
		ThroughputSource: bestServerThroughputStrictAggregate,
		MediaSamples: bestServerMediaRequiredRuns,
		MediaGrade: "good",
		ServiceOK: 4, ServiceTotal: 4,
	}
	storeBestServerCurrentQuality("203.0.113.10:443", "profile-a", complete)

	display, _, ok := loadBestServerCurrentQualityForDisplay("203.0.113.10:443", "profile-a")
	if !ok || display.DownloadMbps != 42.5 || display.ThroughputSource != bestServerThroughputStrictAggregate || display.FallbackDownloadMbps != 0 {
		t.Fatalf("strict current speed was not preserved exactly: %#v ok=%v", display, ok)
	}
	got, ok := loadBestServerCurrentQuality("203.0.113.10:443", "profile-a")
	if !ok || !got.Eligible || got.DownloadMbps != 42.5 || got.ThroughputSource != bestServerThroughputStrictAggregate {
		t.Fatalf("canonical strict measurement must remain reusable for decisions: %#v ok=%v", got, ok)
	}
}

func TestCurrentFallbackSpeedDoesNotRelaxEligibility(t *testing.T) {
	candidate := bestServerQualityCandidate{
		Available: true,
		FallbackDownloadMbps: 80,
		ThroughputSource: bestServerThroughputCurrentFallback,
		MediaSamples: 0,
		MediaGrade: "unknown",
		ServiceOK: 4,
		ServiceTotal: 4,
	}
	if eligibleBestServerQuality(candidate) {
		t.Fatal("legacy fallback throughput must never make a current VPN eligible")
	}
}


func TestCurrentAndBestServerShareCanonicalSpeedAlgorithm(t *testing.T) {
	currentBytes, err := os.ReadFile("best_server_current_live.go")
	if err != nil {
		t.Fatal(err)
	}
	bestBytes, err := os.ReadFile("best_server_quality.go")
	if err != nil {
		t.Fatal(err)
	}
	current := string(currentBytes)
	best := string(bestBytes)
	const canonical = "probeBestServerMediaQuality(ctx, curlPath, socks)"
	if !strings.Contains(current, canonical) || !strings.Contains(best, canonical) {
		t.Fatalf("Current VPN and Best Server must share canonical media/speed probe")
	}
	for _, forbidden := range []string{
		"probeBestServerCurrentFallbackDownload",
		"bestServerCurrentFallbackDownloadURL",
	} {
		if strings.Contains(current, forbidden) {
			t.Fatalf("current VPN reintroduced alternate speed algorithm %q", forbidden)
		}
	}
}
