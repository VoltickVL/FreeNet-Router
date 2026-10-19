package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"
)

// A full logical-profile RTT sweep can exceed the timeout of a remote HTTPS
// reverse proxy. The browser starts one explicit read-only job and polls short
// status responses. Polling never starts or extends any network probe.
const (
	providerRTTAsyncTimeout = 205 * time.Second
	providerRTTJobRetention = 5 * time.Minute
)

type providerRTTAsyncJob struct {
	ID        string                      `json:"id"`
	State     string                      `json:"state"`
	Stage     string                      `json:"stage,omitempty"`
	Completed int                         `json:"completed,omitempty"`
	Total     int                         `json:"total,omitempty"`
	StartedAt time.Time                   `json:"started_at"`
	Result    *providerProfileRTTResponse `json:"result,omitempty"`
	ErrorCode string                      `json:"error_code,omitempty"`
	Error     string                      `json:"error,omitempty"`
}

var providerRTTAsyncJobs struct {
	sync.Mutex
	job *providerRTTAsyncJob
}

func newProviderRTTJobID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(value[:]), nil
}

// Capture a legacy read-only handler response without keeping the original
// HTTP request/connection open. No session cookies are copied into the worker.
type providerRTTBufferWriter struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (w *providerRTTBufferWriter) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}
	return w.header
}
func (w *providerRTTBufferWriter) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
}
func (w *providerRTTBufferWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.body.Write(b)
}

func (a *app) handleProviderRTTJobStart(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "request rejected"})
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	providerRTTAsyncJobs.Lock()
	if providerRTTAsyncJobs.job != nil && providerRTTAsyncJobs.job.State == "running" {
		providerRTTAsyncJobs.Unlock()
		w.Header().Set("Retry-After", "4")
		writeJSON(w, http.StatusConflict, map[string]string{"error_code": "busy", "error": "VPN RTT measurement already running", "mutation": "NONE"})
		return
	}
	id, err := newProviderRTTJobID()
	if err != nil {
		providerRTTAsyncJobs.Unlock()
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "Cannot create read-only RTT job"})
		return
	}
	job := &providerRTTAsyncJob{ID: id, State: "running", Stage: "discovery", StartedAt: time.Now().UTC()}
	providerRTTAsyncJobs.job = job
	providerRTTAsyncJobs.Unlock()
	go a.executeProviderRTTJob(job)
	writeJSON(w, http.StatusAccepted, map[string]string{"id": id, "state": "running", "mutation": "NONE"})
}

// The runner is injectable only for isolated, no-router tests. Production
// always executes the existing safe single-flight RTT handler.
var providerRTTJobScan = func(a *app, ctx context.Context) (providerProfileRTTResponse, int, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1/api/provider-profiles/rtt?refresh=1", nil)
	writer := &providerRTTBufferWriter{}
	if err == nil {
		a.handleProviderProfilesRTT(writer, request)
	}
	var result providerProfileRTTResponse
	if err == nil {
		err = json.Unmarshal(writer.body.Bytes(), &result)
	}
	return result, writer.status, err
}

func (a *app) executeProviderRTTJob(job *providerRTTAsyncJob) {
	ctx, cancel := context.WithTimeout(context.Background(), providerRTTAsyncTimeout)
	defer cancel()
	ctx = context.WithValue(ctx, bestServerProgressKey{}, func(stage string, completed, total int) {
		providerRTTAsyncJobs.Lock()
		if providerRTTAsyncJobs.job == job {
			job.Stage, job.Completed, job.Total = stage, completed, total
		}
		providerRTTAsyncJobs.Unlock()
	})
	result, status, err := providerRTTJobScan(a, ctx)
	providerRTTAsyncJobs.Lock()
	defer providerRTTAsyncJobs.Unlock()
	if providerRTTAsyncJobs.job != job {
		return
	}
	job.State = "failed"
	if err != nil || status != http.StatusOK || !result.Success || result.Mutation != "NONE" {
		job.ErrorCode = "unavailable"
		job.Error = "RTT measurement did not finish safely"
		if status == http.StatusConflict && result.ErrorCode == "busy" {
			job.ErrorCode = "busy"
			job.Error = "AUTO VPN or another router operation is active"
		}
		if ctx.Err() != nil {
			job.ErrorCode = "timeout"
			job.Error = "RTT measurement time budget expired"
		}
		return
	}
	job.State, job.Result, job.Error, job.ErrorCode = "completed", &result, "", ""
}

func (a *app) handleProviderRTTJobStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if len(id) != 32 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid RTT job identifier"})
		return
	}
	if _, err := hex.DecodeString(id); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid RTT job identifier"})
		return
	}
	providerRTTAsyncJobs.Lock()
	var copyJob providerRTTAsyncJob
	ok := providerRTTAsyncJobs.job != nil && providerRTTAsyncJobs.job.ID == id
	if ok {
		copyJob = *providerRTTAsyncJobs.job
		ok = time.Since(copyJob.StartedAt) <= providerRTTAsyncTimeout+providerRTTJobRetention
	}
	providerRTTAsyncJobs.Unlock()
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "RTT job unavailable; no new scan was started"})
		return
	}
	writeJSON(w, http.StatusOK, copyJob)
}
