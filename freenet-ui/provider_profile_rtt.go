package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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
	providerProfileRTTCacheTTL         = 5 * time.Minute
)

type providerProfileRTTItem struct {
	ProfileID string `json:"profile_id"`
	Endpoint  string `json:"endpoint"`
	RTTMS     int    `json:"rtt_ms,omitempty"`
	JitterMS  int    `json:"jitter_ms,omitempty"`
	Reachable bool   `json:"reachable"`
	Attempted bool   `json:"attempted"`
	Status    string `json:"status,omitempty"`
}

type providerProfileRTTResponse struct {
	Success         bool                     `json:"success"`
	Cached          bool                     `json:"cached,omitempty"`
	MeasuredAt      string                   `json:"measured_at,omitempty"`
	Catalog         []subscriptionProfile    `json:"catalog,omitempty"`
	CatalogKey      string                   `json:"catalog_key,omitempty"`
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

type providerProfileRTTCacheEntry struct {
	StoredAt time.Time
	Item     providerProfileRTTItem
}

var providerProfileRTTCache struct {
	sync.Mutex
	Entries map[string]providerProfileRTTCacheEntry
}

func providerProfileRTTCacheCandidateKey(candidate bestServerInternalCandidate) string {
	id := strings.TrimSpace(candidate.Profile.ID)
	endpoint := strings.TrimSpace(profileEndpoint(candidate.Profile))
	raw := strings.TrimSpace(candidate.Raw)
	if id == "" || endpoint == "" || raw == "" {
		return ""
	}
	rawHash := sha256.Sum256([]byte(raw))
	return id + "|" + endpoint + "|" + hex.EncodeToString(rawHash[:8])
}

func cloneProviderProfileRTTItems(items []providerProfileRTTItem) []providerProfileRTTItem {
	return append([]providerProfileRTTItem(nil), items...)
}

func storeProviderProfileRTTCache(candidates []bestServerInternalCandidate, items []providerProfileRTTItem) {
	if len(candidates) == 0 || len(items) == 0 {
		return
	}
	byID := make(map[string]providerProfileRTTItem, len(items))
	for _, item := range items {
		if strings.TrimSpace(item.ProfileID) == "" || !item.Attempted || item.Status == "unknown" {
			continue
		}
		byID[item.ProfileID] = item
	}
	if len(byID) == 0 {
		return
	}
	now := time.Now().UTC()
	providerProfileRTTCache.Lock()
	defer providerProfileRTTCache.Unlock()
	if providerProfileRTTCache.Entries == nil {
		providerProfileRTTCache.Entries = map[string]providerProfileRTTCacheEntry{}
	}
	for _, candidate := range candidates {
		key := providerProfileRTTCacheCandidateKey(candidate)
		item, ok := byID[candidate.Profile.ID]
		if key == "" || !ok {
			continue
		}
		providerProfileRTTCache.Entries[key] = providerProfileRTTCacheEntry{StoredAt: now, Item: item}
	}
	for key, entry := range providerProfileRTTCache.Entries {
		if entry.StoredAt.IsZero() || now.Sub(entry.StoredAt) > providerProfileRTTCacheTTL {
			delete(providerProfileRTTCache.Entries, key)
		}
	}
}

func splitProviderProfileRTTCache(candidates []bestServerInternalCandidate) (map[string]providerProfileRTTItem, []bestServerInternalCandidate, time.Time) {
	cached := make(map[string]providerProfileRTTItem, len(candidates))
	missing := make([]bestServerInternalCandidate, 0, len(candidates))
	now := time.Now().UTC()
	oldest := time.Time{}

	providerProfileRTTCache.Lock()
	defer providerProfileRTTCache.Unlock()
	for _, candidate := range candidates {
		key := providerProfileRTTCacheCandidateKey(candidate)
		entry, ok := providerProfileRTTCache.Entries[key]
		if !ok || key == "" || entry.StoredAt.IsZero() || now.Sub(entry.StoredAt) > providerProfileRTTCacheTTL ||
			!entry.Item.Attempted || entry.Item.Status == "unknown" || entry.Item.ProfileID != candidate.Profile.ID {
			if ok && key != "" {
				delete(providerProfileRTTCache.Entries, key)
			}
			missing = append(missing, candidate)
			continue
		}
		cached[candidate.Profile.ID] = entry.Item
		if oldest.IsZero() || entry.StoredAt.Before(oldest) {
			oldest = entry.StoredAt
		}
	}
	return cached, missing, oldest
}

func mergeProviderProfileRTTItems(candidates []bestServerInternalCandidate, cached map[string]providerProfileRTTItem, measured []providerProfileRTTItem) []providerProfileRTTItem {
	byID := make(map[string]providerProfileRTTItem, len(cached)+len(measured))
	for id, item := range cached {
		byID[id] = item
	}
	for _, item := range measured {
		if strings.TrimSpace(item.ProfileID) != "" {
			byID[item.ProfileID] = item
		}
	}
	out := make([]providerProfileRTTItem, 0, len(candidates))
	for _, candidate := range candidates {
		if item, ok := byID[candidate.Profile.ID]; ok {
			item.Endpoint = profileEndpoint(candidate.Profile)
			out = append(out, item)
			continue
		}
		out = append(out, providerProfileRTTItem{ProfileID: candidate.Profile.ID, Endpoint: profileEndpoint(candidate.Profile), Status: "unknown"})
	}
	sortProviderProfileRTTItems(out)
	return out
}

func loadProviderProfileRTTCache(candidates []bestServerInternalCandidate) ([]providerProfileRTTItem, time.Time, bool) {
	cached, missing, measuredAt := splitProviderProfileRTTCache(candidates)
	if len(candidates) == 0 || len(missing) != 0 || len(cached) != len(candidates) {
		return nil, time.Time{}, false
	}
	return mergeProviderProfileRTTItems(candidates, cached, nil), measuredAt, true
}

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
					Endpoint:  profileEndpoint(candidates[index].Profile),
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
			results[index].Endpoint = profileEndpoint(candidates[index].Profile)
			results[index].Status = "unknown"
		}
	}
	sortProviderProfileRTTItems(results)
	return results
}

func providerProfileRTTCatalog(candidates []bestServerInternalCandidate) []subscriptionProfile {
	out := make([]subscriptionProfile, 0, len(candidates))
	for _, candidate := range candidates {
		p := candidate.Profile
		if strings.TrimSpace(p.ID) == "" || strings.TrimSpace(p.Address) == "" || p.Port <= 0 {
			continue
		}
		out = append(out, subscriptionProfile{
			ID: p.ID, Name: p.Name, CountryCode: p.CountryCode,
			Address: p.Address, Port: p.Port,
		})
	}
	return out
}

func providerProfileRTTCatalogKey(catalog []subscriptionProfile) string {
	if len(catalog) == 0 {
		return ""
	}
	parts := make([]string, 0, len(catalog))
	for _, p := range catalog {
		parts = append(parts, strings.Join([]string{
			strings.TrimSpace(p.ID),
			strings.TrimSpace(p.Name),
			strings.ToLower(strings.TrimSpace(p.CountryCode)),
			strings.TrimSpace(profileEndpoint(p)),
		}, "|"))
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\n")))
	return hex.EncodeToString(sum[:8])
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

	catalog := providerProfileRTTCatalog(filtered)
	catalogKey := providerProfileRTTCatalogKey(catalog)
	if len(catalog) != len(filtered) || catalogKey == "" {
		writeJSON(w, http.StatusServiceUnavailable, providerProfileRTTResponse{
			Success: false, Results: []providerProfileRTTItem{}, Fresh: false, ProbeMode: "logical_vpn_https_ip", Mutation: "NONE",
			Error: "VPN profile catalog snapshot is invalid",
		})
		return
	}

	force := r.URL.Query().Get("refresh") == "1"
	cached := map[string]providerProfileRTTItem{}
	missing := filtered
	measuredAt := time.Time{}
	if !force {
		cached, missing, measuredAt = splitProviderProfileRTTCache(filtered)
		if len(missing) == 0 && len(cached) == len(filtered) {
			items := mergeProviderProfileRTTItems(filtered, cached, nil)
			checked, reachable := 0, 0
			for _, item := range items {
				if item.Attempted {
					checked++
				}
				if item.Reachable {
					reachable++
				}
			}
			writeJSON(w, http.StatusOK, providerProfileRTTResponse{
				Success: true, Cached: true, MeasuredAt: measuredAt.Format(time.RFC3339), Catalog: catalog, CatalogKey: catalogKey, Results: items,
				Profiles: len(filtered), UniqueEndpoints: countProviderUniqueEndpoints(filtered),
				Checked: checked, Reachable: reachable, Unknown: 0, Partial: false,
				ProbeMode: "logical_vpn_https_ip", Fresh: err == nil, Mutation: "NONE",
			})
			return
		}
	}

	toMeasure := filtered
	if !force {
		toMeasure = missing
	}
	measured := []providerProfileRTTItem{}
	if len(toMeasure) > 0 {
		sweepCtx, cancelSweep := context.WithTimeout(r.Context(), bestServerRTTSweepTimeout(len(toMeasure)))
		measured = measureProviderProfileRTT(sweepCtx, toMeasure, a.probeBestServerProfilePing)
		cancelSweep()
		storeProviderProfileRTTCache(toMeasure, measured)
	}
	items := mergeProviderProfileRTTItems(filtered, cached, measured)
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
		Success: true, Cached: false, MeasuredAt: time.Now().UTC().Format(time.RFC3339), Catalog: catalog, CatalogKey: catalogKey, Results: items,
		Profiles: len(filtered), UniqueEndpoints: countProviderUniqueEndpoints(filtered),
		Checked: checked, Reachable: reachable, Unknown: unknown, Partial: checked < len(filtered),
		ProbeMode: "logical_vpn_https_ip", Fresh: err == nil, Mutation: "NONE",
	})
}
