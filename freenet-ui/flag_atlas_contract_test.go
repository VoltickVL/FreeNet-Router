package main

import (
	"os"
	"strings"
	"testing"
)

func TestCanonicalFlagAtlasHasOneLocalSVGOwner(t *testing.T) {
	canonical, err := os.ReadFile("web/vpn-ux-fix.js")
	if err != nil {
		t.Fatal(err)
	}
	js := string(canonical)
	for _, want := range []string{
		"freenetCanonicalAllFlags",
		"data-owner = 'canonical-local-svg'",
		"window.FreeNetFlagBackground",
		"window.FreeNetFlagCodes",
		"data:image/svg+xml",
		"kr:'<rect",
		"hk:'<rect",
		"my:'<rect",
		"au:'<rect",
		"gb:'<rect",
		"kz:'<rect",
		"ar:'<rect",
		"br:'<rect",
		"ca:'<rect",
		"sg:'<rect",
		"il:'<rect",
		"mx:'<rect",
	} {
		if !strings.Contains(js, want) {
			t.Fatalf("canonical flag source missing %q", want)
		}
	}
	for _, forbidden := range []string{`src="http`, `href="http`, `url("http`, `fetch('http`, `fetch("http`} {
		if strings.Contains(js, forbidden) {
			t.Fatalf("canonical flag atlas must not load remote assets: found %q", forbidden)
		}
	}

	accepted, err := os.ReadFile("web/accepted-ux.js")
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"freenetCanonicalFlagAtlas", "data:image/svg+xml", ".flag-kr{", ".flag-kz{"} {
		if strings.Contains(string(accepted), forbidden) {
			t.Fatalf("accepted UX must not own country visuals: found %q", forbidden)
		}
	}

	operation, err := os.ReadFile("web/operation-coordinator.js")
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"flagSVG", "fn-clean-flag", ".flag-no:after{", ".flag-dk{"} {
		if strings.Contains(string(operation), forbidden) {
			t.Fatalf("Operation Coordinator must not own country visuals: found %q", forbidden)
		}
	}
}
