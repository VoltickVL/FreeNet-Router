package main

// Official Xray "run -test -confdir" validates native config without starting
// the server. This is never a replacement for live VPN/DNS/client egress checks.
import (
    "context"
    "errors"
    "io"
    "os"
    "os/exec"
    "path/filepath"
    "strings"
    "time"
)

type nativeXrayValidation struct {
    State string `json:"state"`
    Runtime string `json:"runtime"`
}

func validateNativeXray(ctx context.Context,root string) nativeXrayValidation {
    report:=nativeXrayValidation{State:"UNKNOWN",Runtime:"NOT_VERIFIED"}
    binaryState:=observedPath(root,"sbin/xray",false)
    if binaryState=="MISSING"{report.State="MISSING";return report}
    if binaryState!="PRESENT"{return report}
    binary:=filepath.Join(root,"sbin/xray")
    info,err:=os.Lstat(binary)
    if err!=nil || !info.Mode().IsRegular() || info.Mode()&0111==0{return report}
    rel:="etc/xray/configs"
    dirState:=observedPath(root,rel,true)
    if dirState=="MISSING"{report.State="MISSING";return report}
    if dirState!="PRESENT"{return report}
    dir:=filepath.Join(root,rel)
    entries,err:=os.ReadDir(dir)
    if err!=nil{return report}
    count:=0
    for _,entry:=range entries {
        name:=entry.Name()
        switch strings.ToLower(filepath.Ext(name)) {
        case ".json",".jsonc",".toml",".yaml",".yml":
            item,statErr:=os.Lstat(filepath.Join(dir,name))
            if statErr!=nil || !item.Mode().IsRegular() || item.Mode()&os.ModeSymlink!=0 {
                return report
            }
            count++
        }
    }
    // Official Xray falls back to config.json/stdin if no confdir files exist.
    // Do NOT accidentally validate a different config than XKeen's native one.
    if count==0{report.State="MISSING";return report}

    timeout,cancel:=context.WithTimeout(ctx,15*time.Second)
    defer cancel()
    cmd:=exec.CommandContext(timeout,binary,"run","-test","-confdir",dir)
    // Config errors may echo credentials, Reality keys, endpoints or paths.
    // Discard ALL output. Never send arbitrary upstream diagnostics to UI.
    cmd.Stdout=io.Discard
    cmd.Stderr=io.Discard
    cmd.Stdin=nil
    cmd.WaitDelay=500*time.Millisecond
    if observedPath(root,"etc/xray/dat",true)=="PRESENT" {
        cmd.Env=append(os.Environ(),"XRAY_LOCATION_ASSET="+filepath.Join(root,"etc/xray/dat"))
    }
    if err:=cmd.Run();err!=nil {
        if errors.Is(timeout.Err(),context.DeadlineExceeded){
            report.State="TIMEOUT"
            return report
        }
        var exit *exec.ExitError
        if errors.As(err,&exit){
            report.State="REJECTED"
        }
        return report
    }
    report.State="VALIDATED_CONFIG_ONLY"
    return report
}
