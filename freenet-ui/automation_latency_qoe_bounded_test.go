package main

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestAutoVPNLatencyOnlyIncompleteCapacityIsComparisonOnly(t *testing.T) {
	current := automationMeasuredCurrentFixture()
	current.Eligible = false
	current.DownloadMbps = 0
	current.ThroughputSource = bestServerThroughputAutoMedia
	current.MediaSamples = automationMediaSegmentCount
	current.MediaGrade = "good"
	current.ApplicationMS = automationQualityLatencyStrongMS
	identity := bestServerCurrentQualityIdentityHash(current.Endpoint, "de Fixture")
	trigger := automationQualityTrigger{CurrentIdentity: identity, LatencyDegraded: true}
	response := bestServerQualityResponse{
		CurrentEndpoint: current.Endpoint,
		CurrentIdentity: identity,
		Candidates: []bestServerQualityCandidate{current},
	}
	if _, state := automationCurrentQualityState(response); state != "uncertain" {
		t.Fatalf("no strict speed must remain uncertain for manual/current quality: %s", state)
	}
	if !automationCurrentLatencyFailureReadyForQualityComparison(response, trigger) {
		t.Fatal("persistent matching app RTT and complete HTTPS must permit comparison despite missing aggregate speed")
	}
	for _, tc := range []struct {
		name string
		modify func(*bestServerQualityResponse, *automationQualityTrigger)
	}{
		{"unconfirmed latency", func(_ *bestServerQualityResponse, tr *automationQualityTrigger) { tr.LatencyDegraded = false }},
		{"media trigger is separate", func(_ *bestServerQualityResponse, tr *automationQualityTrigger) { tr.MediaDegraded = true }},
		{"service trigger remains full baseline", func(_ *bestServerQualityResponse, tr *automationQualityTrigger) { tr.ServiceBadMask = 1 }},
		{"different identity", func(r *bestServerQualityResponse, _ *automationQualityTrigger) { r.CurrentIdentity = bestServerCurrentQualityIdentityHash("192.0.2.11:443", "other") }},
		{"endpoint changed", func(r *bestServerQualityResponse, _ *automationQualityTrigger) { r.CurrentEndpoint = "192.0.2.11:443" }},
		{"no current candidate", func(r *bestServerQualityResponse, _ *automationQualityTrigger) { r.Candidates[0].Current = false }},
		{"unavailable current VPN", func(r *bestServerQualityResponse, _ *automationQualityTrigger) { r.Candidates[0].Available = false }},
		{"insufficient HTTPS", func(r *bestServerQualityResponse, _ *automationQualityTrigger) { r.Candidates[0].HTTPSamples = bestServerQualityHTTPRequired - 1 }},
		{"RTT recovered", func(r *bestServerQualityResponse, _ *automationQualityTrigger) { r.Candidates[0].ApplicationMS = automationQualityLatencyMildMS - 1 }},
		{"no RTT evidence", func(r *bestServerQualityResponse, _ *automationQualityTrigger) { r.Candidates[0].ApplicationMS = 0 }},
		{"no candidate", func(r *bestServerQualityResponse, _ *automationQualityTrigger) { r.Candidates = nil }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cpy := response
			cpy.Candidates = append([]bestServerQualityCandidate(nil), response.Candidates...)
			tr := trigger
			tc.modify(&cpy, &tr)
			if automationCurrentLatencyFailureReadyForQualityComparison(cpy, tr) {
				t.Fatal("unverified or recovered current route authorized a new quality scan")
			}
		})
	}
}

func TestAutoVPNLatencyOnlyDoesNotRepeatBulkSpeedtestBeforeComparison(t *testing.T) {
	a := automationQoECooldownFixture(t, time.Hour, "auto")
	oldCurrent, oldForeign, oldRefresh := automationBestCurrentQuality, automationBestForeignScan, automationBestCurrentRefresh
	t.Cleanup(func() {
		automationBestCurrentQuality, automationBestForeignScan, automationBestCurrentRefresh = oldCurrent, oldForeign, oldRefresh
	})
	current := automationMeasuredCurrentFixture()
	current.Eligible = false
	current.ApplicationMS = automationQualityLatencyStrongMS
	current.DownloadMbps = 0
	current.ThroughputSource = bestServerThroughputAutoMedia
	current.MediaSamples = automationMediaSegmentCount
	current.MediaGrade = "good"
	identity := bestServerCurrentQualityIdentityHash(current.Endpoint, "de Fixture")
	currentCalls, foreignCalls, refreshCalls := 0, 0, 0
	automationBestCurrentQuality = func(_ *app, ctx context.Context) (bestServerQualityResponse, error) {
		currentCalls++
		if !autoVPNCurrentMediaProbe(ctx) {
			t.Fatal("persistent latency triggered another costly 4-stream aggregate Speedtest")
		}
		return bestServerQualityResponse{
			CurrentEndpoint: current.Endpoint, CurrentIdentity: identity,
			Candidates: []bestServerQualityCandidate{current},
		}, nil
	}
	foreignErr := errors.New("bounded foreign comparison was reached")
	automationBestForeignScan = func(_ *app, _ context.Context, settings automationSettings, _ string) (bestServerQualityResponse, error) {
		foreignCalls++
		if settings.Policy != automationPolicyBetter || settings.AutoEndpointUpdate {
			t.Fatalf("AUTO latency used mutation-capable settings: %+v", settings)
		}
		return bestServerQualityResponse{}, foreignErr
	}
	automationBestCurrentRefresh = func(*app, context.Context) (int, bestServerRefreshResponse) {
		refreshCalls++
		return 200, bestServerRefreshResponse{Success: true, Applied: true}
	}
	result, err := a.runAutomationQualityCycle(context.Background(), automationSettings{
		Enabled: true, Mode: automationModeBest,
		Policy: automationPolicyDegraded, AutoApply: true, AutoEndpointUpdate: true,
	}, automationQualityTrigger{CurrentIdentity: identity, LatencyDegraded: true})
	if !errors.Is(err, foreignErr) || result.Result != "failed" ||
		result.Mutated || currentCalls != 1 || foreignCalls != 1 || refreshCalls != 0 {
		t.Fatalf("comparison current/foreign/refresh=%d/%d/%d result=%+v error=%v",
			currentCalls, foreignCalls, refreshCalls, result, err)
	}
}

func TestAutoVPNLatencyOnlyRecoveredBeforeComparisonStaysPut(t *testing.T) {
	a := automationQoECooldownFixture(t, time.Hour, "auto")
	oldCurrent, oldForeign := automationBestCurrentQuality, automationBestForeignScan
	t.Cleanup(func() { automationBestCurrentQuality, automationBestForeignScan = oldCurrent, oldForeign })
	c := automationMeasuredCurrentFixture()
	c.Eligible = false
	c.DownloadMbps = 0
	c.ThroughputSource = bestServerThroughputAutoMedia
	c.MediaSamples = automationMediaSegmentCount
	c.MediaGrade = "good"
	c.ApplicationMS = automationQualityLatencyMildMS - 1
	identity := bestServerCurrentQualityIdentityHash(c.Endpoint, "de Fixture")
	automationBestCurrentQuality = func(_ *app, ctx context.Context) (bestServerQualityResponse, error) {
		if !autoVPNCurrentMediaProbe(ctx) { t.Fatal("missing bounded latency current probe") }
		return bestServerQualityResponse{CurrentIdentity: identity, CurrentEndpoint: c.Endpoint,
			Candidates: []bestServerQualityCandidate{c}}, nil
	}
	foreignCalls := 0
	automationBestForeignScan = func(*app, context.Context, automationSettings, string) (bestServerQualityResponse, error) {
		foreignCalls++
		return bestServerQualityResponse{}, errors.New("must not scan recovered RTT")
	}
	result, err := a.runAutomationQualityCycle(context.Background(), automationSettings{
		Enabled: true, Mode: automationModeBest, Policy: automationPolicyBetter, AutoApply: true,
	}, automationQualityTrigger{CurrentIdentity: identity, LatencyDegraded: true})
	if err != nil || result.Mutated || result.Result != "uncertain" || foreignCalls != 0 {
		t.Fatalf("recovered/incomplete evidence must not trigger mutation, result=%+v foreign=%d error=%v", result, foreignCalls, err)
	}
}
