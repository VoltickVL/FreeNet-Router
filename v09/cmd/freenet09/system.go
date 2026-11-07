package main

// The dashboard only reads official Entware/XKeen/Xray state. A process or
// config file is evidence, NEVER proof of VPN egress or healthy DNS routing.
import (
    "encoding/json"
    "os"
    "path/filepath"
    "regexp"
    "strings"
    "time"
)

type nativeConfigSummary struct {
    State string `json:"state"`
    Count int `json:"count"`
    Mode string `json:"mode"`
}

type nativeGeoData struct {
    State string `json:"state"`
    Files int `json:"files"`
    LatestUTC string `json:"latest_utc,omitempty"`
}

type nativeSystem struct {
    Evidence string `json:"evidence"`
    VPN string `json:"vpn"`
    XrayProcess string `json:"xray_process"`
    XKeenInit string `json:"xkeen_init"`
    XKeenConfig string `json:"xkeen_config"`
    DNS nativeConfigSummary `json:"dns"`
    Routing nativeConfigSummary `json:"routing"`
    GeoData nativeGeoData `json:"geodata"`
    XKeenGeoCron string `json:"xkeen_geo_cron"`
    XKeenBalancerCron string `json:"xkeen_balancer_cron"`
}

var xkeenGeoJob = regexp.MustCompile(`(?:^|[ /])xkeen[[:space:]]+-ug(?:[[:space:]]|$)`)
var xkeenSpeedJob = regexp.MustCompile(`(?:^|[ /])xkeen[[:space:]]+-sbt(?:[[:space:]]|$)`)

func safeConfigMode(input, fallback string, allowed ...string) string {
    for _, value := range allowed {
        if input == value { return input }
    }
    return fallback
}

func dnsSummary(root string) nativeConfigSummary {
    fail:=nativeConfigSummary{State:"UNKNOWN",Mode:"NOT_VERIFIED"}
    // XKeen 2.2 Beta upstream uses 02_dns.json, not 03_dns.json
    // (03_inbounds.json). Never derive this path from legacy FreeNet.
    if observedPath(root,"etc/xray/configs/02_dns.json",false)=="MISSING" {
        fail.State="MISSING"
        return fail
    }
    raw,err:=nativeConfig(root,"02_dns.json")
    if err!=nil{return fail}
    var doc struct {
        DNS *struct {
            Servers []json.RawMessage `json:"servers"`
            QueryStrategy string `json:"queryStrategy"`
        } `json:"dns"`
    }
    if json.Unmarshal(raw,&doc)!=nil {return fail}
    if doc.DNS==nil {
        // Original XKeen ships a JSONC 02_dns.json with an empty object:
        // Xray DNS is not configured here. This is not missing or broken.
        var empty map[string]json.RawMessage
        if json.Unmarshal(raw,&empty)==nil && empty!=nil && len(empty)==0 {
            return nativeConfigSummary{State:"NATIVE_DEFAULT",Count:0,Mode:"not-configured"}
        }
        return fail
    }
    if len(doc.DNS.Servers)>256 {return fail}
    state:="CONFIG_ONLY"
    for _,rawServer:=range doc.DNS.Servers {
        // Inspect tag *only*; never return resolver URL, credential or rule.
        var entry struct { Tag string `json:"tag"` }
        if json.Unmarshal(rawServer,&entry)==nil &&
            (entry.Tag=="dns-direct" || entry.Tag=="dns-vless") {
            state="LEGACY_SPLIT_REVIEW_REQUIRED"
        }
    }
    return nativeConfigSummary{
        State:state,Count:len(doc.DNS.Servers),
        Mode:safeConfigMode(doc.DNS.QueryStrategy,"not-specified","UseIP","UseIPv4","UseIPv6"),
    }
}

func routingSummary(root string) nativeConfigSummary {
    fail:=nativeConfigSummary{State:"UNKNOWN",Mode:"NOT_VERIFIED"}
    raw,err:=nativeConfig(root,"05_routing.json")
    if err!=nil{
        if observedPath(root,"etc/xray/configs/05_routing.json",false)=="MISSING" {
            fail.State="MISSING"
        }
        return fail
    }
    var doc struct {
        Routing *struct {
            Rules []json.RawMessage `json:"rules"`
            DomainStrategy string `json:"domainStrategy"`
        } `json:"routing"`
    }
    if json.Unmarshal(raw,&doc)!=nil || doc.Routing==nil{return fail}
    if len(doc.Routing.Rules)>50000 {return fail}
    return nativeConfigSummary{
        State:"CONFIG_ONLY",Count:len(doc.Routing.Rules),
        Mode:safeConfigMode(doc.Routing.DomainStrategy,"not-specified","AsIs","IPIfNonMatch","IPOnDemand"),
    }
}

func geodataSummary(root string) nativeGeoData {
    rel:="etc/xray/dat"
    state:=observedPath(root,rel,true)
    result:=nativeGeoData{State:state}
    if state!="PRESENT" {return result}
    path:=filepath.Join(root,rel)
    entries,err:=os.ReadDir(path)
    if err!=nil {result.State="UNKNOWN";return result}
    var latest time.Time
    for _,entry:=range entries {
        if !strings.HasSuffix(strings.ToLower(entry.Name()),".dat"){continue}
        // Do not follow symlinked geodata into external directories.
        info,err:=os.Lstat(filepath.Join(path,entry.Name()))
        if err!=nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink!=0 {
            result.State="UNKNOWN"
            result.Files=0
            return result
        }
        result.Files++
        if info.ModTime().After(latest){latest=info.ModTime()}
    }
    if result.Files==0{result.State="MISSING";return result}
    if !latest.IsZero(){result.LatestUTC=latest.UTC().Format(time.RFC3339)}
    // "PRESENT" is not the same as "UP_TO_DATE".
    return result
}

func nativeCron(root string) (string,string) {
    rel:="var/spool/cron/crontabs/root"
    state:=observedPath(root,rel,false)
    if state=="MISSING"{return "NOT_CONFIGURED","NOT_CONFIGURED"}
    if state!="PRESENT"{return "UNKNOWN","UNKNOWN"}
    path:=filepath.Join(root,rel)
    before,err:=os.Lstat(path)
    if err!=nil || !before.Mode().IsRegular() || before.Size()>128*1024 {
        return "UNKNOWN","UNKNOWN"
    }
    raw,err:=os.ReadFile(path)
    if err!=nil || int64(len(raw))>128*1024{return "UNKNOWN","UNKNOWN"}
    after,err:=os.Lstat(path)
    if err!=nil || !os.SameFile(before,after) || before.Size()!=after.Size() ||
        !before.ModTime().Equal(after.ModTime()) {
        return "UNKNOWN","UNKNOWN"
    }
    geo,speed:="NOT_CONFIGURED","NOT_CONFIGURED"
    for _,line:=range strings.Split(string(raw),"\n") {
        trimmed:=strings.TrimSpace(line)
        if trimmed=="" || strings.HasPrefix(trimmed,"#"){continue}
        if xkeenGeoJob.MatchString(trimmed){geo="CONFIGURED"}
        if xkeenSpeedJob.MatchString(trimmed){speed="CONFIGURED"}
    }
    // Do not return raw cron text: it may include private parameters.
    return geo,speed
}

func xrayProcessEvidence(procRoot string) string {
    entries,err:=os.ReadDir(procRoot)
    if err!=nil {return "UNKNOWN"}
    for _,entry:=range entries {
        if !entry.IsDir(){continue}
        name:=entry.Name()
        if name=="" || strings.Trim(name,"0123456789")!="" {continue}
        comm,err:=os.ReadFile(filepath.Join(procRoot,name,"comm"))
        if err!=nil{continue}
        if strings.TrimSpace(string(comm))=="xray"{
            // CLI and daemon share the name; cannot certify runtime/egress.
            return "PROCESS_DETECTED"
        }
    }
    return "NOT_DETECTED"
}

func nativeSystemStatus(root,procRoot string) nativeSystem {
    geo,speed:=nativeCron(root)
    return nativeSystem{
        Evidence:"READ_ONLY",VPN:"NOT_VERIFIED",
        XrayProcess:xrayProcessEvidence(procRoot),
        XKeenInit:observedPath(root,"etc/init.d/S05xkeen",false),
        XKeenConfig:observedPath(root,"etc/xkeen/xkeen.json",false),
        DNS:dnsSummary(root),Routing:routingSummary(root),
        GeoData:geodataSummary(root),
        XKeenGeoCron:geo,XKeenBalancerCron:speed,
    }
}
