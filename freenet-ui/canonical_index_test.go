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

	for _, want := range []string{"Обзор", "Подписка", "Настройки", "Маршрутизация", "Журнал"} {
		if !strings.Contains(html, ">"+want+"</button>") {
			t.Fatalf("canonical first-paint navigation missing %q", want)
		}
	}
	for _, retired := range []string{">VPN</button>", ">Сеть</button>", ">Автоматизация</button>", ">Система</button>", ">Доступ и безопасность</button>"} {
		if strings.Contains(html, retired) {
			t.Fatalf("retired first-paint navigation leaked %q", retired)
		}
	}
	if strings.Contains(html, `<div class="side-bottom">`) {
		t.Fatal("legacy sidebar footer must not be present in first-paint HTML")
	}
	if !strings.Contains(html, `const pageLabels={overview:'Обзор',subscription:'Подписка'};`) {
		t.Fatal("legacy page labels remain routable during bootstrap")
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
	if !strings.Contains(body, ">Настройки</button>") || strings.Contains(body, ">Автоматизация</button>") {
		t.Fatalf("root did not receive canonical first-paint shell")
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
