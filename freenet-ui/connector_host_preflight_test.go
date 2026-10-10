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

func TestConnectorHostMemAvailableStrict(t *testing.T) {
    tests := []struct{name, body string; value uint64; ok bool}{
        {"linux arm64", "MemTotal: 524288 kB\nMemAvailable: 245760 kB\n", 245760*1024, true},
        {"missing", "MemTotal: 524288 kB\nMemFree: 120000 kB\n",0,false},
        {"duplicate", "MemAvailable: 100 kB\nMemAvailable: 200 kB\n",0,false},
        {"zero", "MemAvailable: 0 kB\n",0,false},
        {"negative", "MemAvailable: -5 kB\n",0,false},
        {"bad unit", "MemAvailable: 22 MB\n",0,false},
        {"extra", "MemAvailable: 22 kB extra\n",0,false},
        {"overflow", "MemAvailable: 18446744073709551615 kB\n",0,false},
        {"not number", "MemAvailable: bad kB\n",0,false},
    }
    for _,tc := range tests {
        t.Run(tc.name,func(t *testing.T){
            got,ok:=connectorAvailableMemory([]byte(tc.body))
            if got!=tc.value || ok!=tc.ok {t.Fatalf("got %v,%v; want %v,%v",got,ok,tc.value,tc.ok)}
        })
    }
    if _,ok:=connectorAvailableMemory([]byte(strings.Repeat("X",connectorHostMeminfoLimit+1)));ok{
        t.Fatal("oversized meminfo was accepted")
    }
}

func TestConnectorHostPreflightNoFalseConnectionOrMutation(t *testing.T) {
    a,mux:=connectorFixture(t)
    dir:=t.TempDir()
    dest:=filepath.Join(dir,"freenet-tunnel-client")
    t.Setenv("FREENET_TUNNEL_INSTALL_PATH",dest)
    sentinel:=filepath.Join(dir,"working-xray-config.json")
    if err:=os.WriteFile(sentinel,[]byte("untouched-live-config"),0600);err!=nil{t.Fatal(err)}
    mem:=[]byte("MemAvailable: 245760 kB\n")
    p:=connectorHostPreflightFor("linux","arm64","192.168.50.1:1001",dest,mem)
    if !p.Success || p.Mutation!="NONE" || p.LocalMCP!="CONFIG_ONLY" ||
        p.Candidate!="ABSENT" || p.MemoryEvidence!="PROC_MEMINFO" ||
        p.MemoryAvailableBytes!=245760*1024 || p.ExecutableVerified ||
        p.OpenAIConnected || p.ChatGPTConnected || p.NetworkEvidence!="NOT_TESTED"{
        t.Fatalf("preflight inflated runtime evidence: %+v",p)
    }
    if p.Preconditions=="HOST_FACTS_OBSERVED" && (p.StorageExec!="EXEC_FLAG_ALLOWED" || p.FreeBytes<tunnelMinFree) {
        t.Fatal("readiness bypasses storage check")
    }
    bad:=connectorHostPreflightFor("linux","mipsle","192.168.50.1:1001",dest,mem)
    if bad.Preconditions!="STOP" || bad.OpenAIConnected {t.Fatal("unsupported architecture passed")}
    bad=connectorHostPreflightFor("linux","arm64","192.168.50.1:1001",dest,nil)
    if bad.Preconditions!="STOP" || bad.MemoryEvidence!="UNKNOWN" {t.Fatal("unknown RAM permitted")}
    if err:=os.Symlink(sentinel,dest);err!=nil{t.Fatal(err)}
    bad=connectorHostPreflightFor("linux","arm64","192.168.50.1:1001",dest,mem)
    if bad.Candidate!="UNSAFE_FILE_TYPE" || bad.Preconditions!="STOP" {t.Fatal("unsafe symlink permitted")}
    if err:=os.Remove(dest);err!=nil{t.Fatal(err)}

    // The public route must retain requireAuth; bypassing it would leak host
    // details to unauthenticated users over a reverse proxy.
    unauth:=httptest.NewRecorder()
    mux.ServeHTTP(unauth,connectorRequest(http.MethodGet,"/api/admin/connector/host-preflight",""))
    if unauth.Code==http.StatusOK {t.Fatal("anonymous host preflight available")}
    w:=httptest.NewRecorder()
    a.handleConnectorHostPreflight(w,connectorRequest(http.MethodGet,"/api/admin/connector/host-preflight",""))
    if w.Code!=200{t.Fatalf("preflight failed: %d",w.Code)}
    var result connectorHostPreflight
    if err:=json.Unmarshal(w.Body.Bytes(),&result);err!=nil{t.Fatal(err)}
    if result.Mutation!="NONE" || result.NetworkEvidence!="NOT_TESTED" ||
        result.ExecutableVerified || result.OpenAIConnected || result.ChatGPTConnected {
        t.Fatal("preflight implies a tunnel connection")
    }
    w=httptest.NewRecorder()
    a.handleConnectorHostPreflight(w,connectorRequest(http.MethodGet,"/api/admin/connector/host-preflight?cmd=id",""))
    if w.Code!=http.StatusBadRequest{t.Fatal("unexpected user arguments accepted")}
    data,err:=os.ReadFile(sentinel)
    if err!=nil || string(data)!="untouched-live-config"{t.Fatal("preflight mutated file")}
    if _,err:=os.Lstat(dest);!os.IsNotExist(err){t.Fatal("preflight installed a candidate")}
}
