package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNetworkApplySingleflightJoinsSameTarget(t *testing.T) {
	networkApplyFlights = networkApplySingleflight{}
	leader, first := beginNetworkApplyFlight("vladlink\x00firmware")
	if !first {
		t.Fatal("first request must lead")
	}
	follower, second := beginNetworkApplyFlight("vladlink\x00firmware")
	if second || follower != leader {
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

func TestNetworkApplySingleflightDoesNotJoinDifferentTarget(t *testing.T) {
	networkApplyFlights = networkApplySingleflight{}
	first, leader := beginNetworkApplyFlight("vladlink\x00firmware")
	if !leader {
		t.Fatal("first request must lead")
	}
	second, secondLeader := beginNetworkApplyFlight("vladlink\x00xkeen")
	if !secondLeader || second == first {
		t.Fatal("different target must not join the current target")
	}
	finishNetworkApplyFlight(second, http.StatusLocked, networkApplyResponse{Success: false})
}
