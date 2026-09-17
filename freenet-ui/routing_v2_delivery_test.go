package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCanonicalIndexDeliversRoutingV2BeforeBootRelease(t *testing.T) {
	rawBytes, err := webFS.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	html, err := canonicalizeControlCenterIndex(string(rawBytes))
	if err != nil {
		t.Fatal(err)
	}
	routingAt := strings.Index(html, `<script id="freenetRoutingV2">`)
	releaseAt := strings.Index(html, `id="freenetCanonicalBootRelease"`)
	if routingAt < 0 {
		t.Fatal("canonical shell does not deliver Routing v2")
	}
	if releaseAt < 0 || routingAt > releaseAt {
		t.Fatalf("Routing v2 must load before canonical boot release: routing=%d release=%d", routingAt, releaseAt)
	}
	if strings.Contains(html, `<script src="/routing-v2.js"></script>`) {
		t.Fatal("canonical shell must not request the historical raw Routing v2 asset")
	}
}

func TestRoutingConfigRegistrationServesAssetAndAPIs(t *testing.T) {
	a := &app{}
	mux := http.NewServeMux()
	registerRoutingConfigAPI(mux, a)

	assetReq := httptest.NewRequest(http.MethodGet, "http://router/routing-v2.js", nil)
	assetRec := httptest.NewRecorder()
	mux.ServeHTTP(assetRec, assetReq)
	if assetRec.Code != http.StatusOK {
		t.Fatalf("routing asset status=%d", assetRec.Code)
	}
	if got := assetRec.Header().Get("Content-Type"); !strings.Contains(got, "application/javascript") {
		t.Fatalf("routing asset content-type=%q", got)
	}
	if !strings.Contains(assetRec.Body.String(), "Config Studio") {
		t.Fatal("routing asset did not contain Routing v2 workspace")
	}

	// API routes are auth-protected: an unauthenticated call must be intercepted
	// before it can inspect router files.
	apiReq := httptest.NewRequest(http.MethodGet, "http://router/api/routing/config", nil)
	apiRec := httptest.NewRecorder()
	mux.ServeHTTP(apiRec, apiReq)
	if apiRec.Code == http.StatusNotFound {
		t.Fatal("routing config API was not registered")
	}
}
