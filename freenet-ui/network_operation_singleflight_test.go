package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestNetworkApplySingleflightJoinsSameTarget(t *testing.T) {
	networkApplyFlights = networkApplySingleflight{}
	leader, first := beginNetworkApplyFlight("vladlink\x00xkeen")
	if !first {
		t.Fatal("first request must lead")
	}
	follower, second := beginNetworkApplyFlight("vladlink\x00xkeen")
	if second || follower != leader {
		t.Fatal("same target must join the existing flight")
	}

	want := networkApplyResponse{Success: true, Applied: true, Operation: "network", ISP: "vladlink", DNSMode: "xkeen"}
	finishNetworkApplyFlight(leader, http.StatusOK, want)
	r := httptest.NewRequest(http.MethodPost, "/api/network-profile/apply", nil)
	status, got, ok := waitNetworkApplyFlight(r, follower)
	if !ok || status != http.StatusOK || !got.Success || got.ISP != want.ISP || got.DNSMode != want.DNSMode {
		t.Fatalf("follower did not receive leader result: status=%d result=%+v ok=%v", status, got, ok)
	}
}

func TestNetworkApplySingleflightRejectsDifferentTargetWithoutReplacingFlight(t *testing.T) {
	networkApplyFlights = networkApplySingleflight{}
	first, leader := beginNetworkApplyFlight("vladlink\x00xkeen")
	if !leader {
		t.Fatal("first request must lead")
	}
	other, secondLeader := beginNetworkApplyFlight("vladlink\x00firmware")
	if secondLeader || other == first {
		t.Fatal("different target must not become leader or replace the current flight")
	}
	r := httptest.NewRequest(http.MethodPost, "/api/network-profile/apply", nil)
	status, got, ok := waitNetworkApplyFlight(r, other)
	if !ok || status != http.StatusLocked || got.Success || got.RollbackState != "NOT_APPLIED" {
		t.Fatalf("different target conflict mismatch: status=%d result=%+v ok=%v", status, got, ok)
	}
	if networkApplyFlights.current != first {
		t.Fatal("different target must preserve the existing flight")
	}
	finishNetworkApplyFlight(first, http.StatusOK, networkApplyResponse{Success: true})
}

func TestPrepareCanonicalNativeApplyStatePreservesKnownBaseline(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), "native-dns")
	t.Setenv("FREENET_NATIVE_DNS_STATE_DIR", stateDir)
	writeNativeDNSSnapshotForTest(t, stateDir, []byte("// existing native\n{}\n"))
	if err := os.WriteFile(filepath.Join(stateDir, "filter-engine.native"), []byte("skydns\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "intercept.native"), []byte("off\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "assignments.native"), []byte(""), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := prepareCanonicalNativeApplyState(); err != nil {
		t.Fatal(err)
	}
	engine, err := os.ReadFile(filepath.Join(stateDir, "filter-engine.native"))
	if err != nil || string(engine) != "skydns\n" {
		t.Fatalf("known native engine was overwritten: %q err=%v", engine, err)
	}
	intercept, err := os.ReadFile(filepath.Join(stateDir, "intercept.native"))
	if err != nil || string(intercept) != "off\n" {
		t.Fatalf("known native intercept was overwritten: %q err=%v", intercept, err)
	}
}
