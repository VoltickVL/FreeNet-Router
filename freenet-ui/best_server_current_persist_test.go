package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func resetBestServerCurrentQualityMemory() {
	bestServerCurrentQualityCache.Lock()
	bestServerCurrentQualityCache.Entry = bestServerCurrentQualityCacheEntry{}
	bestServerCurrentQualityCache.Unlock()
}

func TestCurrentQualityPersistentCacheIsDisplayOnlyAndIdentityBound(t *testing.T) {
	resetBestServerCurrentQualityMemory()
	defer resetBestServerCurrentQualityMemory()

	path := filepath.Join(t.TempDir(), "current-quality.json")
	t.Setenv("FREENET_CURRENT_QUALITY_CACHE", path)
	endpoint := "198.51.100.24:443"
	filter := `^super-secret-logical-profile-filter$`
	candidate := bestServerQualityCandidate{
		Tested: true, Eligible: true, ID: "current-live", Name: "Belgium, Brussels, Extra", CountryCode: "be",
		Endpoint: endpoint, Current: true, Reachable: true, Available: true,
		TCPRTTMS: 42, TCPJitterMS: 3, ApplicationMS: 66, JitterMS: 5, DownloadMbps: 84.7,
		ThroughputSource: bestServerThroughputStrictAggregate,
		MediaGrade: "good", MediaSamples: bestServerMediaRequiredRuns, ServiceOK: 4, ServiceTotal: 4,
		Reason: "Проверен фактический активный VPN-путь",
	}

	storeBestServerCurrentQuality(endpoint, filter, candidate)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), filter) {
		t.Fatal("logical profile filter leaked into persistent display cache")
	}
	if !strings.Contains(string(data), `"identity_hash"`) {
		t.Fatal("persistent cache must bind identity by hash")
	}

	resetBestServerCurrentQualityMemory() // simulate Web Update / process restart
	shown, _, ok := loadBestServerCurrentQualityForDisplay(endpoint, filter)
	if !ok || shown.DownloadMbps != candidate.DownloadMbps || shown.ApplicationMS != candidate.ApplicationMS {
		t.Fatalf("persisted display cache not restored: ok=%v candidate=%+v", ok, shown)
	}
	if _, ok := loadBestServerCurrentQuality(endpoint, filter); ok {
		t.Fatal("persisted display cache must never become decision evidence after restart")
	}
	if _, _, ok := loadBestServerCurrentQualityForDisplay(endpoint, `^different-profile$`); ok {
		t.Fatal("display cache crossed logical profile identity")
	}
	if _, _, ok := loadBestServerCurrentQualityForDisplay("203.0.113.9:443", filter); ok {
		t.Fatal("display cache crossed endpoint identity")
	}
}

func TestCurrentQualityPersistentCachePreservesFallbackProvenanceAndRejectsLegacyAmbiguity(t *testing.T) {
	resetBestServerCurrentQualityMemory()
	defer resetBestServerCurrentQualityMemory()

	dir := t.TempDir()
	path := filepath.Join(dir, "current-quality.json")
	t.Setenv("FREENET_CURRENT_QUALITY_CACHE", path)
	endpoint := "198.51.100.25:443"
	filter := "^fallback-profile$"
	fallback := bestServerQualityCandidate{
		Tested: true, Eligible: false, ID: "current-live", Name: "Fallback Extra",
		Endpoint: endpoint, Current: true, Reachable: true, Available: true,
		ApplicationMS: 78, JitterMS: 6, FallbackDownloadMbps: 37.4,
		ThroughputSource: bestServerThroughputCurrentFallback,
		Reason: "Проверен фактический активный VPN-путь",
	}
	storeBestServerCurrentQuality(endpoint, filter, fallback)
	resetBestServerCurrentQualityMemory()
	shown, _, ok := loadBestServerCurrentQualityForDisplay(endpoint, filter)
	if !ok || shown.DownloadMbps != 0 || shown.FallbackDownloadMbps != 37.4 ||
		shown.ThroughputSource != bestServerThroughputCurrentFallback {
		t.Fatalf("fallback provenance was not restored exactly: ok=%v candidate=%+v", ok, shown)
	}
	if _, ok := loadBestServerCurrentQuality(endpoint, filter); ok {
		t.Fatal("persisted fallback throughput must never become decision evidence")
	}

	legacy := bestServerCurrentQualityPersistentEntry{
		Schema: 1, IdentityHash: bestServerCurrentQualityIdentityHash(endpoint, filter),
		StoredAt: "2026-10-03T00:00:00Z",
		Candidate: bestServerQualityCandidate{
			Tested: true, Endpoint: endpoint, Current: true, Available: true, DownloadMbps: 55,
		},
	}
	data, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := loadBestServerCurrentQualityForDisplay(endpoint, filter); ok {
		t.Fatal("schema v1 ambiguous download_mbps must be invalidated after provenance migration")
	}
}

func TestCurrentQualityPersistentCacheIgnoresCorruption(t *testing.T) {
	resetBestServerCurrentQualityMemory()
	defer resetBestServerCurrentQualityMemory()
	path := filepath.Join(t.TempDir(), "current-quality.json")
	t.Setenv("FREENET_CURRENT_QUALITY_CACHE", path)
	if err := os.WriteFile(path, []byte(`{"schema":1,"identity_hash":"broken"`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := loadBestServerCurrentQualityForDisplay("198.51.100.24:443", "filter"); ok {
		t.Fatal("corrupted cache must fail closed")
	}
}

func TestCurrentQualityCacheJobDoesNotStartProbe(t *testing.T) {
	resetBestServerCurrentQualityMemory()
	defer resetBestServerCurrentQualityMemory()
	dir := t.TempDir()
	cachePath := filepath.Join(dir, "current-quality.json")
	outPath := filepath.Join(dir, "04_outbounds.json")
	filterPath := filepath.Join(dir, "profile_filter.regex")
	t.Setenv("FREENET_CURRENT_QUALITY_CACHE", cachePath)
	endpoint := "198.51.100.24:443"
	filter := `^BE Brussels Extra$`
	outbound := `{"outbounds":[{"tag":"vless-reality","settings":{"vnext":[{"address":"198.51.100.24","port":443}]}}]}`
	if err := os.WriteFile(outPath, []byte(outbound), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filterPath, []byte(filter+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	storeBestServerCurrentQuality(endpoint, readBestServerCurrentFilter(filterPath), bestServerQualityCandidate{
		Tested: true, Eligible: true, ID: "current-live", Name: "Belgium, Brussels, Extra", CountryCode: "be",
		Endpoint: endpoint, Current: true, Reachable: true, Available: true,
		TCPRTTMS: 40, ApplicationMS: 60, JitterMS: 4, DownloadMbps: 90,
		ThroughputSource: bestServerThroughputStrictAggregate,
		MediaGrade: "good", MediaSamples: bestServerMediaRequiredRuns, ServiceOK: 4, ServiceTotal: 4,
	})
	resetBestServerCurrentQualityMemory() // cache response must be able to use disk only

	a := &app{cfg: config{OutPath: outPath, FilterPath: filterPath}, sem: make(chan struct{}, 1)}
	jobs := &bestServerJobs{}
	var scans atomic.Int32
	handler := jobs.wrap(a, "current", nil, func(context.Context) (bestServerQualityResponse, error) {
		scans.Add(1)
		return bestServerQualityResponse{}, nil
	})
	w := httptest.NewRecorder()
	handler(w, httptest.NewRequest(http.MethodGet, "/api/vpn/current-quality?job=cache", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("cache status=%d body=%s", w.Code, w.Body.String())
	}
	if scans.Load() != 0 || len(a.sem) != 0 {
		t.Fatalf("display cache read started an operation: scans=%d sem=%d", scans.Load(), len(a.sem))
	}
	var response bestServerQualityResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if !response.Success || response.Mutation != "NONE" || len(response.Candidates) != 1 || response.Candidates[0].DownloadMbps != 90 {
		t.Fatalf("unexpected cache response: %+v", response)
	}
}
