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

func TestPreservedNativeSourcesHomeReadOnlyAndNoSecretLeak(t *testing.T) {
 root:=t.TempDir()
 writeFixture(t,root,"etc/xray/configs/02_dns.json",`{"servers":["private-dns.local"]}`)
 writeFixture(t,root,"etc/xray/configs/04_outbounds.json",`{"outbounds":[{"tag":"MY-PRIVATE-ROUTE"}]}`)
 writeFixture(t,root,"etc/xray/configs/05_routing.json",`{"rules":[{"domain":["secret.example"]}]}`)
 writeFixture(t,root,"etc/xray/blanc_subscription.url","https://secret-user:secret-password@secret.example/vpn?token=SECRET-SUBSCRIPTION")
 writeFixture(t,root,"sbin/freenet-ui","legacy")
 writeFixture(t,root,"etc/init.d/S99freenet-ui","legacy")
 // Native XKeen may not have an xkeen.json; this is optional on HOME.
 report:=preservedNativeSources(root)
 if report.State!="NATIVE_INPUTS_DETECTED" ||
  report.DNS!="PRESENT"||report.Routing!="PRESENT"||
  report.Outbounds!="PRESENT"||report.Subscription!="PRESENT"||
  report.XKeenConfig!="MISSING"||report.Runtime!="NOT_VERIFIED" {
  t.Fatalf("wrong clean-install evidence: %+v",report)
 }
 app:=handlersWithProc(root,t.TempDir())
 w:=httptest.NewRecorder()
 app.ServeHTTP(w,httptest.NewRequest(http.MethodGet,"/api/native/preserved-sources",nil))
 if w.Code!=http.StatusOK {t.Fatalf("GET: %d",w.Code)}
 var got nativePreservedSources
 if err:=json.Unmarshal(w.Body.Bytes(),&got);err!=nil {t.Fatal(err)}
 if got!=report {t.Fatalf("GET mismatch: %+v",got)}
 raw:=w.Body.String()
 for _,secret:=range []string{"secret-password","SECRET-SUBSCRIPTION","secret.example","MY-PRIVATE-ROUTE","private-dns.local","freenet.conf"}{
  if strings.Contains(raw,secret){t.Fatalf("confidential source exposed: %s",secret)}
 }
 if w.Header().Get("Cache-Control")!="no-store" {t.Fatal("native source inventory cached")}
 for _,method:=range []string{http.MethodPost,http.MethodPut,http.MethodDelete}{
  reject:=httptest.NewRecorder()
  app.ServeHTTP(reject,httptest.NewRequest(method,"/api/native/preserved-sources",nil))
  if reject.Code==http.StatusOK {t.Fatalf("mutation method accepted: %s",method)}
 }
 after,err:=os.ReadFile(filepath.Join(root,"etc/xray/blanc_subscription.url"))
 if err!=nil||!strings.Contains(string(after),"SECRET-SUBSCRIPTION"){t.Fatal("read-only API changed existing subscription")}
}

func TestPreservedNativeSourcesMissingAndSymlinkStop(t *testing.T) {
 root:=t.TempDir()
 writeFixture(t,root,"etc/xray/configs/02_dns.json","{}")
 writeFixture(t,root,"etc/xray/configs/04_outbounds.json","{}")
 got:=preservedNativeSources(root)
 if got.State!="NATIVE_POLICY_INCOMPLETE"||got.Routing!="MISSING"{t.Fatalf("missing routing not detected: %+v",got)}
 writeFixture(t,root,"etc/xray/configs/05_routing.json","{}")
 if err:=os.Remove(filepath.Join(root,"etc/xray/configs/02_dns.json"));err!=nil{t.Fatal(err)}
 if err:=os.Symlink("/tmp/external-sensitive-dns",filepath.Join(root,"etc/xray/configs/02_dns.json"));err!=nil{t.Skip(err)}
 got=preservedNativeSources(root)
 if got.State!="STOP"||got.DNS!="UNKNOWN" {t.Fatalf("symlink source accepted: %+v",got)}
}

func TestPreservedNativeSourcesBrowserWiring(t *testing.T) {
 app:=handlersWithProc(t.TempDir(),t.TempDir())
 for path,parts:=range map[string][]string{
  "/":[]string{`id="native-preservation-state"`,`data-native-preservation="dns"`,`data-native-preservation="subscription"`},
  "/static/app.js":[]string{"/api/native/preserved-sources","nativePreservation","data-native-preservation"},
 } {
  w:=httptest.NewRecorder()
  app.ServeHTTP(w,httptest.NewRequest(http.MethodGet,path,nil))
  for _,part:=range parts{if !strings.Contains(w.Body.String(),part){t.Fatalf("%s lacks %s",path,part)}}
 }
}
