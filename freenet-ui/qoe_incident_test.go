package main

import (
	"encoding/json"
	"os"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestQoEIncidentSnapshotNeverAssertsClientPlaybackOrLeaksRawState(t *testing.T) {
	now := time.Date(2026, 11, 4, 2, 45, 0, 0, time.UTC)
	secret := "VLESS-UUID-SECRETS-SUBSCRIPTION-URL"
	got := buildQoEIncidentSnapshot("instagram", "xkeen", captureDoctorResponse{
		State:"installed-unverified", TCPRedirect:"tcp-redirect-present",
		TCPPrerouting:"tcp-prerouting-hook-present", Netfilter:"tproxy-rule-present",
		PolicyRule:"fwmark-rule-present", LocalRoute:"local-default-present",
		LANExclude:"no-overlap", Reason:secret, Hook:secret,
	}, map[string]string{"HEALTH_RESULT":"healthy", "HEALTH_LAST":"2026-11-04T02:44:30Z", "HEALTH_MESSAGE":secret, "VPN_PROFILE":secret}, now)
	if got.Mutation != "NONE" || got.ClientEgress != "unknown" || got.Playback != "unknown" ||
		got.ClientTransport != "unknown" || got.DNSRuntime != "unknown" || got.CaptureState != "installed-unverified" {
		t.Fatalf("router readiness cannot become a client's playback claim: %+v", got)
	}
	if got.VPNHealthAt != "2026-11-04T02:44:30Z" || got.VPNHealth != "healthy" {
		t.Fatalf("expected only safe historical health fields: %+v", got)
	}
	raw, err := json.Marshal(got)
	if err != nil { t.Fatal(err) }
	if strings.Contains(string(raw), secret) || strings.Contains(string(raw), "endpoint") || strings.Contains(string(raw), "profile") {
		t.Fatalf("incident output leaked private network evidence: %s", raw)
	}
	if got.ObservedAt != now.Format(time.RFC3339) {t.Fatalf("timestamp=%q",got.ObservedAt)}
}

func TestQoEIncidentFailClosedWhenRouterStateMissingOrMalformed(t *testing.T) {
	now:=time.Date(2026,11,4,2,45,0,0,time.UTC)
	got:=buildQoEIncidentSnapshot("telegram","spoofed-dns",captureDoctorResponse{
		State:"healthy",Netfilter:"TPROXY "+ "SECRET",
	},map[string]string{
		"HEALTH_RESULT":"success; root", "HEALTH_LAST":"not-a-time",
	},now)
	if got.CaptureState!="unknown"||got.KernelTPROXY!="unknown"||got.DNSConfigured!="unknown"||
		got.VPNHealth!="unknown"||got.VPNHealthAt!=""||got.ClientEgress!="unknown" {
		t.Fatalf("untrusted/unavailable data must be UNKNOWN: %+v",got)
	}
	future:=buildQoEIncidentSnapshot("telegram","auto",captureDoctorResponse{},
		map[string]string{"HEALTH_LAST":"2026-11-05T02:45:00Z"},now)
	if future.VPNHealthAt!="" {t.Fatalf("future health must not look current: %+v",future)}
}

func TestQoEIncidentServiceAllowlist(t *testing.T) {
	for _, tc:=range []struct{raw,want string}{
		{"instagram","instagram"},{"Telegram","telegram"},{" insta ",""},{"https://www.instagram.com",""},
		{"../../../etc/passwd",""},{"",""},{"instagram?token=secret",""},
	}{
		if got:=qoeIncidentService(tc.raw);got!=tc.want {t.Errorf("service %q: %q want %q",tc.raw,got,tc.want)}
	}
}

func TestQoEIncidentAPIRequiresAuthAndOnlyGET(t *testing.T){
	a:=&app{sem:make(chan struct{},1)}
	mux:=http.NewServeMux()
	registerQoEIncident(mux,a)
	for _,tc:=range []struct{method,url string}{
		{http.MethodGet,"/api/qoe/incident?service=instagram"},
		{http.MethodPost,"/api/qoe/incident?service=instagram"},
	}{
		w:=httptest.NewRecorder()
		mux.ServeHTTP(w,httptest.NewRequest(tc.method,tc.url,nil))
		if tc.method==http.MethodGet && (w.Code==http.StatusOK||w.Code==http.StatusNotFound) {
			t.Fatalf("unauthenticated diagnosis must be rejected, got %d",w.Code)
		}
		if tc.method==http.MethodPost && w.Code!=http.StatusMethodNotAllowed {
			t.Fatalf("diagnostic POST not allowed, got %d",w.Code)
		}
	}
}

func TestQoEIncidentControlCenterUsesReadOnlyGETWithoutSuccessClaims(t *testing.T){
	data,err:=os.ReadFile("web/settings-v3-core.js")
	if err!=nil{t.Fatal(err)}
	s:=string(data)
	for _,fragment:=range []string{"fn3QoEService","fn3QoECapture","/api/qoe/incident?service=","client_egress","playback","fetchJSON("} {
		if !strings.Contains(s,fragment){t.Fatalf("QoE UI missing %q",fragment)}
	}
	if strings.Contains(s,"/api/qoe/incident',{method:'POST'") {t.Fatal("QoE button must be GET-only")}
}
