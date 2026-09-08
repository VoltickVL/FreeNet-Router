package main

import (
	"testing"
	"time"
)

func resetBestServerCurrentQualityCacheForTest() {
	bestServerCurrentQualityCache.Lock()
	bestServerCurrentQualityCache.Entry = bestServerCurrentQualityCacheEntry{}
	bestServerCurrentQualityCache.Unlock()
}

func TestCurrentQualityCacheRejectsIncompleteEvidence(t *testing.T) {
	resetBestServerCurrentQualityCacheForTest()
	defer resetBestServerCurrentQualityCacheForTest()
	candidate := bestServerQualityCandidate{
		Current: true, Tested: true, Available: true,
		ID: "pl", Endpoint: "192.0.2.1:443", ApplicationMS: 180,
		TCPRTTMS: 120, MediaSamples: 0, ServiceOK: 4, ServiceTotal: 4,
	}
	storeBestServerCurrentQuality("192.0.2.1:443", "PL.*Warsaw", candidate)
	if _, ok := loadBestServerCurrentQuality("192.0.2.1:443", "PL.*Warsaw"); ok {
		t.Fatal("current quality without confirmed speed/eligibility must not be reused")
	}

	candidate.Eligible = true
	candidate.DownloadMbps = 80
	candidate.MediaSamples = bestServerMediaRequiredRuns
	candidate.MediaGrade = "good"
	storeBestServerCurrentQuality("192.0.2.1:443", "PL.*Warsaw", candidate)
	loaded, ok := loadBestServerCurrentQuality("192.0.2.1:443", "PL.*Warsaw")
	if !ok || !loaded.Current || loaded.DownloadMbps != 80 {
		t.Fatalf("complete current quality evidence was not cached: %+v %v", loaded, ok)
	}
}

func TestRecommendationFailsClosedWithoutConfirmedCurrentBaseline(t *testing.T) {
	response := bestServerQualityResponse{
		Available: true,
		Candidates: []bestServerQualityCandidate{
			{ID: "pl", Current: true, Tested: true, Available: true, Eligible: false, ApplicationMS: 180},
			{ID: "de", Tested: true, Available: true, Eligible: true, DownloadMbps: 100, ApplicationMS: 160, Score: 12000},
		},
		Recommendation: &bestServerQualityCandidate{ID: "de", Eligible: true},
	}
	final := applyBestServerRecommendationDeadband(response)
	if final.Available || final.Recommendation != nil {
		t.Fatalf("foreign candidate must not be recommended without comparable current baseline: %+v", final)
	}
	if final.Message == "" {
		t.Fatal("fail-closed result must explain why switching is unavailable")
	}
}

func TestCurrentQualityCacheExpiryStillApplies(t *testing.T) {
	resetBestServerCurrentQualityCacheForTest()
	defer resetBestServerCurrentQualityCacheForTest()
	candidate := bestServerQualityCandidate{Current: true, Tested: true, Available: true, Eligible: true, DownloadMbps: 75}
	bestServerCurrentQualityCache.Lock()
	bestServerCurrentQualityCache.Entry = bestServerCurrentQualityCacheEntry{
		Key: bestServerCurrentQualityKey("192.0.2.2:443", "DE"),
		StoredAt: time.Now().Add(-bestServerCurrentQualityCacheTTL - time.Second),
		Candidate: candidate,
	}
	bestServerCurrentQualityCache.Unlock()
	if _, ok := loadBestServerCurrentQuality("192.0.2.2:443", "DE"); ok {
		t.Fatal("expired current quality evidence must not be reused")
	}
}
