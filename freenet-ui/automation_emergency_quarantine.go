package main

import (
	"crypto/sha256"
	"strings"
	"sync"
	"time"
)

const (
	// A short memory, not a permanent denylist. Availability is always
	// rechecked through the existing independent VPN HTTPS admission.
	automationEmergencyFailureTTL = 5 * time.Minute
	automationEmergencyFailureCap = 128
)

// No credential, subscription URL, URI, or provider address is retained.
// Identical ingress endpoints may contain independent logical VLESS exits, so
// the key must identify the exact credential-bearing catalog row, not IP:port.
type automationEmergencyFailureMemory struct {
	sync.Mutex
	until map[[32]byte]time.Time
}

var automationEmergencyRecentFailures automationEmergencyFailureMemory

func automationEmergencyFailureKey(candidate bestServerInternalCandidate) ([32]byte, bool) {
	raw := strings.TrimSpace(candidate.Raw)
	if raw == "" {
		return [32]byte{}, false
	}
	return sha256.Sum256([]byte(raw)), true
}

func (memory *automationEmergencyFailureMemory) active(candidate bestServerInternalCandidate, now time.Time) bool {
	key, ok := automationEmergencyFailureKey(candidate)
	if !ok {
		return false
	}
	memory.Lock()
	defer memory.Unlock()
	until, found := memory.until[key]
	if !found {
		return false
	}
	if !now.Before(until) {
		delete(memory.until, key)
		return false
	}
	return true
}

// Only a complete, two-round, app-level HTTPS rejection from a running
// isolated SOCKS probe may hold a candidate. RTT misses, canceled probes,
// incomplete checks, and missing probe resources must not poison the pool.
func (memory *automationEmergencyFailureMemory) observe(candidate bestServerInternalCandidate, result bestServerProbeResult, now time.Time) {
	key, ok := automationEmergencyFailureKey(candidate)
	if !ok {
		return
	}
	if !result.OK && !result.ConfirmedApplicationFailure {
		return
	}

	memory.Lock()
	defer memory.Unlock()
	if result.OK {
		delete(memory.until, key)
		return
	}
	if memory.until == nil {
		memory.until = make(map[[32]byte]time.Time)
	}
	for item, expiry := range memory.until {
		if !now.Before(expiry) {
			delete(memory.until, item)
		}
	}
	if len(memory.until) >= automationEmergencyFailureCap {
		var oldest [32]byte
		var oldestExpiry time.Time
		for item, expiry := range memory.until {
			if oldestExpiry.IsZero() || expiry.Before(oldestExpiry) {
				oldest, oldestExpiry = item, expiry
			}
		}
		delete(memory.until, oldest)
	}
	memory.until[key] = now.Add(automationEmergencyFailureTTL)
}

func automationEmergencyTakeCohort(candidates []bestServerInternalCandidate, manualCountry string, limit int) []bestServerInternalCandidate {
	if limit <= 0 || len(candidates) == 0 {
		return nil
	}
	if manualCountry != "" {
		return automationEmergencyManualCountryCohort(candidates, manualCountry, limit)
	}
	if len(candidates) > limit {
		candidates = candidates[:limit]
	}
	return append([]bestServerInternalCandidate(nil), candidates...)
}

// The quarantine is a *preference*, never a terminal block. If there are
// fewer fresh alternatives than the bounded budget, quarantined nodes fill the
// remaining slots and undergo fresh RTT+application checks. This preserves
// emergency recovery even when every allowed node was recently rejected.
func (memory *automationEmergencyFailureMemory) cohort(candidates []bestServerInternalCandidate, manualCountry string, limit int, now time.Time) (selected []bestServerInternalCandidate, quarantined, fallback int) {
	if limit <= 0 || len(candidates) == 0 {
		return nil, 0, 0
	}
	fresh := make([]bestServerInternalCandidate, 0, len(candidates))
	cooling := make([]bestServerInternalCandidate, 0)
	for _, candidate := range candidates {
		if memory.active(candidate, now) {
			cooling = append(cooling, candidate)
		} else {
			fresh = append(fresh, candidate)
		}
	}
	selected = automationEmergencyTakeCohort(fresh, manualCountry, limit)
	if len(selected) < limit {
		reserve := automationEmergencyTakeCohort(cooling, manualCountry, limit-len(selected))
		selected = append(selected, reserve...)
		fallback = len(reserve)
	}
	return selected, len(cooling), fallback
}

// Fresh candidates always receive application admission before quarantined
// reserves, regardless of their lower measured RTT. Country preference is
// preserved *within* each tier and cannot resurrect a known-bad endpoint first.
func (memory *automationEmergencyFailureMemory) applicationOrder(candidates []bestServerInternalCandidate, manualCountry string, now time.Time) []bestServerInternalCandidate {
	fresh := make([]bestServerInternalCandidate, 0, len(candidates))
	cooling := make([]bestServerInternalCandidate, 0)
	for _, candidate := range candidates {
		if memory.active(candidate, now) {
			cooling = append(cooling, candidate)
		} else {
			fresh = append(fresh, candidate)
		}
	}
	if manualCountry != "" {
		fresh = automationEmergencyCountryFirst(fresh, manualCountry)
		cooling = automationEmergencyCountryFirst(cooling, manualCountry)
	}
	return append(fresh, cooling...)
}
