package main

import (
 "context"
 "crypto/tls"
 "net/http"
 "net/http/httptest"
 "os"
 "path/filepath"
 "strings"
 "testing"
)

func TestNativeGeoDataUsesOnlyUpstreamUG(t *testing.T){
 root:=t.TempDir()
 marker:=filepath.Join(t.TempDir(),"called")
 writeFixture(t,root,"sbin/xkeen","#!/bin/sh\n[ \"$#\" = 1 ] && [ \"$1\" = -ug ] || exit 9\nprintf ok > "+marker+"\n")
 if err:=os.Chmod(filepath.Join(root,"sbin/xkeen"),0700);err!=nil{t.Fatal(err)}
 got:=updateNativeGeoData(context.Background(),root)
 if got.State!="NATIVE_COMMAND_SUCCEEDED_NOT_VERIFIED" || got.Runtime!="NOT_VERIFIED"{t.Fatalf("incorrect evidence: %+v",got)}
 if _,err:=os.Stat(marker);err!=nil{t.Fatalf("upstream -ug not called: %v",err)}
 writeFixture(t,root,"etc/init.d/S99freenet-ui","legacy")
 if got:=updateNativeGeoData(context.Background(),root);got.State!="STOP"{t.Fatalf("legacy writer not fenced: %+v",got)}
}
func TestGeoDataHTTPGuards(t *testing.T){
 root:=t.TempDir()
 gate:=make(chan struct{},1)
 mk:=func(method string)*http.Request {
   r:=httptest.NewRequest(method,"https://router.example/api/native/geodata/update",nil)
   r.TLS=&tls.ConnectionState{}
   r.Header.Set("Origin","https://router.example")
   r.Header.Set("X-FreeNet-Action","update-geodata")
   return r
 }
 forbidden:=httptest.NewRecorder()
 r:=mk(http.MethodPost);r.Header.Del("Origin")
 nativeGeoUpdateHTTP(forbidden,r,root,gate)
 if forbidden.Code!=http.StatusForbidden{t.Fatalf("missing CSRF accepted: %d",forbidden.Code)}
 gate<-struct{}{}
 busy:=httptest.NewRecorder()
 nativeGeoUpdateHTTP(busy,mk(http.MethodPost),root,gate)
 <-gate
 if busy.Code!=http.StatusConflict || !strings.Contains(busy.Body.String(),"BUSY"){t.Fatalf("gate not enforced: %d %s",busy.Code,busy.Body.String())}
 missing:=httptest.NewRecorder()
 nativeGeoUpdateHTTP(missing,mk(http.MethodPost),root,gate)
 if missing.Code!=http.StatusConflict || !strings.Contains(missing.Body.String(),"STOP"){t.Fatalf("missing xkeen not refused: %d %s",missing.Code,missing.Body.String())}
}
func TestGeoDataOnlyInAuthenticatedHTTPS(t *testing.T){
 root,_,auth:=newTestAuth(t)
 secured:=secureControlCenter(root,t.TempDir(),auth)
 anon:=httptest.NewRecorder()
 secured.ServeHTTP(anon,httptest.NewRequest(http.MethodPost,"https://router.example/api/native/geodata/update",nil))
 if anon.Code!=http.StatusUnauthorized{t.Fatalf("anonymous native update accepted: %d",anon.Code)}
 preview:=httptest.NewRecorder()
 handlersWithProc(root,t.TempDir()).ServeHTTP(preview,httptest.NewRequest(http.MethodPost,"http://127.0.0.1:1009/api/native/geodata/update",nil))
 if preview.Code==http.StatusOK{t.Fatal("preview unexpectedly supports mutation")}
}
