package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testProviderID = "0123456789abcdef"

func providerPlanOutput(id string) string {
	return strings.Join([]string{
		"========== FreeNet Provider Plan ==========",
		"PROFILE_ID=" + id,
		"PROFILE_NAME=Frankfurt, Germany, Extra",
		"ENDPOINT=203.0.113.10:443",
		"CURRENT_OUTBOUND=present",
		"XRAY_RUNNING=yes",
		"CANDIDATE_XRAY_VALID=yes",
		"EXPECTED_DELTA=replace exactly one vless-reality outbound",
		"EXPECTED_NO_DELTA=ISP/DNS/routing unchanged",
		"MUTATION=NONE",
		"========== END ==========",
	}, "\n")
}

func TestParseProviderPlanAllowlistsSafeFields(t *testing.T) {
	out := providerPlanOutput(testProviderID) + "\nUUID=TEST-SECRET\nPUBLIC_KEY=TEST-PBK\nSUBSCRIPTION=https://secret.invalid/key\n"
	plan, err := parseProviderPlan(out)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Success || !plan.CandidateValid || plan.ProfileID != testProviderID || plan.Endpoint != "203.0.113.10:443" {
		t.Fatalf("unexpected plan: %+v", plan)
	}
	b, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"TEST-SECRET", "TEST-PBK", "secret.invalid", "subscription"} {
		if strings.Contains(string(b), forbidden) {
			t.Fatalf("provider plan leaked %q: %s", forbidden, b)
		}
	}
}

func TestProviderPlanRejectsMutationAndInvalidID(t *testing.T) {
	if _, err := parseProviderPlan(strings.Replace(providerPlanOutput(testProviderID), "MUTATION=NONE", "MUTATION=PENDING", 1)); err == nil {
		t.Fatal("mutating provider plan must be rejected")
	}
	for _, id := range []string{"", "ABCDEF0123456789", "0123456789abcdeg", "123"} {
		if validProfileID(id) {
			t.Fatalf("invalid profile id accepted: %q", id)
		}
	}
}

func TestNetworkPlanCanAttachProviderPlanWithoutChangingNetworkPlan(t *testing.T) {
	provider := writeFakeNetworkHelper(t, "[ \"$1\" = plan ] || exit 9\n[ \"$2\" = \""+testProviderID+"\" ] || exit 8\ncat <<'EOF'\n"+providerPlanOutput(testProviderID)+"\nEOF")
	network := writeFakeNetworkHelper(t, "[ \"$1\" = plan ] || exit 9\ncat <<'EOF'\n"+supportedPlanOutput()+"\nEOF")
	t.Setenv("FREENET_PROVIDER_HELPER", provider)
	t.Setenv("FREENET_NETWORK_HELPER", network)
	a := testNetworkApp(t, "ISP_ID=rostelecom\nDNS_MODE=firmware\n")

	r := httptest.NewRequest(http.MethodGet, "http://192.168.50.1:1001/api/network-profile/plan?provider_profile_id="+testProviderID, nil)
	w := httptest.NewRecorder()
	a.handleNetworkProfilePlan(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var plan networkPlanResponse
	if err := json.Unmarshal(w.Body.Bytes(), &plan); err != nil {
		t.Fatal(err)
	}
	if !plan.Supported || plan.ProviderPlan == nil || !plan.ProviderPlan.CandidateValid || plan.ProviderPlan.ProfileID != testProviderID {
		t.Fatalf("unexpected combined plan: %+v", plan)
	}
}

func TestProviderApplyRequiresConfirmAndFreshPlan(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "provider-applied")
	provider := writeFakeNetworkHelper(t, "if [ \"$1\" = plan ]; then\ncat <<'EOF'\n"+providerPlanOutput(testProviderID)+"\nEOF\nexit 0\nfi\n[ \"$1\" = apply ] || exit 9\n[ \"$2\" = \""+testProviderID+"\" ] || exit 8\necho applied > \""+marker+"\"\necho '[FreeNet Provider] RESULT=SUCCESS'\nexit 0")
	network := writeFakeNetworkHelper(t, "[ \"$1\" = plan ] || exit 9\ncat <<'EOF'\n"+supportedPlanOutput()+"\nEOF")
	t.Setenv("FREENET_PROVIDER_HELPER", provider)
	t.Setenv("FREENET_NETWORK_HELPER", network)
	a := testNetworkApp(t, "ISP_ID=rostelecom\nDNS_MODE=firmware\n")

	noConfirm := `{"operation":"provider","profile_id":"` + testProviderID + `","confirm":false}`
	r := httptest.NewRequest(http.MethodPost, "http://192.168.50.1:1001/api/network-profile/apply", strings.NewReader(noConfirm))
	r.Host = "192.168.50.1:1001"
	r.Header.Set("Origin", "http://192.168.50.1:1001")
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	a.handleNetworkProfileApply(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("no-confirm status=%d body=%s", w.Code, w.Body.String())
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("provider helper ran without explicit confirmation")
	}

	confirmed := `{"operation":"provider","profile_id":"` + testProviderID + `","confirm":true}`
	r = httptest.NewRequest(http.MethodPost, "http://192.168.50.1:1001/api/network-profile/apply", strings.NewReader(confirmed))
	r.Host = "192.168.50.1:1001"
	r.Header.Set("Origin", "http://192.168.50.1:1001")
	r.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	a.handleNetworkProfileApply(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("confirmed status=%d body=%s", w.Code, w.Body.String())
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("provider helper did not run: %v", err)
	}
	var resp networkApplyResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.Success || !resp.Applied || resp.Operation != "provider" || resp.ProviderPlan == nil || resp.RollbackState != "NOT_NEEDED" {
		t.Fatalf("unexpected provider apply response: %+v", resp)
	}
}

func TestProviderApplyFailureSeparatesPrimaryAndRollback(t *testing.T) {
	provider := writeFakeNetworkHelper(t, "if [ \"$1\" = plan ]; then\ncat <<'EOF'\n"+providerPlanOutput(testProviderID)+"\nEOF\nexit 0\nfi\necho '[FreeNet Provider] ERROR: PRIMARY ERROR: Xray restart failed' >&2\necho '[FreeNet Provider] ERROR: ROLLBACK ERROR/STATE: rollback success' >&2\nexit 1")
	t.Setenv("FREENET_PROVIDER_HELPER", provider)
	a := testNetworkApp(t, "ISP_ID=rostelecom\nDNS_MODE=firmware\n")

	payload := `{"operation":"provider","profile_id":"` + testProviderID + `","confirm":true}`
	r := httptest.NewRequest(http.MethodPost, "http://192.168.50.1:1001/api/network-profile/apply", strings.NewReader(payload))
	r.Host = "192.168.50.1:1001"
	r.Header.Set("Origin", "http://192.168.50.1:1001")
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	a.handleNetworkProfileApply(w, r)
	if w.Code != http.StatusBadGateway {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var resp networkApplyResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.PrimaryError == "" || resp.RollbackState != "SUCCESS" {
		t.Fatalf("failure state not separated: %+v", resp)
	}
}
