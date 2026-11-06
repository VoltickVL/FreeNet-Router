package main

import (
 "bytes"
 "encoding/json"
 "net/http"
 "net/http/httptest"
 "os"
 "path/filepath"
 "strings"
 "testing"
)

func nativeTopologyFixture(t *testing.T)(*app,string){
 t.Helper()
 dir:=t.TempDir()
 out:=`{// XKeen JSONC comments are valid in native configuration.
 "outbounds":[
 {"tag":"direct","protocol":"freedom"},
 {"tag":"sub-DE Frankfurt","protocol":"vless","settings":{"vnext":[{"address":"SECRET-HOST","users":[{"id":"CREDENTIAL-NEVER-SEND"}]}]}},
 {"tag":"sub-FI","protocol":"vmess"}, /* inline upstream JSONC block */
 {"tag":"sub-dns","protocol":"dns"},
 {"tag":"sub-12345678-1234-1234-1234-1234567890ab","protocol":"vless"},
 {"tag":"vless://user:password@example.org?token=supersecret","protocol":"trojan"},
 {"tag":"another-sub","protocol":"vless"}]}`
 routing:=`{"routing":{"rules":[],"balancers":[
 {"tag":"balancer","selector":["sub-"],"strategy":{"type":"leastPing"}},
 {"tag":"other-balancer","selector":["another-"],"strategy":{"type":"roundRobin"}}
 ]}}`
 if err:=os.WriteFile(filepath.Join(dir,"04_outbounds.json"),[]byte(out),0600);err!=nil{t.Fatal(err)}
 if err:=os.WriteFile(filepath.Join(dir,"05_routing.json"),[]byte(routing),0600);err!=nil{t.Fatal(err)}
 return &app{cfg:config{OutPath:filepath.Join(dir,"04_outbounds.json")}},dir
}

func TestNativeXrayTopologyMatchesOfficialXKeenSelectorsAndRedactsSecrets(t *testing.T){
 a,dir:=nativeTopologyFixture(t)
 originalOut,_:=os.ReadFile(filepath.Join(dir,"04_outbounds.json"))
 originalRouting,_:=os.ReadFile(filepath.Join(dir,"05_routing.json"))
 got,err:=nativeXrayInventory(dir)
 if err!=nil{t.Fatal(err)}
 if !got.Success||got.State!="CONFIG_ONLY"||got.Mutation!="NONE"||
  got.Runtime!="NOT_VERIFIED"||got.ServerCount!=7||got.BalancerCount!=2{
  t.Fatalf("config-only inventory invalid: %+v",got)
 }
 if got.Balancers[0].EligibleServers!=3||!got.Balancers[0].NativeSBCompatible||
   got.Balancers[1].EligibleServers!=1||got.Balancers[1].NativeSBCompatible{
   t.Fatalf("native eligibility/leastPing mismatch: %+v",got.Balancers)
 }
 if got.Servers[0].Eligible||got.Servers[3].Eligible||got.Servers[5].Eligible||
  !got.Servers[1].Eligible||!got.Servers[2].Eligible||!got.Servers[4].Eligible||
  !got.Servers[6].Eligible{
  t.Fatalf("native prefix protocol exclusion mismatch: %+v",got.Servers)
 }
 if got.Servers[1].Name!="sub-DE Frankfurt"{
  t.Fatalf("native XKeen selector with spaces not preserved: %+v",got.Servers)
 }
 if got.Servers[4].Name!="Сервер 5"||got.Servers[5].Name!="Сервер 6"{
  t.Fatalf("credential-bearing tags must be redacted: %+v",got.Servers)
 }
 raw,err:=json.Marshal(got)
 if err!=nil{t.Fatal(err)}
 for _,secret:=range []string{"CREDENTIAL-NEVER-SEND","SECRET-HOST","12345678-1234","vless://","password@example","supersecret","address","users"}{
  if bytes.Contains(raw,[]byte(secret)){t.Fatalf("secret field leaked: %q",secret)}
 }
 if len(got.OutboundsSHA256)!=64||len(got.RoutingSHA256)!=64{
  t.Fatal("exact config source hashes required for later CAS")
 }
 rec:=httptest.NewRecorder()
 a.handleNativeXrayTopology(rec,httptest.NewRequest(http.MethodGet,"http://router/api/native/xray/topology",nil))
 if rec.Code!=http.StatusOK||!strings.Contains(rec.Body.String(),`"runtime":"NOT_VERIFIED"`){
  t.Fatalf("HTTP read-only status not preserved: %d %s",rec.Code,rec.Body.String())
 }
 afterOut,_:=os.ReadFile(filepath.Join(dir,"04_outbounds.json"))
 afterRouting,_:=os.ReadFile(filepath.Join(dir,"05_routing.json"))
 if !bytes.Equal(afterOut,originalOut)||!bytes.Equal(afterRouting,originalRouting){
  t.Fatal("GET topology modified native Xray config")
 }
}

func TestNativeXrayTopologyUnknownOrContradictoryStopsWithoutLeakingSource(t *testing.T){
 cases:=[]struct{name,out,routing string}{
  {"duplicate-outbound",`{"outbounds":[{"tag":"same","protocol":"vless"},{"tag":"same","protocol":"trojan"}]}`,`{"routing":{"balancers":[]}}`},
  {"duplicate-balancer",`{"outbounds":[]}`,`{"routing":{"balancers":[{"tag":"b","selector":["sub-"]},{"tag":"b","selector":["sub-"]}]}}`},
  {"duplicate-json-key",`{"outbounds":[],"outbounds":[{"tag":"secret","protocol":"vless"}]}`,`{"routing":{"balancers":[]}}`},
  {"missing-outbound-tag",`{"outbounds":[{"protocol":"vless"}]}`,`{"routing":{"balancers":[]}}`},
  {"missing-outbound-protocol",`{"outbounds":[{"tag":"foo"}]}`,`{"routing":{"balancers":[]}}`},
  {"empty-selector",`{"outbounds":[]}`,`{"routing":{"balancers":[{"tag":"b","selector":[""]}]}}`},
  {"missing-routing-root",`{"outbounds":[]}`,`{"notRouting":{}}`},
  {"invalid-routing-array",`{"outbounds":[]}`,`{"routing":{"balancers":"secret"}}`},
  {"invalid-source-json",`{"outbounds":[{"tag":"value", "protocol":"vless"}]`,`{"routing":{"balancers":[]}}`},
 }
 for _,tc:=range cases{
  t.Run(tc.name,func(t *testing.T){
   a,dir:=nativeTopologyFixture(t)
   if err:=os.WriteFile(filepath.Join(dir,"04_outbounds.json"),[]byte(tc.out),0600);err!=nil{t.Fatal(err)}
   if err:=os.WriteFile(filepath.Join(dir,"05_routing.json"),[]byte(tc.routing),0600);err!=nil{t.Fatal(err)}
   if _,err:=nativeXrayInventory(dir);err==nil{t.Fatal("invalid native config must STOP")}
   rec:=httptest.NewRecorder()
   a.handleNativeXrayTopology(rec,httptest.NewRequest(http.MethodGet,"http://router/api/native/xray/topology",nil))
   if rec.Code!=http.StatusConflict||strings.Contains(rec.Body.String(),"same")||
    strings.Contains(rec.Body.String(),"secret")||strings.Contains(rec.Body.String(),"tag"){
    t.Fatalf("native fail closed leaked source: %d %s",rec.Code,rec.Body.String())
   }
  })
 }
}

func TestNativeXrayTopologySymlinksAndOversizeStop(t *testing.T){
 a,dir:=nativeTopologyFixture(t)
 outside:=filepath.Join(t.TempDir(),"outside.json")
 if err:=os.WriteFile(outside,[]byte(`{"outbounds":[]}`),0600);err!=nil{t.Fatal(err)}
 outPath:=filepath.Join(dir,"04_outbounds.json")
 if err:=os.Remove(outPath);err!=nil{t.Fatal(err)}
 if err:=os.Symlink(outside,outPath);err!=nil{t.Skip(err)}
 rec:=httptest.NewRecorder()
 a.handleNativeXrayTopology(rec,httptest.NewRequest(http.MethodGet,"http://router/api/native/xray/topology",nil))
 if rec.Code!=http.StatusConflict{t.Fatal("symlinked outbounds must STOP")}
 if err:=os.Remove(outPath);err!=nil{t.Fatal(err)}
 if err:=os.WriteFile(outPath,bytes.Repeat([]byte("x"),int(nativeXrayInventoryMaxFile)+1),0600);err!=nil{t.Fatal(err)}
 if _,err:=nativeXrayInventory(dir);err==nil{t.Fatal("oversized native config accepted")}
}

func TestNativeXrayTopologyNoBalancerIsNotRuntimeSuccess(t *testing.T){
 _,dir:=nativeTopologyFixture(t)
 if err:=os.WriteFile(filepath.Join(dir,"05_routing.json"),[]byte(`{"routing":{"rules":[]}}`),0600);err!=nil{t.Fatal(err)}
 got,err:=nativeXrayInventory(dir)
 if err!=nil{t.Fatal(err)}
 if !got.Success||got.State!="NO_BALANCER"||got.Runtime!="NOT_VERIFIED"||
  len(got.Balancers)!=0{
  t.Fatalf("no-balancer config not distinguished from live: %+v",got)
 }
}
