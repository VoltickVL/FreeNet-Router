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

func nativeFixture(t *testing.T) string {
    t.Helper()
    root:=t.TempDir()
    out:=`{// Official XKeen JSONC support
"outbounds":[
{"tag":"direct","protocol":"freedom"},
{"tag":"sub-DE Frankfurt","protocol":"vless","settings":{"vnext":[{"address":"PRIVATE-ENDPOINT","users":[{"id":"CREDENTIAL-DO-NOT-SHOW"}]}]}},
{"tag":"sub-server.example.org","protocol":"trojan"},
{"tag":"sub-203.0.113.4","protocol":"vmess"},
{"tag":"sub-UUID-12345678-1234-1234-1234-1234567890ab","protocol":"vless"},
{"tag":"sub-dns","protocol":"dns"},
{"tag":"standalone","protocol":"socks"},
{"tag":"vless://username:password@private.example?token=secret","protocol":"vless"}
]}`
    route:=`{
"routing":{"rules":[],
"balancers":[
{"tag":"native-balanced","selector":["sub-","sub-DE"],"strategy":{"type":"leastPing"}},
{"tag":"other","selector":["standalone"],"strategy":{"type":"random"}}
]}}`
    writeFixture(t,root,"etc/xray/configs/04_outbounds.json",out)
    writeFixture(t,root,"etc/xray/configs/05_routing.json",route)
    return root
}

func TestNativeXrayInventoryReadsOriginalTagsWithoutLeakingCredentials(t *testing.T) {
    root:=nativeFixture(t)
    original,err:=os.ReadFile(filepath.Join(root,"etc/xray/configs/04_outbounds.json"))
    if err!=nil {t.Fatal(err)}
    topology,err:=nativeXrayTopology(root)
    if err!=nil {t.Fatal(err)}
    if topology.State!="CONFIG_ONLY" || topology.Runtime!="NOT_VERIFIED" ||
        len(topology.Servers)!=8 || len(topology.Balancers)!=2 {
        t.Fatalf("unexpected Xray config only state: %+v",topology)
    }
    if topology.Balancers[0].Servers!=4 || topology.Balancers[1].Servers!=1 {
        t.Fatalf("upstream prefix selector semantics broken: %+v",topology.Balancers)
    }
    if topology.Servers[0].InBalancer || topology.Servers[5].InBalancer ||
        !topology.Servers[1].InBalancer || !topology.Servers[2].InBalancer ||
        !topology.Servers[3].InBalancer || !topology.Servers[4].InBalancer {
        t.Fatalf("wrong eligibility/protocol exclusions: %+v",topology.Servers)
    }
    if topology.Servers[1].Tag!="sub-DE Frankfurt" ||
        topology.Servers[2].Tag!="sub-server.example.org" ||
        topology.Servers[3].Tag!="sub-203.0.113.4" {
        t.Fatalf("valid native server names unexpectedly changed: %+v",topology.Servers)
    }
    if topology.Servers[4].Tag!="Сервер 5" || topology.Servers[7].Tag!="Сервер 8" {
        t.Fatalf("credential-bearing tags exposed: %+v",topology.Servers)
    }
    server:=handlers(root)
    res:=httptest.NewRecorder()
    server.ServeHTTP(res,httptest.NewRequest(http.MethodGet,"/api/native/xray/topology",nil))
    if res.Code!=http.StatusOK {t.Fatalf("unexpected HTTP %d",res.Code)}
    for _,secret:=range []string{"PRIVATE-ENDPOINT","CREDENTIAL-DO-NOT-SHOW","12345678-1234","username:password","token=secret","settings","users","address"} {
        if bytes.Contains(res.Body.Bytes(),[]byte(secret)) {t.Fatalf("API leaked credentials/config field %q",secret)}
    }
    var parsed nativeTopology
    if err:=json.Unmarshal(res.Body.Bytes(),&parsed);err!=nil {t.Fatal(err)}
    if parsed.Runtime!="NOT_VERIFIED" {t.Fatalf("runtime falsely verified: %+v",parsed)}
    page:=httptest.NewRecorder()
    server.ServeHTTP(page,httptest.NewRequest(http.MethodGet,"/",nil))
    if page.Code!=http.StatusOK || !strings.Contains(page.Body.String(),"sub-server.example.org") ||
        !strings.Contains(page.Body.String(),"sub-203.0.113.4") ||
        !strings.Contains(page.Body.String(),"native-balanced") ||
        strings.Contains(page.Body.String(),"CREDENTIAL-DO-NOT-SHOW") {
        t.Fatal("browser topology is not rendered safely")
    }
    after,err:=os.ReadFile(filepath.Join(root,"etc/xray/configs/04_outbounds.json"))
    if err!=nil||!bytes.Equal(original,after) {t.Fatal("read-only request changed native Xray config")}
}

func TestNativeJSONCCommentsDoNotChangeStringLiterals(t *testing.T) {
    data:=[]byte(`{"outbounds":[{"tag":"https://example.org/path/*/x","protocol":"vless"}] /* safe */}`)
    stripped,err:=stripXrayComments(data)
    if err!=nil {t.Fatal(err)}
    var out struct{ Outbounds []outboundConfig }
    if err:=json.Unmarshal(stripped,&out);err!=nil {t.Fatal(err)}
    if out.Outbounds[0].Tag!="https://example.org/path/*/x" {t.Fatal("JSON string contents lost")}
    for _,bad:=range []string{`{"outbounds":[ /* unterminated`,`{"outbounds":"unfinished}`,`{"outbounds":[]}} trailing`} {
        if _,err:=stripXrayComments([]byte(bad));err==nil {t.Fatalf("invalid JSONC accepted: %q",bad)}
    }
}

func TestNativeXrayContradictoryConfigStopsWithoutPartialContent(t *testing.T) {
    cases:=[]struct{name,out,route string}{
        {"duplicate outbound",`{"outbounds":[{"tag":"same","protocol":"vless"},{"tag":"same","protocol":"vmess"}]}`,`{"routing":{"balancers":[]}}`},
        {"empty tag",`{"outbounds":[{"tag":"","protocol":"vless"}]}`,`{"routing":{"balancers":[]}}`},
        {"empty protocol",`{"outbounds":[{"tag":"sub-1"}]}`,`{"routing":{"balancers":[]}}`},
        {"bad selector",`{"outbounds":[]}`,`{"routing":{"balancers":[{"tag":"bal","selector":[""]}]}}`},
        {"missing routing",`{"outbounds":[]}`,`{"notRouting":{"balancers":[]}}`},
        {"invalid JSONC",`{"outbounds":[], /* broken`,`{"routing":{"balancers":[]}}`},
        {"missing outbounds",`{"other":[]}`,`{"routing":{"balancers":[]}}`},
    }
    for _,tc:=range cases {
        t.Run(tc.name,func(t *testing.T){
            root:=t.TempDir()
            writeFixture(t,root,"etc/xray/configs/04_outbounds.json",tc.out)
            writeFixture(t,root,"etc/xray/configs/05_routing.json",tc.route)
            got,err:=nativeXrayTopology(root)
            if err==nil || got.State!="STOP" || len(got.Servers)!=0 || len(got.Balancers)!=0 {
                t.Fatalf("contradictory native config not STOP: %+v err=%v",got,err)
            }
            page:=httptest.NewRecorder()
            handlers(root).ServeHTTP(page,httptest.NewRequest(http.MethodGet,"/api/native/xray/topology",nil))
            if page.Code!=http.StatusConflict || strings.Contains(page.Body.String(),"same") {
                t.Fatalf("STOP response leaked candidate config: %d %s",page.Code,page.Body.String())
            }
        })
    }
}

func TestNativeXrayMissingSymlinkOversizedAndHTMLSafe(t *testing.T) {
    root:=nativeFixture(t)
    configDir:=filepath.Join(root,"etc/xray/configs")
    outFile:=filepath.Join(configDir,"04_outbounds.json")
    elsewhere:=filepath.Join(t.TempDir(),"outside")
    if err:=os.WriteFile(elsewhere,[]byte(`{"outbounds":[]}`),0600);err!=nil {t.Fatal(err)}
    if err:=os.Remove(outFile);err!=nil {t.Fatal(err)}
    if err:=os.Symlink(elsewhere,outFile);err!=nil {t.Skip(err)}
    if got,err:=nativeXrayTopology(root);err==nil || got.State!="STOP" {t.Fatalf("symlink Xray config accepted: %+v",got)}
    if err:=os.Remove(outFile);err!=nil {t.Fatal(err)}
    if err:=os.WriteFile(outFile,bytes.Repeat([]byte("x"),int(nativeConfigMaxBytes)+1),0600);err!=nil {t.Fatal(err)}
    if got,err:=nativeXrayTopology(root);err==nil || got.State!="STOP" {t.Fatalf("oversize Xray config accepted: %+v",got)}
    if err:=os.WriteFile(outFile,[]byte(`{"outbounds":[{"tag":"A&B","protocol":"vless"}]}`),0600);err!=nil {t.Fatal(err)}
    page:=httptest.NewRecorder()
    handlers(root).ServeHTTP(page,httptest.NewRequest(http.MethodGet,"/",nil))
    if !strings.Contains(page.Body.String(),"A&amp;B") || strings.Contains(page.Body.String(),"A&B</span>") {
        t.Fatal("template did not HTML-escape native tag")
    }
}

func TestNoBalancerIsConfigOnlyNotLiveEvidence(t *testing.T) {
    root:=nativeFixture(t)
    if err:=os.WriteFile(filepath.Join(root,"etc/xray/configs/05_routing.json"),[]byte(`{"routing":{"rules":[]}}`),0600);err!=nil {t.Fatal(err)}
    got,err:=nativeXrayTopology(root)
    if err!=nil || got.State!="NO_BALANCER" || got.Runtime!="NOT_VERIFIED" {
        t.Fatalf("no-balancer config incorrectly reported as runtime success: %+v %v",got,err)
    }
}
