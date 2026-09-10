package main

import (
	"context"
	"net/http"
	"regexp"
	"sync"
	"time"
)

const bestServerAsyncJobTimeout = 165 * time.Second

// One bounded, read-only quality job per app. Polling never starts a scan.
// Retain only the latest result, with no credentials or raw probe output.
type bestServerJob struct {
	ID        string                     `json:"id"`
	Mode      string                     `json:"mode"`
	State     string                     `json:"state"`
	Stage     string                     `json:"stage"`
	Completed int                        `json:"completed"`
	Total     int                        `json:"total"`
	StartedAt time.Time                  `json:"started_at"`
	Result    *bestServerQualityResponse `json:"result,omitempty"`
	Error     string                     `json:"error,omitempty"`
}

type bestServerJobs struct {
	mu  sync.Mutex
	job *bestServerJob
}

type bestServerProgressKey struct{}

func reportBestServerProgress(ctx context.Context, stage string, completed, total int) {
	if update, ok := ctx.Value(bestServerProgressKey{}).(func(string, int, int)); ok {
		update(stage, completed, total)
	}
}

var bestServerJobID = regexp.MustCompile(`^[a-zA-Z0-9_-]{16,64}$`)

func (jobs *bestServerJobs) wrap(a *app, mode string, legacy http.HandlerFunc, scan func(context.Context) (bestServerQualityResponse, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		action := r.URL.Query().Get("job")
		if action == "" {
			legacy(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		id := r.URL.Query().Get("id")
		if !bestServerJobID.MatchString(id) || (action != "start" && action != "status") {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Invalid quality job request"})
			return
		}
		jobs.mu.Lock()
		defer jobs.mu.Unlock()
		if jobs.job != nil && jobs.job.ID == id && jobs.job.Mode == mode {
			writeJSON(w, http.StatusAccepted, jobs.job)
			return
		}
		if action == "status" {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "Quality job no longer exists; no new scan was started"})
			return
		}
		if jobs.job != nil && jobs.job.State == "running" {
			writeJSON(w, http.StatusConflict, map[string]any{"error": "Another quality check is running"})
			return
		}
		select {
		case a.sem <- struct{}{}:
		default:
			writeJSON(w, http.StatusConflict, map[string]any{"error": "Another operation is active; scan was not started"})
			return
		}
		job := &bestServerJob{ID: id, Mode: mode, State: "running", Stage: "discovery", StartedAt: time.Now().UTC()}
		jobs.job = job
		go func() {
			timeout := bestServerAsyncJobTimeout
			if mode == "current" {
				timeout = bestServerCurrentScanTimeout
			}
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()
			ctx = context.WithValue(ctx, bestServerProgressKey{}, func(stage string, completed, total int) {
				jobs.mu.Lock()
				defer jobs.mu.Unlock()
				job.Stage, job.Completed, job.Total = stage, completed, total
			})
			result, err := scan(ctx)
			<-a.sem
			jobs.mu.Lock()
			defer jobs.mu.Unlock()
			job.State = "completed"
			if err != nil {
				job.State, job.Error = "failed", safeBestServerError(err)
			} else {
				job.Result = &result
			}
		}()
		writeJSON(w, http.StatusAccepted, job)
	}
}
