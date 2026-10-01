package main

import (
	"context"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

const providerProfileRTTTimeout = 8 * time.Second

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
	Fresh           bool                     `json:"fresh"`
	Mutation        string                   `json:"mutation"`
	Error           string                   `json:"error,omitempty"`
}

type providerRTTProbe func(context.Context, subscriptionProfile) bestServerProbeResult

func isUserExcludedVPNCountry(code string) bool {
	switch strings.ToLower(strings.TrimSpace(code)) {
	case "ru", "ua":
		return true
	default:
		return false
	}
}

func measureProviderProfileRTT(ctx context.Context, profiles []subscriptionProfile, probe providerRTTProbe) ([]providerProfileRTTItem, int) {
	if probe == nil {
		probe = defaultBestServerFastRTTProbe
	}
	type endpointGroup struct {
		endpoint string
		profile  subscriptionProfile
	}
	groups := make([]endpointGroup, 0, len(profiles))
	seen := map[string]bool{}
	for _, profile := range profiles {
		endpoint := profileEndpoint(profile)
		if endpoint == "" || seen[endpoint] {
			continue
		}
		seen[endpoint] = true
		groups = append(groups, endpointGroup{endpoint: endpoint, profile: profile})
	}

	type endpointResult struct {
		endpoint string
		probe    bestServerProbeResult
	}
	jobs := make(chan endpointGroup)
	results := make(chan endpointResult, len(groups))
	workers := bestServerFastRTTWorkers
	if workers > len(groups) {
		workers = len(groups)
	}
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for group := range jobs {
				if ctx.Err() != nil {
					continue
				}
				results <- endpointResult{endpoint: group.endpoint, probe: probe(ctx, group.profile)}
			}
		}()
	}
	go func() {
		defer close(jobs)
		for _, group := range groups {
			select {
			case jobs <- group:
			case <-ctx.Done():
				return
			}
		}
	}()
	wg.Wait()
	close(results)

	byEndpoint := map[string]bestServerProbeResult{}
	for result := range results {
		byEndpoint[result.endpoint] = result.probe
	}
	items := make([]providerProfileRTTItem, 0, len(profiles))
	for _, profile := range profiles {
		probeResult, ok := byEndpoint[profileEndpoint(profile)]
		item := providerProfileRTTItem{ProfileID: profile.ID}
		if ok && probeResult.OK {
			item.Reachable = true
			item.RTTMS = probeResult.Median
			item.JitterMS = probeResult.Jitter
		}
		items = append(items, item)
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Reachable != items[j].Reachable {
			return items[i].Reachable
		}
		if items[i].RTTMS != items[j].RTTMS {
			return items[i].RTTMS < items[j].RTTMS
		}
		return items[i].ProfileID < items[j].ProfileID
	})
	return items, len(groups)
}

func (a *app) handleProviderProfilesRTT(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), providerProfileRTTTimeout)
	defer cancel()

	catalog, err := a.subscriptionProfilesForRead(ctx)
	profiles := selectableSubscriptionProfiles(catalog.Profiles)
	filtered := make([]subscriptionProfile, 0, len(profiles))
	for _, profile := range profiles {
		if isUserExcludedVPNCountry(profile.CountryCode) {
			continue
		}
		filtered = append(filtered, profile)
	}
	if len(filtered) == 0 {
		message := "VPN profile catalog is unavailable"
		if err == nil {
			message = "no selectable VPN profiles"
		}
		writeJSON(w, http.StatusServiceUnavailable, providerProfileRTTResponse{
			Success: false, Results: []providerProfileRTTItem{}, Fresh: false, Mutation: "NONE", Error: message,
		})
		return
	}

	items, uniqueEndpoints := measureProviderProfileRTT(ctx, filtered, defaultBestServerFastRTTProbe)
	writeJSON(w, http.StatusOK, providerProfileRTTResponse{
		Success: true, Results: items, Profiles: len(filtered), UniqueEndpoints: uniqueEndpoints,
		Fresh: !catalog.Stale && err == nil, Mutation: "NONE",
	})
}
