package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeFakeNetworkHelper(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "network-helper.sh")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body+"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	return p
}

func testNetworkApp(t *testing.T, configText string) *app {
	t.Helper()
	dir := t.TempDir()
	configPath := filepath.Join(dir, "freenet.conf")
	if err := os.WriteFile(configPath, []byte(configText), 0600); err != nil {
		t.Fatal(err)
	}
	return &app{cfg: config{ConfigPath: configPath, Timeout: 5 * time.Second}, sem: make(chan struct{}, 1)}
}

func supportedPlanOutput() string {
	return strings.Join([]string{
		"========== FreeNet Network Plan ==========",
		"ISP_ID=rostelecom",
		"DNS_MODE=firmware",
		"EFFECTIVE_DNS_MODE=firmware",
		"SUPPORTED=yes",
		"REASON=verified WORK preset",
		"PROXY_DNS=on",
		"PORT53_OWNER=ndnproxy",
		"XRAY_GID=11111",
		"DNS_OUT=yes",
		"VLESS_PROFILE=yes",
		"EXPECTED_DELTA=preserve firmware ndnproxy :53; add/repair split DNS",
		"EXPECTED_NO_DELTA=no VLESS credential rewrite",
		"MUTATION=NONE",
		"========== END ==========",
	}, "\n")
}

func TestParseNetworkPlanAllowlistsFieldsAndDropsSecrets(t *testing.T) {
	out := supportedPlanOutput() + "\nUUID=SECRET_TEST_UUID\nSUBSCRIPTION=https://secret.invalid/key\nPUBLIC_KEY=SECRET_TEST_KEY\n"
	plan, err := parseNetworkPlan(out)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Success || !plan.Supported || plan.ISP != "rostelecom" || plan.Port53Owner != "ndnproxy" || plan.XrayGID != "11111" {
		t.Fatalf("unexpected plan: %+v", plan)
	}
	b, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	text := string(b)
	for _, forbidden := range []string{"SECRET_TEST_UUID", "SECRET_TEST_KEY", "secret.invalid", "subscription"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("plan response leaked forbidden material %q: %s", forbidden, text)
		}
	}
}

func TestParseNetworkPlanRejectsUnexpectedMutation(t *testing.T) {
	out := strings.Replace(supportedPlanOutput(), "MUTATION=NONE", "MUTATION=APPLY", 1)
	if _, err := parseNetworkPlan(out); err == nil {
		t.Fatal("plan with mutation must be rejected")
	}
}

func TestNetworkPlanHandlerUsesExactAllowlistedHelper(t *testing.T) {
	helper := writeFakeNetworkHelper(t, "[ \"$1\" = plan ] || exit 9\ncat <<'EOF'\n"+supportedPlanOutput()+"\nEOF")
	t.Setenv("FREENET_NETWORK_HELPER", helper)
	a := testNetworkApp(t, "ISP_ID=rostelecom\nDNS_MODE=firmware\n")

	r := httptest.NewRequest(http.MethodGet, "http://192.168.50.1:1001/api/network-profile/plan", nil)
	w := httptest.NewRecorder()
	a.handleNetworkProfilePlan(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), helper) {
		t.Fatal("helper path must not be exposed")
	}
	var plan networkPlanResponse
	if err := json.Unmarshal(w.Body.Bytes(), &plan); err != nil {
		t.Fatal(err)
	}
	if !plan.Supported || plan.Mutation != "NONE" {
		t.Fatalf("unexpected plan: %+v", plan)
	}
}

func TestNetworkApplyRequiresExplicitConfirmationAndSavedProfileMatch(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "applied")
	helper := writeFakeNetworkHelper(t, "if [ \"$1\" = plan ]; then\ncat <<'EOF'\n"+supportedPlanOutput()+"\nEOF\nexit 0\nfi\necho applied > \""+marker+"\"\necho '[FreeNet Network] RESULT=SUCCESS'")
	t.Setenv("FREENET_NETWORK_HELPER", helper)
	a := testNetworkApp(t, "ISP_ID=rostelecom\nDNS_MODE=firmware\n")

	for name, payload, wantCode := range map[string]struct {
		payload string
		code    int
	}{
		"no-confirm": {`{"isp":"rostelecom","dns_mode":"firmware","confirm":false}`, http.StatusBadRequest},
		"stale-profile": {`{"isp":"vladlink","dns_mode":"xkeen","confirm":true}`, http.StatusConflict},
	} {
		t.Run(name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "http://192.168.50.1:1001/api/network-profile/apply", strings.NewReader(payload.payload))
			r.Host = "192.168.50.1:1001"
			r.Header.Set("Origin", "http://192.168.50.1:1001")
			r.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			a.handleNetworkProfileApply(w, r)
			if w.Code != wantCode.code {
				t.Fatalf("status=%d want=%d body=%s", w.Code, wantCode.code, w.Body.String())
			}
		})
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("apply helper ran without a valid confirmed saved profile")
	}
}

func TestNetworkApplyRunsPlanBeforeApplyAndReturnsPostPlan(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "applied")
	helper := writeFakeNetworkHelper(t, "if [ \"$1\" = plan ]; then\ncat <<'EOF'\n"+supportedPlanOutput()+"\nEOF\nexit 0\nfi\necho applied > \""+marker+"\"\necho '[FreeNet Network] RESULT=SUCCESS'\nexit 0")
	t.Setenv("FREENET_NETWORK_HELPER", helper)
	a := testNetworkApp(t, "ISP_ID=rostelecom\nDNS_MODE=firmware\n")

	payload := `{"isp":"rostelecom","dns_mode":"firmware","confirm":true}`
	r := httptest.NewRequest(http.MethodPost, "http://192.168.50.1:1001/api/network-profile/apply", strings.NewReader(payload))
	r.Host = "192.168.50.1:1001"
	r.Header.Set("Origin", "http://192.168.50.1:1001")
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	a.handleNetworkProfileApply(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("apply helper did not run: %v", err)
	}
	var resp networkApplyResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.Success || !resp.Applied || resp.RollbackState != "NOT_NEEDED" || !resp.Plan.Supported {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestNetworkApplyFailureSeparatesPrimaryAndRollbackState(t *testing.T) {
	helper := writeFakeNetworkHelper(t, "if [ \"$1\" = plan ]; then\ncat <<'EOF'\n"+supportedPlanOutput()+"\nEOF\nexit 0\nfi\necho '[FreeNet Network] ERROR: PRIMARY ERROR: DNS migration failed and reported rollback success/no live apply' >&2\nexit 1")
	t.Setenv("FREENET_NETWORK_HELPER", helper)
	a := testNetworkApp(t, "ISP_ID=rostelecom\nDNS_MODE=firmware\n")

	payload := `{"isp":"rostelecom","dns_mode":"firmware","confirm":true}`
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
	if resp.PrimaryError == "" || resp.RollbackState != "SUCCESS_OR_NOT_APPLIED" {
		t.Fatalf("failure not separated: %+v", resp)
	}
}
