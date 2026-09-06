package main

import (
	"errors"
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

func ensureCanonicalNativeAssignments() error {
	dir := networkBridgeNativeStateDir()
	if exists, err := networkBridgeExistingAssignmentsSnapshot(dir); err == nil && exists {
		return nil
	}

	config, configErr := networkBridgeRunningConfig()
	if configErr == nil {
		assignments, parseErr := networkBridgeAssignmentsFromRunningConfig(config)
		if parseErr == nil && strings.TrimSpace(assignments) != "" {
			return writeNativeStateFile(filepath.Join(dir, "assignments.native"), assignments)
		}
		// Legacy managed Split detached assignments. Recover them when an exact
		// local snapshot exists; otherwise canonical Native has no assignments.
		if strings.Contains(config, "opkg dns-override") {
			if _, recoverErr := networkBridgeRecoverNativeAssignmentsSnapshot(dir, networkBridgeBackupRoot(), config); recoverErr == nil {
				return nil
			}
		}
	}
	return writeNativeStateFile(filepath.Join(dir, "assignments.native"), "")
}

func prepareCanonicalNativeApplyState() error {
	// 02_dns is not part of the live Native DNS path, but the Xray candidate
	// still needs a validated neutral fragment before we remove Split-only DNS.
	if err := ensureLegacyNativeDNSSnapshot(); err != nil {
		return err
	}
	if err := ensureCanonicalNativeAssignments(); err != nil {
		return err
	}

	// Direct DNS has one deterministic product target. Historical engine/intercept
	// hints no longer gate normal Apply; they are normalized to the canonical
	// Keenetic Native control-plane and the live pre-state is still covered by the
	// transaction snapshot/rollback inside apply_network_profile.sh.
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
		return http.StatusInternalServerError, networkApplyResponse{Success: false, Error: errors.New("network operation flight unavailable").Error()}, true
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
