package main

import (
	"context"
	"errors"
	"testing"
	"time"
)

func automationMeasuredCurrentFixture() bestServerQualityCandidate {
	return bestServerQualityCandidate{
		Tested: true, Current: true, Reachable: true, Available: true, Eligible: true,
		ID: "current-live", Name: "de Current", CountryCode: "de", Endpoint: "192.0.2.10:443",
		ApplicationMS: 160, HTTPSamples: bestServerQualityHTTPRuns, JitterMS: 18,
		VPNRTTMS: 155, VPNJitterMS: 12, VPNPingConfirmed: true,
		DownloadMbps: 60, ThroughputSource: bestServerThroughputStrictAggregate,
		MediaSamples: bestServerMediaRequiredRuns, MediaGrade: "excellent", MediaMbps: 60,
		ServiceOK: 4, ServiceTotal: 4,
	}
}

func TestAutomationCurrentQualityRequiresCompleteCanonicalEvidence(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*bestServerQualityCandidate)
		want   string
	}{
		{"complete healthy", func(*bestServerQualityCandidate) {}, "healthy"},
		{"not tested", func(c *bestServerQualityCandidate) { c.Tested = false }, "uncertain"},
		{"unavailable", func(c *bestServerQualityCandidate) { c.Available = false }, "uncertain"},
		{"no HTTP evidence", func(c *bestServerQualityCandidate) { c.ApplicationMS = 0 }, "uncertain"},
		{"partial HTTP", func(c *bestServerQualityCandidate) { c.HTTPSamples = bestServerQualityHTTPRequired - 1 }, "uncertain"},
		{"no HTTP samples", func(c *bestServerQualityCandidate) { c.HTTPSamples = 0 }, "uncertain"},
		{"media timeout", func(c *bestServerQualityCandidate) {
			c.Eligible, c.DownloadMbps, c.MediaSamples = false, 0, 0
			c.ThroughputSource, c.MediaGrade = "", "unknown"
			c.DownloadIssue = "Speedtest throughput unavailable"
		}, "uncertain"},
		{"one media sample", func(c *bestServerQualityCandidate) {
			c.Eligible, c.DownloadMbps, c.MediaSamples = false, 0, 1
		}, "uncertain"},
		{"two media samples", func(c *bestServerQualityCandidate) {
			c.Eligible, c.DownloadMbps, c.MediaSamples = false, 0, 2
		}, "uncertain"},
		{"incomplete services", func(c *bestServerQualityCandidate) { c.ServiceTotal = 2 }, "uncertain"},
		{"complete slow HTTP", func(c *bestServerQualityCandidate) {
			c.Eligible, c.ApplicationMS = false, bestServerQualityMaxApplicationMS+1
		}, "degraded"},
		{"complete high jitter", func(c *bestServerQualityCandidate) {
			c.Eligible, c.JitterMS = false, bestServerQualityHighJitterMS+1
		}, "degraded"},
		{"complete slow throughput", func(c *bestServerQualityCandidate) {
			c.Eligible, c.DownloadMbps, c.MediaGrade = false, 12, "fair"
		}, "degraded"},
		{"completed tiny throughput rounds to zero", func(c *bestServerQualityCandidate) {
			c.Eligible, c.DownloadMbps, c.MediaMbps, c.MediaGrade = false, 0, 0, "poor"
		}, "degraded"},
		{"complete stalled media", func(c *bestServerQualityCandidate) {
			c.Eligible, c.MediaStalls, c.MediaGrade = false, 1, "fair"
		}, "degraded"},
		{"complete service rejection", func(c *bestServerQualityCandidate) {
			c.Eligible, c.ServiceOK, c.MediaGrade = false, 3, "fair"
		}, "degraded"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			candidate := automationMeasuredCurrentFixture()
			tc.change(&candidate)
			got, state := automationCurrentQualityState(bestServerQualityResponse{
				Candidates: []bestServerQualityCandidate{candidate},
			})
			if state != tc.want || got.ID != candidate.ID {
				t.Fatalf("state=%q candidate=%+v; want %s and the measured baseline", state, got, tc.want)
			}
		})
	}
}

func TestAutomationQualityIntentDoesNotRefreshOrScanRecoveredCurrent(t *testing.T) {
	oldRefresh, oldCurrent, oldForeign := automationBestCurrentRefresh, automationBestCurrentQuality, automationBestForeignScan
	t.Cleanup(func() {
		automationBestCurrentRefresh, automationBestCurrentQuality, automationBestForeignScan = oldRefresh, oldCurrent, oldForeign
	})
	foreignErr := errors.New("foreign comparison reached")
	for _, tc := range []struct {
		name, want string
		change     func(*bestServerQualityCandidate)
		currentErr error
		foreign    bool
	}{
		{"recovered current", "same", func(*bestServerQualityCandidate) {}, nil, false},
		{"missing speed", "uncertain", func(c *bestServerQualityCandidate) {
			c.Eligible, c.DownloadMbps, c.MediaSamples = false, 0, 0
			c.MediaGrade, c.ThroughputSource = "unknown", ""
		}, nil, false},
		{"interrupted HTTP", "uncertain", func(c *bestServerQualityCandidate) {
			c.Eligible, c.HTTPSamples = false, 2
		}, nil, false},
		{"parent deadline", "failed", func(*bestServerQualityCandidate) {}, context.DeadlineExceeded, false},
		{"still degraded", "failed", func(c *bestServerQualityCandidate) {
			c.Eligible, c.ApplicationMS = false, 350
		}, nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := automationQoECooldownFixture(t, time.Hour, "auto")
			before := parseAutomationState(automationStatePath())
			refreshCalls, currentCalls, foreignCalls := 0, 0, 0
			automationBestCurrentRefresh = func(*app, context.Context) (int, bestServerRefreshResponse) {
				refreshCalls++
				return 200, bestServerRefreshResponse{Success: true, Applied: true, Mutation: "APPLIED"}
			}
			automationBestCurrentQuality = func(*app, context.Context) (bestServerQualityResponse, error) {
				currentCalls++
				candidate := automationMeasuredCurrentFixture()
				tc.change(&candidate)
				return bestServerQualityResponse{
					Candidates: []bestServerQualityCandidate{candidate},
					CurrentIdentity: bestServerCurrentQualityIdentityHash(candidate.Endpoint, "de Fixture"),
				}, tc.currentErr
			}
			automationBestForeignScan = func(_ *app, _ context.Context, settings automationSettings, _ string) (bestServerQualityResponse, error) {
				foreignCalls++
				if settings.Policy != automationPolicyBetter || settings.AutoEndpointUpdate {
					t.Fatalf("quality intent did not keep comparison-only settings: %+v", settings)
				}
				return bestServerQualityResponse{}, foreignErr
			}
			settings := automationSettings{
				Enabled: true, Mode: automationModeBest, Policy: automationPolicyDegraded,
				AutoApply: true, AutoEndpointUpdate: true,
			}
			result, err := a.runAutomationQualityCycle(context.Background(), settings, automationQualityTrigger{
				CurrentIdentity: bestServerCurrentQualityIdentityHash("192.0.2.10:443", "de Fixture"),
				LatencyDegraded: true,
			})
			wantForeign := 0
			if tc.foreign {
				wantForeign = 1
			}
			if refreshCalls != 0 || currentCalls != 1 || foreignCalls != wantForeign ||
				result.Result != tc.want || result.Mutated || result.ProfileID != "" {
				t.Fatalf("refresh/current/foreign=%d/%d/%d result=%+v; want 0/1/%d %s without mutation",
					refreshCalls, currentCalls, foreignCalls, result, wantForeign, tc.want)
			}
			if tc.foreign && !errors.Is(err, foreignErr) ||
				tc.currentErr != nil && !errors.Is(err, tc.currentErr) ||
				!tc.foreign && tc.currentErr == nil && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if settings.Policy != automationPolicyDegraded || !settings.AutoEndpointUpdate {
				t.Fatal("quality-only intent changed the caller's stored settings value")
			}
			after := parseAutomationState(automationStatePath())
			if after["LAST_SWITCH"] != before["LAST_SWITCH"] || after["LAST_SWITCH_ORIGIN"] != before["LAST_SWITCH_ORIGIN"] {
				t.Fatal("comparison changed the previous switch baseline")
			}
		})
	}
}
