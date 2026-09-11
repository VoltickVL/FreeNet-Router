package main

import (
	"strings"
	"testing"
)

func TestFinalShellTopbarSidebarAndBrandContract(t *testing.T) {
	asset, err := webFS.ReadFile("web/accepted-ux.js")
	if err != nil {
		t.Fatal(err)
	}
	index, err := webFS.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	js := string(asset)
	html := string(index)

	for _, want := range []string{
		"const shellIconPaths = {",
		"--fn-brand-mark:url(",
		"#topXkeenLink{display:none!important}",
		"xkeen.hidden = true",
		"xkeen.setAttribute('aria-hidden', 'true')",
		"overview-approved-fact.fn-shell-fact,#topFreenetUpdate",
		"summary.appendChild(update)",
		"#topFreenetUpdate.update-available",
		"#topFreenetUpdate .fn-version-copy strong",
		".sidebar>.brand::before",
		".nav-btn.active",
		"icon.innerHTML = shellSVG(button.dataset.page || 'overview')",
	} {
		if !strings.Contains(js, want) {
			t.Fatalf("final shell contract missing %q", want)
		}
	}

	for _, want := range []string{
		`id="topXkeenLink"`,
		`el('topXkeenLink').href=xkeen`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("hidden XKeen compatibility anchor missing %q", want)
		}
	}

	if strings.Contains(js, "MutationObserver") {
		t.Fatal("final shell must remain event-driven and must not use MutationObserver")
	}
	if strings.Contains(js, "quickActionsSection") {
		t.Fatal("final shell cycle must not rewrite accepted Overview/Best Server content")
	}
}
