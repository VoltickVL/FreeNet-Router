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
		"REASON=профиль поддерживается",
		"PROXY_DNS=off",
		"PORT53_OWNER=ndnproxy",
		"XRAY_GID=11111",
		"DNS_ROUTING_MODE=standard",
		"DNS_OUT=yes",
		"VLESS_PROFILE=yes",
		"EXPECTED_DELTA=проверить и применить выбранный DNS режим",
		"EXPECTED_NO_DELTA=no VLESS credential rewrite",
		"MUTATION=NONE",
		"========== END ==========",
	}, "\n")
}

func dynamicPlanHelper(applyTail string) string {
	return `CONF="$FREENET_CONFIG_FILE"
ISP="$(sed -n 's/^ISP_ID=//p' "$CONF" | tail -n 1 | tr -d "'\"")"
DNS="$(sed -n 's/^DNS_MODE=//p' "$CONF" | tail -n 1 | tr -d "'\"")"
case "$DNS" in
  auto|firmware) EFFECTIVE=firmware; ROUTING=standard ;;
  xkeen) EFFECTIVE=xkeen; ROUTING=split ;;
  custom) EFFECTIVE=custom; ROUTING=unknown ;;
esac
if [ "$1" = plan ]; then
cat <<EOF
========== FreeNet Network Plan ==========
ISP_ID=$ISP
DNS_MODE=$DNS
EFFECTIVE_DNS_MODE=$EFFECTIVE
SUPPORTED=yes
REASON=профиль поддерживается
PROXY_DNS=off
PORT53_OWNER=ndnproxy
XRAY_GID=11111
DNS_ROUTING_MODE=$ROUTING
DNS_OUT=yes
VLESS_PROFILE=yes
EXPECTED_DELTA=применить выбранный draft
EXPECTED_NO_DELTA=no VLESS credential rewrite
MUTATION=NONE
========== END ==========
EOF
exit 0
fi
` + applyTail
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

func TestNetworkPlanUsesDraftWithoutPersistingIt(t *testing.T) {
	helper := writeFakeNetworkHelper(t, dynamicPlanHelper("exit 0"))
	t.Setenv("FREENET_NETWORK_HELPER", helper)
	a := testNetworkApp(t, "ISP_ID=rostelecom\nDNS_MODE=firmware\nSETUP_COMPLETE=yes\n")
	before, err := os.ReadFile(a.cfg.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}

	r := httptest.NewRequest(http.MethodGet, "http://192.168.50.1:1001/api/network-profile/plan?isp=vladlink&dns_mode=xkeen", nil)
	w := httptest.NewRecorder()
	a.handleNetworkProfilePlan(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var plan networkPlanResponse
	if err := json.Unmarshal(w.Body.Bytes(), &plan); err != nil {
		t.Fatal(err)
	}
	if plan.ISP != "vladlink" || plan.DNSMode != "xkeen" || plan.ActiveISP != "rostelecom" || plan.ActiveDNSMode != "firmware" || plan.Active {
		t.Fatalf("draft/active state mixed: %+v", plan)
	}
	after, err := os.ReadFile(a.cfg.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatalf("read-only plan mutated config\nbefore=%s\nafter=%s", before, after)
	}
}

func TestNetworkApplyRequiresConfirmationButNotPreSave(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "applied")
	helper := writeFakeNetworkHelper(t, dynamicPlanHelper("echo \"$ISP/$DNS\" > \""+marker+"\"\necho '[FreeNet Network] RESULT=SUCCESS'\nexit 0"))
	t.Setenv("FREENET_NETWORK_HELPER", helper)
	a := testNetworkApp(t, "ISP_ID=rostelecom\nDNS_MODE=firmware\n")

	noConfirm := httptest.NewRequest(http.MethodPost, "http://192.168.50.1:1001/api/network-profile/apply", strings.NewReader(`{"isp":"vladlink","dns_mode":"xkeen","confirm":false}`))
	noConfirm.Host = "192.168.50.1:1001"
	noConfirm.Header.Set("Origin", "http://192.168.50.1:1001")
	noConfirm.Header.Set("Content-Type", "application/json")
	noConfirmW := httptest.NewRecorder()
	a.handleNetworkProfileApply(noConfirmW, noConfirm)
	if noConfirmW.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", noConfirmW.Code, noConfirmW.Body.String())
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("apply helper ran without confirmation")
	}

	payload := `{"isp":"vladlink","dns_mode":"xkeen","confirm":true}`
	r := httptest.NewRequest(http.MethodPost, "http://192.168.50.1:1001/api/network-profile/apply", strings.NewReader(payload))
	r.Host = "192.168.50.1:1001"
	r.Header.Set("Origin", "http://192.168.50.1:1001")
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	a.handleNetworkProfileApply(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if got, err := os.ReadFile(marker); err != nil || strings.TrimSpace(string(got)) != "vladlink/xkeen" {
		t.Fatalf("helper did not receive exact draft: %q err=%v", got, err)
	}
	isp, dns := readNetworkProfileConfig(a.cfg.ConfigPath)
	if isp != "vladlink" || dns != "xkeen" {
		t.Fatalf("accepted draft not committed: %s/%s", isp, dns)
	}
}

func TestNetworkApplyFailureKeepsPreviousActiveConfig(t *testing.T) {
	helper := writeFakeNetworkHelper(t, dynamicPlanHelper("echo '[FreeNet Network] ERROR: PRIMARY ERROR: post-apply Split DNS acceptance failed' >&2\necho '[FreeNet Network] ERROR: ROLLBACK ERROR/STATE: rollback success' >&2\nexit 1"))
	t.Setenv("FREENET_NETWORK_HELPER", helper)
	a := testNetworkApp(t, "ISP_ID=rostelecom\nDNS_MODE=firmware\nSETUP_COMPLETE=yes\n")
	before, err := os.ReadFile(a.cfg.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}

	payload := `{"isp":"vladlink","dns_mode":"xkeen","confirm":true}`
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
		t.Fatalf("failure not separated: %+v", resp)
	}
	after, err := os.ReadFile(a.cfg.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatalf("failed apply changed active config\nbefore=%s\nafter=%s", before, after)
	}
}

func TestNetworkPlanHandlerUsesExactAllowlistedHelper(t *testing.T) {
	helper := writeFakeNetworkHelper(t, dynamicPlanHelper("exit 9"))
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
}
