package main

import (
	"math"
	"testing"
)

func TestBestServerHTTPResponseExcludesConnectionSetup(t *testing.T) {
	// Simulate a remote VPN where SOCKS/TCP/TLS setup consumed 700 ms but the
	// actual request-to-first-byte phase was 142 ms. The UI metric must report
	// HTTP responsiveness, not the whole cold connection transaction.
	ms, ok := parseBestServerHTTPResponseMS("204\t0.700\t0.842")
	if !ok {
		t.Fatal("expected valid HTTP response phase")
	}
	if ms != 142 {
		t.Fatalf("expected 142 ms response phase, got %d", ms)
	}
}

func TestBestServerDownloadUsesBodyPhaseNotColdTransferAverage(t *testing.T) {
	const bytes = int64(16 * 1024 * 1024)
	// Connection/TLS/TTFB took 850 ms; the completed 16 MiB body then arrived in
	// 500 ms. Whole-transfer averaging would report ~99 Mbps, while the payload
	// phase is ~268 Mbps and matches the capacity signal we actually want.
	mbps, ok := parseBestServerDownloadMbps("200\t16777216\t0.850\t1.350", bytes)
	if !ok {
		t.Fatal("expected valid body throughput")
	}
	if math.Abs(mbps-268.435456) > 0.01 {
		t.Fatalf("expected ~268.44 Mbps body throughput, got %.3f", mbps)
	}
	legacyWholeTransfer := float64(bytes) * 8 / 1.350 / 1_000_000
	if mbps < legacyWholeTransfer*2.5 {
		t.Fatalf("body-phase measurement should not be dominated by cold setup: body=%.2f legacy=%.2f", mbps, legacyWholeTransfer)
	}
}

func TestBestServerDownloadRejectsPartialTransfer(t *testing.T) {
	const bytes = int64(16 * 1024 * 1024)
	if _, ok := parseBestServerDownloadMbps("200\t1048576\t0.500\t1.000", bytes); ok {
		t.Fatal("partial download must not become a trusted throughput metric")
	}
}

func TestBestServerV4HighCapacityCurrentBeatsLowCapacityChallenger(t *testing.T) {
	current := bestServerQualityScore(150, 165, 20, 15, 250, true)
	challenger := bestServerQualityScore(120, 155, 15, 10, 8, true)
	if current <= challenger {
		t.Fatalf("high-capacity current VPN must outrank a low-capacity challenger: current=%d challenger=%d", current, challenger)
	}
}
