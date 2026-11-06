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

func TestXKeenNativeSBReadsUpstreamJSONCWithoutInvokingXKeen(t *testing.T){
 dir:=t.TempDir()
 source:=`{
  // Native config. Secrets unrelated to balancer must never enter API.
  "xkeen":{
   "rci_token":"FAKE_SENSITIVE_TEST_TOKEN",
   /* comment with {"enabled":true} inside */
   "xray":{
    "speed_balancer":{
     "enabled":true,
     "interval":"15",
     "hysteresis":25,
     "balancer":"private-identifier",
     "test_url":"https://speed.cloudflare.com/__down?bytes=10000000"
    },
    "comment":"literal // and /* in a quoted string"
   }
  }
 }`
 if err:=os.WriteFile(filepath.Join(dir,"xkeen.json"),[]byte(source),0600);err!=nil{t.Fatal(err)}
 s,err:=xkeenNativeSBReadConfig(dir)
 if err!=nil{t.Fatal(err)}
 if !s.Success||s.Owner!="xkeen"||s.State!="configured-enabled"||!s.Enabled||
    s.IntervalMinutes!=15||s.HysteresisPercent!=25||s.Mutation!="NONE"||s.Runtime!="NOT_VERIFIED"{
  t.Fatalf("native read-only state invalid: %+v",s)
 }
 wire,_:=json.Marshal(s)
 for _,private:=range []string{"FAKE_SENSITIVE_TEST_TOKEN","private-identifier","https://speed.cloudflare.com"}{
  if bytes.Contains(wire,[]byte(private)){t.Fatalf("XKeen JSONC secret exposed in status: %q",private)}
 }
 if got:=strings.Join(s.NativeCommands,",");got!="on,off,status"{
  t.Fatalf("stable XKeen 2.1 capabilities mismatch: %s",got)
 }
}

func TestXKeenNativeSBUnknownConfigAndSymlinkFailClosed(t *testing.T){
 dir:=t.TempDir()
 s,err:=xkeenNativeSBReadConfig(dir)
 if err!=nil||!s.Success||s.State!="not-configured"||s.Enabled{t.Fatalf("absent config not distinguished: %+v %v",s,err)}
 for _,bad:=range []string{
  `{ "xkeen":{"xray":{"speed_balancer":{"enabled":true,"enabled":false}}}}`,
  `{ "xkeen":{"xray":{"speed_balancer":{"enabled":true,/*unclosed}}}}`,
  `{ "xkeen":{"xray":{"speed_balancer":{"enabled":"maybe"}}}}`,
  `{ "xkeen":{"xray":{"speed_balancer":{"enabled":true,"interval":-10}}}}`,
  `{ "xkeen":{"xray":{"speed_balancer":{"enabled":true,"balancer":"\n"}}}}`,
 }{
  if err:=os.WriteFile(filepath.Join(dir,"xkeen.json"),[]byte(bad),0600);err!=nil{t.Fatal(err)}
  if _,err:=xkeenNativeSBReadConfig(dir);err==nil{t.Fatalf("unsafe XKeen JSONC accepted: %q",bad)}
 }
 if err:=os.Remove(filepath.Join(dir,"xkeen.json"));err!=nil{t.Fatal(err)}
 outside:=filepath.Join(t.TempDir(),"xkeen.json")
 if err:=os.WriteFile(outside,[]byte(`{"xkeen":{"xray":{"speed_balancer":{"enabled":true}}}}`),0600);err!=nil{t.Fatal(err)}
 if err:=os.Symlink(outside,filepath.Join(dir,"xkeen.json"));err!=nil{t.Skip(err)}
 if _,err:=xkeenNativeSBReadConfig(dir);err==nil{t.Fatal("symlinked XKeen config must STOP")}
}

func TestXKeenNativeSBHTTPIsReadOnlyAndVersionLocked(t *testing.T){
 original:=xkeenNativeSBMetadata
 t.Cleanup(func(){xkeenNativeSBMetadata=original})
 xkeenNativeSBMetadata=func(_ string)(string,string,error){return "2.1","stable",nil}
 dir:=t.TempDir()
 t.Setenv("FREENET_XKEEN_CONFIG_DIR",dir)
 if err:=os.WriteFile(filepath.Join(dir,"xkeen.json"),[]byte(`{"xkeen":{"xray":{"speed_balancer":{"enabled":false}}}}`),0600);err!=nil{t.Fatal(err)}
 a:=&app{cfg:config{XKeenPath:"/opt/sbin/xkeen"}}
 request:=httptest.NewRequest(http.MethodGet,"http://router/api/xkeen/speed-balancer",nil)
 w:=httptest.NewRecorder()
 a.handleXKeenNativeSBStatus(w,request)
 if w.Code!=http.StatusOK{t.Fatalf("native read-only status HTTP %d: %s",w.Code,w.Body.String())}
 var result xkeenNativeSBStatus
 if json.Unmarshal(w.Body.Bytes(),&result)!=nil||!result.Success||result.InstalledVersion!="2.1"||
  result.State!="disabled"||result.Mutation!="NONE"||result.Runtime!="NOT_VERIFIED"{
  t.Fatalf("native status incorrectly claims live verified: %+v",result)
 }
 for _,tc:=range []struct{
  version,channel string
  expected int
  manual bool
 }{
  {"2.1.1","beta",http.StatusOK,true},
  {"2.2","beta",http.StatusOK,true},
  {"2.2","stable",http.StatusConflict,false},
  {"2.3","beta",http.StatusConflict,false},
  {"2.1.1","unknown",http.StatusConflict,false},
 }{
  xkeenNativeSBMetadata=func(_ string)(string,string,error){return tc.version,tc.channel,nil}
  w=httptest.NewRecorder();a.handleXKeenNativeSBStatus(w,request)
  if w.Code!=tc.expected{t.Fatalf("%s/%s: HTTP %d want %d",tc.version,tc.channel,w.Code,tc.expected)}
  if strings.Contains(w.Body.String(),"/opt/")||strings.Contains(w.Body.String(),"FAKE_SENSITIVE_TEST_TOKEN"){
   t.Fatalf("metadata leaked in read-only response: %s",w.Body.String())
  }
  if tc.expected==http.StatusOK{
   var got xkeenNativeSBStatus
   if json.Unmarshal(w.Body.Bytes(),&got)!=nil||got.InstalledVersion!=tc.version||
      got.InstalledChannel!=tc.channel||got.Mutation!="NONE"||got.Runtime!="NOT_VERIFIED"{
    t.Fatalf("XKeen version-scoped status invalid: %+v",got)
   }
   commands:=strings.Join(got.NativeCommands,",")
   if tc.manual&&!strings.Contains(commands,"next,prev,N"){
    t.Fatalf("native manual balancer commands hidden on beta: %s",commands)
   }
  }
 }

}

func TestFreeNetLegacyPoolNewProvisioningBlockedButExistingNotDeleted(t *testing.T){
 if freeNetAutoLegacyProvisionEnabled{t.Fatal("new private balancer must remain disabled after upstream-first decision")}
 for _,tc:=range []struct{
  name string
  body string
  call func(*app,http.ResponseWriter,*http.Request)
 }{
  {"plan",`{"selection_token":"valid-placeholder","profile_ids":["example"]}`,(*app).handleFreeNetAutoPoolPlan},
  {"activate",`{"plan_token":"valid-placeholder","acknowledge":"prepare-no-restart-autovpn-with-controlled-core-restart"}`,(*app).handleFreeNetAutoPoolActivate},
 }{
  t.Run(tc.name,func(t *testing.T){
   a:=&app{}
   r:=httptest.NewRequest(http.MethodPost,"http://router/api/automation/pool/"+tc.name,strings.NewReader(tc.body))
   r.Header.Set("Content-Type","application/json")
   w:=httptest.NewRecorder()
   tc.call(a,w,r)
   if w.Code!=http.StatusConflict{t.Fatalf("legacy new pool should STOP: %d %s",w.Code,w.Body.String())}
   var state struct{
    Mutation string `json:"mutation"`
    Status string `json:"status"`
    Ready bool `json:"ready"`
    Success bool `json:"success"`
   }
   if err:=json.Unmarshal(w.Body.Bytes(),&state);err!=nil{t.Fatal(err)}
   if state.Status!="native-xkeen-sb-preferred-no-new-pool"||
     state.Mutation!="NONE"||state.Ready||state.Success{
    t.Fatalf("legacy provisioning was not stopped before mutation: %+v",state)
   }
  })
 }
}

func TestXKeenNativeBalancerExactTagCapabilities(t *testing.T){
 for _,tc:=range []struct{version,channel string;known,manual bool}{
  {"2.1","stable",true,false},
  {"2.1.1","beta",true,true},
  {"2.2","beta",true,true},
  {"2.2","stable",false,false},
  {"2.1.1","stable",false,false},
  {"2.0","stable",false,false},
  {"2.3","beta",false,false},
  {"","unknown",false,false},
 }{
  commands,ok:=xkeenNativeSBCapabilities(tc.version,tc.channel)
  if ok!=tc.known{t.Fatalf("capability gate %s/%s: %v",tc.version,tc.channel,ok)}
  manual:=strings.Contains(strings.Join(commands,","),"next,prev,N")
  if manual!=tc.manual{t.Fatalf("manual commands must match release %s/%s",tc.version,tc.channel)}
 }
}
