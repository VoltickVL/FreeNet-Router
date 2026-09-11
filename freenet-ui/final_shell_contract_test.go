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
	js := string(asset)

	for _, want := range []string{
		"const shellIconPaths = {",
		"--fn-brand-mark:url(",
		"#topXkeenLink{display:none!important}",
		"if (xkeen) xkeen.remove()",
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

	if strings.Contains(js, "MutationObserver") {
		t.Fatal("final shell must remain event-driven and must not use MutationObserver")
	}
	if strings.Contains(js, "quickActionsSection") {
		t.Fatal("final shell cycle must not rewrite accepted Overview/Best Server content")
	}
}
