package main

// Native Xray configuration is the sole source for this CONFIG_ONLY view.
// XKeen/Xray own all network operations; this code does not invoke either CLI.
import (
    "bytes"
    "encoding/json"
    "errors"
    "io"
    "os"
    "path/filepath"
    "regexp"
    "strings"
)

const nativeConfigMaxBytes int64 = 2 << 20

type nativeServer struct {
    Tag string `json:"tag"`
    Protocol string `json:"protocol"`
    InBalancer bool `json:"in_balancer"`
}

type nativeBalancer struct {
    Tag string `json:"tag"`
    Strategy string `json:"strategy"`
    Selectors int `json:"selectors"`
    Servers int `json:"servers"`
}

type nativeTopology struct {
    State string `json:"state"`
    Runtime string `json:"runtime"`
    Reason string `json:"reason,omitempty"`
    Servers []nativeServer `json:"servers"`
    Balancers []nativeBalancer `json:"balancers"`
}

func stoppedTopology() nativeTopology {
    return nativeTopology{
        State:"STOP", Runtime:"NOT_VERIFIED",
        Reason:"native-xray-config-unavailable-or-contradictory",
        Servers:[]nativeServer{}, Balancers:[]nativeBalancer{},
    }
}

// The labels are Xray's own tags, not rewritten location names. IPs and DNS
// hostnames are valid labels. Only obviously credential-bearing tags are
// withheld from display, never used as a basis for changing route semantics.
var nativeUUID = regexp.MustCompile(`(?i)[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)

func displayXrayTag(tag string, fallback string) string {
    trimmed := strings.TrimSpace(tag)
    low := strings.ToLower(trimmed)
    if trimmed == "" || len(trimmed)>256 ||
        nativeUUID.MatchString(trimmed) ||
        strings.Contains(low,"://") ||
        strings.Contains(low,"password=") ||
        strings.Contains(low,"token=") ||
        strings.Contains(low,"privatekey") ||
        strings.Contains(low,"shortid=") ||
        strings.ContainsAny(trimmed,"\r\n\x00<>@?") {
        return fallback
    }
    return trimmed
}

// Official XKeen accepts JSONC in Xray configuration files. Keep the lexer
// small and conservative: slash sequences within JSON strings are untouched.
func stripXrayComments(raw []byte) ([]byte,error) {
    out:=make([]byte,0,len(raw))
    inString,escaped,line,block:=false,false,false,false
    for i:=0;i<len(raw);i++ {
        c:=raw[i]
        if line {
            if c=='\n' { line=false; out=append(out,c) }
            continue
        }
        if block {
            if c=='*' && i+1<len(raw) && raw[i+1]=='/' {block=false;i++;continue}
            if c=='\n' { out=append(out,c) }
            continue
        }
        if inString {
            out=append(out,c)
            if escaped { escaped=false } else if c=='\\' {escaped=true} else if c=='"' {inString=false}
            continue
        }
        if c=='"' { inString=true; out=append(out,c);continue }
        if c=='/' && i+1<len(raw) {
            if raw[i+1]=='/' {line=true;i++;continue}
            if raw[i+1]=='*' {block=true;i++;continue}
        }
        out=append(out,c)
    }
    if block || inString {return nil,errors.New("unterminated Xray JSONC")}
    if !json.Valid(out) {return nil,errors.New("invalid Xray JSONC")}
    return out,nil
}

func nativeConfig(root,name string) ([]byte,error) {
    base:=filepath.Join(root,"etc","xray","configs")
    if observedPath(root,"etc/xray/configs",true)!="PRESENT" {return nil,errors.New("native config directory unavailable")}
    if observedPath(root,filepath.Join("etc","xray","configs",name),false)!="PRESENT" {
        return nil,errors.New("native config unavailable")
    }
    file:=filepath.Join(base,name)
    before,err:=os.Lstat(file)
    if err!=nil || !before.Mode().IsRegular() || before.Size()<2 || before.Size()>nativeConfigMaxBytes {
        return nil,errors.New("native config invalid size or type")
    }
    fd,err:=os.Open(file)
    if err!=nil {return nil,errors.New("native config unreadable")}
    defer fd.Close()
    opened,err:=fd.Stat()
    if err!=nil || !os.SameFile(before,opened) {return nil,errors.New("native config changed during open")}
    content,err:=io.ReadAll(io.LimitReader(fd,nativeConfigMaxBytes+1))
    if err!=nil || int64(len(content))>nativeConfigMaxBytes {
        return nil,errors.New("native config read limit")
    }
    after,err:=os.Lstat(file)
    if err!=nil || !os.SameFile(before,after) || !after.ModTime().Equal(before.ModTime()) || after.Size()!=int64(len(content)) {
        return nil,errors.New("native config changed during read")
    }
    return stripXrayComments(content)
}

type outboundConfig struct {
    Tag string `json:"tag"`
    Protocol string `json:"protocol"`
}
type balancerConfig struct {
    Tag string `json:"tag"`
    Selector []string `json:"selector"`
    Strategy struct {Type string `json:"type"`} `json:"strategy"`
}

func nativeXrayTopology(root string) (nativeTopology,error) {
    failed:=stoppedTopology()
    outRaw,err:=nativeConfig(root,"04_outbounds.json")
    if err!=nil{return failed,err}
    routeRaw,err:=nativeConfig(root,"05_routing.json")
    if err!=nil{return failed,err}

    var doc struct {
        Outbounds []outboundConfig `json:"outbounds"`
    }
    var routingDoc struct {
        Routing *struct {
            Balancers []balancerConfig `json:"balancers"`
        } `json:"routing"`
    }
    if json.Unmarshal(outRaw,&doc)!=nil || doc.Outbounds==nil ||
        json.Unmarshal(routeRaw,&routingDoc)!=nil || routingDoc.Routing==nil {
        return failed,errors.New("required Xray config sections absent")
    }
    result:=nativeTopology{
        State:"CONFIG_ONLY", Runtime:"NOT_VERIFIED",
        Servers:[]nativeServer{}, Balancers:[]nativeBalancer{},
    }
    tags:=make(map[string]bool,len(doc.Outbounds))
    for i,o:=range doc.Outbounds {
        if o.Tag==""||o.Protocol==""||len(o.Tag)>512||len(o.Protocol)>80||
            tags[o.Tag]||strings.ContainsAny(o.Tag,"\r\n\x00"){
            return failed,errors.New("native outbound tag/protocol contradictory")
        }
        tags[o.Tag]=true
        result.Servers=append(result.Servers,nativeServer{
            Tag:displayXrayTag(o.Tag,"Сервер "+itoaNative(i+1)),
            Protocol:strings.ToLower(o.Protocol),
        })
    }
    balancerTags:=make(map[string]bool,len(routingDoc.Routing.Balancers))
    for i,b:=range routingDoc.Routing.Balancers {
        if b.Tag==""||balancerTags[b.Tag]||len(b.Tag)>512||len(b.Selector)==0{
            return failed,errors.New("native balancer tag/selector contradictory")
        }
        balancerTags[b.Tag]=true
        for _,prefix:=range b.Selector {
            if prefix==""||strings.ContainsAny(prefix,"\r\n\x00") {
                return failed,errors.New("native selector invalid")
            }
        }
        count:=0
        for k,o:=range doc.Outbounds {
            switch strings.ToLower(o.Protocol) {
            case "freedom","blackhole","dns","loopback": continue
            }
            for _,prefix:=range b.Selector {
                if strings.HasPrefix(o.Tag,prefix) {
                    count++
                    result.Servers[k].InBalancer=true
                    break
                }
            }
        }
        strategy:=b.Strategy.Type
        if strategy=="" {strategy="not-specified"}
        result.Balancers=append(result.Balancers,nativeBalancer{
            Tag:displayXrayTag(b.Tag,"Балансировщик "+itoaNative(i+1)),
            Strategy:strategy, Selectors:len(b.Selector), Servers:count,
        })
    }
    if len(result.Balancers)==0 { result.State="NO_BALANCER" }
    // Never return the raw config or its settings: they may contain VLESS UUID,
    // Reality keys, subscription endpoints or sensitive server addresses.
    return result,nil
}

// strconv is only used to make explicit, stable display fallbacks.
func itoaNative(i int) string {
    const digits="0123456789"
    if i==0 {return "0"}
    var buf [20]byte
    n:=len(buf)
    for i>0 {n--;buf[n]=digits[i%10];i/=10}
    return string(buf[n:])
}

// Guard against accidental embedding of raw settings in template/API fields.
func (t nativeTopology) validSummary() bool {
    return t.Runtime=="NOT_VERIFIED" &&
        (t.State=="CONFIG_ONLY"||t.State=="NO_BALANCER"||t.State=="STOP") &&
        !bytes.Contains([]byte(t.Reason),[]byte("password"))
}
