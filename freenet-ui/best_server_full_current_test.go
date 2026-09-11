package main

import (
	"os"
	"strings"
	"testing"
)

func TestFullBestServerScanReusesCurrentBaselineWithoutImplicitHeavyCheck(t *testing.T) {
	data, err := os.ReadFile("best_server_ux.go")
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	for _, want := range []string{
		"currentBaseline, currentBaselineOK := loadBestServerCurrentQuality",
		"response.Candidates = append(response.Candidates, currentBaseline)",
		"currentIndex := bestServerCurrentCandidateIndex(candidates, currentEndpoint, currentFilter)",
		"Текущий VPN не перепроверялся автоматически",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("Best Server current-baseline contract missing %q", want)
		}
	}
	if strings.Contains(s, "baselineResponse := a.scanActiveCurrentVPNQuality") {
		t.Fatal("Best Server alternatives scan must not spend its bounded Top-3 budget on an implicit current VPN deep check")
	}
	if strings.Contains(s, "if currentBaselineOK {\n\t\tif currentIndex := bestServerCurrentCandidateIndex") {
		t.Fatal("current candidate removal must not depend on a pre-existing current-quality cache")
	}
}
