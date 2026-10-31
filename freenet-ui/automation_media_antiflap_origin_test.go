package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func TestAutoMediaIndependentRangeRejectsUnknownAndIncompleteBodies(t *testing.T) {
	good := fmt.Sprintf("206 %d 0.100 0.200 0.500", automationMediaConfirmationBytes)
	if p := parseAutomationIndependentMediaSegment(good); !p.Valid || p.Degraded || p.Mbps < automationMediaSlowMbps {
		t.Fatalf("valid independent 1 MiB HTTP range was rejected: %+v", p)
	}
	if p := parseAutomationMediaSegment(good, automationMediaConfirmationBytes); p.Valid {
		t.Fatal("ordinary Cloudflare parser admitted a partial HTTP 206")
	}
	for _, input := range []string{
		fmt.Sprintf("206 %d 0.100 0.200 0.500", automationMediaSegmentBytes),
		fmt.Sprintf("200 %d 0.100 0.200 0.500", automationMediaSegmentBytes),
		fmt.Sprintf("206 %d 0.100 0.200 0.500", automationMediaConfirmationBytes/2),
		fmt.Sprintf("302 %d 0.100 0.200 0.500", automationMediaConfirmationBytes),
		fmt.Sprintf("403 %d 0.100 0.200 0.500", automationMediaConfirmationBytes),
		fmt.Sprintf("206 %d 0.500 0.200 0.800", automationMediaConfirmationBytes),
		"206 0 0 0 0",
	} {
		if p := parseAutomationIndependentMediaSegment(input); p.Valid {
			t.Fatalf("incomplete/unknown independent origin incorrectly counted: %q -> %+v", input, p)
		}
	}
	if p := parseAutomationIndependentMediaSegment(fmt.Sprintf("200 %d 0.100 0.200 0.500", automationMediaConfirmationBytes)); !p.Valid {
		t.Fatal("origin returning exact completed HTTP 200 body should be usable")
	}
	if automationIndependentMediaTimeout > automationHealthProbeTimeout ||
		automationIndependentMediaURL == bestServerFallbackSpeedURL ||
		!strings.HasPrefix(automationIndependentMediaURL, "https://") {
		t.Fatal("independent QoE origin/time budget is unsafe")
	}
}

func TestAutoMediaTwoSourcesDisagreeOrUnknownNeverTriggerSwitch(t *testing.T) {
	good := automationMediaEvidence{Complete: true, Samples: 2, MinMbps: 18}
	bad := automationMediaEvidence{Complete: true, Degraded: true, Samples: 2, MinMbps: 1.5}
	fast := autoMediaSample(24, 100*time.Millisecond)
	slow := autoMediaSample(2.2, 200*time.Millisecond)
	testCases := []struct{
		name string
		initial automationMediaEvidence
		force bool
		secondary automationMediaSegment
		wantComplete bool
		wantDegraded bool
		wantCalls int
	}{
		{"healthy watchdog no extra body", good, false, slow, true, false, 0},
		{"bad Cloudflare bad OVH confirmed", bad, false, slow, true, true, 1},
		{"bad Cloudflare fast OVH contradiction", bad, false, fast, false, false, 1},
		{"bad Cloudflare unknown OVH fails closed", bad, false, automationMediaSegment{}, false, false, 1},
		{"good candidate good OVH accepted", good, true, fast, true, false, 1},
		{"good candidate slow OVH rejected", good, true, slow, false, false, 1},
		{"good candidate unknown OVH rejected", good, true, automationMediaSegment{}, false, false, 1},
		{"incomplete primary no extra transfer", automationMediaEvidence{}, true, fast, false, false, 0},
	}
	for _, tc := range testCases {
		t.Run(tc.name,func(t *testing.T){
			calls:=0
			runner:=func(ctx context.Context, curl,socks string,index int) automationMediaSegment{
				if ctx.Err()!=nil || curl!="curl" || socks!="127.0.0.1:8421" || index!=automationMediaSegmentCount {
					t.Fatalf("independent probe not bound to measured SOCKS/current context: %s %s %d",curl,socks,index)
				}
				calls++
				return tc.secondary
			}
			got:=corroborateAutomationMediaSources(context.Background(),"curl","127.0.0.1:8421",tc.initial,tc.force,runner)
			if got.Complete!=tc.wantComplete || got.Degraded!=tc.wantDegraded || calls!=tc.wantCalls {
				t.Fatalf("unverified source authorized media: got=%+v calls=%d; expected complete=%v degraded=%v calls=%d",
					got,calls,tc.wantComplete,tc.wantDegraded,tc.wantCalls)
			}
		})
	}
	ctx,cancel:=context.WithCancel(context.Background());cancel()
	if got:=corroborateAutomationMediaSources(ctx,"curl","127.0.0.1:8421",bad,true,
		func(context.Context,string,string,int)automationMediaSegment {
			t.Fatal("cancelled context started extra media transfer")
			return fast
		});got.Complete {
		t.Fatalf("cancelled media corroboration became conclusive: %+v",got)
	}
}

func TestAutoMediaAfterSwitchQuietBlocksOnlyHeavyMediaComparison(t *testing.T) {
	now:=time.Now().UTC().Truncate(time.Second)
	id:=bestServerCurrentQualityIdentityHash("media.example.invalid:443","de fixture")
	bad:=autoMediaHealth(id,true,true)
	if bad.State!=automationHealthUncertain || !bad.MediaDegraded {
		t.Fatalf("fixture not a quality-only media incident: %+v",bad)
	}
	a:=automationQoECooldownFixture(t,25*time.Minute,"auto")
	_ = a
	for _, when := range []time.Time{now,now.Add(90*time.Second),now.Add(4*time.Minute)} {
		if trigger,due:=automationReachableQualityOptimizationDue(when,bad);due || trigger.MediaDegraded{
			t.Fatalf("recent profile change launched media-only deep scan: %+v due=%v",trigger,due)
		}
	}
	state:=v3ParseState(os.Getenv("FREENET_SETTINGS_V3_STATE"))
	if state["QUALITY_OPTIMIZATION_LAST"]!="" {
		t.Fatalf("media settling reserved a heavy comparison: %v",state)
	}
	if state["QUALITY_MEDIA_COUNT"]!="" && state["QUALITY_MEDIA_COUNT"]!="0" {
		t.Fatalf("media settling accumulated an unauthorized strike: %v",state)
	}
	if !automationMediaSwitchSettling(now,now.Add(-25*time.Minute).Format(time.RFC3339)) ||
		automationMediaSwitchSettling(now,now.Add(-61*time.Minute).Format(time.RFC3339)) ||
		!automationMediaSwitchSettling(now,"bad-state") ||
		!automationMediaSwitchSettling(now,now.Add(time.Minute).Format(time.RFC3339)) {
		t.Fatal("media-only after-switch quiet window is not fail closed")
	}
}

func TestAutoMediaAfterSwitchQuietExpiresAndEmergencyStillWorks(t *testing.T) {
	_ = automationQoECooldownFixture(t,2*time.Hour,"auto")
	now:=time.Now().UTC().Truncate(time.Second)
	id:=bestServerCurrentQualityIdentityHash("media.example.invalid:443","de fixture")
	bad:=autoMediaHealth(id,true,true)
	if _,due:=automationReachableQualityOptimizationDue(now,bad);due {
		t.Fatal("single media observation launched deep comparison")
	}
	trigger,due:=automationReachableQualityOptimizationDue(now.Add(90*time.Second),bad)
	if !due || !trigger.MediaDegraded || trigger.ServiceBadMask!=0 || trigger.LatencyDegraded {
		t.Fatalf("expired media quiet blocked a persistent incident: trigger=%+v due=%v",trigger,due)
	}
	failed:=classifyAutomationApplicationFailure(false,true)
	if got:=classifyAutomationHealth(failed,true,failed);got.State!=automationHealthCritical {
		t.Fatalf("media-only settling interfered with WAN-confirmed VPN outage recovery: %+v",got)
	}
}
