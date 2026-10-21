package main

import (
	"context"
	"net/http"
	"regexp"
	"strconv"
	"sync"
	"time"
)

const bestServerAsyncJobTimeout = 360 * time.Second
const bestServerAutomaticCurrentRetryInterval = 10 * time.Minute
const bestServerAutomaticCurrentRetryIdentities = 8

// One bounded, read-only quality job per app. Polling never starts a scan.
// The Best Server job is intentionally capped at 360 s. Canonical VPN RTT ranking scans
// the pool first; deep checks then spend the remaining budget on the strongest
// candidates and return measured partial results rather than holding the UI for
// the bounded worst-case window. Current-quality remains a separate explicit operation.
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
	automaticCurrentAttempts map[string]time.Time
}

// Called with jobs.mu held. Automatic retries are bounded across page reloads
// and tabs; explicit checks keep their existing admission rules.
func (jobs *bestServerJobs) automaticCurrentRetryRemaining(identity string, now time.Time) time.Duration {
	for key, attemptedAt := range jobs.automaticCurrentAttempts {
		if !now.Before(attemptedAt.Add(bestServerAutomaticCurrentRetryInterval)) {
			delete(jobs.automaticCurrentAttempts, key)
		}
	}
	if attemptedAt, ok := jobs.automaticCurrentAttempts[identity]; ok {
		return attemptedAt.Add(bestServerAutomaticCurrentRetryInterval).Sub(now)
	}
	if len(jobs.automaticCurrentAttempts) >= bestServerAutomaticCurrentRetryIdentities {
		// Keep every still-active identity's limit. A ninth profile waits for
		// the earliest slot instead of evicting evidence of a recent attempt.
		var earliest time.Duration
		for _, attemptedAt := range jobs.automaticCurrentAttempts {
			remaining := attemptedAt.Add(bestServerAutomaticCurrentRetryInterval).Sub(now)
			if earliest == 0 || remaining < earliest {
				earliest = remaining
			}
		}
		return earliest
	}
	return 0
}

func (jobs *bestServerJobs) recordAutomaticCurrentAttempt(identity string, now time.Time) {
	if jobs.automaticCurrentAttempts == nil {
		jobs.automaticCurrentAttempts = make(map[string]time.Time)
	}
	// automaticCurrentRetryRemaining already reserved capacity under jobs.mu.
	jobs.automaticCurrentAttempts[identity] = now
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
		if action == "cache" {
			if mode != "current" {
				writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Display cache is only available for current VPN quality"})
				return
			}
			writeJSON(w, http.StatusOK, a.currentVPNQualityCacheResponse())
			return
		}
		id := r.URL.Query().Get("id")
		if !bestServerJobID.MatchString(id) || (action != "start" && action != "status") {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Invalid quality job request"})
			return
		}
		automatic := r.URL.Query().Get("automatic")
		if automatic != "" && (automatic != "1" || mode != "current" || action != "start") {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Automatic retry is only available for a current VPN check"})
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
		releaseAutomationFence, fenceErr := acquireAutomationHealthLock()
		if fenceErr != nil {
			writeJSON(w, http.StatusConflict, map[string]any{"error": "AUTO VPN health/recovery operation is active; scan was not started"})
			return
		}
		select {
		case a.sem <- struct{}{}:
		default:
			releaseAutomationFence()
			writeJSON(w, http.StatusConflict, map[string]any{"error": "Another operation is active; scan was not started"})
			return
		}
		if automatic == "1" {
			// Read identity only after both operation fences are held. A retry
			// cannot be charged to a profile that changed while waiting.
			endpoint := readBestServerCurrentEndpoint(a.cfg.OutPath)
			filter := readBestServerCurrentFilter(a.cfg.FilterPath)
			if endpoint == "" {
				<-a.sem
				releaseAutomationFence()
				writeJSON(w, http.StatusConflict, map[string]any{"error": "Current VPN is not ready for a speed check"})
				return
			}
			if latest, _, ok := loadBestServerCurrentQualityForDisplay(endpoint, filter); ok &&
				latest.DownloadMbps > 0 && latest.ThroughputSource == bestServerThroughputStrictAggregate {
				<-a.sem
				releaseAutomationFence()
				writeJSON(w, http.StatusOK, a.currentVPNQualityCacheResponse())
				return
			}
			now := time.Now().UTC()
			identity := bestServerCurrentQualityIdentityHash(endpoint, filter)
			if remaining := jobs.automaticCurrentRetryRemaining(identity, now); remaining > 0 {
				<-a.sem
				releaseAutomationFence()
				w.Header().Set("Retry-After", strconv.Itoa(int((remaining+time.Second-1)/time.Second)))
				writeJSON(w, http.StatusConflict, map[string]any{"error": "Automatic speed retry is cooling down"})
				return
			}
			jobs.recordAutomaticCurrentAttempt(identity, now)
		}
		job := &bestServerJob{ID: id, Mode: mode, State: "running", Stage: "discovery", StartedAt: time.Now().UTC()}
		jobs.job = job
		go func() {
			fenceReleased := false
			defer func() {
				if !fenceReleased {
					releaseAutomationFence()
				}
			}()
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
			// Terminal job state is observable by pollers. Release every
			// operation fence before publishing terminal so callers never see
			// "completed" while AUTO recovery is still excluded.
			releaseAutomationFence()
			fenceReleased = true
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
