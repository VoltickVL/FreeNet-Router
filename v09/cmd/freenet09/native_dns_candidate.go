package main

// Build a disposable, testable Xray candidate without inheriting the old
// FreeNet 0.8 Split DNS implementation. The original Xray configuration,
// Xray process, XKeen, resolver, subscription and FreeNet 0.8 stay untouched.
//
// IMPORTANT: a successful Xray -test is NOT authorization to apply. When
// Xray owns UDP/TCP :53, removing DNS inbounds could disconnect the router.
// No code in this file contains a live apply/restart action.

import (
    "bytes"
    "context"
    "encoding/json"
    "errors"
    "io"
    "os"
    "os/exec"
    "path/filepath"
    "strings"
    "time"
)

type nativeDNSCandidateReport struct {
    State string `json:"state"`
    Runtime string `json:"runtime"`
    DNS53Owner string `json:"dns53_owner"`
    RemovedResolvers int `json:"removed_resolvers"`
    RemovedInbounds int `json:"removed_inbounds"`
    RemovedOutbounds int `json:"removed_outbounds"`
    RemovedRules int `json:"removed_rules"`
    Applied bool `json:"applied"`
}

func stoppedDNSCandidate() nativeDNSCandidateReport {
    return nativeDNSCandidateReport{State:"STOP",Runtime:"NOT_VERIFIED",
        DNS53Owner:"UNKNOWN",Applied:false}
}

func parseNativeObject(data []byte) (map[string]json.RawMessage,error) {
    clean,err:=stripXrayComments(data)
    if err!=nil {return nil,errors.New("invalid native JSONC")}
    var out map[string]json.RawMessage
    if json.Unmarshal(clean,&out)!=nil || out==nil {
        return nil,errors.New("native JSON object required")
    }
    return out,nil
}

func parseNativeArray(raw json.RawMessage)([]json.RawMessage,error) {
    var out []json.RawMessage
    if json.Unmarshal(raw,&out)!=nil || out==nil {
        return nil,errors.New("native JSON array required")
    }
    return out,nil
}

func nativeJSONField(obj map[string]json.RawMessage,key string) string {
    var v string
    _=json.Unmarshal(obj[key],&v)
    return v
}

func onlyKeys(obj map[string]json.RawMessage,allowed ...string)bool {
    keys:=make(map[string]bool,len(allowed))
    for _,key:=range allowed{keys[key]=true}
    for key:=range obj {if !keys[key] {return false}}
    return true
}

func nativePort53(raw json.RawMessage)bool {
    if len(raw)==0{return false}
    var number int
    if json.Unmarshal(raw,&number)==nil{return number==53}
    var str string
    return json.Unmarshal(raw,&str)==nil && str=="53"
}

func nativeDNSInboundTag(tag string)bool {
    return tag=="dns-direct"||tag=="dns-vless"||tag=="dns-in"
}

// Each JSON document is handled independently, preserving all non-DNS
// fragments and all unrelated routing rules. Unknown metadata/shape = STOP.
// We do not read or return user VPN settings, URLs, DNS endpoints or secrets.
func buildLegacySplitFreeXrayCandidate(src map[string][]byte)(map[string][]byte,nativeDNSCandidateReport,error){
    result:=stoppedDNSCandidate()
    for _,name:=range []string{"02_dns.json","03_inbounds.json","04_outbounds.json","05_routing.json"}{
        if len(src[name])==0{return nil,result,errors.New("required native file absent")}
    }
    candidate:=make(map[string][]byte,len(src))
    for k,v:=range src {candidate[k]=v}

    // 02_dns: accept ONLY FreeNet 0.8's known all-managed DNS fragment.
    // A custom resolver is user policy; do not guess how to translate it.
    dns,err:=parseNativeObject(src["02_dns.json"])
    if err!=nil{return nil,result,err}
    dnsRaw,hasDNS:=dns["dns"]
    if !hasDNS {return nil,result,errors.New("no legacy DNS found")}
    if !onlyKeys(dns,"dns"){return nil,result,errors.New("unknown DNS top-level content")}
    dnsDoc,err:=parseNativeObject(dnsRaw)
    if err!=nil{return nil,result,err}
    if !onlyKeys(dnsDoc,"tag","servers","queryStrategy") ||
        nativeJSONField(dnsDoc,"tag")!="dns-vless" {
        return nil,result,errors.New("DNS fragment not known 0.8-managed format")
    }
    servers,err:=parseNativeArray(dnsDoc["servers"])
    if err!=nil{return nil,result,err}
    direct,vpn:=0,0
    for _,raw:=range servers {
        entry,err:=parseNativeObject(raw)
        if err!=nil{return nil,result,err}
        switch nativeJSONField(entry,"tag"){
        case "dns-direct":direct++
        case "dns-vless":vpn++
        default:return nil,result,errors.New("user or unknown DNS resolver present")
        }
    }
    if direct==0||vpn==0{return nil,result,errors.New("partial legacy DNS resolver set")}
    result.RemovedResolvers=len(servers)
    candidate["02_dns.json"]=[]byte("{}\n")

    // 03_inbounds: only remove EXACT DNS-owned port 53 dokodemo-door
    // entries. Unknown or unrelated inbounds are carried over unchanged.
    ins,err:=parseNativeObject(src["03_inbounds.json"])
    if err!=nil{return nil,result,err}
    originalIn,err:=parseNativeArray(ins["inbounds"])
    if err!=nil{return nil,result,err}
    preservedIn:=make([]json.RawMessage,0,len(originalIn))
    for _,raw:=range originalIn {
        entry,err:=parseNativeObject(raw)
        if err!=nil{return nil,result,err}
        tag:=nativeJSONField(entry,"tag")
        if !nativeDNSInboundTag(tag) {
            preservedIn=append(preservedIn,raw);continue
        }
        if nativeJSONField(entry,"protocol")!="dokodemo-door" ||
            !nativePort53(entry["port"]) {
            return nil,result,errors.New("unknown DNS inbound cannot be removed")
        }
        result.RemovedInbounds++
    }
    ins["inbounds"],_ =json.Marshal(preservedIn)
    candidate["03_inbounds.json"],_=json.Marshal(ins)

    // 04_outbounds: the only removable outbound is original dns-out.
    outs,err:=parseNativeObject(src["04_outbounds.json"])
    if err!=nil{return nil,result,err}
    originalOut,err:=parseNativeArray(outs["outbounds"])
    if err!=nil{return nil,result,err}
    preservedOut:=make([]json.RawMessage,0,len(originalOut))
    for _,raw:=range originalOut{
        entry,err:=parseNativeObject(raw)
        if err!=nil{return nil,result,err}
        if nativeJSONField(entry,"tag")!="dns-out" {
            preservedOut=append(preservedOut,raw);continue
        }
        if nativeJSONField(entry,"protocol")!="dns" {
            return nil,result,errors.New("unknown DNS outbound cannot be removed")
        }
        result.RemovedOutbounds++
    }
    if result.RemovedOutbounds!=1 {
        return nil,result,errors.New("partial or duplicate legacy dns-out")
    }
    outs["outbounds"],_=json.Marshal(preservedOut)
    candidate["04_outbounds.json"],_=json.Marshal(outs)

    // 05_routing: keep the exact sequence/values of all user rules;
    // only known FreeNet 0.8 DNS-only rules may be removed.
    routes,err:=parseNativeObject(src["05_routing.json"])
    if err!=nil{return nil,result,err}
    routing,err:=parseNativeObject(routes["routing"])
    if err!=nil{return nil,result,err}
    originalRules,err:=parseNativeArray(routing["rules"])
    if err!=nil{return nil,result,err}
    preservedRules:=make([]json.RawMessage,0,len(originalRules))
    directRoute,vpnRoute,portRoute:=0,0,0
    for _,raw:=range originalRules {
        rule,err:=parseNativeObject(raw)
        if err!=nil{return nil,result,err}
        outbound:=nativeJSONField(rule,"outboundTag")
        inbound:=[]string{}
        if v,ok:=rule["inboundTag"];ok {
            if json.Unmarshal(v,&inbound)!=nil{
                return nil,result,errors.New("routing inbound shape unknown")
            }
        }
        hasDNSInbound:=false
        for _,tag:=range inbound {
            if nativeDNSInboundTag(tag){hasDNSInbound=true}
        }
        if !hasDNSInbound && outbound!="dns-out" {
            preservedRules=append(preservedRules,raw);continue
        }
        if !onlyKeys(rule,"type","port","inboundTag","outboundTag") {
            return nil,result,errors.New("DNS rule contains unknown user selectors")
        }
        if kind:=nativeJSONField(rule,"type");kind!="" && kind!="field" {
            return nil,result,errors.New("unexpected legacy DNS rule type")
        }
        if outbound=="dns-out" {
            if (!nativePort53(rule["port"]) && !(len(inbound)==1 && inbound[0]=="dns-in")) ||
                (len(inbound)>0 && !(len(inbound)==1 && inbound[0]=="dns-in")){
                return nil,result,errors.New("unknown dns-out route")
            }
            portRoute++
        } else {
            if len(inbound)!=1 || len(rule["port"])!=0 {
                return nil,result,errors.New("mixed DNS/user routing rule")
            }
            switch {
            case inbound[0]=="dns-direct" && outbound=="direct":directRoute++
            case inbound[0]=="dns-vless" && outbound=="vless-reality":vpnRoute++
            default:return nil,result,errors.New("unexpected legacy DNS outbound route")
            }
        }
        result.RemovedRules++
    }
    if directRoute!=1 || vpnRoute!=1 || portRoute!=1 {
        return nil,result,errors.New("partial/duplicate legacy DNS-only routing")
    }
    routing["rules"],_=json.Marshal(preservedRules)
    routes["routing"],_=json.Marshal(routing)
    candidate["05_routing.json"],_=json.Marshal(routes)

    // A reference in any other Xray fragment would now be dangling.
    for name,content:=range candidate {
        if name=="02_dns.json"||name=="03_inbounds.json"||
            name=="04_outbounds.json"||name=="05_routing.json" {continue}
        for _,token:=range [][]byte{[]byte("dns-out"),[]byte("dns-direct"),[]byte("dns-vless"),[]byte("dns-in")} {
            if bytes.Contains(content,token) {
                return nil,result,errors.New("unknown cross-fragment legacy DNS dependency")
            }
        }
    }
    result.State="CANDIDATE_PREPARED_NOT_APPLIED"
    return candidate,result,nil
}

func readNativeDNS53Owner(ctx context.Context,root string)string {
    path:=filepath.Join(root,"bin/netstat")
    args:=[]string{"-lnptu"}
    switch observedPath(root,"bin/netstat",false) {
    case "PRESENT":
        // A direct netstat executable inside verified Entware.
    case "UNKNOWN":
        // HOME has original Entware /opt/bin/netstat -> /opt/bin/busybox.
        // Never follow an arbitrary symlink into untrusted/external code.
        link,err:=os.Readlink(path)
        busybox:=filepath.Join(root,"bin/busybox")
        if err!=nil || (link!=busybox && link!="busybox") ||
            observedPath(root,"bin/busybox",false)!="PRESENT" {return "UNKNOWN"}
        path=busybox
        args=[]string{"netstat","-lnptu"}
    default:
        // Some routers have the BusyBox applet in /bin instead.
        if filepath.Clean(root)!="/opt" {return "UNKNOWN"}
        path="/bin/netstat"
    }
    info,err:=os.Lstat(path)
    if err!=nil || !info.Mode().IsRegular() || info.Mode().Perm()&0111==0{return "UNKNOWN"}
    limited,cancel:=context.WithTimeout(ctx,3*time.Second)
    defer cancel()
    cmd:=exec.CommandContext(limited,path,args...)
    cmd.Stdin=nil
    cmd.Stderr=io.Discard
    raw,err:=cmd.Output()
    if err!=nil || len(raw)>128*1024{return "UNKNOWN"}
    return netstatDNS53Owner(string(raw))
}

func netstatDNS53Owner(output string)string {
    seenXray,seenRouter,unknown:=false,false,false
    found:=false
    for _,line:=range strings.Split(output,"\n") {
        fields:=strings.Fields(line)
        if len(fields)<6 || !(fields[0]=="tcp"||fields[0]=="udp"||
            fields[0]=="tcp6"||fields[0]=="udp6") {continue}
        local:=fields[3]
        if !(strings.HasSuffix(local,":53")||strings.HasSuffix(local,".53")){continue}
        found=true
        process:=fields[len(fields)-1]
        switch {
        case strings.HasSuffix(process,"/xray"):seenXray=true
        case strings.HasSuffix(process,"/ndnproxy"),strings.HasSuffix(process,"/dnsmasq"):seenRouter=true
        default:unknown=true
        }
    }
    if unknown || (seenXray&&seenRouter){return "UNKNOWN"}
    if seenXray{return "XRAY_DNS53"}
    if seenRouter{return "ROUTER_DNS53"}
    if !found{return "NO_DNS53_LISTENER"}
    return "UNKNOWN"
}

func validateDisposableDNSCandidate(ctx context.Context,root string,files map[string][]byte)bool {
    temp,err:=os.MkdirTemp("","freenet09-dns-candidate-*")
    if err!=nil{return false}
    defer os.RemoveAll(temp)
    if os.Chmod(temp,0700)!=nil{return false}
    for name,data:=range files {
        if filepath.Base(name)!=name || strings.ContainsAny(name,"/\\") {
            return false
        }
        if os.WriteFile(filepath.Join(temp,name),data,0600)!=nil{return false}
    }
    return nativeConfigTest(ctx,root,temp)
}

func nativeDNSCandidatePlan(ctx context.Context,root string)nativeDNSCandidateReport{
    limited,cancel:=context.WithTimeout(ctx,18*time.Second)
    defer cancel()
    ctx=limited
    fail:=stoppedDNSCandidate()
    files,err:=readNativeConfdir(root)
    if err!=nil{return fail}
    candidate,result,err:=buildLegacySplitFreeXrayCandidate(files)
    if err!=nil {return fail}
    liveDir:=filepath.Join(root,"etc/xray/configs")
    if !nativeConfigTest(ctx,root,liveDir) {
        result.State="NATIVE_BASELINE_NOT_VALIDATED"
        return result
    }
    if !validateDisposableDNSCandidate(ctx,root,candidate) {
        result.State="CANDIDATE_REJECTED"
        return result
    }
    after,err:=readNativeConfdir(root)
    if err!=nil || !sameNativeConfdir(files,after){
        result.State="SOURCE_CHANGED_STOP"
        return result
    }
    result.DNS53Owner=readNativeDNS53Owner(ctx,root)
    if result.DNS53Owner=="XRAY_DNS53" {
        result.State="CANDIDATE_VALIDATED_DNS53_TRANSITION_REQUIRED"
    }else if result.DNS53Owner=="ROUTER_DNS53" {
        result.State="CANDIDATE_VALIDATED_NOT_APPLIED"
    }else{
        result.State="DNS53_OWNER_UNKNOWN_STOP"
    }
    // Offline candidate success cannot prove client DNS, VPN or egress.
    return result
}
