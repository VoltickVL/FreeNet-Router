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

func testAuthApp(t *testing.T) *app {
	t.Helper()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "freenet.conf")
	if err := os.WriteFile(cfgPath, []byte("UI_PORT=1001\n"), 0600); err != nil {
		t.Fatal(err)
	}
	a := &app{cfg: config{ConfigPath: cfgPath}, sem: make(chan struct{}, 1)}
	t.Cleanup(func() { authStateByApp.Delete(a) })
	return a
}

func TestPBKDF2SHA256KnownVector(t *testing.T) {
	got := pbkdf2SHA256([]byte("password"), []byte("salt"), 1, 32)
	const want = "120fb6cffcf8b32c43e7225256c4f837a86548c92ccc35480805987cb70be17b"
	if hexString(got) != want {
		t.Fatalf("PBKDF2 mismatch: %s", hexString(got))
	}
}

func hexString(b []byte) string {
	const digits = "0123456789abcdef"
	out := make([]byte, len(b)*2)
	for i, v := range b {
		out[i*2] = digits[v>>4]
		out[i*2+1] = digits[v&0x0f]
	}
	return string(out)
}

func TestCredentialIsSaltedHashedAnd0600(t *testing.T) {
	a := testAuthApp(t)
	const password = "correct horse battery staple"
	if err := a.createCredential(password); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(a.authPath())
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("credential mode = %o", info.Mode().Perm())
	}
	b, err := os.ReadFile(a.authPath())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), password) {
		t.Fatal("plaintext password leaked to credential file")
	}
	var rec credentialRecord
	if err := json.Unmarshal(b, &rec); err != nil {
		t.Fatal(err)
	}
	if rec.Algorithm != authAlgorithm || rec.Iterations != authIterations || rec.Salt == "" || rec.Hash == "" {
		t.Fatalf("unexpected credential record: %+v", rec)
	}
	if !a.verifyPassword(password) {
		t.Fatal("correct password rejected")
	}
	if a.verifyPassword("wrong password") {
		t.Fatal("wrong password accepted")
	}
	if err := a.createCredential("another strong password"); err == nil {
		t.Fatal("credential setup must be one-time")
	}
}

func TestSessionCookieSecurityAttributes(t *testing.T) {
	a := testAuthApp(t)

	r := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
	w := httptest.NewRecorder()
	if err := a.newSession(w, r); err != nil {
		t.Fatal(err)
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies = %d", len(cookies))
	}
	c := cookies[0]
	if c.Name != authCookieName || !c.HttpOnly || c.SameSite != http.SameSiteStrictMode || c.Secure {
		t.Fatalf("unexpected LAN cookie: %+v", c)
	}

	r = httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
	r.Header.Set("X-Forwarded-Proto", "https")
	w = httptest.NewRecorder()
	if err := a.newSession(w, r); err != nil {
		t.Fatal(err)
	}
	c = w.Result().Cookies()[0]
	if !c.Secure {
		t.Fatal("HTTPS/proxy HTTPS session cookie must be Secure")
	}
}

func TestRequireAuthRejectsAnonymousAndAcceptsSession(t *testing.T) {
	a := testAuthApp(t)
	if err := a.createCredential("correct horse battery staple"); err != nil {
		t.Fatal(err)
	}

	called := false
	h := a.requireAuth(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	})

	r := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	w := httptest.NewRecorder()
	h(w, r)
	if w.Code != http.StatusUnauthorized || called {
		t.Fatalf("anonymous request: code=%d called=%v", w.Code, called)
	}

	loginReq := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
	loginW := httptest.NewRecorder()
	if err := a.newSession(loginW, loginReq); err != nil {
		t.Fatal(err)
	}
	cookie := loginW.Result().Cookies()[0]

	called = false
	r = httptest.NewRequest(http.MethodGet, "/api/status", nil)
	r.AddCookie(cookie)
	w = httptest.NewRecorder()
	h(w, r)
	if w.Code != http.StatusNoContent || !called {
		t.Fatalf("authenticated request: code=%d called=%v", w.Code, called)
	}
}

func TestAuthSetupLoginLogoutAllFlow(t *testing.T) {
	a := testAuthApp(t)
	password := "correct horse battery staple"

	setupReq := httptest.NewRequest(http.MethodPost, "/api/auth/setup", strings.NewReader(`{"password":"`+password+`"}`))
	setupReq.Header.Set("Content-Type", "application/json")
	setupW := httptest.NewRecorder()
	a.handleAuthSetup(setupW, setupReq)
	if setupW.Code != http.StatusCreated {
		t.Fatalf("setup code=%d body=%s", setupW.Code, setupW.Body.String())
	}
	setupCookie := setupW.Result().Cookies()[0]

	statusReq := httptest.NewRequest(http.MethodGet, "/api/auth/status", nil)
	statusReq.AddCookie(setupCookie)
	statusW := httptest.NewRecorder()
	a.handleAuthStatus(statusW, statusReq)
	if !strings.Contains(statusW.Body.String(), `"configured":true`) || !strings.Contains(statusW.Body.String(), `"authenticated":true`) {
		t.Fatalf("unexpected auth status: %s", statusW.Body.String())
	}

	wrongReq := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"password":"wrong password"}`))
	wrongReq.Header.Set("Content-Type", "application/json")
	wrongW := httptest.NewRecorder()
	a.handleAuthLogin(wrongW, wrongReq)
	if wrongW.Code != http.StatusUnauthorized {
		t.Fatalf("wrong login code=%d", wrongW.Code)
	}
	if strings.Contains(wrongW.Body.String(), "hash") || strings.Contains(wrongW.Body.String(), "salt") {
		t.Fatal("credential details leaked on failed login")
	}

	loginReq := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"password":"`+password+`"}`))
	loginReq.Header.Set("Content-Type", "application/json")
	loginW := httptest.NewRecorder()
	a.handleAuthLogin(loginW, loginReq)
	if loginW.Code != http.StatusOK {
		t.Fatalf("login code=%d body=%s", loginW.Code, loginW.Body.String())
	}
	loginCookie := loginW.Result().Cookies()[0]

	logoutAllReq := httptest.NewRequest(http.MethodPost, "/api/auth/logout-all", nil)
	logoutAllReq.AddCookie(loginCookie)
	logoutAllW := httptest.NewRecorder()
	a.handleAuthLogoutAll(logoutAllW, logoutAllReq)
	if logoutAllW.Code != http.StatusOK {
		t.Fatalf("logout-all code=%d", logoutAllW.Code)
	}

	probe := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	probe.AddCookie(setupCookie)
	if a.isAuthenticated(probe) {
		t.Fatal("logout-all did not invalidate prior session")
	}
	probe = httptest.NewRequest(http.MethodGet, "/api/status", nil)
	probe.AddCookie(loginCookie)
	if a.isAuthenticated(probe) {
		t.Fatal("logout-all did not invalidate current session")
	}
}
