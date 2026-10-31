package main

import (
 "os"
 "path/filepath"
 "strings"
 "testing"
)

func TestXKeenInitRepairDetectsMixedGenerations(t *testing.T) {
 root:=t.TempDir()
 old:="#!/bin/sh\n. \"/opt/sbin/.xkeen/01_info/03_info_cpu.sh\"\n"
 if err:=os.WriteFile(filepath.Join(root,"S99xkeen"),[]byte(old),0755);err!=nil {t.Fatal(err)}
 reason,repair:=xkeenInitRepairNeed(root)
 if !repair||reason!="legacy-init-references-removed-module" {t.Fatalf("must detect mixed 1.x/2.x init: %s %v",reason,repair)}
 if err:=os.WriteFile(filepath.Join(root,"S05xkeen"),[]byte("#!/bin/sh\nname_client=\"xray\"\n"),0755);err!=nil{t.Fatal(err)}
 reason,repair=xkeenInitRepairNeed(root)
 if !repair||reason!="legacy-init-references-removed-module"{t.Fatal("new init must not hide active legacy S99")}
 if err:=os.Remove(filepath.Join(root,"S99xkeen"));err!=nil{t.Fatal(err)}
 reason,repair=xkeenInitRepairNeed(root)
 if repair||reason!="compatible"{t.Fatalf("expected compatible: %s %v",reason,repair)}
}

func TestXKeenInitRepairRejectsUntrustedFiles(t *testing.T) {
 root:=t.TempDir()
 outside:=filepath.Join(t.TempDir(),"sensitive")
 if err:=os.WriteFile(outside,[]byte("secret"),0600);err!=nil{t.Fatal(err)}
 if err:=os.Symlink(outside,filepath.Join(root,"S99xkeen"));err!=nil{t.Skip("symlinks unavailable")}
 reason,ready:=xkeenInitRepairNeed(root)
 if ready||reason!="init-file-untrusted"{t.Fatalf("symlink must STOP: %s %v",reason,ready)}
}

func TestXKeenInitRepairSnapshotAndRollback(t *testing.T) {
 root:=t.TempDir()
 legacy:="#!/bin/sh\n. \"/opt/sbin/.xkeen/01_info/03_info_cpu.sh\"\n"
 if err:=os.WriteFile(filepath.Join(root,"S99xkeen"),[]byte(legacy),0755);err!=nil{t.Fatal(err)}
 snapshot:=filepath.Join(root,"snapshot")
 if err:=os.Mkdir(snapshot,0700);err!=nil{t.Fatal(err)}
 if err:=xkeenInitRepairBackup(root,snapshot);err!=nil{t.Fatal(err)}
 if err:=os.Remove(filepath.Join(root,"S99xkeen"));err!=nil{t.Fatal(err)}
 if err:=os.WriteFile(filepath.Join(root,"S05xkeen"),[]byte("#!/bin/sh\n"),0755);err!=nil{t.Fatal(err)}
 if err:=xkeenInitRepairRollback(root,snapshot);err!=nil{t.Fatal(err)}
 if _,err:=os.Stat(filepath.Join(root,"S05xkeen"));!os.IsNotExist(err){t.Fatalf("unowned new init must be removed, err=%v",err)}
 got,err:=os.ReadFile(filepath.Join(root,"S99xkeen"))
 if err!=nil || string(got)!=legacy{t.Fatal("legacy bytes not restored")}
}

func TestXKeenInitRepairDurableReceipt(t *testing.T) {
 root:=t.TempDir()
 idle,err:=xkeenInitRepairReceipt(root)
 if err!=nil || idle.State!="IDLE"||idle.Rollback!="NOT_NEEDED"{t.Fatal(idle,err)}
 pending:=xkeenInitRepairDisk{State:"RUNNING",Rollback:"PENDING",Reason:"repair-in-progress",Snapshot:filepath.Join(root,"snapshot")}
 if err:=xkeenInitRepairWrite(root,pending);err!=nil{t.Fatal(err)}
 result,err:=xkeenInitRepairReceipt(root)
 if err!=nil||result.State!="RUNNING"||result.Rollback!="PENDING"{t.Fatal(result,err)}
 bytes,err:=os.ReadFile(xkeenInitRepairStateFile(root))
 if err!=nil||!strings.Contains(string(bytes),"\"RUNNING\""){t.Fatal("missing durable state")}
 if err:=os.WriteFile(xkeenInitRepairStateFile(root),[]byte("truncated"),0600);err!=nil{t.Fatal(err)}
 if _,err:=xkeenInitRepairReceipt(root);err==nil {t.Fatal("corrupt receipt must STOP")}
}
