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

func newXKeenServiceFixture(t *testing.T,script string,running bool)(string,string){
    t.Helper()
    root:=t.TempDir()
    proc:=t.TempDir()
    writeFixture(t,root,"etc/init.d/S05xkeen","#!/bin/sh\n"+script)
    path:=filepath.Join(root,"etc/init.d/S05xkeen")
    if err:=os.Chmod(path,0700);err!=nil{t.Fatal(err)}
    if running{writeFixture(t,proc,"123/comm","xray\n")}
    return root,proc
}

func TestNativeXKeenServiceUsesOnlyOfficialInitCommands(t *testing.T){
    root,proc:=newXKeenServiceFixture(t,
        "case \"$1:$2\" in\n"+
            "start:on|restart:on|status:) exit 0 ;;\n"+
            "*) echo 'SECRET-XKEEN-KEY-DO-NOT-SHOW' >&2; exit 11 ;;\n"+
            "esac\n",true)
    for _,action:=range []string{"start","restart"} {
        got:=nativeXKeenServiceAction(context.Background(),root,proc,action)
        if got.State!="RUNNING_PROCESS_DETECTED" || got.Runtime!="NOT_VERIFIED" ||
            got.Action!=action {
            t.Fatalf("official XKeen %s invalid: %+v",action,got)
        }
        raw,_:=json.Marshal(got)
        if bytes.Contains(raw,[]byte("SECRET-XKEEN")) {
            t.Fatal("native stderr leaked to response")
        }
    }
    got:=nativeXKeenServiceStatus(context.Background(),root,proc)
    if got.State!="RUNNING_PROCESS_DETECTED" || got.Runtime!="NOT_VERIFIED" {
        t.Fatalf("init status is not egress status: %+v",got)
    }
    bad:=nativeXKeenServiceAction(context.Background(),root,proc,"-install")
    if bad.State!="STOP"{t.Fatalf("arbitrary XKeen CLI action allowed: %+v",bad)}
}

func TestNativeXKeenServiceStopAndUnknownFailClosed(t *testing.T){
    root,proc:=newXKeenServiceFixture(t,
        "case \"$1\" in\nstop) exit 0 ;;\nstatus) exit 1 ;;\n*) exit 77 ;;\nesac\n",false)
    state:=nativeXKeenServiceAction(context.Background(),root,proc,"stop")
    if state.State!="STOPPED" || state.Runtime!="NOT_VERIFIED" {
        t.Fatalf("native stop not confirmed: %+v",state)
    }
    writeFixture(t,proc,"222/comm","xray\n")
    inconclusive:=nativeXKeenServiceAction(context.Background(),root,proc,"stop")
    if inconclusive.State!="UNKNOWN" || inconclusive.Runtime!="NOT_VERIFIED" {
        t.Fatalf("still-running Xray must not report stopped: %+v",inconclusive)
    }
    writeFixture(t,root,"etc/init.d/S99freenet-ui","old writer")
    conflict:=nativeXKeenServiceAction(context.Background(),root,proc,"stop")
    if conflict.State!="STOP"{t.Fatalf("legacy FreeNet conflict ignored: %+v",conflict)}
    if err:=os.Remove(filepath.Join(root,"etc/init.d/S99freenet-ui"));err!=nil{t.Fatal(err)}
    if err:=os.Remove(filepath.Join(root,"etc/init.d/S05xkeen"));err!=nil{t.Fatal(err)}
    missing:=nativeXKeenServiceStatus(context.Background(),root,proc)
    if missing.State!="UNKNOWN"{t.Fatalf("missing native init accepted: %+v",missing)}
}

func TestNativeXKeenLifecycleOnlyAuthenticatedTLS(t *testing.T){
    root,_,auth:=newTestAuth(t)
    secured:=secureControlCenter(root,t.TempDir(),auth)
    forbidden:=httptest.NewRecorder()
    secured.ServeHTTP(forbidden,httptest.NewRequest(http.MethodPost,
        "https://router.example/api/native/xkeen/service",
        strings.NewReader(`{"action":"restart"}`)))
    if forbidden.Code!=http.StatusUnauthorized {
        t.Fatalf("anonymous native lifecycle accepted: %d",forbidden.Code)
    }
    preview:=handlersWithProc(root,t.TempDir())
    anon:=httptest.NewRecorder()
    preview.ServeHTTP(anon,httptest.NewRequest(http.MethodPost,
        "http://127.0.0.1:1009/api/native/xkeen/service",
        strings.NewReader(`{"action":"restart"}`)))
    if anon.Code==http.StatusOK{t.Fatal("loopback anonymous native mutation accepted")}

    gate:=make(chan struct{},1)
    req:=httptest.NewRequest(http.MethodPost,"https://router.example/api/native/xkeen/service",
        strings.NewReader(`{"action":"stop"}`))
    req.TLS=&tls.ConnectionState{}
    req.Header.Set("Content-Type","application/json")
    denied:=httptest.NewRecorder()
    nativeXKeenServiceHTTP(denied,req,root,t.TempDir(),gate)
    if denied.Code!=http.StatusForbidden {t.Fatalf("CSRF Origin missing: %d",denied.Code)}
    req.Header.Set("Origin","https://router.example")
    req.Header.Set("X-FreeNet-Action","xkeen-service")
    gate<-struct{}{}
    busy:=httptest.NewRecorder()
    nativeXKeenServiceHTTP(busy,req,root,t.TempDir(),gate)
    <-gate
    if busy.Code!=http.StatusConflict || !strings.Contains(busy.Body.String(),"BUSY") {
        t.Fatalf("simultaneous XKeen/subscription mutation allowed: %d %s",busy.Code,busy.Body.String())
    }
    invalid:=httptest.NewRecorder()
    badReq:=httptest.NewRequest(http.MethodPost,"https://router.example/api/native/xkeen/service",
        strings.NewReader(`{"action":"-i auto"}`))
    badReq.TLS=&tls.ConnectionState{}
    badReq.Header.Set("Content-Type","application/json")
    badReq.Header.Set("Origin","https://router.example")
    badReq.Header.Set("X-FreeNet-Action","xkeen-service")
    nativeXKeenServiceHTTP(invalid,badReq,root,t.TempDir(),gate)
    if invalid.Code!=http.StatusBadRequest {t.Fatalf("unsafe native op allowed: %d",invalid.Code)}
}

func TestNativeXKeenLifecycleBrowserWiring(t *testing.T){
    app:=handlersWithProc(t.TempDir(),t.TempDir())
    page:=httptest.NewRecorder()
    app.ServeHTTP(page,httptest.NewRequest(http.MethodGet,"/",nil))
    for _,part:=range []string{`data-xkeen-action="start"`,`data-xkeen-action="stop"`,
        `data-xkeen-action="restart"`,`id="xkeen-service-actions"`} {
        if !strings.Contains(page.Body.String(),part) {t.Fatalf("native control missing from panel: %s",part)}
    }
    script:=httptest.NewRecorder()
    app.ServeHTTP(script,httptest.NewRequest(http.MethodGet,"/static/app.js",nil))
    for _,part:=range []string{"/api/native/xkeen/service","X-FreeNet-Action","xkeen-service"} {
        if !strings.Contains(script.Body.String(),part){t.Fatalf("browser native integration missing %s",part)}
    }
}

func TestNativeXKeenSymlinkAndNonExecutableStop(t *testing.T){
    root,proc:=newXKeenServiceFixture(t,"exit 0\n",true)
    path:=filepath.Join(root,"etc/init.d/S05xkeen")
    if err:=os.Chmod(path,0600);err!=nil{t.Fatal(err)}
    if state:=nativeXKeenServiceAction(context.Background(),root,proc,"restart");state.State!="STOP"{
        t.Fatalf("nonexecutable native init accepted: %+v",state)
    }
    outside:=filepath.Join(t.TempDir(),"S05xkeen")
    if err:=os.WriteFile(outside,[]byte("#!/bin/sh\nexit 0\n"),0700);err!=nil{t.Fatal(err)}
    if err:=os.Remove(path);err!=nil{t.Fatal(err)}
    if err:=os.Symlink(outside,path);err!=nil{t.Skip(err)}
    if state:=nativeXKeenServiceAction(context.Background(),root,proc,"restart");state.State!="STOP" {
        t.Fatalf("symlinked init accepted: %+v",state)
    }
    if _,err:=os.Lstat(path);errors.Is(err,os.ErrNotExist){t.Fatal("test failed to create symlink")}
}
