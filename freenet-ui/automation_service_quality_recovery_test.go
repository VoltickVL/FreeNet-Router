package main

import (
	"strings"
	"testing"
)

func TestAUTOServiceOutageCanCompareWithIncompleteCurrentSpeedtest(t *testing.T) {
	identity := strings.Repeat("a", 64)
	trigger := automationQualityTrigger{CurrentIdentity: identity, ServiceBadMask: 0x01}
	probe := bestServerQualityResponse{
		Success: true,
		Available: false, // throughput/media did not finish
		CurrentIdentity: identity,
		CurrentEndpoint: "192.0.2.10:443",
		Candidates: []bestServerQualityCandidate{{
			Current: true, Tested: true, Available: true, Eligible: false,
			Endpoint: "192.0.2.10:443",
			ApplicationMS: 145, HTTPSamples: bestServerQualityHTTPRequired,
			ServiceOK: 2, ServiceTotal: 4,
			MediaGrade: "unknown", MediaSamples: 0, DownloadMbps: 0,
		}},
	}
	if _, currentState := automationCurrentQualityState(probe); currentState != "uncertain" {
		t.Fatalf("incomplete speedtest must remain uncertain before identity-bound service fallback: %q", currentState)
	}
	if !automationCurrentServiceFailureReadyForQualityComparison(probe, trigger) {
		t.Fatal("four consecutive live service failures must not be blocked only by missing media speedtest")
	}
	for _, tc := range []struct{
		name string
		alter func(*bestServerQualityResponse,*automationQualityTrigger)
	}{
		{"no-persistent-service-trigger",func(_ *bestServerQualityResponse, t *automationQualityTrigger){t.ServiceBadMask=0;t.LatencyDegraded=true}},
		{"other-profile-identity",func(r *bestServerQualityResponse,_ *automationQualityTrigger){r.CurrentIdentity=strings.Repeat("b",64)}},
		{"invalid-trigger-identity",func(_ *bestServerQualityResponse,t *automationQualityTrigger){t.CurrentIdentity="broken"}},
		{"missing-current-candidate",func(r *bestServerQualityResponse,_ *automationQualityTrigger){r.Candidates=nil}},
		{"profile-not-current",func(r *bestServerQualityResponse,_ *automationQualityTrigger){r.Candidates[0].Current=false}},
		{"not-tested",func(r *bestServerQualityResponse,_ *automationQualityTrigger){r.Candidates[0].Tested=false}},
		{"generic-vpn-not-available",func(r *bestServerQualityResponse,_ *automationQualityTrigger){r.Candidates[0].Available=false}},
		{"endpoint-rotated",func(r *bestServerQualityResponse,_ *automationQualityTrigger){r.Candidates[0].Endpoint="192.0.2.20:443"}},
		{"no-complete-https-samples",func(r *bestServerQualityResponse,_ *automationQualityTrigger){r.Candidates[0].HTTPSamples=1}},
		{"no-application-rtt",func(r *bestServerQualityResponse,_ *automationQualityTrigger){r.Candidates[0].ApplicationMS=0}},
		{"service-healthy",func(r *bestServerQualityResponse,_ *automationQualityTrigger){r.Candidates[0].ServiceOK=4}},
		{"insufficient-service-targets",func(r *bestServerQualityResponse,_ *automationQualityTrigger){r.Candidates[0].ServiceOK=1;r.Candidates[0].ServiceTotal=2}},
	}{
		t.Run(tc.name,func(t *testing.T){
			r:=probe
			r.Candidates=append([]bestServerQualityCandidate(nil),probe.Candidates...)
			got:=trigger
			tc.alter(&r,&got)
			if automationCurrentServiceFailureReadyForQualityComparison(r,got) {
				t.Fatal("incomplete or mismatched service evidence cannot authorize AUTO comparison")
			}
		})
	}
}
