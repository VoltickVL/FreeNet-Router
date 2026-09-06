package main

import (
	"net/http"
	"sync"
)

// networkApplySingleflight collapses duplicate browser submissions for the
// same ISP/DNS target into one authoritative mutation. Followers never start a
// second mutation: they wait for the leader and receive the exact same result.
// A different target is not joined and is handled by the global mutation
// semaphore in the normal way.
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

func beginNetworkApplyFlight(target string) (*networkApplyFlight, bool) {
	networkApplyFlights.mu.Lock()
	defer networkApplyFlights.mu.Unlock()
	if current := networkApplyFlights.current; current != nil && current.target == target {
		return current, false
	}
	flight := &networkApplyFlight{target: target, done: make(chan struct{})}
	networkApplyFlights.current = flight
	return flight, true
}

func finishNetworkApplyFlight(flight *networkApplyFlight, status int, result networkApplyResponse) {
	networkApplyFlights.mu.Lock()
	if networkApplyFlights.current == flight {
		flight.status = status
		flight.result = result
		networkApplyFlights.current = nil
		close(flight.done)
	}
	networkApplyFlights.mu.Unlock()
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
