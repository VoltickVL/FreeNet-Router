package main

import (
	"os"
	"strings"
	"testing"
)

func TestCanonicalRuntimeFlagAtlasCoversActualExtraCatalog(t *testing.T) {
	data, err := os.ReadFile("web/vpn-ux-fix.js")
	if err != nil {
		t.Fatal(err)
	}
	js := string(data)
	for _, want := range []string{
		"freenetCanonicalAllFlags",
		"window.FreeNetFlags",
		"apply(node, code)",
		"preserveAspectRatio=\"none\"",
		"background-image:${uri}!important",
		"dataset.flagSource",
		"::before",
		"::after",
	} {
		if !strings.Contains(js, want) {
			t.Fatalf("canonical runtime flag renderer missing %q", want)
		}
	}
	for _, code := range []string{
		"nl", "ae", "gr", "at", "bg", "co", "be", "ro", "fr", "cz", "es",
		"de", "dk", "ar", "fi", "hk", "hr", "hu", "ie", "tr", "za",
		"jp", "kr", "kz", "pe", "lt", "it", "my", "us", "no",
		"pl", "pt", "mx", "br", "se", "sg", "sk", "au", "il", "ua",
		"gb", "ca", "ch", "ng", "si", "rs", "is", "lu",
	} {
		if !strings.Contains(js, code+":") {
			t.Fatalf("canonical runtime flag atlas missing country code %q", code)
		}
	}
	atlasStart := strings.Index(js, "// Canonical deterministic local flag renderer")
	if atlasStart < 0 {
		t.Fatal("canonical runtime flag block not found")
	}
	atlas := js[atlasStart:]
	for _, forbidden := range []string{"fetch(", "url(http://", "url(https://", "sheet.cssRules"} {
		if strings.Contains(atlas, forbidden) {
			t.Fatalf("canonical runtime flag renderer must stay local/static: found %q", forbidden)
		}
	}
}
