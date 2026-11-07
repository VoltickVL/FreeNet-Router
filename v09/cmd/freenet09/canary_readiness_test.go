package main

import (
 "context"
 "encoding/json"
 "net/http"
 "net/http/httptest"
 "os"
 "path/filepath"
 "strings"
 "testing"
)

func readinessFixture(t *testing.T,xkeenVersion string,legacy bool)(string,string){
 t.Helper()
 root,proc:=t.TempDir(),t.TempDir()
 fakeNativeBinary(t,root,"xkeen","if [ \"$1\" = '-v' ]; then echo 'XKeen "+xkeenVersion+" Beta'; exit 0; fi\nexit 42\n")
 fakeNativeBinary(t,root,"xray","if [ \"$1\" = version ]; then echo 'Xray 26.10.31'; exit 0; fi\nexit 42\n")
 writeFixture(t,root,"etc/init.d/S05xkeen","native init")
 writeFixture(t,root,"etc/xray/configs/05_routing.json","{}")
 writeFixture(t,proc,"234/comm","xray\n")
 if legacy{
  writeFixture(t,root,"sbin/freenet-ui","legacy")
  writeFixture(t,root,"etc/init.d/S99freenet-ui","legacy")
 }
 return root,proc
}

func TestHomeFirstReadinessLegacy21IsStagingOnly(t *testing.T){
 root,proc:=readinessFixture(t,"2.1",true)
 got:=canaryReadiness(context.Background(),root,proc)
 if got.State!="STAGING_ONLY" || got.Reason!="XKEEN_21_LEGACY_08_CUTOVER_BLOCKED" ||
  got.Legacy!="PRESENT" || got.XKeen!="2.1" || got.Xray!="26.10.31" ||got.Runtime!="NOT_VERIFIED" {
  t.Fatalf("HOME 2.1+0.8 must not allow cutover: %+v",got)
 }
 api:=handlersWithProc(root,proc)
 rec:=httptest.NewRecorder()
 api.ServeHTTP(rec,httptest.NewRequest("GET","/api/native/canary-readiness",nil))
 if rec.Code!=http.StatusOK {t.Fatalf("GET readiness HTTP %d",rec.Code)}
 var reply nativeCanaryReadiness
 if err:=json.Unmarshal(rec.Body.Bytes(),&reply);err!=nil{t.Fatal(err)}
 if reply!=got {t.Fatalf("response mismatch: %+v vs %+v",reply,got)}
 if !strings.Contains(rec.Header().Get("Cache-Control"),"no-store"){t.Fatal("readiness cached")}
 for _,method:=range []string{"POST","PATCH","DELETE"}{
  invalid:=httptest.NewRecorder()
  api.ServeHTTP(invalid,httptest.NewRequest(method,"/api/native/canary-readiness",nil))
  if invalid.Code==http.StatusOK {t.Fatalf("unexpected readiness mutation %s",method)}
 }
}

func TestCanaryReadiness22IsNotEgressVerification(t *testing.T){
 root,proc:=readinessFixture(t,"2.2",false)
 got:=canaryReadiness(context.Background(),root,proc)
 if got.State!="CORE_PRECHECK_ONLY" || got.Runtime!="NOT_VERIFIED" ||got.Reason!="BROWSER_SETUP_CUTOVER_AND_EGRESS_NOT_VERIFIED" {
  t.Fatalf("core-only must not be called ready: %+v",got)
 }
 writeFixture(t,root,"sbin/freenet-ui","legacy")
 writeFixture(t,root,"etc/init.d/S99freenet-ui","legacy")
 got=canaryReadiness(context.Background(),root,proc)
 if got.State!="STAGING_ONLY"||got.Reason!="LEGACY_08_CUTOVER_BLOCKED"{t.Fatalf("legacy conflict accepted: %+v",got)}
}

func TestCanaryReadinessPartialAndSymlinkFailClosed(t *testing.T){
 root,proc:=readinessFixture(t,"2.2",true)
 if err:=os.Remove(filepath.Join(root,"etc/init.d/S99freenet-ui"));err!=nil{t.Fatal(err)}
 got:=canaryReadiness(context.Background(),root,proc)
 if got.State!="STOP"||got.Reason!="PARTIAL_LEGACY_FREENET"{t.Fatalf("partial legacy accepted: %+v",got)}
 if err:=os.Remove(filepath.Join(root,"sbin/xkeen"));err!=nil{t.Fatal(err)}
 if err:=os.Symlink("/bin/true",filepath.Join(root,"sbin/xkeen"));err!=nil{t.Skip(err)}
 got=canaryReadiness(context.Background(),root,proc)
 if got.State!="STOP"||got.Reason!="NATIVE_STATE_UNKNOWN"{t.Fatalf("unsafe symlink accepted: %+v",got)}
}

func TestCanaryReadinessBrowserIntegration(t *testing.T){
 app:=handlersWithProc(t.TempDir(),t.TempDir())
 for path,parts:=range map[string][]string{
  "/":[]string{`id="canary-readiness-state"`,`id="canary-readiness-reason"`},
  "/static/app.js":[]string{"/api/native/canary-readiness","canaryReadiness"},
 }{
  w:=httptest.NewRecorder()
  app.ServeHTTP(w,httptest.NewRequest(http.MethodGet,path,nil))
  for _,part:=range parts{if !strings.Contains(w.Body.String(),part){t.Fatalf("%s missing %s",path,part)}}
 }
}
