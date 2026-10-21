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
	"time"
)

func currentDisplayThroughputFixture(t *testing.T) (*app, string, string, string) {
	t.Helper()
	resetBestServerCurrentQualityMemory()
	t.Cleanup(resetBestServerCurrentQualityMemory)
	dir := t.TempDir()
	cachePath := filepath.Join(dir, "current-quality.json")
	outPath := filepath.Join(dir, "04_outbounds.json")
	filterPath := filepath.Join(dir, "profile_filter.regex")
	t.Setenv("FREENET_CURRENT_QUALITY_CACHE", cachePath)
	endpoint := "198.51.100.42:443"
	filter := "^private logical profile$"
	for path, data := range map[string]string{
		outPath: `{"outbounds":[{"tag":"vless-reality","settings":{"vnext":[{"address":"198.51.100.42","port":443}]}}]}`,
		filterPath: filter + "\n",
	} {
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	a := &app{cfg: config{OutPath: outPath, FilterPath: filterPath}, sem: make(chan struct{}, 1)}
	return a, endpoint, filter, cachePath
}

func currentDisplayThroughputCandidate(endpoint string, mbps float64) bestServerQualityCandidate {
	candidate := bestServerQualityCandidate{
		Current: true, Tested: true, Available: true, Reachable: true,
		Endpoint: endpoint, ID: "current-live", Name: "Current VPN",
		ApplicationMS: 140, JitterMS: 8, MediaSamples: 1, MediaGrade: "unknown",
		ServiceOK: 4, ServiceTotal: 4,
	}
	if mbps > 0 {
		candidate.DownloadMbps = mbps
		candidate.ThroughputSource = bestServerThroughputStrictAggregate
		candidate.MediaSamples = bestServerMediaRequiredRuns
		candidate.MediaGrade = "good"
		candidate.Eligible = eligibleBestServerQuality(candidate)
	}
	return candidate
}

func TestCurrentDisplayThroughputRetainsLastSuccessWithoutChangingLatestEvidence(t *testing.T) {
	_, endpoint, filter, cachePath := currentDisplayThroughputFixture(t)
	good := currentDisplayThroughputCandidate(endpoint, 91.4)
	storeBestServerCurrentQuality(endpoint, filter, good)
	first := loadBestServerDisplayThroughput(endpoint, filter)
	if first == nil || first.Historical || first.DownloadMbps != 91.4 {
		t.Fatalf("fresh strict throughput missing: %+v", first)
	}
	if _, ok := loadBestServerCurrentQuality(endpoint, filter); !ok {
		t.Fatal("fresh eligible strict measurement stopped being decision evidence")
	}

	partial := currentDisplayThroughputCandidate(endpoint, 0)
	partial.DownloadIssue = "https://private.example/transfer?token=hidden-token"
	partial.MediaIssue = "password=hidden-password"
	partial.Rejections = []string{"Скорость не измерена"}
	storedAt, display := storeBestServerCurrentQualityDisplaySnapshot(endpoint, filter, partial)
	if display == nil || !display.Historical || display.DownloadMbps != 91.4 || display.MeasuredAt != first.MeasuredAt {
		t.Fatalf("partial result lost or refreshed last-good throughput: first=%+v display=%+v", first, display)
	}
	if storedAt.IsZero() || partial.DownloadMbps != 0 || partial.ThroughputSource != "" || partial.Eligible ||
		partial.DownloadIssue != "https://private.example/transfer?token=hidden-token" {
		t.Fatal("display retention changed the caller's actual measurement")
	}
	partial.Rejections[0] = "caller changed its own slice"
	latest, checkedAt, ok := loadBestServerCurrentQualityForDisplay(endpoint, filter)
	if !ok || latest.DownloadMbps != 0 || latest.Eligible || latest.MediaSamples != 1 || checkedAt != storedAt ||
		len(latest.Rejections) != 1 || latest.Rejections[0] != "Скорость не измерена" {
		t.Fatalf("last-good speed replaced latest evidence or cache shares caller memory: %+v", latest)
	}
	latest.Rejections[0] = "display consumer changed its copy"
	latestAgain, _, _ := loadBestServerCurrentQualityForDisplay(endpoint, filter)
	if latestAgain.Rejections[0] != "Скорость не измерена" {
		t.Fatal("display read returned a mutable cache slice")
	}
	if _, ok := loadBestServerCurrentQuality(endpoint, filter); ok {
		t.Fatal("historical throughput made the incomplete latest candidate reusable for decisions")
	}

	data, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{filter, "private.example", "hidden-token", "hidden-password", "download_issue", "media_issue"} {
		if strings.Contains(string(data), secret) {
			t.Fatalf("persistent display cache leaked %q", secret)
		}
	}
	var persisted bestServerCurrentQualityPersistentEntry
	if err := json.Unmarshal(data, &persisted); err != nil {
		t.Fatal(err)
	}
	if persisted.Candidate.DownloadMbps != 0 || persisted.LastGoodThroughput == nil ||
		persisted.LastGoodThroughput.DownloadMbps != 91.4 || persisted.LastGoodThroughput.MeasuredAt != first.MeasuredAt {
		t.Fatalf("persistent latest/last-good separation lost: %+v", persisted)
	}

	resetBestServerCurrentQualityMemory() // process restart: display may read disk, decisions may not
	restored := loadBestServerDisplayThroughput(endpoint, filter)
	if restored == nil || !restored.Historical || restored.DownloadMbps != 91.4 || restored.MeasuredAt != first.MeasuredAt {
		t.Fatalf("last-good throughput did not survive restart: %+v", restored)
	}
	restored.DownloadMbps = 999
	if again := loadBestServerDisplayThroughput(endpoint, filter); again == nil || again.DownloadMbps != 91.4 {
		t.Fatal("display return value aliases retained state")
	}
	if _, ok := loadBestServerCurrentQuality(endpoint, filter); ok {
		t.Fatal("reading persisted display data hydrated decision evidence")
	}
	after, err := os.ReadFile(cachePath)
	if err != nil || string(after) != string(data) {
		t.Fatal("read-only display loader rewrote persistent data")
	}
}

func TestCurrentDisplayThroughputReadsOldSchema3AndPreservesItsMeasurementTime(t *testing.T) {
	_, endpoint, filter, cachePath := currentDisplayThroughputFixture(t)
	measuredAt := time.Now().UTC().Add(-3 * time.Hour).Truncate(time.Second)
	legacy := bestServerCurrentQualityPersistentEntry{
		Schema: 3, IdentityHash: bestServerCurrentQualityIdentityHash(endpoint, filter),
		StoredAt: measuredAt.Format(time.RFC3339Nano),
		Candidate: currentDisplayThroughputCandidate(endpoint, 84.7),
	}
	data, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cachePath, data, 0600); err != nil {
		t.Fatal(err)
	}
	old := loadBestServerDisplayThroughput(endpoint, filter)
	if old == nil || old.Historical || old.DownloadMbps != 84.7 || old.MeasuredAt != legacy.StoredAt {
		t.Fatalf("optional schema-3 extension discarded an existing strict measurement: %+v", old)
	}
	if _, ok := loadBestServerCurrentQuality(endpoint, filter); ok {
		t.Fatal("old display cache became fresh decision evidence")
	}

	resetBestServerCurrentQualityMemory()
	// The first result after restart is partial. Retention must read the prior
	// envelope directly, not depend on an earlier browser hydration request.
	storeBestServerCurrentQuality(endpoint, filter, currentDisplayThroughputCandidate(endpoint, 0))
	resetBestServerCurrentQualityMemory()
	retained := loadBestServerDisplayThroughput(endpoint, filter)
	if retained == nil || !retained.Historical || retained.DownloadMbps != 84.7 || retained.MeasuredAt != legacy.StoredAt {
		t.Fatalf("partial first result after restart refreshed or lost the old measurement: %+v", retained)
	}

	// A later strict result replaces the old number even when it is slower and
	// independent RTT/jitter criteria make that current candidate ineligible.
	newGood := currentDisplayThroughputCandidate(endpoint, 71.5)
	newGood.ApplicationMS = bestServerQualityMaxApplicationMS + 1
	newGood.JitterMS = bestServerQualityHighJitterMS + 1
	newGood.Eligible = eligibleBestServerQuality(newGood)
	if newGood.Eligible {
		t.Fatal("fixture must have a measured speed but fail unrelated eligibility criteria")
	}
	newAt, fresh := storeBestServerCurrentQualityDisplaySnapshot(endpoint, filter, newGood)
	if fresh == nil || fresh.Historical || fresh.DownloadMbps != 71.5 || fresh.MeasuredAt != newAt.Format(time.RFC3339Nano) {
		t.Fatalf("last-good record did not advance to the actual new strict measurement: %+v", fresh)
	}
	if _, ok := loadBestServerCurrentQuality(endpoint, filter); ok {
		t.Fatal("display speed relaxed current candidate eligibility")
	}
	storeBestServerCurrentQuality(endpoint, filter, currentDisplayThroughputCandidate(endpoint, 0))
	if shown := loadBestServerDisplayThroughput(endpoint, filter); shown == nil || shown.DownloadMbps != 71.5 ||
		!shown.Historical || shown.MeasuredAt != fresh.MeasuredAt {
		t.Fatalf("retention chose maximum historical speed instead of last successful measurement: %+v", shown)
	}
}

func TestCurrentDisplayThroughputInvalidatesBothIdentityComponents(t *testing.T) {
	for _, component := range []string{"endpoint", "filter"} {
		t.Run(component, func(t *testing.T) {
			_, endpoint, filter, _ := currentDisplayThroughputFixture(t)
			storeBestServerCurrentQuality(endpoint, filter, currentDisplayThroughputCandidate(endpoint, 91.4))
			nextEndpoint, nextFilter := endpoint, filter
			if component == "endpoint" {
				nextEndpoint = "203.0.113.8:443"
			} else {
				nextFilter = "^different logical profile on the same endpoint$"
			}
			if got := loadBestServerDisplayThroughput(nextEndpoint, nextFilter); got != nil {
				t.Fatalf("last-good throughput crossed %s identity before new measurement: %+v", component, got)
			}
			storeBestServerCurrentQuality(nextEndpoint, nextFilter, currentDisplayThroughputCandidate(nextEndpoint, 0))
			resetBestServerCurrentQualityMemory()
			if got := loadBestServerDisplayThroughput(nextEndpoint, nextFilter); got != nil {
				t.Fatalf("partial result inherited speed from a different %s: %+v", component, got)
			}
			if got := loadBestServerDisplayThroughput(endpoint, filter); got != nil {
				t.Fatalf("replaced persistent identity still exposed old speed: %+v", got)
			}
		})
	}
}

func TestCurrentDisplayThroughputRejectsUnprovenNestedRecords(t *testing.T) {
	cases := []struct {
		name string
		mutate func(*bestServerStoredThroughput, time.Time)
	}{
		{"zero", func(s *bestServerStoredThroughput, _ time.Time) { s.DownloadMbps = 0 }},
		{"negative", func(s *bestServerStoredThroughput, _ time.Time) { s.DownloadMbps = -1 }},
		{"legacy fallback", func(s *bestServerStoredThroughput, _ time.Time) { s.ThroughputSource = bestServerThroughputCurrentFallback }},
		{"untrusted source", func(s *bestServerStoredThroughput, _ time.Time) { s.ThroughputSource = "https://private.example/?token=secret" }},
		{"invalid time", func(s *bestServerStoredThroughput, _ time.Time) { s.MeasuredAt = "not a timestamp" }},
		{"later than latest measurement", func(s *bestServerStoredThroughput, at time.Time) { s.MeasuredAt = at.Add(time.Minute).Format(time.RFC3339Nano) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, endpoint, filter, cachePath := currentDisplayThroughputFixture(t)
			latestAt := time.Now().UTC().Add(-time.Hour)
			saved := bestServerStoredThroughput{
				DownloadMbps: 91.4, MeasuredAt: latestAt.Add(-time.Hour).Format(time.RFC3339Nano),
				ThroughputSource: bestServerThroughputStrictAggregate,
			}
			tc.mutate(&saved, latestAt)
			entry := bestServerCurrentQualityPersistentEntry{
				Schema: bestServerCurrentQualityPersistentSchema,
				IdentityHash: bestServerCurrentQualityIdentityHash(endpoint, filter),
				StoredAt: latestAt.Format(time.RFC3339Nano),
				Candidate: currentDisplayThroughputCandidate(endpoint, 0), LastGoodThroughput: &saved,
			}
			data, err := json.Marshal(entry)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(cachePath, data, 0600); err != nil {
				t.Fatal(err)
			}
			if got := loadBestServerDisplayThroughput(endpoint, filter); got != nil {
				t.Fatalf("unproven historical record reached display: %+v", got)
			}
			if latest, _, ok := loadBestServerCurrentQualityForDisplay(endpoint, filter); !ok || latest.DownloadMbps != 0 {
				t.Fatal("invalid optional display data destroyed valid latest partial evidence")
			}
		})
	}
}

func TestCurrentDisplayThroughputCacheAPIKeepsActualAndHistoricalValuesSeparate(t *testing.T) {
	a, endpoint, filter, _ := currentDisplayThroughputFixture(t)
	storeBestServerCurrentQuality(endpoint, filter, currentDisplayThroughputCandidate(endpoint, 91.4))
	storeBestServerCurrentQuality(endpoint, filter, currentDisplayThroughputCandidate(endpoint, 0))
	resetBestServerCurrentQualityMemory()
	jobs := &bestServerJobs{}
	var scans atomic.Int32
	handler := jobs.wrap(a, "current", nil, func(context.Context) (bestServerQualityResponse, error) {
		scans.Add(1)
		return bestServerQualityResponse{}, nil
	})
	read := func() bestServerQualityResponse {
		t.Helper()
		w := httptest.NewRecorder()
		handler(w, httptest.NewRequest(http.MethodGet, "/api/vpn/current-quality?job=cache", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("cache HTTP status=%d", w.Code)
		}
		var response bestServerQualityResponse
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		return response
	}
	response := read()
	if !response.Success || response.Mutation != "NONE" || len(response.Candidates) != 1 ||
		response.Candidates[0].DownloadMbps != 0 || response.Candidates[0].Eligible ||
		response.DisplayThroughput == nil || !response.DisplayThroughput.Historical ||
		response.DisplayThroughput.DownloadMbps != 91.4 ||
		response.CurrentIdentity != bestServerCurrentQualityIdentityHash(endpoint, filter) {
		t.Fatalf("cache API mixed latest evidence and historical display: %+v", response)
	}
	if response.DisplayThroughput.ThroughputSource != bestServerThroughputStrictAggregate || response.ScannedAt == "" {
		t.Fatal("cache API omitted provenance or latest measurement timestamp")
	}
	oldIdentity := response.CurrentIdentity
	if err := os.WriteFile(a.cfg.FilterPath, []byte("^another profile$\n"), 0600); err != nil {
		t.Fatal(err)
	}
	response = read()
	if response.DisplayThroughput != nil || len(response.Candidates) != 0 || response.CurrentIdentity == oldIdentity {
		t.Fatalf("same endpoint with changed filter retained prior display identity: %+v", response)
	}
	if scans.Load() != 0 || len(a.sem) != 0 {
		t.Fatal("cache display started a probe or acquired the operation semaphore")
	}
}

func TestCurrentDisplayThroughputNeverPromotesLegacyFallback(t *testing.T) {
	_, endpoint, filter, _ := currentDisplayThroughputFixture(t)
	legacy := currentDisplayThroughputCandidate(endpoint, 0)
	legacy.FallbackDownloadMbps = 200
	legacy.ThroughputSource = bestServerThroughputCurrentFallback
	storeBestServerCurrentQuality(endpoint, filter, legacy)
	if got := loadBestServerDisplayThroughput(endpoint, filter); got != nil {
		t.Fatalf("legacy fallback became last-good strict speed: %+v", got)
	}
	storeBestServerCurrentQuality(endpoint, filter, currentDisplayThroughputCandidate(endpoint, 91.4))
	first := loadBestServerDisplayThroughput(endpoint, filter)
	storeBestServerCurrentQuality(endpoint, filter, legacy)
	got := loadBestServerDisplayThroughput(endpoint, filter)
	if got == nil || first == nil || got.DownloadMbps != 91.4 || !got.Historical || got.MeasuredAt != first.MeasuredAt {
		t.Fatalf("legacy fallback replaced a separately confirmed strict speed: %+v", got)
	}
}

func TestCurrentDisplayThroughputPersistsFailedLatestAndRetainsLastGood(t *testing.T) {
	a, endpoint, filter, cachePath := currentDisplayThroughputFixture(t)
	storeBestServerCurrentQuality(endpoint, filter, currentDisplayThroughputCandidate(endpoint, 91.4))
	good := loadBestServerDisplayThroughput(endpoint, filter)
	if good == nil {
		t.Fatal("test requires prior strict throughput")
	}
	failed := bestServerQualityCandidate{
		Current: true, Tested: true, Endpoint: endpoint, ID: "current-live", Name: "Current VPN",
	}
	markBestServerCurrentProbeFailure(&failed)
	originalReason := failed.Reason
	storedAt, historical := storeBestServerCurrentQualityDisplaySnapshot(endpoint, filter, failed)
	if storedAt.IsZero() || historical == nil || !historical.Historical || historical.DownloadMbps != 91.4 ||
		historical.MeasuredAt != good.MeasuredAt {
		t.Fatalf("zero-metric failure did not retain distinct last-good speed: %+v", historical)
	}
	latest, checkedAt, ok := loadBestServerCurrentQualityForDisplay(endpoint, filter)
	if !ok || latest.Available || latest.Eligible || latest.DownloadMbps != 0 || latest.TCPRTTMS != 0 ||
		latest.ApplicationMS != 0 || latest.Reason != originalReason || checkedAt != storedAt {
		t.Fatalf("failed latest was dropped in favor of prior success: %+v", latest)
	}
	if _, ok := loadBestServerCurrentQuality(endpoint, filter); ok {
		t.Fatal("failed latest remained decision evidence")
	}

	resetBestServerCurrentQualityMemory()
	response := a.currentVPNQualityCacheResponse()
	if !response.Success || response.Available || len(response.Candidates) != 1 ||
		response.Candidates[0].Available || response.Candidates[0].Eligible || response.Candidates[0].DownloadMbps != 0 ||
		response.Candidates[0].Reason != originalReason || response.ScannedAt != storedAt.Format(time.RFC3339Nano) ||
		response.DisplayThroughput == nil || !response.DisplayThroughput.Historical ||
		response.DisplayThroughput.DownloadMbps != 91.4 || response.DisplayThroughput.MeasuredAt != good.MeasuredAt {
		t.Fatalf("restart/cache response replaced actual failure with false health: %+v", response)
	}
	if strings.Contains(response.Message, "подтверждённый замер") {
		t.Fatal("failed latest was described as a confirmed measurement")
	}
	if _, ok := loadBestServerCurrentQuality(endpoint, filter); ok {
		t.Fatal("persisted failure/display speed hydrated decision evidence")
	}
	data, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatal(err)
	}
	var entry bestServerCurrentQualityPersistentEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		t.Fatal(err)
	}
	if entry.Candidate.Available || entry.Candidate.Reason != originalReason || entry.LastGoodThroughput == nil ||
		entry.LastGoodThroughput.MeasuredAt != good.MeasuredAt {
		t.Fatalf("persistent envelope lost failed-latest/last-good separation: %+v", entry)
	}
}

func TestCurrentDisplayThroughputRejectsNoncanonicalUnavailableCandidates(t *testing.T) {
	cases := []struct {
		name string
		mutate func(*bestServerQualityCandidate)
	}{
		{"not current", func(c *bestServerQualityCandidate) { c.Current = false }},
		{"not tested", func(c *bestServerQualityCandidate) { c.Tested = false }},
		{"empty endpoint", func(c *bestServerQualityCandidate) { c.Endpoint = "" }},
		{"different endpoint", func(c *bestServerQualityCandidate) { c.Endpoint = "203.0.113.9:443" }},
		{"eligible", func(c *bestServerQualityCandidate) { c.Eligible = true }},
		{"positive speed", func(c *bestServerQualityCandidate) { c.DownloadMbps = 91.4 }},
		{"fallback speed", func(c *bestServerQualityCandidate) { c.FallbackDownloadMbps = 42 }},
		{"fallback source", func(c *bestServerQualityCandidate) { c.ThroughputSource = bestServerThroughputCurrentFallback }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, endpoint, filter, cachePath := currentDisplayThroughputFixture(t)
			bad := bestServerQualityCandidate{Current: true, Tested: true, Endpoint: endpoint}
			markBestServerCurrentProbeFailure(&bad)
			tc.mutate(&bad)
			at, display := storeBestServerCurrentQualityDisplaySnapshot(endpoint, filter, bad)
			if !at.IsZero() || display != nil {
				t.Fatalf("noncanonical failure was stored: at=%v display=%+v", at, display)
			}
			if _, _, ok := loadBestServerCurrentQualityForDisplay(endpoint, filter); ok {
				t.Fatal("rejected failure reached the display cache")
			}
			// A pre-existing malformed envelope must be rejected before legacy
			// fallback stripping could make it resemble a zero-speed failure.
			entry := bestServerCurrentQualityPersistentEntry{
				Schema: bestServerCurrentQualityPersistentSchema,
				IdentityHash: bestServerCurrentQualityIdentityHash(endpoint, filter),
				StoredAt: time.Now().UTC().Format(time.RFC3339Nano), Candidate: bad,
			}
			data, err := json.Marshal(entry)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(cachePath, data, 0600); err != nil {
				t.Fatal(err)
			}
			if _, _, ok := loadBestServerCurrentQualityForDisplay(endpoint, filter); ok {
				t.Fatal("persistent loader accepted a noncanonical unavailable candidate")
			}
		})
	}
}
