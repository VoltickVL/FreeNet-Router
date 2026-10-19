package main

import (
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "strings"
    "testing"
    "time"
)

// Status requests are short, read-only polls. A status lookup must never
// start a scan, accept a guessed identifier, or return another job.
func TestProviderRTTAsyncStatusRequiresExactJobIdentity(t *testing.T) {
    id := strings.Repeat("a", 32)
    providerRTTAsyncJobs.Lock()
    before := providerRTTAsyncJobs.job
    providerRTTAsyncJobs.job = &providerRTTAsyncJob{
        ID: id, State: "running", StartedAt: time.Now().UTC(),
        Stage: "preflight", Completed: 3, Total: 48,
    }
    providerRTTAsyncJobs.Unlock()
    defer func() {
        providerRTTAsyncJobs.Lock()
        providerRTTAsyncJobs.job = before
        providerRTTAsyncJobs.Unlock()
    }()

    a := &app{}
    ok := httptest.NewRecorder()
    a.handleProviderRTTJobStatus(ok, httptest.NewRequest(http.MethodGet, "http://router.example/api/provider-profiles/rtt/jobs?id="+id, nil))
    if ok.Code != http.StatusOK {
        t.Fatalf("job status HTTP=%d: %s", ok.Code, ok.Body.String())
    }
    var got providerRTTAsyncJob
    if err := json.Unmarshal(ok.Body.Bytes(), &got); err != nil {
        t.Fatal(err)
    }
    if got.ID != id || got.State != "running" || got.Completed != 3 || got.Total != 48 || got.Result != nil {
        t.Fatalf("RTT status must be progress-only and identity-bound: %+v", got)
    }
    if ok.Header().Get("Cache-Control") != "no-store" {
        t.Fatal("job polling must not cache stale progress")
    }
    invalid := httptest.NewRecorder()
    a.handleProviderRTTJobStatus(invalid, httptest.NewRequest(http.MethodGet, "http://router.example/api/provider-profiles/rtt/jobs?id=not-a-job", nil))
    if invalid.Code != http.StatusBadRequest {
        t.Fatalf("invalid job identifier HTTP=%d, want 400", invalid.Code)
    }
    missing := httptest.NewRecorder()
    a.handleProviderRTTJobStatus(missing, httptest.NewRequest(http.MethodGet, "http://router.example/api/provider-profiles/rtt/jobs?id="+strings.Repeat("b", 32), nil))
    if missing.Code != http.StatusNotFound {
        t.Fatalf("other job identifier HTTP=%d, want 404", missing.Code)
    }
}

func TestProviderRTTAsyncBusyDoesNotStartAnotherProbe(t *testing.T) {
    id := strings.Repeat("a", 32)
    providerRTTAsyncJobs.Lock()
    before := providerRTTAsyncJobs.job
    providerRTTAsyncJobs.job = &providerRTTAsyncJob{ID: id, State: "running", StartedAt: time.Now().UTC()}
    providerRTTAsyncJobs.Unlock()
    defer func() {
        providerRTTAsyncJobs.Lock()
        providerRTTAsyncJobs.job = before
        providerRTTAsyncJobs.Unlock()
    }()

    a := &app{}
    request := httptest.NewRequest(http.MethodPost, "http://router.example/api/provider-profiles/rtt/jobs", strings.NewReader("{}"))
    request.Header.Set("Origin", "http://router.example")
    writer := httptest.NewRecorder()
    a.handleProviderRTTJobStart(writer, request)
    if writer.Code != http.StatusConflict {
        t.Fatalf("concurrent RTT start HTTP=%d, want 409: %s", writer.Code, writer.Body.String())
    }
    if writer.Header().Get("Retry-After") != "4" {
        t.Fatalf("busy retry must be bounded: %q", writer.Header().Get("Retry-After"))
    }
    var payload map[string]string
    if err := json.Unmarshal(writer.Body.Bytes(), &payload); err != nil {
        t.Fatal(err)
    }
    if payload["error_code"] != "busy" || payload["mutation"] != "NONE" {
        t.Fatalf("busy RTT must not trigger a second scan or mutate VPN: %+v", payload)
    }
    providerRTTAsyncJobs.Lock()
    same := providerRTTAsyncJobs.job != nil && providerRTTAsyncJobs.job.ID == id && providerRTTAsyncJobs.job.State == "running"
    providerRTTAsyncJobs.Unlock()
    if !same {
        t.Fatal("busy RTT start replaced the running single-flight job")
    }
}
