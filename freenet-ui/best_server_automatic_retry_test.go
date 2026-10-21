package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func automaticCurrentRetryTestApp(t *testing.T) *app {
	t.Helper()
	resetBestServerCurrentQualityMemory()
	t.Cleanup(resetBestServerCurrentQualityMemory)
	dir := t.TempDir()
	t.Setenv("FREENET_AUTO_HEALTH_LOCK", filepath.Join(dir, "auto-health.lock"))
	t.Setenv("FREENET_CURRENT_QUALITY_CACHE", filepath.Join(dir, "current-quality.json"))
	a := &app{cfg: config{
		OutPath: filepath.Join(dir, "outbounds.json"),
		FilterPath: filepath.Join(dir, "profile.regex"),
	}, sem: make(chan struct{}, 1)}
	if err := os.WriteFile(a.cfg.OutPath, []byte(`{"outbounds":[{"tag":"vless-reality","settings":{"vnext":[{"address":"192.0.2.24","port":443}]}}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(a.cfg.FilterPath, []byte("^Example Extra$\n"), 0600); err != nil {
		t.Fatal(err)
	}
	return a
}

func automaticCurrentRetryRequest(handler http.HandlerFunc, query string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	handler(w, httptest.NewRequest(http.MethodGet, "/api/vpn/current-quality?"+query, nil))
	return w
}

func waitAutomaticCurrentRetryJob(t *testing.T, handler http.HandlerFunc, id string) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for {
		w := automaticCurrentRetryRequest(handler, "job=status&id="+id)
		var job bestServerJob
		if w.Code != http.StatusAccepted || json.Unmarshal(w.Body.Bytes(), &job) != nil {
			t.Fatalf("poll: %d %s", w.Code, w.Body.String())
		}
		if job.State != "running" {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("automatic job did not finish")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestAutomaticCurrentRetrySurvivesReloadAndPreservesManualChecks(t *testing.T) {
	a := automaticCurrentRetryTestApp(t)
	jobs := &bestServerJobs{}
	var calls atomic.Int32
	handler := jobs.wrap(a, "current", nil, func(context.Context) (bestServerQualityResponse, error) {
		calls.Add(1)
		return bestServerQualityResponse{Success: true, Mutation: "NONE"}, nil
	})
	first := "automatic-current-check-0001"
	if w := automaticCurrentRetryRequest(handler, "job=start&id="+first+"&automatic=1"); w.Code != http.StatusAccepted {
		t.Fatalf("first automatic attempt: %d %s", w.Code, w.Body.String())
	}
	waitAutomaticCurrentRetryJob(t, handler, first)
	// A lost response rejoins the same job; a page reload uses a different ID.
	if w := automaticCurrentRetryRequest(handler, "job=start&id="+first+"&automatic=1"); w.Code != http.StatusAccepted {
		t.Fatalf("same job could not be rejoined: %d", w.Code)
	}
	w := automaticCurrentRetryRequest(handler, "job=start&id=automatic-current-check-0002&automatic=1")
	if w.Code != http.StatusConflict || w.Header().Get("Retry-After") == "" {
		t.Fatalf("reload bypassed retry limit: %d %s", w.Code, w.Body.String())
	}
	manual := "explicit-current-check-0003"
	if w := automaticCurrentRetryRequest(handler, "job=start&id="+manual); w.Code != http.StatusAccepted {
		t.Fatalf("automatic cooldown blocked explicit check: %d", w.Code)
	}
	waitAutomaticCurrentRetryJob(t, handler, manual)
	if calls.Load() != 2 || len(a.sem) != 0 {
		t.Fatalf("unexpected launches/fence: calls=%d sem=%d", calls.Load(), len(a.sem))
	}
	// A different logical profile at the same endpoint gets its own attempt.
	if err := os.WriteFile(a.cfg.FilterPath, []byte("^Different Extra$\n"), 0600); err != nil {
		t.Fatal(err)
	}
	other := "automatic-current-check-0004"
	if w := automaticCurrentRetryRequest(handler, "job=start&id="+other+"&automatic=1"); w.Code != http.StatusAccepted {
		t.Fatalf("different profile inherited cooldown: %d", w.Code)
	}
	waitAutomaticCurrentRetryJob(t, handler, other)
	if err := os.WriteFile(a.cfg.FilterPath, []byte("^Example Extra$\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if w := automaticCurrentRetryRequest(handler, "job=start&id=automatic-current-check-0005&automatic=1"); w.Code != http.StatusConflict {
		t.Fatalf("returning to a profile bypassed its cooldown: %d", w.Code)
	}
	if calls.Load() != 3 {
		t.Fatalf("unexpected scan count: %d", calls.Load())
	}
}

func TestAutomaticCurrentRetryHonorsBusyFenceWithoutConsumingAttempt(t *testing.T) {
	a := automaticCurrentRetryTestApp(t)
	jobs := &bestServerJobs{}
	var calls atomic.Int32
	handler := jobs.wrap(a, "current", nil, func(context.Context) (bestServerQualityResponse, error) {
		calls.Add(1)
		return bestServerQualityResponse{Success: true}, nil
	})
	release, err := acquireAutomationHealthLock()
	if err != nil {
		t.Fatal(err)
	}
	w := automaticCurrentRetryRequest(handler, "job=start&id=automatic-current-busy-0001&automatic=1")
	release()
	if w.Code != http.StatusConflict || calls.Load() != 0 || len(a.sem) != 0 {
		t.Fatalf("busy admission started work: status=%d calls=%d sem=%d", w.Code, calls.Load(), len(a.sem))
	}
	id := "automatic-current-busy-0002"
	if w := automaticCurrentRetryRequest(handler, "job=start&id="+id+"&automatic=1"); w.Code != http.StatusAccepted {
		t.Fatalf("rejected operation consumed retry: %d %s", w.Code, w.Body.String())
	}
	waitAutomaticCurrentRetryJob(t, handler, id)
}

func TestAutomaticCurrentRetryUsesLatestEvidenceNotHistoricalDisplaySpeed(t *testing.T) {
	a := automaticCurrentRetryTestApp(t)
	endpoint := readBestServerCurrentEndpoint(a.cfg.OutPath)
	filter := readBestServerCurrentFilter(a.cfg.FilterPath)
	good := bestServerQualityCandidate{
		Current: true, Tested: true, Available: true, Endpoint: endpoint,
		DownloadMbps: 91.4, ThroughputSource: bestServerThroughputStrictAggregate,
		ApplicationMS: 234, MediaSamples: 4, MediaGrade: "good",
	}
	storeBestServerCurrentQuality(endpoint, filter, good)
	jobs := &bestServerJobs{}
	var calls atomic.Int32
	handler := jobs.wrap(a, "current", nil, func(context.Context) (bestServerQualityResponse, error) {
		calls.Add(1)
		return bestServerQualityResponse{Success: true}, nil
	})
	w := automaticCurrentRetryRequest(handler, "job=start&id=automatic-measured-check-0001&automatic=1")
	if w.Code != http.StatusOK || calls.Load() != 0 || len(a.sem) != 0 {
		t.Fatalf("automatic request remeasured valid current speed: %d calls=%d", w.Code, calls.Load())
	}
	partial := good
	partial.DownloadMbps, partial.ThroughputSource, partial.MediaSamples = 0, "", 1
	storeBestServerCurrentQuality(endpoint, filter, partial)
	if display := loadBestServerDisplayThroughput(endpoint, filter); display == nil {
		t.Fatal("test requires a retained historical speed")
	}
	id := "automatic-measured-check-0002"
	if w := automaticCurrentRetryRequest(handler, "job=start&id="+id+"&automatic=1"); w.Code != http.StatusAccepted {
		t.Fatalf("historical speed incorrectly suppressed retry of failed latest check: %d %s", w.Code, w.Body.String())
	}
	waitAutomaticCurrentRetryJob(t, handler, id)
	if calls.Load() != 1 {
		t.Fatalf("retry count: %d", calls.Load())
	}
}

func TestAutomaticCurrentRetryAfterUnavailableLatestSurvivesCacheReload(t *testing.T) {
	a := automaticCurrentRetryTestApp(t)
	endpoint := readBestServerCurrentEndpoint(a.cfg.OutPath)
	filter := readBestServerCurrentFilter(a.cfg.FilterPath)
	good := bestServerQualityCandidate{
		Current: true, Tested: true, Available: true, Endpoint: endpoint,
		DownloadMbps: 91.4, ThroughputSource: bestServerThroughputStrictAggregate,
		ApplicationMS: 234, MediaSamples: 4, MediaGrade: "good",
	}
	storeBestServerCurrentQuality(endpoint, filter, good)
	measured := loadBestServerDisplayThroughput(endpoint, filter)
	if measured == nil {
		t.Fatal("test requires a prior strict speed measurement")
	}
	failed := bestServerQualityCandidate{Current: true, Tested: true, Endpoint: endpoint}
	markBestServerCurrentProbeFailure(&failed)
	failedAt, _ := storeBestServerCurrentQualityDisplaySnapshot(endpoint, filter, failed)
	resetBestServerCurrentQualityMemory()
	cache := a.currentVPNQualityCacheResponse()
	if cache.Available || len(cache.Candidates) != 1 || cache.Candidates[0].Available ||
		cache.Candidates[0].DownloadMbps != 0 || cache.ScannedAt != failedAt.Format(time.RFC3339Nano) ||
		cache.DisplayThroughput == nil || !cache.DisplayThroughput.Historical ||
		cache.DisplayThroughput.DownloadMbps != 91.4 || cache.DisplayThroughput.MeasuredAt != measured.MeasuredAt {
		t.Fatalf("cache reload lost failed latest or refreshed historical speed: %+v", cache)
	}
	jobs := &bestServerJobs{}
	var calls atomic.Int32
	handler := jobs.wrap(a, "current", nil, func(context.Context) (bestServerQualityResponse, error) {
		calls.Add(1)
		return bestServerQualityResponse{Success: true}, nil
	})
	id := "automatic-unavailable-check-0001"
	if w := automaticCurrentRetryRequest(handler, "job=start&id="+id+"&automatic=1"); w.Code != http.StatusAccepted {
		t.Fatalf("persisted old success suppressed retry after unavailable latest: %d %s", w.Code, w.Body.String())
	}
	waitAutomaticCurrentRetryJob(t, handler, id)
	w := automaticCurrentRetryRequest(handler, "job=start&id=automatic-unavailable-check-0002&automatic=1")
	if w.Code != http.StatusConflict || w.Header().Get("Retry-After") == "" || calls.Load() != 1 || len(a.sem) != 0 {
		t.Fatalf("unavailable latest caused repeated automatic scans: status=%d calls=%d sem=%d", w.Code, calls.Load(), len(a.sem))
	}
}

func TestAutomaticCurrentRetryExpiresAndIsNotAvailableForBestServer(t *testing.T) {
	a := automaticCurrentRetryTestApp(t)
	jobs := &bestServerJobs{}
	identity := bestServerCurrentQualityIdentityHash(readBestServerCurrentEndpoint(a.cfg.OutPath), readBestServerCurrentFilter(a.cfg.FilterPath))
	jobs.automaticCurrentAttempts = map[string]time.Time{identity: time.Now().Add(-bestServerAutomaticCurrentRetryInterval-time.Second)}
	var calls atomic.Int32
	scan := func(context.Context) (bestServerQualityResponse, error) {
		calls.Add(1)
		return bestServerQualityResponse{Success: true}, nil
	}
	bestHandler := jobs.wrap(a, "best", nil, scan)
	if w := automaticCurrentRetryRequest(bestHandler, "job=start&id=automatic-best-rejected-0001&automatic=1"); w.Code != http.StatusBadRequest || calls.Load() != 0 {
		t.Fatalf("automatic Best Server was admitted: %d calls=%d", w.Code, calls.Load())
	}
	handler := jobs.wrap(a, "current", nil, scan)
	id := "automatic-expired-check-0002"
	if w := automaticCurrentRetryRequest(handler, "job=start&id="+id+"&automatic=1"); w.Code != http.StatusAccepted {
		t.Fatalf("expired retry cooldown remained active: %d", w.Code)
	}
	waitAutomaticCurrentRetryJob(t, handler, id)
}

func TestAutomaticCurrentRetryCapacityDoesNotEvictActiveCooldowns(t *testing.T) {
	a := automaticCurrentRetryTestApp(t)
	jobs := &bestServerJobs{automaticCurrentAttempts: make(map[string]time.Time)}
	for i := 0; i < bestServerAutomaticCurrentRetryIdentities; i++ {
		jobs.automaticCurrentAttempts[fmt.Sprintf("recent-identity-%d", i)] = time.Now().Add(-time.Minute)
	}
	var calls atomic.Int32
	handler := jobs.wrap(a, "current", nil, func(context.Context) (bestServerQualityResponse, error) {
		calls.Add(1)
		return bestServerQualityResponse{Success: true}, nil
	})
	w := automaticCurrentRetryRequest(handler, "job=start&id=automatic-capacity-check-0001&automatic=1")
	if w.Code != http.StatusConflict || w.Header().Get("Retry-After") == "" || calls.Load() != 0 || len(a.sem) != 0 {
		t.Fatalf("full cooldown table admitted work: status=%d calls=%d sem=%d", w.Code, calls.Load(), len(a.sem))
	}
	for i := 0; i < bestServerAutomaticCurrentRetryIdentities; i++ {
		if _, ok := jobs.automaticCurrentAttempts[fmt.Sprintf("recent-identity-%d", i)]; !ok {
			t.Fatal("unexpired identity was evicted")
		}
	}
}

func TestCurrentQualityStatusIdentityMatchesActualEndpointAndProfile(t *testing.T) {
	a := automaticCurrentRetryTestApp(t)
	endpoint := readBestServerCurrentEndpoint(a.cfg.OutPath)
	filter := readBestServerCurrentFilter(a.cfg.FilterPath)
	want := bestServerCurrentQualityIdentityHash(endpoint, filter)
	if got := a.currentVPNQualityStatusIdentity(endpoint); got != want {
		t.Fatalf("status identity differs from current-quality identity: got=%q want=%q", got, want)
	}
	data, err := json.Marshal(statusResponse{Endpoint: endpoint, CurrentIdentity: a.currentVPNQualityStatusIdentity(endpoint)})
	if err != nil || strings.Contains(string(data), filter) || !strings.Contains(string(data), want) {
		t.Fatalf("invalid public identity serialization: %s err=%v", data, err)
	}
	if a.currentVPNQualityStatusIdentity("192.0.2.99:443") != "" {
		t.Fatal("status with a different endpoint acquired an identity")
	}
	if err := os.WriteFile(a.cfg.FilterPath, []byte("^Another Extra$\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if got := a.currentVPNQualityStatusIdentity(endpoint); got == "" || got == want {
		t.Fatal("same endpoint with a different logical profile retained identity")
	}
	if err := os.WriteFile(a.cfg.OutPath, []byte("invalid"), 0600); err != nil {
		t.Fatal(err)
	}
	if a.currentVPNQualityStatusIdentity("—") != "" || a.currentVPNQualityStatusIdentity(endpoint) != "" {
		t.Fatal("missing or invalid active endpoint must not have an identity")
	}
}
