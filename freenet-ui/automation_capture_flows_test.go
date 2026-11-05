package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func testCaptureHookNAT(packets uint64) string {
	return fmt.Sprintf("*nat\n"+
		"-A PREROUTING -p tcp -m conntrack ! --ctstate INVALID -m dscp --dscp 0x3f -j xkeen -c 900 90000\n"+
		"-A PREROUTING -p tcp -m conntrack ! --ctstate INVALID -m multiport --dports 80,443 -j xkeen -c %d 4000\n"+
		"-A PREROUTING -p tcp -j other-chain -c 777 77700\nCOMMIT\n",packets)
}

func testCaptureHookMangle(tcp,udp uint64) string {
	return fmt.Sprintf("*mangle\n"+
		"-A PREROUTING -p udp -m dscp --dscp 0x3f -j xkeen -c 888 88800\n"+
		"-A PREROUTING -p udp -m multiport --dports 80,443 -j xkeen -c %d 4000\n"+
		"-A PREROUTING -p tcp -j xkeen -c %d 4000\n"+
		"-A xkeen -p udp -j TPROXY --on-port 1234 -c 999 99900\nCOMMIT\n",udp,tcp)
}

func testCaptureHookReader(nat,mangle string,callCount *atomic.Int32) func(context.Context,string,...string)(string,bool) {
	return func(ctx context.Context, command string,args ...string)(string,bool) {
		if callCount != nil {callCount.Add(1)}
		if ctx.Err()!=nil || command!="iptables-save" || len(args)!=3 ||
			args[0]!="-c" || args[1]!="-t" {return "",false}
		switch args[2] {
		case "nat":return nat,true
		case "mangle":return mangle,true
		}
		return "",false
	}
}

func TestCaptureFlowParsesOnlyGeneralLANHooks(t *testing.T) {
	reader:=testCaptureHookReader(testCaptureHookNAT(12),testCaptureHookMangle(5,9),nil)
	s:=readAutomationCaptureFlows(context.Background(),reader)
	if !s.tcpValid||!s.udpValid||s.tcpPackets!=17||s.udpPackets!=9||
		s.tcpRules!=2||s.udpRules!=1 {
		t.Fatalf("wrong bounded counter aggregation: %+v",s)
	}
	// The DSCP-only path must never be counted as general LAN traffic.
	if tcp,udp,_,_,ok:=automationCaptureFlowCount(testCaptureHookMangle(0,0),"mangle");
		!ok || tcp!=0 || udp!=0 {
		t.Fatalf("special-only counters were counted tcp=%d udp=%d ok=%t",tcp,udp,ok)
	}
}

func TestCaptureFlowCounterInputFailClosed(t *testing.T) {
	for _,tc:=range []struct{name,raw string}{
		{"no-table","-A PREROUTING -p tcp -j xkeen -c 1 2\nCOMMIT\n"},
		{"no-commit","*nat\n-A PREROUTING -p tcp -j xkeen -c 1 2\n"},
		{"no-counters","*nat\n-A PREROUTING -p tcp -j xkeen\nCOMMIT\n"},
		{"bad-packets","*nat\n-A PREROUTING -p tcp -j xkeen -c bad 12\nCOMMIT\n"},
		{"bad-bytes","*nat\n-A PREROUTING -p tcp -j xkeen -c 1 bad\nCOMMIT\n"},
		{"negative","*nat\n-A PREROUTING -p tcp -j xkeen -c -1 20\nCOMMIT\n"},
		{"overflow","*nat\n-A PREROUTING -p tcp -j xkeen -c 18446744073709551615 20\nCOMMIT\n"},
		{"multiple-overflow",fmt.Sprintf("*nat\n-A PREROUTING -p tcp -j xkeen -c %d 1\n-A PREROUTING -p tcp -j xkeen -c 1 1\nCOMMIT\n",automationCaptureFlowMaxPackets)},
		{"truncated",strings.Repeat("X",captureDoctorMaxOutput+1)},
	} {
		t.Run(tc.name,func(t *testing.T){
			_,_,_,_,valid:=automationCaptureFlowCount(tc.raw,"nat")
			if valid {t.Fatalf("invalid kernel output accepted: %s",tc.name)}
		})
	}
	reader:=testCaptureHookReader(testCaptureHookNAT(7),"",nil)
	if s:=readAutomationCaptureFlows(context.Background(),reader);s.tcpValid||s.udpValid {
		t.Fatalf("partial tables must be UNKNOWN: %+v",s)
	}
	cancelled,cancel:=context.WithCancel(context.Background());cancel()
	if s:=readAutomationCaptureFlows(cancelled,reader);s.tcpValid||s.udpValid {
		t.Fatalf("cancelled observation must be UNKNOWN: %+v",s)
	}
}

func TestCaptureFlowDeltaBaselineIncreaseIdleAndReset(t *testing.T) {
	now:=time.Now().UTC()
	state:=map[string]string{}
	apply:=func(s automationCaptureFlowSnapshot,at time.Time) automationCaptureFlowStatus {
		for key,value:=range automationCaptureFlowSave(state,s,at){state[key]=value}
		return automationCaptureFlowFromState(state,at)
	}
	base:=automationCaptureFlowSnapshot{tcpPackets:10,udpPackets:20,tcpRules:1,udpRules:1,tcpValid:true,udpValid:true}
	if got:=apply(base,now);got.TCP!="baseline"||got.UDP!="baseline"{
		t.Fatalf("first sample claimed measured activity: %+v",got)
	}
	next:=automationCaptureFlowSnapshot{tcpPackets:12,udpPackets:20,tcpRules:1,udpRules:1,tcpValid:true,udpValid:true}
	if got:=apply(next,now.Add(time.Minute));got.TCP!="observed"||got.UDP!="no-increase" {
		t.Fatalf("invalid packet delta: %+v",got)
	}
	if got:=apply(next,now.Add(2*time.Minute));got.TCP!="no-increase"||got.UDP!="no-increase" {
		t.Fatalf("idle counters must not imply no clients: %+v",got)
	}
	reset:=automationCaptureFlowSnapshot{tcpPackets:2,udpPackets:3,tcpRules:1,udpRules:1,tcpValid:true,udpValid:true}
	if got:=apply(reset,now.Add(3*time.Minute));got.TCP!="baseline"||got.UDP!="baseline" {
		t.Fatalf("reset must not produce false observed: %+v",got)
	}
	reshaped:=automationCaptureFlowSnapshot{tcpPackets:500,udpPackets:100,tcpRules:2,udpRules:1,tcpValid:true,udpValid:true}
	if got:=apply(reshaped,now.Add(4*time.Minute));got.TCP!="baseline"||got.UDP!="baseline"{
		t.Fatalf("new rule count must reset comparison: %+v",got)
	}
	unknown:=automationCaptureFlowSnapshot{}
	if got:=apply(unknown,now.Add(5*time.Minute));got.TCP!="unknown"||got.UDP!="unknown"{
		t.Fatalf("missing rules must be UNKNOWN: %+v",got)
	}
	if got:=apply(next,now.Add(6*time.Minute));got.TCP!="baseline"||got.UDP!="baseline"{
		t.Fatalf("sample after unknown requires a fresh baseline: %+v",got)
	}
	if got:=apply(next,now.Add(20*time.Minute));got.TCP!="baseline"||got.UDP!="baseline"{
		t.Fatalf("stale baseline must not be compared: %+v",got)
	}
}

func TestCaptureFlowStatusDoesNotLeakCountersOrIdentity(t *testing.T) {
	now:=time.Now().UTC()
	raw:=map[string]string{
		"CLIENT_FLOW_OBSERVED_AT":now.Format(time.RFC3339Nano),
		"CLIENT_FLOW_TCP_RESULT":"observed",
		"CLIENT_FLOW_UDP_RESULT":"no-increase",
		"CLIENT_FLOW_TCP_PACKETS":"99998888",
		"CLIENT_FLOW_RULE_SECRET":"private-uuid.example",
	}
	s:=automationCaptureFlowFromState(raw,now)
	out,err:=json.Marshal(s);if err!=nil{t.Fatal(err)}
	if s.TCP!="observed"||s.UDP!="no-increase"||
		strings.Contains(string(out),"99998888")||strings.Contains(string(out),"private-uuid"){
		t.Fatalf("raw counter or secret exported: %s",out)
	}
	for _,bad:=range []string{"observed;uuid=secret","bad",""}{
		raw["CLIENT_FLOW_TCP_RESULT"]=bad
		if got:=automationCaptureFlowFromState(raw,now);got.TCP!="unknown" {
			t.Fatalf("untrusted result accepted: %+v",got)
		}
	}
	for _,shift:=range []time.Duration{-11*time.Minute,2*time.Minute}{
		raw["CLIENT_FLOW_OBSERVED_AT"]=now.Add(shift).Format(time.RFC3339Nano)
		if got:=automationCaptureFlowFromState(raw,now);got.TCP!="unknown"||
			got.UDP!="unknown"||got.ObservedAt!="" {
			t.Fatalf("stale/future counter result accepted: %+v",got)
		}
	}
}

func TestScheduledCaptureSamplesExistingKernelHooksWithoutMutation(t *testing.T) {
	path:=filepath.Join(t.TempDir(),"freenet.state")
	t.Setenv("FREENET_SETTINGS_V3_STATE",path)
	a:=&app{}
	now:=time.Now().UTC()
	var calls atomic.Int32
	inspect:=func(context.Context) captureDoctorResponse{
		return captureDoctorResponse{State:"installed-unverified",
			TCPPrerouting:"tcp-prerouting-hook-present",Netfilter:"tproxy-rule-present"}
	}
	sample:=func(nat,udp uint64){
		reader:=testCaptureHookReader(testCaptureHookNAT(nat),testCaptureHookMangle(0,udp),&calls)
		a.observeAutomationCapturePathWithCounters(context.Background(),now,inspect,reader)
	}
	sample(4,7)
	before:=v3ParseState(path)
	if before["CLIENT_FLOW_TCP_RESULT"]!="baseline"||before["CLIENT_FLOW_UDP_RESULT"]!="baseline"{
		t.Fatalf("first watchdog must make baseline, got: %v %v",before["CLIENT_FLOW_TCP_RESULT"],before["CLIENT_FLOW_UDP_RESULT"])
	}
	// No extra shell commands when normal 5m observation is not due.
	sample(6,7)
	if calls.Load()!=2 {t.Fatalf("unexpected duplicate kernel reads %d",calls.Load())}
	now=now.Add(automationCapturePathInterval+time.Second)
	sample(6,10)
	state:=v3ParseState(path)
	if state["CLIENT_FLOW_TCP_RESULT"]!="observed" || state["CLIENT_FLOW_UDP_RESULT"]!="observed"{
		t.Fatalf("sampled client hook activity missing: %v / %v",state["CLIENT_FLOW_TCP_RESULT"],state["CLIENT_FLOW_UDP_RESULT"])
	}
	if state["HEALTH_RESULT"]!="" || state["LAST_SWITCH"]!="" || state["LAST_RUN"]!="" {
		t.Fatalf("read-only observer changed AUTO decision or switch state")
	}
	if calls.Load()!=4 {t.Fatalf("expected two bounded read passes, got %d",calls.Load())}
	body,err:=os.ReadFile(path);if err!=nil{t.Fatal(err)}
	if strings.Contains(string(body),"private-uuid") || strings.Contains(string(body),"--dports") {
		t.Fatalf("raw client rule stored: %s",body)
	}
	snapshot:=automationCapturePathFromState(state,time.Now().UTC())
	if snapshot.HookActivity.TCP!="observed"||snapshot.HookActivity.UDP!="observed" {
		t.Fatalf("Settings/Journal status not updated: %+v",snapshot.HookActivity)
	}
}
