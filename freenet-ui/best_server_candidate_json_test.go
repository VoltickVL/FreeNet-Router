package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBestServerCandidateJSONOmitsProviderSpecificTransferIssues(t *testing.T) {
	payload, err := json.Marshal(bestServerQualityCandidate{
		ID: "test", Name: "Test", Endpoint: "example.test:443", Tested: true,
		DownloadIssue: "Speedtest internal detail", MediaIssue: "another internal detail",
		Rejections: []string{"Скорость Speedtest не измерена"},
	})
	if err != nil {
		t.Fatal(err)
	}
	text := string(payload)
	if strings.Contains(text, "download_issue") || strings.Contains(text, "media_issue") || strings.Contains(text, "internal detail") {
		t.Fatalf("provider-specific transfer detail leaked: %s", text)
	}
	if !strings.Contains(text, "Скорость Speedtest не измерена") {
		t.Fatalf("normalized rejection reason missing: %s", text)
	}
}

func TestBestServerCandidateJSONExposesThroughputProvenanceWithoutInternalIssues(t *testing.T) {
	payload, err := json.Marshal(bestServerQualityCandidate{
		ID: "current", Name: "Current Extra", Endpoint: "example.test:443", Current: true, Tested: true, Available: true,
		FallbackDownloadMbps: 37.4, ThroughputSource: bestServerThroughputCurrentFallback,
		DownloadIssue: "provider internal detail",
	})
	if err != nil {
		t.Fatal(err)
	}
	text := string(payload)
	if !strings.Contains(text, `"fallback_download_mbps":37.4`) || !strings.Contains(text, `"throughput_source":"current_fallback"`) {
		t.Fatalf("public throughput provenance missing: %s", text)
	}
	if strings.Contains(text, `"download_mbps"`) {
		t.Fatalf("fallback throughput must not masquerade as canonical download_mbps: %s", text)
	}
	if strings.Contains(text, "provider internal detail") || strings.Contains(text, "download_issue") {
		t.Fatalf("internal transfer issue leaked with provenance: %s", text)
	}
}

func TestBestServerCandidateJSONNormalizesEmojiFlagForCrossPlatformUI(t *testing.T) {
	payload, err := json.Marshal(bestServerQualityCandidate{
		ID: "dk", Name: "🇩🇰 Копенгаген, Дания, Extra", Endpoint: "example.test:443", Tested: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	text := string(payload)
	if !strings.Contains(text, `"country_code":"dk"`) {
		t.Fatalf("country metadata missing: %s", text)
	}
	if !strings.Contains(text, `"name":"Копенгаген, Дания, Extra"`) {
		t.Fatalf("display label not normalized: %s", text)
	}
	if strings.Contains(text, "🇩🇰") {
		t.Fatalf("platform-dependent emoji leaked into public candidate label: %s", text)
	}
}
