package main

import (
    "context"
    "os/exec"
    "time"
)

// A backup is never approved solely by its tag or its former position in
// the pool. Original VLESS gets the same independent evidence as any other
// candidate before a live automatic override.
func freeNetAutoPoolOriginalApproved(probe automationHealthProbe, mediaRecovery bool) bool {
    if probe.State!=automationHealthHealthy || probe.CurrentIdentity=="" {
        return false
    }
    if !mediaRecovery {return true}
    return probe.MediaComplete && !probe.MediaDegraded &&
        !probe.QualityDegraded && probe.ServiceComplete
}

// Media measurements are router-side throughput surrogates sent through the
// currently selected live balancer. They do not prove Instagram playback.
type freeNetAutoPoolMediaPlan struct {
    Updates map[string]string
    Due bool
    Status string
}

func freeNetAutoPoolPlanMedia(state map[string]string, now time.Time, identity string, media automationMediaEvidence) freeNetAutoPoolMediaPlan {
    plan:=freeNetAutoPoolMediaPlan{Status:"media-unverified"}
    if !validAutomationQualityIdentity(identity) || now.IsZero() {
        return plan
    }
    // A manual or emergency switch also starts the quiet period. Old
    // observations must not trigger a media-only ping-pong at its expiry.
    if automationMediaSwitchSettling(now,state["POOL_MEDIA_LAST_SWITCH"]) {
        plan.Updates=map[string]string{}
        clearAutomationMediaEvidence(plan.Updates)
        plan.Status="media-settling"
        return plan
    }
    probe:=classifyAutomationMediaQuality(automationHealthProbe{
        State:automationHealthHealthy,CurrentIdentity:identity,
    },media)
    if !probe.MediaComplete {
        return plan // partial/unknown media is never a switch trigger
    }
    plan.Updates,plan.Due=automationMediaQualityOptimizationPlan(state,now,probe)
    if !media.Degraded {
        plan.Status="media-healthy"
    } else if plan.Due {
        plan.Status="media-confirmed"
    } else {
        plan.Status="media-observing"
    }
    return plan
}

// Separate read-only observation: two origins, no DIRECT retry or Xray
// restart. Persisting the strike/cooldown state is fail-closed.
func (a *app) freeNetAutoPoolMediaHealth(
    ctx context.Context, receipt freeNetAutoPoolReceipt,
) (automationHealthResult,bool,string) {
    good:=automationHealthResult{State:automationHealthHealthy,
        Reason:"HTTPS через действующий Xray pool подтверждён; media QoE в этом цикле не измерялось."}
    sampledCtx:=automationScheduledWatchdogMediaContext(ctx,time.Now().UTC())
    if !automationMediaProbeAllowed(sampledCtx) {
        return good,false,""
    }
    runner:=func(c context.Context,args ...string)([]byte,error){
        return runFreeNetAutoXrayCLI(c,a.routingXrayBin(),args...)
    }
    override,selected,err:=freeNetAutoReadBalancer(sampledCtx,runner,receipt.APIAddress)
    if err!=nil{return automationHealthResult{State:"uncertain",Reason:"Нельзя определить живой pool target до медиапроверки."},false,""}
    tag:=override
    if tag==""{tag=selected}
    allowed:=tag=="vless-reality"
    for _,candidate:=range receipt.ProfileTags{if candidate==tag{allowed=true}}
    if !allowed || !freeNetAutoTagAllowed(tag) {
        return automationHealthResult{State:"uncertain",Reason:"Медиапроверка остановлена: выбранный target вне подтверждённого пула."},false,""
    }
    curlPath,err:=exec.LookPath("curl")
    if err!=nil{return automationHealthResult{State:"uncertain",Reason:"Медиапроверка недоступна: curl не найден."},false,""}
    before:=a.freeNetAutoXrayIdentity()
    if before==""{return automationHealthResult{State:"uncertain",Reason:"Xray runtime identity unknown."},false,""}
    media:=probeAutomationMediaSegments(sampledCtx,curlPath,receipt.ProbeAddress)
    afterOverride,afterSelected,readErr:=freeNetAutoReadBalancer(sampledCtx,runner,receipt.APIAddress)
    afterTag:=afterOverride
    if afterTag==""{afterTag=afterSelected}
    if sampledCtx.Err()!=nil || readErr!=nil || tag!=afterTag ||
        before!=a.freeNetAutoXrayIdentity() {
        return automationHealthResult{State:"uncertain",Reason:"Xray pool изменился во время медиапроверки; измерение отклонено."},false,""
    }
    identity:=sha256Hex([]byte("freenet-pool-media-v1:"+receipt.AfterOutSHA+":"+receipt.AfterRoutingSHA+":"+tag))
    now:=time.Now().UTC()
    var plan freeNetAutoPoolMediaPlan
    stateErr:=v3UpdateState(func(state map[string]string)map[string]string{
        plan=freeNetAutoPoolPlanMedia(state,now,identity,media)
        return plan.Updates
    })
    if stateErr!=nil{
        return automationHealthResult{State:"uncertain",Reason:"Нельзя сохранить подтверждения media QoE; переключение запрещено."},false,""
    }
    switch plan.Status {
    case "media-healthy":
        return automationHealthResult{State:automationHealthHealthy,
            Reason:"HTTPS и два независимых media-source body transfer через выбранный Xray pool доступны; playback клиента не проверен."},false,tag
    case "media-settling":
        return automationHealthResult{State:"uncertain",
            Reason:"Media QoE наблюдается, но действует безопасная пауза после предыдущего переключения."},false,tag
    case "media-observing":
        return automationHealthResult{State:"uncertain",
            Reason:"Media QoE медленный на двух источниках; для переключения нужны повторные устойчивые наблюдения."},false,tag
    case "media-confirmed":
        return automationHealthResult{State:"uncertain",
            Reason:"Media QoE подтверждён повторными медленными измерениями; требуется проверка лучшего резервного узла."},true,tag
    default:
        return good,false,tag
    }
}
