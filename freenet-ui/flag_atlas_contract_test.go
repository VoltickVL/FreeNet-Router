package main

import (
	"os"
	"strings"
	"testing"
)

func TestCanonicalFlagAtlasCoversComplexRuntimeFlags(t *testing.T) {
	data, err := os.ReadFile("web/accepted-ux.js")
	if err != nil {
		t.Fatal(err)
	}
	js := string(data)
	for _, want := range []string{
		"freenetCanonicalFlagAtlas",
		"data:image/svg+xml",
		"kr: '<rect",
		"hk: '<rect",
		"my: '<rect",
		"au: '<rect",
		"gb: '<rect",
		"kz: '<rect",
		"ar: '<rect",
		"br: '<rect",
		"ca: '<rect",
		"sg: '<rect",
		"il: '<rect",
		"mx: '<rect",
		".flag-icon.fn-clean-flag.flag-${code}>svg{display:none!important}",
	} {
		if !strings.Contains(js, want) {
			t.Fatalf("accepted flag atlas missing %q", want)
		}
	}
	atlasStart := strings.Index(js, "// Canonical local SVG atlas")
	if atlasStart < 0 {
		t.Fatal("canonical flag atlas block not found")
	}
	atlas := js[atlasStart:]
	for _, forbidden := range []string{`src="http`, `href="http`, `url("http`, `fetch('http`, `fetch("http`} {
		if strings.Contains(atlas, forbidden) {
			t.Fatalf("canonical flag atlas must not load remote assets: found %q", forbidden)
		}
	}
}
