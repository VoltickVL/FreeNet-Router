package main

import "testing"

func TestBestServerMediaQualityRejectsPeriodicStalls(t *testing.T) {
	stable := summarizeBestServerMediaQuality([]float64{92, 88, 95, 91, 86, 90}, 3, 3)
	stalled := summarizeBestServerMediaQuality([]float64{96, 91, 8, 94, 7, 89}, 3, 3)
	if !stable.OK || stable.Grade != "excellent" || stable.Stalls != 0 {
		t.Fatalf("stable media path should be excellent: %+v", stable)
	}
	if !stalled.OK || stalled.Stalls < 2 || stalled.Penalty <= stable.Penalty {
		t.Fatalf("periodic stalls must be detected and penalized: stable=%+v stalled=%+v", stable, stalled)
	}
}

func TestBestServerMediaQualityPenalizesServicePathFailures(t *testing.T) {
	allOK := summarizeBestServerMediaQuality([]float64{60, 62, 64, 61, 59, 63}, 3, 3)
	partial := summarizeBestServerMediaQuality([]float64{60, 62, 64, 61, 59, 63}, 1, 3)
	if partial.Penalty <= allOK.Penalty {
		t.Fatalf("service path failures must increase penalty: all=%+v partial=%+v", allOK, partial)
	}
	if partial.Grade == "excellent" || partial.Grade == "good" {
		t.Fatalf("service path failures must reduce media grade: %+v", partial)
	}
}

func TestBestServerMediaQualityNeedsEnoughSamples(t *testing.T) {
	result := summarizeBestServerMediaQuality([]float64{80, 82, 79}, 3, 3)
	if result.OK || result.Penalty == 0 {
		t.Fatalf("too few media samples must fail closed: %+v", result)
	}
}

func TestBestServerConcurrentMediaQualityUsesAggregateCapacity(t *testing.T) {
	// Six concurrent ~40 Mbps streams represent roughly a 240 Mbps path.
	// The old serial probe treated the same path as ~40 Mbps and could be even
	// lower on 1 MiB objects because every transfer paid TCP slow-start.
	result := summarizeBestServerConcurrentMediaQuality([]float64{38, 41, 39, 42, 40, 41}, 3, 3)
	if !result.OK {
		t.Fatalf("concurrent media sample should be usable: %+v", result)
	}
	if result.MedianMbps < 230 || result.MedianMbps > 260 {
		t.Fatalf("aggregate capacity should reflect concurrent streams, got %.1f Mbps", result.MedianMbps)
	}
	if result.Grade != "excellent" {
		t.Fatalf("stable ~240 Mbps aggregate path should be excellent: %+v", result)
	}
}

func TestBestServerConcurrentMediaQualityStillFailsClosedOnTooFewStreams(t *testing.T) {
	result := summarizeBestServerConcurrentMediaQuality([]float64{70, 68, 72}, 3, 3)
	if result.OK {
		t.Fatalf("concurrent probe must still require at least four completed streams: %+v", result)
	}
}
