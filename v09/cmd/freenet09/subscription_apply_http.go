package main

import (
    "encoding/json"
    "io"
    "mime"
    "net/http"
)

type subscriptionApplyInput struct {
    URL string `json:"url"`
    Refresh bool `json:"refresh"`
    Balancer string `json:"balancer"`
    Selector string `json:"selector"`
}

// This handler is deliberately NOT wired to the anonymous preview server.
// It can be reached only after secureControlCenter authenticates a TLS session.
func subscriptionApplyHTTP(w http.ResponseWriter,r *http.Request,root,procRoot string,gate chan struct{}){
    w.Header().Set("Cache-Control","no-store")
    w.Header().Set("Content-Type","application/json; charset=utf-8")
    send:=func(status int,out nativeApplyResult){
        w.WriteHeader(status)
        _=json.NewEncoder(w).Encode(out)
    }
    if r.Method!=http.MethodPost || r.TLS==nil || !requestSameOrigin(r) ||
        r.Header.Get("X-FreeNet-Action")!="apply-subscription" {
        send(http.StatusForbidden,applyStopped())
        return
    }
    ct,_,err:=mime.ParseMediaType(r.Header.Get("Content-Type"))
    if err!=nil || ct!="application/json" {
        send(http.StatusUnsupportedMediaType,applyStopped())
        return
    }
    select {
    case gate<-struct{}{}:defer func(){<-gate}()
    default:
        out:=applyStopped()
        out.State="BUSY"
        send(http.StatusConflict,out)
        return
    }
    decoder:=json.NewDecoder(http.MaxBytesReader(w,r.Body,8192))
    decoder.DisallowUnknownFields()
    var request subscriptionApplyInput
    if err:=decoder.Decode(&request);err!=nil || decoder.Decode(&struct{}{})!=io.EOF ||
        request.Refresh && request.URL!="" || !request.Refresh && request.URL=="" {
        send(http.StatusBadRequest,applyStopped())
        return
    }
    if !request.Refresh {
        if _,err:=validateSubscriptionURL(request.URL);err!=nil{
            send(http.StatusBadRequest,applyStopped())
            return
        }
    }
    result:=subscriptionOperationForBalancer(r.Context(),root,procRoot,
        request.URL,request.Refresh,request.Balancer,request.Selector)
    if result.State!="CONFIG_APPLIED_NOT_EGRESS_VERIFIED" {
        send(http.StatusConflict,result)
        return
    }
    send(http.StatusOK,result)
}
