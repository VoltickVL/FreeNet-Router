package main

import (
    "bytes"
    "context"
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "os"
    "path/filepath"
    "strings"
    "testing"
    "time"
)

func nativeValidationFixture(t *testing.T,script string) string {
    t.Helper()
    root:=t.TempDir()
    fakeNativeBinary(t,root,"xray",script)
    writeFixture(t,root,"etc/xray/configs/04_outbounds.json","{}")
    writeFixture(t,root,"etc/xray/configs/05_routing.json","{}")
    return root
}

func TestOfficialXrayConfigValidationNeverStartsServerOrLeadsSecrets(t *testing.T){
    root:=nativeValidationFixture(t,`if [ "$#" -ne 4 ] || [ "$1" != "run" ] || [ "$2" != "-test" ] || [ "$3" != "-confdir" ] || [ ! -d "$4" ]; then
  printf 'WRONG_NATIVE_ARGUMENTS\\n' >&2
  exit 19
fi
printf 'Configuration OK. SECRET-PRIVATE-DO-NOT-SHOW\\n'
printf 'Reality SECRET-KEY-DO-NOT-SHOW\\n' >&2
exit 0
`)
    before,err:=os.ReadFile(filepath.Join(root,"etc/xray/configs/04_outbounds.json"))
    if err!=nil{t.Fatal(err)}
    report:=validateNativeXray(context.Background(),root)
    if report.State!="VALIDATED_CONFIG_ONLY" || report.Runtime!="NOT_VERIFIED" {
        t.Fatalf("official Xray config -test not applied: %+v",report)
    }
    app:=handlersWithProc(root,t.TempDir())
    rec:=httptest.NewRecorder()
    app.ServeHTTP(rec,httptest.NewRequest(http.MethodGet,"/api/native/xray/validate",nil))
    if rec.Code!=http.StatusOK{t.Fatalf("native test HTTP %d",rec.Code)}
    var result nativeXrayValidation
    if err:=json.Unmarshal(rec.Body.Bytes(),&result);err!=nil{t.Fatal(err)}
    if result.State!="VALIDATED_CONFIG_ONLY" || result.Runtime!="NOT_VERIFIED"{
        t.Fatalf("native test falsely claimed live egress: %+v",result)
    }
    if !strings.Contains(rec.Header().Get("Cache-Control"),"no-store") {
        t.Fatal("native CLI test response cached")
    }
    for _,secret:=range []string{"SECRET-PRIVATE","SECRET-KEY","Reality","Configuration OK","WRONG_NATIVE_ARGUMENTS"} {
        if bytes.Contains(rec.Body.Bytes(),[]byte(secret)){
            t.Fatalf("Xray -test output exposed: %s",secret)
        }
    }
    after,err:=os.ReadFile(filepath.Join(root,"etc/xray/configs/04_outbounds.json"))
    if err!=nil || !bytes.Equal(before,after){t.Fatal("config changed during native validation")}
    for _,method:=range []string{"POST","PUT","DELETE","PATCH"}{
        denied:=httptest.NewRecorder()
        app.ServeHTTP(denied,httptest.NewRequest(method,"/api/native/xray/validate",strings.NewReader("{}")))
        if denied.Code==http.StatusOK{t.Fatalf("mutation method allowed: %s",method)}
    }
}

func TestOfficialXrayValidationRejectsWithoutEchoingErrors(t *testing.T){
    root:=nativeValidationFixture(t,`printf 'failed with UUID SECRET-XRAY-UUID\\n' >&2
exit 23
`)
    got:=validateNativeXray(context.Background(),root)
    if got.State!="REJECTED" || got.Runtime!="NOT_VERIFIED" {
        t.Fatalf("native rejected config not conveyed safely: %+v",got)
    }
    rec:=httptest.NewRecorder()
    handlersWithProc(root,t.TempDir()).ServeHTTP(rec,httptest.NewRequest(http.MethodGet,"/api/native/xray/validate",nil))
    if bytes.Contains(rec.Body.Bytes(),[]byte("SECRET-XRAY-UUID")){
        t.Fatal("native error output leaked to browser")
    }
}

func TestOfficialXrayValidationFailsClosedOnMissingAndSymlink(t *testing.T){
    root:=t.TempDir()
    if got:=validateNativeXray(context.Background(),root);got.State!="MISSING" {
        t.Fatalf("missing native binary accepted: %+v",got)
    }
    fakeNativeBinary(t,root,"xray","exit 0\n")
    if got:=validateNativeXray(context.Background(),root);got.State!="MISSING" {
        t.Fatalf("missing native config accepted: %+v",got)
    }
    writeFixture(t,root,"etc/xray/configs/04_outbounds.json","{}")
    outside:=filepath.Join(t.TempDir(),"05_routing.json")
    if err:=os.WriteFile(outside,[]byte("{}"),0600);err!=nil{t.Fatal(err)}
    if err:=os.Symlink(outside,filepath.Join(root,"etc/xray/configs/05_routing.json"));err!=nil{t.Skip(err)}
    if got:=validateNativeXray(context.Background(),root);got.State!="UNKNOWN"{
        t.Fatalf("unsafe config symlink trusted: %+v",got)
    }
}

func TestOfficialXrayValidationTimeoutIsBounded(t *testing.T){
    root:=nativeValidationFixture(t,"sleep 3\nexit 0\n")
    ctx,cancel:=context.WithTimeout(context.Background(),25*time.Millisecond)
    defer cancel()
    start:=time.Now()
    got:=validateNativeXray(ctx,root)
    if got.State!="TIMEOUT" || got.Runtime!="NOT_VERIFIED" {
        t.Fatalf("timed out Xray -test falsely successful: %+v",got)
    }
    if time.Since(start)>2*time.Second{t.Fatalf("native validator child did not terminate promptly: %s",time.Since(start))}
}

func TestOfficialXrayValidatorRejectsConcurrentRequests(t *testing.T){
    root:=nativeValidationFixture(t,`touch "$4/../native-validator-started"
sleep 1
exit 0
`)
    server:=handlersWithProc(root,t.TempDir())
    done:=make(chan nativeXrayValidation,1)
    go func(){
        rec:=httptest.NewRecorder()
        server.ServeHTTP(rec,httptest.NewRequest(http.MethodGet,"/api/native/xray/validate",nil))
        var got nativeXrayValidation
        _=json.Unmarshal(rec.Body.Bytes(),&got)
        done<-got
    }()
    marker:=filepath.Join(root,"etc/xray/native-validator-started")
    deadline:=time.Now().Add(2*time.Second)
    for {
        if _,err:=os.Stat(marker);err==nil{break}
        if time.Now().After(deadline){t.Fatal("fake native validator never started")}
        time.Sleep(10*time.Millisecond)
    }
    busy:=httptest.NewRecorder()
    server.ServeHTTP(busy,httptest.NewRequest(http.MethodGet,"/api/native/xray/validate",nil))
    if busy.Code!=http.StatusConflict || !strings.Contains(busy.Body.String(),`"state":"BUSY"`) {
        t.Fatalf("second concurrent validator not rejected: %d %s",busy.Code,busy.Body.String())
    }
    got:=<-done
    if got.State!="VALIDATED_CONFIG_ONLY" {
        t.Fatalf("first native check not completed: %+v",got)
    }
}
