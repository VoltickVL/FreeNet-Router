package main

// Private subscription downloads are HTTPS-only, bounded, non-redirecting, and
// immune to DNS rebinding/SSRF. No URL or upstream error is logged or returned.
import (
    "context"
    "errors"
    "io"
    "net"
    "net/http"
    "net/netip"
    "net/url"
    "strconv"
    "strings"
    "time"
)

var errSubscriptionNetwork = errors.New("subscription-download-unavailable")

var disallowedSubscriptionIP = []netip.Prefix{
    netip.MustParsePrefix("0.0.0.0/8"),
    netip.MustParsePrefix("10.0.0.0/8"),
    netip.MustParsePrefix("100.64.0.0/10"),
    netip.MustParsePrefix("127.0.0.0/8"),
    netip.MustParsePrefix("169.254.0.0/16"),
    netip.MustParsePrefix("172.16.0.0/12"),
    netip.MustParsePrefix("192.0.0.0/24"),
    netip.MustParsePrefix("192.0.2.0/24"),
    netip.MustParsePrefix("192.168.0.0/16"),
    netip.MustParsePrefix("198.18.0.0/15"),
    netip.MustParsePrefix("198.51.100.0/24"),
    netip.MustParsePrefix("203.0.113.0/24"),
    netip.MustParsePrefix("224.0.0.0/4"),
    netip.MustParsePrefix("240.0.0.0/4"),
    netip.MustParsePrefix("::/128"),
    netip.MustParsePrefix("::1/128"),
    netip.MustParsePrefix("fc00::/7"),
    netip.MustParsePrefix("fe80::/10"),
    netip.MustParsePrefix("2001:db8::/32"),
    netip.MustParsePrefix("2001::/32"),
    netip.MustParsePrefix("2002::/16"),
    netip.MustParsePrefix("64:ff9b::/96"),
}

func publicSubscriptionIP(ip netip.Addr) bool {
    ip=ip.Unmap()
    if !ip.IsValid() || !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
        return false
    }
    for _,prefix:=range disallowedSubscriptionIP{
        if prefix.Contains(ip){return false}
    }
    return true
}

func validateSubscriptionURL(raw string) (*url.URL,error){
    if len(raw)==0 || len(raw)>4096 || strings.TrimSpace(raw)!=raw ||
        strings.ContainsAny(raw,"#\r\n") {
        return nil,errSubscriptionInvalid
    }
    u,err:=url.ParseRequestURI(raw)
    if err!=nil || u.Scheme!="https" || u.Opaque!="" ||
        u.User!=nil || u.Fragment!="" || u.Hostname()=="" {
        return nil,errSubscriptionInvalid
    }
    host:=u.Hostname()
    if len(host)>253 || strings.ContainsAny(host," \r\n\t%") ||
        strings.EqualFold(host,"localhost") ||
        strings.HasSuffix(strings.ToLower(host),".local") ||
        strings.HasSuffix(strings.ToLower(host),".localhost") ||
        strings.HasSuffix(strings.ToLower(host),".internal") {
        return nil,errSubscriptionInvalid
    }
    for _,ch:=range host {if ch>127 || ch<33 {return nil,errSubscriptionInvalid}}
    if _,err:=netip.ParseAddr(host);err==nil{return nil,errSubscriptionInvalid}
    port:=u.Port()
    if port!="" && port!="443" && port!="8443" {
        return nil,errSubscriptionInvalid
    }
    return u,nil
}

func fetchNativeSubscription(ctx context.Context, rawURL string)([]byte,error){
    u,err:=validateSubscriptionURL(rawURL)
    if err!=nil{return nil,errSubscriptionInvalid}
    ctx,cancel:=context.WithTimeout(ctx,12*time.Second)
    defer cancel()
    host:=u.Hostname()
    ips,err:=net.DefaultResolver.LookupIPAddr(ctx,host)
    if err!=nil || len(ips)==0 {return nil,errSubscriptionNetwork}
    // Reject mixed public/private DNS responses, never merely choose a
    // "good" address while a private rebinding target is also returned.
    for _,entry:=range ips {
        ip,conv:=netip.ParseAddr(entry.IP.String())
        if conv!=nil || !publicSubscriptionIP(ip){return nil,errSubscriptionNetwork}
    }
    port:=u.Port()
    if port==""{port="443"}
    transport:=&http.Transport{
        Proxy:nil,DisableKeepAlives:true,MaxIdleConns:0,
        TLSHandshakeTimeout:5*time.Second,
        ResponseHeaderTimeout:7*time.Second,
    }
    transport.DialContext=func(dialCtx context.Context,network,address string)(net.Conn,error){
        destHost,destPort,splitErr:=net.SplitHostPort(address)
        if splitErr!=nil || destHost!=host || destPort!=port {
            return nil,errSubscriptionNetwork
        }
        dialer:=&net.Dialer{Timeout:4*time.Second}
        for _,entry:=range ips {
            ip,conv:=netip.ParseAddr(entry.IP.String())
            if conv!=nil || !publicSubscriptionIP(ip){return nil,errSubscriptionNetwork}
            conn,dialErr:=dialer.DialContext(dialCtx,"tcp",net.JoinHostPort(entry.IP.String(),strconv.Itoa(mustPortNumber(port))))
            if dialErr==nil{return conn,nil}
        }
        return nil,errSubscriptionNetwork
    }
    defer transport.CloseIdleConnections()
    client:=&http.Client{
        Transport:transport,Timeout:12*time.Second,
        CheckRedirect:func(_ *http.Request,_ []*http.Request)error{
            return http.ErrUseLastResponse
        },
    }
    req,err:=http.NewRequestWithContext(ctx,http.MethodGet,u.String(),nil)
    if err!=nil{return nil,errSubscriptionInvalid}
    req.Header.Set("User-Agent","Happ/1.0")
    req.Header.Set("Accept","text/plain, application/octet-stream, */*")
    resp,err:=client.Do(req)
    if err!=nil{return nil,errSubscriptionNetwork}
    defer resp.Body.Close()
    if resp.StatusCode!=http.StatusOK {return nil,errSubscriptionNetwork}
    if resp.ContentLength>maxSubscriptionBody {return nil,errSubscriptionInvalid}
    body,err:=io.ReadAll(io.LimitReader(resp.Body,maxSubscriptionBody+1))
    if err!=nil{return nil,errSubscriptionNetwork}
    if len(body)>maxSubscriptionBody {return nil,errSubscriptionInvalid}
    return body,nil
}

// URL validity has already constrained this to either 443 or 8443.
func mustPortNumber(port string) int {
    if port=="8443" {return 8443}
    return 443
}

func inspectURLSubscription(ctx context.Context, subURL string) subscriptionInspection {
    fail:=stoppedSubscription()
    body,err:=fetchNativeSubscription(ctx,subURL)
    if err!=nil{return fail}
    return subscriptionPreviewFromBody(body)
}
