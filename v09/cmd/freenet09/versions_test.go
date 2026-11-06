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

func fakeNativeBinary(t *testing.T,root,name,script string) {
    t.Helper()
    rel:=filepath.Join("sbin",name)
    writeFixture(t,root,rel,"#!/bin/sh\n"+script)
    if err:=os.Chmod(filepath.Join(root,rel),0700);err!=nil{t.Fatal(err)}
}

func TestNativeVersionAPIUsesOfficialFixedCommandsAndHidesRawOutput(t *testing.T){
    root:=t.TempDir()
    fakeNativeBinary(t,root,"xkeen",`if [ "$1" != "-v" ] || [ "$#" -ne 1 ]; then
  echo "UNSAFE_XKEEN_COMMAND" >&2
  exit 19
fi
printf 'XKeen 2.1\nsubscription=https://SECRET-SUBSCRIPTION\n'
`)
    fakeNativeBinary(t,root,"xray",`if [ "$1" != "version" ] || [ "$#" -ne 1 ]; then
  echo "UNSAFE_XRAY_COMMAND" >&2
  exit 19
fi
printf 'Xray 26.10.31 (Xray, Penetrates Everything.)\nReality SECRET-PRIVATE-KEY\n'
`)
    api:=handlersWithProc(root,t.TempDir())
    rec:=httptest.NewRecorder()
    api.ServeHTTP(rec,httptest.NewRequest(http.MethodGet,"/api/native/versions",nil))
    if rec.Code!=http.StatusOK {t.Fatalf("native versions API http %d",rec.Code)}
    var got nativeVersionReport
    if err:=json.Unmarshal(rec.Body.Bytes(),&got);err!=nil{t.Fatal(err)}
    if got.Source!="OFFICIAL_CLI_READ_ONLY" || got.Runtime!="NOT_VERIFIED" ||
        got.XKeen.State!="VERSION_DETECTED" || got.XKeen.Version!="2.1" ||
        got.Xray.State!="VERSION_DETECTED" || got.Xray.Version!="26.10.31" {
        t.Fatalf("official versions not detected precisely: %+v",got)
    }
    for _,s:=range []string{"SECRET-SUBSCRIPTION","SECRET-PRIVATE-KEY",
        "UNSAFE_XKEEN_COMMAND","UNSAFE_XRAY_COMMAND","Reality","subscription"} {
        if bytes.Contains(rec.Body.Bytes(),[]byte(s)){t.Fatalf("unfiltered command output leaked: %q",s)}
    }
    if !strings.Contains(rec.Header().Get("Cache-Control"),"no-store"){
        t.Fatal("read-only versions response cached")
    }
    for _,method:=range []string{"POST","PUT","DELETE","PATCH"}{
        denied:=httptest.NewRecorder()
        api.ServeHTTP(denied,httptest.NewRequest(method,"/api/native/versions",strings.NewReader("{}")))
        if denied.Code==http.StatusOK{t.Fatalf("version mutation method accepted: %s",method)}
    }
}

func TestNativeVersionMissingSymlinkAndInvalidOutputAreUnknown(t *testing.T){
    root:=t.TempDir()
    empty:=detectNativeVersions(context.Background(),root)
    if empty.XKeen.State!="MISSING" || empty.Xray.State!="MISSING" {
        t.Fatalf("missing stack must be MISSING: %+v",empty)
    }
    fakeNativeBinary(t,root,"xray",`printf 'Xray secret.example token=NO-PUBLIC-CONFIG\n'
`)
    if got:=oneNativeVersion(context.Background(),root,"xray");got.State!="UNKNOWN"||got.Version!=""{
        t.Fatalf("invalid version exposed: %+v",got)
    }
    outside:=filepath.Join(t.TempDir(),"xkeen")
    if err:=os.WriteFile(outside,[]byte("#!/bin/sh\necho 'XKeen 2.1'\n"),0700);err!=nil{t.Fatal(err)}
    if err:=os.Symlink(outside,filepath.Join(root,"sbin/xkeen"));err!=nil{t.Skip(err)}
    if got:=oneNativeVersion(context.Background(),root,"xkeen");got.State!="UNKNOWN" {
        t.Fatalf("symlink executable trusted: %+v",got)
    }
    if got:=oneNativeVersion(context.Background(),root,"not-real");got.State!="UNKNOWN" {
        t.Fatalf("arbitrary binary dispatch allowed: %+v",got)
    }
    for _,input:=range []string{"Xray 26.10.31.55","password=ABC123","Xray X.10.31","Xray 123456.1"} {
        if version:=parseNativeVersion("xray",input);version!="" {
            t.Fatalf("bad version %q accepted as %q",input,version)
        }
    }
    if got:=parseNativeVersion("xkeen","2.1");got!="2.1" {
        t.Fatalf("official -v compact format rejected: %q",got)
    }
}

func TestNativeVersionTimeoutFailsClosed(t *testing.T){
    root:=t.TempDir()
    fakeNativeBinary(t,root,"xkeen",`sleep 3
printf 'XKeen 2.1\n'
`)
    ctx,cancel:=context.WithTimeout(context.Background(),25*time.Millisecond)
    defer cancel()
    start:=time.Now()
    got:=oneNativeVersion(ctx,root,"xkeen")
    if got.State!="TIMEOUT" || got.Version!="" {
        t.Fatalf("timed-out official command returned version: %+v",got)
    }
    if time.Since(start)>2*time.Second {
        t.Fatalf("native subprocess cleanup did not respect timeout: %s",time.Since(start))
    }
}

func TestNativeVersionsNeverClaimVPNRuntimeVerified(t *testing.T){
    root:=t.TempDir()
    fakeNativeBinary(t,root,"xkeen",`printf '2.1\n'
`)
    fakeNativeBinary(t,root,"xray",`printf 'Xray 26.10.31\n'
`)
    got:=detectNativeVersions(context.Background(),root)
    if got.Runtime!="NOT_VERIFIED" || got.XKeen.Version!="2.1" ||
        got.Xray.Version!="26.10.31" {
        t.Fatalf("versions must not be confused with egress: %+v",got)
    }
    fakeNativeBinary(t,root,"xray",`printf 'Xray 26.10.31\n'
`)
    if err:=os.Chmod(filepath.Join(root,"sbin/xray"),0600);err!=nil{t.Fatal(err)}
    nonexec:=oneNativeVersion(context.Background(),root,"xray")
    if nonexec.State!="UNKNOWN" || nonexec.Version!="" {
        t.Fatalf("nonexecutable native binary must fail closed: %+v",nonexec)
    }
}

func TestActualXKeenStableMetaFormat(t *testing.T) {
    // Observed read-only on HOME: XKeen 2.1 Stable includes ANSI colors
    // and also names its Xray version. The two must not be conflated.
    output:="  \x1b[93mXKeen 2.1 Stable\x1b[0m (время сборки: \x1b[96m2026-10-06 10:58:45 MSK\x1b[0m)\n"+
        "  Ядро проксирования Xray версии \x1b[93m26.10.31\x1b[0m\n"
    if v:=parseNativeVersion("xkeen",output);v!="2.1" {
        t.Fatalf("official ANSI-colored XKeen 2.1 Stable output not recognized: %q",v)
    }
    if v:=parseNativeVersion("xray","Xray 26.10.31 (Xray, Penetrates Everything.) a4a3dff (go1.27.3 linux/arm64)");v!="26.10.31" {
        t.Fatalf("official Xray 26.10.31 output not recognized: %q",v)
    }
}
