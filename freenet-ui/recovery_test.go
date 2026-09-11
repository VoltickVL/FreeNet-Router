package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func recoveryTestApp(t *testing.T) *app {
	t.Helper()
	dir := t.TempDir()
	return &app{cfg: config{
		ConfigPath:  filepath.Join(dir, "freenet.conf"),
		UpdateState: filepath.Join(dir, "update.state"),
		UpdateLock:  filepath.Join(dir, "update.lock"),
	}}
}

func recoveryRequest(method, target string, body io.Reader) *http.Request {
	r := httptest.NewRequest(method, target, body)
	r.Host = "freenet.example:8443"
	r.Header.Set("Origin", "https://freenet.example:8443")
	r.Header.Set("X-Forwarded-Host", "freenet.example:8443")
	r.Header.Set("X-Forwarded-Proto", "https")
	r.RemoteAddr = "127.0.0.1:50000"
	return r
}

func recoverySessionCookie(t *testing.T, a *app) *http.Cookie {
	t.Helper()
	r := recoveryRequest(http.MethodGet, "https://freenet.example:8443/recovery", nil)
	w := httptest.NewRecorder()
	if err := a.newSession(w, r); err != nil {
		t.Fatal(err)
	}
	res := w.Result()
	defer res.Body.Close()
	cookies := res.Cookies()
	if len(cookies) != 1 || cookies[0].Name != authCookieName {
		t.Fatalf("unexpected recovery session cookies: %#v", cookies)
	}
	return cookies[0]
}

func TestRecoveryPageIsIndependentFromMainAssets(t *testing.T) {
	a := recoveryTestApp(t)
	r := recoveryRequest(http.MethodGet, "https://freenet.example:8443/recovery", nil)
	w := httptest.NewRecorder()
	a.handleRecovery(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "Аварийное восстановление") || !strings.Contains(body, "v"+version) {
		t.Fatalf("recovery facts missing: %s", body)
	}
	if recoveryBodyContainsMainAssets(body) {
		t.Fatalf("recovery page depends on main frontend assets: %s", body)
	}
}

func TestRegisterGeoDataAPIRegistersRecoverySurface(t *testing.T) {
	a := recoveryTestApp(t)
	mux := http.NewServeMux()
	registerGeoDataAPI(mux, a)
	r := recoveryRequest(http.MethodGet, "https://freenet.example:8443/recovery", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "FreeNet Recovery") {
		t.Fatalf("registered recovery route status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestRecoveryLoginUsesCanonicalCredentialAndSession(t *testing.T) {
	a := recoveryTestApp(t)
	password := "correct horse battery staple"
	if err := a.createCredential(password); err != nil {
		t.Fatal(err)
	}
	form := url.Values{"password": {password}}.Encode()
	r := recoveryRequest(http.MethodPost, "https://freenet.example:8443/recovery/login", strings.NewReader(form))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	a.handleRecoveryLogin(w, r)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("login status=%d body=%s", w.Code, w.Body.String())
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != authCookieName || cookies[0].Value == "" {
		t.Fatalf("canonical auth cookie missing: %#v", cookies)
	}

	view := recoveryRequest(http.MethodGet, "https://freenet.example:8443/recovery", nil)
	view.AddCookie(cookies[0])
	viewW := httptest.NewRecorder()
	a.handleRecovery(viewW, view)
	if viewW.Code != http.StatusOK || !strings.Contains(viewW.Body.String(), "Состояние updater") {
		t.Fatalf("authenticated recovery unavailable: status=%d body=%s", viewW.Code, viewW.Body.String())
	}
}

func TestRecoveryUpdateRejectsUnauthenticatedRequest(t *testing.T) {
	a := recoveryTestApp(t)
	r := recoveryRequest(http.MethodPost, "https://freenet.example:8443/recovery/update", nil)
	w := httptest.NewRecorder()
	a.handleRecoveryUpdate(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d want 401", w.Code)
	}
}

func TestRecoveryUpdateUsesCanonicalPlanTarget(t *testing.T) {
	a := recoveryTestApp(t)
	dir := filepath.Dir(a.cfg.ConfigPath)
	marker := filepath.Join(dir, "applied-target")
	t.Setenv("RECOVERY_APPLY_MARKER", marker)
	script := filepath.Join(dir, "self_update.sh")
	content := `#!/bin/sh
set -eu
case "${1:-}" in
  plan)
    echo 'SUCCESS=yes'
    echo 'READY=yes'
    echo 'CURRENT_VERSION=v0.3.19'
    echo 'LATEST_VERSION=v9.9.9'
    echo 'TARGET_TAG=v9.9.9'
    echo 'UPDATE_AVAILABLE=yes'
    echo 'MANIFEST_VERIFIED=yes'
    ;;
  apply)
    printf '%s\n' "${2:-}" > "$RECOVERY_APPLY_MARKER"
    ;;
  *) exit 2 ;;
esac
`
	if err := os.WriteFile(script, []byte(content), 0700); err != nil {
		t.Fatal(err)
	}
	a.cfg.SelfUpdatePath = script

	cookie := recoverySessionCookie(t, a)
	r := recoveryRequest(http.MethodPost, "https://freenet.example:8443/recovery/update", nil)
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	a.handleRecoveryUpdate(w, r)
	if w.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		b, err := os.ReadFile(marker)
		if err == nil {
			if got := strings.TrimSpace(string(b)); got != "v9.9.9" {
				t.Fatalf("apply target=%q want canonical plan target", got)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("canonical updater apply was not started")
}

func TestRecoveryUpdateRejectsCrossOriginBeforeMutation(t *testing.T) {
	a := recoveryTestApp(t)
	dir := filepath.Dir(a.cfg.ConfigPath)
	marker := filepath.Join(dir, "should-not-exist")
	t.Setenv("RECOVERY_APPLY_MARKER", marker)
	script := filepath.Join(dir, "self_update.sh")
	content := `#!/bin/sh
if [ "${1:-}" = plan ]; then
  echo 'SUCCESS=yes'; echo 'READY=yes'; echo 'TARGET_TAG=v9.9.9'; echo 'UPDATE_AVAILABLE=yes'; echo 'MANIFEST_VERIFIED=yes'
else
  touch "$RECOVERY_APPLY_MARKER"
fi
`
	if err := os.WriteFile(script, []byte(content), 0700); err != nil {
		t.Fatal(err)
	}
	a.cfg.SelfUpdatePath = script
	cookie := recoverySessionCookie(t, a)

	r := recoveryRequest(http.MethodPost, "https://freenet.example:8443/recovery/update", nil)
	r.Header.Set("Origin", "https://evil.example")
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	a.handleRecoveryUpdate(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status=%d want 403", w.Code)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("cross-origin request reached updater mutation")
	}
}

func TestRecoveryEscapesPrimaryError(t *testing.T) {
	a := recoveryTestApp(t)
	if err := os.WriteFile(a.cfg.UpdateState, []byte("STATE=FAILED\nPRIMARY_ERROR=<script>alert(1)</script>\nROLLBACK_STATE=NOT_NEEDED\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cookie := recoverySessionCookie(t, a)
	r := recoveryRequest(http.MethodGet, "https://freenet.example:8443/recovery", nil)
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	a.handleRecovery(w, r)
	body := w.Body.String()
	if strings.Contains(body, "<script>alert(1)</script>") || !strings.Contains(body, "&lt;script&gt;alert(1)&lt;/script&gt;") {
		t.Fatalf("PRIMARY ERROR was not safely escaped: %s", body)
	}
}
