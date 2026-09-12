package main

import (
	"strings"
	"sync"
	"time"
)

const bestServerCurrentQualityCacheTTL = 2 * time.Minute

type bestServerCurrentQualityCacheEntry struct {
	Key       string
	StoredAt  time.Time
	Candidate bestServerQualityCandidate
}

var bestServerCurrentQualityCache struct {
	sync.Mutex
	Entry bestServerCurrentQualityCacheEntry
}

func bestServerCurrentQualityKey(endpoint, filter string) string {
	return strings.TrimSpace(endpoint) + "|" + strings.TrimSpace(filter)
}

func storeBestServerCurrentQuality(endpoint, filter string, candidate bestServerQualityCandidate) {
	// Do not let an incomplete current-only run become the baseline for a later
	// full comparison. A reusable baseline must include the same complete speed,
	// service and stability evidence required for a switchable candidate.
	if !candidate.Current || !candidate.Tested || !candidate.Eligible || candidate.DownloadMbps <= 0 || strings.TrimSpace(endpoint) == "" {
		return
	}
	bestServerCurrentQualityCache.Lock()
	defer bestServerCurrentQualityCache.Unlock()
	bestServerCurrentQualityCache.Entry = bestServerCurrentQualityCacheEntry{
		Key:       bestServerCurrentQualityKey(endpoint, filter),
		StoredAt:  time.Now(),
		Candidate: candidate,
	}
}

func loadBestServerCurrentQuality(endpoint, filter string) (bestServerQualityCandidate, bool) {
	candidate, storedAt, ok := loadBestServerCurrentQualityForDisplay(endpoint, filter)
	if !ok || time.Since(storedAt) > bestServerCurrentQualityCacheTTL {
		return bestServerQualityCandidate{}, false
	}
	return candidate, true
}

// loadBestServerCurrentQualityForDisplay returns the last complete measurement
// for the exact current logical identity even after the decision-cache TTL has
// expired. It is observability-only: Best Server and AUTO VPN decisions keep
// using loadBestServerCurrentQuality and therefore retain the strict freshness
// window above. Callers must expose StoredAt so stale measurements are never
// presented as a fresh validation fact.
func loadBestServerCurrentQualityForDisplay(endpoint, filter string) (bestServerQualityCandidate, time.Time, bool) {
	key := bestServerCurrentQualityKey(endpoint, filter)
	bestServerCurrentQualityCache.Lock()
	defer bestServerCurrentQualityCache.Unlock()
	entry := bestServerCurrentQualityCache.Entry
	if key == "|" || entry.Key != key || entry.StoredAt.IsZero() {
		return bestServerQualityCandidate{}, time.Time{}, false
	}
	candidate := entry.Candidate
	candidate.Current = true
	return candidate, entry.StoredAt, true
}

func currentBestServerQualityCandidate(response bestServerQualityResponse) (bestServerQualityCandidate, bool) {
	for _, candidate := range response.Candidates {
		if candidate.Current && candidate.Tested {
			return candidate, true
		}
	}
	return bestServerQualityCandidate{}, false
}
