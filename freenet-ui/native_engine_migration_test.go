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

func legacySplitPlanForTest() networkPlanResponse {
	return networkPlanResponse{
		Success:          true,
		Supported:        true,
		EffectiveDNSMode: "firmware",
		ProxyDNS:         "off",
		Port53Owner:      "xray",
		DNSRoutingMode:   "split",
		DNSOut:           true,
		VLESSProfile:     true,
		Mutation:         "NONE",
	}
}

func TestLegacyNativeEnginePlanRequiresExplicitConfirmation(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), "native-dns")
	t.Setenv("FREENET_NATIVE_DNS_STATE_DIR", stateDir)

	plan := legacySplitPlanForTest()
	enrichNativeFilterEngineMigration(&plan)
	if !plan.NativeFilterEngineConfirmRequired {
		t.Fatal("legacy Split without native filter-engine snapshot must require confirmation")
	}
	want := strings.Join([]string{"public", "interceptor", "nextdns", "skydns"}, ",")
	if got := strings.Join(plan.NativeFilterEngineChoices, ","); got != want {
		t.Fatalf("unexpected choices: %q", got)
	}
	if _, err := os.Stat(nativeFilterEngineSnapshotPath()); !os.IsNotExist(err) {
		t.Fatalf("read-only plan created migration state: %v", err)
	}
}

func TestLegacyNativeEngineConfirmationPersistsOnlyExplicitAllowedChoice(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), "native-dns")
	t.Setenv("FREENET_NATIVE_DNS_STATE_DIR", stateDir)
	plan := legacySplitPlanForTest()
	enrichNativeFilterEngineMigration(&plan)

	for _, invalid := range []string{"", "opkg", "unknown", "public;reboot"} {
		if err := persistConfirmedLegacyNativeFilterEngine(plan, invalid); err == nil {
			t.Fatalf("invalid native engine %q was accepted", invalid)
		}
		if _, err := os.Stat(nativeFilterEngineSnapshotPath()); !os.IsNotExist(err) {
			t.Fatalf("invalid choice %q created snapshot: %v", invalid, err)
		}
	}

	if err := persistConfirmedLegacyNativeFilterEngine(plan, "public"); err != nil {
		t.Fatalf("confirmed native engine was not persisted: %v", err)
	}
	data, err := os.ReadFile(nativeFilterEngineSnapshotPath())
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "public\n" {
		t.Fatalf("unexpected persisted baseline %q", data)
	}

	if err := persistConfirmedLegacyNativeFilterEngine(plan, "skydns"); err == nil {
		t.Fatal("existing confirmed snapshot must not be overwritten by stale plan")
	}
	data, err = os.ReadFile(nativeFilterEngineSnapshotPath())
	if err != nil || string(data) != "public\n" {
		t.Fatalf("confirmed baseline changed after stale request: %q err=%v", data, err)
	}
}

func TestExistingNativeEngineSnapshotDoesNotRequestMigration(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), "native-dns")
	t.Setenv("FREENET_NATIVE_DNS_STATE_DIR", stateDir)
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "filter-engine.native"), []byte("public\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	plan := legacySplitPlanForTest()
	enrichNativeFilterEngineMigration(&plan)
	if plan.NativeFilterEngineConfirmRequired || len(plan.NativeFilterEngineChoices) != 0 {
		t.Fatalf("existing snapshot unexpectedly requested confirmation: %+v", plan)
	}
}

func legacyMigrationNetworkHelper(marker string) string {
	return `CONF="$FREENET_CONFIG_FILE"
ISP="$(sed -n 's/^ISP_ID=//p' "$CONF" | tail -n 1 | tr -d "'\"")"
DNS="$(sed -n 's/^DNS_MODE=//p' "$CONF" | tail -n 1 | tr -d "'\"")"
if [ "$1" = plan ]; then
  if [ -f "` + marker + `" ]; then OWNER=ndnproxy; ROUTING=native; DNSOUT=no; else OWNER=xray; ROUTING=split; DNSOUT=yes; fi
  cat <<EOF
========== FreeNet Network Plan ==========
ISP_ID=$ISP
DNS_MODE=$DNS
EFFECTIVE_DNS_MODE=firmware
SUPPORTED=yes
REASON=профиль поддерживается
PROXY_DNS=off
PORT53_OWNER=$OWNER
XRAY_GID=11111
DNS_ROUTING_MODE=$ROUTING
DNS_OUT=$DNSOUT
VLESS_PROFILE=yes
EXPECTED_DELTA=вернуть native DNS
EXPECTED_NO_DELTA=no VPN credential rewrite
MUTATION=NONE
========== END ==========
EOF
  exit 0
fi
[ "$(cat "$FREENET_NATIVE_DNS_STATE_DIR/filter-engine.native" 2>/dev/null)" = public ] || exit 23
touch "` + marker + `"
echo '[FreeNet Network] RESULT=SUCCESS'
exit 0`
}

func TestNetworkApplyRequiresLegacyEngineChoiceBeforeHelperMutation(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), "native-dns")
	marker := filepath.Join(t.TempDir(), "helper-applied")
	t.Setenv("FREENET_NATIVE_DNS_STATE_DIR", stateDir)
	t.Setenv("FREENET_NETWORK_HELPER", writeFakeNetworkHelper(t, legacyMigrationNetworkHelper(marker)))
	a := testNetworkApp(t, "ISP_ID=rostelecom\nDNS_MODE=xkeen\nSETUP_COMPLETE=yes\n")

	payload := `{"operation":"network","isp":"rostelecom","dns_mode":"firmware","confirm":true}`
	r := httptest.NewRequest(http.MethodPost, "http://192.168.50.1:1001/api/network-profile/apply", strings.NewReader(payload))
	r.Host = "192.168.50.1:1001"
	r.Header.Set("Origin", "http://192.168.50.1:1001")
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	a.handleNetworkProfileApply(w, r)
	if w.Code != http.StatusConflict {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("network helper mutated runtime without one-time confirmation: %v", err)
	}
	if _, err := os.Stat(nativeFilterEngineSnapshotPath()); !os.IsNotExist(err) {
		t.Fatalf("missing confirmation unexpectedly persisted baseline: %v", err)
	}
	var resp networkApplyResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.RollbackState != "NOT_APPLIED" || !resp.Plan.NativeFilterEngineConfirmRequired {
		t.Fatalf("confirmation gate was not exposed safely: %+v", resp)
	}
}

func TestNetworkApplyPersistsConfirmedLegacyEngineThenUsesExistingTransaction(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), "native-dns")
	marker := filepath.Join(t.TempDir(), "helper-applied")
	t.Setenv("FREENET_NATIVE_DNS_STATE_DIR", stateDir)
	t.Setenv("FREENET_NETWORK_HELPER", writeFakeNetworkHelper(t, legacyMigrationNetworkHelper(marker)))
	a := testNetworkApp(t, "ISP_ID=rostelecom\nDNS_MODE=xkeen\nSETUP_COMPLETE=yes\n")

	payload := `{"operation":"network","isp":"rostelecom","dns_mode":"firmware","native_filter_engine":"public","confirm":true}`
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
		t.Fatalf("existing network transaction did not run: %v", err)
	}
	data, err := os.ReadFile(nativeFilterEngineSnapshotPath())
	if err != nil || string(data) != "public\n" {
		t.Fatalf("confirmed baseline missing: %q err=%v", data, err)
	}
	isp, dns := readNetworkProfileConfig(a.cfg.ConfigPath)
	if isp != "rostelecom" || dns != "firmware" {
		t.Fatalf("accepted native profile not committed: %s/%s", isp, dns)
	}
}
