package main

import (
	"errors"
	"strings"
	"testing"
)

func TestBestServerErrorResponsesAreNotSuccessfulMeasurements(t *testing.T) {
	for _, code := range []string{"302", "403", "429", "500", "000", "2xx"} {
		if bestServerHTTPStatusOK(code) { t.Fatalf("accepted HTTP %s", code) }
		if _, ok := parseBestServerHTTPResponseMS(code+"\t0.1\t0.2"); ok { t.Fatalf("error page counted as service success: %s", code) }
		if _, ok := parseBestServerDownloadMbpsAtLeast(code+"\t4194304\t0.1\t0.2", 2097152); ok { t.Fatalf("error payload counted as speed: %s", code) }
	}
	if !bestServerHTTPStatusOK("200") || !bestServerHTTPStatusOK("204") { t.Fatal("valid successful HTTP rejected") }
}

func TestBestServerTransferDiagnosticsExcludeRawSecrets(t *testing.T) {
	result := bestServerTransferIssue("403\t123\t0.1\t0.2", nil)
	if result != "HTTP 403; curl 0; получено 123 байт" { t.Fatal(result) }
	result = bestServerTransferIssue("https://secret.example/token vless://sensitive", errors.New("credential secret"))
	if strings.Contains(result, "secret") || strings.Contains(result, "vless") || strings.Contains(result, "token") { t.Fatalf("raw data leaked: %s", result) }
}

func TestBestServerRejectionsDistinguishUnmeasuredAndSlow(t *testing.T) {
	c := bestServerQualityCandidate{Tested: true, Available: true, MediaSamples: 0, ServiceTotal: 4, ServiceOK: 4}
	if got := strings.Join(bestServerRejectionReasons(c), ";"); !strings.Contains(got, "Скорость не измерена") || !strings.Contains(got, "0/6") { t.Fatal(got) }
	c.DownloadMbps = 4
	if got := strings.Join(bestServerRejectionReasons(c), ";"); !strings.Contains(got, "ниже 20") || strings.Contains(got, "Скорость не измерена") { t.Fatal(got) }
	c.Tested = false
	if got := strings.Join(bestServerRejectionReasons(c), ";"); !strings.Contains(got, "не выполнялась") { t.Fatal(got) }
}
