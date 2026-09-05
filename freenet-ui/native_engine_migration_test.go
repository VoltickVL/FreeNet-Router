package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

func writeNativeDNSSnapshotForTest(t *testing.T, stateDir string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "02_dns.native"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	if err := os.WriteFile(filepath.Join(stateDir, "02_dns.native.sha256"), []byte(hex.EncodeToString(sum[:])+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeLegacyMigrationConfig(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeLegacyMigrationXray(t *testing.T, script string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "xray")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+script+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func setupLegacyNativeRecoveryFixture(t *testing.T, xrayScript string) (string, string, string, []byte) {
	t.Helper()
	stateDir := filepath.Join(t.TempDir(), "native-dns")
	backupRoot := filepath.Join(t.TempDir(), "backups")
	configDir := filepath.Join(t.TempDir(), "configs")
	assetDir := filepath.Join(t.TempDir(), "assets")
	if err := os.MkdirAll(assetDir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FREENET_NATIVE_DNS_STATE_DIR", stateDir)
	t.Setenv("FREENET_BACKUP_ROOT", backupRoot)
	t.Setenv("FREENET_CONFIG_DIR", configDir)
	t.Setenv("FREENET_XRAY_ASSET_DIR", assetDir)
	t.Setenv("FREENET_XRAY_BIN", writeLegacyMigrationXray(t, xrayScript))

	writeLegacyMigrationConfig(t, configDir, "03_inbounds.json", `{
  // current repaired Split
  "inbounds": [
    {"tag":"redir","port":5000,"protocol":"dokodemo-door"},
    {"tag":"dns","port":53,"protocol":"dokodemo-door"},
  ],
}`)
	writeLegacyMigrationConfig(t, configDir, "04_outbounds.json", `{
  "outbounds": [
    {"tag":"vless-reality","protocol":"vless"},
    {"tag":"direct","protocol":"freedom"},
    {"tag":"dns-out","protocol":"dns"},
  ],
}`)
	writeLegacyMigrationConfig(t, configDir, "05_routing.json", `{
  "routing": {"rules": [
    {"type":"field","inboundTag":["dns-vless"],"outboundTag":"vless-reality"},
    {"type":"field","inboundTag":["dns-direct"],"outboundTag":"direct"},
    {"type":"field","port":53,"outboundTag":"dns-out"},
    {"type":"field","port":"0-65535","outboundTag":"vless-reality"},
  ]},
}`)

	nativeDir := filepath.Join(backupRoot, "freenet-network-standard-20260901-120000")
	nativeDNS := []byte("// exact legacy native 02_dns; keep opaque\n{}\n")
	writeLegacyMigrationConfig(t, nativeDir, "02_dns.json", string(nativeDNS))
	writeLegacyMigrationConfig(t, nativeDir, "03_inbounds.json", `{
  "inbounds": [{"tag":"redir","port":5000,"protocol":"dokodemo-door"},],
}`)
	writeLegacyMigrationConfig(t, nativeDir, "04_outbounds.json", `{
  "outbounds": [
    {"tag":"vless-reality","protocol":"vless"},
    {"tag":"direct","protocol":"freedom"},
  ],
}`)
	writeLegacyMigrationConfig(t, nativeDir, "05_routing.json", `{
  "routing":{"rules":[
    {"type":"field","port":"0-65535","outboundTag":"vless-reality"},
  ],},
}`)
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(nativeDir, old, old); err != nil {
		t.Fatal(err)
	}

	// A newer backup from the failed/repaired Split must not be mistaken for native.
	splitDir := filepath.Join(backupRoot, "freenet-network-native-20260905-120000")
	writeLegacyMigrationConfig(t, splitDir, "02_dns.json", `{"dns":{"servers":["8.8.8.8"]}}`)
	writeLegacyMigrationConfig(t, splitDir, "03_inbounds.json", `{"inbounds":[{"tag":"dns","port":53,"protocol":"dokodemo-door"}]}`)
	writeLegacyMigrationConfig(t, splitDir, "04_outbounds.json", `{"outbounds":[{"tag":"dns-out","protocol":"dns"}]}`)
	writeLegacyMigrationConfig(t, splitDir, "05_routing.json", `{"routing":{"rules":[{"port":53,"outboundTag":"dns-out"}]}}`)
	return stateDir, backupRoot, configDir, nativeDNS
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
	writeNativeDNSSnapshotForTest(t, stateDir, []byte("// native\n{}\n"))
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

func TestLegacyNativeDNSRecoveryUsesNewestUnambiguousNativeBackup(t *testing.T) {
	stateDir, _, _, nativeDNS := setupLegacyNativeRecoveryFixture(t, "exit 0")
	plan := legacySplitPlanForTest()
	enrichNativeFilterEngineMigration(&plan)
	if !plan.NativeFilterEngineConfirmRequired {
		t.Fatal("expected engine confirmation before recovery")
	}
	if err := persistConfirmedLegacyNativeFilterEngine(plan, "public"); err != nil {
		t.Fatalf("legacy recovery failed: %v", err)
	}

	recovered, err := os.ReadFile(filepath.Join(stateDir, "02_dns.native"))
	if err != nil {
		t.Fatal(err)
	}
	if string(recovered) != string(nativeDNS) {
		t.Fatalf("native DNS was not restored byte-for-byte: %q", recovered)
	}
	valid, missing, err := legacyNativeDNSSnapshotStatus()
	if err != nil || !valid || missing {
		t.Fatalf("recovered native DNS snapshot invalid: valid=%v missing=%v err=%v", valid, missing, err)
	}
	engine, err := os.ReadFile(filepath.Join(stateDir, "filter-engine.native"))
	if err != nil || string(engine) != "public\n" {
		t.Fatalf("engine baseline not committed after DNS recovery: %q err=%v", engine, err)
	}
}

func TestLegacyNativeDNSRecoveryRunsAfterEngineWasAlreadyConfirmed(t *testing.T) {
	stateDir, _, _, nativeDNS := setupLegacyNativeRecoveryFixture(t, "exit 0")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "filter-engine.native"), []byte("public\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	plan := legacySplitPlanForTest()
	enrichNativeFilterEngineMigration(&plan)
	if plan.NativeFilterEngineConfirmRequired {
		t.Fatal("confirmed engine must not be requested again")
	}
	if err := persistConfirmedLegacyNativeFilterEngine(plan, ""); err != nil {
		t.Fatalf("post-confirmation recovery failed: %v", err)
	}
	recovered, err := os.ReadFile(filepath.Join(stateDir, "02_dns.native"))
	if err != nil || string(recovered) != string(nativeDNS) {
		t.Fatalf("post-confirmation recovery did not persist exact native DNS: %q err=%v", recovered, err)
	}
}

func TestLegacyNativeDNSRecoveryStopsWhenOnlySplitBackupsExist(t *testing.T) {
	stateDir, backupRoot, _, _ := setupLegacyNativeRecoveryFixture(t, "exit 0")
	if err := os.RemoveAll(filepath.Join(backupRoot, "freenet-network-standard-20260901-120000")); err != nil {
		t.Fatal(err)
	}
	plan := legacySplitPlanForTest()
	enrichNativeFilterEngineMigration(&plan)
	err := persistConfirmedLegacyNativeFilterEngine(plan, "public")
	if err == nil || !strings.Contains(err.Error(), "не найден однозначный native network backup") {
		t.Fatalf("expected fail-closed missing native backup, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(stateDir, "filter-engine.native")); !os.IsNotExist(statErr) {
		t.Fatalf("engine baseline persisted despite failed native DNS recovery: %v", statErr)
	}
}

func TestLegacyNativeDNSRecoveryStopsOnPartialSnapshot(t *testing.T) {
	stateDir, _, _, _ := setupLegacyNativeRecoveryFixture(t, "exit 0")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "filter-engine.native"), []byte("public\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "02_dns.native"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	plan := legacySplitPlanForTest()
	enrichNativeFilterEngineMigration(&plan)
	err := persistConfirmedLegacyNativeFilterEngine(plan, "")
	if err == nil || !strings.Contains(err.Error(), "snapshot неполон") {
		t.Fatalf("expected partial snapshot STOP, got %v", err)
	}
}

func TestLegacyNativeDNSRecoveryDoesNotPersistEngineWhenCandidateValidationFails(t *testing.T) {
	stateDir, _, _, _ := setupLegacyNativeRecoveryFixture(t, `case "$4" in
  *freenet-network-*) exit 0 ;;
  *) exit 1 ;;
esac`)
	plan := legacySplitPlanForTest()
	enrichNativeFilterEngineMigration(&plan)
	err := persistConfirmedLegacyNativeFilterEngine(plan, "public")
	if err == nil || !strings.Contains(err.Error(), "не прошёл Xray candidate validation") {
		t.Fatalf("expected candidate validation STOP, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(stateDir, "filter-engine.native")); !os.IsNotExist(statErr) {
		t.Fatalf("engine baseline persisted before candidate acceptance: %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(stateDir, "02_dns.native")); !os.IsNotExist(statErr) {
		t.Fatalf("native DNS baseline persisted before candidate acceptance: %v", statErr)
	}
}

func legacyMigrationNetworkHelper(marker string) string {
	return `CONF="$FREENET_CONFIG_FILE"
ISP="$(sed -n 's/^ISP_ID=//p' "$CONF" | tail -n 1 | tr -d "'\"")"
DNS="$(sed -n 's/^DNS_MODE=//p' "$CONF" | tail -n 1 | tr -d "'\"")"
if [ "$1" = plan ]; then
  if [ -f "` + marker + `" ]; then
    OWNER=ndnproxy; ROUTING=native; DNSOUT=no; OVERRIDE=off; ENGINE=public; INTERCEPT=on; ASSIGNMENTS=present; INBOUND=0
  else
    OWNER=xray; ROUTING=split; DNSOUT=yes; OVERRIDE=on; ENGINE=opkg; INTERCEPT=off; ASSIGNMENTS=none; INBOUND=1
  fi
  cat <<EOF
========== FreeNet Network Plan ==========
ISP_ID=$ISP
DNS_MODE=$DNS
EFFECTIVE_DNS_MODE=firmware
SUPPORTED=yes
REASON=профиль поддерживается
PROXY_DNS=off
NDM_DNS_OVERRIDE=$OVERRIDE
NDM_FILTER_ENGINE=$ENGINE
NDM_DNS_INTERCEPT=$INTERCEPT
NDM_DNS_ASSIGNMENTS=$ASSIGNMENTS
PORT53_OWNER=$OWNER
XRAY_DNS_INBOUND_COUNT=$INBOUND
XRAY_RUNNING=yes
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
	writeNativeDNSSnapshotForTest(t, stateDir, []byte("// native\n{}\n"))
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
