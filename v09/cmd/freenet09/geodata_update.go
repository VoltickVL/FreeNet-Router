package main

// GeoData updates belong to the installed XKeen 2.1. FreeNet does not
// download databases, create a scheduler, or edit Xray/XKeen configuration.
import (
    "context"
    "encoding/json"
    "io"
    "net/http"
    "os"
    "os/exec"
    "path/filepath"
    "time"
)

type nativeGeoUpdateReport struct {
    State string `json:"state"`
    Runtime string `json:"runtime"`
}

func updateNativeGeoData(ctx context.Context, root string) nativeGeoUpdateReport {
    result:=nativeGeoUpdateReport{State:"STOP",Runtime:"NOT_VERIFIED"}
    // Legacy FreeNet can still manage geodata; never create a second writer.
    if observedPath(root,"etc/init.d/S99freenet-ui",false)!="MISSING" ||
        observedPath(root,"sbin/xkeen",false)!="PRESENT" {
        return result
    }
    path:=filepath.Join(root,"sbin/xkeen")
    info,err:=os.Lstat(path)
    if err!=nil || !info.Mode().IsRegular() || info.Mode()&0111==0 {
        return result
    }
    bounded,cancel:=context.WithTimeout(ctx,180*time.Second)
    defer cancel()
    cmd:=exec.CommandContext(bounded,path,"-ug")
    cmd.Stdin=nil
    cmd.Stdout=io.Discard
    cmd.Stderr=io.Discard
    cmd.WaitDelay=500*time.Millisecond
    err=cmd.Run()
    if bounded.Err()==context.DeadlineExceeded {
        result.State="TIMEOUT_UNKNOWN"
    } else if err!=nil || bounded.Err()!=nil {
        result.State="UNKNOWN"
    } else {
        // CLI exit success does not prove data freshness or client egress.
        result.State="NATIVE_COMMAND_SUCCEEDED_NOT_VERIFIED"
    }
    return result
}

func nativeGeoUpdateHTTP(w http.ResponseWriter,r *http.Request,root string,gate chan struct{}) {
    w.Header().Set("Content-Type","application/json; charset=utf-8")
    w.Header().Set("Cache-Control","no-store")
    send:=func(code int,state string){
        w.WriteHeader(code)
        _=json.NewEncoder(w).Encode(nativeGeoUpdateReport{State:state,Runtime:"NOT_VERIFIED"})
    }
    if r.Method!=http.MethodPost || r.TLS==nil ||
        !requestSameOrigin(r) || r.Header.Get("X-FreeNet-Action")!="update-geodata" {
        send(http.StatusForbidden,"STOP")
        return
    }
    if r.ContentLength>0 {
        send(http.StatusBadRequest,"STOP")
        return
    }
    select {
    case gate<-struct{}{}:
        defer func(){<-gate}()
    default:
        send(http.StatusConflict,"BUSY")
        return
    }
    state:=updateNativeGeoData(r.Context(),root)
    status:=http.StatusConflict
    if state.State=="NATIVE_COMMAND_SUCCEEDED_NOT_VERIFIED"{status=http.StatusOK}
    w.WriteHeader(status)
    _=json.NewEncoder(w).Encode(state)
}
