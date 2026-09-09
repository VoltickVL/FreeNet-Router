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
