package main

import (
 "crypto/tls"
 "encoding/json"
 "net/http"
 "net/http/httptest"
 "os"
 "path/filepath"
 "strings"
 "testing"
 "time"
)

func connectorFixture(t *testing.T) (*app,*http.ServeMux) {
 t.Helper()
 dir:=t.TempDir()
 a:=&app{cfg:config{ConfigPath:filepath.Join(dir,"freenet.conf")}}
 mux:=http.NewServeMux();registerAdminDiagnostics(mux,a)
 return a,mux
}
func connectorRequest(method,endpoint,body string)*http.Request{
 req:=httptest.NewRequest(method,"http://localhost:1001"+endpoint,strings.NewReader(body))
 req.RemoteAddr="127.0.0.1:5555"
 if body!="" {req.Header.Set("Content-Type","application/json")}
 if method=="POST"{req.Header.Set("Origin","http://localhost:1001")}
 return req
}
func connectorPair(t *testing.T,a *app)string{
 t.Helper()
 w:=httptest.NewRecorder()
 a.handleConnectorPair(w,connectorRequest("POST","/api/admin/connector/pair",`{"confirm":true}`))
 if w.Code!=200{t.Fatalf("pair: %d %s",w.Code,w.Body.String())}
 var out map[string]any
 if err:=json.Unmarshal(w.Body.Bytes(),&out);err!=nil{t.Fatal(err)}
 token,ok:=out["token"].(string)
 if !ok||len(token)!=64{t.Fatal("pair did not return one valid token")}
 if out["transport"]!="LOCAL_LOOPBACK_ONLY"{t.Fatal("machine transport is not restricted")}
 if w.Header().Get("Cache-Control")!="no-store"{t.Fatal("token response cacheable")}
 return token
}
func connectorMachine(a *app,token string)*httptest.ResponseRecorder{
 w:=httptest.NewRecorder()
 req:=connectorRequest("GET","/api/connector/diagnostics?tool=get_recent_journal","")
 if token!=""{req.Header.Set("Authorization","Bearer "+token)}
 a.handleConnectorMachineDiagnostics(w,req)
 return w
}

func TestConnectorGrantPairRotateRevokeAndAudit(t *testing.T){
 a,_:=connectorFixture(t)
 if w:=connectorMachine(a,"");w.Code!=401{t.Fatalf("machine API default-open: %d",w.Code)}
 first:=connectorPair(t,a)
 info,err:=os.Lstat(a.connectorGrantPath())
 if err!=nil||info.Mode().Perm()!=0600{t.Fatal("credential store not 0600 regular")}
 bytes,err:=os.ReadFile(a.connectorGrantPath());if err!=nil{t.Fatal(err)}
 if strings.Contains(string(bytes),first){t.Fatal("plaintext bearer token persisted")}
 if w:=connectorMachine(a,first);w.Code!=200{t.Fatalf("valid localhost machine token failed: %d %s",w.Code,w.Body.String())}
 grant,ok,err:=a.readConnectorGrant()
 if err!=nil||!ok||grant.LastUsedAt.IsZero(){t.Fatal("machine access audit timestamp missing")}
 second:=connectorPair(t,a)
 if first==second{t.Fatal("grant was reused on rotation")}
 if w:=connectorMachine(a,first);w.Code!=401{t.Fatal("old grant accepted after rotation")}
 if w:=connectorMachine(a,second);w.Code!=200{t.Fatal("replacement grant rejected")}
 status:=httptest.NewRecorder()
 a.handleConnectorGrantStatus(status,connectorRequest("GET","/api/admin/connector/grant",""))
 if status.Code!=200||strings.Contains(status.Body.String(),second)||strings.Contains(status.Body.String(),sessionDigest(second)){t.Fatal("status disclosed secret")}
 revoke:=httptest.NewRecorder()
 a.handleConnectorRevoke(revoke,connectorRequest("POST","/api/admin/connector/revoke",`{"confirm":true}`))
 if revoke.Code!=200{t.Fatalf("revoke failed: %d %s",revoke.Code,revoke.Body.String())}
 if w:=connectorMachine(a,second);w.Code!=401{t.Fatal("revoked grant retained access")}
 if _,err:=os.Stat(a.connectorGrantPath());!os.IsNotExist(err){t.Fatal("grant file survived revocation")}
}

func TestConnectorGrantExpiredCorruptAndUnsafeState(t *testing.T){
 a,_:=connectorFixture(t)
 token:=connectorPair(t,a)
 grant,ok,err:=a.readConnectorGrant()
 if !ok||err!=nil{t.Fatal(err)}
 grant.IssuedAt=time.Now().Add(-connectorGrantTTL-time.Minute)
 grant.ExpiresAt=grant.IssuedAt.Add(connectorGrantTTL)
 if err:=a.saveConnectorGrant(grant);err!=nil{t.Fatal(err)}
 if w:=connectorMachine(a,token);w.Code!=401{t.Fatal("expired token accepted")}
 if err:=os.Chmod(a.connectorGrantPath(),0644);err!=nil{t.Fatal(err)}
 if w:=connectorMachine(a,token);w.Code!=401{t.Fatal("unsafe chmod bypassed")}
 broken:=httptest.NewRecorder()
 a.handleConnectorPair(broken,connectorRequest("POST","/api/admin/connector/pair",`{"confirm":true}`))
 if broken.Code!=503{t.Fatalf("unsafe file overwritten: %d",broken.Code)}
 if err:=os.Remove(a.connectorGrantPath());err!=nil{t.Fatal(err)}
 if err:=os.Symlink("/dev/null",a.connectorGrantPath());err!=nil{t.Fatal(err)}
 if w:=connectorMachine(a,token);w.Code!=401{t.Fatal("credential symlink accepted")}
 revoke:=httptest.NewRecorder()
 a.handleConnectorRevoke(revoke,connectorRequest("POST","/api/admin/connector/revoke",`{"confirm":true}`))
 if revoke.Code!=503{t.Fatal("revoke should refuse unsafe symlink")}
}

func TestConnectorMachineRefusesCookieWANHostOrTokenQuery(t *testing.T){
 a,_:=connectorFixture(t);token:=connectorPair(t,a)
 cases:=[]struct{name string;alter func(*http.Request)}{
 {"remote address",func(r *http.Request){r.RemoteAddr="192.0.2.10:1234"}},
 {"public host",func(r *http.Request){r.Host="public.example.com"}},
 {"cookie",func(r *http.Request){r.Header.Set("Cookie","freenet_session=legacy")}},
 {"url token",func(r *http.Request){r.URL.RawQuery="tool=get_status&token=abc"}},
 {"wrong token",func(r *http.Request){r.Header.Set("Authorization","Bearer "+strings.Repeat("a",64))}},
 }
 for _,tc:=range cases{
  t.Run(tc.name,func(t *testing.T){
   req:=connectorRequest("GET","/api/connector/diagnostics?tool=get_recent_journal","")
   req.Header.Set("Authorization","Bearer "+token);tc.alter(req)
   w:=httptest.NewRecorder();a.handleConnectorMachineDiagnostics(w,req)
   if w.Code!=401{t.Fatalf("machine API exposed to %s: %d",tc.name,w.Code)}
   if strings.Contains(w.Body.String(),token){t.Fatal("failed access leaked token")}
  })
 }
}

func TestConnectorPairRequiresAdminAuthAndRealSecureOrigin(t *testing.T){
 a,mux:=connectorFixture(t)
 for _,route:=range []string{"/api/admin/connector/pair","/api/admin/connector/revoke"}{
  w:=httptest.NewRecorder()
  mux.ServeHTTP(w,connectorRequest("POST",route,`{"confirm":true}`))
  if w.Code!=401{t.Fatalf("anonymous %s accepted: %d",route,w.Code)}
 }
 get:=httptest.NewRecorder()
 mux.ServeHTTP(get,connectorRequest("GET","/api/admin/connector/grant",""))
 if get.Code!=401{t.Fatal("anonymous grant state exposed")}
 req:=connectorRequest("POST","/api/admin/connector/pair",`{"confirm":true}`)
 req.RemoteAddr="192.0.2.10:1234";req.Header.Set("X-Forwarded-Proto","https")
 w:=httptest.NewRecorder();a.handleConnectorPair(w,req)
 if w.Code!=403{t.Fatal("untrusted spoofed X-Forwarded-Proto accepted")}
 req.TLS=&tls.ConnectionState{} // direct transport TLS can issue grants
 w=httptest.NewRecorder();a.handleConnectorPair(w,req)
 if w.Code!=200{t.Fatalf("TLS pairing rejected: %d",w.Code)}
 req=connectorRequest("POST","/api/admin/connector/pair",`{"confirm":true}`)
 req.Header.Set("Origin","https://attacker.invalid")
 w=httptest.NewRecorder();a.handleConnectorPair(w,req)
 if w.Code!=403{t.Fatal("cross-origin pairing accepted")}
 req=connectorRequest("POST","/api/admin/connector/pair",`{"confirm":false}`)
 w=httptest.NewRecorder();a.handleConnectorPair(w,req)
 if w.Code!=400{t.Fatal("unconfirmed pairing accepted")}
}

func TestConnectorMachineLocalAuditRateLimit(t *testing.T){
 a,_:=connectorFixture(t);token:=connectorPair(t,a)
 for i:=0;i<60;i++{
  if w:=connectorMachine(a,token);w.Code!=200{t.Fatalf("call %d: %d",i,w.Code)}
 }
 if w:=connectorMachine(a,token);w.Code!=429{t.Fatal("rate limit ineffective")}
}
