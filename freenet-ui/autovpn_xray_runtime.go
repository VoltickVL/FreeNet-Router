package main

import (
    "context"
    "errors"
    "io"
    "net/http"
    "net/url"
    "os/exec"
    "strings"
    "time"
)

// The controller never executes a shell, never accepts a free-form command
// and never exposes Xray CLI output through the HTTP API.
type freeNetAutoXrayRunner func(context.Context, ...string)([]byte,error)
type freeNetAutoLiveCheck func(context.Context)error
type freeNetAutoIdentityCheck func()string

type freeNetAutoSwitchResult struct {
    Mutation string `json:"mutation"`
    Rollback string `json:"rollback"`
    Target string `json:"target,omitempty"`
    Status string `json:"status"`
}

func freeNetAutoParseBalancerInfo(text string)(override, selected string,err error) {
    if len(text)==0 || len(text)>16<<10{return "","",errors.New("balancer info unavailable")}
    section:=""
    foundOverride:=false;foundSelects:=false
    for _,line:=range strings.Split(text,"\n"){
        line=strings.TrimSpace(line)
        switch{
        case strings.HasPrefix(line,"Selecting Override:"):
            section="override";foundOverride=true;continue
        case strings.HasPrefix(line,"Selects:"):
            section="selects";foundSelects=true;continue
        }
        fields:=strings.Fields(line)
        if len(fields)!=2 || !xkeenPortDecimal(fields[0]){continue}
        tag:=fields[1]
        if tag!="vless-reality" && !strings.HasPrefix(tag,freeNetAutoOutboundPrefix) {
            return "","",errors.New("balancer reports unknown target")
        }
        if section=="override" {
            if override!="" {return "","",errors.New("multiple overrides unsupported")}
            override=tag
        }
        if section=="selects" {
            if selected!="" {return "","",errors.New("multiple primary targets unsupported")}
            selected=tag
        }
    }
    if !foundOverride || !foundSelects {return "","",errors.New("balancer status incomplete")}
    return override,selected,nil
}

// Strictly bounded invocation. All arguments are constructed from previously
// approved tags and the loopback-only address; no user-provided arguments.
func runFreeNetAutoXrayCLI(ctx context.Context,binary string,args ...string)([]byte,error) {
    if ctx==nil || strings.TrimSpace(binary)==""{return nil,errors.New("Xray CLI unavailable")}
    runCtx,cancel:=context.WithTimeout(ctx,4*time.Second)
    defer cancel()
    cmd:=exec.CommandContext(runCtx,binary,append([]string{"api"},args...)...)
    cmd.Stderr=io.Discard
    output,err:=cmd.Output()
    if err!=nil||runCtx.Err()!=nil||len(output)>16<<10 {
        return nil,errors.New("Xray API call failed")
    }
    return output,nil
}

func freeNetAutoReadBalancer(ctx context.Context,runner freeNetAutoXrayRunner,apiAddress string)(override,selected string,err error) {
    output,err:=runner(ctx,"bi","-s",apiAddress,freeNetAutoBalancerTag)
    if err!=nil{return "","",errors.New("Xray balancer API unavailable")}
    return freeNetAutoParseBalancerInfo(string(output))
}

// Switch may only be invoked by the guarded FreeNet AutoVPN state machine.
// It has no release or apply side effects on its own. In particular, it never
// restarts the core or edits subscriptions, routing files, DNS or firewall.
func freeNetAutoSwitchWith(
    ctx context.Context,
    apiAddress,target string,
    allowed map[string]bool,
    runner freeNetAutoXrayRunner,
    checkPath freeNetAutoLiveCheck,
    processIdentity freeNetAutoIdentityCheck,
) (freeNetAutoSwitchResult,error) {
    result:=freeNetAutoSwitchResult{Mutation:"NONE",Rollback:"NOT_APPLIED",Status:"blocked"}
    if ctx==nil||runner==nil||checkPath==nil||processIdentity==nil||
       !freeNetAutoAPIAddressAllowed(apiAddress)||
       !allowed[target]||!freeNetAutoTagAllowed(target){
        return result,errors.New("AutoVPN runtime command not approved")
    }
    beforeIdentity:=processIdentity()
    if beforeIdentity=="" {return result,errors.New("Xray runtime identity unverified")}
    original,_,err:=freeNetAutoReadBalancer(ctx,runner,apiAddress)
    if err!=nil{return result,err}
    if original!="" && (!allowed[original]||!freeNetAutoTagAllowed(original)){
        return result,errors.New("another controller owns the Xray balancer")
    }
    if processIdentity()!=beforeIdentity {return result,errors.New("Xray runtime changed before switch")}
    if original==target {
        if checkPath(ctx)!=nil||processIdentity()!=beforeIdentity {
            return result,errors.New("current live target failed acceptance")
        }
        result.Status="already-active";result.Rollback="NOT_NEEDED";result.Target=target
        return result,nil
    }
    apply:=func(dest string)error {
        var e error
        if dest=="" {
            _,e=runner(ctx,"bo","-s",apiAddress,"-b",freeNetAutoBalancerTag,"-r")
        }else{
            _,e=runner(ctx,"bo","-s",apiAddress,"-b",freeNetAutoBalancerTag,dest)
        }
        return e
    }
    rollback:=func()error {
        if processIdentity()!=beforeIdentity{return errors.New("Xray runtime changed during rollback")}
        if apply(original)!=nil{return errors.New("override restoration command failed")}
        restored,_,readErr:=freeNetAutoReadBalancer(ctx,runner,apiAddress)
        if readErr!=nil||restored!=original||processIdentity()!=beforeIdentity{
            return errors.New("override restoration unconfirmed")
        }
        return nil
    }
    // Once a bo call starts, all failures must attempt a verified rollback.
    applyErr:=apply(target)
    current,_,readErr:=freeNetAutoReadBalancer(ctx,runner,apiAddress)
    if applyErr==nil && readErr==nil && current==target &&
        processIdentity()==beforeIdentity && checkPath(ctx)==nil &&
        processIdentity()==beforeIdentity {
        result.Mutation="SWITCHED";result.Rollback="NOT_NEEDED"
        result.Status="router-xray-egress-verified";result.Target=target
        return result,nil
    }
    if rollback()!=nil {
        result.Mutation="STOP";result.Rollback="FAILED/UNKNOWN";result.Status="rollback-unconfirmed"
        return result,errors.New("AutoVPN override rollback unconfirmed")
    }
    result.Mutation="ROLLED_BACK";result.Rollback="SUCCESS";result.Status="target-rejected"
    return result,errors.New("AutoVPN candidate failed live acceptance")
}
func freeNetAutoTagAllowed(tag string)bool{
    if tag=="vless-reality"{return true}
    if !strings.HasPrefix(tag,freeNetAutoOutboundPrefix){return false}
    suffix:=strings.TrimPrefix(tag,freeNetAutoOutboundPrefix)
    return suffix=="1"||suffix=="2"
}
func freeNetAutoAPIAddressAllowed(address string)bool {
    u,err:=url.Parse("tcp://"+address)
    if err!=nil || u.Hostname()!="127.0.0.1"{return false}
    port:=u.Port()
    if !xkeenPortDecimal(port) {return false}
    value:=0
    for _,c:=range port{value=value*10+int(c-'0');if value>65535{return false}}
    return value>=1024
}

// Verifies data flow through the running, loopback-only SOCKS inbound
// attached exclusively to the balancer. There is never a DIRECT retry.
func freeNetAutoProbeXrayEgress(ctx context.Context,probeAddress string)error {
    if !freeNetAutoAPIAddressAllowed(probeAddress){return errors.New("unsafe Xray probe listener")}
    proxyURL:=&url.URL{Scheme:"socks5",Host:probeAddress}
    transport:=&http.Transport{
        Proxy:http.ProxyURL(proxyURL),
        MaxIdleConns:1,
        DisableKeepAlives:true,
        TLSHandshakeTimeout:3*time.Second,
        ResponseHeaderTimeout:3*time.Second,
    }
    defer transport.CloseIdleConnections()
    client:=&http.Client{Transport:transport,Timeout:6*time.Second,
        CheckRedirect:func(*http.Request,[]*http.Request)error{return http.ErrUseLastResponse}}
    req,err:=http.NewRequestWithContext(ctx,http.MethodGet,freeNetAutoProbeURL,nil)
    if err!=nil{return errors.New("HTTPS probe unavailable")}
    resp,err:=client.Do(req)
    if err!=nil{return errors.New("live Xray HTTPS egress unconfirmed")}
    defer resp.Body.Close()
    if resp.StatusCode!=http.StatusNoContent {
        return errors.New("live Xray HTTPS response unacceptable")
    }
    return nil
}
