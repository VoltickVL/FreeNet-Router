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

func TestConnectorDiagnosticContractStrictReadOnly(t *testing.T) {
 dir:=t.TempDir()
 t.Setenv("FREENET_ROUTING_CONFIG_DIR",dir)
 if err:=os.WriteFile(filepath.Join(dir,"05_routing.json"),[]byte(`{"routing":{"rules":[{"type":"field","domain":["full:sg.api.io.mi.com"],"outboundTag":"direct"}]}}`),0600);err!=nil {t.Fatal(err)}
 if err:=os.WriteFile(filepath.Join(dir,"04_outbounds.json"),[]byte(`{"outbounds":[{"tag":"direct","protocol":"freedom","settings":{"private":"SECRET-MUST-NOT-LEAK"}}]}`),0600);err!=nil {t.Fatal(err)}
 a:=&app{}
 tests:=[]struct{ query string;code int }{
  {"tool=get_recent_journal",200},
  {"tool=test_route&host=sg.api.io.mi.com",200},
  {"tool=get_status&host=sg.api.io.mi.com",400},
  {"tool=run_shell",400},
  {"tool=test_route&host=router.lan",400},
  {"tool=get_recent_journal&tool=get_status",400},
  {"tool=get_recent_journal&unexpected=1",400},
 }
 for _,tc:=range tests {
  w:=httptest.NewRecorder()
  a.handleConnectorDiagnostics(w,httptest.NewRequest(http.MethodGet,"http://router/api/admin/connector/diagnostics?"+tc.query,nil))
  if w.Code!=tc.code {t.Fatalf("%s: got %d want %d body=%s",tc.query,w.Code,tc.code,w.Body.String())}
  if strings.Contains(w.Body.String(),"SECRET-MUST-NOT-LEAK")||strings.Contains(w.Body.String(),"private_key")||strings.Contains(w.Body.String(),"vless://") {t.Fatalf("secret escaped: %s",tc.query)}
  if w.Header().Get("Cache-Control")!="no-store" {t.Fatalf("%s: not cache-safe",tc.query)}
  var body map[string]any
  if err:=json.Unmarshal(w.Body.Bytes(),&body);err!=nil {t.Fatal(err)}
  if body["mutation"]!="NONE" {t.Fatalf("mutation contract changed: %s",tc.query)}
  if tc.query=="tool=test_route&host=sg.api.io.mi.com" {
   if body["expected_action"]!="DIRECT"||body["observed_route"]!="NOT_OBSERVED"||body["runtime_route_observed"]!=false {t.Fatalf("invalid evidence separation: %v",body)}
   if _,ok:=body["outbound_tag"];ok {t.Fatal("raw outbound tag escaped")}
  }
  if tc.query=="tool=get_recent_journal" && body["available"]!=false {t.Fatal("unimplemented journal falsely claimed available")}
 }
}

func TestConnectorDiagnosticEndpointRequiresAuthentication(t *testing.T) {
 mux:=http.NewServeMux();a:=&app{};registerAdminDiagnostics(mux,a)
 w:=httptest.NewRecorder()
 mux.ServeHTTP(w,httptest.NewRequest(http.MethodGet,"http://router/api/admin/connector/diagnostics?tool=get_status",nil))
 if w.Code==200 {t.Fatal("connector diagnostics accessible without login")}
}
