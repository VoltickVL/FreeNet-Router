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

func writeExecutableTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0755); err != nil {
		t.Fatal(err)
	}
}

func setupFinalizeFakeHelpers(t *testing.T) (string, string, string) {
	t.Helper()
	tmp := t.TempDir()
	state := filepath.Join(tmp, "finalized")
	applyMarker := filepath.Join(tmp, "apply-called")
	finalize := filepath.Join(tmp, "finalize.sh")
	network := filepath.Join(tmp, "network.sh")

	writeExecutableTestFile(t, network, `#!/bin/sh
[ "${1:-}" = plan ] || exit 2
cat <<'EOF'
ISP_ID=rostelecom
DNS_MODE=firmware
EFFECTIVE_DNS_MODE=firmware
SUPPORTED=yes
REASON=accepted
PROXY_DNS=on
PORT53_OWNER=ndnproxy
XRAY_GID=11111
DNS_OUT=yes
VLESS_PROFILE=yes
EXPECTED_DELTA=NONE
EXPECTED_NO_DELTA=network unchanged
MUTATION=NONE
EOF
`)

	writeExecutableTestFile(t, finalize, `#!/bin/sh
STATE="$FREENET_FINALIZE_TEST_STATE"
MARKER="$FREENET_FINALIZE_APPLY_MARKER"
case "${1:-}" in
  plan)
    READY="${FREENET_FINALIZE_TEST_READY:-yes}"
    SETUP=no
    AUTO=off
    REASON='ready to finalize'
    if [ "$READY" != yes ]; then REASON='provider/network acceptance is incomplete'; fi
    if [ -f "$STATE" ]; then SETUP=yes; AUTO=on; fi
    cat <<EOF
READY=$READY
REASON=$REASON
SETUP_COMPLETE=$SETUP
SUBSCRIPTION_CONFIGURED=yes
PREFERRED_PROFILE_SET=yes
NETWORK_SUPPORTED=yes
XRAY_RUNNING=yes
XRAY_VALID=yes
DNS_OUT=yes
VLESS_PROFILE=yes
XKEEN_AUTOSTART=$AUTO
AUTO_ENDPOINT_UPDATE=no
AUTO_ENDPOINT_CRON=*/15 * * * *
EXPECTED_DELTA=set SETUP_COMPLETE=yes and enable autostart
EXPECTED_NO_DELTA=no credential rewrite
MUTATION=NONE
EOF
    [ "$READY" = yes ]
    ;;
  apply)
    : > "$MARKER"
    : > "$STATE"
    echo '[FreeNet Setup Finalize] RESULT=SUCCESS'
    ;;
  *) exit 2 ;;
esac
`)

	t.Setenv("FREENET_FINALIZE_HELPER", finalize)
	t.Setenv("FREENET_NETWORK_HELPER", network)
	t.Setenv("FREENET_FINALIZE_TEST_STATE", state)
	t.Setenv("FREENET_FINALIZE_APPLY_MARKER", applyMarker)
	return state, applyMarker, finalize
}

func TestParseSetupFinalizePlanUsesAllowlist(t *testing.T) {
	plan, err := parseSetupFinalizePlan(`READY=yes
REASON=ready
SETUP_COMPLETE=no
SUBSCRIPTION_CONFIGURED=yes
PREFERRED_PROFILE_SET=yes
NETWORK_SUPPORTED=yes
XRAY_RUNNING=yes
XRAY_VALID=yes
DNS_OUT=yes
VLESS_PROFILE=yes
XKEEN_AUTOSTART=off
AUTO_ENDPOINT_UPDATE=no
AUTO_ENDPOINT_CRON=*/15 * * * *
EXPECTED_DELTA=complete setup
EXPECTED_NO_DELTA=credentials unchanged
MUTATION=NONE
SECRET_URL=https://example.invalid/secret
UUID=should-not-surface
`)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Success || !plan.Ready || plan.SetupComplete || plan.XKeenAutostart != "off" {
		t.Fatalf("неожиданный финальный план: %+v", plan)
	}
	b, _ := json.Marshal(plan)
	if strings.Contains(string(b), "example.invalid") || strings.Contains(string(b), "should-not-surface") {
		t.Fatalf("неизвестные/секретные поля попали в API: %s", b)
	}
}

func TestSetupFinalizePlanIsAttachedToReadOnlyNetworkPlan(t *testing.T) {
	_, _, _ = setupFinalizeFakeHelpers(t)
	a := &app{cfg: config{Timeout: 5 * time.Second}, sem: make(chan struct{}, 1)}
	req := httptest.NewRequest(http.MethodGet, "/api/network-profile/plan?setup_finalize=1", nil)
	rr := httptest.NewRecorder()
	a.handleNetworkProfilePlan(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("ожидался 200, получен %d: %s", rr.Code, rr.Body.String())
	}
	var got networkPlanResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.SetupFinalizePlan == nil || !got.SetupFinalizePlan.Ready || got.SetupFinalizePlan.Mutation != "NONE" {
		t.Fatalf("финальный read-only план не приложен: %+v", got.SetupFinalizePlan)
	}
}

func TestSetupFinalizeApplyRequiresExplicitConfirmation(t *testing.T) {
	_, marker, _ := setupFinalizeFakeHelpers(t)
	a := &app{cfg: config{Timeout: 5 * time.Second}, sem: make(chan struct{}, 1)}
	req := httptest.NewRequest(http.MethodPost, "/api/network-profile/apply", strings.NewReader(`{"operation":"finalize","confirm":false}`))
	req.Body = newBody(`{"operation":"finalize","confirm":false}`)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	a.handleNetworkProfileApply(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("без подтверждения ожидался 400, получен %d", rr.Code)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("finalize apply не должен запускаться без confirm=true")
	}
}

func TestSetupFinalizeApplyRunsFreshPlanAndConfirmsAcceptance(t *testing.T) {
	state, marker, _ := setupFinalizeFakeHelpers(t)
	a := &app{cfg: config{Timeout: 5 * time.Second}, sem: make(chan struct{}, 1)}
	req := httptest.NewRequest(http.MethodPost, "/api/network-profile/apply", strings.NewReader(`{"operation":"finalize","confirm":true}`))
	req.Body = newBody(`{"operation":"finalize","confirm":true}`)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	a.handleNetworkProfileApply(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("ожидался успешный finalize, получен %d: %s", rr.Code, rr.Body.String())
	}
	var got networkApplyResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.Success || !got.Applied || got.Operation != "finalize" || got.RollbackState != "NOT_NEEDED" {
		t.Fatalf("неожиданный ответ finalize: %+v", got)
	}
	if got.SetupFinalizePlan == nil || !got.SetupFinalizePlan.SetupComplete || got.SetupFinalizePlan.XKeenAutostart != "on" {
		t.Fatalf("post-acceptance не подтверждён: %+v", got.SetupFinalizePlan)
	}
	if _, err := os.Stat(state); err != nil {
		t.Fatalf("finalize state не создан: %v", err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("apply helper не вызывался: %v", err)
	}
}

func TestSetupFinalizeApplyStopsWhenFreshPlanNotReady(t *testing.T) {
	_, marker, _ := setupFinalizeFakeHelpers(t)
	t.Setenv("FREENET_FINALIZE_TEST_READY", "no")
	a := &app{cfg: config{Timeout: 5 * time.Second}, sem: make(chan struct{}, 1)}
	req := httptest.NewRequest(http.MethodPost, "/api/network-profile/apply", strings.NewReader(`{"operation":"finalize","confirm":true}`))
	req.Body = newBody(`{"operation":"finalize","confirm":true}`)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	a.handleNetworkProfileApply(rr, req)
	if rr.Code != http.StatusConflict {
		t.Fatalf("для READY=no ожидался 409, получен %d: %s", rr.Code, rr.Body.String())
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("apply нельзя запускать после READY=no fresh plan")
	}
}

func newBody(s string) *stringReadCloser {
	return &stringReadCloser{Reader: strings.NewReader(strings.ReplaceAll(s, `\"`, `"`))}
}

type stringReadCloser struct {
	*strings.Reader
}

func (r *stringReadCloser) Close() error { return nil }
