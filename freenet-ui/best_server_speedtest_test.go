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
	if !strings.Contains(got, "size=8000000") || !strings.Contains(got, "nocache=123") {
		t.Fatalf("missing bounded size/nonce: %s", got)
	}
	if strings.Contains(got, "upload.php") {
		t.Fatalf("upload endpoint leaked into download URL: %s", got)
	}
}

func TestBestServerSpeedtestDownloadURLFallsBackToHost(t *testing.T) {
	server := bestServerSpeedtestServer{Host: "speed.example:8080"}
	got := bestServerSpeedtestDownloadURL(server, 7)
	if got != "https://speed.example:8080/download?size=8000000&nocache=7" {
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
	mbps, ok := parseBestServerDownloadMbpsAtLeast("200\t8000000\t0.2\t1.2", 1600000)
	if !ok || mbps < 63.9 || mbps > 64.1 {
		t.Fatalf("unexpected stream throughput: %.2f ok=%v", mbps, ok)
	}
	if _, ok := parseBestServerDownloadMbpsAtLeast("403\t8000000\t0.2\t1.2", 1600000); ok {
		t.Fatal("HTTP error payload must not count as Speedtest throughput")
	}
}

func TestBestServerSpeedtestServerIndexSpreadsConcurrentStreamsAcrossServers(t *testing.T) {
	got := []int{
		bestServerSpeedtestServerIndex(8, 4, 0, 0),
		bestServerSpeedtestServerIndex(8, 4, 0, 1),
		bestServerSpeedtestServerIndex(8, 4, 0, 2),
		bestServerSpeedtestServerIndex(8, 4, 0, 3),
		bestServerSpeedtestServerIndex(8, 4, 1, 0),
		bestServerSpeedtestServerIndex(8, 4, 1, 1),
		bestServerSpeedtestServerIndex(8, 4, 1, 2),
		bestServerSpeedtestServerIndex(8, 4, 1, 3),
	}
	want := []int{0, 1, 2, 3, 4, 5, 6, 7}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("server distribution[%d]=%d want %d", i, got[i], want[i])
		}
	}
}

func TestBestServerSpeedtestServerIndexWrapsDeterministically(t *testing.T) {
	if got := bestServerSpeedtestServerIndex(5, 4, 1, 3); got != 2 {
		t.Fatalf("wrapped server index=%d want 2", got)
	}
	for _, tc := range [][4]int{{0, 4, 0, 0}, {4, 0, 0, 0}, {4, 4, -1, 0}, {4, 4, 0, -1}} {
		if got := bestServerSpeedtestServerIndex(tc[0], tc[1], tc[2], tc[3]); got != -1 {
			t.Fatalf("invalid input returned %d", got)
		}
	}
}
