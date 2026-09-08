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
	if !candidate.Current || !candidate.Tested || strings.TrimSpace(endpoint) == "" {
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
	key := bestServerCurrentQualityKey(endpoint, filter)
	bestServerCurrentQualityCache.Lock()
	defer bestServerCurrentQualityCache.Unlock()
	entry := bestServerCurrentQualityCache.Entry
	if key == "|" || entry.Key != key || entry.StoredAt.IsZero() || time.Since(entry.StoredAt) > bestServerCurrentQualityCacheTTL {
		return bestServerQualityCandidate{}, false
	}
	candidate := entry.Candidate
	candidate.Current = true
	return candidate, true
}

func currentBestServerQualityCandidate(response bestServerQualityResponse) (bestServerQualityCandidate, bool) {
	for _, candidate := range response.Candidates {
		if candidate.Current && candidate.Tested {
			return candidate, true
		}
	}
	return bestServerQualityCandidate{}, false
}
