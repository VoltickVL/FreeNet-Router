package main

import (
    "context"
    "errors"
    "fmt"
    "strings"
    "testing"
)

type fakeFreeNetAutoCore struct {
    override string
    selected string
    calls []string
    failSwitch bool
    failRollback bool
}
func (core *fakeFreeNetAutoCore) command(_ context.Context,args ...string)([]byte,error){
    if len(args)==0{return nil,errors.New("empty request")}
    core.calls=append(core.calls,strings.Join(args," "))
    switch args[0] {
    case "bi":
        s:="Balancer: freenet-auto-vpn\nSelecting Override:\n"
        if core.override!="" {s+="    0   "+core.override+"\n"}
        s+="Selects:\n    0   "+core.selected+"\n"
        return []byte(s),nil
    case "bo":
        if len(args)<6{return nil,errors.New("incomplete override")}
        if args[len(args)-1]=="-r" {
            if core.failRollback{return nil,errors.New("cannot restore")}
            core.override=""
        }else{
            if core.failSwitch{return nil,errors.New("candidate rejected")}
            core.override=args[len(args)-1]
        }
        return []byte("ok"),nil
    }
    return nil,fmt.Errorf("unexpected command")
}
func TestFreeNetAutoSwitcherIsNoRestartAndActuallyVerified(t *testing.T){
    core:=&fakeFreeNetAutoCore{selected:"vless-reality"}
    approved:=map[string]bool{"vless-reality":true,"freenet-auto-node-1":true}
    probes:=0
    receipt,err:=freeNetAutoSwitchWith(context.Background(),"127.0.0.1:10085",
        "freenet-auto-node-1",approved,core.command,
        func(context.Context)error{probes++;return nil},
        func()string{return "xray-owned-pid:starttime"})
    if err!=nil || receipt.Mutation!="SWITCHED" ||
      receipt.Target!="freenet-auto-node-1" || probes!=1{
        t.Fatalf("verified no-restart switch failed: %+v %v",receipt,err)
    }
    for _,call:=range core.calls{
        if strings.Contains(call,"restart")||strings.Contains(call,"sh ")||
          strings.Contains(call,"core-restart"){t.Fatal("switch restarted or invoked a shell")}
    }
    if core.override!="freenet-auto-node-1"{t.Fatal("balancer override not active")}
}
func TestFreeNetAutoSwitcherRestoresPreviousOverrideOnFailure(t *testing.T){
    core:=&fakeFreeNetAutoCore{selected:"vless-reality",override:"freenet-auto-node-2"}
    approved:=map[string]bool{"vless-reality":true,"freenet-auto-node-1":true,"freenet-auto-node-2":true}
    receipt,err:=freeNetAutoSwitchWith(context.Background(),"127.0.0.1:10085",
        "freenet-auto-node-1",approved,core.command,
        func(context.Context)error{return errors.New("HTTPS probe timeout")},
        func()string{return "xray-owned-pid:starttime"})
    if err==nil || receipt.Mutation!="ROLLED_BACK"||
      receipt.Rollback!="SUCCESS" || core.override!="freenet-auto-node-2"{
        t.Fatalf("failed target changed working override: %+v %v",receipt,err)
    }
}
func TestFreeNetAutoSwitcherStopsOnUnverifiedRollback(t *testing.T){
    core:=&fakeFreeNetAutoCore{selected:"vless-reality",failRollback:true}
    approved:=map[string]bool{"vless-reality":true,"freenet-auto-node-1":true}
    receipt,err:=freeNetAutoSwitchWith(context.Background(),"127.0.0.1:10085",
        "freenet-auto-node-1",approved,core.command,
        func(context.Context)error{return errors.New("HTTPS failure")},
        func()string{return "xray-owned-pid:starttime"})
    if err==nil || receipt.Mutation!="STOP"||receipt.Rollback!="FAILED/UNKNOWN"{
        t.Fatalf("unverified rollback did not STOP: %+v %v",receipt,err)
    }
}
func TestFreeNetAutoSwitcherDisallowsUnapprovedAndExposedTargets(t *testing.T){
    tests:=[]struct{addr,target string;approved map[string]bool}{
        {"0.0.0.0:10085","freenet-auto-node-1",map[string]bool{"freenet-auto-node-1":true}},
        {"127.0.0.1:10085","direct",map[string]bool{"direct":true}},
        {"127.0.0.1:10085","freenet-auto-node-2",map[string]bool{"freenet-auto-node-1":true}},
        {"127.0.0.1:80","freenet-auto-node-1",map[string]bool{"freenet-auto-node-1":true}},
    }
    for _,tc:=range tests{
        t.Run(tc.addr+"/"+tc.target,func(t *testing.T){
            commands:=0
            receipt,err:=freeNetAutoSwitchWith(context.Background(),tc.addr,tc.target,
                tc.approved,func(context.Context,...string)([]byte,error){commands++;return nil,nil},
                func(context.Context)error{return nil},
                func()string{return "pid"})
            if err==nil||commands>0||receipt.Mutation!="NONE"{
                t.Fatal("unsafe switch reached RPC")
            }
        })
    }
}
func TestFreeNetAutoBalancerInfoRejectsAmbiguousAndUnknownTargets(t *testing.T){
    for _,sample:=range []string{"", "Selects:\n 0 vless-reality",
        "Selecting Override:\n 0 direct\nSelects:\n 0 vless-reality",
        "Selecting Override:\n 0 freenet-auto-node-1\n 1 freenet-auto-node-2\nSelects:\n 0 vless-reality"} {
        _,_,err:=freeNetAutoParseBalancerInfo(sample)
        if err==nil{t.Fatal("ambiguous balancer info was accepted")}
    }
}
