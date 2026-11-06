package main

// XKeen owns the router and proxy lifecycle. FreeNet only invokes the
// installed ORIGINAL S05xkeen init with fixed, documented arguments.
import (
    "context"
    "errors"
    "io"
    "os"
    "os/exec"
    "path/filepath"
    "time"
)

type xkeenServiceReport struct {
    State string `json:"state"`
    Runtime string `json:"runtime"`
    Action string `json:"action,omitempty"`
}

func nativeXKeenInit(ctx context.Context,root,action string) (string,bool) {
    var args []string
    switch action {
    case "status": args=[]string{"status"}
    case "start": args=[]string{"start","on"}
    case "stop": args=[]string{"stop"}
    case "restart": args=[]string{"restart","on"}
    default:return "UNKNOWN",false
    }
    if observedPath(root,"etc/init.d/S05xkeen",false)!="PRESENT" {
        return "UNKNOWN",false
    }
    path:=filepath.Join(root,"etc/init.d/S05xkeen")
    info,err:=os.Lstat(path)
    if err!=nil || !info.Mode().IsRegular() || info.Mode()&0111==0 {
        return "UNKNOWN",false
    }
    deadline:=5*time.Second
    if action!="status"{deadline=45*time.Second}
    c,cancel:=context.WithTimeout(ctx,deadline)
    defer cancel()
    cmd:=exec.CommandContext(c,path,args...)
    cmd.Stdout=io.Discard
    cmd.Stderr=io.Discard
    cmd.Stdin=nil
    cmd.WaitDelay=500*time.Millisecond
    if err:=cmd.Run();err!=nil {
        if errors.Is(c.Err(),context.DeadlineExceeded){return "TIMEOUT",false}
        var exit *exec.ExitError
        if action=="status" && errors.As(err,&exit) {
            // The native init returns nonzero when the proxy is not running.
            return "NOT_RUNNING",false
        }
        return "UNKNOWN",false
    }
    if c.Err()!=nil{return "UNKNOWN",false}
    return "COMMAND_SUCCEEDED",true
}

func nativeXKeenServiceStatus(ctx context.Context,root,procRoot string)xkeenServiceReport{
    report:=xkeenServiceReport{State:"UNKNOWN",Runtime:"NOT_VERIFIED"}
    state,ok:=nativeXKeenInit(ctx,root,"status")
    process:=xrayProcessEvidence(procRoot)
    if ok && process=="PROCESS_DETECTED" {
        report.State="RUNNING_PROCESS_DETECTED"
    } else if state=="NOT_RUNNING" && process=="NOT_DETECTED" {
        report.State="STOPPED"
    } else if state=="TIMEOUT" {
        report.State="TIMEOUT"
    }
    return report
}

func nativeXKeenServiceAction(ctx context.Context,root,procRoot,action string)xkeenServiceReport{
    result:=xkeenServiceReport{State:"STOP",Runtime:"NOT_VERIFIED",Action:action}
    if action!="start" && action!="stop" && action!="restart" {
        return result
    }
    // Old FreeNet may own background writers and policy. Refuse a second
    // Control Center owner even if native XKeen itself is installed.
    if observedPath(root,"etc/init.d/S99freenet-ui",false)!="MISSING" {
        return result
    }
    state,ok:=nativeXKeenInit(ctx,root,action)
    if !ok {
        if state=="TIMEOUT"{result.State="TIMEOUT"}
        return result
    }
    after:=nativeXKeenServiceStatus(context.Background(),root,procRoot)
    // No optimistic green "VPN works": status/process are only evidence.
    if action=="stop" && after.State=="STOPPED" {
        result.State="STOPPED"
    } else if action!="stop" && after.State=="RUNNING_PROCESS_DETECTED" {
        result.State="RUNNING_PROCESS_DETECTED"
    } else {
        result.State="UNKNOWN"
    }
    return result
}
