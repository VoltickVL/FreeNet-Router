package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSettingsV3IsDeliveredByCanonicalAutomationPipeline(t *testing.T) {
	req := httptest.NewRequest("GET", "http://router/accepted-ux.js", nil)
	rec := httptest.NewRecorder()
	serveAcceptedUXWithAutomation(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("accepted UX status=%d want 200", rec.Code)
	}
	body := rec.Body.String()
	order := []string{
		"/api/automation/assets/runtime-acceptance.js",
		"/api/automation/assets/automation.js",
		"/api/automation/assets/automation-async.js",
		"/api/automation/assets/settings-v3.js",
	}
	last := -1
	for _, want := range order {
		idx := strings.Index(body, want)
		if idx < 0 {
			t.Fatalf("canonical accepted UX pipeline does not load %q", want)
		}
		if idx <= last {
			t.Fatalf("Settings v3 pipeline order is wrong around %q", want)
		}
		last = idx
	}

	assetReq := httptest.NewRequest("GET", "http://router/api/automation/assets/settings-v3.js", nil)
	assetRec := httptest.NewRecorder()
	serveAutomationAsset("web/settings-v3.js")(assetRec, assetReq)
	if assetRec.Code != http.StatusOK {
		t.Fatalf("settings-v3 asset status=%d want 200", assetRec.Code)
	}
	if got := assetRec.Header().Get("Content-Type"); !strings.Contains(got, "application/javascript") {
		t.Fatalf("settings-v3 content-type=%q", got)
	}
	if !strings.Contains(assetRec.Body.String(), "Настройки / Система") {
		t.Fatal("served settings-v3 asset is not the accepted render")
	}
}

func TestSettingsV3RoutesAreRegisteredThroughProductionGeoDataChain(t *testing.T) {
	a := &app{}
	mux := http.NewServeMux()
	registerGeoDataAPI(mux, a)

	for _, path := range []string{"/api/settings-v3", "/api/settings-v3/action", "/api/automation/assets/settings-v3.js"} {
		req := httptest.NewRequest("GET", "http://router"+path, nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code == http.StatusNotFound {
			t.Fatalf("production registration chain dropped %s", path)
		}
	}
}
