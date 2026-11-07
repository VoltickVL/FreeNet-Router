package main

import (
 "bytes"
 "context"
 "encoding/json"
 "net/http"
 "net/http/httptest"
 "os"
 "path/filepath"
 "reflect"
 "strings"
 "testing"
)

func legacyDNSFixture(t *testing.T)(string,map[string][]byte){
 t.Helper()
 root:=t.TempDir()
 writeFixture(t,root,"etc/xray/configs/02_dns.json",`{
   "dns":{"tag":"dns-vless","queryStrategy":"UseIPv4","servers":[
     {"tag":"dns-direct","address":"SECRET-PRIVATE-DIRECT","domains":["domain:direct.example"]},
     {"tag":"dns-vless","address":"https://SECRET-PRIVATE-DOH/dns-query","domains":["domain:vpn.example"]},
     {"tag":"dns-vless","address":"https://SECRET-PRIVATE-DOH/dns-query"}
   ]}
 }`)
 writeFixture(t,root,"etc/xray/configs/03_inbounds.json",`{"inbounds":[
   {"tag":"redirect","protocol":"dokodemo-door","port":1181},
   {"tag":"dns-in","protocol":"dokodemo-door","port":53}
 ]}`)
 writeFixture(t,root,"etc/xray/configs/04_outbounds.json",`{"outbounds":[
   {"tag":"direct","protocol":"freedom","streamSettings":{"sockopt":{"mark":255}}},
   {"tag":"vless-reality","protocol":"vless","settings":{"secret":"SECRET-PRIVATE-UUID"}},
   {"tag":"dns-out","protocol":"dns"}
 ]}`)
 writeFixture(t,root,"etc/xray/configs/05_routing.json",`{"routing":{
   "rules":[
    {"type":"field","inboundTag":["dns-vless"],"outboundTag":"vless-reality"},
    {"type":"field","inboundTag":["dns-direct"],"outboundTag":"direct"},
    {"type":"field","port":53,"outboundTag":"dns-out"},
    {"type":"field","domain":["domain:youtube.com"],"outboundTag":"direct"},
    {"type":"field","domain":["domain:company.internal"],"outboundTag":"vless-reality"}
   ],"balancers":[{"tag":"native","selector":["vless"]}]
 }}`)
 writeFixture(t,root,"etc/xray/blanc_subscription.url","https://private.example/?token=SECRET-PRIVATE-SUBSCRIPTION")
 original,err:=readNativeConfdir(root)
 if err!=nil{t.Fatal(err)}
 return root,original
}

func decodedNativeRouteRules(t *testing.T,data []byte)[]json.RawMessage{
 t.Helper()
 var decoded struct{Routing struct{Rules []json.RawMessage `json:"rules"`} `json:"routing"`}
 if err:=json.Unmarshal(data,&decoded);err!=nil{t.Fatal(err)}
 return decoded.Routing.Rules
}

func TestLegacyDNSCandidateIsIsolatedPreservesUserPolicy(t *testing.T){
 root,before:=legacyDNSFixture(t)
 candidate,result,err:=buildLegacySplitFreeXrayCandidate(before)
 if err!=nil{t.Fatal(err)}
 if result.State!="CANDIDATE_PREPARED_NOT_APPLIED" || result.Applied ||
    result.RemovedResolvers!=3 || result.RemovedOutbounds!=1 ||
    result.RemovedInbounds!=1 || result.RemovedRules!=3 {
  t.Fatalf("candidate counts and truthfulness: %+v",result)
 }
 if strings.TrimSpace(string(candidate["02_dns.json"]))!="{}"{
  t.Fatal("candidate inherited legacy DNS engine")
 }
 var outbound struct{Outbounds []struct{Tag string `json:"tag"`} `json:"outbounds"`}
 if json.Unmarshal(candidate["04_outbounds.json"],&outbound)!=nil||len(outbound.Outbounds)!=2{
  t.Fatal("candidate outbounds incorrectly changed")
 }
 oldRules:=decodedNativeRouteRules(t,before["05_routing.json"])
 newRules:=decodedNativeRouteRules(t,candidate["05_routing.json"])
 if len(newRules)!=2||!reflect.DeepEqual(oldRules[3:],newRules) {
  t.Fatal("original user DIRECT/VPN routing semantics/order changed")
 }
 for name,raw:=range before{
  if strings.Contains(name,"subscription"){t.Fatal("subscription present in Xray confdir")}
  got,err:=os.ReadFile(filepath.Join(root,"etc/xray/configs",name))
  if err!=nil||!bytes.Equal(got,raw){t.Fatalf("live %s mutated",name)}
 }
 private,err:=os.ReadFile(filepath.Join(root,"etc/xray/blanc_subscription.url"))
 if err!=nil||!bytes.Contains(private,[]byte("SECRET-PRIVATE-SUBSCRIPTION")){t.Fatal("private subscription mutated")}
 if !bytes.Contains(candidate["04_outbounds.json"],[]byte("SECRET-PRIVATE-UUID")){
  t.Fatal("user VPN credentials were lost from private candidate")
 }
}

func TestLegacyDNSCandidateStopsOnUnknownUserDNSOrPartial(t *testing.T){
 for _,tc:=range []struct{name,file,old,new string}{
  {"custom DNS resolver","02_dns.json",`"tag":"dns-direct"`,`"tag":"user-dns"`},
  {"ambiguous DNS top-level","02_dns.json",`"tag":"dns-vless","queryStrategy"`,`"tag":"user-dns","queryStrategy"`},
  {"unknown outbound protocol","04_outbounds.json",`"tag":"dns-out","protocol":"dns"`,`"tag":"dns-out","protocol":"vless"`},
  {"unknown DNS selector","05_routing.json",`"inboundTag":["dns-direct"],"outboundTag":"direct"`,`"inboundTag":["dns-direct"],"domain":["private"],"outboundTag":"direct"`},
  {"missing dns port rule","05_routing.json",`"port":53,"outboundTag":"dns-out"`,`"port":54,"outboundTag":"dns-out"`},
 }{
  t.Run(tc.name,func(t *testing.T){
   _,files:=legacyDNSFixture(t)
   data:=strings.Replace(string(files[tc.file]),tc.old,tc.new,1)
   if data==string(files[tc.file]) {t.Fatal("invalid test fixture mutation")}
   files[tc.file]=[]byte(data)
   _,state,err:=buildLegacySplitFreeXrayCandidate(files)
   if err==nil||state.Applied||state.State!="STOP"{t.Fatalf("unsafe policy accepted: %+v %v",state,err)}
  })
 }
 _,files:=legacyDNSFixture(t)
 files["06_custom.json"]=[]byte(`{"routing":{"rules":[{"inboundTag":["dns-in"]}]}}`)
 if _,_,err:=buildLegacySplitFreeXrayCandidate(files);err==nil{t.Fatal("dangling DNS reference in other file accepted")}
}

func TestNativeDNS53OwnerParsing(t *testing.T) {
 samples:=[]struct{out,want string}{
  {"tcp 0 0 0.0.0.0:53 0.0.0.0:* LISTEN 1261/xray\nudp 0 0 0.0.0.0:53 0.0.0.0:* 1261/xray","XRAY_DNS53"},
  {"udp 0 0 0.0.0.0:53 0.0.0.0:* 455/ndnproxy","ROUTER_DNS53"},
  {"tcp 0 0 0.0.0.0:443 0.0.0.0:* LISTEN 66/web","NO_DNS53_LISTENER"},
  {"udp 0 0 0.0.0.0:53 0.0.0.0:* -","UNKNOWN"},
  {"udp 0 0 0.0.0.0:53 0.0.0.0:* 456/ndnproxy\ntcp 0 0 0.0.0.0:53 0.0.0.0:* LISTEN 1261/xray","UNKNOWN"},
 }
 for _,tc:=range samples{
  if got:=netstatDNS53Owner(tc.out);got!=tc.want{t.Fatalf("netstat owner %q, want %q",got,tc.want)}
 }
}

func TestNativeDNSCandidateValidatesButNeverApplies(t *testing.T){
 root,before:=legacyDNSFixture(t)
 fakeNativeBinary(t,root,"xray",`if [ "$1" != "run" ] || [ "$2" != "-test" ] || [ "$3" != "-confdir" ]; then exit 13; fi
test -d "$4" || exit 14
exit 0
`)
 fakeNativeBinary(t,root,"netstat",`echo 'tcp 0 0 0.0.0.0:53 0.0.0.0:* LISTEN 1261/xray'`)
 out:=nativeDNSCandidatePlan(context.Background(),root)
 if out.State!="CANDIDATE_VALIDATED_DNS53_TRANSITION_REQUIRED" ||
    out.DNS53Owner!="XRAY_DNS53"||out.Runtime!="NOT_VERIFIED"||out.Applied {
  t.Fatalf("active Xray :53 not blocked: %+v",out)
 }
 after,err:=readNativeConfdir(root)
 if err!=nil || !sameNativeConfdir(before,after){t.Fatal("candidate analysis modified live Xray")}
 for _,p:=range []string{"etc/xray/configs/04_dns_policy_candidate.json","etc/freenet09"}{
  if _,err:=os.Lstat(filepath.Join(root,p));err==nil{t.Fatalf("candidate leaked to live root: %s",p)}
 }
 api:=handlersWithProc(root,t.TempDir())
 got:=httptest.NewRecorder()
 api.ServeHTTP(got,httptest.NewRequest(http.MethodGet,"/api/native/dns-candidate",nil))
 if got.Code!=http.StatusOK || !strings.Contains(got.Body.String(),"CANDIDATE_VALIDATED_DNS53_TRANSITION_REQUIRED") {
  t.Fatalf("read-only candidate API: %d %s",got.Code,got.Body.String())
 }
 for _,secret:=range []string{"SECRET-PRIVATE-UUID","SECRET-PRIVATE-DOH","SECRET-PRIVATE-SUBSCRIPTION","private.example"} {
  if strings.Contains(got.Body.String(),secret){t.Fatalf("secret leaked in candidate API: %s",secret)}
 }
 for _,method:=range []string{http.MethodPost,http.MethodPut,http.MethodDelete}{
  w:=httptest.NewRecorder()
  api.ServeHTTP(w,httptest.NewRequest(method,"/api/native/dns-candidate",nil))
  if w.Code==http.StatusOK {t.Fatalf("write method %s unexpectedly accepted",method)}
 }
}

func TestNativeDNSCandidateRejectsInvalidXrayWithoutWrites(t *testing.T) {
 root,before:=legacyDNSFixture(t)
 fakeNativeBinary(t,root,"xray",`case "$4" in *freenet09-dns-candidate-*) exit 47 ;; esac
exit 0
`)
 fakeNativeBinary(t,root,"netstat",`echo 'udp 0 0 0.0.0.0:53 0.0.0.0:* 1261/xray'`)
 got:=nativeDNSCandidatePlan(context.Background(),root)
 if got.State!="CANDIDATE_REJECTED"||got.Applied{t.Fatalf("invalid candidate accepted: %+v",got)}
 after,err:=readNativeConfdir(root)
 if err!=nil||!sameNativeConfdir(before,after){t.Fatal("rejected candidate changed live state")}
}
