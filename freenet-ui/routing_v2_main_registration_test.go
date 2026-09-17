package main

import (
	"os"
	"strings"
	"testing"
)

func TestMainCallsProductionRouteAggregator(t *testing.T) {
	data, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "registerGeoDataAPI(mux, a)") {
		t.Fatal("production main must call the route aggregator that wires Routing v2 and Policy APIs")
	}
}

func TestProductionRouteAggregatorRegistersRoutingV2AndPolicyRoutes(t *testing.T) {
	data, err := os.ReadFile("geodata_api.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(data)
	for _, required := range []string{
		"registerCanonicalIndexRoute(mux, a)",
		"registerRoutingConfigAPI(mux, a)",
		"registerPolicyPreviewAPI(mux, a)",
	} {
		if !strings.Contains(source, required) {
			t.Fatalf("production route aggregator missing %q", required)
		}
	}
}
