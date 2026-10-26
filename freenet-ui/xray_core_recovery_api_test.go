package main

import (
    "bytes"
    "net/http"
    "net/http/httptest"
    "testing"
)

func TestXrayCoreRecoveryRequiresAuthenticatedPost(t *testing.T) {
    a := &app{sem:make(chan struct{},1)}
    mux := http.NewServeMux()
    mux.HandleFunc("POST /api/xray/core/recover",a.requireAuth(a.handleXrayCoreRecovery))
    req := httptest.NewRequest(http.MethodPost,"/api/xray/core/recover",bytes.NewBufferString(`{"expected_version":"v26.9.9"}`))
    req.Header.Set("Content-Type","application/json")
    recorder:=httptest.NewRecorder()
    mux.ServeHTTP(recorder,req)
    if recorder.Code==http.StatusOK || recorder.Code==http.StatusNotFound {
        t.Fatalf("native recovery must be authenticated; got HTTP %d",recorder.Code)
    }
}

func TestXrayCoreRecoveryRejectsMissingOrInvalidExpectedVersion(t *testing.T) {
    a:=&app{sem:make(chan struct{},1)}
    cases:=[]string{`{}`,`{"expected_version":""}`,`{"expected_version":"../../xray"}`}
    for _,body:=range cases {
        rec:=httptest.NewRecorder()
        req:=httptest.NewRequest(http.MethodPost,"/api/xray/core/recover",bytes.NewBufferString(body))
        a.handleXrayCoreRecovery(rec,req)
        if rec.Code != http.StatusBadRequest {
            t.Fatalf("invalid recovery version %q accepted with HTTP %d",body,rec.Code)
        }
    }
}
