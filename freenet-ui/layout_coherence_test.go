package main

import (
	"strings"
	"testing"
)

func TestControlCenterLayoutCoherenceContract(t *testing.T) {
	for _, want := range []string{
		`html{scrollbar-gutter:stable}`,
		`#controlCenter .main{--fn-center-max:1180px;--fn-center-gutter:24px}`,
		`body #controlCenter .main .content{width:min(var(--fn-center-max),calc(100% - 2 * var(--fn-center-gutter)))!important`,
		`body #controlCenter .main>.topbar.overview-approved{padding-right:max(var(--fn-center-gutter),calc((100% - var(--fn-center-max)) / 2))!important`,
		`#controlCenter .fn3-dns-layout{grid-template-columns:minmax(0,1fr)!important}`,
		`#controlCenter .fn3-dns-modes{grid-template-columns:repeat(2,minmax(0,1fr))!important}`,
		`#controlCenter .fn3-dns-resolvers{grid-template-columns:repeat(2,minmax(0,1fr))!important}`,
		`@keyframes freenetPageEnter`,
		`translateY(4px)`,
		`@media(prefers-reduced-motion:reduce)`,
	} {
		if !strings.Contains(freenetUnifiedDesignCSS, want) {
			t.Fatalf("shared layout contract missing %q", want)
		}
	}
}

func TestControlCenterLayoutCoherenceKeepsMobileStack(t *testing.T) {
	for _, want := range []string{
		`@media(max-width:820px){#controlCenter .main{--fn-center-gutter:10px}body #controlCenter .main .content{width:calc(100% - 20px)!important}}`,
		`@media(max-width:720px){#controlCenter .fn3-dns-modes,#controlCenter .fn3-dns-resolvers{grid-template-columns:minmax(0,1fr)!important}}`,
	} {
		if !strings.Contains(freenetUnifiedDesignCSS, want) {
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

	unifiedAt := strings.Index(html, `id='freenetUnifiedDesignStyles'`)
	bootAt := strings.Index(html, `html.freenet-canonical-boot`)
	layoutAt := strings.Index(html, `--fn-center-max:1180px`)
	scriptAt := strings.Index(html, `<script`)
	if unifiedAt < 0 || bootAt < unifiedAt || layoutAt < unifiedAt || layoutAt > scriptAt || bootAt > scriptAt {
		t.Fatalf("boot and layout must be in the single stylesheet before scripts: CSS=%d boot=%d layout=%d script=%d", unifiedAt, bootAt, layoutAt, scriptAt)
	}
	if strings.Contains(html, `id="freenetLayoutCoherenceStyles"`) || strings.Contains(html, `id="freenetCanonicalBootStyle"`) {
		t.Fatal("retired Go style owners must not be emitted")
	}
}
