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

func ensureCanonicalNativeDNSSnapshot() error {
	valid, _, err := legacyNativeDNSSnapshotStatus()
	if err == nil && valid {
		return nil
	}

	// Native Keenetic/ndnproxy does not use Xray 02_dns. When a legacy install
	// has no trustworthy snapshot, the deterministic neutral fragment is enough:
	// the shell transaction still validates the complete stripped Xray candidate
	// before any live DNS mutation.
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

	// Preserve assignments that are still present in a native control-plane.
	// A legacy Split may already have detached them; in that case there is no
	// reliable current assignment fact, so canonical Native uses an empty active
	// assignment set instead of blocking the whole DNS transition.
	if config, err := networkBridgeRunningConfig(); err == nil {
		if assignments, parseErr := networkBridgeAssignmentsFromRunningConfig(config); parseErr == nil && strings.TrimSpace(assignments) != "" {
			return writeNativeStateFile(filepath.Join(dir, "assignments.native"), assignments)
		}
	}
	return writeNativeStateFile(filepath.Join(dir, "assignments.native"), "")
}

func prepareCanonicalNativeApplyState() error {
	if err := ensureCanonicalNativeDNSSnapshot(); err != nil {
		return err
	}
	if err := ensureCanonicalNativeAssignments(); err != nil {
		return err
	}

	// Direct DNS has one deterministic fallback control-plane for legacy states.
	// These files are FreeNet-owned migration state only; the live shell apply
	// still snapshots the real pre-state, validates the candidate, performs
	// acceptance and rolls back on any failure.
	dir := nativeDNSStateDir()
	if err := writeNativeStateFile(filepath.Join(dir, "filter-engine.native"), "public\n"); err != nil {
		return err
	}
	if err := writeNativeStateFile(filepath.Join(dir, "intercept.native"), "on\n"); err != nil {
		return err
	}
	return nil
}

func beginNetworkApplyFlight(target string) (*networkApplyFlight, bool) {
	networkApplyFlights.mu.Lock()
	if current := networkApplyFlights.current; current != nil {
		if current.target == target {
			networkApplyFlights.mu.Unlock()
			return current, false
		}
		// A different target is a real concurrent mutation request. Return a
		// completed follower result without replacing or disturbing the leader.
		done := make(chan struct{})
		close(done)
		networkApplyFlights.mu.Unlock()
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
	networkApplyFlights.mu.Unlock()

	if strings.HasSuffix(target, "\x00firmware") {
		if err := prepareCanonicalNativeApplyState(); err != nil {
			finishNetworkApplyFlight(flight, http.StatusServiceUnavailable, networkApplyResponse{
				Success:       false,
				Applied:       false,
				Operation:     "network",
				RollbackState: "NOT_APPLIED",
				PrimaryError:  err.Error(),
				Error:         "не удалось подготовить canonical Native DNS state",
			})
			return flight, false
		}
	}
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
