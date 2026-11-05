package main

import (
    "os"
    "path/filepath"
    "strconv"
    "strings"
    "testing"
)

func freeNetAutoTestProc(t *testing.T,root string,pid int,start uint64,config string){
    t.Helper()
    dir:=filepath.Join(root,strconv.Itoa(pid))
    if err:=os.MkdirAll(dir,0700);err!=nil{t.Fatal(err)}
    files:=map[string][]byte{
        "comm":[]byte("xray\n"),
        "environ":[]byte("XRAY_LOCATION_CONFDIR="+config+"\x00PATH=/opt/bin\x00"),
        "stat":[]byte(strconv.Itoa(pid)+" (xray) S "+strings.Repeat("1 ",18)+strconv.FormatUint(start,10)+" 0 0\n"),
    }
    for name,data:=range files {
        if err:=os.WriteFile(filepath.Join(dir,name),data,0600);err!=nil{t.Fatal(err)}
    }
}

func TestFreeNetAutoXrayIdentityRejectsUnknownAndMultipleCores(t *testing.T){
    proc:=t.TempDir()
    conf:="/opt/etc/xray/configs"
    if got:=freeNetAutoOwnedXrayIdentityFrom(proc,conf);got!="" {
        t.Fatal("invented identity without a process")
    }
    freeNetAutoTestProc(t,proc,101,67890,conf)
    if got:=freeNetAutoOwnedXrayIdentityFrom(proc,conf);got!="101:67890"{
        t.Fatalf("missing unique process starttime: %q",got)
    }
    freeNetAutoTestProc(t,proc,102,67891,conf)
    if got:=freeNetAutoOwnedXrayIdentityFrom(proc,conf);got!="" {
        t.Fatal("multiple Xray processes accepted")
    }
    if got:=freeNetAutoOwnedXrayIdentityFrom(proc,"not-absolute");got!="" {
        t.Fatal("relative core identity accepted")
    }
}
