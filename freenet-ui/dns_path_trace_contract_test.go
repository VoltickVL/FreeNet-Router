package main

import (
	"os"
	"strings"
	"testing"
)

func TestDNSPathTraceRouteIsRegistered(t *testing.T) {
	data, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, "registerDNSPathTrace(mux, a)") {
		t.Fatal("DNS path trace route is not registered")
	}
}

func TestDNSPathTraceIsReadOnlyByContract(t *testing.T) {
	data, err := os.ReadFile("dns_path_trace.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, forbidden := range []string{
		"writeNetworkProfileConfig(",
		"runNetworkApplyFor(",
		"ndmc -c",
		"04_outbounds.json",
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("read-only DNS trace contains forbidden mutation/credential surface %q", forbidden)
		}
	}
	if !strings.Contains(text, "Mutation: \"NONE\"") {
		t.Fatal("DNS trace must report MUTATION=NONE")
	}
}
