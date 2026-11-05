package main

import (
    "bytes"
    "context"
    "errors"
    "encoding/json"
    "net/http"
    "os"
    "path/filepath"
    "sort"
    "time"
)

type freeNetAutoStatusResponse struct{
    Success bool `json:"success"`
    Active bool `json:"active"`
    Mutation string `json:"mutation"`
    Status string `json:"status"`
    NodeCount int `json:"node_count,omitempty"`
    Nodes []map[string]string `json:"nodes,omitempty"`
    CurrentTag string `json:"current_tag,omitempty"`
    APIReachable bool `json:"api_reachable"`
    CoreIdentityVerified bool `json:"core_identity_verified"`
}
type freeNetAutoSwitchRequest struct {
    ProfileID string `json:"profile_id"`
}
type freeNetAutoRPCResponse struct {
    Success bool `json:"success"`
    Mutation string `json:"mutation"`
    Rollback string `json:"rollback"`
    Status string `json:"status"`
    CurrentTag string `json:"current_tag,omitempty"`
}

func (a *app) freeNetAutoPoolLiveReceipt()(freeNetAutoPoolReceipt,error){
    empty:=freeNetAutoPoolReceipt{}
    if !freeNetAutoPoolPathAbsent(freeNetAutoPoolPending()){
        return empty,errAutoPoolStateUnknown
    }
    receipt,_,_,err:=freeNetAutoPoolReadCheckpoint(freeNetAutoPoolActive())
    if err!=nil{return empty,err}
    out,readErr:=os.ReadFile(a.cfg.OutPath)
    routing,routeErr:=os.ReadFile(filepath.Join(a.routingConfigDir(),"05_routing.json"))
    if readErr!=nil||routeErr!=nil ||
       sha256Hex(out)!=receipt.AfterOutSHA||
       sha256Hex(routing)!=receipt.AfterRoutingSHA {
        return empty,errAutoPoolStateUnknown
    }
    return receipt,nil
}

func (a *app) handleFreeNetAutoPoolStatus(w http.ResponseWriter,r *http.Request){
    resp:=freeNetAutoStatusResponse{Success:true,Mutation:"NONE",Status:"not-configured"}
    if freeNetAutoPoolPathAbsent(freeNetAutoPoolPending())&&
       freeNetAutoPoolPathAbsent(freeNetAutoPoolActive()){
        writeJSON(w,http.StatusOK,resp);return
    }
    receipt,err:=a.freeNetAutoPoolLiveReceipt()
    if err!=nil{resp.Success=false;resp.Status="state-unknown-stop";writeJSON(w,http.StatusServiceUnavailable,resp);return}
    resp.Active=true;resp.NodeCount=1+len(receipt.ProfileTags)
    resp.Nodes=[]map[string]string{{"id":"original","tag":"vless-reality"}}
    var ids []string
    for id:=range receipt.ProfileTags{ids=append(ids,id)}
    sort.Strings(ids)
    for _,id:=range ids{resp.Nodes=append(resp.Nodes,map[string]string{"id":id,"tag":receipt.ProfileTags[id]})}
    resp.CoreIdentityVerified=a.freeNetAutoXrayIdentity()!=""
    if !resp.CoreIdentityVerified {
        resp.Status="runtime-owner-unverified";writeJSON(w,http.StatusConflict,resp);return
    }
    ctx,cancel:=context.WithTimeout(r.Context(),5*time.Second)
    defer cancel()
    apiRun:=func(ctx context.Context,args ...string)([]byte,error){
        return runFreeNetAutoXrayCLI(ctx,a.routingXrayBin(),args...)
    }
    override,selected,err:=freeNetAutoReadBalancer(ctx,apiRun,receipt.APIAddress)
    if err!=nil{resp.Status="runtime-balancer-unavailable";writeJSON(w,http.StatusConflict,resp);return}
    allowed:=map[string]bool{"vless-reality":true}
    for _,tag:=range receipt.ProfileTags{allowed[tag]=true}
    if override!=""&&!allowed[override]{resp.Success=false;resp.Status="foreign-override-stop";writeJSON(w,http.StatusServiceUnavailable,resp);return}
    if selected!=""&&!allowed[selected]{resp.Success=false;resp.Status="unknown-principal-stop";writeJSON(w,http.StatusServiceUnavailable,resp);return}
    resp.CurrentTag=override
    if resp.CurrentTag==""{resp.CurrentTag=selected}
    resp.APIReachable=true;resp.Status="live-balancer-ready"
    writeJSON(w,http.StatusOK,resp)
}

var errAutoPoolStateUnknown=errors.New("AutoVPN active runtime state unverified")

func (a *app) handleFreeNetAutoPoolSwitch(w http.ResponseWriter,r *http.Request){
    resp:=freeNetAutoRPCResponse{Mutation:"NONE",Rollback:"NOT_APPLIED",Status:"blocked"}
    var req freeNetAutoSwitchRequest
    if !decodeFreeNetAutoJSON(w,r,&req){return}
    if req.ProfileID!="original" && !validProfileID(req.ProfileID){
        resp.Status="invalid-profile";writeJSON(w,http.StatusBadRequest,resp);return
    }
    if a.mutationBlockedBySelfUpdate(w){return}
    select{case a.sem<-struct{}{}:defer func(){<-a.sem}()
    default:resp.Status="operation-busy";writeJSON(w,http.StatusConflict,resp);return}
    releaseHealth,err:=acquireAutomationHealthLock()
    if err!=nil{resp.Status="health-busy";writeJSON(w,http.StatusConflict,resp);return}
    defer releaseHealth()
    releaseBest,err:=acquireAutomationBestLock()
    if err!=nil{resp.Status="best-server-busy";writeJSON(w,http.StatusConflict,resp);return}
    defer releaseBest()
    if !a.visionQUICReadyWhileHeld(){
        resp.Status="runtime-or-transaction-unknown";writeJSON(w,http.StatusConflict,resp);return
    }
    receipt,err:=a.freeNetAutoPoolLiveReceipt()
    if err!=nil{resp.Status="pool-not-ready";writeJSON(w,http.StatusConflict,resp);return}
    target,ok:=receipt.ProfileTags[req.ProfileID]
    if req.ProfileID=="original"{target="vless-reality";ok=true}
    if !ok||!freeNetAutoTagAllowed(target){
        resp.Status="profile-outside-active-pool";writeJSON(w,http.StatusConflict,resp);return
    }
    // The running config must still encode this exact measured profile from
    // a fresh source. A silent subscription revision cannot switch secrets.
    probeCtx,cancel:=context.WithTimeout(r.Context(),48*time.Second)
    defer cancel()
    if req.ProfileID!="original" {
    internal,_,_,source,e:=a.discoverBestServerCandidatesWithSource(probeCtx)
    if e!=nil||!source.Fresh{
        resp.Status="fresh-subscription-unavailable";writeJSON(w,http.StatusConflict,resp);return
    }
    var matched *bestServerInternalCandidate
    for i:=range internal{
        if internal[i].Profile.ID==req.ProfileID{matched=&internal[i];break}
    }
    if matched==nil{
        resp.Status="profile-not-in-fresh-subscription";writeJSON(w,http.StatusConflict,resp);return
    }
    expected,err:=buildBestServerProbeOutbound(matched.Raw,matched.Profile)
    if err!=nil{resp.Status="profile-unsupported";writeJSON(w,http.StatusConflict,resp);return}
    expected["tag"]=target
    liveConfig,readErr:=os.ReadFile(a.cfg.OutPath)
    if readErr!=nil||!freeNetAutoOutboundMatches(liveConfig,target,expected){
        resp.Status="active-profile-revision-mismatch";writeJSON(w,http.StatusConflict,resp);return
    }
    if !a.probeBestServerApplicationPreflight(probeCtx,*matched).OK{
        resp.Status="fresh-https-candidate-rejected";writeJSON(w,http.StatusConflict,resp);return
    }
    }
    if os.Mkdir(a.cfg.LockPath,0700)!=nil{
        resp.Status="shared-writer-busy";writeJSON(w,http.StatusConflict,resp);return
    }
    if os.WriteFile(filepath.Join(a.cfg.LockPath,"autovpn-pool-owner"),
        []byte("freenet-auto-pool-v1\n"),0600)!=nil{
        resp.Status="writer-lock-uncertain";setAutomationMutationBlocked(true)
        resp.Mutation="STOP";resp.Rollback="FAILED/UNKNOWN"
        writeJSON(w,http.StatusServiceUnavailable,resp);return
    }
    retainLock:=false
    defer func(){if !retainLock{freeNetAutoReleaseLock(a.cfg.LockPath)}}()
    if _,err:=a.freeNetAutoPoolLiveReceipt();err!=nil{
        resp.Status="pool-revision-changed";writeJSON(w,http.StatusConflict,resp);return
    }
    runCtx,runCancel:=context.WithTimeout(context.Background(),18*time.Second)
    defer runCancel()
    runner:=func(ctx context.Context,args ...string)([]byte,error){
        return runFreeNetAutoXrayCLI(ctx,a.routingXrayBin(),args...)
    }
    allowed:=map[string]bool{"vless-reality":true}
    for _,tag:=range receipt.ProfileTags{allowed[tag]=true}
    result,err:=freeNetAutoSwitchWith(runCtx,receipt.APIAddress,target,allowed,runner,
        func(ctx context.Context)error{return freeNetAutoProbeXrayEgress(ctx,receipt.ProbeAddress)},
        a.freeNetAutoXrayIdentity)
    resp.Mutation=result.Mutation;resp.Rollback=result.Rollback;resp.Status=result.Status
    if result.Mutation=="STOP"{
        setAutomationMutationBlocked(true);retainLock=true
        writeJSON(w,http.StatusServiceUnavailable,resp);return
    }
    if err!=nil{writeJSON(w,http.StatusBadGateway,resp);return}
    // Manual live selection counts as a real switch for media hysteresis.
    // A new synthetic slowdown must not immediately undo the operator's
    // choice. Persist the quiet window before returning success.
    if result.Mutation=="SWITCHED" {
        if e:=v3WriteState(map[string]string{"POOL_MEDIA_LAST_SWITCH":time.Now().UTC().Format(time.RFC3339)});e!=nil{
            // The runtime switch succeeded, so never pretend it did not
            // happen. Keep the accurate result, flag QoE policy persistence.
            resp.Status="router-xray-egress-verified-media-cooldown-unpersisted"
        }
    }
    resp.Success=true;resp.CurrentTag=result.Target
    writeJSON(w,http.StatusOK,resp)
}

func freeNetAutoOutboundMatches(live []byte,tag string,expected map[string]any)bool{
    if rejectDuplicateJSONKeys(live)!=nil{return false}
    var doc struct {Outbounds []map[string]any `json:"outbounds"`}
    if json.Unmarshal(live,&doc)!=nil{return false}
    expectedJSON,err:=json.Marshal(expected)
    if err!=nil{return false}
    found:=0
    for _,out:=range doc.Outbounds{
        if value,ok:=out["tag"].(string);ok&&value==tag{
            found++
            actual,_:=json.Marshal(out)
            if !bytes.Equal(actual,expectedJSON){return false}
        }
    }
    return found==1
}

func registerFreeNetAutoPoolAPI(mux *http.ServeMux,a *app){
    mux.HandleFunc("GET /api/automation/pool/status",a.requireAuth(a.handleFreeNetAutoPoolStatus))
    mux.HandleFunc("POST /api/automation/pool/plan",a.requireAuth(a.handleFreeNetAutoPoolPlan))
    mux.HandleFunc("POST /api/automation/pool/activate",a.requireAuth(a.handleFreeNetAutoPoolActivate))
    mux.HandleFunc("POST /api/automation/pool/switch",a.requireAuth(a.handleFreeNetAutoPoolSwitch))
    mux.HandleFunc("POST /api/automation/pool/disable",a.requireAuth(a.handleFreeNetAutoPoolDisable))
}
