package main

import (
    "context"
    "strings"
    "testing"
)

func autoMediaMeasuredFixture() bestServerQualityCandidate {
    return bestServerQualityCandidate{
        ID: strings.Repeat("a", 16), Name: "DE Synthetic, Extra",
        Tested: true, Available: true, ApplicationMS: 160,
        JitterMS: 12, HTTPSamples: bestServerQualityHTTPRuns,
        ThroughputSource: bestServerThroughputAutoMedia,
        DownloadMbps: 12, MediaMbps: 12,
        MediaSamples: automationMediaSegmentCount,
        MediaGrade: "good", MediaStalls: 0,
    }
}

func TestAutomationMediaSequentialAdmissionUsesStreamingNotParallelCapacity(t *testing.T) {
    good := autoMediaMeasuredFixture()
    if !automationMediaMeasuredCandidateEligible(good) {
        t.Fatal("complete sequential media with usable throughput must be eligible")
    }
    cases := []struct {
        name string
        change func(*bestServerQualityCandidate)
    }{
        {"parallel aggregate 80 Mbps is not a video proof", func(c *bestServerQualityCandidate) { c.ThroughputSource = bestServerThroughputStrictAggregate; c.DownloadMbps = 80; c.MediaMbps = 80 }},
        {"single stream below the actual watchdog threshold", func(c *bestServerQualityCandidate) { c.DownloadMbps = automationMediaSlowMbps - .1; c.MediaMbps = automationMediaSlowMbps - .1 }},
        {"only one of two serial transfers", func(c *bestServerQualityCandidate) { c.MediaSamples = 1 }},
        {"one serial stall", func(c *bestServerQualityCandidate) { c.MediaStalls = 1; c.MediaGrade = "poor" }},
        {"no application HTTPS", func(c *bestServerQualityCandidate) { c.ApplicationMS = 0 }},
        {"latency known degraded", func(c *bestServerQualityCandidate) { c.ApplicationMS = automationQualityLatencyMildMS }},
        {"jitter outside acceptance", func(c *bestServerQualityCandidate) { c.JitterMS = bestServerQualityHighJitterMS + 1 }},
        {"changed identity", func(c *bestServerQualityCandidate) { c.ID = "invalid" }},
        {"current profile is not an alternative", func(c *bestServerQualityCandidate) { c.Current = true }},
    }
    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            c := good
            tc.change(&c)
            if automationMediaMeasuredCandidateEligible(c) {
                t.Fatalf("unsafe or unmeasured candidate incorrectly admitted: %+v", c)
            }
        })
    }
}

func TestAutoMediaMeasuredRankingUsesSequentialEligibilityBeforeStopping(t *testing.T) {
    previous := bestServerAutoMediaAppProbe
    t.Cleanup(func() { bestServerAutoMediaAppProbe = previous })
    calls := 0
    bestServerAutoMediaAppProbe = func(_ *app, ctx context.Context, candidate bestServerInternalCandidate) bestServerQualityApplicationResult {
        if ctx.Err() != nil { t.Fatal("scan context unexpectedly expired") }
        calls++
        return bestServerQualityApplicationResult{
            OK: true, DownloadOK: true, DownloadMbps: 14,
            ThroughputSource: bestServerThroughputAutoMedia,
            VPN: bestServerProbeResult{OK: true, Median: 130, Jitter: 10},
            HTTP: bestServerProbeResult{OK: true, Median: 150, Jitter: 10, Samples: []int{145,150,152,148,153}},
            Media: bestServerMediaQualityResult{
                OK: true, Samples: automationMediaSegmentCount, MinMbps: 14,
                MedianMbps: 14, Grade: "good", Stalls: 0,
            },
        }
    }
    items := []bestServerInternalCandidate{
        {Profile: subscriptionProfile{ID: strings.Repeat("a",16), Name: "DE Synthetic, Extra", Address:"203.0.113.11",Port:443}},
        {Profile: subscriptionProfile{ID: strings.Repeat("b",16), Name: "CH Synthetic, Extra", Address:"203.0.113.12",Port:443}},
    }
    a := &app{}
    out := a.rankMeasuredBestServerBatches(context.Background(), items, len(items), false, "203.0.113.10:443", "BE Current, Extra", 1, true)
    if calls != 1 || out.DeepChecked != 1 || out.EligibleFound != 1 || out.Recommendation == nil {
        t.Fatalf("media target not counted in same bounded scan: calls=%d result=%+v", calls, out)
    }
    c := out.Candidates[0]
    if !c.Eligible || c.ThroughputSource != bestServerThroughputAutoMedia || c.DownloadMbps != 14 ||
       len(c.Rejections) != 0 || c.MediaSamples != automationMediaSegmentCount {
        t.Fatalf("AUTO candidate not promoted from sequential transfer evidence: %+v", c)
    }
    // Generic 4-stream transfer and 4/4 HEAD rules remain unchanged for
    // manual Speedtest. This media-only result is not generic-eligible.
    if eligibleBestServerQuality(c) {
        t.Fatal("AUTO media-only eligibility leaked into generic manual quality policy")
    }
}
