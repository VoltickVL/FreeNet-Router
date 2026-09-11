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
		"canonicalExtraFlagCodes",
		"'kz','pe','my','au','ng'",
		".top-title{display:none!important}",
		".flag-co{background:linear-gradient(to bottom,#fcd116 0 50%",
		".flag-ae{background:linear-gradient(to right,#ff0000 0 25%",
		".flag-kr{background:radial-gradient",
		".flag-kz",
		".flag-pe",
		".flag-my",
		".flag-au",
		".flag-ng",
	} {
		if !strings.Contains(js, want) {
			t.Fatalf("selector hygiene contract missing %q", want)
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
