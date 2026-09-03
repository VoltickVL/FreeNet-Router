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
