package main

import (
    "encoding/json"
    "io"
    "net/http"
    "strings"
    "time"
)

type loginRequest struct {
    Code string `json:"code"`
}

func (a *adminAuth) handleLogin(w http.ResponseWriter,r *http.Request){
    w.Header().Set("Cache-Control","no-store")
    w.Header().Set("Content-Type","application/json; charset=utf-8")
    if r.Method!=http.MethodPost || r.TLS==nil || !requestSameOrigin(r) ||
        r.Header.Get("X-FreeNet-Action")!="login" ||
        !strings.HasPrefix(r.Header.Get("Content-Type"),"application/json") {
        http.Error(w,`{"state":"DENIED"}`,http.StatusForbidden)
        return
    }
    ip:=callerIP(r)
    if !a.loginPermitted(ip,nowAuth()) {
        http.Error(w,`{"state":"DENIED"}`,http.StatusTooManyRequests)
        return
    }
    decoder:=json.NewDecoder(http.MaxBytesReader(w,r.Body,512))
    decoder.DisallowUnknownFields()
    var input loginRequest
    if err:=decoder.Decode(&input);err!=nil || decoder.Decode(&struct{}{})!=io.EOF {
        a.failedLogin(ip,nowAuth())
        http.Error(w,`{"state":"DENIED"}`,http.StatusBadRequest)
        return
    }
    if !a.validatePairingCode(input.Code){
        a.failedLogin(ip,nowAuth())
        http.Error(w,`{"state":"DENIED"}`,http.StatusUnauthorized)
        return
    }
    if err:=a.issueSession(w);err!=nil{
        http.Error(w,`{"state":"DENIED"}`,http.StatusInternalServerError)
        return
    }
    w.WriteHeader(http.StatusNoContent)
}

func nowAuth() time.Time { return time.Now() }

func secureControlCenter(root,procRoot string,auth *adminAuth) http.Handler {
    base:=handlersWithProc(root,procRoot)
    return http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
        w.Header().Set("Cache-Control","no-store")
        w.Header().Set("X-Content-Type-Options","nosniff")
        w.Header().Set("X-Frame-Options","DENY")
        w.Header().Set("Referrer-Policy","no-referrer")
        w.Header().Set("Content-Security-Policy",
            "default-src 'none'; style-src 'self'; script-src 'self'; img-src 'self'; connect-src 'self'; manifest-src 'self'; worker-src 'self'; base-uri 'none'; frame-ancestors 'none'; form-action 'none'")
        if r.TLS==nil {
            http.Error(w,"TLS required",http.StatusForbidden)
            return
        }
        switch {
        case r.URL.Path=="/api/auth/login":
            auth.handleLogin(w,r)
            return
        case r.URL.Path=="/api/auth/status" && r.Method==http.MethodGet:
            w.Header().Set("Content-Type","application/json; charset=utf-8")
            _=json.NewEncoder(w).Encode(map[string]bool{"authenticated":auth.authenticated(r)})
            return
        case r.URL.Path=="/api/auth/logout":
            if r.Method!=http.MethodPost || !auth.authenticated(r) ||
                !requestSameOrigin(r) || r.Header.Get("X-FreeNet-Action")!="logout" {
                http.Error(w,"Access denied",http.StatusForbidden)
                return
            }
            auth.logout(w)
            w.WriteHeader(http.StatusNoContent)
            return
        case r.URL.Path=="/healthz" || r.URL.Path=="/versionz":
            base.ServeHTTP(w,r)
            return
        case strings.HasPrefix(r.URL.Path,"/static/") ||
            r.URL.Path=="/manifest.webmanifest" || r.URL.Path=="/sw.js":
            base.ServeHTTP(w,r)
            return
        }
        if !auth.authenticated(r) {
            if r.Method==http.MethodGet && r.URL.Path=="/" {
                writeWebAsset(w,"login.html","text/html; charset=utf-8")
                return
            }
            w.Header().Set("Content-Type","application/json; charset=utf-8")
            http.Error(w,`{"state":"AUTH_REQUIRED"}`,http.StatusUnauthorized)
            return
        }
        // All existing Xray/XKeen and subscription endpoints are now behind
        // the HTTPS authenticated session; no unauthenticated LAN control.
        base.ServeHTTP(w,r)
    })
}
