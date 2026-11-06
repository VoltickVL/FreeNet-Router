package main

import (
    "bytes"
    "encoding/base64"
    "encoding/json"
    "net/netip"
    "net/http"
    "net/http/httptest"
    "strings"
    "testing"
)

const fakeVLESSReality = "vless://01234567-89ab-4cde-8012-0123456789ab@node.example.net:443?encryption=none&security=reality&sni=cloud.example.net&fp=chrome&pbk=PUBLIC-KEY-TEST&sid=a1b2&type=tcp&flow=xtls-rprx-vision#Germany%20Node"
const fakeVLESSTLS = "vless://01234567-89ab-4cde-8012-0123456789ab@edge.example.net:8443?encryption=none&security=tls&sni=edge.example.net&type=ws&path=%2Fws&host=edge.example.net#Netherlands"
const fakeTrojanTLS = "trojan://SUPER-SECRET-TEST@trojan.example.net:443?security=tls&sni=trojan.example.net&type=grpc&servicename=grpc123#London"

func TestNativeSubscriptionParsesRealFormatsToOnlyXrayOutbounds(t *testing.T){
    links:=fakeVLESSReality+"\n"+fakeVLESSTLS+"\n"+fakeTrojanTLS+"\n"
    for _,payload:=range [][]byte{
        []byte(links),
        []byte(base64.StdEncoding.EncodeToString([]byte(links))),
        []byte(base64.RawURLEncoding.EncodeToString([]byte(links))),
    } {
        nodes,err:=nativeSubscriptionNodes(payload)
        if err!=nil || len(nodes)!=3 {t.Fatalf("valid native subscription refused: err=%v count=%d",err,len(nodes))}
        if nodes[0].Name!="Germany Node" || nodes[1].Name!="Netherlands" ||
            nodes[2].Name!="London" || nodes[0].Tag==nodes[1].Tag {
            t.Fatalf("metadata not stable: %s %s",nodes[0].Name,nodes[1].Name)
        }
        candidate,err:=nativeSubscriptionCandidate(nodes)
        if err!=nil{t.Fatal(err)}
        var doc struct { Outbounds []struct {
            Tag string `json:"tag"`
            Protocol string `json:"protocol"`
            Settings json.RawMessage `json:"settings"`
            StreamSettings json.RawMessage `json:"streamSettings"`
        } `json:"outbounds"` }
        if err:=json.Unmarshal(candidate,&doc);err!=nil {t.Fatal(err)}
        if len(doc.Outbounds)!=3 || doc.Outbounds[0].Protocol!="vless" ||
            doc.Outbounds[2].Protocol!="trojan" {
            t.Fatal("candidate not a standard Xray outbound fragment")
        }
        if !bytes.Contains(candidate,[]byte("01234567-89ab-4cde-8012-0123456789ab")) ||
            !bytes.Contains(candidate,[]byte("PUBLIC-KEY-TEST")) ||
            !bytes.Contains(candidate,[]byte("xtls-rprx-vision")) ||
            !bytes.Contains(candidate,[]byte("wsSettings")) ||
            !bytes.Contains(candidate,[]byte("grpcSettings")) {
            t.Fatal("official Xray fields lost during conversion")
        }
        preview:=subscriptionPreviewFromBody(payload)
        if preview.State!="PREVIEW_ONLY" || preview.Count!=3 ||
            preview.Runtime!="NOT_VERIFIED" || preview.Applied ||
            len(preview.Servers)!=3 {
            t.Fatalf("preview incorrectly claims native apply: %+v",preview)
        }
        encoded,err:=json.Marshal(preview)
        if err!=nil {t.Fatal(err)}
        for _,secret:=range []string{"01234567-89ab-4cde-8012-0123456789ab",
            "PUBLIC-KEY-TEST","SUPER-SECRET-TEST","cloud.example.net","node.example.net"} {
            if bytes.Contains(encoded,[]byte(secret)) {
                t.Fatalf("credential-bearing native settings escaped into browser preview: %q",secret)
            }
        }
    }
}

func TestSubscriptionFailsClosedOnUnsupportedOrPartialFormats(t *testing.T){
    invalid:=[]string{
        "",
        "vmess://unsupported",
        "vless://bad-uuid@node.example.net:443?security=reality&pbk=key&sni=ok&fp=chrome#test",
        "vless://01234567-89ab-4cde-8012-0123456789ab@node.example.net:443?security=none#no-tls",
        fakeVLESSReality+"\n"+"vmess://bad",
        fakeVLESSReality+"\n"+fakeVLESSReality,
        strings.Replace(fakeVLESSReality,"type=tcp","type=invalid",1),
        strings.Replace(fakeVLESSReality,"security=reality","security=unknown",1),
        strings.Replace(fakeVLESSReality,"pbk=PUBLIC-KEY-TEST","pbk=",1),
        strings.Replace(fakeVLESSReality,"&fp=chrome","",1),
        strings.Replace(fakeVLESSReality,"&flow=xtls-rprx-vision","&flow=unknown",1),
        strings.Replace(fakeVLESSReality,"&sid=a1b2","&sid=a1b2&sid=a1b2",1),
        strings.Replace(fakeVLESSReality,"&sid=a1b2","&made-up-key=123",1),
        strings.Repeat(fakeVLESSReality+"\n",maxSubscriptionNodes+1),
        strings.Repeat("x",maxSubscriptionBody+1),
    }
    for _,item:=range invalid {
        nodes,err:=nativeSubscriptionNodes([]byte(item))
        if err==nil || len(nodes)!=0 {t.Fatalf("partial/unsupported subscription accepted")}
        result:=subscriptionPreviewFromBody([]byte(item))
        if result.State!="STOP" || result.Applied || result.Count!=0 || len(result.Servers)!=0 {
            t.Fatal("partial subscription may not be applied or shown as successful")
        }
    }
}

func TestSubscriptionPublicURLAndSSRFGuard(t *testing.T) {
    safe:=[]string{"https://subscription.example.org/api/v1?token=SECRET-TEST",
        "https://node.example.org:8443/sub/ABC"}
    for _,value:=range safe{
        if _,err:=validateSubscriptionURL(value);err!=nil{
            t.Fatalf("valid HTTPS subscription URL rejected")
        }
    }
    bad:=[]string{
        "http://provider.example/secret",
        "https://127.0.0.1/secret",
        "https://192.168.50.1/secret",
        "https://[::1]/secret",
        "https://user:pass@provider.example/secret",
        "https://provider.local/secret",
        "https://provider.internal/secret",
        "https://provider.example:2222/sub",
        "https://provider.example/sub#token",
        "file:///opt/etc/xray/configs/04_outbounds.json",
        "https://provider.example/sub\nX-Injected: true",
    }
    for _,value:=range bad {
        if _,err:=validateSubscriptionURL(value);err==nil{t.Fatalf("SSRF/unsafe URL accepted")}
    }
    for _,test:=range []struct{ip string;safe bool}{
        {"1.1.1.1",true},{"8.8.8.8",true},
        {"127.0.0.1",false},{"192.168.1.1",false},{"10.1.2.3",false},
        {"100.100.10.10",false},{"169.254.169.254",false},
        {"198.18.1.1",false},{"203.0.113.7",false},
        {"::1",false},{"fc00::1",false},{"2001:db8::dead",false},
        {"2001:4860:4860::8888",true},
    }{
        ip,err:=netip.ParseAddr(test.ip)
        if err!=nil{t.Fatal(err)}
        if got:=publicSubscriptionIP(ip);got!=test.safe{
            t.Fatalf("IP %s public classification unexpected: %v",test.ip,got)
        }
    }
}

func TestSubscriptionLoopbackOnlyOriginAndNoSecretResponse(t *testing.T){
    app:=handlersWithProc(t.TempDir(),t.TempDir())
    uri:="/api/subscription/preview"
    makeRequest:=func(origin,host,action,contentType,body string)*httptest.ResponseRecorder{
        req:=httptest.NewRequest(http.MethodPost,"http://127.0.0.1:1009"+uri,strings.NewReader(body))
        req.Host=host
        if origin!=""{req.Header.Set("Origin",origin)}
        if action!=""{req.Header.Set("X-FreeNet-Action",action)}
        if contentType!=""{req.Header.Set("Content-Type",contentType)}
        rec:=httptest.NewRecorder()
        app.ServeHTTP(rec,req)
        return rec
    }
    payload:=`{"url":"https://127.0.0.1:8443/secrets?token=VERY-SECRET"}`
    rec:=makeRequest("http://127.0.0.1:1009","127.0.0.1:1009",
        "preview-subscription","application/json",payload)
    if rec.Code!=http.StatusBadRequest || strings.Contains(rec.Body.String(),"VERY-SECRET") {
        t.Fatalf("invalid SSRF URL not stopped privately: %d",rec.Code)
    }
    for _,attempt:=range []struct{origin,host,action string}{
        {"http://evil.example","127.0.0.1:1009","preview-subscription"},
        {"","127.0.0.1:1009","preview-subscription"},
        {"http://127.0.0.1:1009","evil.example:1009","preview-subscription"},
        {"http://127.0.0.1:1009","127.0.0.1:1009",""},
    }{
        got:=makeRequest(attempt.origin,attempt.host,attempt.action,
            "application/json",payload)
        if got.Code!=http.StatusForbidden{t.Fatalf("unsafe Origin/Host/action passed: %d",got.Code)}
    }
    invalid:=makeRequest("http://127.0.0.1:1009","127.0.0.1:1009",
        "preview-subscription","text/plain",payload)
    if invalid.Code!=http.StatusUnsupportedMediaType{t.Fatal("simple cross-site POST accepted")}
    if !strings.Contains(rec.Header().Get("Cache-Control"),"no-store") {
        t.Fatal("subscription response must never be cached")
    }
    for _,method:=range []string{"GET","PUT","DELETE","PATCH"} {
        req:=httptest.NewRequest(method,"http://127.0.0.1:1009"+uri,nil)
        res:=httptest.NewRecorder()
        app.ServeHTTP(res,req)
        if res.Code==http.StatusOK {t.Fatal("unsupported subscription method accepted")}
    }
}

func TestSubscriptionPreviewUIHasNoApplyOrStoredURL(t *testing.T) {
    h:=handlersWithProc(t.TempDir(),t.TempDir())
    html:=httptest.NewRecorder()
    h.ServeHTTP(html,httptest.NewRequest(http.MethodGet,"/",nil))
    page:=html.Body.String()
    for _,mark:=range []string{"subscription-url","subscription-preview","Проверить подписку",
        "НЕ ПРИМЕНЕНО"}{
        if !strings.Contains(page,mark){t.Fatalf("subscription preview UI missing %s",mark)}
    }
    js:=httptest.NewRecorder()
    h.ServeHTTP(js,httptest.NewRequest(http.MethodGet,"/static/app.js",nil))
    source:=js.Body.String()
    for _,mark:=range []string{"/api/subscription/preview","X-FreeNet-Action",
        "subscriptionURL.value = \"\"","textContent"}{
        if !strings.Contains(source,mark){t.Fatalf("safe subscription JS missing %s",mark)}
    }
    if strings.Contains(source,"localStorage.setItem") ||
        strings.Contains(source,"sessionStorage.setItem") {
        t.Fatal("subscription URL must not be persisted in browser")
    }
}
