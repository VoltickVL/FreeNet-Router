package main

import "testing"

func TestBestServerAggregateMediaQualityUsesCombinedCapacity(t *testing.T) {
	result := summarizeBestServerAggregateMediaQuality([]float64{38, 41, 39, 42, 40, 41}, 4, 4)
	if !result.OK {
		t.Fatalf("aggregate Speedtest sample should be usable: %+v", result)
	}
	if result.MedianMbps < 230 || result.MedianMbps > 260 {
		t.Fatalf("aggregate capacity should reflect concurrent streams, got %.1f Mbps", result.MedianMbps)
	}
	if result.Grade != "excellent" {
		t.Fatalf("stable aggregate path should be excellent: %+v", result)
	}
}

func TestBestServerAggregateMediaQualityRejectsStreamStall(t *testing.T) {
	stable := summarizeBestServerAggregateMediaQuality([]float64{42, 40, 39, 41, 38, 40}, 4, 4)
	stalled := summarizeBestServerAggregateMediaQuality([]float64{42, 40, 5, 41, 38, 40}, 4, 4)
	if !stalled.OK || stalled.Stalls < 1 || stalled.Penalty <= stable.Penalty {
		t.Fatalf("stream stall must be detected and penalized: stable=%+v stalled=%+v", stable, stalled)
	}
}

func TestBestServerAggregateMediaQualityPenalizesServicePathFailures(t *testing.T) {
	allOK := summarizeBestServerAggregateMediaQuality([]float64{40, 42, 41, 39, 38, 43}, 4, 4)
	partial := summarizeBestServerAggregateMediaQuality([]float64{40, 42, 41, 39, 38, 43}, 2, 4)
	if partial.Penalty <= allOK.Penalty {
		t.Fatalf("service path failures must increase penalty: all=%+v partial=%+v", allOK, partial)
	}
	if partial.Grade == "excellent" || partial.Grade == "good" {
		t.Fatalf("service path failures must reduce grade: %+v", partial)
	}
}

func TestBestServerAggregateMediaQualityNeedsAllStreams(t *testing.T) {
	result := summarizeBestServerAggregateMediaQuality([]float64{80, 82, 79, 81, 80}, 4, 4)
	if result.OK || result.Penalty == 0 {
		t.Fatalf("too few Speedtest streams must fail closed: %+v", result)
	}
}
