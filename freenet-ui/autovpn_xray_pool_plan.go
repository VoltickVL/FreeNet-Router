package main

import (
    "bytes"
    "context"
    "encoding/json"
    "errors"
    "io"
    "net"
    "net/http"
    "os"
    "path/filepath"
    "strings"
    "sync"
    "time"
)

const freeNetAutoPlanLifetime=120*time.Second

// Management decision beta.87: XKeen 2.1 / Xray 26.10.31 own outbound
// balancing. No new independent FreeNet Xray pool may be provisioned.
// Existing active pools remain available to guarded status/switch/disable;
// do not delete or rewrite their Xray configuration during migration.
const freeNetAutoLegacyProvisionEnabled=false

type freeNetAutoPlanRequest struct {
    SelectionToken string `json:"selection_token"`
    ProfileIDs []string `json:"profile_ids"`
}
type freeNetAutoActivateRequest struct {
    PlanToken string `json:"plan_token"`
    Acknowledge string `json:"acknowledge"`
}
type freeNetAutoPlanResponse struct {
    Success bool `json:"success"`
    Ready bool `json:"ready"`
    Mutation string `json:"mutation"`
    PlanToken string `json:"plan_token,omitempty"`
    ExpiresInSeconds int `json:"expires_in_seconds,omitempty"`
    ActiveNodes int `json:"active_nodes,omitempty"`
    ExpectedDelta string `json:"expected_delta,omitempty"`
    ExpectedNoDelta string `json:"expected_no_delta,omitempty"`
    Status string `json:"status"`
}
type freeNetAutoPendingPlan struct {
    Token string
    SelectionToken string
    CreatedAt time.Time
    BeforeOut []byte
    BeforeRouting []byte
    Candidate freeNetAutoPoolCandidate
    ProfileTags map[string]string
    OriginalEndpoint string
    OriginalFilter string
}
var freeNetAutoPlans struct {
    sync.Mutex
    Current *freeNetAutoPendingPlan
}

func decodeFreeNetAutoJSON(w http.ResponseWriter,r *http.Request,target any)bool{
    if !sameOrigin(r){http.Error(w,"cross-origin rejected",http.StatusForbidden);return false}
    if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(r.Header.Get("Content-Type"))),"application/json"){
        http.Error(w,"application/json required",http.StatusUnsupportedMediaType);return false
    }
    body:=http.MaxBytesReader(w,r.Body,4096)
    defer body.Close()
    raw,err:=io.ReadAll(body)
    if err!=nil||rejectDuplicateJSONKeys(raw)!=nil{
        http.Error(w,"invalid AutoVPN request",http.StatusBadRequest);return false
    }
    decoder:=json.NewDecoder(bytes.NewReader(raw))
    decoder.DisallowUnknownFields()
    if decoder.Decode(target)!=nil{
        http.Error(w,"invalid AutoVPN request",http.StatusBadRequest);return false
    }
    var extra any
    if decoder.Decode(&extra)!=io.EOF{
        http.Error(w,"trailing AutoVPN request",http.StatusBadRequest);return false
    }
    return true
}

func freeNetAutoReserveControlPorts()(string,string,error){
    a,err:=net.Listen("tcp","127.0.0.1:0")
    if err!=nil{return "","",errors.New("no local API port")}
    defer a.Close()
    b,err:=net.Listen("tcp","127.0.0.1:0")
    if err!=nil{return "","",errors.New("no local probe port")}
    defer b.Close()
    api:=a.Addr().String();probe:=b.Addr().String()
    if !freeNetAutoAPIAddressAllowed(api)||!freeNetAutoAPIAddressAllowed(probe)||api==probe{
        return "","",errors.New("invalid reserved loopback ports")
    }
    return api,probe,nil
}
func freeNetAutoLocalPortsFree(addresses ...string)bool{
    opened:=[]net.Listener{}
    defer func(){for _,listener:=range opened{_ =listener.Close()}}()
    for _,address:=range addresses{
        if !freeNetAutoAPIAddressAllowed(address){return false}
        listener,err:=net.Listen("tcp",address)
        if err!=nil{return false}
        opened=append(opened,listener)
    }
    return true
}

func (a *app) freeNetAutoPoolReadSource()([]byte,[]byte,string,string,error){
    if a==nil || filepath.Base(a.cfg.OutPath)!="04_outbounds.json" {
        return nil,nil,"","",errors.New("unsupported live Xray outbound layout")
    }
    dir:=a.routingConfigDir()
    get:=func(path string)([]byte,error){
        info,err:=os.Lstat(path)
        if err!=nil||!info.Mode().IsRegular()||info.Size()==0||
            info.Size()>visionQUICMaxConfigBytes {return nil,errors.New("Xray source unavailable")}
        b,err:=os.ReadFile(path)
        if err!=nil||len(b)==0||len(b)>visionQUICMaxConfigBytes{return nil,errors.New("Xray source unreadable")}
        return b,nil
    }
    out,err:=get(a.cfg.OutPath);if err!=nil{return nil,nil,"","",err}
    routing,err:=get(filepath.Join(dir,"05_routing.json"));if err!=nil{return nil,nil,"","",err}
    current:=readBestServerCurrentEndpoint(a.cfg.OutPath)
    filter:=readBestServerCurrentFilter(a.cfg.FilterPath)
    if current==""||filter=="" {return nil,nil,"","",errors.New("active VPN identity unverified")}
    return out,routing,current,filter,nil
}

func (a *app) handleFreeNetAutoPoolPlan(w http.ResponseWriter,r *http.Request){
    resp:=freeNetAutoPlanResponse{Mutation:"NONE",Status:"blocked"}
    var req freeNetAutoPlanRequest
    if !decodeFreeNetAutoJSON(w,r,&req){return}
    if !freeNetAutoLegacyProvisionEnabled{
        resp.Status="native-xkeen-sb-preferred-no-new-pool"
        writeJSON(w,http.StatusConflict,resp);return
    }
    if !a.visionQUICReady() || !freeNetAutoPoolPathAbsent(freeNetAutoPoolPending())||
        !freeNetAutoPoolPathAbsent(freeNetAutoPoolActive()){
        writeJSON(w,http.StatusConflict,resp);return
    }
    if !validBestServerSelectionToken(req.SelectionToken) ||
       len(req.ProfileIDs)==0||len(req.ProfileIDs)>=freeNetAutoPoolLimit{
        resp.Status="invalid-selected-profiles";writeJSON(w,http.StatusBadRequest,resp);return
    }
    ids:=map[string]bool{}
    for _,id:=range req.ProfileIDs{
        if !validProfileID(id)||ids[id]{
            resp.Status="invalid-selected-profiles";writeJSON(w,http.StatusBadRequest,resp);return
        }
        ids[id]=true
    }
    beforeOut,beforeRouting,current,filter,err:=a.freeNetAutoPoolReadSource()
    if err!=nil{
        resp.Status="active-state-unverified";writeJSON(w,http.StatusConflict,resp);return
    }
    reserve:=[]map[string]any{}
    profileTags:=map[string]string{}
    ctx,cancel:=context.WithTimeout(r.Context(),100*time.Second)
    defer cancel()
    for i,id:=range req.ProfileIDs{
        entry,err:=a.loadBestServerSelectionCandidate(req.SelectionToken,id,current,filter)
        if err!=nil {
            resp.Status="measured-selection-stale";writeJSON(w,http.StatusConflict,resp);return
        }
        if endpointsEqual(profileEndpoint(entry.Profile),current){
            resp.Status="alternative-matches-current";writeJSON(w,http.StatusConflict,resp);return
        }
        // Source snapshot eligibility is necessary but not sufficient:
        // require fresh named HTTPS proof before preparing this pool.
        probe:=a.probeBestServerApplicationPreflight(ctx,entry)
        if !probe.OK||ctx.Err()!=nil{
            resp.Status="alternative-https-rejected";writeJSON(w,http.StatusConflict,resp);return
        }
        alt,err:=buildBestServerProbeOutbound(entry.Raw,entry.Profile)
        if err!=nil{
            resp.Status="alternative-transport-unsupported";writeJSON(w,http.StatusConflict,resp);return
        }
        reserve=append(reserve,alt)
        profileTags[id]=freeNetAutoOutboundPrefix+string(rune('1'+i))
    }
    apiAddr,probeAddr,err:=freeNetAutoReserveControlPorts()
    if err!=nil{resp.Status="loopback-port-unavailable";writeJSON(w,http.StatusServiceUnavailable,resp);return}
    candidate,err:=buildFreeNetAutoPoolCandidate(beforeOut,beforeRouting,reserve,apiAddr,probeAddr)
    if err!=nil{resp.Status="config-unsupported";writeJSON(w,http.StatusConflict,resp);return}
    if a.validateFreeNetAutoPoolCandidate(ctx,candidate)!=nil{
        resp.Status="full-xray-preflight-failed";writeJSON(w,http.StatusConflict,resp);return
    }
    nonce,err:=newBestServerSelectionToken()
    if err!=nil{resp.Status="plan-token-unavailable";writeJSON(w,http.StatusServiceUnavailable,resp);return}
    freeNetAutoPlans.Lock()
    freeNetAutoPlans.Current=&freeNetAutoPendingPlan{
        Token:nonce,SelectionToken:req.SelectionToken,CreatedAt:time.Now().UTC(),
        BeforeOut:beforeOut,BeforeRouting:beforeRouting,Candidate:candidate,
        ProfileTags:profileTags,OriginalEndpoint:current,OriginalFilter:filter,
    }
    freeNetAutoPlans.Unlock()
    resp.Success=true;resp.Ready=true;resp.Status="ready";resp.PlanToken=nonce
    resp.ExpiresInSeconds=int(freeNetAutoPlanLifetime.Seconds())
    resp.ActiveNodes=1+len(req.ProfileIDs)
    resp.ExpectedDelta="One guarded Xray core-only restart to prepare a local balancer; later node switches use live Xray API without restart"
    resp.ExpectedNoDelta="ISP policies, DIRECT rules, DNS, credentials and firewall unchanged"
    writeJSON(w,http.StatusOK,resp)
}
