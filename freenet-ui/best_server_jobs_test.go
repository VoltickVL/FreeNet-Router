package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestBestServerJobDetachedSingleFlightAndReadOnlyPoll(t *testing.T) {
	a := &app{sem: make(chan struct{}, 1)}
	jobs := &bestServerJobs{}
	release := make(chan struct{})
	defer close(release)
	var calls atomic.Int32
	scan := func(ctx context.Context) (bestServerQualityResponse, error) {
		calls.Add(1)
		reportBestServerProgress(ctx, "quality", 0, 1)
		select { case <-release: case <-ctx.Done(): return bestServerQualityResponse{}, ctx.Err() }
		return bestServerQualityResponse{Success: true, Mutation: "NONE", Candidates: []bestServerQualityCandidate{}}, nil
	}
	handler := jobs.wrap(a, "best", func(http.ResponseWriter, *http.Request) { t.Error("unexpected legacy scan") }, scan)
	request := func(action, id string, ctx context.Context) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		handler(w, httptest.NewRequest("GET", "/?job="+action+"&id="+id, nil).WithContext(ctx))
		return w
	}
	ctx, cancel := context.WithCancel(context.Background())
	w := request("start", "test-quality-job-0001", ctx)
	cancel()
	if w.Code != 202 { t.Fatalf("start: %d %s", w.Code, w.Body.String()) }
	if len(a.sem) != 1 { t.Fatal("job must hold operation exclusion") }
	if w := request("start", "test-quality-job-0002", context.Background()); w.Code != 409 { t.Fatalf("parallel start = %d", w.Code) }
	if w := request("status", "test-quality-job-0002", context.Background()); w.Code != 404 { t.Fatalf("unknown poll = %d", w.Code) }
	if w := request("start", "test-quality-job-0001", context.Background()); w.Code != 202 { t.Fatalf("same ID join = %d", w.Code) }
	deadline := time.Now().Add(time.Second)
	for calls.Load() == 0 && time.Now().Before(deadline) { time.Sleep(time.Millisecond) }
	if calls.Load() != 1 { t.Fatalf("expected one scan, got %d", calls.Load()) }
	w = request("status", "test-quality-job-0001", context.Background())
	var state bestServerJob
	if err := json.Unmarshal(w.Body.Bytes(), &state); err != nil { t.Fatal(err) }
	if state.State != "running" { t.Fatalf("request cancellation killed background job: %+v", state) }
}

func TestBestServerJobTerminalResultAndFailure(t *testing.T) {
	for _, fail := range []bool{false, true} {
		a := &app{sem: make(chan struct{}, 1)}
		jobs := &bestServerJobs{}
		handler := jobs.wrap(a, "current", nil, func(context.Context) (bestServerQualityResponse, error) {
			if fail { return bestServerQualityResponse{}, context.DeadlineExceeded }
			return bestServerQualityResponse{Success: true, Candidates: []bestServerQualityCandidate{}}, nil
		})
		w := httptest.NewRecorder()
		handler(w, httptest.NewRequest("GET", "/?job=start&id=test-quality-job-0001", nil))
		deadline := time.Now().Add(time.Second)
		for {
			w = httptest.NewRecorder()
			handler(w, httptest.NewRequest("GET", "/?job=status&id=test-quality-job-0001", nil))
			var state bestServerJob
			if err := json.Unmarshal(w.Body.Bytes(), &state); err != nil { t.Fatal(err) }
			if state.State != "running" {
				if fail && (state.State != "failed" || state.Error == "") { t.Fatalf("missing failure: %+v", state) }
				if !fail && (state.Result == nil || !state.Result.Success) { t.Fatalf("missing result: %+v", state) }
				if len(a.sem) != 0 { t.Fatal("operation exclusion leaked") }
				break
			}
			if time.Now().After(deadline) { t.Fatal("job never finished") }
			time.Sleep(time.Millisecond)
		}
	}
}

func TestBestServerEligibilityRejectsWeakEvidence(t *testing.T) {
	good := bestServerQualityCandidate{Available: true, DownloadMbps: 50, MediaSamples: 6, MediaGrade: "good", ServiceOK: 4, ServiceTotal: 4, JitterMS: 10, TCPJitterMS: 10}
	if !eligibleBestServerQuality(good) { t.Fatal("stable measured candidate rejected") }
	for _, change := range []func(*bestServerQualityCandidate){
		func(c *bestServerQualityCandidate) { c.MediaSamples = 4 },
		func(c *bestServerQualityCandidate) { c.MediaStalls = 1 },
		func(c *bestServerQualityCandidate) { c.ServiceOK = 3 },
		func(c *bestServerQualityCandidate) { c.DownloadMbps = 5 },
		func(c *bestServerQualityCandidate) { c.MediaGrade = "fair" },
		func(c *bestServerQualityCandidate) { c.JitterMS = 200 },
		func(c *bestServerQualityCandidate) { c.ServiceTotal = 0; c.ServiceOK = 0 },
	} {
		c := good; change(&c)
		if eligibleBestServerQuality(c) { t.Fatalf("weak candidate admitted: %+v", c) }
	}
}

func TestBestServerBudgetSkipsUnfinishableProbe(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	candidates := []bestServerInternalCandidate{{Profile: subscriptionProfile{ID: "example", Address: "192.0.2.1", Port: 443}}}
	result := rankBestServerQualityCandidates(ctx, candidates, 1, false, "", "",
		func(context.Context, subscriptionProfile) bestServerProbeResult { return bestServerProbeResult{OK: true, Median: 10} },
		func(context.Context, bestServerInternalCandidate) bestServerQualityApplicationResult { t.Error("probe without sufficient budget"); return bestServerQualityApplicationResult{} },
	)
	if !result.Partial || result.Available || result.Recommendation != nil { t.Fatalf("unfinished scan must be partial without a winner: %+v", result) }
	if ctx.Err() != nil { t.Fatal("budget guard should return before global expiry") }
}
