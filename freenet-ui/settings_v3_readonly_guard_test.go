package main

import (
	"os"
	"strings"
	"testing"
)

func TestSettingsV3SubscriptionCheckIsReadOnlyForActiveVPN(t *testing.T) {
	src, err := os.ReadFile("settings_v3.go")
	if err != nil {
		t.Fatalf("read settings_v3.go: %v", err)
	}
	text := string(src)
	start := strings.Index(text, "func (a *app) runV3SubscriptionResult(ctx context.Context)")
	if start < 0 {
		t.Fatal("runV3SubscriptionResult is missing")
	}
	end := strings.Index(text[start:], "\nfunc (a *app) runV3Subscription(ctx context.Context)")
	if end < 0 {
		t.Fatal("cannot isolate runV3SubscriptionResult")
	}
	body := text[start : start+end]
	if !strings.Contains(body, "a.refreshSubscriptionProfiles(ctx)") {
		t.Fatal("subscription check must use the safe catalog refresh path")
	}
	for _, forbidden := range []string{
		"settingsV3UpdaterPath()",
		"blanc_xkeen_update_outbounds",
		"runCommand(",
		"exec.CommandContext",
		"a.runAction(",
		"a.cfg.VPNPath",
		"a.cfg.XKeenPath",
		"-restart",
		"/api/network-profile/apply",
	} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("subscription check must not mutate active VPN or restart Xray; found %q", forbidden)
		}
	}
}

func TestSettingsV3ActionSubscriptionBranchReturnsBeforeMutatingActions(t *testing.T) {
	src, err := os.ReadFile("settings_v3.go")
	if err != nil {
		t.Fatalf("read settings_v3.go: %v", err)
	}
	text := string(src)
	start := strings.Index(text, "case \"subscription_check\":")
	if start < 0 {
		t.Fatal("subscription_check branch is missing")
	}
	end := strings.Index(text[start:], "\n\tcase \"geodata_update\":")
	if end < 0 {
		t.Fatal("cannot isolate subscription_check branch")
	}
	branch := text[start : start+end]
	if !strings.Contains(branch, "return") {
		t.Fatal("subscription_check must return before mutating maintenance actions")
	}
	for _, forbidden := range []string{"runV3GeoData", "runV3Backup", "restoreV3Backup", "runV3FreeNetCheck", "runAction", "network-profile/apply"} {
		if strings.Contains(branch, forbidden) {
			t.Fatalf("subscription_check branch leaked into a mutating action: %q", forbidden)
		}
	}
}
