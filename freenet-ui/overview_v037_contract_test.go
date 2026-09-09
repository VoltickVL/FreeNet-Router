package main

import (
    "os"
    "strings"
    "testing"
    "time"
)

func TestV037BestServerBudgets(t *testing.T) {
    if bestServerQualityCandidateTimeout < 50*time.Second { t.Fatalf("candidate deep-probe budget too small: %v", bestServerQualityCandidateTimeout) }
    if bestServerQualityScanTimeout < 6*bestServerQualityCandidateTimeout { t.Fatalf("global scan budget cannot finish a full measured set: %v", bestServerQualityScanTimeout) }
    if bestServerCurrentScanTimeout < bestServerQualityCandidateTimeout { t.Fatalf("current baseline budget smaller than candidate budget") }
    if bestServerTargetedRetryTimeout < bestServerQualityCandidateTimeout { t.Fatalf("targeted retry budget smaller than candidate budget") }
    if bestServerSpeedtestRunTimeout < 8*time.Second || bestServerSpeedtestServerTries < 3 { t.Fatalf("speedtest retry budget not expanded") }
    if bestServerMediaTimeout < 20*time.Second { t.Fatalf("media budget too small: %v", bestServerMediaTimeout) }
}

func TestV037OverviewScaleContract(t *testing.T) {
    data, err := os.ReadFile("web/operation-coordinator.js")
    if err != nil { t.Fatal(err) }
    s := string(data)
    for _, needle := range []string{
        "Issue #350: v0.3.7 final render-scale alignment",
        "width:min(1370px,calc(100% - 46px))",
        ".page[data-page-view=\\\"overview\\\"] .page-head h1{font-size:34px",
        ".best-v4-shell{grid-template-columns:minmax(365px,390px)",
        ".vpn-option-title h4{font-size:18px",
        ".vpn-option .best-v4-pill b{font-size:17px",
        ".fn-render-fact>strong{font-size:12px",
    } {
        if !strings.Contains(s, needle) { t.Fatalf("v0.3.7 Overview scale contract missing %q", needle) }
    }
}
