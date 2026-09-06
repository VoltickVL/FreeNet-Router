package main

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Duplicate browser submissions for the same ISP/DNS target must collapse into
// one authoritative mutation. Followers wait for the leader and receive the
// exact same terminal result; they never start a second mutation.
type networkApplyFlight struct {
	target string
	done   chan struct{}
	status int
	result networkApplyResponse
}

type networkApplySingleflight struct {
	mu      sync.Mutex
	current *networkApplyFlight
}

var networkApplyFlights networkApplySingleflight

func writeNativeStateFile(path, value string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".canonical-native-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	keep := false
	defer func() {
		_ = f.Close()
		if !keep {
			_ = os.Remove(tmp)
		}
	}()
	if err := f.Chmod(0o600); err != nil {
		return err
	}
	if _, err := f.WriteString(value); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	keep = true
	return nil
}

func validNativeEngineStateToken(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || value == "opkg" {
		return false
	}
	for _, r := range value {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-') {
			return false
		}
	}
	return true
}

func ensureCanonicalNativeControlPlane() error {
	dir := nativeDNSStateDir()
	enginePath := filepath.Join(dir, "filter-engine.native")
	interceptPath := filepath.Join(dir, "intercept.native")

	// A valid snapshot is an exact fact from a previous managed Native -> Split
	// transition. Keep it byte-semantically instead of forcing every router to
	// one filter engine/intercept combination. Only legacy/missing state receives
	// the deterministic fallback used by FreeNet's default Native profile.
	engine := ""
	if data, err := os.ReadFile(enginePath); err == nil {
		engine = strings.TrimSpace(string(data))
	}
	if !validNativeEngineStateToken(engine) {
		if err := writeNativeStateFile(enginePath, "public\n"); err != nil {
			return err
		}
	}

	intercept := ""
	if data, err := os.ReadFile(interceptPath); err == nil {
		intercept = strings.TrimSpace(string(data))
	}
	if intercept != "on" && intercept != "off" {
		if err := writeNativeStateFile(interceptPath, "on\n"); err != nil {
			return err
		}
	}
	return nil
}

func ensureCanonicalNativeDNSSnapshot() error {
	valid, _, err := legacyNativeDNSSnapshotStatus()
	if err == nil && valid {
		return nil
	}

	// Prefer exact historical/managed recovery when it is available. It preserves
	// an opaque legacy Native 02_dns (including JSONC/comments) without making that
	// history a normal-path gate.
	if err := ensureLegacyNativeDNSSnapshot(); err == nil {
		return nil
	}

	// Native Keenetic/ndnproxy does not use Xray 02_dns. If no trustworthy legacy
	// baseline remains, use a deterministic neutral fragment. The shell transaction
	// still validates the complete stripped Xray candidate before live mutation.
	data := canonicalLegacyNativeDNS
	sum := sha256.Sum256(data)
	dnsPath, hashPath := legacyNativeDNSSnapshotPaths()
	if err := writeNativeStateFile(dnsPath, string(data)); err != nil {
		return err
	}
	return writeNativeStateFile(hashPath, hex.EncodeToString(sum[:])+"\n")
}

func ensureCanonicalNativeAssignments() error {
	dir := networkBridgeNativeStateDir()
	if exists, err := networkBridgeExistingAssignmentsSnapshot(dir); err == nil && exists {
		return nil
	}

	// If assignments are still active, they are the best current fact and can be
	// saved directly. A managed/legacy Split normally has none active, so then try
	// the historical recovery path before falling back to an empty active set.
	if config, err := networkBridgeRunningConfig(); err == nil {
		if assignments, parseErr := networkBridgeAssignmentsFromRunningConfig(config); parseErr == nil && strings.TrimSpace(assignments) != "" {
			return writeNativeStateFile(filepath.Join(dir, "assignments.native"), assignments)
		}
		if _, recoverErr := networkBridgeRecoverNativeAssignmentsSnapshot(dir, networkBridgeBackupRoot(), config); recoverErr == nil {
			return nil
		}
	}
	return writeNativeStateFile(filepath.Join(dir, "assignments.native"), "")
}

func prepareCanonicalNativeApplyState() error {
	if err := ensureCanonicalNativeControlPlane(); err != nil {
		return err
	}
	if err := ensureCanonicalNativeDNSSnapshot(); err != nil {
		return err
	}
	if err := ensureCanonicalNativeAssignments(); err != nil {
		return err
	}
	return nil
}

func beginNetworkApplyFlight(target string) (*networkApplyFlight, bool) {
	networkApplyFlights.mu.Lock()
	defer networkApplyFlights.mu.Unlock()
	if current := networkApplyFlights.current; current != nil {
		if current.target == target {
			return current, false
		}
		// A different target is a real concurrent mutation request. Return a
		// completed follower result without replacing or disturbing the leader.
		done := make(chan struct{})
		close(done)
		return &networkApplyFlight{
			target: target,
			done:   done,
			status: http.StatusLocked,
			result: networkApplyResponse{
				Success:       false,
				Applied:       false,
				Operation:     "network",
				RollbackState: "NOT_APPLIED",
				Error:         "FreeNet выполняет другую сетевую цель; параллельная mutation заблокирована",
			},
		}, false
	}
	flight := &networkApplyFlight{target: target, done: make(chan struct{})}
	networkApplyFlights.current = flight
	return flight, true
}

func finishNetworkApplyFlight(flight *networkApplyFlight, status int, result networkApplyResponse) {
	networkApplyFlights.mu.Lock()
	defer networkApplyFlights.mu.Unlock()
	if networkApplyFlights.current != flight {
		return
	}
	flight.status = status
	flight.result = result
	networkApplyFlights.current = nil
	close(flight.done)
}

func waitNetworkApplyFlight(r *http.Request, flight *networkApplyFlight) (int, networkApplyResponse, bool) {
	if flight == nil {
		return http.StatusInternalServerError, networkApplyResponse{Success: false, Error: "network operation flight unavailable"}, true
	}
	select {
	case <-flight.done:
		status := flight.status
		if status == 0 {
			status = http.StatusInternalServerError
		}
		return status, flight.result, true
	case <-r.Context().Done():
		return 0, networkApplyResponse{}, false
	}
}
