package main

import (
    "bytes"
    "context"
    "encoding/json"
    "errors"
    "net/http"
    "net/http/httptest"
    "os"
    "path/filepath"
    "strings"
    "testing"
)

func TestSubscriptionRouteTargetsRequireActualNativeRuleAndSelector(t *testing.T) {
    good:=map[string][]byte{
        "05_routing.json":[]byte(`{
            // Direct is preserved and still routes to original-proxy.
            "routing":{"rules":[
                {"domain":["domain:direct.example"],"outboundTag":"original-proxy"},
                {"balancerTag":"balancer"}
            ],"balancers":[{"tag":"balancer","selector":["sub-"],"strategy":{"type":"leastPing"}}]}
        }`),
    }
    options,err:=nativeSubscriptionRouteTargets(good)
    if err!=nil || len(options)!=1 || options[0].Balancer!="balancer" ||
        options[0].Selector!="sub-" {t.Fatalf("wrong native routing binding: %+v %v",options,err)}
    selected,err:=chooseNativeRouteTarget(good,nativeRouteTarget{})
    if err!=nil || selected!=options[0]{t.Fatalf("single native balancer should auto select: %+v %v",selected,err)}
    none:=map[string][]byte{"05_routing.json":[]byte(
        `{"routing":{"rules":[{"outboundTag":"direct"}],"balancers":[{"tag":"balancer","selector":["sub-"]}]}}`)}
    if _,err:=nativeSubscriptionRouteTargets(none);err==nil {
        t.Fatal("unreferenced Xray balancer must not be presented as active routing")
    }
    mixed:=map[string][]byte{"05_routing.json":[]byte(
        `{"routing":{"rules":[{"outboundTag":"direct","balancerTag":"balancer"}],"balancers":[{"tag":"balancer","selector":["sub-"]}]}}`)}
    if _,err:=nativeSubscriptionRouteTargets(mixed);err==nil{
        t.Fatal("Xray outboundTag wins over balancerTag; must not claim balancer is referenced")
    }
    script:=map[string][]byte{"05_routing.json":[]byte(
        `{"routing":{"script":"custom.lua","rules":[{"balancerTag":"balancer"}],"balancers":[{"tag":"balancer","selector":["sub-"]}]}}`)}
    if _,err:=nativeSubscriptionRouteTargets(script);err==nil {
        t.Fatal("custom native script may override routing: STOP")
    }
}

func TestSubscriptionSupportsExplicitNativeBalancerWithoutTouchingRouting(t *testing.T) {
    root,proc:=nativeApplyFixture(t,mockXraySuccess,mockXKeenSuccess)
    routingPath:=filepath.Join(root,"etc/xray/configs/05_routing.json")
    original,err:=os.ReadFile(routingPath)
    if err!=nil{t.Fatal(err)}
    writeFixture(t,root,"etc/xray/configs/05_routing.json",
        `{"routing":{"rules":[{"balancerTag":"first"},{"balancerTag":"second"}],"balancers":[{"tag":"first","selector":["proxy-"]},{"tag":"second","selector":["sub-"]}]}}`)
    current,err:=os.ReadFile(routingPath)
    if err!=nil{t.Fatal(err)}
    _=original
    files,err:=readNativeConfdir(root)
    if err!=nil{t.Fatal(err)}
    choices,err:=nativeSubscriptionRouteTargets(files)
    if err!=nil || len(choices)!=2 {t.Fatalf("must offer two real native balancers: %+v %v",choices,err)}
    if _,err:=chooseNativeRouteTarget(files,nativeRouteTarget{});err==nil{
        t.Fatal("ambiguous balancer must not be silently chosen")
    }
    bad:=subscriptionOperationWithRoute(context.Background(),root,proc,nativeURLCanary,false,
        nativeRouteTarget{},fakeSubscriptionFetch)
    if bad.Applied || bad.State!="STOP"{
        t.Fatalf("ambiguous native routing incorrectly applied: %+v",bad)
    }
    if _,err:=os.Stat(filepath.Join(root,"etc/xray/configs",subscriptionFragmentName));!errors.Is(err,os.ErrNotExist){
        t.Fatal("unselected routing changed Xray config")
    }
    chosen:=nativeRouteTarget{Balancer:"second",Selector:"sub-"}
    good:=subscriptionOperationWithRoute(context.Background(),root,proc,nativeURLCanary,false,
        chosen,fakeSubscriptionFetch)
    if good.State!="CONFIG_APPLIED_NOT_EGRESS_VERIFIED" || good.Balancer!="second" ||
        good.Selector!="sub-" || !good.Applied{
        t.Fatalf("explicit native balancer not used: %+v",good)
    }
    fragment,err:=os.ReadFile(filepath.Join(root,"etc/xray/configs",subscriptionFragmentName))
    if err!=nil || !bytes.Contains(fragment,[]byte("sub-freenet-")) {
        t.Fatalf("generated native Xray tags do not match selected selector")
    }
    if after,err:=os.ReadFile(routingPath);err!=nil || !bytes.Equal(current,after){
        t.Fatal("FreeNet modified native 05_routing.json")
    }
    status:=nativeSubscriptionStatus(root)
    if !status.Applied || status.Balancer!="second" || status.Selector!="sub-" {
        t.Fatalf("private receipt did not preserve original Xray balancer: %+v",status)
    }
    refresh:=subscriptionOperationWithRoute(context.Background(),root,proc,"",true,
        nativeRouteTarget{},fakeSubscriptionFetch)
    if !refresh.Applied || refresh.Balancer!="second" || refresh.Selector!="sub-" {
        t.Fatalf("refresh lost existing native balancer choice: %+v",refresh)
    }
    // A later routing edit invalidates our selector. Never auto-rebind to
    // another provider/ISP route and never silently change native rules.
    writeFixture(t,root,"etc/xray/configs/05_routing.json",
        `{"routing":{"rules":[{"balancerTag":"first"}],"balancers":[{"tag":"first","selector":["proxy-"]}]}}`)
    drift:=subscriptionOperationWithRoute(context.Background(),root,proc,"",true,
        nativeRouteTarget{},fakeSubscriptionFetch)
    if drift.State!="STOP" || drift.Applied {
        t.Fatalf("routing drift incorrectly triggered subscription restart: %+v",drift)
    }
    if st:=nativeSubscriptionStatus(root);st.State!="STOP" || st.Applied {
        t.Fatalf("routing drift shown as applied: %+v",st)
    }
}

func TestNativeSelectorHTTPNotAvailableAnonymously(t *testing.T) {
    root,_,auth:=newTestAuth(t)
    secure:=secureControlCenter(root,t.TempDir(),auth)
    rec:=httptest.NewRecorder()
    secure.ServeHTTP(rec,httptest.NewRequest(http.MethodGet,
        "https://router.example/api/subscription/targets",nil))
    if rec.Code!=http.StatusUnauthorized {
        t.Fatalf("native selector inventory exposed without auth: %d",rec.Code)
    }
    preview:=handlersWithProc(root,t.TempDir())
    unknown:=httptest.NewRecorder()
    preview.ServeHTTP(unknown,httptest.NewRequest(http.MethodGet,
        "http://127.0.0.1:1009/api/subscription/targets",nil))
    if unknown.Code==http.StatusOK {
        t.Fatal("anonymous local preview should not expose native apply selector choices")
    }
}

func TestSubscribedNodesUseOnlySelectedNativePrefix(t *testing.T) {
    nodes,err:=nativeSubscriptionNodes([]byte(fakeVLESSReality+"\n"+fakeVLESSTLS))
    if err!=nil{t.Fatal(err)}
    selected:=nativeRouteTarget{Balancer:"balancer",Selector:"proxy-"}
    result,err:=bindSubscriptionNodesToNativeSelector(nodes,selected)
    if err!=nil || len(result)!=2 {t.Fatalf("binding failed: %v",err)}
    for _,n:=range result {
        if !strings.HasPrefix(n.Tag,"proxy-freenet-") ||
            n.outbound["tag"]!=n.Tag || strings.HasPrefix(n.Tag,"freenet-sub-"){
            t.Fatalf("subscription tag not linked to native Xray selector: %s",n.Tag)
        }
    }
    raw,err:=nativeSubscriptionCandidate(result)
    if err!=nil{t.Fatal(err)}
    var parsed struct{Outbounds []struct{Tag string `json:"tag"`} `json:"outbounds"`}
    if json.Unmarshal(raw,&parsed)!=nil || len(parsed.Outbounds)!=2 ||
        !strings.HasPrefix(parsed.Outbounds[0].Tag,selected.Selector) {
        t.Fatal("official Xray outbound candidate lost native selector tags")
    }
    if _,err:=bindSubscriptionNodesToNativeSelector(nodes,nativeRouteTarget{Balancer:"b"});err==nil{
        t.Fatal("empty selector must fail closed")
    }
}
