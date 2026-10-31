package main

// Targeted recovery of the XKeen 1.x -> 2.x init.d migration.
// Never reinstall Xray, XKeen, subscriptions, DNS, routes or firewall rules.
import (
 "context"
 "encoding/json"
 "errors"
 "io"
 "net/http"
 "os"
 "path/filepath"
 "strings"
 "time"
)

type xkeenInitRepairResponse struct {
 State string `json:"state"`
 Reason string `json:"reason,omitempty"`
 Installed string `json:"installed_version,omitempty"`
 Ready bool `json:"ready"`
 XrayOnline bool `json:"xray_online"`
 Rollback string `json:"rollback"`
 Mutation string `json:"mutation"`
}

type xkeenInitRepairDisk struct {
 State string `json:"state"`
 Reason string `json:"reason,omitempty"`
 Rollback string `json:"rollback"`
 Snapshot string `json:"snapshot,omitempty"`
}

var (
 xkeenInitRepairRun = func(ctx context.Context, path string) error {
  _, err := runCommand(ctx, path, "-ri")
  return err
 }
 xkeenInitRepairStart = func(ctx context.Context, a *app) error {
  return a.startXrayControlled(ctx)
 }
)

func xkeenInitDir() string { return "/opt/etc/init.d" }
func xkeenInitRepairStateFile(root string) string {
 return filepath.Join(root, ".freenet-xkeen-init-repair-state.json")
}

func xkeenInitSafeFile(path string, optional bool) bool {
 st, err := os.Lstat(path)
 if errors.Is(err, os.ErrNotExist) { return optional }
 return err == nil && st.Mode().IsRegular() && st.Size() > 0 && st.Size() <= 512<<10
}

func xkeenInitReadBounded(path string) (string, bool) {
 if !xkeenInitSafeFile(path, false) { return "", false }
 b, err := os.ReadFile(path)
 return string(b), err == nil
}

func xkeenInitRepairNeed(root string) (string, bool) {
 for _, name := range []string{"S05xkeen","S99xkeen","S99xkeenstart","S24xray"} {
  if !xkeenInitSafeFile(filepath.Join(root,name),true) { return "init-file-untrusted", false }
 }
 old, present := xkeenInitReadBounded(filepath.Join(root,"S99xkeen"))
 if !present {
  if current, ok := xkeenInitReadBounded(filepath.Join(root,"S05xkeen")); ok &&
   !strings.Contains(current, "03_info_cpu.sh") &&
   !strings.Contains(current, "S99xkeen") { return "compatible", false }
  return "init-generation-unverified", false
 }
 if !strings.Contains(old, "03_info_cpu.sh") {
  return "legacy-init-unverified", false
 }
 return "legacy-init-references-removed-module", true
}

func xkeenInitRepairReceipt(root string) (xkeenInitRepairDisk, error) {
 var state xkeenInitRepairDisk
 data, err := os.ReadFile(xkeenInitRepairStateFile(root))
 if errors.Is(err,os.ErrNotExist) {
  return xkeenInitRepairDisk{State:"IDLE",Rollback:"NOT_NEEDED"}, nil
 }
 if err != nil || len(data) > 2048 || json.Unmarshal(data,&state) != nil {
  return state, errors.New("repair-state-unavailable")
 }
 switch state.State {
 case "IDLE","RUNNING","SUCCESS","STOP":
 default: return state, errors.New("repair-state-invalid")
 }
 return state,nil
}

func xkeenInitRepairWrite(root string, state xkeenInitRepairDisk) error {
 data, err := json.Marshal(state)
 if err != nil || len(data)>2048 { return errors.New("repair-state-invalid") }
 f,err:=os.CreateTemp(root,".freenet-xkeen-init-state-*")
 if err!=nil { return err }
 tmp:=f.Name()
 defer os.Remove(tmp)
 if f.Chmod(0600)!=nil {f.Close();return errors.New("repair-state-mode")}
 _,writeErr:=f.Write(append(data,'\n'))
 syncErr:=f.Sync()
 closeErr:=f.Close()
 if writeErr!=nil||syncErr!=nil||closeErr!=nil {return errors.New("repair-state-write")}
 return os.Rename(tmp,xkeenInitRepairStateFile(root))
}

func xkeenInitRepairInventory(a *app, root string) xkeenInitRepairResponse {
 result:=xkeenInitRepairResponse{State:"STOP",Reason:"runtime-unverified",Rollback:"NOT_NEEDED",Mutation:"NONE"}
 if a==nil || !xkeenLiveTreeReady(a.cfg.XKeenPath) {result.Reason="xkeen-installation-unverified";return result}
 v,ch,err:=readXKeenInstalledMetadata(a.cfg.XKeenPath)
 if err!=nil || ch!="stable" || v!="2.1" {
  result.Reason="xkeen-version-not-supported-for-init-repair";return result
 }
 result.Installed=v
 receipt,err:=xkeenInitRepairReceipt(root)
 if err!=nil {result.Reason="repair-receipt-unverified";result.Rollback="UNKNOWN";return result}
 if receipt.State=="RUNNING"||receipt.State=="STOP" {
  result.Reason="previous-repair-incomplete"
  result.Rollback="UNKNOWN"
  if receipt.State=="STOP" {result.Rollback=receipt.Rollback}
  return result
 }
 reason,need:=xkeenInitRepairNeed(root)
 if !need {
  result.Reason=reason
  if reason=="compatible" {result.State="ready"}
  return result
 }
 moduleBase:=filepath.Join(filepath.Dir(a.cfg.XKeenPath),".xkeen")
 tmpl,ok:=xkeenInitReadBounded(filepath.Join(moduleBase,"02_install","07_install_register","04_register_init.sh"))
 if !ok || !strings.Contains(tmpl,"name_client=") ||
  !strings.Contains(tmpl,"start_auto=") ||
  strings.Contains(tmpl,"03_info_cpu.sh") {
  result.Reason="current-init-template-unverified";return result
 }
 importText,ok:=xkeenInitReadBounded(filepath.Join(moduleBase,"01_info","00_info_import.sh"))
 if !ok || !strings.Contains(importText,"03_info_xkeen.sh") ||
  strings.Contains(importText,"03_info_cpu.sh") {
  result.Reason="xkeen-module-generation-mismatch";return result
 }
 if xrayServiceProcessRunning("xray") {result.Reason="xray-already-running";return result}
 binary,_,err:=a.resolvedXrayCoreBinary()
 if err!=nil {result.Reason="xray-identity-unverified";return result}
 configDir,err:=a.xrayCoreLiveConfigDir()
 if err!=nil {result.Reason="xray-config-identity-unverified";return result}
 inv:=scanXrayProcessInventory("/proc",configDir,binary)
 if !inv.Success || inv.State!="production-xray-absent" {
  result.Reason="xray-process-inventory-unverified";return result
 }
 if a.xrayRuntimeDesiredState()!=xrayRuntimeDesiredRunning {
  result.Reason="xray-intentionally-stopped";return result
 }
 if xkeenUpdateBusy(a) {result.Reason="another-operation-or-recovery-busy";return result}
 result.State="repairable"
 result.Reason=reason
 result.Ready=true
 return result
}

var xkeenInitRepairNames=[]string{"S05xkeen","S99xkeen","S99xkeenstart","S24xray"}

func xkeenInitRepairBackup(root, dir string) error {
 for _,name:=range xkeenInitRepairNames {
  from:=filepath.Join(root,name)
  if !xkeenInitSafeFile(from,true) {return errors.New("unsafe-init-file")}
  data,err:=os.ReadFile(from)
  if errors.Is(err,os.ErrNotExist) {continue}
  if err!=nil {return err}
  target:=filepath.Join(dir,name)
  f,err:=os.OpenFile(target,os.O_WRONLY|os.O_CREATE|os.O_EXCL,0600)
  if err!=nil {return err}
  _,werr:=f.Write(data)
  serr:=f.Sync()
  cerr:=f.Close()
  if werr!=nil||serr!=nil||cerr!=nil {return errors.New("init-backup-unconfirmed")}
 }
 return nil
}

// Restore only the explicitly owned init file names from an independent
// verified local snapshot. Do not touch any user Xray/XKeen configuration.
func xkeenInitRepairRollback(root, dir string) error {
 for _,name:=range xkeenInitRepairNames {
  live:=filepath.Join(root,name)
  saved:=filepath.Join(dir,name)
  if !xkeenInitSafeFile(live,true)||!xkeenInitSafeFile(saved,true) {
   return errors.New("rollback-file-untrusted")
  }
  data,err:=os.ReadFile(saved)
  if errors.Is(err,os.ErrNotExist) {
   if removeErr:=os.Remove(live);removeErr!=nil&&!errors.Is(removeErr,os.ErrNotExist){return removeErr}
   continue
  }
  if err!=nil {return err}
  temp,err:=os.CreateTemp(root,".freenet-xkeen-init-restore-*")
  if err!=nil {return err}
  tmp:=temp.Name()
  _,werr:=temp.Write(data)
  modeErr:=temp.Chmod(0755)
  syncErr:=temp.Sync()
  closeErr:=temp.Close()
  if werr!=nil||modeErr!=nil||syncErr!=nil||closeErr!=nil {os.Remove(tmp);return errors.New("restore-write-unconfirmed")}
  if err:=os.Rename(tmp,live);err!=nil {os.Remove(tmp);return err}
 }
 reason,need:=xkeenInitRepairNeed(root)
 if !need || reason!="legacy-init-references-removed-module" {return errors.New("rollback-postcheck-unconfirmed")}
 return nil
}

func xkeenInitRepairApply(ctx context.Context, a *app, root string) xkeenInitRepairResponse {
 failed:=xkeenInitRepairResponse{State:"STOP",Reason:"repair-not-started",Rollback:"NOT_NEEDED",Mutation:"NONE"}
 if a==nil {return failed}
 select {
 case a.sem<-struct{}{}: defer func(){<-a.sem}()
 default:failed.Reason="another-operation-busy";return failed
 }
 unlock,err:=reserveXKeenCrossProcessLock(a.cfg.XKeenPath)
 if err!=nil {failed.Reason="xkeen-lock-unavailable";return failed}
 defer unlock()
 plan:=xkeenInitRepairInventory(a,root)
 if !plan.Ready {return plan}
 snapshot,err:=os.MkdirTemp(root,".freenet-xkeen-init-snapshot-*")
 if err!=nil {failed.Reason="snapshot-unavailable";return failed}
 if err=os.Chmod(snapshot,0700);err!=nil {failed.Reason="snapshot-permissions-unavailable";return failed}
 if err=xkeenInitRepairBackup(root,snapshot);err!=nil {
  failed.Reason="snapshot-unverified"
  return failed
 }
 disk:=xkeenInitRepairDisk{State:"RUNNING",Reason:"repair-in-progress",Rollback:"PENDING",Snapshot:snapshot}
 if xkeenInitRepairWrite(root,disk)!=nil {
  failed.Reason="repair-receipt-not-durable";return failed
 }
 fail:=func(reason string) xkeenInitRepairResponse {
  disk.State="STOP"
  disk.Reason=reason
  disk.Rollback="UNKNOWN"
  if !xrayServiceProcessRunning("xray") {
   if xkeenInitRepairRollback(root,snapshot)==nil {disk.Rollback="SUCCESS"} else {disk.Rollback="FAILED"}
  }
  if xkeenInitRepairWrite(root,disk)!=nil {disk.Rollback="UNKNOWN";disk.Reason="repair-receipt-not-durable"}
  return xkeenInitRepairResponse{State:"STOP",Reason:disk.Reason,Rollback:disk.Rollback,Mutation:"INIT_ATTEMPTED",XrayOnline:xrayServiceProcessRunning("xray")}
 }
 ctx,cancel:=context.WithTimeout(ctx,100*time.Second)
 defer cancel()
 if xkeenInitRepairRun(ctx,a.cfg.XKeenPath)!=nil {return fail("native-init-rebuild-failed")}
 if reason,need:=xkeenInitRepairNeed(root);need||reason!="compatible" {return fail("init-migration-postcheck-failed")}
 if err:=xkeenInitRepairStart(ctx,a);err!=nil {return fail("xray-start-postcheck-failed")}
 if !xrayServiceProcessRunning("xray") {return fail("xray-not-running-after-repair")}
 disk.State="SUCCESS"
 disk.Reason="xkeen-init-compatible-xray-running"
 disk.Rollback="NOT_NEEDED"
 if xkeenInitRepairWrite(root,disk)!=nil {
  return xkeenInitRepairResponse{State:"STOP",Reason:"repair-result-not-durable",Rollback:"UNKNOWN",Mutation:"INIT_ATTEMPTED",XrayOnline:true}
 }
 return xkeenInitRepairResponse{State:"SUCCESS",Reason:disk.Reason,Installed:"2.1",Ready:true,Rollback:"NOT_NEEDED",Mutation:"INIT_ONLY",XrayOnline:true}
}

func (a *app) handleXKeenInitPlan(w http.ResponseWriter, _ *http.Request) {
 writeJSON(w,http.StatusOK,xkeenInitRepairInventory(a,xkeenInitDir()))
}

func (a *app) handleXKeenInitRepair(w http.ResponseWriter,r *http.Request) {
 if !sameOrigin(r) || !strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")),"application/json") {
  writeJSON(w,http.StatusForbidden,xkeenInitRepairResponse{State:"STOP",Reason:"invalid-origin-or-content-type",Mutation:"NONE",Rollback:"NOT_NEEDED"});return
 }
 var req struct{ Confirm string `json:"confirm"` }
 dec:=json.NewDecoder(http.MaxBytesReader(w,r.Body,256))
 dec.DisallowUnknownFields()
 if dec.Decode(&req)!=nil || req.Confirm!="repair-legacy-xkeen-init" {
  writeJSON(w,http.StatusBadRequest,xkeenInitRepairResponse{State:"STOP",Reason:"explicit-confirmation-required",Mutation:"NONE",Rollback:"NOT_NEEDED"});return
 }
 result:=xkeenInitRepairApply(r.Context(),a,xkeenInitDir())
 if result.State!="SUCCESS" {
  writeJSON(w,http.StatusConflict,result)
  return
 }
 v3AppendEvent("xray_runtime","success","XKeen init migrated to compatible S05xkeen, verified Xray running; old init retained in snapshot.")
 writeJSON(w,http.StatusOK,result)
}
