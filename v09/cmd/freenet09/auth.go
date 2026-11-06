package main

// Admin access for the thin XKeen/Xray web shell. The administrator bootstrap
// token has 256 bits of random entropy: a SHA-256 digest is sufficient for
// storing a random bearer token (NOT suitable for low-entropy passwords).
// Xray/XKeen authentication and credentials are not duplicated or modified.
import (
    "crypto/rand"
    "crypto/sha256"
    "crypto/subtle"
    "encoding/base64"
    "encoding/hex"
    "errors"
    "net"
    "net/http"
    "net/url"
    "os"
    "path/filepath"
    "strconv"
    "strings"
    "sync"
    "time"
)

const adminHashRelative = "etc/freenet09/admin.sha256"
const adminCookieName = "__Host-freenet09"
const adminSessionTTL = 12*time.Hour

type loginFailures struct {
    count int
    first time.Time
    locked time.Time
}

type adminAuth struct {
    credential [32]byte
    mu sync.Mutex
    session [32]byte
    sessionValid bool
    expires time.Time
    failed map[string]loginFailures
}

func initializeAdminCredential(root string)(string,error){
    if observedPath(root,adminHashRelative,false)!="MISSING" {
        return "",errors.New("admin credential already present or unknown")
    }
    if observedPath(root,"etc",true)!="PRESENT" {
        return "",errors.New("Entware etc directory unavailable")
    }
    folder:=filepath.Join(root,"etc","freenet09")
    if err:=os.Mkdir(folder,0700);err!=nil {
        return "",errors.New("admin directory already present or unavailable")
    }
    var raw [32]byte
    if _,err:=rand.Read(raw[:]);err!=nil{return "",errors.New("secure randomness unavailable")}
    token:=base64.RawURLEncoding.EncodeToString(raw[:])
    fingerprint:=sha256.Sum256([]byte(token))
    file:=filepath.Join(folder,"admin.sha256")
    fd,err:=os.OpenFile(file,os.O_WRONLY|os.O_CREATE|os.O_EXCL,0600)
    if err!=nil{return "",errors.New("credential file cannot be created")}
    defer fd.Close()
    if _,err=fd.WriteString(hex.EncodeToString(fingerprint[:])+"\n");err!=nil {
        return "",errors.New("credential file cannot be written")
    }
    if err=fd.Sync();err!=nil{return "",errors.New("credential file sync failed")}
    // The caller displays this pairing code ONCE during the local bootstrap.
    // Never publish it to logs/issues/API, and never store it in plaintext.
    return token,nil
}

func loadAdminAuth(root string)(*adminAuth,error){
    if observedPath(root,adminHashRelative,false)!="PRESENT" {
        return nil,errors.New("admin credential missing")
    }
    file:=filepath.Join(root,adminHashRelative)
    info,err:=os.Lstat(file)
    if err!=nil || !info.Mode().IsRegular() || info.Mode().Perm()!=0600 ||
        info.Size()>128 {
        return nil,errors.New("admin credential has unsafe permissions")
    }
    bytes,err:=os.ReadFile(file)
    if err!=nil{return nil,errors.New("admin credential unreadable")}
    encoded:=strings.TrimSpace(string(bytes))
    decoded,err:=hex.DecodeString(encoded)
    if err!=nil || len(decoded)!=32{return nil,errors.New("admin credential invalid")}
    state:=&adminAuth{failed:make(map[string]loginFailures)}
    copy(state.credential[:],decoded)
    return state,nil
}

func requestSameOrigin(r *http.Request) bool {
    if r.Host=="" || r.Header.Get("Origin")=="" {return false}
    scheme:="http"
    if r.TLS!=nil {scheme="https"}
    parsed,err:=url.Parse(r.Header.Get("Origin"))
    if err!=nil || parsed.Scheme!=scheme || parsed.Host!=r.Host ||
        parsed.Path!="" || parsed.RawQuery!="" || parsed.Fragment!="" ||
        parsed.User!=nil || parsed.Opaque!="" {
        return false
    }
    if r.TLS==nil {
        host,_,err:=net.SplitHostPort(r.Host)
        if err!=nil{return false}
        ip:=net.ParseIP(host)
        if ip==nil || !ip.IsLoopback(){return false}
    }
    if v:=r.Header.Get("Sec-Fetch-Site");v!="" && v!="same-origin" {
        return false
    }
    return true
}

func callerIP(r *http.Request) string {
    host,_,err:=net.SplitHostPort(r.RemoteAddr)
    if err!=nil{return "unknown"}
    ip:=net.ParseIP(host)
    if ip==nil{return "unknown"}
    return ip.String()
}

func (a *adminAuth) loginPermitted(ip string,now time.Time) bool {
    a.mu.Lock()
    defer a.mu.Unlock()
    attempts:=a.failed[ip]
    return !now.Before(attempts.locked)
}

func (a *adminAuth) failedLogin(ip string,now time.Time){
    a.mu.Lock()
    defer a.mu.Unlock()
    attempts:=a.failed[ip]
    if now.Sub(attempts.first)>5*time.Minute || attempts.first.IsZero() {
        attempts=loginFailures{first:now}
    }
    attempts.count++
    if attempts.count>=5{attempts.locked=now.Add(60*time.Second)}
    if len(a.failed)>128{a.failed=make(map[string]loginFailures)}
    a.failed[ip]=attempts
}

func (a *adminAuth) validatePairingCode(code string) bool {
    if len(code)!=43 {return false}
    digest:=sha256.Sum256([]byte(code))
    return subtle.ConstantTimeCompare(digest[:],a.credential[:])==1
}

func (a *adminAuth) issueSession(w http.ResponseWriter) error {
    var token [32]byte
    if _,err:=rand.Read(token[:]);err!=nil{return errors.New("session randomness unavailable")}
    digest:=sha256.Sum256(token[:])
    expires:=time.Now().Add(adminSessionTTL)
    a.mu.Lock()
    a.session=digest
    a.sessionValid=true
    a.expires=expires
    a.failed=make(map[string]loginFailures)
    a.mu.Unlock()
    http.SetCookie(w,&http.Cookie{
        Name:adminCookieName,
        Value:hex.EncodeToString(token[:]), Path:"/",
        Secure:true,HttpOnly:true,SameSite:http.SameSiteStrictMode,
        Expires:expires,
    })
    return nil
}

func (a *adminAuth) authenticated(r *http.Request) bool {
    if r.TLS==nil{return false}
    cookie,err:=r.Cookie(adminCookieName)
    if err!=nil{return false}
    raw,err:=hex.DecodeString(cookie.Value)
    if err!=nil || len(raw)!=32{return false}
    digest:=sha256.Sum256(raw)
    a.mu.Lock()
    defer a.mu.Unlock()
    return a.sessionValid && time.Now().Before(a.expires) &&
        subtle.ConstantTimeCompare(digest[:],a.session[:])==1
}

func (a *adminAuth) logout(w http.ResponseWriter){
    a.mu.Lock()
    a.sessionValid=false
    a.session=[32]byte{}
    a.mu.Unlock()
    http.SetCookie(w,&http.Cookie{
        Name:adminCookieName,Value:"",Path:"/",Secure:true,HttpOnly:true,
        SameSite:http.SameSiteStrictMode,MaxAge:-1,
    })
}

func secureListenAddress(addr string) bool {
    host,port,err:=net.SplitHostPort(addr)
    if err!=nil || port=="" {return false}
    numeric,err:=strconv.Atoi(port)
    if err!=nil || numeric<1 || numeric>65535 {return false}
    ip:=net.ParseIP(host)
    return ip!=nil && !ip.IsUnspecified() && !ip.IsMulticast()
}
