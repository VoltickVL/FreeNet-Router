package main

import (
	"math"
	"testing"
)

func TestBestServerHTTPResponseExcludesConnectionSetup(t *testing.T) {
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

func TestBestServerDownloadStrictParserRejectsPartialTransfer(t *testing.T) {
	const bytes = int64(16 * 1024 * 1024)
	if _, ok := parseBestServerDownloadMbps("200\t1048576\t0.500\t1.000", bytes); ok {
		t.Fatal("strict media/object parser must reject a truncated transfer")
	}
}

func TestBestServerCapacityAcceptsUsefulPartialBodyOnTimeout(t *testing.T) {
	// A bounded 16 MiB probe can hit its deadline after receiving several MiB.
	// The body-phase rate is still useful capacity evidence and must not vanish.
	mbps, ok := parseBestServerDownloadMbpsAtLeast("200\t4194304\t0.800\t2.800", 2*1024*1024)
	if !ok {
		t.Fatal("expected sufficient partial body to remain a valid capacity sample")
	}
	if math.Abs(mbps-16.777216) > 0.01 {
		t.Fatalf("expected ~16.78 Mbps partial-body throughput, got %.3f", mbps)
	}
	if _, ok := parseBestServerDownloadMbpsAtLeast("200\t524288\t0.800\t2.800", 2*1024*1024); ok {
		t.Fatal("too-small partial body must remain untrusted")
	}
}

func TestBestServerV4HighCapacityCurrentBeatsLowCapacityChallenger(t *testing.T) {
	current := bestServerQualityScore(150, 165, 20, 15, 250, true)
	challenger := bestServerQualityScore(120, 155, 15, 10, 8, true)
	if current <= challenger {
		t.Fatalf("high-capacity current VPN must outrank a low-capacity challenger: current=%d challenger=%d", current, challenger)
	}
}
