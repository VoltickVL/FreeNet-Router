package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	bestServerSelectionSnapshotSchema = 1
	bestServerSelectionSnapshotTTL    = 20 * time.Minute
	defaultBestServerSelectionDir     = "/opt/var/lib/freenet/best-server-selections"
)

type bestServerSelectionEntry struct {
	ID  string `json:"id"`
	Raw string `json:"raw"`
}

type bestServerSelectionSnapshot struct {
	Schema            int                        `json:"schema"`
	Token             string                     `json:"token"`
	Purpose           string                     `json:"purpose,omitempty"`
	CreatedAt         string                     `json:"created_at"`
	SourceFingerprint string                     `json:"source_fingerprint"`
	CurrentEndpoint   string                     `json:"current_endpoint"`
	CurrentFilterHash string                     `json:"current_filter_hash"`
	Candidates        []bestServerSelectionEntry `json:"candidates"`
}

func bestServerSelectionSnapshotDir() string {
	if path := strings.TrimSpace(os.Getenv("FREENET_BEST_SELECTION_DIR")); path != "" {
		return path
	}
	return defaultBestServerSelectionDir
}

func bestServerSelectionSnapshotPath(token string) string {
	if !validBestServerSelectionToken(token) {
		return ""
	}
	return filepath.Join(bestServerSelectionSnapshotDir(), token+".json")
}

func validBestServerSelectionToken(token string) bool {
	if len(token) != 32 {
		return false
	}
	for _, r := range token {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			return false
		}
	}
	return true
}

func newBestServerSelectionToken() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func bestServerSelectionFilterHash(filter string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(filter)))
	return hex.EncodeToString(sum[:])
}

func (a *app) bestServerSelectionSourceFingerprint() (string, error) {
	raw, err := os.ReadFile(a.cfg.SubPath)
	if err != nil {
		return "", errors.New("subscription is not configured")
	}
	secretURL := strings.TrimSpace(string(raw))
	if err := validateSubscriptionURL(secretURL); err != nil {
		return "", errors.New("stored subscription URL is invalid")
	}
	return providerSubscriptionSourceFingerprint(secretURL), nil
}

func cleanupBestServerSelectionSnapshots(now time.Time) {
	dir := bestServerSelectionSnapshotDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		token := strings.TrimSuffix(entry.Name(), ".json")
		if !validBestServerSelectionToken(token) {
			continue
		}
		path := bestServerSelectionSnapshotPath(token)
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var snapshot bestServerSelectionSnapshot
		if json.Unmarshal(data, &snapshot) != nil {
			_ = os.Remove(path)
			continue
		}
		createdAt, err := time.Parse(time.RFC3339, strings.TrimSpace(snapshot.CreatedAt))
		if err != nil || now.Sub(createdAt) > bestServerSelectionSnapshotTTL {
			_ = os.Remove(path)
		}
	}
}

func (a *app) storeBestServerSelectionSnapshot(currentEndpoint, currentFilter string, internal []bestServerInternalCandidate, measured []bestServerQualityCandidate) (string, error) {
	return a.storeBestServerSelectionSnapshotWithPurpose(currentEndpoint, currentFilter, internal, measured, "")
}

func (a *app) storeBestServerSelectionSnapshotWithPurpose(currentEndpoint, currentFilter string, internal []bestServerInternalCandidate, measured []bestServerQualityCandidate, purpose string) (string, error) {
	eligible := make(map[string]struct{})
	for _, candidate := range measured {
		id := strings.TrimSpace(candidate.ID)
		if candidate.Current || !candidate.Tested || !candidate.Available || !candidate.Eligible || !validProfileID(id) {
			continue
		}
		eligible[id] = struct{}{}
	}
	if len(eligible) == 0 {
		return "", nil
	}

	entries := make([]bestServerSelectionEntry, 0, len(eligible))
	seen := make(map[string]struct{}, len(eligible))
	for _, candidate := range internal {
		id := strings.TrimSpace(candidate.Profile.ID)
		if _, ok := eligible[id]; !ok {
			continue
		}
		if _, duplicate := seen[id]; duplicate {
			continue
		}
		raw := strings.TrimSpace(candidate.Raw)
		parsed, ok := parseSafeVLESSProfile(raw)
		if !ok || parsed.ID != id {
			return "", errors.New("measured VPN snapshot identity is inconsistent")
		}
		entries = append(entries, bestServerSelectionEntry{ID: id, Raw: raw})
		seen[id] = struct{}{}
	}
	if len(entries) != len(eligible) {
		return "", errors.New("measured VPN snapshot is incomplete")
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].ID < entries[j].ID })

	sourceFingerprint, err := a.bestServerSelectionSourceFingerprint()
	if err != nil {
		return "", err
	}
	token, err := newBestServerSelectionToken()
	if err != nil {
		return "", errors.New("cannot create VPN selection token")
	}
	snapshot := bestServerSelectionSnapshot{
		Schema:            bestServerSelectionSnapshotSchema,
		Token:             token,
		Purpose:           purpose,
		CreatedAt:         time.Now().UTC().Format(time.RFC3339),
		SourceFingerprint: sourceFingerprint,
		CurrentEndpoint:   strings.TrimSpace(currentEndpoint),
		CurrentFilterHash: bestServerSelectionFilterHash(currentFilter),
		Candidates:        entries,
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		return "", errors.New("cannot encode VPN selection snapshot")
	}
	cleanupBestServerSelectionSnapshots(time.Now().UTC())
	path := bestServerSelectionSnapshotPath(token)
	if path == "" {
		return "", errors.New("cannot build VPN selection snapshot path")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return "", errors.New("cannot prepare VPN selection snapshot directory")
	}
	if err := atomicWrite(path, encoded, 0600); err != nil {
		return "", errors.New("cannot persist VPN selection snapshot")
	}
	return token, nil
}

func (a *app) attachBestServerSelectionSnapshot(response *bestServerQualityResponse, internal []bestServerInternalCandidate, currentEndpoint, currentFilter string) error {
	if response == nil {
		return errors.New("VPN selection response is unavailable")
	}
	token, err := a.storeBestServerSelectionSnapshot(currentEndpoint, currentFilter, internal, response.Candidates)
	if err != nil {
		return err
	}
	response.SelectionToken = token
	return nil
}

func (a *app) loadBestServerSelectionCandidate(token, profileID, currentEndpoint, currentFilter string) (bestServerInternalCandidate, error) {
	candidate, _, err := a.loadBestServerSelectionCandidateWithPurpose(token, profileID, currentEndpoint, currentFilter)
	return candidate, err
}

func (a *app) loadBestServerSelectionCandidateWithPurpose(token, profileID, currentEndpoint, currentFilter string) (bestServerInternalCandidate, bool, error) {
	token = strings.TrimSpace(token)
	profileID = strings.TrimSpace(profileID)
	if !validBestServerSelectionToken(token) || !validProfileID(profileID) {
		return bestServerInternalCandidate{}, false, errors.New("invalid VPN selection snapshot")
	}
	path := bestServerSelectionSnapshotPath(token)
	if path == "" {
		return bestServerInternalCandidate{}, false, errors.New("invalid VPN selection snapshot")
	}
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 || len(data) > maxSubscriptionBytes {
		return bestServerInternalCandidate{}, false, errors.New("VPN selection snapshot is unavailable; run Best Server again")
	}
	var snapshot bestServerSelectionSnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return bestServerInternalCandidate{}, false, errors.New("VPN selection snapshot is invalid; run Best Server again")
	}
	if snapshot.Schema != bestServerSelectionSnapshotSchema || snapshot.Token != token {
		return bestServerInternalCandidate{}, false, errors.New("VPN selection snapshot was replaced; run Best Server again")
	}
	createdAt, err := time.Parse(time.RFC3339, strings.TrimSpace(snapshot.CreatedAt))
	if err != nil || time.Since(createdAt) < -time.Minute || time.Since(createdAt) > bestServerSelectionSnapshotTTL {
		return bestServerInternalCandidate{}, false, errors.New("VPN selection snapshot expired; run Best Server again")
	}
	sourceFingerprint, err := a.bestServerSelectionSourceFingerprint()
	if err != nil || snapshot.SourceFingerprint != sourceFingerprint {
		return bestServerInternalCandidate{}, false, errors.New("VPN subscription changed after Best Server scan; run Best Server again")
	}
	if !endpointsEqual(snapshot.CurrentEndpoint, currentEndpoint) || snapshot.CurrentFilterHash != bestServerSelectionFilterHash(currentFilter) {
		return bestServerInternalCandidate{}, false, errors.New("current VPN changed after Best Server scan; run Best Server again")
	}
	for _, entry := range snapshot.Candidates {
		if entry.ID != profileID {
			continue
		}
		raw := strings.TrimSpace(entry.Raw)
		profile, ok := parseSafeVLESSProfile(raw)
		if !ok || profile.ID != profileID {
			return bestServerInternalCandidate{}, false, errors.New("stored VPN selection candidate is invalid")
		}
		return bestServerInternalCandidate{Profile: profile, Raw: raw}, snapshot.Purpose == "manual_rtt", nil
	}
	return bestServerInternalCandidate{}, false, errors.New("selected VPN was not part of the measured Best Server snapshot")
}

func consumeBestServerSelectionSnapshot(token string) {
	path := bestServerSelectionSnapshotPath(strings.TrimSpace(token))
	if path == "" {
		return
	}
	_ = os.Remove(path)
}

func (a *app) materializeBestServerSelectionProviderCache(candidate bestServerInternalCandidate) (string, string, func(), error) {
	raw := strings.TrimSpace(candidate.Raw)
	profile, ok := parseSafeVLESSProfile(raw)
	if !ok || profile.ID != candidate.Profile.ID {
		return "", "", nil, errors.New("selected VPN snapshot is invalid")
	}
	sourceFingerprint, err := a.bestServerSelectionSourceFingerprint()
	if err != nil {
		return "", "", nil, err
	}
	dir, err := os.MkdirTemp("", "freenet-provider-selection-")
	if err != nil {
		return "", "", nil, errors.New("cannot prepare selected VPN cache")
	}
	_ = os.Chmod(dir, 0700)
	cleanup := func() { _ = os.RemoveAll(dir) }
	cachePath := filepath.Join(dir, "provider.lkg")
	sourcePath := filepath.Join(dir, "provider.lkg.source.sha256")
	if err := os.WriteFile(cachePath, []byte(raw+"\n"), 0600); err != nil {
		cleanup()
		return "", "", nil, errors.New("cannot stage selected VPN cache")
	}
	if err := os.WriteFile(sourcePath, []byte(sourceFingerprint+"\n"), 0600); err != nil {
		cleanup()
		return "", "", nil, errors.New("cannot bind selected VPN cache to subscription")
	}
	return cachePath, sourcePath, cleanup, nil
}

func (a *app) runProviderSelectionCommand(ctx context.Context, mode string, candidate bestServerInternalCandidate) ([]byte, error) {
	return a.runProviderSelectionCommandWithRTTMode(ctx, mode, candidate, false)
}

// Manual RTT applies reuse measured credentials and skip only a duplicate
// isolated preflight. Candidate Xray validation, backup, live acceptance and
// rollback remain mandatory in the provider helper.
func (a *app) runProviderSelectionCommandWithRTTMode(ctx context.Context, mode string, candidate bestServerInternalCandidate, manualRTT bool) ([]byte, error) {
	return a.runProviderSelectionCommandWithManualOptions(ctx, mode, candidate, manualRTT, false)
}

// An explicit manual selection is allowed to target even an unresponsive
// VPN, but never bypasses Xray config validation, core-only safety or rollback.
func (a *app) runProviderSelectionCommandWithManualOptions(ctx context.Context, mode string, candidate bestServerInternalCandidate, manualRTT, forceManual bool) ([]byte, error) {
	cachePath, sourcePath, cleanup, err := a.materializeBestServerSelectionProviderCache(candidate)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	cmd := exec.CommandContext(ctx, providerHelperPath(), mode, candidate.Profile.ID)
	rttFlag := "0"
	if manualRTT && mode == "apply-core" {
		rttFlag = "1"
	}
	forceFlag := "0"
	if forceManual && mode == "apply-core" {
		forceFlag = "1"
	}
	cmd.Env = append(os.Environ(),
		"PATH=/opt/bin:/opt/sbin:/opt/usr/bin:/opt/usr/sbin:/bin:/sbin:/usr/bin:/usr/sbin",
		"FREENET_SUB_FILE="+a.cfg.SubPath,
		"FREENET_PROVIDER_SUBSCRIPTION_CACHE="+cachePath,
		"FREENET_PROVIDER_SUBSCRIPTION_SOURCE="+sourcePath,
		"FREENET_PROVIDER_RTT_MANUAL="+rttFlag,
		"FREENET_PROVIDER_FORCE_MANUAL="+forceFlag,
	)
	cmd.WaitDelay = 2 * time.Second
	output, err := cmd.CombinedOutput()
	if errors.Is(err, exec.ErrWaitDelay) && ctx.Err() == nil {
		err = nil
	}
	return output, err
}

func (a *app) runProviderSelectionPlan(candidate bestServerInternalCandidate) (providerPlanResponse, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	output, err := a.runProviderSelectionCommand(ctx, "plan", candidate)
	if ctx.Err() == context.DeadlineExceeded {
		return providerPlanResponse{ProfileID: candidate.Profile.ID}, errors.New("Проверка выбранного VPN-сервера не завершилась вовремя.")
	}
	if err != nil {
		if reason := providerPlanFailureReason(output); reason != "" {
			return providerPlanResponse{ProfileID: candidate.Profile.ID}, errors.New(reason)
		}
		return providerPlanResponse{ProfileID: candidate.Profile.ID}, errors.New("Не удалось повторно проверить выбранный VPN-сервер.")
	}
	plan, parseErr := parseProviderPlan(string(output))
	if parseErr != nil {
		return plan, errors.New("FreeNet получил неполный ответ проверки выбранного VPN-сервера.")
	}
	return plan, nil
}
