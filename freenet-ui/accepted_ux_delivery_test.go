package main

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAcceptedUXIsEmbeddedServedAndLoaded(t *testing.T) {
	a := &app{}

	assetReq := httptest.NewRequest("GET", "/accepted-ux.js", nil)
	assetW := httptest.NewRecorder()
	a.handleIndex(assetW, assetReq)
	if assetW.Code != 200 {
		t.Fatalf("accepted UX asset status = %d", assetW.Code)
	}
	asset := assetW.Body.String()
	for _, want := range []string{"Запомнить меня на этом устройстве", "Обновление FreeNet", "freenetUpdatePopover"} {
		if !strings.Contains(asset, want) {
			t.Fatalf("accepted UX asset missing %q", want)
		}
	}
	if strings.Contains(asset, "MutationObserver") {
		t.Fatal("accepted UX must stay event-driven and must not use MutationObserver")
	}

	indexReq := httptest.NewRequest("GET", "/", nil)
	indexW := httptest.NewRecorder()
	a.handleIndex(indexW, indexReq)
	if indexW.Code != 200 {
		t.Fatalf("index status = %d", indexW.Code)
	}
	if !strings.Contains(indexW.Body.String(), "/accepted-ux.js?v=v") {
		t.Fatal("accepted UX script is not loaded by Control Center index")
	}
}
