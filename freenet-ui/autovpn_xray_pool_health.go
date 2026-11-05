package main

import (
    "context"
    "errors"
    "os"
    "path/filepath"
    "time"
)

const freeNetAutoPoolFailoverBudget=95*time.Second

// This is the live-pool-specific recovery path. It never reads the old
// 04_outbounds.json VLESS endpoint as proof of the ACTUAL selected balancer
// target, and it never calls the legacy provider restart/apply helper.
func freeNetAutoPoolHealthDecision(firstFailed,secondFailed,wanHealthy bool)string {
    if !firstFailed || !secondFailed{return "healthy"}
    if !wanHealthy{return "uncertain"}
    return "critical"
}
func (a *app) freeNetAutoPoolHealthWatch(parent context.Context,settings automationSettings) (automationHealthResult,error) {
    if !freeNetAutoPoolPathAbsent(freeNetAutoPoolPending()){
        setAutomationMutationBlocked(true)
        return automationHealthResult{State:"uncertain",Reason:"AutoVPN pool transition incomplete; STOP pending reconciliation"},nil
    }
    receipt,err:=a.freeNetAutoPoolLiveReceipt()
    if err!=nil || a.freeNetAutoXrayIdentity()=="" {
        return automationHealthResult{State:"uncertain",Reason:"AutoVPN pool source/runtime identity unverified; no mutation"},err
    }
    ctx,cancel:=context.WithTimeout(parent,freeNetAutoPoolFailoverBudget)
    defer cancel()
    firstErr:=freeNetAutoProbeXrayEgress(ctx,receipt.ProbeAddress)
    mediaRecovery:=false
    observedMediaTag:=""
    if firstErr==nil {
        // Probe the real selected balancer target, not the static original
        // outbound. A completed, repeatedly degraded two-origin transfer
        // may authorize a measured comparison, never an immediate switch.
        observation,due,mediaTag:=a.freeNetAutoPoolMediaHealth(ctx,receipt)
        observedMediaTag=mediaTag
        if !due{return observation,nil}
        if !probeAutomationWAN(ctx){
            return automationHealthResult{State:"uncertain",Reason:"Media slowdown detected but WAN health is unknown; current VPN retained."},nil
        }
        mediaRecovery=true
    }else{
        // Two independent confirmations and healthy WAN before outage recovery.
        secondErr:=freeNetAutoProbeXrayEgress(ctx,receipt.ProbeAddress)
        decision:=freeNetAutoPoolHealthDecision(firstErr!=nil,secondErr!=nil,probeAutomationWAN(ctx))
        if decision!="critical"{
            return automationHealthResult{State:decision,Reason:"Неоднозначная деградация VPN/WAN: исходный маршрут сохранён."},nil
        }
    }
    if settings.Mode!=automationModeBest || !settings.AutoApply {
        return automationHealthResult{State:automationHealthCritical,Reason:"VPN дважды не прошёл HTTPS; автоматическая смена узла выключена."},nil
    }
    if automationMutationBlockedState() || a.updateLockHeld() {
        return automationHealthResult{State:"uncertain",Reason:"Pool recovery blocked by update/unknown prior rollback."},nil
    }
    if _,active,err:=automationEmergencyCooldownDue(a,time.Now().UTC());err!=nil || active {
        return automationHealthResult{State:automationHealthCritical,Reason:"VPN не отвечает; действует защищённая пауза повторного failover."},err
    }
    releaseHealth,err:=acquireAutomationHealthLock()
    if err!=nil{return automationHealthResult{State:"busy",Reason:"AUTO health operation busy."},nil}
    defer releaseHealth()
    releaseBest,err:=acquireAutomationBestLock()
    if err!=nil{return automationHealthResult{State:"busy",Reason:"AUTO Best Server operation busy."},nil}
    defer releaseBest()
    select{case a.sem<-struct{}{}:defer func(){<-a.sem}()
    default:return automationHealthResult{State:"busy",Reason:"Another FreeNet mutation is active."},nil}
    if !a.visionQUICReadyWhileHeld() || !freeNetAutoPoolPathAbsent(freeNetAutoPoolPending()) {
        return automationHealthResult{State:"uncertain",Reason:"Pool state changed before failover; no mutation."},nil
    }
    liveReceipt,err:=a.freeNetAutoPoolLiveReceipt()
    if err!=nil || liveReceipt.AfterRoutingSHA!=receipt.AfterRoutingSHA ||
        liveReceipt.AfterOutSHA!=receipt.AfterOutSHA {
        return automationHealthResult{State:"uncertain",Reason:"AutoVPN pool revision changed; no failover."},nil
    }
    runner:=func(c context.Context,args ...string)([]byte,error) {
        return runFreeNetAutoXrayCLI(c,a.routingXrayBin(),args...)
    }
    currentOverride,selected,err:=freeNetAutoReadBalancer(ctx,runner,receipt.APIAddress)
    if err!=nil{return automationHealthResult{State:"uncertain",Reason:"Xray runtime balancer unavailable."},err}
    liveTag:=currentOverride
    if liveTag==""{liveTag=selected}
    if mediaRecovery && liveTag!=observedMediaTag {
        return automationHealthResult{State:"uncertain",Reason:"Профиль пула изменился после media QoE измерения; переключение отменено."},nil
    }
    allowed:=map[string]bool{"vless-reality":true}
    for _,tag:=range receipt.ProfileTags{allowed[tag]=true}
    if !allowed[liveTag] || !freeNetAutoTagAllowed(liveTag) {
        return automationHealthResult{State:"uncertain",Reason:"Xray runtime target is outside the approved pool."},nil
    }
    // Fresh catalog + isolated named HTTPS proof for at most two stored
    // alternatives. Never expand to a subscription-wide restart-driven scan.
    internal,_,_,source,err:=a.discoverBestServerCandidatesWithSource(ctx)
    if err!=nil || !source.Fresh {
        return automationHealthResult{State:automationHealthCritical,Reason:"VPN failed; fresh source for safe replacement unavailable."},nil
    }
    currentCountry:=a.status().CountryCode
    target:=""
    // Never choose "vless-reality" solely because it is the original:
    // it is not a proven fallback. The original remains selectable manually
    // with guarded live acceptance and rollback.
    for _,candidate:=range internal {
        tag,found:=receipt.ProfileTags[candidate.Profile.ID]
        if !found || tag==liveTag || !automationCountryAllowed(settings,currentCountry,candidate.Profile.CountryCode){
            continue
        }
        expected,e:=buildBestServerProbeOutbound(candidate.Raw,candidate.Profile)
        if e!=nil{continue}
        expected["tag"]=tag
        bytes,readErr:=os.ReadFile(a.cfg.OutPath)
        if readErr!=nil||!freeNetAutoOutboundMatches(bytes,tag,expected){continue}
        if !a.probeBestServerApplicationPreflight(ctx,candidate).OK{continue}
        if mediaRecovery {
            // Healthy 204 alone is not a better media path. Demand
            // corroborated full-body transfer on both independent sources.
            quality:=a.probeAutomationQualityCandidate(ctx,candidate)
            if quality.State!=automationHealthHealthy || !quality.MediaComplete ||
                quality.MediaDegraded || quality.QualityDegraded || !quality.ServiceComplete {
                continue
            }
        }
        target=tag
        break
    }
    // With only one standby, the original VLESS can be the sole recovery
    // candidate. It must pass its OWN isolated HTTPS (and, for a media
    // episode, two-origin body transfer) before an automatic override.
    // Never assume that "original" means "healthy".
    if target=="" && liveTag!="vless-reality" {
        var original automationHealthProbe
        if mediaRecovery {
            original=a.probeAutomationCurrentVPN(ctx)
        }else{
            original=a.probeAutomationCurrentVPNApplicationOnly(ctx)
        }
        if freeNetAutoPoolOriginalApproved(original,mediaRecovery) {
            target="vless-reality"
        }
    }
    if target=="" {
        _,_=recordAutomationEmergencyFailure(a,time.Now().UTC())
        return automationHealthResult{State:automationHealthCritical,Reason:"VPN degraded: no approved and HTTPS-verified standby available."},nil
    }
    // Shared provider/update writer. The existing helper knows this lock.
    if os.Mkdir(a.cfg.LockPath,0700)!=nil{
        return automationHealthResult{State:"busy",Reason:"Network writer is busy; no switch."},nil
    }
    if os.WriteFile(filepath.Join(a.cfg.LockPath,"autovpn-pool-owner"),
        []byte("freenet-auto-pool-v1\n"),0600)!=nil{
        setAutomationMutationBlocked(true)
        return automationHealthResult{State:"uncertain",Reason:"AutoVPN writer ownership unconfirmed; STOP."},errors.New("AutoVPN writer ownership unknown")
    }
    retainLock:=false
    defer func(){if !retainLock{freeNetAutoReleaseLock(a.cfg.LockPath)}}()
    if _,err:=a.freeNetAutoPoolLiveReceipt();err!=nil{
        return automationHealthResult{State:"uncertain",Reason:"Pool changed under lock; no switch."},nil
    }
    before:=a.freeNetAutoXrayIdentity()
    result,switchErr:=freeNetAutoSwitchWith(ctx,receipt.APIAddress,target,allowed,runner,
        func(c context.Context)error{
            return freeNetAutoPoolPostApplyAccept(c,mediaRecovery,
                func(checkCtx context.Context)error{
                    return freeNetAutoProbeXrayEgress(checkCtx,receipt.ProbeAddress)
                },
                func(checkCtx context.Context)automationMediaEvidence{
                    return freeNetAutoPoolProbeMedia(checkCtx,receipt.ProbeAddress)
                })
        },
        a.freeNetAutoXrayIdentity)
    if result.Mutation=="STOP" {
        retainLock=true
        setAutomationMutationBlocked(true)
        return automationHealthResult{State:"uncertain",Reason:"AutoVPN failover rollback unverified; STOP."},switchErr
    }
    if switchErr!=nil || a.freeNetAutoXrayIdentity()!=before {
        _,_=recordAutomationEmergencyFailure(a,time.Now().UTC())
        return automationHealthResult{State:automationHealthCritical,Reason:"Replacement rejected; original Xray override restored."},switchErr
    }
    _=clearAutomationEmergencyCooldown()
    // Even a non-media switch starts the media-only quiet window. Never
    // chase an apparent second slow CDN sample immediately after recovery.
    _=v3WriteState(map[string]string{"POOL_MEDIA_LAST_SWITCH":time.Now().UTC().Format(time.RFC3339)})
    if mediaRecovery {
        writeAutomationStateV2("switched","AutoVPN выбрал подтверждённый качественный резерв без перезапуска.", "yes",true)
        appendAutomationHistoryV2("success","Xray media QoE pool switch: two-origin transfer admission and HTTPS acceptance.")
        return automationHealthResult{State:automationHealthHealthy,Reason:"AUTO VPN переключил измеренно качественный резерв после повторных медленных media-сегментов; реальный playback клиента не проверялся.",Mutated:true},nil
    }
    writeAutomationStateV2("switched","AutoVPN переключил подтверждённый Xray pool target без перезапуска.", "yes",true)
    appendAutomationHistoryV2("success","Xray live failover verified via local HTTPS without core restart.")
    return automationHealthResult{State:automationHealthHealthy,Reason:"AUTO VPN восстановил HTTPS через резервный сервер без перезапуска Xray.",Mutated:true},nil
}
