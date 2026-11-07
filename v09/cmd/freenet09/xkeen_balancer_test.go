package main

import (
 "context"
 "crypto/tls"
 "encoding/json"
 "net/http"
 "net/http/httptest"
 "os"
 "path/filepath"
 "strings"
 "testing"
)

func balancerFixture(t *testing.T, command string) (string,string,string) {
 t.Helper()
 root,proc:=newXKeenServiceFixture(t,"case \"$1\" in status) exit 0;; *) exit 40;; esac\n",true)
 writeFixture(t,root,"etc/xray/configs/05_routing.json","{}")
 writeFixture(t,root,"sbin/xkeen","#!/bin/sh\ncase \"$1\" in\n -v) echo 'XKeen 2.2 Beta'; exit 0;;\n -sb) "+command+";;\n *) exit 44;;\nesac\n")
 path:=filepath.Join(root,"sbin/xkeen")
 if err:=os.Chmod(path,0700);err!=nil{t.Fatal(err)}
 log:=filepath.Join(root,"command-receipt")
 return root,proc,log
}

func TestXKeen22BalancerFixedCommandsAndPrivacy(t *testing.T){
 root,proc,log:=balancerFixture(t,"printf '%s' \"$2\" > '"+filepath.Join(t.TempDir(),"unused")+"'\n exit 0")
 // The fake prints a credential on stdout/stderr; only fixed enum is returned.
 writeFixture(t,root,"sbin/xkeen","#!/bin/sh\nif [ \"$1\" = '-v' ]; then echo 'XKeen 2.2 Beta'; exit 0; fi\nif [ \"$1\" = '-sb' ]; then printf '%s' \"$2\" >> '"+log+"'; echo 'SECRET-UUID' >&2; exit 0; fi\nexit 44\n")
 for _,action:=range []string{"next","prev","auto"}{
  got:=nativeBalancerAction(context.Background(),root,proc,action)
  if got.State!="UPSTREAM_COMMAND_ACCEPTED_NOT_VERIFIED" || got.Runtime!="NOT_VERIFIED" {t.Fatalf("action %s: %+v",action,got)}
  raw,_:=json.Marshal(got)
  if strings.Contains(string(raw),"SECRET-UUID"){t.Fatal("native stderr exposed")}
 }
 content,err:=os.ReadFile(log);if err!=nil{t.Fatal(err)}
 if string(content)!="nextprevauto"{t.Fatalf("wrong upstream commands: %s",content)}
 for _,action:=range []string{"on","off","list","1","-i","next; touch /tmp/unsafe",""}{
  if got:=nativeBalancerAction(context.Background(),root,proc,action);got.State!="STOP" {t.Fatalf("unsafe CLI accepted: %q %+v",action,got)}
 }
}

func TestXKeen22BalancerFailClosed(t *testing.T){
 root,proc,_:=balancerFixture(t,"exit 0")
 writeFixture(t,root,"etc/init.d/S99freenet-ui","legacy")
 if got:=nativeBalancerAction(context.Background(),root,proc,"next");got.State!="STOP"{t.Fatal("legacy writer not blocked")}
 if err:=os.Remove(filepath.Join(root,"etc/init.d/S99freenet-ui"));err!=nil{t.Fatal(err)}
 writeFixture(t,root,"sbin/xkeen","#!/bin/sh\nif [ \"$1\" = '-v' ]; then echo 'XKeen 2.1'; exit 0; fi\nexit 0\n")
 if got:=nativeBalancerAction(context.Background(),root,proc,"next");got.State!="STOP"{t.Fatal("2.1 allowed")}
 writeFixture(t,root,"sbin/xkeen","#!/bin/sh\nif [ \"$1\" = '-v' ]; then echo 'XKeen 2.2 Beta'; exit 0; fi\nexit 6\n")
 if got:=nativeBalancerAction(context.Background(),root,proc,"next");got.State!="UNKNOWN"{t.Fatalf("failed command accepted: %+v",got)}
 if err:=os.Remove(filepath.Join(root,"sbin/xkeen"));err!=nil{t.Fatal(err)}
 if err:=os.Symlink("/bin/true",filepath.Join(root,"sbin/xkeen"));err!=nil{t.Skip(err)}
 if got:=nativeBalancerAction(context.Background(),root,proc,"next");got.State!="STOP"{t.Fatal("symlink executable allowed")}
}

func TestXKeen22BalancerHTTPSCSRFAndGate(t *testing.T){
 root,proc,_:=balancerFixture(t,"exit 0")
 gate:=make(chan struct{},1)
 call:=func(method,body,origin,token string) *httptest.ResponseRecorder{
  req:=httptest.NewRequest(method,"https://router.example/api/native/xkeen/balancer",strings.NewReader(body))
  req.TLS=&tls.ConnectionState{}
  req.Header.Set("Content-Type","application/json")
  if origin!="" {req.Header.Set("Origin",origin)}
  if token!="" {req.Header.Set("X-FreeNet-Action",token)}
  w:=httptest.NewRecorder()
  nativeBalancerHTTP(w,req,root,proc,gate)
  return w
 }
 if got:=call("POST",`{"action":"next"}`,"","xkeen-balancer");got.Code!=http.StatusForbidden{t.Fatal("no origin accepted")}
 if got:=call("POST",`{"action":"next"}`,"https://evil.test","xkeen-balancer");got.Code!=http.StatusForbidden{t.Fatal("foreign origin accepted")}
 if got:=call("POST",`{"action":"next"}`,"https://router.example","");got.Code!=http.StatusForbidden{t.Fatal("missing action header accepted")}
 if got:=call("POST",`{"action":"on"}`,"https://router.example","xkeen-balancer");got.Code!=http.StatusBadRequest{t.Fatal("unsafe balancer option accepted")}
 gate<-struct{}{}
 if got:=call("POST",`{"action":"next"}`,"https://router.example","xkeen-balancer");got.Code!=http.StatusConflict || !strings.Contains(got.Body.String(),"BUSY"){t.Fatal("mutation gate bypassed")}
 <-gate
 if got:=call("POST",`{"action":"next"}`,"https://router.example","xkeen-balancer");got.Code!=http.StatusOK || !strings.Contains(got.Body.String(),"NOT_VERIFIED"){t.Fatal("native command not returned")}
 preview:=handlersWithProc(root,proc)
 w:=httptest.NewRecorder()
 preview.ServeHTTP(w,httptest.NewRequest("POST","http://127.0.0.1:1009/api/native/xkeen/balancer",strings.NewReader(`{"action":"next"}`)))
 if w.Code==http.StatusOK {t.Fatal("unauthenticated preview accepts mutation")}
}

func TestXKeen22BalancerBrowserWiring(t *testing.T){
 app:=handlersWithProc(t.TempDir(),t.TempDir())
 for path,parts:=range map[string][]string{
  "/":[]string{`id="xkeen-balancer-actions"`,`data-xkeen-balancer="next"`,`data-xkeen-balancer="prev"`,`data-xkeen-balancer="auto"`},
  "/static/app.js":[]string{"/api/native/xkeen/balancer","xkeen-balancer","data-xkeen-balancer"},
 } {
  w:=httptest.NewRecorder();app.ServeHTTP(w,httptest.NewRequest(http.MethodGet,path,nil))
  for _,part:=range parts {if !strings.Contains(w.Body.String(),part){t.Fatalf("%s lacks %s",path,part)}}
 }
}

func TestXKeen21ReleaseUIHidesUnsupportedBetaControls(t *testing.T){
 raw,err:=webAssets.ReadFile("web/app.js")
 if err!=nil {t.Fatal(err)}
 script:=string(raw)
 for _,part:=range []string{
  `detectedXKeenVersion === "2.2"`,
  `secureControlCenterAuthenticated && detectedXKeenVersion === "2.2"`,
  `actions.hidden = !(secureControlCenterAuthenticated && detectedXKeenVersion === "2.2")`,
  `detectedXKeenVersion === "2.1"`,
 } {
  if !strings.Contains(script,part){t.Fatalf("missing 2.1/2.2 guarded UI condition: %s",part)}
 }
 if strings.Contains(script,`balancerButtons.hidden = false`) {
  t.Fatal("beta-only balancer actions unconditionally enabled for XKeen 2.1")
 }
}
