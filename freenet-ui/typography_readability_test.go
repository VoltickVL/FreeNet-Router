package main

import (
	"strings"
	"testing"
)

func TestTypographyReadabilityLayer(t *testing.T) {
	assetData, err := webFS.ReadFile("web/self-update.js")
	if err != nil {
		t.Fatal(err)
	}
	asset := string(assetData)
	for _, required := range []string{
		"mountTypographyReadability",
		"--fn-text-body:14px",
		".page-head p{font-size:14px",
		".hint{font-size:12.5px",
		".notice{font-size:12.5px",
		".status-pill span{font-size:11px",
		".profile-option-main{font-size:12.5px",
		".nav-btn{font-size:14px",
		"@media(max-width:600px)",
	} {
		if !strings.Contains(asset, required) {
			t.Fatalf("typography readability contract missing %q", required)
		}
	}

	if strings.Contains(asset, ".hint{font-size:9px") || strings.Contains(asset, ".notice{font-size:10px") {
		t.Fatal("semantic helper text must not regress to micro typography")
	}
}
