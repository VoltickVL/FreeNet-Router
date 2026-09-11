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
	coordinatorAsset, err := webFS.ReadFile("web/operation-coordinator.js")
	if err != nil {
		t.Fatal(err)
	}
	js := string(asset)
	html := string(index)
	coordinator := string(coordinatorAsset)

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
		".fn-shell-fact>.fn-top-fact-icon,.fn-version-icon",
		"const icon = fact.querySelector(':scope > .fn-top-fact-icon');",
		"if (icon) fact.append(icon, copy);",
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

	for _, want := range []string{
		"span.className = 'fn-top-fact-icon'",
		"if (!item.querySelector('.fn-top-fact-icon')) item.prepend(svgIcon(index === 0 ? 'provider' : 'dns'))",
	} {
		if !strings.Contains(coordinator, want) {
			t.Fatalf("canonical topbar icon owner missing %q", want)
		}
	}

	if strings.Contains(js, "icon.className = 'fn-shell-fact-icon'") {
		t.Fatal("accepted UX must not create a second Provider/DNS topbar icon")
	}
	if strings.Contains(js, "MutationObserver") {
		t.Fatal("final shell must remain event-driven and must not use MutationObserver")
	}
	if strings.Contains(js, "quickActionsSection") {
		t.Fatal("final shell cycle must not rewrite accepted Overview/Best Server content")
	}
}
