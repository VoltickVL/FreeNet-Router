package main

import (
 "bytes"
 "encoding/json"
 "strings"
 "testing"
)

const freeNetAutoPoolCurrent = `{
 "outbounds": [
  {"tag":"vless-reality","protocol":"vless","settings":{"vnext":[{"address":"198.51.100.10","port":443,"users":[{"id":"11111111-1111-4111-8111-111111111111"}]}]},"streamSettings":{"network":"tcp","security":"reality","realitySettings":{"serverName":"example.com","publicKey":"NON_SECRET_TEST_PUBLIC_KEY","shortId":"12345678"}}},
  {"tag":"direct","protocol":"freedom","settings":{}},
  {"tag":"block","protocol":"blackhole"}
 ]
}`
const freeNetAutoPoolRouting = `{"routing":{"domainStrategy":"AsIs","rules":[
 {"domain":["domain:example.test"],"outboundTag":"direct"},
 {"network":"tcp,udp","outboundTag":"vless-reality"}
 ]}}`

func freeNetAutoPoolAlternative(t *testing.T) map[string]any {
 t.Helper()
 var m map[string]any
 raw:=`{"tag":"vless-reality","protocol":"vless","settings":{"vnext":[{"address":"203.0.113.20","port":443,"users":[{"id":"22222222-2222-4222-8222-222222222222"}]}]},"streamSettings":{"network":"tcp","security":"reality","realitySettings":{"serverName":"alternative.test","publicKey":"NON_SECRET_TEST_PUBLIC_KEY","shortId":"87654321"}}}`
 if err:=json.Unmarshal([]byte(raw),&m);err!=nil{t.Fatal(err)}
 return m
}

func TestFreeNetAutoPoolCompilesOnlyVPNRules(t *testing.T) {
 alt:=freeNetAutoPoolAlternative(t)
 p,err:=buildFreeNetAutoPoolCandidate([]byte(freeNetAutoPoolCurrent),[]byte(freeNetAutoPoolRouting),[]map[string]any{alt},"127.0.0.1:10085","127.0.0.1:10086")
 if err!=nil{t.Fatal(err)}
 if p.MigratedRules!=1 || len(p.Tags)!=2 ||
   p.Tags[0]!="vless-reality" || p.Tags[1]!="freenet-auto-node-1"{
  t.Fatalf("wrong pool topology: %+v",p)
 }
 var doc struct {
  Outbounds []map[string]json.RawMessage `json:"outbounds"`
 }
 if err:=json.Unmarshal(p.Outbounds,&doc);err!=nil{t.Fatal(err)}
 if len(doc.Outbounds)!=4 {t.Fatal("must preserve original three and append only one alternative")}
 var direct string
 if json.Unmarshal(doc.Outbounds[1]["tag"],&direct)!=nil || direct!="direct"{
  t.Fatal("DIRECT must be preserved at original position")
 }
 var routing struct{
  Routing struct{
   Rules []map[string]json.RawMessage `json:"rules"`
   Balancers []struct{
    Tag string `json:"tag"`
    Selector []string `json:"selector"`
    Strategy map[string]string `json:"strategy"`
   } `json:"balancers"`
  } `json:"routing"`
 }
 if err:=json.Unmarshal(p.Routing,&routing);err!=nil{t.Fatal(err)}
 if len(routing.Routing.Rules)!=3 || len(routing.Routing.Balancers)!=1 {
  t.Fatal("routing scope changed")
 }
 var routingRoot map[string]json.RawMessage
 if err:=json.Unmarshal(p.Routing,&routingRoot);err!=nil{t.Fatal(err)}
 var api struct {Tag string `json:"tag"`; Listen string `json:"listen"`; Services []string `json:"services"`}
 if err:=json.Unmarshal(routingRoot["api"],&api);err!=nil{t.Fatal(err)}
 if api.Tag!=freeNetAutoAPITag || api.Listen!="127.0.0.1:10085" ||
   len(api.Services)!=1 || api.Services[0]!="RoutingService" {
  t.Fatal("API is not limited to the local routing service")
 }
 var obs struct {SubjectSelector []string `json:"subjectSelector"`; ProbeInterval string `json:"probeInterval"`}
 if err:=json.Unmarshal(routingRoot["observatory"],&obs);err!=nil{t.Fatal(err)}
 if len(obs.SubjectSelector)!=2 || obs.ProbeInterval!="60s" {
  t.Fatal("leastPing needs its matching, bounded observatory")
 }
 var probes []struct {Tag string `json:"tag"`; Listen string `json:"listen"`; Port int `json:"port"`; Protocol string `json:"protocol"`}
 if err:=json.Unmarshal(routingRoot["inbounds"],&probes);err!=nil{t.Fatal(err)}
 if len(probes)!=1||probes[0].Tag!=freeNetAutoProbeTag||
  probes[0].Listen!="127.0.0.1"||probes[0].Port!=10086||probes[0].Protocol!="socks"{
  t.Fatal("live HTTPS probe must be a loopback-only SOCKS inbound")
 }
 if !bytes.Equal(bytes.TrimSpace(routing.Routing.Rules[0]["balancerTag"]),[]byte(`"freenet-auto-vpn"`)) {t.Fatal("live probe must use the VPN balancer")}
 if !bytes.Equal(bytes.TrimSpace(routing.Routing.Rules[2]["outboundTag"]),[]byte(`"direct"`)){
  t.Fatal("DIRECT rule unexpectedly rewritten")
 }
 if _,ok:=routing.Routing.Rules[2]["outboundTag"];ok {t.Fatal("old VPN outboundTag retained")}
 if !bytes.Equal(bytes.TrimSpace(routing.Routing.Rules[2]["balancerTag"]),[]byte(`"freenet-auto-vpn"`)){
  t.Fatal("VPN routing did not switch to balancer")
 }
 bal:=routing.Routing.Balancers[0]
 if bal.Tag!="freenet-auto-vpn" || bal.Strategy["type"]!="leastPing" ||
  len(bal.Selector)!=2 || bal.Selector[1]!=freeNetAutoOutboundPrefix{
  t.Fatal("unrecognized Xray routing balancer")
 }
 if !bytes.Contains(p.Outbounds,[]byte("203.0.113.20")) ||
    !bytes.Contains(p.Outbounds,[]byte("198.51.100.10")) {
  t.Fatal("the original and alternative endpoint must coexist in candidate")
 }
 // Pure: no mutation of input, which must remain a rollback reference.
 if _,ok:=alt["balancerTag"];ok {t.Fatal("source outbound mutated")}
}
func TestFreeNetAutoPoolFailClosedForUnknownOrConflictingLayouts(t *testing.T) {
 alt:=freeNetAutoPoolAlternative(t)
 tests:=[]struct{name,out,rout string;alts []map[string]any}{
  {"duplicate-tag",strings.Replace(freeNetAutoPoolCurrent,`"tag":"direct"`,`"tag":"vless-reality"`,1),freeNetAutoPoolRouting,[]map[string]any{alt}},
  {"reserved-tag",strings.Replace(freeNetAutoPoolCurrent,`"tag":"direct"`,`"tag":"freenet-auto-node-other"`,1),freeNetAutoPoolRouting,[]map[string]any{alt}},
  {"existing-balancer",freeNetAutoPoolCurrent,strings.Replace(freeNetAutoPoolRouting,`"rules":[`,`"balancers":[{"tag":"custom","selector":["other"]}],"rules":[`,1),[]map[string]any{alt}},
  {"existing-rule-balancer",freeNetAutoPoolCurrent,strings.Replace(freeNetAutoPoolRouting,`"outboundTag":"direct"`,`"balancerTag":"existing"`,1),[]map[string]any{alt}},
  {"no-vpn-rule",freeNetAutoPoolCurrent,strings.Replace(freeNetAutoPoolRouting,`"outboundTag":"vless-reality"`,`"outboundTag":"direct"`,1),[]map[string]any{alt}},
  {"duplicate-json-key",freeNetAutoPoolCurrent,strings.Replace(freeNetAutoPoolRouting,`"domainStrategy":"AsIs"`,`"domainStrategy":"AsIs","domainStrategy":"AsIs"`,1),[]map[string]any{alt}},
  {"oversized-outbound",freeNetAutoPoolCurrent+strings.Repeat(" ",maxConfigStudioFileBytes),freeNetAutoPoolRouting,[]map[string]any{alt}},
  {"no-alternatives",freeNetAutoPoolCurrent,freeNetAutoPoolRouting,nil},
  {"too-many",freeNetAutoPoolCurrent,freeNetAutoPoolRouting,[]map[string]any{alt,alt,alt}},
 }
 for _,tc:=range tests{
  t.Run(tc.name,func(t *testing.T){
   candidate,err:=buildFreeNetAutoPoolCandidate([]byte(tc.out),[]byte(tc.rout),tc.alts,"127.0.0.1:10085","127.0.0.1:10086")
   if err==nil{t.Fatalf("unsafe pool admitted: tags=%v",candidate.Tags)}
   if len(candidate.Outbounds)!=0||len(candidate.Routing)!=0{
    t.Fatal("rejected candidate returned executable output")
   }
  })
 }
}
func TestFreeNetAutoPoolRejectsUnsupportedAlternative(t *testing.T) {
 for _,value:=range []string{"ws","grpc","httpupgrade"} {
  t.Run(value,func(t *testing.T){
   alt:=freeNetAutoPoolAlternative(t)
   stream:=alt["streamSettings"].(map[string]any)
   stream["network"]=value
   p,err:=buildFreeNetAutoPoolCandidate([]byte(freeNetAutoPoolCurrent),[]byte(freeNetAutoPoolRouting),[]map[string]any{alt},"127.0.0.1:10085","127.0.0.1:10086")
   if err==nil || len(p.Tags)!=0{t.Fatal("unsupported alternate VLESS transport admitted")}
  })
 }
}

func TestFreeNetAutoPoolRejectsPublicOrPrivilegedRPC(t *testing.T) {
 alt:=freeNetAutoPoolAlternative(t)
 for _,addr:=range []string{
  "0.0.0.0:10085","192.168.1.1:10085","[::1]:10085","localhost:10085",
  "127.0.0.1:80","127.0.0.1:0","127.0.0.1:65536","",
 } {
  t.Run(addr,func(t *testing.T){
   c,err:=buildFreeNetAutoPoolCandidate([]byte(freeNetAutoPoolCurrent),[]byte(freeNetAutoPoolRouting),[]map[string]any{alt},addr,"127.0.0.1:10086")
   if err==nil||len(c.Outbounds)>0||len(c.Routing)>0{t.Fatal("unsafe API listener accepted")}
  })
 }
}
func TestFreeNetAutoPoolRejectsPriorAPIAndObservatory(t *testing.T) {
 alt:=freeNetAutoPoolAlternative(t)
 for _,key:=range []string{"api","observatory","burstObservatory"} {
  t.Run(key,func(t *testing.T){
   var obj map[string]any
   if err:=json.Unmarshal([]byte(freeNetAutoPoolRouting),&obj);err!=nil{t.Fatal(err)}
   obj[key]=map[string]any{"existing":"external"}
   bad,_:=json.Marshal(obj)
   c,err:=buildFreeNetAutoPoolCandidate([]byte(freeNetAutoPoolCurrent),bad,[]map[string]any{alt},"127.0.0.1:10085","127.0.0.1:10086")
   if err==nil||len(c.Outbounds)>0{t.Fatal("existing API/observer overwritten")}
  })
 }
}
 
func TestFreeNetAutoPoolRejectsUnsafeProbeAddresses(t *testing.T){
 alt:=freeNetAutoPoolAlternative(t)
 for _,address:=range []string{"0.0.0.0:10086","192.168.1.2:10086","127.0.0.1:10085","127.0.0.1:80","[::1]:10086","localhost:10086"}{
  t.Run(address,func(t *testing.T){
   p,e:=buildFreeNetAutoPoolCandidate([]byte(freeNetAutoPoolCurrent),[]byte(freeNetAutoPoolRouting),
    []map[string]any{alt},"127.0.0.1:10085",address)
   if e==nil||len(p.Outbounds)>0{t.Fatal("unsafe probe listener accepted")}
  })
 }
}
