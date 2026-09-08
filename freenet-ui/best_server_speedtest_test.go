package main

import (
	"strings"
	"testing"
)

func TestBestServerSpeedtestDownloadURLUsesServerOrigin(t *testing.T) {
	server := bestServerSpeedtestServer{URL: "https://speed.example:8080/speedtest/upload.php", Host: "ignored.example:8080"}
	got := bestServerSpeedtestDownloadURL(server, 123)
	if !strings.HasPrefix(got, "https://speed.example:8080/download?") {
		t.Fatalf("unexpected Speedtest download URL: %s", got)
	}
	if !strings.Contains(got, "size=4000000") || !strings.Contains(got, "nocache=123") {
		t.Fatalf("missing bounded size/nonce: %s", got)
	}
	if strings.Contains(got, "upload.php") {
		t.Fatalf("upload endpoint leaked into download URL: %s", got)
	}
}

func TestBestServerSpeedtestDownloadURLFallsBackToHost(t *testing.T) {
	server := bestServerSpeedtestServer{Host: "speed.example:8080"}
	got := bestServerSpeedtestDownloadURL(server, 7)
	if got != "https://speed.example:8080/download?size=4000000&nocache=7" {
		t.Fatalf("unexpected host fallback: %s", got)
	}
}

func TestBestServerSpeedtestDownloadURLRejectsUnsafeHost(t *testing.T) {
	for _, host := range []string{"", "bad host", "example.com/path", "example.com?x=1"} {
		if got := bestServerSpeedtestDownloadURL(bestServerSpeedtestServer{Host: host}, 1); got != "" {
			t.Fatalf("unsafe host accepted %q -> %q", host, got)
		}
	}
}

func TestBestServerSpeedtestThroughputParserAcceptsUsefulPartialBody(t *testing.T) {
	// 4,000,000 bytes in one second of body phase = 32 Mbit/s.
	mbps, ok := parseBestServerDownloadMbpsAtLeast("200\t4000000\t0.2\t1.2", 800000)
	if !ok || mbps < 31.9 || mbps > 32.1 {
		t.Fatalf("unexpected single-stream throughput: %.2f ok=%v", mbps, ok)
	}
	if _, ok := parseBestServerDownloadMbpsAtLeast("403\t4000000\t0.2\t1.2", 800000); ok {
		t.Fatal("HTTP error payload must not count as Speedtest throughput")
	}
}
