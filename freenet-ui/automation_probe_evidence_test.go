package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAutomationHTTPStatusIsEvidenceNotOutage(t *testing.T) {
	for _, status := range []string{"200", "204", "301", "403", "405", "503"} {
		t.Run(status, func(t *testing.T) {
			ms, ok := parseAutomationVPNHTTPResponse(status + "\t0.100\t0.450")
			if !ok || ms != 350 {
				t.Fatalf("valid VPN HTTPS %s was treated as transport down: ms=%d ok=%t", status, ms, ok)
			}
		})
	}
	for _, malformed := range []string{"", "000\t0.1\t0.2", "503\t0.5\t0.4", "200\tNaN\t0.9", "407", "200\t0.1\tInf"} {
		if _, ok := parseAutomationVPNHTTPResponse(malformed); ok {
			t.Fatalf("invalid synthetic response accepted: %q", malformed)
		}
	}
}

func TestAutomationProbeClassifiesTimeoutAndEnvironmentAsUnknown(t *testing.T) {
	for _, code := range []int{6, 28, 60, 97, 127} {
		if automationHTTPConclusiveCurlExit(code) {
			t.Fatalf("probe failure exit=%d is not conclusive VPN outage", code)
		}
	}
	for _, code := range []int{7, 35, 56} {
		if !automationHTTPConclusiveCurlExit(code) {
			t.Fatalf("known connection failure %d not tracked", code)
		}
	}
	fixtures := []struct {
		name, body string
		timeout time.Duration
		reachable, failed bool
	}{
		{"server-405", "printf '405\\t0.11\\t0.33'\nexit 0\n", time.Second, true, false},
		{"timeout-exit-28", "exit 28\n", time.Second, false, true},
		{"dns-exit-6", "exit 6\n", time.Second, false, false},
		{"refused-exit-7", "exit 7\n", time.Second, false, true},
		{"slow-probe", "sleep 1\nexit 7\n", 25*time.Millisecond, false, true},
	}
	for _, tc := range fixtures {
		t.Run(tc.name, func(t *testing.T) {
			script := filepath.Join(t.TempDir(), "fake-curl")
			if err := os.WriteFile(script, []byte("#!/bin/sh\n"+tc.body), 0700); err != nil {
				t.Fatal(err)
			}
			got := runAutomationVPNHTTPProbe(context.Background(), script, "127.0.0.1:18181", "https://example.invalid", tc.timeout, true)
			if got.Reachable != tc.reachable || got.ConclusiveFailure != tc.failed {
				t.Fatalf("probe result=%+v wanted reachable=%t failed=%t", got, tc.reachable, tc.failed)
			}
		})
	}
}

func TestAutomationNamedProbeNeedsAllConclusiveFailures(t *testing.T) {
	base := func(outcomes []automationVPNHTTPObservation) (int, bool, bool, int) {
		calls := 0
		runner := func(_ context.Context, _, _, _ string, timeout time.Duration, named bool) automationVPNHTTPObservation {
			if !named || timeout != automationHealthNamedTimeout {
				t.Fatalf("unexpected named probe mode=%t timeout=%s", named, timeout)
			}
			i := calls
			calls++
			if i >= len(outcomes) {
				t.Fatalf("extra named origin probe %d", i)
			}
			return outcomes[i]
		}
		ms, ok, failed := probeAutomationNamedHTTPSDetailedWith(context.Background(), "curl", "socks", runner)
		return ms, ok, failed, calls
	}
	for _, tc := range []struct{
		name string
		input []automationVPNHTTPObservation
		wantMS int
		wantReachable, wantFailure bool
		wantCalls int
	}{
		{"both-proven-down", []automationVPNHTTPObservation{{ConclusiveFailure:true},{ConclusiveFailure:true}},0,false,true,2},
		{"one-timeout", []automationVPNHTTPObservation{{ConclusiveFailure:true},{}},0,false,false,2},
		{"other-provider-HTTP-403", []automationVPNHTTPObservation{{ConclusiveFailure:true},{Reachable:true,MS:283}},283,true,false,2},
		{"primary-HTTP-success", []automationVPNHTTPObservation{{Reachable:true,MS:178}},178,true,false,1},
	}{
		t.Run(tc.name,func(t *testing.T){
			ms,ok,failed,calls:=base(tc.input)
			if ms!=tc.wantMS||ok!=tc.wantReachable||failed!=tc.wantFailure||calls!=tc.wantCalls{
				t.Fatalf("%s result: ms=%d ok=%t completeFail=%t calls=%d",tc.name,ms,ok,failed,calls)
			}
		})
	}
}

func TestAutomationCurrentHealthCodeDoesNotTreatQoEAsOutage(t *testing.T) {
	genericUp := classifyAutomationReachableQuality(219, 0, 4)
	if genericUp.State != automationHealthUncertain || genericUp.QualityPoints != 0 || !genericUp.QualityDegraded {
		t.Fatalf("all media blocked with HTTPS alive is not global VPN down: %+v", genericUp)
	}
	if got := classifyAutomationHealth(genericUp, true, genericUp); got.State == automationHealthCritical {
		t.Fatalf("media failure authorized emergency switch: %+v", got)
	}
	if got := classifyAutomationApplicationFailure(true,true); got.State != automationHealthFailed || strings.Contains(got.Reason,"аварий") {
		t.Fatalf("named HTTPS blocked but transport passed: %+v",got)
	}
	if got := classifyAutomationApplicationFailure(false,true); got.State != automationHealthFailed {
		t.Fatalf("two named + IP decisively unavailable must still be FAILED: %+v",got)
	}
}
