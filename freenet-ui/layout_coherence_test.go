package main

import (
	"strings"
	"testing"
)

func TestControlCenterLayoutCoherenceContract(t *testing.T) {
	for _, want := range []string{
		`html{scrollbar-gutter:stable}`,
		`body #controlCenter .main .content{width:min(1180px,calc(100% - 48px))!important`,
		`#controlCenter .fn3-dns-layout{grid-template-columns:minmax(0,1fr)!important}`,
		`#controlCenter .fn3-dns-modes{grid-template-columns:repeat(2,minmax(0,1fr))!important}`,
		`#controlCenter .fn3-dns-resolvers{grid-template-columns:repeat(2,minmax(0,1fr))!important}`,
		`@keyframes freenetPageEnter`,
		`translateY(4px)`,
		`@media(prefers-reduced-motion:reduce)`,
	} {
		if !strings.Contains(controlCenterLayoutCoherenceStyle, want) {
			t.Fatalf("shared layout contract missing %q", want)
		}
	}
}

func TestControlCenterLayoutCoherenceKeepsMobileStack(t *testing.T) {
	for _, want := range []string{
		`@media(max-width:820px){body #controlCenter .main .content{width:calc(100% - 20px)!important}}`,
		`@media(max-width:720px){#controlCenter .fn3-dns-modes,#controlCenter .fn3-dns-resolvers{grid-template-columns:minmax(0,1fr)!important}}`,
	} {
		if !strings.Contains(controlCenterLayoutCoherenceStyle, want) {
			t.Fatalf("responsive layout contract missing %q", want)
		}
	}
}

func TestCanonicalIndexDeliversLayoutBeforeLegacyPaint(t *testing.T) {
	rawBytes, err := webFS.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	html, err := canonicalizeControlCenterIndex(string(rawBytes))
	if err != nil {
		t.Fatal(err)
	}

	bootAt := strings.Index(html, `id="freenetCanonicalBootStyle"`)
	layoutAt := strings.Index(html, `id="freenetLayoutCoherenceStyles"`)
	legacyAt := strings.Index(html, `<style>`)
	if bootAt < 0 || layoutAt < 0 || legacyAt < 0 || bootAt > layoutAt || layoutAt > legacyAt {
		t.Fatalf("layout must be delivered with first-paint head styles: boot=%d layout=%d legacy=%d", bootAt, layoutAt, legacyAt)
	}
}
