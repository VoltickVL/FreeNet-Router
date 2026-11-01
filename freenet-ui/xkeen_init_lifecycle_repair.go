package main

// Targeted recovery of the XKeen 1.x -> 2.x init.d migration.
// Never reinstall Xray, XKeen, subscriptions, DNS, routes or firewall rules.
import (
 "context"
 "encoding/json"
 "errors"
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
 Phase string `json:"phase,omitempty"`
}

var (
 xkeenInitRepairRun = func(ctx context.Context, path string) error {
  _, err := runCommand(ctx, path, "-ri")
  return err
 }
 xkeenInitRepairStart = func(ctx context.Context, a *app) error {
  if vpnTransactionPending() { return a.startXrayAfterInitRecovery(ctx) }
  return a.startXrayControlled(ctx)
 }
)

func xkeenInitDir() string { return "/opt/etc/init.d" }

const xkeenLegacyNetfilterHook = "/opt/etc/ndm/netfilter.d/proxy.sh"
const xkeenRepairHookSnapshotName = "netfilter-proxy.before"

// The existing VPN writer is UNKNOWN, but the runtime can be established
// without mutation: all three files match the durable snapshot, no live
// Xray, probe, zombie or unknown process exists, and no owner lock remains.
// Only this exact recovery shape may cross the general pending fence.
func xkeenInitPendingRepairProof(d vpnTransactionDoctorResponse, inv xrayProcessInventoryResponse, hook string) bool {
 return xrayPendingReadOnlyEligible(d, inv) &&
  hook == "legacy-removed-module-reference"
}

func (a *app) xkeenInitPendingRepairAdmission() bool {
 if a == nil || !vpnTransactionPending() || !automationMutationBlockedState() {
  return false
 }
 binary, _, err := a.resolvedXrayCoreBinary()
 if err != nil { return false }
 configDir, err := a.xrayCoreLiveConfigDir()
 if err != nil { return false }
 inv := scanXrayProcessInventory("/proc", configDir, binary)
 return xkeenInitPendingRepairProof(a.vpnTransactionDoctor(), inv,
  xrayDiagnosticHookState(xkeenLegacyNetfilterHook))
}

// A dedicated start path, available only inside an already durably journaled
// init repair. Generic /api/xray/service and every other mutation continue
// to refuse PENDING/UNKNOWN. This never clears the VPN checkpoint.
func (a *app) startXrayAfterInitRecovery(ctx context.Context) error {
 receipt, err := xkeenInitRepairReceipt(xkeenInitDir())
 if err != nil || receipt.State != "RUNNING" || receipt.Phase != "START" ||
  a == nil || a.sem == nil || len(a.sem) == 0 ||
  !a.xkeenInitPendingRepairAdmission() {
  return errors.New("pending-init-repair-admission-unverified")
 }
 if reason, needs := xkeenInitRepairNeed(xkeenInitDir()); needs || reason != "compatible" {
  return errors.New("repaired-init-not-compatible")
 }
 if a.xrayRuntimeDesiredState() != xrayRuntimeDesiredRunning {
  return errors.New("xray-intentionally-stopped")
 }
 if err := a.validateConfigStudioLive(ctx); err != nil {
  return errors.New("xray-live-config-precheck-failed")
 }
 if _, err := runCommand(ctx, a.cfg.XKeenPath, "-start"); err != nil {
  return errors.New("native-xkeen-start-failed")
 }
 if !waitForXrayOnline(ctx) {
  return errors.New("xray-start-not-confirmed")
 }
 if err := a.validateConfigStudioLive(ctx); err != nil {
  return errors.New("xray-post-start-config-not-confirmed")
 }
 if xrayDiagnosticHookState(xkeenLegacyNetfilterHook) != "modern-generated-hook" {
  return errors.New("netfilter-hook-not-migrated")
 }
 binary, _, err := a.resolvedXrayCoreBinary()
 if err != nil { return errors.New("xray-post-start-identity-unverified") }
 dir, err := a.xrayCoreLiveConfigDir()
 if err != nil { return errors.New("xray-post-start-config-identity-unverified") }
 inventory := scanXrayProcessInventory("/proc", dir, binary)
 if !inventory.Success || inventory.ProductionCount != 1 ||
  inventory.UnknownCount != 0 || inventory.ProbeCount != 0 || inventory.ZombieCount != 0 {
  return errors.New("xray-post-start-process-unverified")
 }
 return nil
}

// The native XKeen start rewrites the netfilter hook. A verified, private
// backup of its exact old bytes is mandatory before any init regeneration.
func xkeenInitRepairHookBackup(path, snapshot string) error {
 if xrayDiagnosticHookState(path) != "legacy-removed-module-reference" {
  return errors.New("netfilter-hook-not-legacy")
 }
 data, ok := xkeenInitReadBounded(path)
 if !ok { return errors.New("netfilter-hook-unreadable") }
 file, err := os.OpenFile(filepath.Join(snapshot,xkeenRepairHookSnapshotName),os.O_CREATE|os.O_EXCL|os.O_WRONLY,0600)
 if err != nil { return errors.New("netfilter-snapshot-unavailable") }
 _, writeErr := file.Write([]byte(data))
 syncErr := file.Sync()
 closeErr := file.Close()
 if writeErr != nil || syncErr != nil || closeErr != nil {
  return errors.New("netfilter-snapshot-unconfirmed")
 }
 return nil
}

func xkeenInitRepairHookRollback(path, snapshot string) error {
 saved, ok := xkeenInitReadBounded(filepath.Join(snapshot,xkeenRepairHookSnapshotName))
 if !ok || !strings.Contains(saved,"03_info_cpu.sh") {
  return errors.New("netfilter-snapshot-unverified")
 }
 if !xkeenInitSafeFile(path,true) {
  return errors.New("netfilter-live-file-untrusted")
 }
 file, err := os.CreateTemp(filepath.Dir(path),".freenet-netfilter-rollback-*")
 if err != nil { return errors.New("netfilter-restore-workspace-unavailable") }
 tmp := file.Name()
 defer os.Remove(tmp)
 _, writeErr := file.Write([]byte(saved))
 modeErr := file.Chmod(0755)
 syncErr := file.Sync()
 closeErr := file.Close()
 if writeErr != nil || modeErr != nil || syncErr != nil || closeErr != nil {
  return errors.New("netfilter-restore-write-unconfirmed")
 }
 if err := os.Rename(tmp,path); err != nil { return errors.New("netfilter-restore-rename-failed") }
 restored, ok := xkeenInitReadBounded(path)
 if !ok || restored != saved { return errors.New("netfilter-restore-postcheck-failed") }
 return nil
}

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

func xkeenInitRepairMutationBlocked() bool {
 state,err:=xkeenInitRepairReceipt(xkeenInitDir())
 return err!=nil || state.State=="RUNNING" ||
  (state.State=="STOP" && (state.Rollback=="UNKNOWN" || state.Rollback=="FAILED"))
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
 return xkeenInitRepairInventoryScoped(a,root,false)
}

func xkeenInitRepairInventoryScoped(a *app, root string, slotReserved bool) xkeenInitRepairResponse {
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
 if !xkeenUpdateEnoughSpace(root) {result.Reason="snapshot-space-unavailable";return result}
 if a.xrayRuntimeDesiredState()!=xrayRuntimeDesiredRunning {
  result.Reason="xray-intentionally-stopped";return result
 }
 // Do not bypass the original transaction fence generically. Only the
 // exact, read-only established STOPPED+MATCHED+legacy-hook condition can
 // make this one targeted native-init repair eligible.
 if vpnTransactionPending() || automationMutationBlockedState() {
  if !a.xkeenInitPendingRepairAdmission() {
   result.Reason="pending-vpn-recovery-unverified";return result
  }
 }
 held,_:=a.updateLockStatus()
 operation := xrayCoreOperationSnapshot().State
 if held || a.selfUpdateStateAdmission()!=nil || operation=="RUNNING" || operation=="STOP" ||
  (!slotReserved && a.sem != nil && len(a.sem)>0) {
  result.Reason="another-operation-or-recovery-busy";return result
 }
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
 plan:=xkeenInitRepairInventoryScoped(a,root,true)
 if !plan.Ready {return plan}
 snapshot,err:=os.MkdirTemp(root,".freenet-xkeen-init-snapshot-*")
 if err!=nil {failed.Reason="snapshot-unavailable";return failed}
 if err=os.Chmod(snapshot,0700);err!=nil {failed.Reason="snapshot-permissions-unavailable";return failed}
 if err=xkeenInitRepairBackup(root,snapshot);err!=nil {
  failed.Reason="snapshot-unverified"
  return failed
 }
 // This repair is intentionally restricted to observed legacy netfilter.
 // Back up the hook before the native XKeen action can regenerate it.
 if err=xkeenInitRepairHookBackup(xkeenLegacyNetfilterHook,snapshot);err!=nil {
  failed.Reason="netfilter-snapshot-unverified"
  return failed
 }
 if vpnTransactionPending() && !a.xkeenInitPendingRepairAdmission() {
  failed.Reason="pending-vpn-recovery-drift"
  return failed
 }
 disk:=xkeenInitRepairDisk{State:"RUNNING",Reason:"repair-in-progress",Rollback:"PENDING",Snapshot:snapshot,Phase:"INIT_REBUILD"}
 if xkeenInitRepairWrite(root,disk)!=nil {
  failed.Reason="repair-receipt-not-durable";return failed
 }
 nativeAttempted:=false
 startAttempted:=false
 fail:=func(reason string) xkeenInitRepairResponse {
  disk.State="STOP"
  disk.Reason=reason
  disk.Rollback="UNKNOWN"
  if !xrayServiceProcessRunning("xray") {
   initErr:=xkeenInitRepairRollback(root,snapshot)
   hookErr:=xkeenInitRepairHookRollback(xkeenLegacyNetfilterHook,snapshot)
   if initErr!=nil || hookErr!=nil {disk.Rollback="FAILED"} else if !nativeAttempted && !startAttempted {
    disk.Rollback="SUCCESS"
   }
   // A native -ri can migrate port preferences; -start can change firewall
   // rules. Restored init/hook files do NOT prove full network rollback.
   // If either native action was attempted, UNKNOWN = STOP.
  }
  if xkeenInitRepairWrite(root,disk)!=nil {disk.Rollback="UNKNOWN";disk.Reason="repair-receipt-not-durable"}
  return xkeenInitRepairResponse{State:"STOP",Reason:disk.Reason,Rollback:disk.Rollback,Mutation:"INIT_ATTEMPTED",XrayOnline:xrayServiceProcessRunning("xray")}
 }
 // After a durable RUNNING receipt the worker must finish even if the browser disconnects.
 ctx,cancel:=context.WithTimeout(context.Background(),100*time.Second)
 defer cancel()
 nativeAttempted=true
 if xkeenInitRepairRun(ctx,a.cfg.XKeenPath)!=nil {return fail("native-init-rebuild-failed")}
 if reason,need:=xkeenInitRepairNeed(root);need||reason!="compatible" {return fail("init-migration-postcheck-failed")}
 if vpnTransactionPending() && !a.xkeenInitPendingRepairAdmission() {
  return fail("pending-vpn-recovery-drift")
 }
 disk.Phase="START"
 if xkeenInitRepairWrite(root,disk)!=nil {return fail("start-receipt-not-durable")}
 startAttempted=true
 if err:=xkeenInitRepairStart(ctx,a);err!=nil {return fail("xray-start-postcheck-failed")}
 if !xrayServiceProcessRunning("xray") {return fail("xray-not-running-after-repair")}
 // This fixes native init only; the original VPN failure checkpoint is
 // neither deleted nor silently accepted as restored VPN connectivity.
 if vpnTransactionPending() {
  d:=a.vpnTransactionDoctor()
  if d.Checkpoint!="pending" || !d.FilesRestored || d.Outbound!="matched" ||
   d.Profile!="matched" || d.Filter!="matched" || d.XrayCore!="running" ||
   d.LockState!="absent" || !d.MutationBlocked {
   return fail("original-vpn-checkpoint-postcheck-failed")
  }
 }
 disk.State="SUCCESS"
 disk.Reason="xkeen-init-compatible-xray-running"
 disk.Rollback="NOT_NEEDED"
 disk.Phase="DONE"
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
