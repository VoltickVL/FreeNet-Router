package main

import (
 "context"
 "fmt"
 "net/http"
 "net/http/httptest"
 "os"
 "os/exec"
 "strings"
 "testing"
)

func TestTunnelHealthProjectionIsBoundedAndSecretFree(t *testing.T) {
 secret:="sk-"+strings.Repeat("s",45)
 payload:=fmt.Sprintf(`{
  "schema_version":1,"ready":false,"snapshot_at":"2026-10-10T00:00:00Z",
  "runtime":{"instance_id":"tunnel_secret","version":"v0.0.16"},
  "components":{
   "control-plane":{"status":"degraded","state":"backoff","reason_code":"http_error",
      "details":{"failure_category":"http_error","http_status":403,"consecutive_failures":7,
        "url":"https://private.example/%s","error":"%s"}},
   "mcp":{"status":"unknown","state":"starting",
      "details":{"startup_probe":{"state":"pending"},"tool_names":["secret"]}},
   "oauth":{"status":"disabled","state":"disabled"},
   "response-delivery":{"status":"unknown","state":"not_observed"},
   "proxy":{"status":"degraded","state":"my_secret","details":{"raw_key":"%s"}}
  }, "access_token":"%s"
 }`,secret,secret,secret,secret)
 result,ok:=connectorTunnelParseHealth([]byte(payload))
 if !ok || result.HealthState!="OBSERVED" || result.ClientReady || result.ChatGPTConnected{
  t.Fatalf("invalid health interpretation: %+v",result)
 }
 if result.ControlPlane.Status!="degraded" || result.ControlPlane.State!="backoff" ||
    result.ControlPlane.ReasonCode!="http_error" ||result.ControlPlane.FailureCategory!="http_error"||
    result.ControlPlane.HTTPStatus!=403||result.ControlPlane.ConsecutiveFailures!=7{
  t.Fatalf("lost structured upstream control-plane diagnosis: %+v",result.ControlPlane)
 }
 if result.MCP.StartupProbe!="pending"||result.MCP.State!="starting"{
  t.Fatalf("lost upstream MCP startup state: %+v",result.MCP)
 }
 w:=httptest.NewRecorder()
 writeJSON(w,http.StatusOK,result)
 encoded:=w.Body.String()
 for _,bad:=range []string{secret,"https://private.example","tunnel_secret","raw_key","access_token","tool_names","my_secret"} {
  if strings.Contains(encoded,bad){t.Fatalf("secret/raw upstream property leaked: %s",bad)}
 }
}

func TestTunnelHealthRejectsMalformedOrUnexpectedSchemas(t *testing.T) {
 cases:=[]string{"", "{}", "null", "{", `{"schema_version":2,"ready":true,"components":{}}`,
  `{"schema_version":1,"ready":true}`,`{"schema_version":1,"components":{}}`}
 for _,body:=range cases{
  got,ok:=connectorTunnelParseHealth([]byte(body))
  if ok||got.HealthState!="INVALID_HEALTH"||got.ClientReady||got.ChatGPTConnected{
   t.Fatalf("unexpected readiness from bad health document: %q %+v",body,got)
  }
 }
 big:=[]byte(strings.Repeat("X",connectorTunnelHealthMaxBytes+1))
 if _,ok:=connectorTunnelParseHealth(big);ok{t.Fatal("oversized payload accepted")}
 for _,bad:=range []string{"sk-"+strings.Repeat("q",35),"Bearer_credential","tunnel_0123456789abcdef","http://private/path","some_value\nkey"}{
  if connectorTunnelSafeCode(bad)!="OTHER_REDACTED"{t.Fatalf("unsafe code survived: %q",bad)}
 }
}

func TestTunnelHealthLocalReadOnlyHTTPBoundaries(t *testing.T){
 t.Run("valid not ready",func(t *testing.T){
  srv:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
   if r.Method!=http.MethodGet{t.Fatal("unexpected mutation HTTP method")}
   w.Header().Set("Content-Type","application/json")
   _,_=w.Write([]byte(`{"schema_version":1,"ready":false,"components":{"control-plane":{"status":"degraded","state":"backoff","details":{"http_status":401,"failure_category":"http_error"}}}}`))
  }))
  defer srv.Close()
  result:=connectorTunnelProbeHealth(context.Background(),srv.URL)
  if result.HealthState!="OBSERVED"||result.ClientReady||result.ControlPlane.HTTPStatus!=401{
   t.Fatalf("wrong safe diagnosis: %+v",result)
  }
 })
 t.Run("refuse redirects",func(t *testing.T){
  destination:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
   t.Error("health probe followed HTTP redirect")
  }))
  defer destination.Close()
  source:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
   http.Redirect(w,r,destination.URL,http.StatusFound)
  }))
  defer source.Close()
  got:=connectorTunnelProbeHealth(context.Background(),source.URL)
  if got.HealthState!="HTTP_UNAVAILABLE"||got.ClientReady{t.Fatal("redirect treated as ready")}
 })
 t.Run("unreachable",func(t *testing.T){
  got:=connectorTunnelProbeHealth(context.Background(),"http://127.0.0.1:0/health?details=true")
  if got.HealthState!="UNAVAILABLE"||got.ClientReady{t.Fatal("failed localhost health promoted to ready")}
 })
 t.Run("large body",func(t *testing.T){
  srv:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
   _,_=w.Write([]byte(strings.Repeat("a",connectorTunnelHealthMaxBytes+4)))
  }))
  defer srv.Close()
  got:=connectorTunnelProbeHealth(context.Background(),srv.URL)
  if got.HealthState!="INVALID_HEALTH"||got.ClientReady{t.Fatal("oversized response was accepted")}
 })
}

func TestTunnelHealthRequiresAdminAndNeverMutatesClient(t *testing.T){
 a,mux:=connectorFixture(t)
 req:=connectorRequest(http.MethodGet,"/api/admin/connector/tunnel/health","")
 unauth:=httptest.NewRecorder()
 mux.ServeHTTP(unauth,req)
 if unauth.Code==http.StatusOK{t.Fatal("anonymous caller can read component health")}
 w:=httptest.NewRecorder()
 a.handleTunnelHealthDiagnostics(w,connectorRequest(http.MethodGet,"/api/admin/connector/tunnel/health",""))
 if w.Code!=200||!strings.Contains(w.Body.String(),"NOT_RUNNING"){t.Fatal("read-only no-process state unavailable")}
 a.connectorTunnelCmd=&exec.Cmd{};a.connectorTunnelState="STARTING"
 w=httptest.NewRecorder()
 a.handleTunnelHealthDiagnostics(w,connectorRequest(http.MethodGet,"/api/admin/connector/tunnel/health?url=http://evil.invalid",""))
 if w.Code!=http.StatusBadRequest{t.Fatal("user-provided probe target accepted")}
 // No installer, credentials, OpenAI client or routing file is touched.
 sentinel:=t.TempDir()+"/config.json"
 if err:=os.WriteFile(sentinel,[]byte("live-xray"),0600);err!=nil{t.Fatal(err)}
 w=httptest.NewRecorder()
 a.handleTunnelHealthDiagnostics(w,connectorRequest(http.MethodGet,"/api/admin/connector/tunnel/health",""))
 if w.Code!=200||strings.Contains(w.Body.String(),"sk-"){t.Fatal("unexpected health output or credential leak")}
 raw,err:=os.ReadFile(sentinel)
 if err!=nil||string(raw)!="live-xray"{t.Fatal("probe mutated router data")}
}
