package main

import (
	"strings"
	"testing"
)

func TestAcceptedUXKeepsAuthCredentialsOnProxiedBrowserRequests(t *testing.T) {
	b, err := webFS.ReadFile("web/accepted-ux.js")
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{
		`path.startsWith('/api/auth/')`,
		`credentials: 'include'`,
		`body.remember = !!qs('#authRemember')?.checked`,
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("proxied auth browser contract missing %q", want)
		}
	}
	if strings.Contains(s, "MutationObserver") {
		t.Fatal("auth compatibility fix must remain event-driven")
	}
}
