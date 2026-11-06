package main

import (
    "context"
    "errors"
    "net/http"
    "os"
    "path/filepath"
    "time"
)

type freeNetAutoApplyResponse struct{
    Success bool `json:"success"`
    Mutation string `json:"mutation"`
    Rollback string `json:"rollback"`
    CoreRestart bool `json:"core_restart"`
    Status string `json:"status"`
}

func freeNetAutoLockOwned(path string)bool{
    data,err:=os.ReadFile(filepath.Join(path,"autovpn-pool-owner"))
    return err==nil && string(data)=="freenet-auto-pool-v1\n"
}
func freeNetAutoReleaseLock(path string){
    if freeNetAutoLockOwned(path){_ =os.RemoveAll(path)}
}
func (a *app) freeNetAutoPoolRestorePending(ctx context.Context,source freeNetAutoPoolReceipt,
    beforeOut,beforeRouting []byte,restartRequired bool)error{
    if a==nil{return errors.New("AutoVPN rollback owner missing")}
    paths:=[]struct{path,beforeSHA,afterSHA string;old []byte}{
        {a.cfg.OutPath,source.BeforeOutSHA,source.AfterOutSHA,beforeOut},
        {filepath.Join(a.routingConfigDir(),"05_routing.json"),
            source.BeforeRoutingSHA,source.AfterRoutingSHA,beforeRouting},
    }
    touched:=false
    for _,p:=range paths{
        info,err:=os.Lstat(p.path)
        if err!=nil||!info.Mode().IsRegular(){return errors.New("AutoVPN live file unavailable for rollback")}
        data,err:=os.ReadFile(p.path)
        if err!=nil {return errors.New("AutoVPN live file unreadable for rollback")}
        digest:=sha256Hex(data)
        if digest!=p.beforeSHA && digest!=p.afterSHA{
            return errors.New("AutoVPN rollback detected external file mutation")
        }
        if digest==p.afterSHA {
            if err:=atomicWrite(p.path,p.old,info.Mode().Perm());err!=nil{
                return errors.New("AutoVPN restore write failed")
            }
            touched=true
        }
    }
    if touched && visionQUICSyncDir(a.routingConfigDir())!=nil{
        return errors.New("AutoVPN restore disk sync failed")
    }
    for _,p:=range paths{
        data,err:=os.ReadFile(p.path)
        if err!=nil||sha256Hex(data)!=p.beforeSHA{
            return errors.New("AutoVPN restored bytes unverified")
        }
    }
    if restartRequired{
        if err:=a.restartXrayCorePreservingFirewall(ctx);err!=nil{
            return errors.New("AutoVPN original runtime restart failed")
        }
    }
    if err:=freeNetAutoPoolClearPending();err!=nil{
        return errors.New("AutoVPN rollback checkpoint cleanup unverified")
    }
    return nil
}

func (a *app) handleFreeNetAutoPoolActivate(w http.ResponseWriter,r *http.Request){
    resp:=freeNetAutoApplyResponse{Mutation:"NONE",Rollback:"NOT_APPLIED",Status:"blocked"}
    var request freeNetAutoActivateRequest
    if !decodeFreeNetAutoJSON(w,r,&request){return}
    if !freeNetAutoLegacyProvisionEnabled{
        resp.Status="native-xkeen-sb-preferred-no-new-pool"
        writeJSON(w,http.StatusConflict,resp);return
    }
    if request.Acknowledge!="prepare-no-restart-autovpn-with-controlled-core-restart"||
       !validBestServerSelectionToken(request.PlanToken){
        resp.Status="confirmation-missing";writeJSON(w,http.StatusBadRequest,resp);return
    }
    if a.mutationBlockedBySelfUpdate(w){return}
    freeNetAutoPlans.Lock()
    plan:=freeNetAutoPlans.Current
    if plan!=nil && plan.Token==request.PlanToken{
        freeNetAutoPlans.Current=nil // a plan is consumed at most once
    }
    freeNetAutoPlans.Unlock()
    if plan==nil||plan.Token!=request.PlanToken||
       time.Since(plan.CreatedAt)<0||time.Since(plan.CreatedAt)>freeNetAutoPlanLifetime{
        resp.Status="stale-plan";writeJSON(w,http.StatusConflict,resp);return
    }
    select{case a.sem<-struct{}{}:defer func(){<-a.sem}()
    default:resp.Status="operation-busy";writeJSON(w,http.StatusConflict,resp);return}
    releaseHealth,err:=acquireAutomationHealthLock()
    if err!=nil{resp.Status="health-watch-busy";writeJSON(w,http.StatusConflict,resp);return}
    defer releaseHealth()
    releaseBest,err:=acquireAutomationBestLock()
    if err!=nil{resp.Status="best-server-busy";writeJSON(w,http.StatusConflict,resp);return}
    defer releaseBest()
    if !a.visionQUICReadyWhileHeld()||!freeNetAutoPoolPathAbsent(freeNetAutoPoolPending())||
        !freeNetAutoPoolPathAbsent(freeNetAutoPoolActive()){
        resp.Status="runtime-or-state-unknown";writeJSON(w,http.StatusConflict,resp);return
    }
    if err:=xrayCoreRestartPreflight();err!=nil{
        resp.Status="core-only-restart-unavailable";writeJSON(w,http.StatusConflict,resp);return
    }
    if os.Mkdir(a.cfg.LockPath,0700)!=nil{
        resp.Status="shared-writer-lock-busy";writeJSON(w,http.StatusConflict,resp);return
    }
    if os.WriteFile(filepath.Join(a.cfg.LockPath,"autovpn-pool-owner"),
        []byte("freenet-auto-pool-v1\n"),0600)!=nil{
        _ =os.Remove(a.cfg.LockPath)
        resp.Status="writer-ownership-unavailable";writeJSON(w,http.StatusConflict,resp);return
    }
    retainWriterLock:=false
    defer func(){
        if retainWriterLock{return}
        // A pending checkpoint is a persistent STOP, not a reason to
        // release the shared cross-process network writer lock.
        if freeNetAutoPoolPathAbsent(freeNetAutoPoolPending()){
            freeNetAutoReleaseLock(a.cfg.LockPath)
        }
    }()
    originalOut,originalRouting,endpoint,filter,err:=a.freeNetAutoPoolReadSource()
    if err!=nil || sha256Hex(originalOut)!=sha256Hex(plan.BeforeOut) ||
       sha256Hex(originalRouting)!=sha256Hex(plan.BeforeRouting) ||
       !endpointsEqual(endpoint,plan.OriginalEndpoint) ||
       filter!=plan.OriginalFilter {
        resp.Status="source-changed-before-apply";writeJSON(w,http.StatusConflict,resp);return
    }
    for id:=range plan.ProfileTags{
        if _,err:=a.loadBestServerSelectionCandidate(plan.SelectionToken,id,endpoint,filter);err!=nil{
            resp.Status="selection-changed-before-apply";writeJSON(w,http.StatusConflict,resp);return
        }
    }
    if !freeNetAutoLocalPortsFree(plan.Candidate.APIAddress,plan.Candidate.ProbeAddress){
        resp.Status="control-ports-occupied";writeJSON(w,http.StatusConflict,resp);return
    }
    validationCtx,cancel:=context.WithTimeout(context.Background(),30*time.Second)
    err=a.validateFreeNetAutoPoolCandidate(validationCtx,plan.Candidate)
    cancel()
    if err!=nil {resp.Status="candidate-revalidation-failed";writeJSON(w,http.StatusConflict,resp);return}
    // Durable exact two-file backup BEFORE either config fragment changes.
    receipt,err:=freeNetAutoPoolCreateCheckpoint(originalOut,originalRouting,plan.Candidate,plan.ProfileTags)
    if err!=nil {
        if !freeNetAutoPoolPathAbsent(freeNetAutoPoolPending()){
            setAutomationMutationBlocked(true)
            resp.Rollback="FAILED/UNKNOWN";resp.Mutation="STOP"
        }
        resp.Status="checkpoint-unavailable";writeJSON(w,http.StatusServiceUnavailable,resp);return
    }
    runCtx,cancelRun:=context.WithTimeout(context.Background(),110*time.Second)
    defer cancelRun()
    runtimeChanged:=false
    rollback:=func(){
        restoreCtx,cancelRestore:=context.WithTimeout(context.Background(),70*time.Second)
        defer cancelRestore()
        rbErr:=a.freeNetAutoPoolRestorePending(restoreCtx,receipt,originalOut,originalRouting,runtimeChanged)
        if rbErr!=nil{
            setAutomationMutationBlocked(true)
            retainWriterLock=true
            resp.Mutation="STOP";resp.Rollback="FAILED/UNKNOWN";resp.Status="rollback-unconfirmed"
        }else{
            resp.Mutation="ROLLED_BACK";resp.Rollback="SUCCESS"
        }
    }
    routingPath:=filepath.Join(a.routingConfigDir(),"05_routing.json")
    outInfo,err:=os.Lstat(a.cfg.OutPath)
    routingInfo,routeErr:=os.Lstat(routingPath)
    if err!=nil||routeErr!=nil||!outInfo.Mode().IsRegular()||!routingInfo.Mode().IsRegular(){
        resp.Status="live-source-lost";rollback();writeJSON(w,http.StatusServiceUnavailable,resp);return
    }
    if atomicWrite(a.cfg.OutPath,plan.Candidate.Outbounds,outInfo.Mode().Perm())!=nil ||
       atomicWrite(routingPath,plan.Candidate.Routing,routingInfo.Mode().Perm())!=nil ||
       visionQUICSyncDir(a.routingConfigDir())!=nil {
        resp.Status="candidate-disk-write-failed";rollback();writeJSON(w,http.StatusBadGateway,resp);return
    }
    // Recheck exact candidate bytes and full confdir before the one necessary
    // controlled restart. Neither WAN firewall nor ISP policies change.
    actualOut,err:=os.ReadFile(a.cfg.OutPath)
    actualRoute,routeErr:=os.ReadFile(routingPath)
    if err!=nil||routeErr!=nil||sha256Hex(actualOut)!=receipt.AfterOutSHA||
       sha256Hex(actualRoute)!=receipt.AfterRoutingSHA ||
       a.validateConfigStudioLive(runCtx)!=nil{
        resp.Status="post-write-revalidation-failed";rollback();writeJSON(w,http.StatusBadGateway,resp);return
    }
    runtimeChanged=true
    if a.restartXrayCorePreservingFirewall(runCtx)!=nil {
        resp.Status="pool-core-activation-failed";rollback();writeJSON(w,http.StatusBadGateway,resp);return
    }
    xrayBinary:=a.routingXrayBin()
    runAPI:=func(ctx context.Context,args ...string)([]byte,error){
        return runFreeNetAutoXrayCLI(ctx,xrayBinary,args...)
    }
    // Pin the already working original VPN immediately after activation.
    // Without this, leastPing could silently select a different country.
    allowed:=map[string]bool{"vless-reality":true}
    beforeIdentity:=a.freeNetAutoXrayIdentity()
    switchReceipt,switchErr:=freeNetAutoSwitchWith(runCtx,receipt.APIAddress,
        "vless-reality",allowed,runAPI,
        func(ctx context.Context)error{return freeNetAutoProbeXrayEgress(ctx,receipt.ProbeAddress)},
        a.freeNetAutoXrayIdentity)
    if beforeIdentity==""||switchErr!=nil||switchReceipt.Status=="rollback-unconfirmed"||
       a.freeNetAutoXrayIdentity()!=beforeIdentity {
        resp.Status="initial-live-egress-or-control-failed";rollback();writeJSON(w,http.StatusBadGateway,resp);return
    }
    if freeNetAutoPoolPromoteCheckpoint()!=nil {
        // Active receipt may have been published even if directory sync failed.
        // Do not erase it or blindly change runtime after ambiguous promotion.
        setAutomationMutationBlocked(true)
        retainWriterLock=true
        resp.Mutation="STOP";resp.Rollback="FAILED/UNKNOWN";resp.Status="pool-promotion-unverified"
        writeJSON(w,http.StatusServiceUnavailable,resp);return
    }
    resp.Success=true;resp.Mutation="ACTIVATED";resp.Rollback="NOT_NEEDED"
    resp.CoreRestart=true;resp.Status="pool-ready-no-restart-switching"
    writeJSON(w,http.StatusOK,resp)
}

// A startup or runtime reconciliation of an incomplete one-time setup must
// STOP further mutations until the exact before/candidate hashes can be read.
func (a *app) freeNetAutoPoolFailClosedStartup() {
    if !freeNetAutoPoolPathAbsent(freeNetAutoPoolPending()){
        setAutomationMutationBlocked(true)
    }
}
