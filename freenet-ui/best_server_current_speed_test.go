package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestCurrentVPNFallbackDownloadProducesDisplayableSpeed(t *testing.T) {
	dir := t.TempDir()
	curl := filepath.Join(dir, "curl")
	if err := os.WriteFile(curl, []byte("#!/bin/sh\nprintf '200\\t2000000\\t0.100\\t0.900'\n"), 0755); err != nil {
		t.Fatal(err)
	}
	mbps, issue := probeBestServerCurrentFallbackDownload(context.Background(), curl, "127.0.0.1:1080")
	if issue != "" {
		t.Fatalf("unexpected fallback issue: %s", issue)
	}
	if mbps < 19.9 || mbps > 20.1 {
		t.Fatalf("expected about 20 Mbps, got %.2f", mbps)
	}
}

func TestCurrentQualityCacheSeparatesDisplayFromDecisionEvidence(t *testing.T) {
	resetBestServerCurrentQualityCacheForTest()
	defer resetBestServerCurrentQualityCacheForTest()

	partial := bestServerQualityCandidate{
		Current: true, Tested: true, Available: true,
		ApplicationMS: 120, JitterMS: 8,
		FallbackDownloadMbps: 42.5, ThroughputSource: bestServerThroughputCurrentFallback,
		Eligible: false,
	}
	storeBestServerCurrentQuality("203.0.113.10:443", "profile-a", partial)

	display, _, ok := loadBestServerCurrentQualityForDisplay("203.0.113.10:443", "profile-a")
	if !ok || display.DownloadMbps != 0 || display.FallbackDownloadMbps != partial.FallbackDownloadMbps ||
		display.ThroughputSource != bestServerThroughputCurrentFallback {
		t.Fatalf("fallback current measurement must remain visible with provenance: %#v ok=%v", display, ok)
	}
	if _, ok := loadBestServerCurrentQuality("203.0.113.10:443", "profile-a"); ok {
		t.Fatal("fallback display measurement must never become Best Server/AUTO decision evidence")
	}

	complete := partial
	complete.FallbackDownloadMbps = 0
	complete.DownloadMbps = 42.5
	complete.ThroughputSource = bestServerThroughputStrictAggregate
	complete.Eligible = true
	storeBestServerCurrentQuality("203.0.113.10:443", "profile-a", complete)
	if got, ok := loadBestServerCurrentQuality("203.0.113.10:443", "profile-a"); !ok || !got.Eligible ||
		got.DownloadMbps != 42.5 || got.ThroughputSource != bestServerThroughputStrictAggregate || got.FallbackDownloadMbps != 0 {
		t.Fatalf("fresh eligible strict measurement must remain reusable for decisions: %#v ok=%v", got, ok)
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
	if candidate.DownloadMbps != 0 {
		t.Fatalf("fallback throughput must never populate canonical download_mbps: %#v", candidate)
	}
	if eligibleBestServerQuality(candidate) {
		t.Fatal("fallback throughput alone must not make a current VPN eligible for Best Server/AUTO decisions")
	}
}
