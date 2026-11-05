package main

import (
    "context"
    "errors"
    "net/http"
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
    if firstErr==nil {
        return automationHealthResult{State:automationHealthHealthy,Reason:"Действующий Xray balancer подтвердил HTTPS через локальный VPN SOCKS."},nil
    }
    // Two independent confirmations and healthy WAN before any failover.
    secondErr:=freeNetAutoProbeXrayEgress(ctx,receipt.ProbeAddress)
    decision:=freeNetAutoPoolHealthDecision(firstErr!=nil,secondErr!=nil,probeAutomationWAN(ctx))
    if decision!="critical"{
        return automationHealthResult{State:decision,Reason:"Неоднозначная деградация VPN/WAN: исходный маршрут сохранён."},nil
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
    if liveTag!="vless-reality"{target="vless-reality"} // original VPN is first safe fallback
    if target=="" {
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
            target=tag
            break
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
        func(c context.Context)error{return freeNetAutoProbeXrayEgress(c,receipt.ProbeAddress)},
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
    writeAutomationStateV2("switched","AutoVPN переключил подтверждённый Xray pool target без перезапуска.", "yes",true)
    appendAutomationHistoryV2("success","Xray live failover verified via local HTTPS without core restart.")
    return automationHealthResult{State:automationHealthHealthy,Reason:"AUTO VPN восстановил HTTPS через резервный сервер без перезапуска Xray.",Mutated:true},nil
}
