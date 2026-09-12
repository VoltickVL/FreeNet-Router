package main

import (
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestSettingsV3IsDeliveredByProductionIndex(t *testing.T) {
	a := &app{}

	rootReq := httptest.NewRequest("GET", "http://router/", nil)
	rootRec := httptest.NewRecorder()
	a.handleIndex(rootRec, rootReq)
	if rootRec.Code != 200 {
		t.Fatalf("root status=%d want 200", rootRec.Code)
	}
	body := rootRec.Body.String()
	if !strings.Contains(body, `/settings-v3.js?v=v`) {
		t.Fatal("production index does not load settings-v3.js")
	}
	if strings.Index(body, `/accepted-ux.js?v=v`) > strings.Index(body, `/settings-v3.js?v=v`) {
		t.Fatal("settings-v3.js must load after accepted-ux.js")
	}

	assetReq := httptest.NewRequest("GET", "http://router/settings-v3.js", nil)
	assetRec := httptest.NewRecorder()
	a.handleIndex(assetRec, assetReq)
	if assetRec.Code != 200 {
		t.Fatalf("settings-v3.js status=%d want 200", assetRec.Code)
	}
	if got := assetRec.Header().Get("Content-Type"); !strings.Contains(got, "application/javascript") {
		t.Fatalf("settings-v3.js content-type=%q", got)
	}
	if !strings.Contains(assetRec.Body.String(), "Настройки / Система") {
		t.Fatal("served settings-v3.js is not the accepted Settings v3 asset")
	}
}

func TestSettingsV3ProductionMuxRegistrationCannotBeDropped(t *testing.T) {
	mainSource, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(mainSource)
	for _, want := range []string{
		`web/settings-v3.js`,
		`registerSettingsV3API(mux, a)`,
		`r.URL.Path == "/settings-v3.js"`,
		`<script src=\"/settings-v3.js?v=v%s\"></script>`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("production Settings v3 wiring missing %q", want)
		}
	}
}
