package main

import (
	"os"
	"strings"
	"testing"
)

func TestFullBestServerScanOwnsCurrentBaseline(t *testing.T) {
	data, err := os.ReadFile("best_server_ux.go")
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	for _, want := range []string{
		"currentBaseline, currentBaselineOK := loadBestServerCurrentQuality",
		"baselineResponse := a.scanActiveCurrentVPNQuality",
		"response.Candidates = append(response.Candidates, currentBaseline)",
		"currentIndex := bestServerCurrentCandidateIndex(candidates, currentEndpoint, currentFilter)",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("full Best Server current-baseline contract missing %q", want)
		}
	}
	if strings.Contains(s, "if cachedOK {\n\t\tif currentIndex := bestServerCurrentCandidateIndex") {
		t.Fatal("current removal must not depend on a pre-existing cache")
	}
}
