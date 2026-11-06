package main

import (
    "bytes"
    "context"
    "crypto/tls"
    "encoding/json"
    "errors"
    "net/http"
    "net/http/httptest"
    "os"
    "path/filepath"
    "strings"
    "testing"
)

func nativeApplyFixture(t *testing.T,xrayShell,xkeenShell string)(string,string) {
    t.Helper()
    root:=t.TempDir()
    proc:=t.TempDir()
    if err:=os.MkdirAll(filepath.Join(root,"etc/freenet09"),0700);err!=nil {t.Fatal(err)}
    writeFixture(t,root,"etc/xray/configs/04_outbounds.json",
        "{\"outbounds\":[{\"tag\":\"original-proxy\",\"protocol\":\"vless\",\"settings\":{\"secret\":\"ORIGINAL-DO-NOT-CHANGE\"}}]}")
    writeFixture(t,root,"etc/xray/configs/05_routing.json",
        "{\"routing\":{\"rules\":[{\"outboundTag\":\"original-proxy\"}]}}")
    writeFixture(t,proc,"123/comm","xray\n")
    fakeNativeBinary(t,root,"xray",xrayShell)
    fakeNativeBinary(t,root,"xkeen",xkeenShell)
    return root,proc
}

const mockXraySuccess = "if [ \"$1\" != \"run\" ] || [ \"$2\" != \"-test\" ] || [ \"$3\" != \"-confdir\" ] || [ ! -f \"$4/04_zz_freenet09_subscription_tail.json\" ]; then\nexit 21\nfi\nexit 0\n"
const mockXKeenSuccess = "if [ \"$1\" != \"-restart\" ] && [ \"$1\" != \"-status\" ]; then exit 19; fi\nif [ \"$XKEEN_FOREGROUND\" != \"1\" ]; then exit 27; fi\nexit 0\n"
const nativeURLCanary = "https://subscription.example.org/s?token=SECRET-URL-CANARY"

func fakeSubscriptionFetch(_ context.Context,url string)([]byte,error){
    if url!=nativeURLCanary{return nil,errors.New("unexpected test URL")}
    return []byte(fakeVLESSReality+"\n"+fakeVLESSTLS),nil
}

func TestNativeSubscriptionApplyPreservesOriginalXKeenAndXray(t *testing.T){
    root,proc:=nativeApplyFixture(t,mockXraySuccess,mockXKeenSuccess)
    original,err:=os.ReadFile(filepath.Join(root,"etc/xray/configs/04_outbounds.json"))
    if err!=nil{t.Fatal(err)}
    if before:=nativeSubscriptionStatus(root);before.State!="NOT_CONFIGURED"{
        t.Fatalf("unexpected state: %+v",before)
    }
    got:=subscriptionOperationWithFetch(context.Background(),root,proc,nativeURLCanary,false,fakeSubscriptionFetch)
    if got.State!="CONFIG_APPLIED_NOT_EGRESS_VERIFIED" ||
        got.Runtime!="NOT_VERIFIED" || !got.Applied || got.Count!=2 {
        t.Fatalf("native transaction not recorded: %+v",got)
    }
    current,err:=os.ReadFile(filepath.Join(root,"etc/xray/configs/04_outbounds.json"))
    if err!=nil || !bytes.Equal(original,current){t.Fatal("original Xray config was modified")}
    managed,err:=os.ReadFile(filepath.Join(root,"etc/xray/configs",subscriptionFragmentName))
    if err!=nil || !bytes.Contains(managed,[]byte("PUBLIC-KEY-TEST")) ||
        !bytes.Contains(managed,[]byte("freenet-sub-")){
        t.Fatal("native outbound fragment not applied")
    }
    var doc map[string][]json.RawMessage
    if err:=json.Unmarshal(managed,&doc);err!=nil || len(doc["outbounds"])!=2{
        t.Fatal("invalid native outbound fragment")
    }
    statePath:=filepath.Join(root,"etc/freenet09",subscriptionStateName)
    privateState,err:=os.ReadFile(statePath)
    if err!=nil || !bytes.Contains(privateState,[]byte(nativeURLCanary)){
        t.Fatal("private URL not persisted")
    }
    info,err:=os.Lstat(statePath)
    if err!=nil || info.Mode().Perm()!=0600{t.Fatal("private subscription mode not 0600")}
    st:=nativeSubscriptionStatus(root)
    if !st.Applied || st.State!="CONFIG_APPLIED_NOT_EGRESS_VERIFIED" || st.Count!=2 {
        t.Fatalf("native status false: %+v",st)
    }
    refresh:=subscriptionOperationWithFetch(context.Background(),root,proc,"",true,fakeSubscriptionFetch)
    if !refresh.Applied || refresh.Count!=2 {t.Fatalf("native refresh failed: %+v",refresh)}
}

func TestNativeApplyStopsIfLegacyFreeNetOrUnownedFragmentPresent(t *testing.T){
    root,proc:=nativeApplyFixture(t,mockXraySuccess,mockXKeenSuccess)
    writeFixture(t,root,"etc/init.d/S99freenet-ui","legacy controller")
    got:=subscriptionOperationWithFetch(context.Background(),root,proc,nativeURLCanary,false,fakeSubscriptionFetch)
    if got.State!="STOP"||got.Applied{t.Fatal("old FreeNet 0.8 controller allowed a second writer")}
    if err:=os.Remove(filepath.Join(root,"etc/init.d/S99freenet-ui"));err!=nil{t.Fatal(err)}
    writeFixture(t,root,"etc/xray/configs/"+subscriptionFragmentName,"{\"outbounds\":[]}")
    got=subscriptionOperationWithFetch(context.Background(),root,proc,nativeURLCanary,false,fakeSubscriptionFetch)
    if got.State!="STOP"||got.Applied{t.Fatal("unowned native fragment overwritten")}
}

func TestNativeApplyRejectedCandidateDoesNotMutateLiveXray(t *testing.T){
    root,proc:=nativeApplyFixture(t,"exit 33\n",mockXKeenSuccess)
    got:=subscriptionOperationWithFetch(context.Background(),root,proc,nativeURLCanary,false,fakeSubscriptionFetch)
    if got.State!="REJECTED" || got.Applied {t.Fatalf("invalid candidate not rejected: %+v",got)}
    if _,err:=os.Stat(filepath.Join(root,"etc/xray/configs",subscriptionFragmentName));!errors.Is(err,os.ErrNotExist){
        t.Fatal("rejected candidate written to live config")
    }
}

func TestNativeApplyRollbackRestoresOnlyManagedFragment(t *testing.T){
    script:="if [ \"$1\" = \"-restart\" ]; then\n"+
        "marker=\"$0.once\"\nif [ ! -e \"$marker\" ]; then touch \"$marker\"; exit 22; fi\nfi\n"+
        "if [ \"$XKEEN_FOREGROUND\" != \"1\" ]; then exit 20; fi\nexit 0\n"
    root,proc:=nativeApplyFixture(t,mockXraySuccess,script)
    result:=subscriptionOperationWithFetch(context.Background(),root,proc,nativeURLCanary,false,fakeSubscriptionFetch)
    if result.State!="ROLLED_BACK" || result.Applied {
        t.Fatalf("failed native restart not rolled back: %+v",result)
    }
    if _,err:=os.Stat(filepath.Join(root,"etc/xray/configs",subscriptionFragmentName));!errors.Is(err,os.ErrNotExist){
        t.Fatal("failed managed fragment persisted")
    }
    if _,err:=os.Stat(filepath.Join(root,"etc/freenet09",subscriptionStateName));!errors.Is(err,os.ErrNotExist){
        t.Fatal("failed saved URL persisted")
    }
}

func TestNativeApplyRollbackFailedIsUnknownSTOP(t *testing.T) {
    root,proc:=nativeApplyFixture(t,mockXraySuccess,"exit 23\n")
    result:=subscriptionOperationWithFetch(context.Background(),root,proc,nativeURLCanary,false,fakeSubscriptionFetch)
    if result.State!="ROLLBACK_FAILED" || result.Runtime!="UNKNOWN" || result.Applied {
        t.Fatalf("rollback failed was falsely successful: %+v",result)
    }
}

func TestNativeApplyStopsOnXKeenStoplistOrSymlink(t *testing.T){
    root,proc:=nativeApplyFixture(t,mockXraySuccess,mockXKeenSuccess)
    writeFixture(t,root,"etc/xray/configs/04_outbounds.copy.json","{}")
    got:=subscriptionOperationWithFetch(context.Background(),root,proc,nativeURLCanary,false,fakeSubscriptionFetch)
    if got.State!="STOP"{t.Fatal("XKeen stop-list filename accepted")}
    if err:=os.Remove(filepath.Join(root,"etc/xray/configs/04_outbounds.copy.json"));err!=nil{t.Fatal(err)}
    target:=filepath.Join(t.TempDir(),"external.json")
    if err:=os.WriteFile(target,[]byte("{}"),0600);err!=nil{t.Fatal(err)}
    if err:=os.Symlink(target,filepath.Join(root,"etc/xray/configs/05_secret.json"));err!=nil{t.Skip(err)}
    got=subscriptionOperationWithFetch(context.Background(),root,proc,nativeURLCanary,false,fakeSubscriptionFetch)
    if got.State!="STOP"{t.Fatal("external symlink loaded into native candidate")}
}

func TestNativeApplyAPIOnlyUnderAuthenticatedTLS(t *testing.T){
    root,_,auth:=newTestAuth(t)
    local:=handlersWithProc(root,t.TempDir())
    anon:=httptest.NewRecorder()
    local.ServeHTTP(anon,httptest.NewRequest(http.MethodPost,
        "http://127.0.0.1:1009/api/subscription/apply",nil))
    if anon.Code==http.StatusOK {t.Fatal("anonymous loopback preview allowed native mutation")}
    secure:=secureControlCenter(root,t.TempDir(),auth)
    req:=httptest.NewRequest(http.MethodPost,"https://router.example/api/subscription/apply",nil)
    req.TLS=&tls.ConnectionState{}
    denied:=httptest.NewRecorder()
    secure.ServeHTTP(denied,req)
    if denied.Code!=http.StatusUnauthorized {t.Fatalf("unauthenticated apply: %d",denied.Code)}
    gate:=make(chan struct{},1)
    csrfReq:=httptest.NewRequest(http.MethodPost,
        "https://router.example/api/subscription/apply",
        strings.NewReader("{\"url\":\"https://provider.example/sub?token=VERY-PRIVATE\"}"))
    csrfReq.TLS=&tls.ConnectionState{}
    csrfReq.Header.Set("Content-Type","application/json")
    rec:=httptest.NewRecorder()
    subscriptionApplyHTTP(rec,csrfReq,root,t.TempDir(),gate)
    if rec.Code!=http.StatusForbidden || strings.Contains(rec.Body.String(),"VERY-PRIVATE"){
        t.Fatal("CSRF bypass or URL leaked")
    }
    csrfReq.Header.Set("Origin","https://router.example")
    csrfReq.Header.Set("X-FreeNet-Action","apply-subscription")
    gate<-struct{}{}
    busy:=httptest.NewRecorder()
    subscriptionApplyHTTP(busy,csrfReq,root,t.TempDir(),gate)
    <-gate
    if busy.Code!=http.StatusConflict || !strings.Contains(busy.Body.String(),"BUSY"){
        t.Fatal("concurrent native apply not serialized")
    }
}

func TestSubscriptionFragmentUsesOfficialXrayTailMerge(t *testing.T) {
    // Xray's official multiple-config merge prepends new outbounds unless
    // the filename contains "tail". Existing default DIRECT/VPN must retain
    // its precedence after importing extra subscription nodes.
    if !strings.Contains(strings.ToLower(subscriptionFragmentName),"tail") {
        t.Fatalf("native Xray fragment %q would prepend outbound and change default routing",subscriptionFragmentName)
    }
    if strings.Contains(subscriptionFragmentName,"copy") ||
        strings.Contains(subscriptionFragmentName,"old") ||
        strings.Contains(subscriptionFragmentName,"bak") {
        t.Fatal("fragment name collides with XKeen config stoplist")
    }
    root,proc:=nativeApplyFixture(t,mockXraySuccess,mockXKeenSuccess)
    got:=subscriptionOperationWithFetch(context.Background(),root,proc,nativeURLCanary,false,fakeSubscriptionFetch)
    if got.State!="CONFIG_APPLIED_NOT_EGRESS_VERIFIED" || !got.Applied {
        t.Fatalf("native subscription import with safe tail failed: %+v",got)
    }
    path:=filepath.Join(root,"etc/xray/configs",subscriptionFragmentName)
    if _,err:=os.Stat(path);err!=nil{t.Fatal("tail fragment not written")}
    // The pre-existing default outbound stays untouched and physically
    // precedes the managed tail file under Xray's documented sort order.
    if !(filepath.Base(path)>"04_outbounds.json") {
        t.Fatal("managed Xray fragment unexpectedly ordered before native default")
    }
}

func TestLegacyNonTailSubscriptionFragmentStopsBeforeMutation(t *testing.T) {
    root,proc:=nativeApplyFixture(t,mockXraySuccess,mockXKeenSuccess)
    writeFixture(t,root,"etc/xray/configs/04_zz_freenet09_subscription.json",
        `{"outbounds":[{"tag":"freenet-sub-old","protocol":"freedom"}]}`)
    result:=subscriptionOperationWithFetch(context.Background(),root,proc,nativeURLCanary,false,fakeSubscriptionFetch)
    if result.Applied || result.State!="STOP" {
        t.Fatalf("unowned old fragment must STOP safely: %+v",result)
    }
    if _,err:=os.Stat(filepath.Join(root,"etc/xray/configs",subscriptionFragmentName));!errors.Is(err,os.ErrNotExist) {
        t.Fatal("managed tail fragment must not be written over conflicting old config")
    }
}
