package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRememberedSessionSurvivesAppRestartWithoutPersistingRawToken(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "freenet.conf")
	if err := os.WriteFile(cfgPath, []byte("UI_PORT=1001\n"), 0600); err != nil {
		t.Fatal(err)
	}
	newApp := func() *app {
		return &app{cfg: config{ConfigPath: cfgPath}, sem: make(chan struct{}, 1)}
	}
	a1 := newApp()
	t.Cleanup(func() { authStateByApp.Delete(a1) })

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
	if err := a1.newSessionWithRemember(w, r, true); err != nil {
		t.Fatal(err)
	}
	cookie := w.Result().Cookies()[0]
	if cookie.MaxAge != int(authRememberSessionTTL.Seconds()) || cookie.Expires.IsZero() {
		t.Fatalf("remembered cookie lifetime mismatch: MaxAge=%d Expires=%v", cookie.MaxAge, cookie.Expires)
	}
	b, err := os.ReadFile(a1.authSessionPath())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), cookie.Value) {
		t.Fatal("raw remembered session token leaked to disk")
	}
	if !strings.Contains(string(b), sessionDigest(cookie.Value)) {
		t.Fatal("remembered session digest missing from persistent store")
	}

	authStateByApp.Delete(a1)
	a2 := newApp()
	t.Cleanup(func() { authStateByApp.Delete(a2) })
	probe := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	probe.AddCookie(cookie)
	if !a2.isAuthenticated(probe) {
		t.Fatal("remembered session did not survive app restart")
	}

	logout := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	logout.AddCookie(cookie)
	logoutW := httptest.NewRecorder()
	a2.handleAuthLogout(logoutW, logout)
	if logoutW.Code != http.StatusOK {
		t.Fatalf("logout code=%d", logoutW.Code)
	}
	authStateByApp.Delete(a2)
	a3 := newApp()
	t.Cleanup(func() { authStateByApp.Delete(a3) })
	probe = httptest.NewRequest(http.MethodGet, "/api/status", nil)
	probe.AddCookie(cookie)
	if a3.isAuthenticated(probe) {
		t.Fatal("logout did not revoke persisted remembered session")
	}
}

func TestNonRememberedSessionKeepsLegacyCookieLifetimeWithoutServerPersistence(t *testing.T) {
	a := testAuthApp(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
	if err := a.newSessionWithRemember(w, r, false); err != nil {
		t.Fatal(err)
	}
	cookie := w.Result().Cookies()[0]
	if cookie.MaxAge != int(authSessionTTL.Seconds()) || cookie.Expires.IsZero() {
		t.Fatalf("normal session cookie lifetime mismatch: MaxAge=%d Expires=%v", cookie.MaxAge, cookie.Expires)
	}
	if _, err := os.Stat(a.authSessionPath()); !os.IsNotExist(err) {
		t.Fatalf("non-remembered session created persistent store: %v", err)
	}
}
