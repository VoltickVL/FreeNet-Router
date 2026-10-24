package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	bestServerCurrentQualityCacheTTL              = 2 * time.Minute
	bestServerCurrentQualityPersistentSchema      = 3
	bestServerCurrentQualityPersistentPathDefault = "/opt/var/lib/freenet/current-quality.json"
)

// Last-good throughput is a display record, never a candidate measurement or
// decision input. Its identity is the enclosing cache key/hash.
type bestServerStoredThroughput struct {
	DownloadMbps     float64 `json:"download_mbps"`
	MeasuredAt       string  `json:"measured_at"`
	ThroughputSource string  `json:"throughput_source"`
}

type bestServerDisplayThroughput struct {
	DownloadMbps     float64 `json:"download_mbps"`
	MeasuredAt       string  `json:"measured_at"`
	Historical       bool    `json:"historical"`
	ThroughputSource string  `json:"throughput_source"`
}

type bestServerCurrentQualityCacheEntry struct {
	Key                string
	StoredAt           time.Time
	Candidate          bestServerQualityCandidate
	LastGoodThroughput bestServerStoredThroughput
}

type bestServerCurrentQualityPersistentEntry struct {
	Schema             int                         `json:"schema"`
	IdentityHash       string                      `json:"identity_hash"`
	StoredAt           string                      `json:"stored_at"`
	Candidate          bestServerQualityCandidate  `json:"candidate"`
	LastGoodThroughput *bestServerStoredThroughput `json:"last_good_throughput,omitempty"`
}

// Serialize the read/merge/write that preserves last-good throughput. Readers
// still take only the short RAM lock and never hydrate decision evidence.
var bestServerCurrentQualityWriteMu sync.Mutex

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

func bestServerCurrentThroughputProvenanceValid(candidate bestServerQualityCandidate) bool {
	strict := candidate.DownloadMbps > 0
	fallback := candidate.FallbackDownloadMbps > 0
	if fallback || candidate.ThroughputSource == bestServerThroughputCurrentFallback {
		return false
	}
	if strict {
		return candidate.ThroughputSource == bestServerThroughputStrictAggregate
	}
	return candidate.ThroughputSource == ""
}

// An unavailable observation can replace latest display evidence, but carries
// no throughput/eligibility claim. Check the original value before sanitizing
// legacy fields so malformed or fallback evidence cannot become a valid failure.
func bestServerCurrentUnavailableCandidateValid(candidate bestServerQualityCandidate, endpoint string) bool {
	expected := strings.TrimSpace(endpoint)
	return expected != "" && strings.TrimSpace(candidate.Endpoint) == expected &&
		candidate.Current && candidate.Tested && !candidate.Available && !candidate.Eligible &&
		candidate.DownloadMbps == 0 && candidate.FallbackDownloadMbps == 0 && candidate.ThroughputSource == ""
}

func sanitizeBestServerCurrentQuality(candidate bestServerQualityCandidate) bestServerQualityCandidate {
	// Legacy current_fallback is never exposed or persisted as current VPN
	// speed. Keep only canonical strict throughput; partial RTT/service evidence
	// may still be retained with speed unknown.
	candidate.Rejections = append([]string(nil), candidate.Rejections...)
	candidate.ChatGPTHTTPSStatus = sanitizeBestServerChatGPTHTTPSStatus(candidate.ChatGPTHTTPSStatus)
	candidate.FallbackDownloadMbps = 0
	if candidate.ThroughputSource == bestServerThroughputCurrentFallback {
		candidate.ThroughputSource = ""
	}
	return candidate
}

func bestServerStoredThroughputForCandidate(candidate bestServerQualityCandidate, measuredAt time.Time) bestServerStoredThroughput {
	if !candidate.Current || !candidate.Tested || !candidate.Available || measuredAt.IsZero() ||
		candidate.DownloadMbps <= 0 || math.IsNaN(candidate.DownloadMbps) || math.IsInf(candidate.DownloadMbps, 0) ||
		candidate.ThroughputSource != bestServerThroughputStrictAggregate ||
		candidate.FallbackDownloadMbps != 0 || candidate.MediaSamples < bestServerMediaRequiredRuns {
		return bestServerStoredThroughput{}
	}
	return bestServerStoredThroughput{
		DownloadMbps: candidate.DownloadMbps,
		MeasuredAt: measuredAt.UTC().Format(time.RFC3339Nano),
		ThroughputSource: bestServerThroughputStrictAggregate,
	}
}

func sanitizeBestServerStoredThroughput(saved bestServerStoredThroughput, latestAt time.Time) bestServerStoredThroughput {
	if saved.DownloadMbps <= 0 || math.IsNaN(saved.DownloadMbps) || math.IsInf(saved.DownloadMbps, 0) ||
		saved.ThroughputSource != bestServerThroughputStrictAggregate {
		return bestServerStoredThroughput{}
	}
	measuredAt, err := time.Parse(time.RFC3339Nano, saved.MeasuredAt)
	if err != nil || measuredAt.IsZero() || latestAt.IsZero() || measuredAt.After(latestAt) {
		return bestServerStoredThroughput{}
	}
	saved.MeasuredAt = measuredAt.UTC().Format(time.RFC3339Nano)
	return saved
}

func bestServerCurrentLastGoodThroughput(entry bestServerCurrentQualityCacheEntry) bestServerStoredThroughput {
	// This also reads existing schema-3 caches written before the optional
	// last_good_throughput field existed. The original measurement time survives.
	if current := bestServerStoredThroughputForCandidate(entry.Candidate, entry.StoredAt); current.DownloadMbps > 0 {
		return current
	}
	return sanitizeBestServerStoredThroughput(entry.LastGoodThroughput, entry.StoredAt)
}

func bestServerDisplayThroughputFromEntry(entry bestServerCurrentQualityCacheEntry) *bestServerDisplayThroughput {
	saved := bestServerCurrentLastGoodThroughput(entry)
	if saved.DownloadMbps <= 0 {
		return nil
	}
	current := bestServerStoredThroughputForCandidate(entry.Candidate, entry.StoredAt)
	return &bestServerDisplayThroughput{
		DownloadMbps: saved.DownloadMbps,
		MeasuredAt: saved.MeasuredAt,
		Historical: current.DownloadMbps <= 0 || current.MeasuredAt != saved.MeasuredAt,
		ThroughputSource: bestServerThroughputStrictAggregate,
	}
}

func writeBestServerCurrentQualityPersistent(endpoint, filter string, storedAt time.Time, candidate bestServerQualityCandidate, lastGood bestServerStoredThroughput) {
	candidate = sanitizeBestServerCurrentQuality(candidate)
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
	if saved := sanitizeBestServerStoredThroughput(lastGood, storedAt); saved.DownloadMbps > 0 {
		entry.LastGoodThroughput = &saved
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

func loadBestServerCurrentQualityPersistentEntry(endpoint, filter string) (bestServerCurrentQualityCacheEntry, bool) {
	path := bestServerCurrentQualityPersistentPath()
	data, err := os.ReadFile(path)
	if err != nil {
		return bestServerCurrentQualityCacheEntry{}, false
	}
	var entry bestServerCurrentQualityPersistentEntry
	if err := json.Unmarshal(data, &entry); err != nil || entry.Schema != bestServerCurrentQualityPersistentSchema {
		return bestServerCurrentQualityCacheEntry{}, false
	}
	if entry.IdentityHash == "" || entry.IdentityHash != bestServerCurrentQualityIdentityHash(endpoint, filter) {
		return bestServerCurrentQualityCacheEntry{}, false
	}
	storedAt, err := time.Parse(time.RFC3339Nano, entry.StoredAt)
	if err != nil || storedAt.IsZero() {
		return bestServerCurrentQualityCacheEntry{}, false
	}
	if !entry.Candidate.Available && !bestServerCurrentUnavailableCandidateValid(entry.Candidate, endpoint) {
		return bestServerCurrentQualityCacheEntry{}, false
	}
	candidate := sanitizeBestServerCurrentQuality(entry.Candidate)
	if !candidate.Current || !candidate.Tested || strings.TrimSpace(candidate.Endpoint) == "" {
		return bestServerCurrentQualityCacheEntry{}, false
	}
	if strings.TrimSpace(candidate.Endpoint) != strings.TrimSpace(endpoint) {
		return bestServerCurrentQualityCacheEntry{}, false
	}
	if !bestServerCurrentThroughputProvenanceValid(candidate) {
		return bestServerCurrentQualityCacheEntry{}, false
	}
	if candidate.Available && candidate.ApplicationMS <= 0 && candidate.TCPRTTMS <= 0 && candidate.DownloadMbps <= 0 {
		return bestServerCurrentQualityCacheEntry{}, false
	}
	candidate.Current = true
	loaded := bestServerCurrentQualityCacheEntry{
		Key: bestServerCurrentQualityKey(endpoint, filter), StoredAt: storedAt, Candidate: candidate,
	}
	if entry.LastGoodThroughput != nil {
		loaded.LastGoodThroughput = *entry.LastGoodThroughput
	}
	loaded.LastGoodThroughput = bestServerCurrentLastGoodThroughput(loaded)
	return loaded, true
}

func loadBestServerCurrentQualityPersistent(endpoint, filter string) (bestServerQualityCandidate, time.Time, bool) {
	entry, ok := loadBestServerCurrentQualityPersistentEntry(endpoint, filter)
	return entry.Candidate, entry.StoredAt, ok
}

func storeBestServerCurrentQuality(endpoint, filter string, candidate bestServerQualityCandidate) {
	_, _ = storeBestServerCurrentQualityDisplaySnapshot(endpoint, filter, candidate)
}

// Return the exact stored snapshot's display values to the completed scan. A
// later concurrent observation cannot mix its timestamp/speed into this result.
func storeBestServerCurrentQualityDisplaySnapshot(endpoint, filter string, candidate bestServerQualityCandidate) (time.Time, *bestServerDisplayThroughput) {
	// Keep real current-only observations, including an unconfirmed route with
	// no speed/eligibility claim. The separately retained display speed remains
	// strict-only; the latest candidate is never filled from that old record.
	if !candidate.Available && !bestServerCurrentUnavailableCandidateValid(candidate, endpoint) {
		return time.Time{}, nil
	}
	candidate = sanitizeBestServerCurrentQuality(candidate)
	if !candidate.Current || !candidate.Tested || strings.TrimSpace(endpoint) == "" {
		return time.Time{}, nil
	}
	if candidate.Endpoint != "" && strings.TrimSpace(candidate.Endpoint) != strings.TrimSpace(endpoint) {
		return time.Time{}, nil
	}
	if !bestServerCurrentThroughputProvenanceValid(candidate) {
		return time.Time{}, nil
	}
	if candidate.Available && candidate.ApplicationMS <= 0 && candidate.TCPRTTMS <= 0 && candidate.DownloadMbps <= 0 {
		return time.Time{}, nil
	}
	bestServerCurrentQualityWriteMu.Lock()
	defer bestServerCurrentQualityWriteMu.Unlock()
	storedAt := time.Now().UTC()
	lastGood := bestServerStoredThroughputForCandidate(candidate, storedAt)
	if lastGood.DownloadMbps <= 0 {
		if previous, ok := loadBestServerCurrentQualityDisplayEntry(endpoint, filter); ok {
			lastGood = sanitizeBestServerStoredThroughput(bestServerCurrentLastGoodThroughput(previous), storedAt)
		}
	}
	entry := bestServerCurrentQualityCacheEntry{
		Key:               bestServerCurrentQualityKey(endpoint, filter),
		StoredAt:          storedAt,
		Candidate:         candidate,
		LastGoodThroughput: lastGood,
	}
	bestServerCurrentQualityCache.Lock()
	bestServerCurrentQualityCache.Entry = entry
	bestServerCurrentQualityCache.Unlock()

	// Disk state is observability-only. It deliberately stores only the public
	// current-quality candidate, separate last-good speed and a hash of logical
	// identity: no filter, subscription URL, UUID, Reality keys or raw outbound.
	writeBestServerCurrentQualityPersistent(endpoint, filter, storedAt, candidate, lastGood)
	return storedAt, bestServerDisplayThroughputFromEntry(entry)
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
	candidate := sanitizeBestServerCurrentQuality(entry.Candidate)
	candidate.Current = true
	if !candidate.Eligible || candidate.DownloadMbps <= 0 ||
		candidate.ThroughputSource != bestServerThroughputStrictAggregate {
		return bestServerQualityCandidate{}, false
	}
	return candidate, true
}

// loadBestServerCurrentQualityForDisplay returns the last measured current-only
// result for the exact logical identity, including partial evidence that is not
// eligible for decisions. It may fall back to persisted state after process
// restart/Web Update. Decision paths never call the persisted loader.
func loadBestServerCurrentQualityForDisplay(endpoint, filter string) (bestServerQualityCandidate, time.Time, bool) {
	entry, ok := loadBestServerCurrentQualityDisplayEntry(endpoint, filter)
	return entry.Candidate, entry.StoredAt, ok
}

func loadBestServerCurrentQualityDisplayEntry(endpoint, filter string) (bestServerCurrentQualityCacheEntry, bool) {
	key := bestServerCurrentQualityKey(endpoint, filter)
	bestServerCurrentQualityCache.Lock()
	entry := bestServerCurrentQualityCache.Entry
	bestServerCurrentQualityCache.Unlock()
	if key != "|" && entry.Key == key && !entry.StoredAt.IsZero() {
		if !entry.Candidate.Available && !bestServerCurrentUnavailableCandidateValid(entry.Candidate, endpoint) {
			return bestServerCurrentQualityCacheEntry{}, false
		}
		entry.Candidate = sanitizeBestServerCurrentQuality(entry.Candidate)
		entry.Candidate.Current = true
		entry.LastGoodThroughput = bestServerCurrentLastGoodThroughput(entry)
		return entry, true
	}
	if key == "|" {
		return bestServerCurrentQualityCacheEntry{}, false
	}
	return loadBestServerCurrentQualityPersistentEntry(endpoint, filter)
}

// Read-only display lookup. It never replaces candidate.DownloadMbps and never
// populates the RAM-only decision cache after a restart.
func loadBestServerDisplayThroughput(endpoint, filter string) *bestServerDisplayThroughput {
	entry, ok := loadBestServerCurrentQualityDisplayEntry(endpoint, filter)
	if !ok {
		return nil
	}
	return bestServerDisplayThroughputFromEntry(entry)
}

func (a *app) currentVPNQualityCacheResponse() bestServerQualityResponse {
	currentEndpoint := readBestServerCurrentEndpoint(a.cfg.OutPath)
	currentFilter := readBestServerCurrentFilter(a.cfg.FilterPath)
	currentIdentity := ""
	if strings.TrimSpace(currentEndpoint) != "" {
		currentIdentity = bestServerCurrentQualityIdentityHash(currentEndpoint, currentFilter)
	}
	entry, ok := loadBestServerCurrentQualityDisplayEntry(currentEndpoint, currentFilter)
	response := bestServerQualityResponse{
		Success: true, Available: false, Candidates: []bestServerQualityCandidate{}, ProfilesScanned: 0, ProfilesTotal: 1,
		Mutation: "NONE", CurrentEndpoint: currentEndpoint,
		CurrentIdentity: currentIdentity,
		Message: "Сохранённой проверки текущего VPN пока нет.",
	}
	if !ok {
		return response
	}
	response.Available = entry.Candidate.Available
	response.ScannedAt = entry.StoredAt.UTC().Format(time.RFC3339Nano)
	response.Candidates = []bestServerQualityCandidate{entry.Candidate}
	response.DisplayThroughput = bestServerDisplayThroughputFromEntry(entry)
	response.ProfilesScanned = 1
	response.Message = "Показана последняя проверка текущего VPN; новая проверка не запускалась."
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
