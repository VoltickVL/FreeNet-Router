package main

import (
	"context"
	"testing"
	"time"
)

func TestBestServerAggregateMediaQualityUsesCombinedCapacity(t *testing.T) {
	result := summarizeBestServerAggregateMediaQuality([]float64{44, 46, 45, 43}, 4, 4)
	if !result.OK {
		t.Fatalf("aggregate Speedtest sample should be usable: %+v", result)
	}
	if result.MedianMbps < 170 || result.MedianMbps > 190 {
		t.Fatalf("aggregate capacity should reflect concurrent streams, got %.1f Mbps", result.MedianMbps)
	}
	if result.Grade != "excellent" {
		t.Fatalf("stable aggregate path should be excellent: %+v", result)
	}
}

func TestBestServerAggregateMediaQualityAcceptsThreeOfFourStreams(t *testing.T) {
	result := summarizeBestServerAggregateMediaQuality([]float64{50, 48, 49}, 4, 4)
	if !result.OK || result.Samples != 3 || result.MedianMbps < 140 {
		t.Fatalf("three completed streams should form bounded aggregate evidence: %+v", result)
	}
}

func TestBestServerAggregateMediaQualityRejectsTooFewStreams(t *testing.T) {
	result := summarizeBestServerAggregateMediaQuality([]float64{80, 82}, 4, 4)
	if result.OK || result.Penalty == 0 {
		t.Fatalf("fewer than required Speedtest streams must fail closed: %+v", result)
	}
}

func TestBestServerAggregateMediaQualityRejectsStreamStall(t *testing.T) {
	stable := summarizeBestServerAggregateMediaQuality([]float64{42, 40, 39, 41}, 4, 4)
	stalled := summarizeBestServerAggregateMediaQuality([]float64{42, 40, 5, 41}, 4, 4)
	if !stalled.OK || stalled.Stalls < 1 || stalled.Penalty <= stable.Penalty {
		t.Fatalf("stream stall must be detected and penalized: stable=%+v stalled=%+v", stable, stalled)
	}
}

func TestBestServerAggregateMediaQualityPenalizesServicePathFailures(t *testing.T) {
	allOK := summarizeBestServerAggregateMediaQuality([]float64{40, 42, 41, 39}, 4, 4)
	partial := summarizeBestServerAggregateMediaQuality([]float64{40, 42, 41, 39}, 2, 4)
	if partial.Penalty <= allOK.Penalty {
		t.Fatalf("service path failures must increase penalty: all=%+v partial=%+v", allOK, partial)
	}
	if partial.Grade == "excellent" || partial.Grade == "good" {
		t.Fatalf("service path failures must reduce grade: %+v", partial)
	}
}


func TestRunBestServerMediaStagesOverlap(t *testing.T) {
	started := time.Now()
	result := runBestServerMediaStages(
		context.Background(),
		func(context.Context) ([]float64, string) {
			time.Sleep(120 * time.Millisecond)
			return []float64{40, 41, 42, 43}, "speed-note"
		},
		func(context.Context) (int, int) {
			time.Sleep(120 * time.Millisecond)
			return 4, 4
		},
	)
	elapsed := time.Since(started)
	if elapsed >= 200*time.Millisecond {
		t.Fatalf("media/service stages look sequential: elapsed=%s", elapsed)
	}
	if len(result.speeds) != 4 || result.issue != "speed-note" || result.serviceOK != 4 || result.serviceTotal != 4 {
		t.Fatalf("unexpected overlapped stage result: %+v", result)
	}
}
