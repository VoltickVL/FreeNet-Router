package main

import (
 "context"
 "encoding/json"
 "net/http"
 "net/http/httptest"
 "strings"
 "testing"
)

func TestCaptureDoctorNoKernelTPROXYFailClosed(t *testing.T) {
 read:=func(_ context.Context,command string,args ...string)(string,bool){
  switch command{
  case "iptables-save":return "*mangle\n:PREROUTING ACCEPT [0:0]\nCOMMIT\n",true
  case "ip":
   if len(args)>1&&args[1]=="rule"{return "0: from all lookup local\n",true}
   return "local 127.0.0.0/8 dev lo table local",true
  }
  return "",false
 }
 got:=captureDoctorClassify(context.Background(),read,"modern-generated-hook")
 if got.State!="tcp-redirect-missing"||got.Netfilter!="tproxy-rule-absent"||got.Mutation!="NONE"{
  t.Fatalf("expected missing kernel hook despite valid file: %+v",got)
 }
}

func TestCaptureDoctorInstalledStillNotVerified(t *testing.T) {
 read:=func(_ context.Context,command string,args ...string)(string,bool){
  switch command {
  case "iptables-save":
   if len(args)>1&&args[1]=="nat"{return "-A PREROUTING -p tcp -j xkeen\n-A xkeen -p tcp -j REDIRECT --to-ports 5000\n",true}
   return "-A PREROUTING -p tcp -j TPROXY --on-port 12345 --tproxy-mark 0x1/0x1",true
  case "ip":
   if len(args)>1&&args[1]=="rule"{return "100: from all fwmark 0x1 lookup 100",true}
   return "local default dev lo table 100 scope host",true
  }
  return "",false
 }
 got:=captureDoctorClassify(context.Background(),read,"modern-generated-hook")
 if got.State!="installed-unverified"||got.Reason!="lan-packet-hits-not-measured" {t.Fatalf("cannot claim LAN VPN healthy: %+v",got)}
}

func TestCaptureDoctorNoSecretOrRawRules(t *testing.T) {
 secret:="VERY-PRIVATE-UUID-AND-ROUTER-CREDENTIAL"
 read:=func(_ context.Context,command string,args ...string)(string,bool){
  switch command {
  case "nft":return "chain prerouting { meta l4proto tcp tproxy to :1234 comment "+secret+" }",true
  case "ip":return "fwmark 0x1 lookup 100 "+secret,true
  }
  return "",false
 }
 got:=captureDoctorClassify(context.Background(),read,"modern-generated-hook")
 b,err:=json.Marshal(got);if err!=nil{t.Fatal(err)}
 if strings.Contains(string(b),secret)||strings.Contains(string(b),"1234") {t.Fatalf("raw kernel inventory leaked: %s",b)}
 if got.State=="healthy"{t.Fatal("cannot infer client egress from kernel rules")}
}

func TestCaptureDoctorAuthRequired(t *testing.T){
 a:=&app{sem:make(chan struct{},1)}
 mux:=http.NewServeMux();registerCaptureDoctor(mux,a)
 w:=httptest.NewRecorder()
 mux.ServeHTTP(w,httptest.NewRequest(http.MethodGet,"/api/routing/capture-doctor",nil))
 if w.Code==http.StatusOK||w.Code==http.StatusNotFound {t.Fatalf("capture doctor must require auth, got %d",w.Code)}
}

func TestCaptureDoctorNegatedInvalidIsNormalTCPHook(t *testing.T) {
	// Real XKeen/iptables-save syntax on an installed Keenetic:
	// "! --ctstate INVALID" explicitly allows non-INVALID connections.
	nat := "-A PREROUTING -p tcp -m connmark --mark 0x100 -m conntrack ! --ctstate INVALID -m dscp --dscp 0x3f -j xkeen\n" +
		"-A PREROUTING -p tcp -m connmark --mark 0x100 -m conntrack ! --ctstate INVALID -m multiport --dports 80,443 -j xkeen\n" +
		"-A xkeen -p tcp -j REDIRECT --to-ports 65432\n"
	redirect, hook := captureDoctorNATState(nat)
	if redirect != "tcp-redirect-present" || hook != "tcp-prerouting-hook-present" {
		t.Fatalf("normal TCP hook was misclassified: redirect=%s hook=%s", redirect, hook)
	}
	read := func(_ context.Context, binary string, args ...string) (string, bool) {
		switch binary {
		case "iptables-save":
			if len(args)>1 && args[1]=="nat" {return nat,true}
			return "-A PREROUTING -p udp -j TPROXY --on-port 65432",true
		case "ip":
			if len(args)>1 && args[1]=="rule" {return "100: from all fwmark 0x1 lookup 100",true}
			return "local default dev lo table 100",true
		}
		return "",false
	}
	result := captureDoctorClassify(context.Background(),read,"")
	if result.State != "installed-unverified" || result.TCPPrerouting != "tcp-prerouting-hook-present" || result.Mutation!="NONE" {
		t.Fatalf("normal hook must be present but not claim playback: %+v",result)
	}
}

func TestCaptureDoctorSpecialOnlyHooksRemainUnverified(t *testing.T) {
	for _,tc := range []struct{name,nat,want string}{
		{"positive-invalid",
			"-A PREROUTING -p tcp -m conntrack --ctstate INVALID -j xkeen\n",
			"tcp-prerouting-invalid-only"},
		{"negated-invalid-but-dscp-only",
			"-A PREROUTING -p tcp -m conntrack ! --ctstate INVALID -m dscp --dscp 0x3f -j xkeen\n",
			"tcp-prerouting-invalid-only"},
		{"positive-established-only",
			"-A PREROUTING -p tcp -m conntrack --ctstate ESTABLISHED -j xkeen\n",
			"tcp-prerouting-invalid-only"},
		{"missing",
			"-A PREROUTING -p udp -j xkeen\n",
			"tcp-prerouting-hook-absent"},
		{"explicit-new",
			"-A PREROUTING -p tcp -m conntrack --ctstate NEW -j xkeen\n",
			"tcp-prerouting-hook-present"},
	} {
		t.Run(tc.name,func(t *testing.T) {
			_, got:=captureDoctorNATState(tc.nat)
			if got!=tc.want {t.Fatalf("%s: got %q want %q",tc.name,got,tc.want)}
		})
	}
}
