package main

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func autoMediaSample(mbps float64, ttfb time.Duration) automationMediaSegment {
	return automationMediaSegment{
		Valid:true, Mbps:mbps, TTFB:ttfb,
		Degraded:mbps < automationMediaSlowMbps || ttfb >= automationMediaHighTTFB,
	}
}

func autoMediaHealth(identity string, bad, known bool) automationHealthProbe {
	p := automationHealthProbe{
		State:automationHealthHealthy, CurrentIdentity:identity,
		ServiceComplete:true, ServiceOKMask:bestServerServiceAllMask,
	}
	if !known { return classifyAutomationMediaQuality(p, automationMediaEvidence{}) }
	return classifyAutomationMediaQuality(p, automationMediaEvidence{
		Complete:true, Samples:automationMediaSegmentCount, Degraded:bad, MinMbps:2,
	})
}

func TestAutoMediaStrictHTTPAndCompletedTransfer(t *testing.T) {
	for _, tc := range []struct{
		name, output string
		valid,bad bool
	}{
		{"fast",fmt.Sprintf("200 %d 0.100 0.180 0.320",automationMediaSegmentBytes),true,false},
		{"slow",fmt.Sprintf("200 %d 0.100 0.200 1.800",automationMediaSegmentBytes),true,true},
		{"high TTFB",fmt.Sprintf("200 %d 0.100 1.200 1.400",automationMediaSegmentBytes),true,true},
		{"HTTP403","403 0 0.100 0.200 0.300",false,false},
		{"HTTP429","429 0 0.100 0.200 0.300",false,false},
		{"HTTP503","503 0 0.100 0.200 0.300",false,false},
		{"redirect",fmt.Sprintf("302 %d 0.100 0.200 0.300",automationMediaSegmentBytes),false,false},
		{"partial",fmt.Sprintf("206 %d 0.100 0.200 0.300",automationMediaSegmentBytes),false,false},
		{"truncated",fmt.Sprintf("200 %d 0.100 0.200 0.300",automationMediaSegmentBytes/2),false,false},
		{"wrong size",fmt.Sprintf("200 %d 0.100 0.200 0.300",automationMediaSegmentBytes*2),false,false},
		{"negative time",fmt.Sprintf("200 %d -0.100 0.200 0.300",automationMediaSegmentBytes),false,false},
		{"reverse time",fmt.Sprintf("200 %d 0.300 0.200 0.400",automationMediaSegmentBytes),false,false},
		{"NaN",fmt.Sprintf("200 %d NaN 0.200 0.300",automationMediaSegmentBytes),false,false},
		{"Inf",fmt.Sprintf("200 %d 0.100 +Inf 0.300",automationMediaSegmentBytes),false,false},
		{"extra columns",fmt.Sprintf("200 %d 0.100 0.200 0.300 bad",automationMediaSegmentBytes),false,false},
	}{
		t.Run(tc.name,func(t *testing.T){
			got:=parseAutomationMediaSegment(tc.output,automationMediaSegmentBytes)
			if got.Valid!=tc.valid || got.Degraded!=tc.bad {
				t.Fatalf("media response %q incorrectly classified: %+v",tc.output,got)
			}
		})
	}
}

func TestAutoMediaPairedSegmentsRequireTwoValidConsistentSamples(t *testing.T) {
	good, bad:=autoMediaSample(30,100*time.Millisecond),autoMediaSample(2,time.Second)
	for _,tc:=range []struct{
		name string
		samples []automationMediaSegment
		complete,degraded bool
		calls int
	}{
		{"two fast",[]automationMediaSegment{good,good},true,false,2},
		{"two stalled",[]automationMediaSegment{bad,bad},true,true,2},
		{"one dip",[]automationMediaSegment{bad,good},false,false,2},
		{"second dip",[]automationMediaSegment{good,bad},false,false,2},
		{"first unknown",[]automationMediaSegment{{},bad},false,false,1},
		{"second unknown",[]automationMediaSegment{bad,{}},false,false,2},
	}{
		t.Run(tc.name,func(t *testing.T){
			calls:=0
			runner:=func(_ context.Context,curl,socks string,index int) automationMediaSegment {
				if curl!="curl" || socks!="127.0.0.1:42391" || index!=calls {
					t.Fatalf("media test lost exact isolated SOCKS source: %s %s %d",curl,socks,index)
				}
				calls++
				return tc.samples[index]
			}
			got:=probeAutomationMediaSegmentsWith(context.Background(),"curl","127.0.0.1:42391",runner)
			if got.Complete!=tc.complete || got.Degraded!=tc.degraded || calls!=tc.calls {
				t.Fatalf("got=%+v calls=%d expected complete=%v bad=%v calls=%d",got,calls,tc.complete,tc.degraded,tc.calls)
			}
		})
	}
	ctx,cancel:=context.WithCancel(context.Background());cancel()
	calls:=0
	got:=probeAutomationMediaSegmentsWith(ctx,"curl","127.0.0.1:42391",func(context.Context,string,string,int)automationMediaSegment{calls++;return good})
	if got.Complete || calls!=0 {t.Fatalf("cancelled media runner gave evidence: %+v calls=%d",got,calls)}
}

func TestAutoMediaHysteresisAndIdentity(t *testing.T) {
	identity:=bestServerCurrentQualityIdentityHash("media.example.invalid:443","profile-one")
	other:=bestServerCurrentQualityIdentityHash("media.example.invalid:444","profile-one")
	now:=time.Date(2026,10,28,0,0,0,0,time.UTC)
	bad:=autoMediaHealth(identity,true,true)
	if bad.State!=automationHealthUncertain || !bad.QualityDegraded || !bad.MediaDegraded || !bad.MediaComplete{
		t.Fatalf("media stalls not classified as quality-only: %+v",bad)
	}
	if got:=classifyAutomationHealth(bad,true,bad);got.State==automationHealthCritical{
		t.Fatal("media quality triggered global VPN emergency")
	}
	state:=map[string]string{}
	step:=func(at time.Time,p automationHealthProbe)(automationQualityTrigger,bool){
		updates,trigger,due:=automationReachableQualityOptimizationPlan(state,at,p)
		for k,v:=range updates{state[k]=v}
		return trigger,due
	}
	tr,due:=step(now,bad)
	if due || tr.MediaDegraded {t.Fatal("single media observation triggered expensive scan")}
	tr,due=step(now.Add(30*time.Second),bad)
	if !due || !tr.MediaDegraded || tr.LatencyDegraded || tr.ServiceBadMask!=0 || tr.CurrentIdentity!=identity{
		t.Fatalf("persistent media trigger lost: %+v due=%v",tr,due)
	}
	if state["QUALITY_MEDIA_COUNT"]!="0" || state["QUALITY_OPTIMIZATION_LAST"]!=now.Add(30*time.Second).Format(time.RFC3339){
		t.Fatalf("shared optimization reservation missing: %v",state)
	}
	for i:=1;i<9;i++{if _,due:=step(now.Add(30*time.Second+time.Duration(i)*time.Minute),bad);due{
		t.Fatal("media bypassed bounded retry cooldown")
	}}
	state=map[string]string{}
	step(now,bad)
	step(now.Add(10*time.Second),autoMediaHealth(identity,false,false))
	if state["QUALITY_MEDIA_COUNT"]!="1"{t.Fatal("unknown media erased or advanced a verified stall")}
	// This second valid observation is still inside the 30s minimum span:
	// it must be counted but must NOT start a comparison yet.
	tr,due=step(now.Add(20*time.Second),bad)
	if due || tr.MediaDegraded || state["QUALITY_MEDIA_COUNT"]!="2"{
		t.Fatal("stalled evidence was not preserved without premature scan")
	}
	step(now.Add(50*time.Second),autoMediaHealth(identity,false,true))
	if state["QUALITY_MEDIA_COUNT"]!="0"{t.Fatal("healthy media did not clear stale evidence")}
	step(now.Add(60*time.Second),bad)
	step(now.Add(70*time.Second),autoMediaHealth(other,true,true))
	if state["QUALITY_MEDIA_COUNT"]!="1" || state["QUALITY_MEDIA_IDENTITY"]!=other{
		t.Fatal("two different VPN endpoints shared a stall streak")
	}
}

func TestAutoMediaRejectsReplayCorruptCooldownAndIncompleteServices(t *testing.T){
	id:=strings.Repeat("a",64)
	now:=time.Date(2026,10,28,0,0,0,0,time.UTC)
	bad:=autoMediaHealth(id,true,true)
	for _,tc:=range []struct{name string;mutate func(map[string]string)}{
		{"invalid count",func(s map[string]string){s["QUALITY_MEDIA_COUNT"]="999"}},
		{"invalid last",func(s map[string]string){s["QUALITY_MEDIA_LAST"]="broken"}},
		{"expired",func(s map[string]string){s["QUALITY_MEDIA_SINCE"]=now.Add(-21*time.Minute).Format(time.RFC3339Nano)}},
		{"future last",func(s map[string]string){s["QUALITY_MEDIA_LAST"]=now.Add(time.Minute).Format(time.RFC3339Nano)}},
		{"corrupt cooldown",func(s map[string]string){s["QUALITY_OPTIMIZATION_LAST"]="bad-time"}},
	}{
		t.Run(tc.name,func(t *testing.T){
			state:=map[string]string{
				"QUALITY_MEDIA_IDENTITY":id,"QUALITY_MEDIA_COUNT":"1",
				"QUALITY_MEDIA_SINCE":now.Add(-2*time.Minute).Format(time.RFC3339Nano),
				"QUALITY_MEDIA_LAST":now.Add(-time.Minute).Format(time.RFC3339Nano),
			}
			tc.mutate(state)
			if changes,_,due:=automationReachableQualityOptimizationPlan(state,now,bad);due{
				t.Fatalf("invalid state authorized scan: %v",changes)
			}
		})
	}
	state:=map[string]string{}
	first,_,due:=automationReachableQualityOptimizationPlan(state,now,bad)
	for k,v:=range first{state[k]=v}
	if due || state["QUALITY_MEDIA_COUNT"]!="1"{t.Fatal("single media sample started scan")}
	for i:=0;i<10;i++{
		if _,_,due=automationReachableQualityOptimizationPlan(state,now,bad);due{
			t.Fatal("duplicate probe timestamp authorized scan")
		}
	}
	// A restricted HEAD response is UNKNOWN, not a reason to ignore actual
	// downloads. Confirmed media evidence must remain independent.
	headRestricted:=bad
	headRestricted.ServiceComplete=false
	headRestricted.ServiceOKMask=0
	headRestricted.ServiceBadMask=0
	changes,tr,mediaDue:=automationReachableQualityOptimizationPlan(state,now.Add(time.Minute),headRestricted)
	if !mediaDue || !tr.MediaDegraded || tr.ServiceBadMask!=0 {
		t.Fatalf("false HEAD restriction blocked confirmed media damage: %+v %+v %t",changes,tr,mediaDue)
	}
	// Missing media evidence is different: no automatic candidate scan.
	noMedia:=bad; noMedia.MediaComplete=false
	state=map[string]string{}
	for i:=0;i<5;i++{
		if _,_,due=automationReachableQualityOptimizationPlan(state,now.Add(time.Duration(i)*time.Minute),noMedia);due{
			t.Fatal("incomplete media observations authorized an automatic scan")
		}
	}
}

func TestAutoMediaIntentBypassesOnlyStaleHealthyDeepScan(t *testing.T){
	oldRefresh,oldCurrent,oldForeign:=automationBestCurrentRefresh,automationBestCurrentQuality,automationMediaForeignScan
	t.Cleanup(func(){automationBestCurrentRefresh,automationBestCurrentQuality,automationMediaForeignScan=oldRefresh,oldCurrent,oldForeign})
	a:=automationQoECooldownFixture(t,time.Hour,"auto")
	sentinel:=errors.New("reached bounded comparison")
	currentCalls,foreignCalls:=0,0
	automationBestCurrentRefresh=func(*app,context.Context)(int,bestServerRefreshResponse){
		t.Fatal("media-only comparison attempted endpoint update")
		return 0,bestServerRefreshResponse{}
	}
	automationBestCurrentQuality=func(*app,context.Context)(bestServerQualityResponse,error){
		currentCalls++
		c:=automationMeasuredCurrentFixture()
		// In real lag episodes a deep 40 MiB test or HEAD can be incomplete;
		// a fully identified, HTTPS-reachable current route still needs a
		// media-only comparison instead of a generic "uncertain" dead end.
		c.Eligible=false
		c.MediaSamples=0
		c.ServiceTotal=0
		return bestServerQualityResponse{
			CurrentEndpoint:c.Endpoint,
			Candidates:[]bestServerQualityCandidate{c},
			CurrentIdentity:bestServerCurrentQualityIdentityHash(c.Endpoint,"de Fixture"),
		},nil
	}
	automationMediaForeignScan=func(_ *app,_ context.Context,settings automationSettings,_ string)(bestServerQualityResponse,error){
		foreignCalls++
		if settings.Policy!=automationPolicyBetter || settings.AutoEndpointUpdate{t.Fatal("media-only triggered endpoint refresh")}
		return bestServerQualityResponse{},sentinel
	}
	settings:=automationSettings{Enabled:true,Mode:automationModeBest,Policy:automationPolicyDegraded,AutoApply:true,AutoEndpointUpdate:true}
	trigger:=automationQualityTrigger{
		CurrentIdentity:bestServerCurrentQualityIdentityHash("192.0.2.10:443","de Fixture"),
		MediaDegraded:true,
	}
	result,err:=a.runAutomationQualityCycle(context.Background(),settings,trigger)
	if !errors.Is(err,sentinel) || result.Mutated || currentCalls!=1 || foreignCalls!=1{
		t.Fatalf("persistent media stall hidden behind older healthy deep scan: result=%+v calls=%d/%d err=%v",result,currentCalls,foreignCalls,err)
	}
}

func TestAutoMediaCandidateCanBeVerifiedWithoutAllHEADResponses(t *testing.T){
	raw:=strings.Split(strings.TrimSpace(testSubscriptionPlain),"\n")[0]
	profile,ok:=parseSafeVLESSProfile(raw)
	if !ok {t.Fatal("test profile invalid")}
	c:=bestServerQualityCandidate{
		ID:profile.ID, Tested:true, Available:true, Reachable:true, Current:false,
		ApplicationMS:145, HTTPSamples:bestServerQualityHTTPRuns, JitterMS:15,
		DownloadMbps:45, MediaMbps:38, MediaSamples:bestServerMediaRequiredRuns,
		MediaStalls:0, MediaGrade:"poor", ServiceOK:1, ServiceTotal:4,
	}
	if c.Eligible || !automationMediaMeasuredCandidateEligible(c) {
		t.Fatal("strict HEAD failed to admit an independently verified stream candidate")
	}
	for _,tc:=range []struct{name string;change func(*bestServerQualityCandidate)}{
		{"not measured",func(x *bestServerQualityCandidate){x.Tested=false}},
		{"no HTTPS",func(x *bestServerQualityCandidate){x.HTTPSamples=0}},
		{"low transfer",func(x *bestServerQualityCandidate){x.MediaMbps=3}},
		{"stalled",func(x *bestServerQualityCandidate){x.MediaStalls=1}},
		{"high RTT",func(x *bestServerQualityCandidate){x.ApplicationMS=950}},
		{"high jitter",func(x *bestServerQualityCandidate){x.JitterMS=105}},
		{"not enough samples",func(x *bestServerQualityCandidate){x.MediaSamples=0}},
		{"no download",func(x *bestServerQualityCandidate){x.DownloadMbps=0}},
		{"not reachable",func(x *bestServerQualityCandidate){x.Available=false}},
	}{
		t.Run(tc.name,func(t *testing.T){
			copy:=c;tc.change(&copy)
			if automationMediaMeasuredCandidateEligible(copy) {
				t.Fatalf("unverified media alternative became eligible: %+v",copy)
			}
		})
	}
}

func TestAutoMediaManualHeavyScanDoesNotDisableWatchdog(t *testing.T) {
	dir:=t.TempDir()
	config:=filepath.Join(dir,"freenet.conf")
	state:=filepath.Join(dir,"watch.state")
	t.Setenv("FREENET_SETTINGS_V3_STATE",state)
	if err:=os.WriteFile(config,[]byte("AUTO_VPN_V1=yes\nAUTO_VPN_V1_INTERVAL=manual\nAUTO_VPN_MODE=best\nAUTO_VPN_HEALTH_INTERVAL=1m\n"),0600);err!=nil{t.Fatal(err)}
	if err:=os.WriteFile(state,[]byte("HEALTH_SCHEDULE_LAST=2026-10-27T12:00:00Z\n"),0600);err!=nil{t.Fatal(err)}
	if !automationHealthDue(config,time.Date(2026,10,27,12,1,0,0,time.UTC)){
		t.Fatal("manual heavy scan disabled independent health watchdog")
	}
	if got:=readAutomationSettings(config);!got.Enabled || got.Mode!=automationModeBest || got.Interval!="manual"{
		t.Fatalf("manual mode lost independent health settings: %+v",got)
	}
	if automationMediaSegmentBytes*automationMediaSegmentCount>1<<20 || automationMediaSegmentTimeout>3*time.Second{
		t.Fatal("media watchdog unbounded bandwidth or time")
	}
}

func TestAutoMediaTimedOutPartialBodyCountsOnlyVerifiedStall(t *testing.T) {
	// Build an actual curl-like process exit status 28 without contacting the network.
	// The parser accepts only exit code 28, so all other errors remain UNKNOWN.
	command := exec.Command("sh", "-c", "exit 28")
	err := command.Run()
	if err == nil { t.Fatal("expected exit 28") }
	tests := []struct {
		name string
		output string
		err error
		valid bool
	}{
		{"verified stalled HTTP200", fmt.Sprintf("200 65536 0.100 0.200 3.000"), err, true},
		{"HTTP403", "403 65536 0.100 0.200 3.000", err, false},
		{"zero body", "200 0 0.100 0.200 3.000", err, false},
		{"tiny partial", "200 1000 0.100 0.200 3.000", err, false},
		{"unproven short transfer", "200 65536 0.100 0.200 0.300", err, false},
		{"completed body timeout", fmt.Sprintf("200 %d 0.100 0.200 3.000", automationMediaSegmentBytes), err, false},
		{"other error", "200 65536 0.100 0.200 3.000", errors.New("network unavailable"), false},
		{"missing metrics", "200 65536", err, false},
		{"NaN metrics", "200 65536 NaN 0.200 3.000", err, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := parseAutomationTimedOutMediaSegment(tc.output, tc.err, automationMediaSegmentBytes)
			if got.Valid != tc.valid || (tc.valid && !got.Degraded) {
				t.Fatalf("invalid timeout classification: %+v want valid=%v", got, tc.valid)
			}
		})
	}
}
