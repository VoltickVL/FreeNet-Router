package main

import (
	"net/http"
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

// Returns (flight, leader, targetConflict). A different target never replaces
// the current flight: it is rejected as a real concurrent mutation request.
func beginNetworkApplyFlight(target string) (*networkApplyFlight, bool, bool) {
	networkApplyFlights.mu.Lock()
	defer networkApplyFlights.mu.Unlock()
	if current := networkApplyFlights.current; current != nil {
		if current.target == target {
			return current, false, false
		}
		return current, false, true
	}
	flight := &networkApplyFlight{target: target, done: make(chan struct{})}
	networkApplyFlights.current = flight
	return flight, true, false
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
