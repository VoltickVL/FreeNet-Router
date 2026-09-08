package main

import "testing"

func TestBestServerMediaQualityAcceptsStableSingleStreamSamples(t *testing.T) {
	stable := summarizeBestServerMediaQuality([]float64{92, 88, 95}, 3, 3)
	if !stable.OK || stable.Grade != "excellent" || stable.Stalls != 0 {
		t.Fatalf("stable Speedtest samples should be excellent: %+v", stable)
	}
}

func TestBestServerMediaQualityRejectsSingleStreamStall(t *testing.T) {
	stable := summarizeBestServerMediaQuality([]float64{92, 88, 95}, 3, 3)
	stalled := summarizeBestServerMediaQuality([]float64{96, 8, 94}, 3, 3)
	if !stalled.OK || stalled.Stalls < 1 || stalled.Penalty <= stable.Penalty {
		t.Fatalf("single-stream stall must be detected and penalized: stable=%+v stalled=%+v", stable, stalled)
	}
}

func TestBestServerMediaQualityPenalizesServicePathFailures(t *testing.T) {
	allOK := summarizeBestServerMediaQuality([]float64{60, 62, 64}, 3, 3)
	partial := summarizeBestServerMediaQuality([]float64{60, 62, 64}, 1, 3)
	if partial.Penalty <= allOK.Penalty {
		t.Fatalf("service path failures must increase penalty: all=%+v partial=%+v", allOK, partial)
	}
	if partial.Grade == "excellent" || partial.Grade == "good" {
		t.Fatalf("service path failures must reduce grade: %+v", partial)
	}
}

func TestBestServerMediaQualityNeedsAllSingleStreamSamples(t *testing.T) {
	result := summarizeBestServerMediaQuality([]float64{80, 82}, 3, 3)
	if result.OK || result.Penalty == 0 {
		t.Fatalf("too few Speedtest samples must fail closed: %+v", result)
	}
}
