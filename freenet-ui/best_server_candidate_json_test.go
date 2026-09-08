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
