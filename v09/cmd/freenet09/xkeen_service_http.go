package main

import (
    "encoding/json"
    "io"
    "mime"
    "net/http"
)

// Mutation never reaches the unauthenticated preview. The same bounded
// channel gates subscription imports and XKeen lifecycle actions.
func nativeXKeenServiceHTTP(w http.ResponseWriter,r *http.Request,root,procRoot string,gate chan struct{}){
    w.Header().Set("Content-Type","application/json; charset=utf-8")
    w.Header().Set("Cache-Control","no-store")
    write:=func(status int,v xkeenServiceReport){
        w.WriteHeader(status)
        _=json.NewEncoder(w).Encode(v)
    }
    stop:=xkeenServiceReport{State:"STOP",Runtime:"NOT_VERIFIED"}
    if r.TLS==nil || !requestSameOrigin(r) {write(http.StatusForbidden,stop);return}
    if r.Method==http.MethodGet {
        write(http.StatusOK,nativeXKeenServiceStatus(r.Context(),root,procRoot))
        return
    }
    if r.Method!=http.MethodPost || r.Header.Get("X-FreeNet-Action")!="xkeen-service" {
        write(http.StatusForbidden,stop)
        return
    }
    contentType,_,err:=mime.ParseMediaType(r.Header.Get("Content-Type"))
    if err!=nil || contentType!="application/json" {
        write(http.StatusUnsupportedMediaType,stop)
        return
    }
    decoder:=json.NewDecoder(http.MaxBytesReader(w,r.Body,512))
    decoder.DisallowUnknownFields()
    var request struct{Action string `json:"action"`}
    if err=decoder.Decode(&request);err!=nil || decoder.Decode(&struct{}{})!=io.EOF {
        write(http.StatusBadRequest,stop)
        return
    }
    if request.Action!="start" && request.Action!="stop" && request.Action!="restart" {
        write(http.StatusBadRequest,stop)
        return
    }
    select {
    case gate<-struct{}{}:
        defer func(){<-gate}()
    default:
        write(http.StatusConflict,xkeenServiceReport{State:"BUSY",Runtime:"NOT_VERIFIED"})
        return
    }
    result:=nativeXKeenServiceAction(r.Context(),root,procRoot,request.Action)
    if result.State!="RUNNING_PROCESS_DETECTED" && result.State!="STOPPED" {
        write(http.StatusConflict,result)
        return
    }
    write(http.StatusOK,result)
}
