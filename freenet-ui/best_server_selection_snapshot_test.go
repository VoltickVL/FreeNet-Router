package main

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMeasuredSelectionApplyUsesExactSnapshotWithoutFreshRediscovery(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("FREENET_BEST_SELECTION_DIR", filepath.Join(dir, "selections"))
	t.Setenv("FREENET_AUTOMATION_STATE", filepath.Join(dir, "automation.state"))
	t.Setenv("FREENET_AUTOMATION_HISTORY", filepath.Join(dir, "automation.history"))

	subPath := filepath.Join(dir, "subscription.url")
	if err := os.WriteFile(subPath, []byte("https://provider.example.invalid/subscription-token\n"), 0600); err != nil {
		t.Fatal(err)
	}

	raw := strings.Split(strings.TrimSpace(testSubscriptionPlain), "\n")[0]
	profile, ok := parseSafeVLESSProfile(raw)
	if !ok || !validProfileID(profile.ID) {
		t.Fatalf("fixture profile is not selectable: %+v ok=%v", profile, ok)
	}
	a := testNetworkApp(t, "DNS_MODE=firmware\n")
	a.cfg.SubPath = subPath
	a.cfg.OutPath = filepath.Join(dir, "04_outbounds.json")
	a.cfg.FilterPath = filepath.Join(dir, "profile.filter")
	const currentEndpoint = "192.0.2.99:443"
	const currentFilter = "^PL Warsaw, Poland, Extra$"
	if err := os.WriteFile(a.cfg.OutPath, []byte(`{"outbounds":[{"tag":"vless-reality","settings":{"vnext":[{"address":"192.0.2.99","port":443}]}}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(a.cfg.FilterPath, []byte(currentFilter+"\n"), 0600); err != nil {
		t.Fatal(err)
	}

	measured := []bestServerQualityCandidate{{
		ID: profile.ID, Name: profile.Name, Endpoint: profileEndpoint(profile),
		Tested: true, Available: true, Eligible: true,
	}}
	token, err := a.storeBestServerSelectionSnapshot(currentEndpoint, currentFilter, []bestServerInternalCandidate{{Profile: profile, Raw: raw}}, measured)
	if err != nil {
		t.Fatal(err)
	}
	if !validBestServerSelectionToken(token) {
		t.Fatalf("invalid selection token: %q", token)
	}

	marker := filepath.Join(dir, "snapshot-used")
	provider := writeFakeNetworkHelper(t, `
grep -F 'TEST-ID-A@203.0.113.10:443' "$FREENET_PROVIDER_SUBSCRIPTION_CACHE" >/dev/null || {
  echo '[FreeNet Provider] ERROR: measured snapshot missing' >&2
  exit 7
}
grep -F 'TEST-ID-B' "$FREENET_PROVIDER_SUBSCRIPTION_CACHE" >/dev/null && {
  echo '[FreeNet Provider] ERROR: unmeasured profile leaked into snapshot' >&2
  exit 8
}
cat <<EOF
========== FreeNet Provider Plan ==========
PROFILE_ID=$2
PROFILE_NAME=Frankfurt, Germany, Extra
ENDPOINT=203.0.113.10:443
CURRENT_OUTBOUND=present
XRAY_RUNNING=yes
CANDIDATE_XRAY_VALID=yes
CANDIDATE_ROUTE_OK=yes
EXPECTED_DELTA=replace exactly one vless-reality outbound
EXPECTED_NO_DELTA=ISP/DNS/routing unchanged
MUTATION=NONE
========== END ==========
EOF
if [ "$1" = apply ]; then
  cat > "$FREENET_TEST_OUT_PATH" <<'EOF'
{"outbounds":[{"tag":"vless-reality","settings":{"vnext":[{"address":"203.0.113.10","port":443}]}}]}
EOF
  echo applied > "`+marker+`"
  echo '[FreeNet Provider] RESULT=SUCCESS'
fi
exit 0`)
	network := writeFakeNetworkHelper(t, "[ \"$1\" = plan ] || exit 9\ncat <<'EOF'\n"+supportedPlanOutput()+"\nEOF")
	t.Setenv("FREENET_PROVIDER_HELPER", provider)
	t.Setenv("FREENET_NETWORK_HELPER", network)
	t.Setenv("FREENET_TEST_OUT_PATH", a.cfg.OutPath)

	status, result := a.executeProviderProfileApply(networkApplyRequest{
		Operation: "provider", ProfileID: profile.ID, SelectionToken: token, Confirm: true,
	})
	if status != http.StatusOK || !result.Success || !result.Applied {
		t.Fatalf("snapshot apply failed: status=%d result=%+v", status, result)
	}
	if result.ProviderPlan == nil || result.ProviderPlan.ProfileID != profile.ID || result.ProviderPlan.Endpoint != "203.0.113.10:443" {
		t.Fatalf("apply did not report the exact measured candidate: %+v", result.ProviderPlan)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("provider apply did not use staged measured snapshot: %v", err)
	}
	if _, err := a.loadBestServerSelectionCandidate(token, profile.ID, currentEndpoint, currentFilter); err == nil {
		t.Fatal("successful apply did not consume one-time selection snapshot")
	}
}

func TestMeasuredSelectionSnapshotRejectsChangedCurrentVPNAndWrongToken(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("FREENET_BEST_SELECTION_DIR", filepath.Join(dir, "selections"))
	subPath := filepath.Join(dir, "subscription.url")
	if err := os.WriteFile(subPath, []byte("https://provider.example.invalid/subscription-token\n"), 0600); err != nil {
		t.Fatal(err)
	}
	raw := strings.Split(strings.TrimSpace(testSubscriptionPlain), "\n")[0]
	profile, ok := parseSafeVLESSProfile(raw)
	if !ok {
		t.Fatal("fixture profile rejected")
	}
	a := testNetworkApp(t, "DNS_MODE=firmware\n")
	a.cfg.SubPath = subPath
	measured := []bestServerQualityCandidate{{ID: profile.ID, Tested: true, Available: true, Eligible: true}}
	token, err := a.storeBestServerSelectionSnapshot("198.51.100.1:443", "^old$", []bestServerInternalCandidate{{Profile: profile, Raw: raw}}, measured)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.loadBestServerSelectionCandidate(token, profile.ID, "198.51.100.2:443", "^old$"); err == nil {
		t.Fatal("selection snapshot survived a current endpoint change")
	}
	if _, err := a.loadBestServerSelectionCandidate(strings.Repeat("a", 32), profile.ID, "198.51.100.1:443", "^old$"); err == nil {
		t.Fatal("wrong selection token was accepted")
	}
}
