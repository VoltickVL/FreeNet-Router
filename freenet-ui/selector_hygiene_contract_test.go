package main

import (
	"os"
	"strings"
	"testing"
)

func TestSelectorHygieneContract(t *testing.T) {
	ux, err := webFS.ReadFile("web/accepted-ux.js")
	if err != nil {
		t.Fatal(err)
	}
	js := string(ux)
	for _, want := range []string{
		".top-title{display:none!important}",
	} {
		if !strings.Contains(js, want) {
			t.Fatalf("selector hygiene contract missing %q", want)
		}
	}
	for _, forbidden := range []string{"freenetCanonicalFlagAtlas", "canonicalExtraFlagCodes", ".flag-co{", ".flag-ae{", ".flag-kr{"} {
		if strings.Contains(js, forbidden) {
			t.Fatalf("selector hygiene must not own flag pixels: %q", forbidden)
		}
	}
	flags, err := webFS.ReadFile("web/vpn-ux-fix.js")
	if err != nil {
		t.Fatal(err)
	}
	canonical := string(flags)
	for _, want := range []string{"window.FreeNetFlags", "co:", "ae:", "kr:", "kz:", "pe:", "my:", "au:", "ng:"} {
		if !strings.Contains(canonical, want) {
			t.Fatalf("canonical flag owner missing %q", want)
		}
	}
}

func TestUpdaterExcludesWhitelistFamilies(t *testing.T) {
	body, err := os.ReadFile("../scripts/blanc_xkeen_update_outbounds.sh")
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	if strings.Count(text, "grep -vi 'Whitelist'") < 2 {
		t.Fatal("updater must exclude Whitelist profiles in discovery and matching paths")
	}
	if !strings.Contains(text, "grep -vi 'Expired'") {
		t.Fatal("updater must keep Expired exclusion")
	}
}
