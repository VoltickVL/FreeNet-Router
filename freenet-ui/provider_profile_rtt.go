package main

import (
	"context"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	providerProfileRTTWorkers          = 2
	providerProfileRTTDiscoveryTimeout = 30 * time.Second
)

type providerProfileRTTItem struct {
	ProfileID string `json:"profile_id"`
	RTTMS     int    `json:"rtt_ms,omitempty"`
	JitterMS  int    `json:"jitter_ms,omitempty"`
	Reachable bool   `json:"reachable"`
	Attempted bool   `json:"attempted"`
	Status    string `json:"status,omitempty"`
}

type providerProfileRTTResponse struct {
	Success         bool                     `json:"success"`
	Results         []providerProfileRTTItem `json:"results"`
	Profiles        int                      `json:"profiles"`
	UniqueEndpoints int                      `json:"unique_endpoints"`
	Checked         int                      `json:"checked"`
	Reachable       int                      `json:"reachable"`
	Unknown         int                      `json:"unknown,omitempty"`
	Partial         bool                     `json:"partial,omitempty"`
	ProbeMode       string                   `json:"probe_mode,omitempty"`
	Fresh           bool                     `json:"fresh"`
	Mutation        string                   `json:"mutation"`
	Error           string                   `json:"error,omitempty"`
}

type providerRTTProbe func(context.Context, bestServerInternalCandidate) bestServerProbeResult

var providerProfileRTTScanGate = make(chan struct{}, 1)

func providerProfileRTTStatusRank(item providerProfileRTTItem) int {
	switch {
	case item.Reachable:
		return 0
	case item.Status == "unknown":
		return 1
	default:
		return 2
	}
}

func sortProviderProfileRTTItems(items []providerProfileRTTItem) {
	sort.SliceStable(items, func(i, j int) bool {
		ri, rj := providerProfileRTTStatusRank(items[i]), providerProfileRTTStatusRank(items[j])
		if ri != rj {
			return ri < rj
		}
		if items[i].Reachable && items[i].RTTMS != items[j].RTTMS {
			return items[i].RTTMS < items[j].RTTMS
		}
		if items[i].Reachable && items[i].JitterMS != items[j].JitterMS {
			return items[i].JitterMS < items[j].JitterMS
		}
		return items[i].ProfileID < items[j].ProfileID
	})
}

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
	var completed atomic.Int32
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
				probeEndedByContext := probeCtx.Err() != nil
				cancel()
				item := providerProfileRTTItem{
					ProfileID: candidates[index].Profile.ID,
					Attempted: !probeEndedByContext,
					Status:    "unreachable",
				}
				switch {
				case probeEndedByContext:
					item.Status = "unknown"
				case value.OK:
					item.Reachable = true
					item.Status = "reachable"
					item.RTTMS = value.Median
					item.JitterMS = value.Jitter
				}
				results[index] = item
				done := int(completed.Add(1))
				reportBestServerProgress(ctx, "preflight", done, len(candidates))
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

	// A global deadline means UNKNOWN, not "server dead". Preserve identity and
	// expose that distinction to the UI instead of manufacturing a negative
	// measurement for work that never started.
	for index := range results {
		if results[index].ProfileID == "" {
			results[index].ProfileID = candidates[index].Profile.ID
			results[index].Status = "unknown"
		}
	}
	sortProviderProfileRTTItems(results)
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
			Success: false, Results: []providerProfileRTTItem{}, Fresh: false, ProbeMode: "logical_vpn_https_ip", Mutation: "NONE", Error: guardError,
		})
		return
	}
	defer releaseGuards()

	discoveryCtx, cancelDiscovery := context.WithTimeout(r.Context(), providerProfileRTTDiscoveryTimeout)
	all, _, _, err := a.discoverBestServerCandidates(discoveryCtx)
	cancelDiscovery()
	filtered := filterForeignBestServerCandidates(all)
	if len(filtered) == 0 {
		message := "VPN profile catalog is unavailable"
		if err == nil {
			message = "no selectable VPN profiles"
		}
		writeJSON(w, http.StatusServiceUnavailable, providerProfileRTTResponse{
			Success: false, Results: []providerProfileRTTItem{}, Fresh: false, ProbeMode: "logical_vpn_https_ip", Mutation: "NONE", Error: message,
		})
		return
	}

	sweepCtx, cancelSweep := context.WithTimeout(r.Context(), bestServerRTTSweepTimeout(len(filtered)))
	defer cancelSweep()
	items := measureProviderProfileRTT(sweepCtx, filtered, a.probeBestServerProfilePing)
	checked, reachable, unknown := 0, 0, 0
	for _, item := range items {
		if item.Attempted {
			checked++
		} else {
			unknown++
		}
		if item.Reachable {
			reachable++
		}
	}
	writeJSON(w, http.StatusOK, providerProfileRTTResponse{
		Success: true, Results: items, Profiles: len(filtered), UniqueEndpoints: countProviderUniqueEndpoints(filtered),
		Checked: checked, Reachable: reachable, Unknown: unknown, Partial: checked < len(filtered),
		ProbeMode: "logical_vpn_https_ip", Fresh: err == nil, Mutation: "NONE",
	})
}
