package main

// Subscription adapter: an HTTPS subscription is converted ONLY into standard
// Xray outbounds. XKeen and Xray, not FreeNet, own routing and VPN behavior.
// Partner references: XKeen Control Panel (BSD-3-Clause) subscription_parser.go
// and XKeen-UI (MIT) frontend/src/lib/generator.js. No source copied.
import (
    "crypto/sha256"
    "encoding/base64"
    "encoding/hex"
    "encoding/json"
    "errors"
    "fmt"
    "net/url"
    "strconv"
    "strings"
    "unicode/utf8"
)

const maxSubscriptionBody = 1 << 20
const maxSubscriptionNodes = 256

var errSubscriptionInvalid = errors.New("subscription-invalid-or-unsupported")

type subscriptionNode struct {
    Tag string `json:"tag"`
    Name string `json:"name"`
    Protocol string `json:"protocol"`
    // Outbound is a credential-bearing native Xray config. NEVER JSON serialize
    // it through a Control Center response or log.
    outbound map[string]any
}

type subscriptionInspection struct {
    State string `json:"state"`
    Runtime string `json:"runtime"`
    Count int `json:"count"`
    Servers []subscriptionNode `json:"servers"`
    Applied bool `json:"applied"`
}

func stoppedSubscription() subscriptionInspection {
    return subscriptionInspection{State:"STOP",Runtime:"NOT_VERIFIED",Servers:[]subscriptionNode{},Applied:false}
}

func decodedSubscription(data []byte) (string,error) {
    if len(data)==0 || len(data)>maxSubscriptionBody {
        return "",errSubscriptionInvalid
    }
    raw:=strings.TrimSpace(string(data))
    if raw=="" || !utf8.ValidString(raw) {return "",errSubscriptionInvalid}
    if strings.HasPrefix(raw,"vless://") || strings.HasPrefix(raw,"trojan://") {return raw,nil}
    if strings.Contains(raw,"\n") && (strings.HasPrefix(raw,"vless://") || strings.HasPrefix(raw,"trojan://")) {
        return raw,nil
    }
    // Many providers publish standard or unpadded URL-safe base64 share lines.
    encoded:=strings.Map(func(r rune)rune{
        if r=='\r' || r=='\n' || r==' ' || r=='\t' {return -1}
        return r
    },raw)
    if len(encoded)==0 || len(encoded)>maxSubscriptionBody {return "",errSubscriptionInvalid}
    var decoded []byte
    var err error
    for _,codec:=range []*base64.Encoding{base64.StdEncoding,base64.RawStdEncoding,base64.URLEncoding,base64.RawURLEncoding} {
        decoded,err=codec.DecodeString(encoded)
        if err==nil {break}
    }
    if err!=nil || len(decoded)==0 || len(decoded)>maxSubscriptionBody ||
        !utf8.Valid(decoded) {return "",errSubscriptionInvalid}
    result:=strings.TrimSpace(string(decoded))
    if !strings.HasPrefix(result,"vless://") && !strings.HasPrefix(result,"trojan://") {
        return "",errSubscriptionInvalid
    }
    return result,nil
}

func nativeSubscriptionNodes(data []byte) ([]subscriptionNode,error) {
    raw,err:=decodedSubscription(data)
    if err!=nil {return nil,err}
    lines:=strings.Split(raw,"\n")
    if len(lines)>maxSubscriptionNodes {return nil,errSubscriptionInvalid}
    nodes:=make([]subscriptionNode,0,len(lines))
    tags:=make(map[string]bool)
    for _,line:=range lines {
        line=strings.TrimSpace(line)
        if line==""{continue}
        if len(line)>8192{return nil,errSubscriptionInvalid}
        node,err:=nativeSubscriptionLink(line)
        if err!=nil || tags[node.Tag]{return nil,errSubscriptionInvalid}
        tags[node.Tag]=true
        nodes=append(nodes,node)
    }
    if len(nodes)==0{return nil,errSubscriptionInvalid}
    return nodes,nil
}

func nativeSubscriptionLink(link string)(subscriptionNode,error){
    fail:=subscriptionNode{}
    u,err:=url.Parse(link)
    if err!=nil || u.Opaque!="" || u.Hostname()=="" || u.User==nil ||
        u.Fragment=="" && u.Hostname()=="" || u.RawQuery=="" && u.Scheme=="vless" {
        return fail,errSubscriptionInvalid
    }
    if u.Scheme!="vless" && u.Scheme!="trojan" {return fail,errSubscriptionInvalid}
    if _,hasPassword:=u.User.Password();hasPassword {return fail,errSubscriptionInvalid}
    if strings.ContainsAny(u.Hostname(),"\r\n\t ") || strings.Contains(u.Hostname(),"%") {
        return fail,errSubscriptionInvalid
    }
    port,err:=strconv.Atoi(u.Port())
    if err!=nil || port<1 || port>65535{return fail,errSubscriptionInvalid}
    q,err:=url.ParseQuery(u.RawQuery)
    if err!=nil{return fail,errSubscriptionInvalid}
    known:=map[string]bool{
        "security":true,"type":true,"encryption":true,"flow":true,
        "pbk":true,"sid":true,"sni":true,"fp":true,"spx":true,
        "path":true,"host":true,"alpn":true,"serviceName":true,
        "servicename":true,"authority":true,"mode":true,
    }
    for key,values:=range q {
        if !known[key] || len(values)!=1 || len(values[0])>2048 {
            return fail,errSubscriptionInvalid
        }
    }
    encryption:=q.Get("encryption")
    if u.Scheme=="vless" && encryption!="" && encryption!="none" {return fail,errSubscriptionInvalid}
    security:=q.Get("security")
    if security!="reality" && security!="tls" {return fail,errSubscriptionInvalid}
    network:=q.Get("type")
    if network=="" {network="tcp"}
    if network!="tcp" && network!="ws" && network!="grpc" &&
        network!="httpupgrade" && network!="xhttp" {
        return fail,errSubscriptionInvalid
    }
    if (q.Get("flow")!="" && q.Get("flow")!="xtls-rprx-vision") ||
        (q.Get("flow")!="" && u.Scheme!="vless") {
        return fail,errSubscriptionInvalid
    }
    var settings map[string]any
    if u.Scheme=="vless" {
        id:=u.User.Username()
        if !nativeUUID.MatchString(id) || len(id)!=36 {return fail,errSubscriptionInvalid}
        user:=map[string]any{"id":id,"encryption":"none"}
        if flow:=q.Get("flow");flow!="" {user["flow"]=flow}
        settings=map[string]any{"vnext":[]any{map[string]any{
            "address":u.Hostname(),"port":port,"users":[]any{user},
        }}}
    } else {
        password:=u.User.Username()
        if password=="" || len(password)>2048 {return fail,errSubscriptionInvalid}
        settings=map[string]any{"servers":[]any{map[string]any{
            "address":u.Hostname(),"port":port,"password":password,
        }}}
    }
    stream:=map[string]any{"network":network,"security":security}
    if security=="reality" {
        if q.Get("pbk")=="" || q.Get("sni")=="" || q.Get("fp")=="" {
            return fail,errSubscriptionInvalid
        }
        reality:=map[string]any{"publicKey":q.Get("pbk"),"serverName":q.Get("sni"),
            "fingerprint":q.Get("fp")}
        if v:=q.Get("sid");v!=""{reality["shortId"]=v}
        if v:=q.Get("spx");v!=""{reality["spiderX"]=v}
        stream["realitySettings"]=reality
    } else {
        tls:=map[string]any{}
        if v:=q.Get("sni");v!="" {tls["serverName"]=v}
        if v:=q.Get("fp");v!="" {tls["fingerprint"]=v}
        if v:=q.Get("alpn");v!="" {
            tls["alpn"]=strings.Split(v,",")
        }
        stream["tlsSettings"]=tls
    }
    switch network {
    case "ws":
        opts:=map[string]any{"path":"/"}
        if v:=q.Get("path");v!=""{opts["path"]=v}
        if v:=q.Get("host");v!=""{
            opts["headers"]=map[string]string{"Host":v}
        }
        stream["wsSettings"]=opts
    case "grpc":
        opts:=map[string]any{}
        if v:=q.Get("serviceName");v!=""{opts["serviceName"]=v} else if v:=q.Get("servicename");v!=""{opts["serviceName"]=v}
        if v:=q.Get("authority");v!=""{opts["authority"]=v}
        stream["grpcSettings"]=opts
    case "httpupgrade":
        opts:=map[string]any{"path":"/"}
        if v:=q.Get("path");v!=""{opts["path"]=v}
        if v:=q.Get("host");v!=""{opts["host"]=v}
        stream["httpupgradeSettings"]=opts
    case "xhttp":
        opts:=map[string]any{"path":"/"}
        if v:=q.Get("path");v!=""{opts["path"]=v}
        if v:=q.Get("host");v!=""{opts["host"]=v}
        if v:=q.Get("mode");v!=""{opts["mode"]=v}
        stream["xhttpSettings"]=opts
    }
    if q.Get("host")!="" && network=="tcp" {return fail,errSubscriptionInvalid}
    if q.Get("path")!="" && network=="tcp" {return fail,errSubscriptionInvalid}
    if q.Get("mode")!="" && network!="xhttp" {return fail,errSubscriptionInvalid}
    // Stable opaque tag, not a UUID, token, URL or arbitrary provider text.
    digest:=sha256.Sum256([]byte(link))
    tag:="freenet-sub-"+hex.EncodeToString(digest[:8])
    name:=displayXrayTag(u.Fragment,u.Hostname())
    return subscriptionNode{Tag:tag,Name:name,Protocol:u.Scheme,
        outbound:map[string]any{
            "tag":tag,"protocol":u.Scheme,"settings":settings,
            "streamSettings":stream,
        }},nil
}

func nativeSubscriptionCandidate(nodes []subscriptionNode) ([]byte,error){
    if len(nodes)==0 || len(nodes)>maxSubscriptionNodes {return nil,errSubscriptionInvalid}
    out:=make([]map[string]any,0,len(nodes))
    for _,node:=range nodes {
        if node.outbound==nil{return nil,errSubscriptionInvalid}
        out=append(out,node.outbound)
    }
    candidate,err:=json.MarshalIndent(map[string]any{"outbounds":out},"","  ")
    if err!=nil{return nil,fmt.Errorf("native candidate serialization failed")}
    return candidate,nil
}

func subscriptionPreviewFromBody(data []byte) subscriptionInspection {
    fail:=stoppedSubscription()
    nodes,err:=nativeSubscriptionNodes(data)
    if err!=nil{return fail}
    if _,err=nativeSubscriptionCandidate(nodes);err!=nil{return fail}
    // Unexported 'outbound' fields are stripped by encoding/json. The preview
    // NEVER persists a token or writes to /opt/etc/xray/configs.
    return subscriptionInspection{State:"PREVIEW_ONLY",Runtime:"NOT_VERIFIED",
        Count:len(nodes),Servers:nodes,Applied:false}
}
