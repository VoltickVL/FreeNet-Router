package main

import (
    "context"
    "net/http"
    "os"
    "path/filepath"
    "time"
)

type freeNetAutoDisableRequest struct{
    Acknowledge string `json:"acknowledge"`
}
func (a *app) handleFreeNetAutoPoolDisable(w http.ResponseWriter,r *http.Request){
    result:=freeNetAutoApplyResponse{Mutation:"NONE",Rollback:"NOT_APPLIED",Status:"blocked"}
    var req freeNetAutoDisableRequest
    if !decodeFreeNetAutoJSON(w,r,&req){return}
    if req.Acknowledge!="restore-original-vpn-config-with-controlled-core-restart"{
        result.Status="confirmation-missing";writeJSON(w,http.StatusBadRequest,result);return
    }
    if a.mutationBlockedBySelfUpdate(w){return}
    select{case a.sem<-struct{}{}:defer func(){<-a.sem}()
    default:result.Status="operation-busy";writeJSON(w,http.StatusConflict,result);return}
    releaseHealth,err:=acquireAutomationHealthLock()
    if err!=nil{result.Status="health-busy";writeJSON(w,http.StatusConflict,result);return}
    defer releaseHealth()
    releaseBest,err:=acquireAutomationBestLock()
    if err!=nil{result.Status="best-server-busy";writeJSON(w,http.StatusConflict,result);return}
    defer releaseBest()
    if !a.visionQUICReadyWhileHeld()||a.freeNetAutoXrayIdentity()==""{
        result.Status="runtime-owner-unverified";writeJSON(w,http.StatusConflict,result);return
    }
    receipt,beforeOut,beforeRouting,err:=freeNetAutoPoolReadCheckpoint(freeNetAutoPoolActive())
    if err!=nil{result.Status="active-backup-invalid";writeJSON(w,http.StatusConflict,result);return}
    currentOut,currentRouting,_,_,err:=a.freeNetAutoPoolReadSource()
    if err!=nil || sha256Hex(currentOut)!=receipt.AfterOutSHA ||
       sha256Hex(currentRouting)!=receipt.AfterRoutingSHA {
        result.Status="active-config-revision-mismatch";writeJSON(w,http.StatusConflict,result);return
    }
    if err:=xrayCoreRestartPreflight();err!=nil{
        result.Status="core-only-restart-unavailable";writeJSON(w,http.StatusConflict,result);return
    }
    if os.Mkdir(a.cfg.LockPath,0700)!=nil{
        result.Status="writer-busy";writeJSON(w,http.StatusConflict,result);return
    }
    if os.WriteFile(filepath.Join(a.cfg.LockPath,"autovpn-pool-owner"),
      []byte("freenet-auto-pool-v1\n"),0600)!=nil{
       setAutomationMutationBlocked(true)
       result.Mutation="STOP";result.Rollback="FAILED/UNKNOWN";result.Status="writer-ownership-uncertain"
       writeJSON(w,http.StatusServiceUnavailable,result);return
    }
    retainLock:=false
    defer func(){if !retainLock{freeNetAutoReleaseLock(a.cfg.LockPath)}}()
    if _,err:=a.freeNetAutoPoolLiveReceipt();err!=nil{
        result.Status="state-changed-under-lock";writeJSON(w,http.StatusConflict,result);return
    }
    outInfo,outErr:=os.Lstat(a.cfg.OutPath)
    routePath:=filepath.Join(a.routingConfigDir(),"05_routing.json")
    routeInfo,routeErr:=os.Lstat(routePath)
    if outErr!=nil||routeErr!=nil||!outInfo.Mode().IsRegular()||!routeInfo.Mode().IsRegular(){
        result.Status="source-unavailable";writeJSON(w,http.StatusConflict,result);return
    }
    ctx,cancel:=context.WithTimeout(context.Background(),110*time.Second)
    defer cancel()
    runtimeChanged:=false
    rollback:=func() {
        restoreCtx,restoreCancel:=context.WithTimeout(context.Background(),70*time.Second)
        defer restoreCancel()
        restored:=atomicWrite(a.cfg.OutPath,currentOut,outInfo.Mode().Perm())==nil &&
                  atomicWrite(routePath,currentRouting,routeInfo.Mode().Perm())==nil &&
                  visionQUICSyncDir(a.routingConfigDir())==nil
        if restored && runtimeChanged{
            restored=a.restartXrayCorePreservingFirewall(restoreCtx)==nil
        }
        actualOut,_:=os.ReadFile(a.cfg.OutPath)
        actualRouting,_:=os.ReadFile(routePath)
        if !restored || sha256Hex(actualOut)!=receipt.AfterOutSHA ||
            sha256Hex(actualRouting)!=receipt.AfterRoutingSHA {
            retainLock=true
            setAutomationMutationBlocked(true)
            result.Mutation="STOP";result.Rollback="FAILED/UNKNOWN";result.Status="restore-original-pool-failed"
        }else{
            result.Mutation="ROLLED_BACK";result.Rollback="SUCCESS"
        }
    }
    if atomicWrite(a.cfg.OutPath,beforeOut,outInfo.Mode().Perm())!=nil ||
       atomicWrite(routePath,beforeRouting,routeInfo.Mode().Perm())!=nil ||
       visionQUICSyncDir(a.routingConfigDir())!=nil {
        result.Status="original-file-restore-failed";rollback()
        writeJSON(w,http.StatusBadGateway,result);return
    }
    runtimeChanged=true
    if a.restartXrayCorePreservingFirewall(ctx)!=nil{
        result.Status="original-runtime-restart-failed";rollback()
        writeJSON(w,http.StatusBadGateway,result);return
    }
    // If the old Xray bytes are valid and the original process is healthy,
    // destroy the old private checkpoint only after the post-apply check.
    actualOut,readErr:=os.ReadFile(a.cfg.OutPath)
    actualRouting,routeReadErr:=os.ReadFile(routePath)
    if readErr!=nil||routeReadErr!=nil ||
        sha256Hex(actualOut)!=receipt.BeforeOutSHA ||
        sha256Hex(actualRouting)!=receipt.BeforeRoutingSHA ||
        a.validateConfigStudioLive(ctx)!=nil{
        result.Status="restored-runtime-postcheck-failed";rollback()
        writeJSON(w,http.StatusBadGateway,result);return
    }
    if os.RemoveAll(freeNetAutoPoolActive())!=nil ||
        visionQUICSyncDir(freeNetAutoPoolStateDir())!=nil{
        retainLock=true
        setAutomationMutationBlocked(true)
        result.Mutation="STOP";result.Rollback="FAILED/UNKNOWN";result.Status="active-pool-removal-unknown"
        writeJSON(w,http.StatusServiceUnavailable,result);return
    }
    result.Success=true;result.Mutation="DEACTIVATED";result.Rollback="NOT_NEEDED"
    result.CoreRestart=true;result.Status="original-config-and-runtime-restored"
    writeJSON(w,http.StatusOK,result)
}
