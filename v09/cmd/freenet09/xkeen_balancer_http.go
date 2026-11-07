package main

import (
 "encoding/json"
 "io"
 "mime"
 "net/http"
)

func nativeBalancerHTTP(w http.ResponseWriter,r *http.Request,root,procRoot string,gate chan struct{}) {
 w.Header().Set("Content-Type","application/json; charset=utf-8")
 w.Header().Set("Cache-Control","no-store")
 write:=func(status int,state string,action string){
  w.WriteHeader(status)
  _=json.NewEncoder(w).Encode(nativeBalancerActionReport{State:state,Runtime:"NOT_VERIFIED",Action:action})
 }
 if r.TLS==nil || r.Method!=http.MethodPost || !requestSameOrigin(r) ||
  r.Header.Get("X-FreeNet-Action")!="xkeen-balancer" {write(http.StatusForbidden,"STOP","");return}
 kind,_,err:=mime.ParseMediaType(r.Header.Get("Content-Type"))
 if err!=nil || kind!="application/json" {write(http.StatusUnsupportedMediaType,"STOP","");return}
 decoder:=json.NewDecoder(http.MaxBytesReader(w,r.Body,512))
 decoder.DisallowUnknownFields()
 var input struct{Action string `json:"action"`}
 if err:=decoder.Decode(&input);err!=nil || decoder.Decode(&struct{}{})!=io.EOF {
  write(http.StatusBadRequest,"STOP","");return
 }
 if input.Action!="next" && input.Action!="prev" && input.Action!="auto" {
  write(http.StatusBadRequest,"STOP","");return
 }
 select {
 case gate<-struct{}{}: defer func(){<-gate}()
 default:write(http.StatusConflict,"BUSY",input.Action);return
 }
 result:=nativeBalancerAction(r.Context(),root,procRoot,input.Action)
 if result.State!="UPSTREAM_COMMAND_ACCEPTED_NOT_VERIFIED" {
  w.WriteHeader(http.StatusConflict)
 }
 _=json.NewEncoder(w).Encode(result)
}
