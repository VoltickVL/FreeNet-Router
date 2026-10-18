package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestRecoveryRememberChoicePersistsOnlyWhenSelected(t *testing.T) {
	if authRememberSessionTTL != 30*24*time.Hour {
		t.Fatal("canonical login copy and session duration must agree")
	}
	for _, remember := range []bool{false, true} {
		name := "temporary"
		if remember {
			name = "remembered"
		}
		t.Run(name, func(t *testing.T) {
			a := recoveryTestApp(t)
			t.Cleanup(func() { authStateByApp.Delete(a) })
			const password = "fixture password for recovery only"
			if err := a.createCredential(password); err != nil {
				t.Fatal(err)
			}
			view := httptest.NewRecorder()
			a.handleRecovery(view, recoveryRequest(http.MethodGet, "https://freenet.example:8443/recovery", nil))
			if !strings.Contains(view.Body.String(), `name="remember" type="checkbox" value="yes" checked`) {
				t.Fatal("Recovery must show the 30-day choice without main UI assets")
			}
			form := url.Values{"password": {password}}
			if remember {
				form.Set("remember", "yes")
			}
			r := recoveryRequest(http.MethodPost, "https://freenet.example:8443/recovery/login", strings.NewReader(form.Encode()))
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			w := httptest.NewRecorder()
			a.handleRecoveryLogin(w, r)
			if w.Code != http.StatusSeeOther {
				t.Fatalf("login status=%d", w.Code)
			}
			cookies := w.Result().Cookies()
			if len(cookies) != 1 {
				t.Fatal("expected one canonical cookie")
			}
			cookie := cookies[0]
			ttl := authSessionTTL
			if remember {
				ttl = authRememberSessionTTL
			}
			if cookie.MaxAge != int(ttl.Seconds()) || !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteStrictMode || cookie.Domain != "" {
				t.Fatal("canonical cookie lifetime or security attributes changed")
			}
			a2 := &app{cfg: a.cfg}
			t.Cleanup(func() { authStateByApp.Delete(a2) })
			probe := recoveryRequest(http.MethodGet, "https://freenet.example:8443/api/status", nil)
			probe.AddCookie(cookie)
			if a2.isAuthenticated(probe) != remember {
				t.Fatal("restart persistence must follow the explicit remember choice")
			}
			if remember {
				logout := recoveryRequest(http.MethodPost, "https://freenet.example:8443/api/auth/logout-all", nil)
				logout.AddCookie(cookie)
				out := httptest.NewRecorder()
				a2.handleAuthLogoutAll(out, logout)
				if out.Code != http.StatusOK {
					t.Fatalf("logout all status=%d", out.Code)
				}
				a3 := &app{cfg: a.cfg}
				t.Cleanup(func() { authStateByApp.Delete(a3) })
				if a3.isAuthenticated(probe) {
					t.Fatal("logout-all must revoke remembered sessions across restart")
				}
			}
		})
	}
}

func TestRememberedSessionExpiryAndTamperRemainFailClosed(t *testing.T) {
	a := testAuthApp(t)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
	if err := a.newSessionWithRemember(w, r, true); err != nil {
		t.Fatal(err)
	}
	cookie := w.Result().Cookies()[0]
	probe := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	probe.AddCookie(&http.Cookie{Name: authCookieName, Value: strings.Repeat("0", 64)})
	if a.isAuthenticated(probe) {
		t.Fatal("unknown token was accepted")
	}
	state := a.authState()
	state.mu.Lock()
	err := a.savePersistentSessionsLocked([]persistentSessionRecord{{Hash: sessionDigest(cookie.Value), ExpiresAt: time.Now().Add(-time.Second)}})
	state.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	authStateByApp.Delete(a)
	probe = httptest.NewRequest(http.MethodGet, "/api/status", nil)
	probe.AddCookie(cookie)
	if a.isAuthenticated(probe) {
		t.Fatal("expired persistent session was accepted after restart")
	}
}
