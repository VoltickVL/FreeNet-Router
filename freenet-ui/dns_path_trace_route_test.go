package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDNSPathTraceRouteRequiresAuthentication(t *testing.T) {
	a := &app{sem: make(chan struct{}, 1)}
	mux := http.NewServeMux()
	registerDNSPathTrace(mux, a)

	r := httptest.NewRequest(http.MethodGet, "http://192.168.50.1:1001/api/dns/path-trace?host=ipleak.net", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code == http.StatusNotFound {
		t.Fatal("DNS path trace route is not mounted")
	}
	if w.Code == http.StatusOK {
		t.Fatal("DNS path trace must not bypass authentication")
	}
}
