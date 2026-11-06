package main

// Bind subscription nodes only to selectors that already exist in native
// Xray routing. FreeNet does not create balancers, routes or a VPN engine.
import (
    "encoding/json"
    "errors"
    "sort"
    "strings"
)

type nativeRouteTarget struct {
    Balancer string `json:"balancer"`
    Selector string `json:"selector"`
}
type nativeRouteTargets struct {
    State string `json:"state"`
    Targets []nativeRouteTarget `json:"targets"`
}

var errNativeRouteNotReady=errors.New("native-routing-selector-unavailable")

func nativeSubscriptionRouteTargets(files map[string][]byte)([]nativeRouteTarget,error){
    raw,ok:=files["05_routing.json"]
    if !ok {return nil,errNativeRouteNotReady}
    clean,err:=stripXrayComments(raw)
    if err!=nil{return nil,errNativeRouteNotReady}
    var doc struct {
        Routing *struct {
            Script string `json:"script"`
            Rules []struct {
                BalancerTag string `json:"balancerTag"`
                OutboundTag string `json:"outboundTag"`
            } `json:"rules"`
            Balancers []struct {
                Tag string `json:"tag"`
                Selector []string `json:"selector"`
            } `json:"balancers"`
        } `json:"routing"`
    }
    if json.Unmarshal(clean,&doc)!=nil || doc.Routing==nil || doc.Routing.Script!="" {
        return nil,errNativeRouteNotReady
    }
    referenced:=make(map[string]bool)
    for _,rule:=range doc.Routing.Rules {
        // Xray gives outboundTag precedence if both were specified.
        if rule.BalancerTag!="" && rule.OutboundTag=="" {referenced[rule.BalancerTag]=true}
    }
    options:=make([]nativeRouteTarget,0)
    seen:=make(map[string]bool)
    for _,balancer:=range doc.Routing.Balancers {
        if !referenced[balancer.Tag] || balancer.Tag=="" ||
            len(balancer.Tag)>160 || displayXrayTag(balancer.Tag,"")!=balancer.Tag {
            continue
        }
        for _,selector:=range balancer.Selector {
            if selector=="" || len(selector)>96 ||
                displayXrayTag(selector,"")!=selector ||
                strings.TrimSpace(selector)!=selector {continue}
            key:=balancer.Tag+"\x00"+selector
            if seen[key] {continue}
            seen[key]=true
            options=append(options,nativeRouteTarget{Balancer:balancer.Tag,Selector:selector})
        }
    }
    if len(options)==0 || len(options)>64 {return nil,errNativeRouteNotReady}
    sort.Slice(options,func(i,j int)bool{
        if options[i].Balancer==options[j].Balancer {
            return options[i].Selector<options[j].Selector
        }
        return options[i].Balancer<options[j].Balancer
    })
    return options,nil
}

func nativeRouteOptionsReport(root string)nativeRouteTargets{
    stop:=nativeRouteTargets{State:"STOP",Targets:[]nativeRouteTarget{}}
    if observedPath(root,"etc/init.d/S99freenet-ui",false)!="MISSING" {return stop}
    files,err:=readNativeConfdir(root)
    if err!=nil{return stop}
    options,err:=nativeSubscriptionRouteTargets(files)
    if err!=nil{return stop}
    return nativeRouteTargets{State:"CONFIG_ONLY",Targets:options}
}

func chooseNativeRouteTarget(files map[string][]byte,request nativeRouteTarget)(nativeRouteTarget,error){
    options,err:=nativeSubscriptionRouteTargets(files)
    if err!=nil{return nativeRouteTarget{},err}
    if request.Balancer=="" && request.Selector=="" && len(options)==1 {
        return options[0],nil
    }
    for _,option:=range options {
        if option==request{return option,nil}
    }
    return nativeRouteTarget{},errNativeRouteNotReady
}

func bindSubscriptionNodesToNativeSelector(nodes []subscriptionNode,target nativeRouteTarget)([]subscriptionNode,error){
    if target.Balancer=="" || target.Selector=="" || len(nodes)==0 {
        return nil,errNativeRouteNotReady
    }
    updated:=make([]subscriptionNode,0,len(nodes))
    seen:=make(map[string]bool)
    for _,node:=range nodes {
        if !strings.HasPrefix(node.Tag,"freenet-sub-") || node.outbound==nil {
            return nil,errNativeRouteNotReady
        }
        digest:=strings.TrimPrefix(node.Tag,"freenet-sub-")
        if len(digest)!=16{return nil,errNativeRouteNotReady}
        tag:=target.Selector+"freenet-"+digest
        if len(tag)>512 || displayXrayTag(tag,"")!=tag || seen[tag] {
            return nil,errNativeRouteNotReady
        }
        seen[tag]=true
        node.Tag=tag
        node.outbound["tag"]=tag
        updated=append(updated,node)
    }
    return updated,nil
}
