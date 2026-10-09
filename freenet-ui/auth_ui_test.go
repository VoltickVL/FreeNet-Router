package main

import (
	"strings"
	"testing"
)

func TestAuthUIContract(t *testing.T) {
	b, err := webFS.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, needle := range []string{
		`id="authSection"`,
		`id="controlCenter"`,
		`id="authPassword"`,
		`id="authPasswordConfirm"`,
		`id="authSubmitBtn"`,
		`id="logoutBtn"`,
		`id="logoutAllBtn"`,
		`/api/auth/status`,
		`/api/auth/setup`,
		`/api/auth/login`,
		`/api/auth/logout`,
		`/api/auth/logout-all`,
		`loadAuthStatus();`,
	} {
		if !strings.Contains(s, needle) {
			t.Fatalf("auth UI contract missing %q", needle)
		}
	}
	if !strings.Contains(s, `id="controlCenter" class="app" hidden`) {
		t.Fatal("authenticated app shell must be hidden until login")
	}
}

func TestAuthFirstLoginShowsProgressWithoutSkippingServerGate(t *testing.T) {
	b, err := webFS.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, marker := range []string{
		`authPending=false`,
		`if(authPending)return`,
		`setAuthPending(true)`,
		`finally{`,
		`setAuthPending(false)`,
		`aria-live="polite"`,
		`button.disabled=authPending`,
		`button.setAttribute('aria-busy',String(authPending))`,
		`if(!j||j.configured!==true||j.authenticated!==true)`,
		`renderAuth(j)`,
	} {
		if !strings.Contains(s, marker) {
			t.Fatalf("slow-MIPS auth progress contract missing %q", marker)
		}
	}
	// Explicitly prevent returning to an extra GET /api/auth/status after
	// an already authenticated success response from setup/login.
	if strings.Contains(s, `el('authPasswordConfirm').value='';await loadAuthStatus()`) {
		t.Fatal("successful login must not wait for redundant auth/status GET")
	}
}
