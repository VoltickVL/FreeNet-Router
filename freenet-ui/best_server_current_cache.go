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
	// Keep real current-only measurements for observability even when the strict
	// Best Server eligibility contract is incomplete. Decision callers remain
	// fail-closed in loadBestServerCurrentQuality below.
	if !candidate.Current || !candidate.Tested || !candidate.Available || strings.TrimSpace(endpoint) == "" {
		return
	}
	if candidate.ApplicationMS <= 0 && candidate.TCPRTTMS <= 0 && candidate.DownloadMbps <= 0 {
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
	if !ok || time.Since(storedAt) > bestServerCurrentQualityCacheTTL || !candidate.Eligible || candidate.DownloadMbps <= 0 {
		return bestServerQualityCandidate{}, false
	}
	return candidate, true
}

// loadBestServerCurrentQualityForDisplay returns the last measured current-only
// result for the exact logical identity, including partial evidence that is not
// eligible for decisions. It is observability-only: Best Server and AUTO VPN
// decisions use loadBestServerCurrentQuality and therefore still require a
// fresh, fully eligible measurement.
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
