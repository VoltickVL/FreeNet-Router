package main

import (
	"os"
	"strings"
	"testing"
)

func TestUnifiedDesignSystemCanonicalFirstPaintAndSingleStyleOwner(t *testing.T) {
	css, err := os.ReadFile("web/design-system.css")
	if err != nil {
		t.Fatal(err)
	}
	design := string(css)
	for _, required := range []string{
		"FreeNet UI design tokens: shared baseline for current and future Control Center tabs",
		".sidebar", ".nav-btn", ".vpn-option", ".best-v4-metrics",
		".fn-modal-root", ".fn3-", ".rv2-", ".cs-shell",
	} {
		if !strings.Contains(design, required) {
			t.Fatalf("unified CSS missing main component %q", required)
		}
	}
	for _, regression := range []string{
		"font-size:7px", "font-size:7.5px",
		"@media(min-width:821px) and (max-height:820px)",
		"#quickActionsSection{overflow:hidden!important}",
		".vpn-alternatives-panel{overflow:hidden!important}",
		".vpn-option{overflow:hidden!important}",
	} {
		if strings.Contains(design, regression) {
			t.Fatalf("old clipping/microscopic typography returned: %q", regression)
		}
	}

	for _, asset := range []string{
		"accepted-ux.js", "automation-async.js", "automation.js",
		"config-studio-parity.js", "config-studio-ux.js", "operation-coordinator.js",
		"routing-v2.js", "routing-apply-ui.js", "self-update.js",
		"settings-dns-ui.js", "settings-v3-core.js", "settings-v3.js",
		"topbar-settings-profile-cache.js", "vpn-picker-v2.js", "xray-core-manager.js",
	} {
		js, err := os.ReadFile("web/" + asset)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(js), "style.textContent =") {
			t.Fatalf("%s must use unified design CSS, not inject another stylesheet", asset)
		}
	}

	raw, err := os.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "<style>") {
		t.Fatal("legacy inline style must be moved to the unified design file")
	}
	page, err := canonicalizeControlCenterIndex(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(page, "<style id='freenetUnifiedDesignStyles'>") != 1 {
		t.Fatal("canonical page must load precisely one shared design stylesheet")
	}
	if strings.Contains(page, "<div class=\"side-bottom\">") {
		t.Fatal("legacy phantom sidebar footer must not be present")
	}
	cssAt := strings.Index(page, "<style id='freenetUnifiedDesignStyles'>")
	scriptAt := strings.Index(page, "<script")
	if cssAt < 0 || scriptAt < 0 || cssAt > scriptAt {
		t.Fatalf("shared stylesheet must be present before accepted UX scripts execute: cssAt=%d scriptAt=%d cssLen=%d", cssAt, scriptAt, len(page))
	}
}
