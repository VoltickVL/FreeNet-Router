package main

import (
	"os"
	"strings"
	"testing"
)

func TestUIPolishContract(t *testing.T) {
	content, err := os.ReadFile("web/vpn-selector-reconcile.js")
	if err != nil {
		t.Fatalf("read reconcile asset: %v", err)
	}
	js := string(content)

	required := []string{
		"settingsGearSVG",
		"normalizeCurrentVPNFlags",
		"flagPrefix",
		"frnRoutingApplyPanel",
		"/api/routing/config",
		"/api/routing/validate",
		"/api/routing/apply",
		"ROLLBACK FAILED",
		"STOP",
	}
	for _, token := range required {
		if !strings.Contains(js, token) {
			t.Fatalf("vpn selector reconcile asset must contain %q", token)
		}
	}

	forbidden := []string{
		"subscription_url",
		"vless://",
		"shortId",
		"privateKey",
	}
	for _, token := range forbidden {
		if strings.Contains(js, token) {
			t.Fatalf("vpn selector reconcile asset must not expose secret token %q", token)
		}
	}
}
