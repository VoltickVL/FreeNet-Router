package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"\n\t"time"
)

func newVPNTransactionDoctorFixture(t *testing.T) (*app, string, map[string]string) {
	t.Helper()
	dir := t.TempDir()
	checkpoint := filepath.Join(dir, "checkpoint")
	if err := os.Mkdir(checkpoint, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FREENET_VPN_TRANSACTION_DIR", checkpoint)
	t.Setenv("FREENET_AUTOMATION_STATE", filepath.Join(dir, "automation.state"))
	t.Setenv("FREENET_PROFILE_FILE", filepath.Join(dir, "vpn_profile_name"))
	if err := os.WriteFile(filepath.Join(checkpoint, "state"), []byte("PENDING\n"), 0600); err != nil {
		t.Fatal(err)
	}
	live := map[string]string{
		"out": filepath.Join(dir, "04_outbounds.json"),
		"profile": filepath.Join(dir, "vpn_profile_name"),
		"filter": filepath.Join(dir, "profile_filter.regex"),
	}
	// The fixture intentionally contains fake credentials. Neither the HTTP
	// endpoint nor the journal diagnostics may echo them or their hashes.
	private := map[string]string{
		"out": `{"outbounds":[{"protocol":"vless","secret":"TEST-SENSITIVE-OUTBOUND"}]}`,
		"profile": "TEST-SENSITIVE-PROFILE",
		"filter": "TEST-SENSITIVE-FILTER",
	}
	for kind, path := range live {
		if err := os.WriteFile(path, []byte(private[kind]), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(checkpoint, kind+".before"), []byte(private[kind]), 0600); err != nil {
			t.Fatal(err)
		}
	}
	a := &app{cfg: config{OutPath: live["out"], FilterPath: live["filter"]}, sem: make(chan struct{}, 1)}
	return a, checkpoint, live
}

func TestVPNTransactionDoctorReadOnlyMatchingFilesStillSTOP(t *testing.T) {
	a, checkpoint, live := newVPNTransactionDoctorFixture(t)
	before := map[string][]byte{}
	for kind, path := range live {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		before[kind] = data
	}
	result := a.vpnTransactionDoctor()
	if !result.MutationBlocked || result.Checkpoint != "pending" || !result.FilesRestored ||
		result.Outbound != "matched" || result.Profile != "matched" || result.Filter != "matched" ||
		!strings.HasPrefix(result.Decision, "STOP_") || result.Mutation != "NONE" {
		t.Fatalf("checkpoint was treated as safe to unlock: %+v", result)
	}
	w := httptest.NewRecorder()
	a.handleVPNTransactionDoctor(w, httptest.NewRequest(http.MethodGet, "/api/vpn/transaction-doctor", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "\"read-only\"") {
		t.Fatalf("doctor HTTP failed: %d %s", w.Code, w.Body.String())
	}
	for _, sensitive := range []string{"TEST-SENSITIVE-OUTBOUND", "TEST-SENSITIVE-PROFILE", "TEST-SENSITIVE-FILTER", checkpoint} {
		if strings.Contains(w.Body.String(), sensitive) {
			t.Fatalf("private data leaked in doctor JSON: %q", sensitive)
		}
	}
	// Journal export has the same read-only result; the old entire diagnostic
	// export and the standalone endpoint never mutate live config or checkpoint.
	export := a.journalDiagnostics(time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC))
	encoded, err := json.Marshal(export)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), "\"vpn_transaction\"") || strings.Contains(string(encoded), "TEST-SENSITIVE-OUTBOUND") {
		t.Fatalf("journal VPN transaction summary missing or leaked contents: %s", encoded)
	}
	for kind, path := range live {
		after, err := os.ReadFile(path)
		if err != nil || string(after) != string(before[kind]) {
			t.Fatalf("read-only doctor altered %s, err=%v", kind, err)
		}
	}
	if !vpnTransactionPending() {
		t.Fatal("doctor incorrectly cleared durable checkpoint")
	}
}


func TestVPNTransactionDoctorMismatchAndInvalidCheckpoint(t *testing.T) {
	t.Run("mismatched-live-outbound", func(t *testing.T) {
		a, _, live := newVPNTransactionDoctorFixture(t)
		if err := os.WriteFile(live["out"], []byte("modified candidate"), 0600); err != nil {
			t.Fatal(err)
		}
		result := a.vpnTransactionDoctor()
		if result.Outbound != "different" || result.FilesRestored || !result.MutationBlocked ||
			result.Decision != "STOP_FILES_UNRESOLVED" {
			t.Fatalf("unsafe mismatch accepted: %+v", result)
		}
	})
	t.Run("symlinked-checkpoint-entry", func(t *testing.T) {
		a, checkpoint, _ := newVPNTransactionDoctorFixture(t)
		path := filepath.Join(checkpoint, "profile.before")
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join(checkpoint, "out.before"), path); err != nil {
			t.Skipf("symlink not supported: %v", err)
		}
		result := a.vpnTransactionDoctor()
		if result.Profile != "unknown" || result.FilesRestored || !result.MutationBlocked {
			t.Fatalf("unsafe checkpoint entry accepted: %+v", result)
		}
	})
	t.Run("invalid-state", func(t *testing.T) {
		a, checkpoint, _ := newVPNTransactionDoctorFixture(t)
		if err := os.WriteFile(filepath.Join(checkpoint, "state"), []byte("COMMITTED\n"), 0600); err != nil {
			t.Fatal(err)
		}
		result := a.vpnTransactionDoctor()
		if result.Checkpoint != "invalid" || result.Decision != "STOP_CHECKPOINT_INVALID" || !result.MutationBlocked {
			t.Fatalf("inconsistent checkpoint accepted: %+v", result)
		}
	})
	t.Run("absent-snapshot", func(t *testing.T) {
		a, checkpoint, live := newVPNTransactionDoctorFixture(t)
		if err := os.Remove(filepath.Join(checkpoint, "filter.before")); err != nil {
			t.Fatal(err)
		}
		result := a.vpnTransactionDoctor()
		if result.Filter != "invalid" || result.FilesRestored || !result.MutationBlocked {
			t.Fatalf("incomplete checkpoint accepted: %+v", result)
		}
		if got, err := os.ReadFile(live["filter"]); err != nil || string(got) != "TEST-SENSITIVE-FILTER" {
			t.Fatalf("incomplete checkpoint diagnostic mutated live filter: err=%v", err)
		}
	})
}

func TestAutomationApplyFailureUsesPrimaryError(t *testing.T) {
	result := networkApplyResponse{
		Error: "provider profile apply failed",
		PrimaryError: "Xray/XKeen runtime acceptance failed after provider apply; stage=xkeen-start; code=3",
		RollbackState: "FAILED/UNKNOWN",
	}
	got := automationApplyFailureDetail(result)
	if !strings.Contains(got, "stage=xkeen-start; code=3") || got == result.Error {
		t.Fatalf("AUTO summary lost actionable primary error: %q", got)
	}
	if got := automationApplyFailureDetail(networkApplyResponse{}); got != "provider profile apply failed" {
		t.Fatalf("missing fallback is misleading: %q", got)
	}
}
