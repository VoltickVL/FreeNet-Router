package main

import (
	"os"
	"strings"
	"testing"
)

func TestCanonicalFlagAtlasHasSingleVisualOwner(t *testing.T) {
	canonicalBytes, err := os.ReadFile("web/vpn-ux-fix.js")
	if err != nil {
		t.Fatal(err)
	}
	canonical := string(canonicalBytes)
	for _, want := range []string{
		"freenetCanonicalAllFlags",
		"window.FreeNetFlags",
		"kr:", "hk:", "my:", "au:", "gb:", "kz:",
		"ar:", "br:", "ca:", "sg:", "il:", "mx:",
	} {
		if !strings.Contains(canonical, want) {
			t.Fatalf("canonical flag source missing %q", want)
		}
	}

	acceptedBytes, err := os.ReadFile("web/accepted-ux.js")
	if err != nil {
		t.Fatal(err)
	}
	accepted := string(acceptedBytes)
	for _, forbidden := range []string{"freenetCanonicalFlagAtlas", "fn-clean-flag", "// Canonical local SVG atlas"} {
		if strings.Contains(accepted, forbidden) {
			t.Fatalf("accepted UX still owns flag pixels through %q", forbidden)
		}
	}

	indexBytes, err := os.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	index := string(indexBytes)
	for _, forbidden := range []string{".flag-de{", ".flag-pl{", ".flag-us{", ".flag-kr{"} {
		if strings.Contains(index, forbidden) {
			t.Fatalf("legacy index flag renderer survived: %q", forbidden)
		}
	}

	for _, path := range []string{"web/operation-coordinator.js", "web/topbar-settings-profile-cache.js"} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		src := string(data)
		for _, forbidden := range []string{"freenetCanonicalFlagAtlas", "fn-clean-flag", "const flagSVG = code =>"} {
			if strings.Contains(src, forbidden) {
				t.Fatalf("%s still owns flag pixels through %q", path, forbidden)
			}
		}
	}
}
