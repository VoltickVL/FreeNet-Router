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


func TestXKeenInitPendingRepairProofFailClosed(t *testing.T) {
 d := vpnTransactionDoctorResponse{
  Mode:"read-only",Mutation:"NONE",Checkpoint:"pending",
  Outbound:"matched",Profile:"matched",Filter:"matched",
  FilesRestored:true,XrayCore:"stopped",MutationBlocked:true,
  Decision:"STOP_XRAY_NOT_CONFIRMED",LockState:"absent",
 }
 inv := xrayProcessInventoryResponse{Success:true,Mutation:"NONE",State:"production-xray-absent"}
 hook := "legacy-removed-module-reference"
 if !xkeenInitPendingRepairProof(d,inv,hook) { t.Fatal("verified Work checkpoint not admissible for targeted init-only recovery") }
 cases:=[]struct{
  name string
  mutate func(*vpnTransactionDoctorResponse,*xrayProcessInventoryResponse,*string)
 }{
  {"unknown-checkpoint",func(d *vpnTransactionDoctorResponse,_ *xrayProcessInventoryResponse,_ *string){d.Checkpoint="invalid"}},
  {"original-outbound-drift",func(d *vpnTransactionDoctorResponse,_ *xrayProcessInventoryResponse,_ *string){d.Outbound="different"}},
  {"original-profile-drift",func(d *vpnTransactionDoctorResponse,_ *xrayProcessInventoryResponse,_ *string){d.Profile="different"}},
  {"original-filter-drift",func(d *vpnTransactionDoctorResponse,_ *xrayProcessInventoryResponse,_ *string){d.Filter="different"}},
  {"checkpoint-bytes-unrestored",func(d *vpnTransactionDoctorResponse,_ *xrayProcessInventoryResponse,_ *string){d.FilesRestored=false}},
  {"running-xray",func(d *vpnTransactionDoctorResponse,_ *xrayProcessInventoryResponse,_ *string){d.XrayCore="running"}},
  {"unknown-xray",func(d *vpnTransactionDoctorResponse,_ *xrayProcessInventoryResponse,_ *string){d.XrayCore="unknown"}},
  {"incomplete-recovery",func(d *vpnTransactionDoctorResponse,_ *xrayProcessInventoryResponse,_ *string){d.RecoveryAttempt="unresolved"}},
  {"active-writer",func(d *vpnTransactionDoctorResponse,_ *xrayProcessInventoryResponse,_ *string){d.LockState="pid_present_unverified"}},
  {"unknown-operation",func(d *vpnTransactionDoctorResponse,_ *xrayProcessInventoryResponse,_ *string){d.Decision="STOP_FILES_UNRESOLVED"}},
  {"foreign-process",func(_ *vpnTransactionDoctorResponse,i *xrayProcessInventoryResponse,_ *string){i.UnknownCount=1}},
  {"existing-production",func(_ *vpnTransactionDoctorResponse,i *xrayProcessInventoryResponse,_ *string){i.ProductionCount=1}},
  {"active-probe",func(_ *vpnTransactionDoctorResponse,i *xrayProcessInventoryResponse,_ *string){i.ProbeCount=1}},
  {"zombie",func(_ *vpnTransactionDoctorResponse,i *xrayProcessInventoryResponse,_ *string){i.ZombieCount=1}},
  {"inventory-unverified",func(_ *vpnTransactionDoctorResponse,i *xrayProcessInventoryResponse,_ *string){i.Success=false}},
  {"legacy-hook-not-confirmed",func(_ *vpnTransactionDoctorResponse,_ *xrayProcessInventoryResponse,h *string){*h="modern-generated-hook"}},
  {"netfilter-symlink-untrusted",func(_ *vpnTransactionDoctorResponse,_ *xrayProcessInventoryResponse,h *string){*h="untrusted"}},
 }
 for _,tc:=range cases {
  t.Run(tc.name,func(t *testing.T){
   dd,ii,hh:=d,inv,hook
   tc.mutate(&dd,&ii,&hh)
   if xkeenInitPendingRepairProof(dd,ii,hh){t.Fatal("unverified checkpoint/runtime/hook admitted for native mutation")}
  })
 }
}

func TestXKeenInitNetfilterSnapshotRollbackAndSymlinkFence(t *testing.T) {
 root:=t.TempDir()
 snap:=filepath.Join(root,"snapshot")
 if err:=os.Mkdir(snap,0700);err!=nil{t.Fatal(err)}
 hook:=filepath.Join(root,"proxy.sh")
 original:="#!/bin/sh\n. '/opt/sbin/.xkeen/01_info/03_info_cpu.sh'\n# private-token-must-not-be-logged\n"
 if err:=os.WriteFile(hook,[]byte(original),0755);err!=nil{t.Fatal(err)}
 if err:=xkeenInitRepairHookBackup(hook,snap);err!=nil{t.Fatal(err)}
 metadata,err:=os.Stat(filepath.Join(snap,xkeenRepairHookSnapshotName))
 if err!=nil || metadata.Mode().Perm() & 0077 != 0 {t.Fatal("snapshot must be owner-only")}
 if err:=os.WriteFile(hook,[]byte("#!/bin/sh\n# XKeen: Auto-generated file. DO NOT EDIT!\n"),0755);err!=nil{t.Fatal(err)}
 if err:=xkeenInitRepairHookRollback(hook,snap);err!=nil{t.Fatal(err)}
 restored,err:=os.ReadFile(hook)
 if err!=nil || string(restored)!=original {t.Fatal("netfilter restoration must be byte-for-byte")}
 if err:=os.Remove(hook);err!=nil{t.Fatal(err)}
 outside:=filepath.Join(t.TempDir(),"outside")
 if err:=os.WriteFile(outside,[]byte("sensitive"),0600);err!=nil{t.Fatal(err)}
 if err:=os.Symlink(outside,hook);err!=nil{t.Skip("symlinks unavailable")}
 if err:=xkeenInitRepairHookRollback(hook,snap);err==nil {t.Fatal("symlinked live hook bypassed safe rollback")}
 unchanged,err:=os.ReadFile(outside)
 if err!=nil || string(unchanged)!="sensitive" {t.Fatal("external file was modified")}
}

func TestXKeenInitNetfilterSnapshotRequiresExactLegacyHook(t *testing.T){
 dir:=t.TempDir()
 snap:=filepath.Join(dir,"snapshot")
 if err:=os.Mkdir(snap,0700);err!=nil {t.Fatal(err)}
 hook:=filepath.Join(dir,"proxy.sh")
 if err:=xkeenInitRepairHookBackup(hook,snap);err==nil {t.Fatal("absent hook accepted")}
 if err:=os.WriteFile(hook,[]byte("# XKeen: Auto-generated file. DO NOT EDIT!\n"),0755);err!=nil{t.Fatal(err)}
 if err:=xkeenInitRepairHookBackup(hook,snap);err==nil {t.Fatal("modern hook overwritten as legacy")}
}
