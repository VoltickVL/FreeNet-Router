package main

import (
	"strings"
	"testing"
)

func TestProfileLabelHygieneIsCanonicalPresentationOnly(t *testing.T) {
	js := string(profileLabelHygieneAsset)
	for _, required := range []string{
		`#fn3Profile`,
		`#fn3ProfileSmall`,
		`0x1F1E6`,
		`0x1F1FF`,
		`MutationObserver`,
		`__freenetProfileLabelHygieneLoaded`,
	} {
		if !strings.Contains(js, required) {
			t.Fatalf("profile label hygiene missing %q", required)
		}
	}
	for _, forbidden := range []string{
		`fetch(`,
		`XMLHttpRequest`,
		`/api/network-profile/apply`,
		`/api/routing/apply`,
		`location.reload`,
	} {
		if strings.Contains(js, forbidden) {
			t.Fatalf("profile label hygiene must remain presentation-only: found %q", forbidden)
		}
	}
}

func TestCanonicalIndexEmbedsProfileLabelHygieneBeforeBootRelease(t *testing.T) {
	rawBytes, err := webFS.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	html, err := canonicalizeControlCenterIndex(string(rawBytes))
	if err != nil {
		t.Fatal(err)
	}
	hygieneAt := strings.Index(html, `id="freenetProfileLabelHygiene"`)
	releaseAt := strings.Index(html, `id="freenetCanonicalBootRelease"`)
	if hygieneAt < 0 {
		t.Fatal("canonical shell does not embed Settings profile label hygiene")
	}
	if releaseAt < 0 || hygieneAt > releaseAt {
		t.Fatalf("profile label hygiene must execute before boot release: hygiene=%d release=%d", hygieneAt, releaseAt)
	}
}
