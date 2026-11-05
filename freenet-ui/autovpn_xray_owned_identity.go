package main

import (
    "bytes"
    "os"
    "path/filepath"
    "strconv"
    "strings"
)

// A successful RPC is not evidence that the Xray process remained unchanged.
// Bind every no-restart decision to the unique process started with our actual
// configuration directory, including its /proc start tick (PID reuse guard).
func freeNetAutoOwnedXrayIdentityFrom(procRoot,configDir string)string{
    if !filepath.IsAbs(configDir)||configDir=="/"{return ""}
    entries,err:=os.ReadDir(procRoot)
    if err!=nil{return ""}
    identity:=""
    for _,entry:=range entries{
        if !entry.IsDir(){continue}
        pid,err:=strconv.Atoi(entry.Name())
        if err!=nil||pid<1{continue}
        dir:=filepath.Join(procRoot,entry.Name())
        comm,err:=os.ReadFile(filepath.Join(dir,"comm"))
        if err!=nil||strings.TrimSpace(string(comm))!="xray"{continue}
        env,err:=os.ReadFile(filepath.Join(dir,"environ"))
        if err!=nil{continue}
        wanted:="XRAY_LOCATION_CONFDIR="+filepath.Clean(configDir)
        if !envContains(env,wanted){continue}
        if envContains(env,isolatedXrayProbeFlag){continue}
        stat,err:=os.ReadFile(filepath.Join(dir,"stat"))
        if err!=nil{return ""}
        suffix:=bytes.LastIndexByte(stat,')')
        if suffix<0{return ""}
        fields:=strings.Fields(string(stat[suffix+1:]))
        if len(fields)<=19||fields[19]==""{return ""}
        start,err:=strconv.ParseUint(fields[19],10,64)
        if err!=nil||start==0{return ""}
        if identity!=""{return ""} // unexpected second core: STOP
        identity=strconv.Itoa(pid)+":"+strconv.FormatUint(start,10)
    }
    return identity
}
func (a *app) freeNetAutoXrayIdentity()string{
    if a==nil||a.cfg.OutPath==""{return ""}
    return freeNetAutoOwnedXrayIdentityFrom("/proc",filepath.Dir(a.cfg.OutPath))
}
