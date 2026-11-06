package main

// Subscription preview is intentionally loopback-only until the Browser Setup
// is authenticated. The response never returns URLs, UUIDs, Reality material,
// provider headers, native settings or downloaded config.
import (
    "context"
    "encoding/json"
    "io"
    "mime"
    "net"
    "net/http"
    "net/url"
    "strings"
    "time"
)

type subscriptionPreviewRequest struct {
    URL string `json:"url"`
}

func subscriptionSameOrigin(r *http.Request) bool {
    if r.Method!=http.MethodPost || r.TLS!=nil {
        // This endpoint belongs to the existing HTTP loopback-only preview;
        // remote HTTPS requires separate authenticated listener and CSRF.
        return false
    }
    host,_,err:=net.SplitHostPort(r.Host)
    if err!=nil {return false}
    ip:=net.ParseIP(host)
    if ip==nil || !ip.IsLoopback(){return false}
    origin:=r.Header.Get("Origin")
    parsed,err:=url.Parse(origin)
    if err!=nil || parsed.Scheme!="http" || parsed.Host!=r.Host ||
        parsed.User!=nil || parsed.RawQuery!="" || parsed.Fragment!="" ||
        parsed.Path!="" {
        return false
    }
    if site:=r.Header.Get("Sec-Fetch-Site");site!="" && site!="same-origin" {
        return false
    }
    return r.Header.Get("X-FreeNet-Action")=="preview-subscription"
}

func handleSubscriptionPreview(w http.ResponseWriter,r *http.Request, gate chan struct{}) {
    w.Header().Set("Content-Type","application/json; charset=utf-8")
    w.Header().Set("Cache-Control","no-store")
    writeResult:=func(status int,report subscriptionInspection){
        w.WriteHeader(status)
        _=json.NewEncoder(w).Encode(report)
    }
    if !subscriptionSameOrigin(r) {
        writeResult(http.StatusForbidden,stoppedSubscription())
        return
    }
    ct,_,err:=mime.ParseMediaType(r.Header.Get("Content-Type"))
    if err!=nil || ct!="application/json" {
        writeResult(http.StatusUnsupportedMediaType,stoppedSubscription())
        return
    }
    select {
    case gate <- struct{}{}:
        defer func(){<-gate}()
    default:
        response:=stoppedSubscription()
        response.State="BUSY"
        writeResult(http.StatusConflict,response)
        return
    }
    reader:=json.NewDecoder(http.MaxBytesReader(w,r.Body,4096))
    reader.DisallowUnknownFields()
    var input subscriptionPreviewRequest
    if err=reader.Decode(&input);err!=nil || reader.Decode(&struct{}{})!=io.EOF {
        writeResult(http.StatusBadRequest,stoppedSubscription())
        return
    }
    // Validate BEFORE network access; no details from URL/errors are logged.
    if _,err=validateSubscriptionURL(input.URL);err!=nil {
        writeResult(http.StatusBadRequest,stoppedSubscription())
        return
    }
    ctx,cancel:=context.WithTimeout(r.Context(),12*time.Second)
    defer cancel()
    result:=inspectURLSubscription(ctx,input.URL)
    if result.State!="PREVIEW_ONLY" {
        writeResult(http.StatusUnprocessableEntity,result)
        return
    }
    writeResult(http.StatusOK,result)
}

// Keep supported methods explicit: nobody may POST directly to the native
// apply/config endpoints in preview mode.
func subscriptionHTTPMethod(r *http.Request) bool {
    return strings.EqualFold(r.Method,http.MethodPost)
}
