package main

import (
	"errors"
	"testing"
	"time"
)

func splitPostApplyFixture(active bool) networkPlanResponse {
	return networkPlanResponse{
		Success: true, Supported: true, Active: active,
		DNSMode: "xkeen", EffectiveDNSMode: "xkeen", Mutation: "NONE",
	}
}

func TestSplitPostApplyReadyDoesNotRereadOrSleep(t *testing.T) {
	ready := splitPostApplyFixture(true)
	reads, sleeps := 0, 0
	got, err := settleSplitDNSPostApplyPlan(ready, nil, func() (networkPlanResponse, error) {
		reads++
		return networkPlanResponse{}, nil
	}, func(time.Duration) { sleeps++ })
	if err != nil || !verifiedSplitDNSPostApplyPlan(got, err) || reads != 0 || sleeps != 0 {
		t.Fatalf("ready plan must not rerun any operation: got=%+v err=%v reads=%d sleeps=%d", got, err, reads, sleeps)
	}
}

func TestSplitPostApplyTransientFalseNegativeSettlesReadOnly(t *testing.T) {
	reads, sleeps := 0, 0
	got, err := settleSplitDNSPostApplyPlan(splitPostApplyFixture(false), nil, func() (networkPlanResponse, error) {
		reads++
		if reads == 1 {
			return splitPostApplyFixture(false), nil
		}
		return splitPostApplyFixture(true), nil
	}, func(delay time.Duration) {
		if delay != splitDNSPostApplyInterval {
			t.Fatalf("unexpected recheck delay %s", delay)
		}
		sleeps++
	})
	if err != nil || !verifiedSplitDNSPostApplyPlan(got, err) || reads != 2 || sleeps != 2 {
		t.Fatalf("transient restart identity must settle with only read-only checks: got=%+v err=%v reads=%d sleeps=%d", got, err, reads, sleeps)
	}
}

func TestSplitPostApplyPersistentUnverifiedMustNotBecomeSuccess(t *testing.T) {
	reads, sleeps := 0, 0
	got, err := settleSplitDNSPostApplyPlan(splitPostApplyFixture(false), nil, func() (networkPlanResponse, error) {
		reads++
		return splitPostApplyFixture(false), nil
	}, func(time.Duration) { sleeps++ })
	if err != nil || verifiedSplitDNSPostApplyPlan(got, err) || reads != splitDNSPostApplyRechecks || sleeps != splitDNSPostApplyRechecks {
		t.Fatalf("persistent incomplete plan must retain rollback admission: got=%+v err=%v reads=%d sleeps=%d", got, err, reads, sleeps)
	}
}

func TestSplitPostApplyHardErrorStopsWithoutRetry(t *testing.T) {
	original := errors.New("network plan unavailable")
	reads, sleeps := 0, 0
	_, err := settleSplitDNSPostApplyPlan(networkPlanResponse{}, original, func() (networkPlanResponse, error) {
		reads++
		return splitPostApplyFixture(true), nil
	}, func(time.Duration) { sleeps++ })
	if !errors.Is(err, original) || reads != 0 || sleeps != 0 {
		t.Fatalf("hard read-only plan error must not be hidden: err=%v reads=%d sleeps=%d", err, reads, sleeps)
	}
}

func TestSplitPostApplyReadErrorStopsAtFirstFailure(t *testing.T) {
	original := errors.New("probe unavailable")
	reads, sleeps := 0, 0
	_, err := settleSplitDNSPostApplyPlan(splitPostApplyFixture(false), nil, func() (networkPlanResponse, error) {
		reads++
		return networkPlanResponse{}, original
	}, func(time.Duration) { sleeps++ })
	if !errors.Is(err, original) || reads != 1 || sleeps != 1 {
		t.Fatalf("probe error must not be retried indefinitely: err=%v reads=%d sleeps=%d", err, reads, sleeps)
	}
}

func TestSplitPostApplyRejectsWrongModeAndUnsafePlans(t *testing.T) {
	tests := []struct {
		name string
		edit func(*networkPlanResponse)
	}{
		{"wrong_dns_mode", func(p *networkPlanResponse) { p.DNSMode = "firmware" }},
		{"wrong_effective_mode", func(p *networkPlanResponse) { p.EffectiveDNSMode = "firmware" }},
		{"unsupported", func(p *networkPlanResponse) { p.Supported = false }},
		{"unsuccessful", func(p *networkPlanResponse) { p.Success = false }},
		{"mutation_detected", func(p *networkPlanResponse) { p.Mutation = "APPLY" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := splitPostApplyFixture(true)
			tt.edit(&p)
			if verifiedSplitDNSPostApplyPlan(p, nil) {
				t.Fatalf("unsafe plan incorrectly accepted: %+v", p)
			}
			reads, sleeps := 0, 0
			got, err := settleSplitDNSPostApplyPlan(p, nil, func() (networkPlanResponse, error) {
				reads++
				return p, nil
			}, func(time.Duration) { sleeps++ })
			if verifiedSplitDNSPostApplyPlan(got, err) {
				t.Fatalf("unsafe target was marked accepted: %+v", got)
			}
			if !p.Success || !p.Supported || p.Mutation != "NONE" {
				if reads != 0 || sleeps != 0 {
					t.Fatalf("unsafe plan should not be retried: reads=%d sleeps=%d", reads, sleeps)
				}
			} else if reads != splitDNSPostApplyRechecks || sleeps != splitDNSPostApplyRechecks {
				t.Fatalf("mode mismatch must stop after bounded checks: reads=%d sleeps=%d", reads, sleeps)
			}
		})
	}
}
