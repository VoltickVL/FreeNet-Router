package main

import (
	"strings"
	"testing"
)

func TestAuthVisibilityGuardKeepsProtectedUIHidden(t *testing.T) {
	index, err := webFS.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	ux, err := webFS.ReadFile("web/accepted-ux.js")
	if err != nil {
		t.Fatal(err)
	}

	html := string(index)
	js := string(ux)
	for _, want := range []string{
		`<div id="controlCenter" class="app" hidden>`,
		`el('authSection').hidden=authAuthenticated`,
		`el('controlCenter').hidden=!authAuthenticated`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("canonical auth visibility contract missing %q", want)
		}
	}

	const guard = `#authSection[hidden],#controlCenter[hidden],#fnSubscriptionHistoryToggle[hidden]{display:none!important}`
	if !strings.Contains(js, guard) {
		t.Fatalf("authoritative auth visibility guard missing %q", guard)
	}
	if !strings.Contains(js, `visibilityGuard.textContent = visibilityRule`) {
		t.Fatal("visibility guard must be installed synchronously when accepted UX loads")
	}
}
