package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNetworkApplySingleflightJoinsSameTarget(t *testing.T) {
	networkApplyFlights = networkApplySingleflight{}
	leader, first, conflict := beginNetworkApplyFlight("vladlink\x00firmware")
	if !first || conflict {
		t.Fatal("first request must lead")
	}
	follower, second, conflict := beginNetworkApplyFlight("vladlink\x00firmware")
	if second || conflict || follower != leader {
		t.Fatal("same target must join the existing flight")
	}

	want := networkApplyResponse{Success: true, Applied: true, Operation: "network", ISP: "vladlink", DNSMode: "firmware"}
	finishNetworkApplyFlight(leader, http.StatusOK, want)
	r := httptest.NewRequest(http.MethodPost, "/api/network-profile/apply", nil)
	status, got, ok := waitNetworkApplyFlight(r, follower)
	if !ok || status != http.StatusOK || !got.Success || got.ISP != want.ISP || got.DNSMode != want.DNSMode {
		t.Fatalf("follower did not receive leader result: status=%d result=%+v ok=%v", status, got, ok)
	}
}

func TestNetworkApplySingleflightRejectsDifferentTargetWithoutReplacingFlight(t *testing.T) {
	networkApplyFlights = networkApplySingleflight{}
	first, leader, conflict := beginNetworkApplyFlight("vladlink\x00firmware")
	if !leader || conflict {
		t.Fatal("first request must lead")
	}
	current, secondLeader, conflict := beginNetworkApplyFlight("vladlink\x00xkeen")
	if secondLeader || !conflict || current != first {
		t.Fatal("different target must report conflict and preserve the current flight")
	}
	finishNetworkApplyFlight(first, http.StatusOK, networkApplyResponse{Success: true})
}
