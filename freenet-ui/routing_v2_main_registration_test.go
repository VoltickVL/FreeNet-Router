package main

import (
	"os"
	"strings"
	"testing"
)

func TestMainRegistersRoutingV2AndPolicyProductionRoutes(t *testing.T) {
	data, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	mainSource := string(data)
	for _, required := range []string{
		"registerCanonicalIndexRoute(mux, a)",
		"registerRoutingConfigAPI(mux, a)",
		"registerPolicyPreviewAPI(mux, a)",
	} {
		if !strings.Contains(mainSource, required) {
			t.Fatalf("production main route registration missing %q", required)
		}
	}
}
