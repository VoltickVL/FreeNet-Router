package main

import (
	"context"
	"testing"
	"time"
)

func TestProbeBestServerHTTPAnyFallsBackToIndependentOrigin(t *testing.T) {
	calls := []string{}
	runner := func(_ context.Context, _, _, target string, _ time.Duration) (int, bool) {
		calls = append(calls, target)
		if target == "https://cp.cloudflare.com/generate_204" {
			return 87, true
		}
		return 0, false
	}
	ms, target, ok := probeBestServerHTTPAnyWith(
		context.Background(),
		"curl",
		"127.0.0.1:1080",
		[]string{"https://www.gstatic.com/generate_204", "https://cp.cloudflare.com/generate_204"},
		time.Second,
		runner,
	)
	if !ok || ms != 87 || target != "https://cp.cloudflare.com/generate_204" {
		t.Fatalf("fallback result ms=%d target=%q ok=%v", ms, target, ok)
	}
	if len(calls) != 2 || calls[0] == calls[1] {
		t.Fatalf("independent origins were not tried in order: %#v", calls)
	}
}

func TestProbeBestServerHTTPAnyStopsAfterPrimarySuccess(t *testing.T) {
	calls := 0
	runner := func(_ context.Context, _, _, _ string, _ time.Duration) (int, bool) {
		calls++
		return 42, true
	}
	ms, _, ok := probeBestServerHTTPAnyWith(
		context.Background(),
		"curl",
		"127.0.0.1:1080",
		[]string{"primary", "secondary"},
		time.Second,
		runner,
	)
	if !ok || ms != 42 || calls != 1 {
		t.Fatalf("primary success ms=%d ok=%v calls=%d", ms, ok, calls)
	}
}

func TestProbeBestServerTransportRTTUsesFixedIPHTTPSWithoutDNS(t *testing.T) {
	calls := 0
	runner := func(_ context.Context, curlPath, socks, target string, timeout time.Duration) (int, bool) {
		calls++
		if curlPath != "curl" || socks != "127.0.0.1:1080" {
			t.Fatalf("unexpected runner args curl=%q socks=%q", curlPath, socks)
		}
		if target != bestServerTransportProbeURL {
			t.Fatalf("target=%q want=%q", target, bestServerTransportProbeURL)
		}
		if timeout != bestServerTransportProbeTimeout {
			t.Fatalf("timeout=%s want=%s", timeout, bestServerTransportProbeTimeout)
		}
		return 163, true
	}
	ms, ok := probeBestServerTransportRTTWith(
		context.Background(), "curl", "127.0.0.1:1080",
		bestServerTransportProbeURL, bestServerTransportProbeTimeout, runner,
	)
	if !ok || ms != 163 || calls != 1 {
		t.Fatalf("fixed-IP VPN RTT result ms=%d ok=%v calls=%d", ms, ok, calls)
	}
}


func TestProbeBestServerConfirmedVPNPingUsesWarmupAndMedian(t *testing.T) {
	values := []int{999, 180, 160, 170, 175, 165}
	calls := 0
	runner := func(_ context.Context, _, _, target string, _ time.Duration) (int, bool) {
		if target != bestServerTransportProbeURL {
			t.Fatalf("target=%q want=%q", target, bestServerTransportProbeURL)
		}
		if calls >= len(values) {
			t.Fatalf("too many calls: %d", calls+1)
		}
		value := values[calls]
		calls++
		return value, true
	}
	got := probeBestServerConfirmedVPNPingWith(context.Background(), "curl", "127.0.0.1:1080", runner)
	wantCalls := bestServerConfirmedVPNPingWarmupRuns + bestServerConfirmedVPNPingRuns
	if calls != wantCalls {
		t.Fatalf("calls=%d want=%d", calls, wantCalls)
	}
	if !got.OK || got.Median != 170 || got.Jitter != 20 {
		t.Fatalf("confirmed result=%+v want median=170 jitter=20", got)
	}
	for _, sample := range got.Samples {
		if sample == 999 {
			t.Fatalf("warm-up leaked into measured samples: %+v", got)
		}
	}
}
