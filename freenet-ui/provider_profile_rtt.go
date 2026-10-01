package main

import (
	"context"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	providerProfileRTTTimeout = 90 * time.Second
	providerProfileRTTWorkers = 2
)

type providerProfileRTTItem struct {
	ProfileID string `json:"profile_id"`
	RTTMS     int    `json:"rtt_ms,omitempty"`
	JitterMS  int    `json:"jitter_ms,omitempty"`
	Reachable bool   `json:"reachable"`
}

type providerProfileRTTResponse struct {
	Success         bool                     `json:"success"`
	Results         []providerProfileRTTItem `json:"results"`
	Profiles        int                      `json:"profiles"`
	UniqueEndpoints int                      `json:"unique_endpoints"`
	ProbeMode       string                   `json:"probe_mode,omitempty"`
	Fresh           bool                     `json:"fresh"`
	Mutation        string                   `json:"mutation"`
	Error           string                   `json:"error,omitempty"`
}

type providerRTTProbe func(context.Context, bestServerInternalCandidate) bestServerProbeResult

var providerProfileRTTScanGate = make(chan struct{}, 1)

func beginProviderProfileRTTScan() bool {
	select {
	case providerProfileRTTScanGate <- struct{}{}:
		return true
	default:
		return false
	}
}

func endProviderProfileRTTScan() {
	select {
	case <-providerProfileRTTScanGate:
	default:
	}
}

func isUserExcludedVPNCountry(code string) bool {
	switch strings.ToLower(strings.TrimSpace(code)) {
	case "ru", "ua":
		return true
	default:
		return false
	}
}

// measureProviderProfileRTT intentionally measures every logical profile.
// Different VLESS/Reality profiles may share one public IP:port while routing
// through different exits. Endpoint TCP RTT therefore must never be copied from
// one logical profile to another.
func measureProviderProfileRTT(ctx context.Context, candidates []bestServerInternalCandidate, probe providerRTTProbe) []providerProfileRTTItem {
	if probe == nil {
		return []providerProfileRTTItem{}
	}
	results := make([]providerProfileRTTItem, len(candidates))
	jobs := make(chan int)
	workers := providerProfileRTTWorkers
	if workers > len(candidates) {
		workers = len(candidates)
	}
	var wg sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for index := range jobs {
				if ctx.Err() != nil {
					continue
				}
				probeCtx, cancel := context.WithTimeout(ctx, bestServerProfilePingTimeout)
				value := probe(probeCtx, candidates[index])
				cancel()
				item := providerProfileRTTItem{ProfileID: candidates[index].Profile.ID}
				if value.OK {
					item.Reachable = true
					item.RTTMS = value.Median
					item.JitterMS = value.Jitter
				}
				results[index] = item
			}
		}()
	}
	go func() {
		defer close(jobs)
		for index := range candidates {
			select {
			case jobs <- index:
			case <-ctx.Done():
				return
			}
		}
	}()
	wg.Wait()

	// Preserve IDs for jobs that could not start before the bounded deadline.
	for index := range results {
		if results[index].ProfileID == "" {
			results[index].ProfileID = candidates[index].Profile.ID
		}
	}
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].Reachable != results[j].Reachable {
			return results[i].Reachable
		}
		if results[i].RTTMS != results[j].RTTMS {
			return results[i].RTTMS < results[j].RTTMS
		}
		return results[i].ProfileID < results[j].ProfileID
	})
	return results
}

func countProviderUniqueEndpoints(candidates []bestServerInternalCandidate) int {
	seen := map[string]struct{}{}
	for _, candidate := range candidates {
		if endpoint := strings.TrimSpace(profileEndpoint(candidate.Profile)); endpoint != "" {
			seen[endpoint] = struct{}{}
		}
	}
	return len(seen)
}

func acquireProviderProfileRTTGuards(a *app) (func(), string) {
	releaseAutomationFence, fenceErr := acquireAutomationHealthLock()
	if fenceErr != nil {
		return nil, "AUTO VPN health/recovery operation is already running"
	}
	releaseOperation, operationOK := tryAcquireFreeNetOperation(a)
	if !operationOK {
		releaseAutomationFence()
		return nil, "another FreeNet operation is already running"
	}
	if !beginProviderProfileRTTScan() {
		releaseOperation()
		releaseAutomationFence()
		return nil, "VPN ping is already running"
	}
	return func() {
		endProviderProfileRTTScan()
		releaseOperation()
		releaseAutomationFence()
	}, ""
}

func (a *app) handleProviderProfilesRTT(w http.ResponseWriter, r *http.Request) {
	releaseGuards, guardError := acquireProviderProfileRTTGuards(a)
	if releaseGuards == nil {
		writeJSON(w, http.StatusConflict, providerProfileRTTResponse{
			Success: false, Results: []providerProfileRTTItem{}, Fresh: false, ProbeMode: "proxy_http", Mutation: "NONE", Error: guardError,
		})
		return
	}
	defer releaseGuards()

	ctx, cancel := context.WithTimeout(r.Context(), providerProfileRTTTimeout)
	defer cancel()

	all, _, _, err := a.discoverBestServerCandidates(ctx)
	filtered := filterForeignBestServerCandidates(all)
	if len(filtered) == 0 {
		message := "VPN profile catalog is unavailable"
		if err == nil {
			message = "no selectable VPN profiles"
		}
		writeJSON(w, http.StatusServiceUnavailable, providerProfileRTTResponse{
			Success: false, Results: []providerProfileRTTItem{}, Fresh: false, ProbeMode: "proxy_http", Mutation: "NONE", Error: message,
		})
		return
	}

	items := measureProviderProfileRTT(ctx, filtered, a.probeBestServerProfilePing)
	writeJSON(w, http.StatusOK, providerProfileRTTResponse{
		Success: true, Results: items, Profiles: len(filtered), UniqueEndpoints: countProviderUniqueEndpoints(filtered),
		ProbeMode: "proxy_http", Fresh: err == nil, Mutation: "NONE",
	})
}
