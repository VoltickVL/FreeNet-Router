package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCanonicalizeControlCenterIndexRemovesLegacyFirstPaint(t *testing.T) {
	rawBytes, err := webFS.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	html, err := canonicalizeControlCenterIndex(string(rawBytes))
	if err != nil {
		t.Fatal(err)
	}

	navStart := strings.Index(html, `<nav class="nav" aria-label="Навигация Control Center">`)
	if navStart < 0 {
		t.Fatal("canonical first-paint navigation missing")
	}
	navEndRel := strings.Index(html[navStart:], `</nav>`)
	if navEndRel < 0 {
		t.Fatal("canonical first-paint navigation end missing")
	}
	nav := html[navStart : navStart+navEndRel+len(`</nav>`)]

	for _, want := range []string{"Обзор", "Настройки", "Маршрутизация", "Журнал", "Администрирование"} {
		if !strings.Contains(nav, ">"+want+"</span></button>") {
			t.Fatalf("canonical first-paint navigation missing %q", want)
		}
	}
	for _, want := range []string{`data-page="overview"`, `data-page="settings"`, `data-page="network"`, `data-page="journal"`, `data-page="admin"`} {
		if !strings.Contains(nav, want) {
			t.Fatalf("canonical first-paint route missing %q", want)
		}
	}
	for _, retired := range []string{`data-page="vpn"`, `data-page="automation"`, `data-page="subscription"`, `data-page="system"`, `data-page="access"`} {
		if strings.Contains(nav, retired) {
			t.Fatalf("retired first-paint navigation route leaked %q", retired)
		}
	}
	if got := strings.Count(nav, `<svg viewBox="0 0 24 24"`); got != 5 {
		t.Fatalf("canonical first-paint navigation must ship five final SVG icons, got %d", got)
	}
	if got := strings.Count(nav, `data-freenet-shell="1"`); got != 5 {
		t.Fatalf("canonical first-paint icons must be marked final before accepted UX mounts, got %d", got)
	}
	if strings.Contains(html, `<div class="side-bottom">`) {
		t.Fatal("legacy sidebar footer must not be present in first-paint HTML")
	}
	if !strings.Contains(html, `const pageLabels={overview:'Обзор',settings:'Настройки',network:'Маршрутизация',journal:'Журнал',admin:'Администрирование'};`) {
		t.Fatal("canonical page labels are not final during bootstrap")
	}
}

func TestCanonicalizeControlCenterIndexGatesLegacyPaintUntilAcceptedShellReady(t *testing.T) {
	rawBytes, err := webFS.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	html, err := canonicalizeControlCenterIndex(string(rawBytes))
	if err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{
		`<html lang="ru" class="freenet-canonical-boot">`,
		`id="freenetCanonicalBootStyle"`,
		`html.freenet-canonical-boot body>*{visibility:hidden!important}`,
		`radial-gradient(circle at 68% -15%,#17345c 0,#0d1d32 42%,#081523 72%,#07101b 100%)`,
		`id="freenetCanonicalBootRelease"`,
		`document.getElementById('freenetAcceptedUXStyles')`,
		`document.getElementById('freenetFinalShellPolishStyles')`,
		`document.querySelector('.sidebar>.brand .fn-brand-lockup-svg')`,
		`const canonicalRoutes = ['overview','settings','network','journal','admin']`,
		`document.querySelectorAll('.sidebar .nav>.nav-btn[data-page]')`,
		`.nav-icon[data-freenet-shell="1"] svg`,

		`root.classList.remove('freenet-canonical-boot')`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("canonical boot gate missing %q", want)
		}
	}

	bootStyleAt := strings.Index(html, `id="freenetCanonicalBootStyle"`)
	legacyStyleAt := strings.Index(html, `<style>`) // static legacy stylesheet from index.html
	if bootStyleAt < 0 || legacyStyleAt < 0 || bootStyleAt > legacyStyleAt {
		t.Fatalf("boot gate must be delivered before legacy stylesheet: boot=%d legacy=%d", bootStyleAt, legacyStyleAt)
	}
	if strings.Contains(html, `setTimeout(()=>{root.classList.remove('freenet-canonical-boot')`) {
		t.Fatal("boot gate must not fail-open to a legacy shell on a timer")
	}
}

func TestCanonicalIndexExactRootKeepsAssetFallback(t *testing.T) {
	a := &app{}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", a.handleIndex)
	registerCanonicalIndexRoute(mux, a)

	rootReq := httptest.NewRequest("GET", "http://router/", nil)
	rootRec := httptest.NewRecorder()
	mux.ServeHTTP(rootRec, rootReq)
	if rootRec.Code != http.StatusOK {
		t.Fatalf("canonical root status=%d", rootRec.Code)
	}
	body := rootRec.Body.String()
	rootNavStart := strings.Index(body, `<nav class="nav" aria-label="Навигация Control Center">`)
	rootNavEndRel := -1
	if rootNavStart >= 0 {
		rootNavEndRel = strings.Index(body[rootNavStart:], `</nav>`)
	}
	if rootNavStart < 0 || rootNavEndRel < 0 {
		t.Fatal("root canonical navigation missing")
	}
	rootNav := body[rootNavStart : rootNavStart+rootNavEndRel+len(`</nav>`)]
	if !strings.Contains(rootNav, ">Настройки</span></button>") || strings.Contains(rootNav, `data-page="subscription"`) || strings.Contains(rootNav, `data-page="automation"`) || !strings.Contains(rootNav, `data-page="journal"`) {
		t.Fatalf("root did not receive final canonical first-paint shell: %s", rootNav)
	}
	if !strings.Contains(body, `/accepted-ux.js?v=v`) {
		t.Fatal("root lost accepted UX delivery chain")
	}
	if !strings.Contains(body, `class="freenet-canonical-boot"`) {
		t.Fatal("root must start behind canonical boot gate")
	}
	acceptedAt := strings.Index(body, `/accepted-ux.js?v=v`)
	releaseAt := strings.Index(body, `id="freenetCanonicalBootRelease"`)
	if acceptedAt < 0 || releaseAt < 0 || releaseAt < acceptedAt {
		t.Fatalf("boot release must execute after accepted UX delivery: accepted=%d release=%d", acceptedAt, releaseAt)
	}

	assetReq := httptest.NewRequest("GET", "http://router/accepted-ux.js", nil)
	assetRec := httptest.NewRecorder()
	mux.ServeHTTP(assetRec, assetReq)
	if assetRec.Code != http.StatusOK {
		t.Fatalf("asset fallback status=%d", assetRec.Code)
	}
	if got := assetRec.Header().Get("Content-Type"); !strings.Contains(got, "application/javascript") {
		t.Fatalf("asset fallback content-type=%q", got)
	}
}
