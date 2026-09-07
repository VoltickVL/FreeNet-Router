package main

import "testing"

func TestBestServerQualityV3RejectsSlowWinner(t *testing.T) {
	// A server with visibly poor throughput must not win merely because its
	// latency is somewhat lower. This is the runtime failure observed on WORK:
	// a ~5 Mbps candidate was presented as "best" while faster nearby profiles
	// were expected to be more useful for normal browsing/video traffic.
	slow := bestServerQualityScore(420, 105, 18, 12, 5.2, true)
	balanced := bestServerQualityScore(620, 130, 25, 18, 80, true)
	if balanced <= slow {
		t.Fatalf("balanced 80 Mbps candidate must outrank 5.2 Mbps candidate: balanced=%d slow=%d", balanced, slow)
	}
}

func TestBestServerQualityV3RewardsUsableThroughput(t *testing.T) {
	low := bestServerQualityScore(500, 120, 20, 15, 12, true)
	good := bestServerQualityScore(500, 120, 20, 15, 75, true)
	if good-low < 1500 {
		t.Fatalf("throughput must materially affect score, delta=%d", good-low)
	}
}

func TestBestServerQualityV3NoSpeedMeasurementCannotWin(t *testing.T) {
	unknown := bestServerQualityScore(350, 100, 10, 8, 0, false)
	measured := bestServerQualityScore(520, 125, 18, 12, 45, true)
	if measured <= unknown {
		t.Fatalf("measured usable candidate must outrank candidate without throughput measurement: measured=%d unknown=%d", measured, unknown)
	}
}
