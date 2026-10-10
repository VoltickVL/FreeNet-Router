package main

import (
 "encoding/json"
 "context"
 "net"
 "net/http"
 "net/http/httptest"
 "os"
 "os/exec"
 "path/filepath"
 "strings"
 "testing"
 "time"
)

func connectorTestTransport(t *testing.T,a *app) string {
 t.Helper()
 if err:=a.connectorPrivateDir();err!=nil{t.Fatal(err)}
 id:="tunnel_"+strings.Repeat("a",32)
 payload,_:=json.Marshal(connectorTunnelProfile{Version:1,TunnelID:id,Enabled:false})
 if err:=atomicWrite(a.connectorTunnelProfilePath(),payload,0600);err!=nil{t.Fatal(err)}
 if err:=atomicWrite(a.connectorTransportKeyPath(),[]byte("sk-"+strings.Repeat("x",40)),0600);err!=nil{t.Fatal(err)}
 token:=strings.Repeat("a",64)
 if err:=atomicWrite(a.connectorTransportHeaderPath(),[]byte("Bearer "+token),0600);err!=nil{t.Fatal(err)}
 return token
}

func TestRemoteMCPRequiresBrowserApprovalAndNeverWritesRouter(t *testing.T){
 a,_:=connectorFixture(t)
 token:=connectorTestTransport(t,a)
 a.connectorTunnelCmd=&exec.Cmd{}
 a.connectorTunnelState="STARTING"
 code,_,_:=mcpTestRPC(t,a,token,"tools/call",map[string]any{"name":"get_status","arguments":map[string]any{}})
 if code!=http.StatusForbidden{t.Fatalf("remote diagnostics before approval: %d",code)}
 code,r,_:=mcpTestRPC(t,a,token,"tools/call",map[string]any{"name":"request_access","arguments":map[string]any{}})
 if code!=200{t.Fatalf("cannot request approval: %d",code)}
 data:=r["result"].(map[string]any)["structuredContent"].(map[string]any)
 if data["state"]!="PENDING"{t.Fatal("request did not stay pending")}
 requestID:=data["request_id"].(string)
 // No LAN secret, cookie or SSH command can be smuggled into the machine tool.
 code,_,_=mcpTestRPC(t,a,token,"tools/call",map[string]any{"name":"request_access","arguments":map[string]any{"command":"id"}})
 if code!=http.StatusBadRequest{t.Fatal("request accepted arbitrary arguments")}
 code,_,_=mcpTestRPC(t,a,token,"tools/call",map[string]any{"name":"get_status","arguments":map[string]any{}})
 if code!=http.StatusForbidden{t.Fatal("pending approval exposed runtime status")}
 req:=connectorRequest("POST","/api/admin/connector/access/approve",`{"confirm":true,"request_id":"`+requestID+`","approve":true}`)
 w:=httptest.NewRecorder();a.handleConnectorRemoteApprove(w,req)
 if w.Code!=200{t.Fatalf("approval failed: %d %s",w.Code,w.Body.String())}
 code,_,_=mcpTestRPC(t,a,token,"tools/call",map[string]any{"name":"get_status","arguments":map[string]any{}})
 if code!=200{t.Fatalf("approved diagnostics unavailable: %d",code)}
 // Replay of an already-approved request may never renew the window.
 w=httptest.NewRecorder();a.handleConnectorRemoteApprove(w,connectorRequest("POST","/api/admin/connector/access/approve",`{"confirm":true,"request_id":"`+requestID+`","approve":true}`))
 if w.Code!=http.StatusConflict{t.Fatalf("approval replay prolonged session: %d",w.Code)}
 a.connectorRemoteRevoke()
 code,_,_=mcpTestRPC(t,a,token,"tools/call",map[string]any{"name":"get_status","arguments":map[string]any{}})
 if code!=http.StatusForbidden{t.Fatal("revoked access still authorized")}
 // Restart loses in-memory session even if the machine key remains on disk.
 restarted:=&app{cfg:a.cfg}
 code,_,_=mcpTestRPC(t,restarted,token,"tools/call",map[string]any{"name":"get_status","arguments":map[string]any{}})
 if code!=http.StatusForbidden{t.Fatal("restart retained approved access")}
}

func TestRemoteMCPRejectsUnsafeMachineHeadersAndExpiry(t *testing.T){
 a,_:=connectorFixture(t)
 token:=connectorTestTransport(t,a)
 mk:=func()*http.Request {return mcpTestRequest(t,token,[]byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))}
 checks:=[]func(*http.Request){
  func(r *http.Request){r.RemoteAddr="192.0.2.10:2121"},
  func(r *http.Request){r.Host="freenet.example.com"},
  func(r *http.Request){r.Header.Set("Cookie","x=y")},
  func(r *http.Request){r.URL.RawQuery="token=xxx"},
  func(r *http.Request){r.Header.Add("Authorization","Bearer "+token)},
 }
 for _,alter:=range checks {req:=mk();alter(req);w:=httptest.NewRecorder();a.handleConnectorMCP(w,req);if w.Code!=401{t.Fatalf("machine boundary bypass: %d",w.Code)}}
 if err:=os.Chmod(a.connectorTransportHeaderPath(),0644);err!=nil{t.Fatal(err)}
 w:=httptest.NewRecorder();a.handleConnectorMCP(w,mk());if w.Code!=401{t.Fatal("world readable transport token permitted")}
 if err:=os.Chmod(a.connectorTransportHeaderPath(),0600);err!=nil{t.Fatal(err)}
 if err:=os.Remove(a.connectorTransportHeaderPath());err!=nil{t.Fatal(err)}
 if err:=os.Symlink("/dev/null",a.connectorTransportHeaderPath());err!=nil{t.Fatal(err)}
 w=httptest.NewRecorder();a.handleConnectorMCP(w,mk());if w.Code!=401{t.Fatal("transport credential symlink permitted")}
 a.connectorRemoteMu.Lock();a.connectorRemote.GrantedUntil=time.Now().Add(-time.Second);a.connectorRemoteMu.Unlock()
 if a.connectorRemoteCanRead(){t.Fatal("expired approval permitted")}
}

func TestRemoteAdminApprovalRequiresConfirmedSecureOrigin(t *testing.T){
 a,_:=connectorFixture(t);_ = connectorTestTransport(t,a)
 a.connectorTunnelCmd=&exec.Cmd{}
 a.connectorTunnelState="STARTING"
 result,_:=a.connectorRemoteRequestAccess();id:=result["request_id"].(string)
 body:=`{"confirm":true,"request_id":"`+id+`","approve":true}`
 unsafe:=connectorRequest("POST","/api/admin/connector/access/approve",body)
 unsafe.RemoteAddr="192.0.2.1:30000";unsafe.Host="freenet.example.net:8443";unsafe.Header.Set("Origin","https://freenet.example.net:8443")
 unsafe.Header.Set("X-Forwarded-Proto","https")
 unsafe.Header.Set("X-Forwarded-Host","freenet.example.net:8443")
 w:=httptest.NewRecorder();a.handleConnectorRemoteApprove(w,unsafe)
 if w.Code!=403 || a.connectorRemoteCanRead(){t.Fatal("untrusted forwarded HTTPS spoof granted access")}
 // Only an actual loopback reverse-proxy hop with a matching HTTPS Origin passes.
 safe:=connectorRequest("POST","/api/admin/connector/access/approve",body)
 safe.Host="127.0.0.1:1001";safe.Header.Set("Origin","https://freenet.example.net:8443")
 safe.Header.Set("X-Forwarded-Proto","https");safe.Header.Set("X-Forwarded-Host","freenet.example.net:8443")
 w=httptest.NewRecorder();a.handleConnectorRemoteApprove(w,safe)
 if w.Code!=200 || !a.connectorRemoteCanRead(){t.Fatalf("trusted loopback HTTPS approval failed: %d",w.Code)}
}

func TestSecureProvisioningSameRouterProxyPeer(t *testing.T){
 a,_:=connectorFixture(t);_ =a
 mk:=func(peer string)*http.Request{
  req:=connectorRequest("POST","/api/admin/connector/access/revoke",`{"confirm":true}`)
  req.RemoteAddr=peer
  req.Host="freenet.example.net:8443"
  req.Header.Set("Origin","https://freenet.example.net:8443")
  req.Header.Set("X-Forwarded-Proto","https")
  req=req.WithContext(context.WithValue(req.Context(),http.LocalAddrContextKey,&net.TCPAddr{IP:net.ParseIP("192.168.50.1"),Port:1001}))
  return req
 }
 if !connectorAdminSecureOrigin(mk("192.168.50.1:40500")){t.Fatal("same-router reverse proxy incorrectly rejected")}
 if connectorAdminSecureOrigin(mk("192.168.50.22:40500")){t.Fatal("LAN client forged proxy headers")}
 wrong:=mk("192.168.50.1:40500");wrong.Header.Set("Origin","http://freenet.example.net:8443")
 if connectorAdminSecureOrigin(wrong){t.Fatal("cleartext origin accepted")}
 forwarded:=mk("192.168.50.1:40500");forwarded.Header.Set("X-Forwarded-Proto","http")
 if connectorAdminSecureOrigin(forwarded){t.Fatal("downgraded proxy accepted")}
}

func TestTunnelProvisioningPrivateOneTimeAndNoSecretsInResponses(t *testing.T){
 a,_:=connectorFixture(t)
 a.cfg.Listen="192.168.50.1:1001"
 id:="tunnel_"+strings.Repeat("f",32)
 key:="sk-"+strings.Repeat("K",40)
 body:=`{"confirm":true,"tunnel_id":"`+id+`","runtime_api_key":"`+key+`"}`
 w:=httptest.NewRecorder();a.handleTunnelConfigure(w,connectorRequest("POST","/api/admin/connector/tunnel/configure",body))
 if w.Code!=200{t.Fatalf("provision refused: %d %s",w.Code,w.Body.String())}
 if !a.connectorTransportConfigured(){t.Fatal("persisted credential state not valid")}
 if strings.Contains(w.Body.String(),key)||strings.Contains(w.Body.String(),id){t.Fatal("configuration response disclosed input")}
 for _,file:=range []string{a.connectorTunnelProfilePath(),a.connectorTransportKeyPath(),a.connectorTransportHeaderPath(),a.connectorTunnelConfigPath()} {
  st,err:=os.Lstat(file);if err!=nil||!st.Mode().IsRegular()||st.Mode().Perm()!=0600{t.Fatalf("credential file unsafe: %s",filepath.Base(file))}
 }
 cfg,err:=os.ReadFile(a.connectorTunnelConfigPath());if err!=nil{t.Fatal(err)}
 if strings.Contains(string(cfg),key)||strings.Contains(string(cfg),"Bearer "){t.Fatal("credentials written in client YAML")}
 w=httptest.NewRecorder();a.handleTunnelConfigure(w,connectorRequest("POST","/api/admin/connector/tunnel/configure",body))
 if w.Code!=409{t.Fatal("re-provision overwrote an existing credential")}
 w=httptest.NewRecorder();a.handleTunnelStart(w,connectorRequest("POST","/api/admin/connector/tunnel/start",`{"confirm":true}`))
 if w.Code==200{t.Fatal("tunnel started without an installed attested binary")}
 if _,err:=os.Lstat(tunnelInstallPath());err==nil && strings.HasPrefix(tunnelInstallPath(),t.TempDir()){t.Fatal("test mutated system install slot")}
}

func TestTunnelProfileRejectsSymlinkAndMachineKeyMismatch(t *testing.T){
 a,_:=connectorFixture(t);token:=connectorTestTransport(t,a)
 if _,err:=a.readTunnelProfile();err!=nil{t.Fatal(err)}
 if err:=os.Remove(a.connectorTunnelProfilePath());err!=nil{t.Fatal(err)}
 if err:=os.Symlink("/dev/null",a.connectorTunnelProfilePath());err!=nil{t.Fatal(err)}
 if _,err:=a.readTunnelProfile();err==nil{t.Fatal("symlinked tunnel config accepted")}
 _=token
}

func TestConnectorRejectsOpenAIAdminKey(t *testing.T){
 for _,key:=range []string{"sk-admin-"+strings.Repeat("a",50),"sk-admin-"+strings.Repeat("b",20)}{
  if connectorRuntimeKeyAllowed(key){t.Fatal("privileged admin key accepted as runtime credential")}
 }
 if !connectorRuntimeKeyAllowed("sk-proj-"+strings.Repeat("a",36)){t.Fatal("limited project key incorrectly rejected")}
}

func TestTunnelForgetRevokeRotateAndSymlinkStop(t *testing.T){
 a,_:=connectorFixture(t)
 a.cfg.Listen="192.168.50.1:1001"
 body:=`{"confirm":true,"tunnel_id":"tunnel_`+strings.Repeat("d",32)+`","runtime_api_key":"sk-`+strings.Repeat("r",48)+`"}`
 configure:=func()int{
  w:=httptest.NewRecorder()
  a.handleTunnelConfigure(w,connectorRequest("POST","/api/admin/connector/tunnel/configure",body))
  return w.Code
 }
 if code:=configure();code!=200{t.Fatalf("initial setup failed: %d",code)}
 a.connectorRemoteMu.Lock()
 a.connectorRemote.GrantedUntil=time.Now().Add(15*time.Minute)
 a.connectorRemoteMu.Unlock()
 w:=httptest.NewRecorder()
 a.handleTunnelForget(w,connectorRequest("POST","/api/admin/connector/tunnel/forget",`{"confirm":true}`))
 if w.Code!=200||a.connectorRemoteCanRead()||a.connectorTransportConfigured(){
  t.Fatalf("operator revoke failed to close diagnostics and remove credentials: %d %s",w.Code,w.Body.String())
 }
 for _,path:=range []string{a.connectorTunnelProfilePath(),a.connectorTransportKeyPath(),a.connectorTransportHeaderPath(),a.connectorTunnelConfigPath()}{
  if _,err:=os.Lstat(path);!os.IsNotExist(err){t.Fatal("credential not deleted")}
 }
 if code:=configure();code!=200{t.Fatalf("reprovision after revocation failed: %d",code)}
 if err:=os.Remove(a.connectorTransportKeyPath());err!=nil{t.Fatal(err)}
 if err:=os.Symlink("/dev/null",a.connectorTransportKeyPath());err!=nil{t.Fatal(err)}
 w=httptest.NewRecorder()
 a.handleTunnelForget(w,connectorRequest("POST","/api/admin/connector/tunnel/forget",`{"confirm":true}`))
 if w.Code!=http.StatusServiceUnavailable{t.Fatalf("unsafe credential symlink was removed: %d",w.Code)}
 if st,err:=os.Lstat(a.connectorTransportKeyPath());err!=nil||st.Mode()&os.ModeSymlink==0{t.Fatal("unsafe credential unexpectedly changed")}
}
