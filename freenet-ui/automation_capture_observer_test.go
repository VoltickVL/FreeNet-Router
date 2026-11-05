package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestAutoCapturePathObservesBoundedStateWithoutSwitch(t *testing.T) {
	stateFile := filepath.Join(t.TempDir(),"watchdog.state")
	t.Setenv("FREENET_SETTINGS_V3_STATE",stateFile)
	now := time.Now().UTC()
	var calls int
	inspect := func(context.Context) captureDoctorResponse {
		calls++
		return captureDoctorResponse{
			State:"installed-unverified", TCPPrerouting:"tcp-prerouting-hook-present",
			Netfilter:"tproxy-rule-present", Hook:"do-not-export-private-identifier",
			Reason:"do-not-export-private-endpoint",
		}
	}
	a := &app{}
	a.observeAutomationCapturePath(context.Background(),now,inspect)
	raw,err:=os.ReadFile(stateFile);if err!=nil{t.Fatal(err)}
	if strings.Contains(string(raw),"do-not-export-private") {
		t.Fatalf("raw rule detail escaped fixed enums: %s",raw)
	}
	result:=automationCapturePathFromState(v3ParseState(stateFile),time.Now().UTC())
	if result.State!="installed-unverified" || result.TCP!="tcp-prerouting-hook-present" ||
		result.UDP!="tproxy-rule-present" || result.ObservedAt=="" {
		t.Fatalf("missing safe recorded result: %+v",result)
	}
	a.observeAutomationCapturePath(context.Background(),now.Add(time.Minute),inspect)
	if calls!=1 {t.Fatalf("early second probe occurred: %d",calls)}
	a.observeAutomationCapturePath(context.Background(),now.Add(automationCapturePathInterval+time.Second),
		func(context.Context) captureDoctorResponse {
			calls++
			return captureDoctorResponse{State:"tcp-hook-missing",
				TCPPrerouting:"tcp-prerouting-hook-absent",Netfilter:"tproxy-rule-present"}
		})
	if calls!=2 {t.Fatalf("due probe missing: %d",calls)}
	state:=v3ParseState(stateFile)
	if state["CLIENT_CAPTURE_STATE"]!="tcp-hook-missing" {
		t.Fatalf("capture risk was not recorded: %v",state["CLIENT_CAPTURE_STATE"])
	}
	// Data collection is deliberately independent of HEALTH_RESULT/endpoint.
	if state["HEALTH_RESULT"]!="" || state["LAST_SWITCH"]!="" {
		t.Fatal("observer changed AUTO decision/mutation state")
	}
}

func TestAutoCapturePathUnknownAndFreshnessFailClosed(t *testing.T){
	now:=time.Now().UTC()
	for _,tc:=range []struct{value string;want bool}{
		{"",true},
		{"malformed",false},
		{now.Add(time.Minute).Format(time.RFC3339Nano),false},
		{now.Add(-time.Minute).Format(time.RFC3339Nano),false},
		{now.Add(-6*time.Minute).Format(time.RFC3339Nano),true},
	}{
		if got:=automationCapturePathDue(tc.value,now);got!=tc.want{
			t.Fatalf("due raw=%q got=%t want=%t",tc.value,got,tc.want)
		}
	}
	for _,tc:=range []struct{name string;state map[string]string}{
		{"no-state",map[string]string{}},
		{"stale",map[string]string{
			"CLIENT_CAPTURE_LAST":now.Add(-11*time.Minute).Format(time.RFC3339Nano),
			"CLIENT_CAPTURE_STATE":"installed-unverified"}},
		{"future",map[string]string{
			"CLIENT_CAPTURE_LAST":now.Add(2*time.Minute).Format(time.RFC3339Nano),
			"CLIENT_CAPTURE_STATE":"installed-unverified"}},
	}{
		t.Run(tc.name,func(t *testing.T){
			observed:=automationCapturePathFromState(tc.state,now)
			if observed.State!="unverified"||observed.TCP!="unknown"||
				observed.UDP!="unknown"||observed.ObservedAt!="" {
				t.Fatalf("untrusted stale state accepted: %+v",observed)
			}
		})
	}
	state:=automationCapturePathFromState(map[string]string{
		"CLIENT_CAPTURE_LAST":now.Format(time.RFC3339Nano),
		"CLIENT_CAPTURE_STATE":"SECRET.private.example",
		"CLIENT_CAPTURE_TCP":"tcp-prerouting-hook-present;uuid",
		"CLIENT_CAPTURE_UDP":"tproxy-rule-present?token"},now)
	out,err:=json.Marshal(state);if err!=nil{t.Fatal(err)}
	if strings.Contains(string(out),"SECRET")||strings.Contains(string(out),"uuid")||
		strings.Contains(string(out),"token")||state.State!="unverified"{
		t.Fatalf("unsupported enum was exported: %s",out)
	}
}

func TestAutoCapturePathConcurrentReservation(t *testing.T){
	t.Setenv("FREENET_SETTINGS_V3_STATE",filepath.Join(t.TempDir(),"state"))
	a:=&app{}
	now:=time.Now().UTC()
	var count atomic.Int32
	var wg sync.WaitGroup
	inspect:=func(context.Context) captureDoctorResponse {
		count.Add(1)
		time.Sleep(30*time.Millisecond)
		return captureDoctorResponse{State:"installed-unverified"}
	}
	for i:=0;i<6;i++{
		wg.Add(1)
		go func(){defer wg.Done();a.observeAutomationCapturePath(context.Background(),now,inspect)}()
	}
	wg.Wait()
	if got:=count.Load();got!=1 {t.Fatalf("duplicate scheduled capture passes: %d",got)}
	cancelled,cancel:=context.WithCancel(context.Background())
	cancel()
	a.observeAutomationCapturePath(cancelled,now.Add(10*time.Minute),inspect)
	if count.Load()!=1 {t.Fatal("cancelled capture still ran")}
}

func TestAutoCapturePathDoesNotTrustInspectionPayload(t *testing.T){
	t.Setenv("FREENET_SETTINGS_V3_STATE",filepath.Join(t.TempDir(),"state"))
	a:=&app{}
	a.observeAutomationCapturePath(context.Background(),time.Now().UTC(),func(context.Context) captureDoctorResponse {
		return captureDoctorResponse{
			State:"token=unsafe",TCPPrerouting:"tcp-prerouting-hook-present/SECRET",
			Netfilter:"tproxy-rule-present/SECRET",Hook:"vless://private",Reason:"uuid=private",
		}
	})
	data,err:=os.ReadFile(settingsV3StatePath());if err!=nil{t.Fatal(err)}
	if strings.Contains(string(data),"SECRET")||strings.Contains(string(data),"vless://")||
		strings.Contains(string(data),"uuid=")||strings.Contains(string(data),"token=unsafe"){
		t.Fatalf("untrusted kernel data persisted: %s",data)
	}
	if got:=v3ParseState(settingsV3StatePath())["CLIENT_CAPTURE_STATE"];got!="unverified"{
		t.Fatalf("unknown capture state accepted: %s",got)
	}
}

func TestAutoCaptureModeAwareFailClosed(t *testing.T) {
	for _,tc:=range []struct{
		name string
		value captureDoctorResponse
		want string
	}{
		{"tproxy-tcp-without-nat-redirect-possible",
			captureDoctorResponse{State:"tcp-redirect-missing",Netfilter:"tproxy-rule-present"},
			"unverified"},
		{"tproxy-tcp-with-unknown-netfilter",
			captureDoctorResponse{State:"tcp-redirect-missing",Netfilter:"unknown"},
			"unverified"},
		{"tcp-only-redirect-is-possible",
			captureDoctorResponse{State:"capture-rule-missing",TCPRedirect:"tcp-redirect-present",
				TCPPrerouting:"tcp-prerouting-hook-present",Netfilter:"tproxy-rule-absent"},
			"unverified"},
		{"hybrid-missing-normal-tcp-hook",
			captureDoctorResponse{State:"tcp-hook-missing",TCPRedirect:"tcp-redirect-present",
				TCPPrerouting:"tcp-prerouting-hook-absent",Netfilter:"tproxy-rule-present"},
			"tcp-hook-missing"},
		{"unknown-rule-is-unknown",
			captureDoctorResponse{State:"secret=http://sensitive.invalid"},
			"unverified"},
		{"hybrid-rules-not-playback",
			captureDoctorResponse{State:"installed-unverified",TCPRedirect:"tcp-redirect-present",
				TCPPrerouting:"tcp-prerouting-hook-present",Netfilter:"tproxy-rule-present"},
			"installed-unverified"},
	} {
		t.Run(tc.name,func(t *testing.T){
			if got:=automationCaptureObservedState(tc.value);got!=tc.want{
				t.Fatalf("observed=%q want=%q",got,tc.want)
			}
		})
	}
}
