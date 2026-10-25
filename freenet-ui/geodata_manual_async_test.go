package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func decodeManualGeoDataResponse(t *testing.T, recorder *httptest.ResponseRecorder) manualGeoDataOperationResponse {
	t.Helper()
	var out manualGeoDataOperationResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &out); err != nil {
		t.Fatalf("invalid GeoData operation response: %v", err)
	}
	return out
}

func waitManualGeoDataTerminal(t *testing.T, a *app) manualGeoDataOperationResponse {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		rec := httptest.NewRecorder()
		a.handleGeoDataManualStatus(rec, httptest.NewRequest(http.MethodGet, "/api/settings-v3/geodata-operation", nil))
		if rec.Code != http.StatusOK || rec.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("status must be read-only, uncached HTTP 200: code=%d", rec.Code)
		}
		status := decodeManualGeoDataResponse(t, rec)
		if status.Operation != nil && !status.Active {
			return status
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("manual GeoData operation did not become terminal")
	return manualGeoDataOperationResponse{}
}

func TestManualGeoDataHTTPReturnsAcceptedAndIgnoresClientDisconnect(t *testing.T) {
	manualGeoDataUpdates = operationCoordinator{}
	old := manualGeoDataExecutor
	defer func() { manualGeoDataExecutor = old; manualGeoDataUpdates = operationCoordinator{} }()

	started := make(chan context.Context, 1)
	release := make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	var calls atomic.Int32
	manualGeoDataExecutor = func(_ *app, ctx context.Context) error {
		calls.Add(1)
		started <- ctx
		<-release
		return ctx.Err()
	}

	a := &app{}
	clientCtx, cancelRequest := context.WithCancel(context.Background())
	start := time.Now()
	first := httptest.NewRecorder()
	a.handleGeoDataManualStart(first, httptest.NewRequest(http.MethodPost, "/api/settings-v3/action", nil).WithContext(clientCtx))
	if first.Code != http.StatusAccepted || time.Since(start) > time.Second {
		t.Fatalf("long-running native GeoData must not block HTTP: code=%d elapsed=%v", first.Code, time.Since(start))
	}
	body := decodeManualGeoDataResponse(t, first)
	if !body.Success || !body.Active || body.Operation == nil {
		t.Fatalf("expected accepted active operation, got %+v", body)
	}
	var nativeContext context.Context
	select {
	case nativeContext = <-started:
	case <-time.After(time.Second):
		t.Fatal("GeoData worker did not start")
	}
	cancelRequest()
	if err := nativeContext.Err(); err != nil {
		t.Fatalf("client disconnect must not cancel native XKeen: %v", err)
	}

	duplicate := httptest.NewRecorder()
	a.handleGeoDataManualStart(duplicate, httptest.NewRequest(http.MethodPost, "/api/settings-v3/action", nil))
	dup := decodeManualGeoDataResponse(t, duplicate)
	if duplicate.Code != http.StatusAccepted || !dup.Active || dup.Operation == nil || dup.Operation.ID != body.Operation.ID || calls.Load() != 1 {
		t.Fatalf("duplicate manual click must attach to the same active run without another mutation: %+v, calls=%d", dup, calls.Load())
	}

	close(release)
	terminal := waitManualGeoDataTerminal(t, a)
	if terminal.Operation.State != "success" || terminal.Operation.ID != body.Operation.ID || calls.Load() != 1 {
		t.Fatalf("expected one successful detached execution: %+v calls=%d", terminal, calls.Load())
	}
}

func TestManualGeoDataStatusNeverExposesNativeSecrets(t *testing.T) {
	manualGeoDataUpdates = operationCoordinator{}
	old := manualGeoDataExecutor
	defer func() { manualGeoDataExecutor = old; manualGeoDataUpdates = operationCoordinator{} }()
	manualGeoDataExecutor = func(_ *app, _ context.Context) error {
		return errors.New("native failed: subscription=https://example.invalid/uuid token=my-secret")
	}
	a := &app{}
	start := httptest.NewRecorder()
	a.handleGeoDataManualStart(start, httptest.NewRequest(http.MethodPost, "/api/settings-v3/action", nil))
	if start.Code != http.StatusAccepted {
		t.Fatalf("unexpected start status %d", start.Code)
	}
	status := waitManualGeoDataTerminal(t, a)
	if status.Operation.State != "failed" || status.Operation.Error == "" ||
		strings.Contains(status.Operation.Error, "example.invalid") || strings.Contains(status.Operation.Error, "my-secret") ||
		strings.Contains(status.Operation.Error, "subscription") {
		t.Fatalf("manual status failed to redact untrusted error output: %+v", status)
	}
	for _, err := range []error{context.DeadlineExceeded, context.Canceled} {
		if reason := manualGeoDataSafeError(err); reason == "" || strings.Contains(reason, "context") {
			t.Fatalf("timeout/cancel must have a bounded, localized reason: %q", reason)
		}
	}
}

func TestManualGeoDataStatusDoesNotInventSuccessAfterRestart(t *testing.T) {
	manualGeoDataUpdates = operationCoordinator{}
	a := &app{}
	rec := httptest.NewRecorder()
	a.handleGeoDataManualStatus(rec, httptest.NewRequest(http.MethodGet, "/api/settings-v3/geodata-operation", nil))
	s := decodeManualGeoDataResponse(t, rec)
	if rec.Code != http.StatusOK || s.Active || s.Operation != nil {
		t.Fatalf("missing in-memory operation cannot be treated as success: %+v", s)
	}
}

func TestGeoDataNativeExecutorReportsCancellationWithoutTranscript(t *testing.T) {
	dir := t.TempDir()
	script := dir + "/xkeen"
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf 'subscription=https://example.invalid/uuid\\n'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := runVerifiedGeoDataXKeen(ctx, script)
	var native *geoDataNativeFailure
	if !errors.As(err, &native) || native.kind != "canceled" ||
		strings.Contains(err.Error(), "example.invalid") {
		t.Fatalf("cancelled native run must emit fixed reason only: %v", err)
	}
}

func TestManualGeoDataUsesShortStartAndPolledBrowserStatus(t *testing.T) {
	ui, err := os.ReadFile("web/settings-v3-core.js")
	if err != nil {
		t.Fatal(err)
	}
	backend, err := os.ReadFile("settings_v3.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{
		"startGeoDataOperation()",
		"pollGeoDataOperation(data.operation.id)",
		"resumeGeoDataOperation()",
		"/api/settings-v3/geodata-operation",
		"GeoData: статус неизвестен",
	} {
		if !strings.Contains(string(ui), required) {
			t.Fatalf("GeoData UI must handle async state rather than awaiting XKeen: missing %q", required)
		}
	}
	for _, required := range []string{
		"GET /api/settings-v3/geodata-operation",
		"a.handleGeoDataManualStart(w, r)",
	} {
		if !strings.Contains(string(backend), required) {
			t.Fatalf("GeoData transport does not register async route %q", required)
		}
	}
	if strings.Contains(string(backend), "err = a.runV3GeoData(ctx); message") {
		t.Fatal("manual GeoData request must not block behind native XKeen")
	}
}
