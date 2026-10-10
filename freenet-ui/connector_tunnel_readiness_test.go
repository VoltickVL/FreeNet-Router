package main

import (
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "os"
    "path/filepath"
    "strings"
    "testing"
)

func TestConnectorTunnelPreflightArchitecturesAndListener(t *testing.T) {
    cases := []struct {
        os, arch, listen, wantArch, wantNext, wantURL string
        ready bool
    }{
        {"linux","arm64","192.168.50.1:1001","OFFICIAL_BINARY_ARCH_SUPPORTED","PLATFORM_TUNNEL_AND_CLIENT_SETUP","http://127.0.0.1:1001/mcp",true},
        {"linux","amd64","192.168.50.1:1001","OFFICIAL_BINARY_ARCH_SUPPORTED","PLATFORM_TUNNEL_AND_CLIENT_SETUP","http://127.0.0.1:1001/mcp",true},
        {"linux","mipsle","192.168.50.1:1001","OFFICIAL_BINARY_UNAVAILABLE","TRUSTED_EXTERNAL_HOST_AND_SECURE_BRIDGE_REQUIRED","http://127.0.0.1:1001/mcp",true},
        {"linux","mips","192.168.50.1:1001","OFFICIAL_BINARY_UNAVAILABLE","TRUSTED_EXTERNAL_HOST_AND_SECURE_BRIDGE_REQUIRED","http://127.0.0.1:1001/mcp",true},
        {"linux","arm","192.168.50.1:1001","OFFICIAL_BINARY_UNAVAILABLE","TRUSTED_EXTERNAL_HOST_AND_SECURE_BRIDGE_REQUIRED","http://127.0.0.1:1001/mcp",true},
        {"darwin","arm64","127.0.0.1:1001","EXTERNAL_TRUSTED_HOST_REQUIRED","VERIFY_TRUSTED_BRIDGE","http://127.0.0.1:1001/mcp",true},
        {"linux","arm64","broken","OFFICIAL_BINARY_ARCH_SUPPORTED","VERIFY_LOOPBACK_LISTENER","",false},
        {"linux","arm64","0.0.0.0:1001","OFFICIAL_BINARY_ARCH_SUPPORTED","VERIFY_LOOPBACK_LISTENER","",false},
    }
    for _,tt:=range cases {
        r:=connectorReadinessFor(tt.os,tt.arch,tt.listen)
        if r.ArchitectureStatus!=tt.wantArch || r.NextStep!=tt.wantNext || r.LoopbackMCP!=tt.wantURL || r.MCPReadyOnRouter!=tt.ready {
            t.Fatalf("%s/%s %q: %+v",tt.os,tt.arch,tt.listen,r)
        }
        if !r.Success || r.Mutation!="NONE" || r.ExternalConnected || r.ExternalEvidence!="NOT_OBSERVED" {
            t.Fatalf("false claim of live tunnel/mutation: %+v",r)
        }
        if strings.Contains(r.Notes,"sk-") || strings.Contains(r.Notes,"token=") { t.Fatal("secret-like output") }
    }
}

func TestConnectorPreflightAuthorizedReadOnlyNoSecrets(t *testing.T) {
    a,mux:=connectorFixture(t)
    a.cfg.Listen="192.168.50.1:1001"
    w:=httptest.NewRecorder()
    mux.ServeHTTP(w,connectorRequest("GET","/api/admin/connector/readiness",""))
    if w.Code==200 {t.Fatal("anonymous readiness accepted")}
    w=httptest.NewRecorder()
    a.handleConnectorTunnelReadiness(w,connectorRequest("GET","/api/admin/connector/readiness",""))
    if w.Code!=200 {t.Fatalf("readiness unavailable: %d %s",w.Code,w.Body.String())}
    var result map[string]any
    if err:=json.Unmarshal(w.Body.Bytes(),&result);err!=nil{t.Fatal(err)}
    if result["mutation"]!="NONE" || result["external_connected"]!=false || result["grant_active"]!=false {t.Fatalf("unsafe claims: %+v",result)}
    if w.Header().Get("Cache-Control")!="no-store"{t.Fatal("readiness cached")}
    before,err:=os.ReadDir(filepath.Dir(a.cfg.ConfigPath))
    if err!=nil {t.Fatal(err)}
    if len(before)!=0 {t.Fatal("preflight created private files")}
    secret:=connectorPair(t,a)
    w=httptest.NewRecorder()
    a.handleConnectorTunnelReadiness(w,connectorRequest("GET","/api/admin/connector/readiness",""))
    if w.Code!=200 {t.Fatal("active state unavailable")}
    if strings.Contains(w.Body.String(),secret) || strings.Contains(w.Body.String(),sessionDigest(secret)) {t.Fatal("readiness exposed token/hash")}
    var active map[string]any
    if err:=json.Unmarshal(w.Body.Bytes(),&active);err!=nil{t.Fatal(err)}
    if active["grant_active"]!=true {t.Fatal("active grant not reported")}
    w=httptest.NewRecorder()
    a.handleConnectorTunnelReadiness(w,connectorRequest("GET","/api/admin/connector/readiness?secret=abc",""))
    if w.Code!=http.StatusBadRequest {t.Fatal("preflight accepted query inputs")}
    if err:=os.Chmod(a.connectorGrantPath(),0644);err!=nil {t.Fatal(err)}
    w=httptest.NewRecorder()
    a.handleConnectorTunnelReadiness(w,connectorRequest("GET","/api/admin/connector/readiness",""))
    if w.Code!=503 || strings.Contains(w.Body.String(),secret){t.Fatal("unsafe grant store ignored or disclosed")}
}
