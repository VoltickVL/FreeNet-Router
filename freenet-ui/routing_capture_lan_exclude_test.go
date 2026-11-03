package main

import (
 "context"
 "errors"
 "strings"
 "testing"
)

func TestCaptureDoctorLANExcludeTable(t *testing.T) {
 const routes="default via 192.168.50.1 dev br0\n192.168.50.0/24 dev br0 proto kernel scope link src 192.168.50.1\n"
 const diskRisk="192.168.0.0/16\n"
 const liveRisk="create user_exclude hash:net family inet\nadd user_exclude 192.168.0.0/16\n"
 cases:=[]struct{name,disk,live string; diskOK,liveOK bool;want string}{
  {"both",diskRisk,liveRisk,true,true,"both"},
  {"disk-only",diskRisk,"create user_exclude hash:net",true,true,"disk-only"},
  {"runtime-only","#192.168.0.0/16\n",liveRisk,true,true,"runtime-only"},
  {"commented","#192.168.0.0/16\n","create user_exclude hash:net",true,true,"no-overlap"},
  {"narrow-host","192.168.50.2\n","add user_exclude 192.168.50.2",true,true,"no-overlap"},
  {"unrelated","10.0.0.0/8\n","add user_exclude 172.16.0.0/12",true,true,"no-overlap"},
  {"disk-unknown",diskRisk,"",true,false,"disk-risk-runtime-unknown"},
  {"live-unknown","",liveRisk,false,true,"runtime-risk-disk-unknown"},
  {"both-unknown","","",false,false,"unverified"},
 }
 for _,tc:=range cases {
  t.Run(tc.name,func(t *testing.T){
   got,_:=captureDoctorLANExcludeState(routes,true,tc.disk,tc.diskOK,tc.live,tc.liveOK)
   if got!=tc.want {t.Fatalf("got %q want %q",got,tc.want)}
  })
 }
 state,_:=captureDoctorLANExcludeState("",false,diskRisk,true,liveRisk,true)
 if state!="unverified" {t.Fatalf("no routes must fail closed, got %s",state)}
}

func TestCaptureDoctorLANExcludeIsReadOnlyAndNoPrivateData(t *testing.T){
 var calls []string
 read:=func(_ context.Context,cmd string,args ...string)(string,bool){
  calls=append(calls,cmd+" "+strings.Join(args," "))
  switch cmd {
  case "ip":return "192.168.50.0/24 dev br0 proto kernel scope link src 192.168.50.1",true
  case "ipset":return "add user_exclude 192.168.0.0/16",true
  }
  return "",false
 }
 readFile:=func(path string)([]byte,error){
  if path!="/opt/etc/xkeen/ip_exclude.lst" {t.Fatalf("unexpected file %s",path)}
  return []byte("#192.168.0.0/16\n"),nil
 }
 state,reason:=captureDoctorLANExclude(context.Background(),read,readFile)
 if state!="runtime-only" || reason!="broad-lan-exclusion-active-only" {t.Fatalf("%s/%s",state,reason)}
 for _,call:=range calls {if call!="ip -4 route show" && call!="ipset save user_exclude" {t.Fatalf("unexpected command %q",call)}}
 if len(calls)!=2 {t.Fatalf("unexpected reads: %v",calls)}
 for _,secret:=range []string{"192.168","user_exclude","192.168.50.1"} {
  if strings.Contains(state+reason,secret){t.Fatalf("private data returned")}
 }
 _,_ = captureDoctorLANExclude(context.Background(),func(context.Context,string,...string)(string,bool){return "",false},func(string)([]byte,error){return nil,errors.New("not present")})
}
