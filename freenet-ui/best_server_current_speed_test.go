package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func resetBestServerCurrentQualityCacheForTest() {
	bestServerCurrentQualityCache.Lock()
	bestServerCurrentQualityCache.Entry = bestServerCurrentQualityCacheEntry{}
	bestServerCurrentQualityCache.Unlock()
}

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
		ApplicationMS: 120, JitterMS: 8, DownloadMbps: 42.5,
		Eligible: false,
	}
	storeBestServerCurrentQuality("203.0.113.10:443", "profile-a", partial)

	display, _, ok := loadBestServerCurrentQualityForDisplay("203.0.113.10:443", "profile-a")
	if !ok || display.DownloadMbps != partial.DownloadMbps {
		t.Fatalf("partial current measurement must remain visible: %#v ok=%v", display, ok)
	}
	if _, ok := loadBestServerCurrentQuality("203.0.113.10:443", "profile-a"); ok {
		t.Fatal("partial display measurement must never become Best Server/AUTO decision evidence")
	}

	complete := partial
	complete.Eligible = true
	storeBestServerCurrentQuality("203.0.113.10:443", "profile-a", complete)
	if got, ok := loadBestServerCurrentQuality("203.0.113.10:443", "profile-a"); !ok || !got.Eligible {
		t.Fatalf("fresh eligible measurement must remain reusable for decisions: %#v ok=%v", got, ok)
	}
}

func TestCurrentFallbackSpeedDoesNotRelaxEligibility(t *testing.T) {
	candidate := bestServerQualityCandidate{
		Available: true,
		DownloadMbps: 80,
		MediaSamples: 0,
		MediaGrade: "unknown",
		ServiceOK: 4,
		ServiceTotal: 4,
	}
	if eligibleBestServerQuality(candidate) {
		t.Fatal("fallback throughput alone must not make a current VPN eligible for Best Server/AUTO decisions")
	}
}
