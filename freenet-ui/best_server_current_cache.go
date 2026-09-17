package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	bestServerCurrentQualityCacheTTL              = 2 * time.Minute
	bestServerCurrentQualityPersistentSchema      = 1
	bestServerCurrentQualityPersistentPathDefault = "/opt/var/lib/freenet/current-quality.json"
)

type bestServerCurrentQualityCacheEntry struct {
	Key       string
	StoredAt  time.Time
	Candidate bestServerQualityCandidate
}

type bestServerCurrentQualityPersistentEntry struct {
	Schema       int                        `json:"schema"`
	IdentityHash string                     `json:"identity_hash"`
	StoredAt     string                     `json:"stored_at"`
	Candidate    bestServerQualityCandidate `json:"candidate"`
}

var bestServerCurrentQualityCache struct {
	sync.Mutex
	Entry bestServerCurrentQualityCacheEntry
}

func bestServerCurrentQualityPersistentPath() string {
	if value := strings.TrimSpace(os.Getenv("FREENET_CURRENT_QUALITY_CACHE")); value != "" {
		return value
	}
	return bestServerCurrentQualityPersistentPathDefault
}

func bestServerCurrentQualityKey(endpoint, filter string) string {
	return strings.TrimSpace(endpoint) + "|" + strings.TrimSpace(filter)
}

func bestServerCurrentQualityIdentityHash(endpoint, filter string) string {
	sum := sha256.Sum256([]byte(bestServerCurrentQualityKey(endpoint, filter)))
	return hex.EncodeToString(sum[:])
}

func writeBestServerCurrentQualityPersistent(endpoint, filter string, storedAt time.Time, candidate bestServerQualityCandidate) {
	path := bestServerCurrentQualityPersistentPath()
	if strings.TrimSpace(path) == "" {
		return
	}
	entry := bestServerCurrentQualityPersistentEntry{
		Schema:       bestServerCurrentQualityPersistentSchema,
		IdentityHash: bestServerCurrentQualityIdentityHash(endpoint, filter),
		StoredAt:     storedAt.UTC().Format(time.RFC3339Nano),
		Candidate:    candidate,
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return
	}
	tmp := path + ".new"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
	}
}

func loadBestServerCurrentQualityPersistent(endpoint, filter string) (bestServerQualityCandidate, time.Time, bool) {
	path := bestServerCurrentQualityPersistentPath()
	data, err := os.ReadFile(path)
	if err != nil {
		return bestServerQualityCandidate{}, time.Time{}, false
	}
	var entry bestServerCurrentQualityPersistentEntry
	if err := json.Unmarshal(data, &entry); err != nil || entry.Schema != bestServerCurrentQualityPersistentSchema {
		return bestServerQualityCandidate{}, time.Time{}, false
	}
	if entry.IdentityHash == "" || entry.IdentityHash != bestServerCurrentQualityIdentityHash(endpoint, filter) {
		return bestServerQualityCandidate{}, time.Time{}, false
	}
	storedAt, err := time.Parse(time.RFC3339Nano, entry.StoredAt)
	if err != nil || storedAt.IsZero() {
		return bestServerQualityCandidate{}, time.Time{}, false
	}
	candidate := entry.Candidate
	if !candidate.Current || !candidate.Tested || !candidate.Available || strings.TrimSpace(candidate.Endpoint) == "" {
		return bestServerQualityCandidate{}, time.Time{}, false
	}
	if strings.TrimSpace(candidate.Endpoint) != strings.TrimSpace(endpoint) {
		return bestServerQualityCandidate{}, time.Time{}, false
	}
	if candidate.ApplicationMS <= 0 && candidate.TCPRTTMS <= 0 && candidate.DownloadMbps <= 0 {
		return bestServerQualityCandidate{}, time.Time{}, false
	}
	candidate.Current = true
	return candidate, storedAt, true
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
	storedAt := time.Now().UTC()
	bestServerCurrentQualityCache.Lock()
	bestServerCurrentQualityCache.Entry = bestServerCurrentQualityCacheEntry{
		Key:       bestServerCurrentQualityKey(endpoint, filter),
		StoredAt:  storedAt,
		Candidate: candidate,
	}
	bestServerCurrentQualityCache.Unlock()

	// Disk state is observability-only. It deliberately stores only the public
	// current-quality candidate plus a hash of logical identity: no filter,
	// subscription URL, UUID, Reality keys or raw outbound are written.
	writeBestServerCurrentQualityPersistent(endpoint, filter, storedAt, candidate)
}

func loadBestServerCurrentQuality(endpoint, filter string) (bestServerQualityCandidate, bool) {
	// Decision path intentionally uses RAM-only evidence from this process.
	// Persisted display state survives restart/update but must never become a
	// fresh AUTO VPN / Best Server decision input merely because its timestamp
	// happens to be recent.
	key := bestServerCurrentQualityKey(endpoint, filter)
	bestServerCurrentQualityCache.Lock()
	entry := bestServerCurrentQualityCache.Entry
	bestServerCurrentQualityCache.Unlock()
	if key == "|" || entry.Key != key || entry.StoredAt.IsZero() || time.Since(entry.StoredAt) > bestServerCurrentQualityCacheTTL {
		return bestServerQualityCandidate{}, false
	}
	candidate := entry.Candidate
	candidate.Current = true
	if !candidate.Eligible || candidate.DownloadMbps <= 0 {
		return bestServerQualityCandidate{}, false
	}
	return candidate, true
}

// loadBestServerCurrentQualityForDisplay returns the last measured current-only
// result for the exact logical identity, including partial evidence that is not
// eligible for decisions. It may fall back to persisted state after process
// restart/Web Update. Decision paths never call the persisted loader.
func loadBestServerCurrentQualityForDisplay(endpoint, filter string) (bestServerQualityCandidate, time.Time, bool) {
	key := bestServerCurrentQualityKey(endpoint, filter)
	bestServerCurrentQualityCache.Lock()
	entry := bestServerCurrentQualityCache.Entry
	bestServerCurrentQualityCache.Unlock()
	if key != "|" && entry.Key == key && !entry.StoredAt.IsZero() {
		candidate := entry.Candidate
		candidate.Current = true
		return candidate, entry.StoredAt, true
	}
	if key == "|" {
		return bestServerQualityCandidate{}, time.Time{}, false
	}
	return loadBestServerCurrentQualityPersistent(endpoint, filter)
}

func (a *app) currentVPNQualityCacheResponse() bestServerQualityResponse {
	currentEndpoint := readBestServerCurrentEndpoint(a.cfg.OutPath)
	currentFilter := readBestServerCurrentFilter(a.cfg.FilterPath)
	candidate, storedAt, ok := loadBestServerCurrentQualityForDisplay(currentEndpoint, currentFilter)
	response := bestServerQualityResponse{
		Success: true, Available: false, Candidates: []bestServerQualityCandidate{}, ProfilesScanned: 0, ProfilesTotal: 1,
		Mutation: "NONE", CurrentEndpoint: currentEndpoint,
		Message: "Сохранённого подтверждённого замера текущего VPN пока нет.",
	}
	if !ok {
		return response
	}
	response.Available = candidate.Available
	response.ScannedAt = storedAt.UTC().Format(time.RFC3339Nano)
	response.Candidates = []bestServerQualityCandidate{candidate}
	response.ProfilesScanned = 1
	response.Message = "Показан последний подтверждённый замер текущего VPN; новая проверка не запускалась."
	return response
}

func currentBestServerQualityCandidate(response bestServerQualityResponse) (bestServerQualityCandidate, bool) {
	for _, candidate := range response.Candidates {
		if candidate.Current && candidate.Tested {
			return candidate, true
		}
	}
	return bestServerQualityCandidate{}, false
}
