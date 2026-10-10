package main

import (
 "crypto/subtle"
 "encoding/hex"
 "encoding/json"
 "errors"
 "io"
 "net/http"
 "os"
 "strings"
 "time"
)

const connectorRemoteRequestTTL = 10 * time.Minute
const connectorRemoteApprovalTTL = 15 * time.Minute

// Only this short-lived in-memory authorization unlocks remote diagnostics.
// Restarting FreeNet loses both a pending request and any prior approval.
type connectorRemoteSession struct {
 PendingID string
 PendingUntil time.Time
 GrantedUntil time.Time
 LastRequest time.Time
}

func (a *app) connectorRemoteTransportMatch(r *http.Request) bool {
 if !requestFromLoopback(r) || !connectorLoopbackHost(r.Host) ||
    r.Header.Get("Cookie")!="" || r.URL.Query().Has("token") || len(r.Header.Values("Authorization"))!=1 {return false}
 auth:=r.Header.Get("Authorization")
 if len(auth)!=len("Bearer ")+64 || !strings.HasPrefix(auth,"Bearer ") {return false}
 if _,err:=hex.DecodeString(strings.TrimPrefix(auth,"Bearer "));err!=nil{return false}
 data,err:=connectorReadPrivateFile(a.connectorTransportHeaderPath(),96)
 if err!=nil || len(data)!=len(auth) {return false}
 return subtle.ConstantTimeCompare([]byte(auth),data)==1
}

func (a *app) connectorRemoteRequestAccess() (map[string]any,error) {
 a.connectorRemoteMu.Lock()
 defer a.connectorRemoteMu.Unlock()
 now:=time.Now().UTC()
 if now.Before(a.connectorRemote.GrantedUntil) {
  return map[string]any{"state":"APPROVED","expires_at":a.connectorRemote.GrantedUntil,"mutation":"AUTHORIZATION_ONLY"},nil
 }
 if now.Before(a.connectorRemote.PendingUntil) && a.connectorRemote.PendingID!="" {
  return map[string]any{"state":"PENDING","request_id":a.connectorRemote.PendingID,"expires_at":a.connectorRemote.PendingUntil,"mutation":"AUTHORIZATION_REQUEST_ONLY","instruction":"Откройте FreeNet → Администрирование и подтвердите запрос доступа в защищённой сессии браузера."},nil
 }
 if !a.connectorRemote.LastRequest.IsZero() && now.Sub(a.connectorRemote.LastRequest)<time.Minute {return nil,errors.New("access request rate-limited")}
 id,err:=randomSessionToken()
 if err!=nil{return nil,errors.New("access request unavailable")}
 a.connectorRemote=connectorRemoteSession{PendingID:id,PendingUntil:now.Add(connectorRemoteRequestTTL),LastRequest:now}
 return map[string]any{"state":"PENDING","request_id":id,"expires_at":a.connectorRemote.PendingUntil,"mutation":"AUTHORIZATION_REQUEST_ONLY","instruction":"Откройте FreeNet → Администрирование и подтвердите запрос доступа в защищённой сессии браузера."},nil
}

func (a *app) connectorRemoteStatus() map[string]any {
 a.connectorRemoteMu.Lock()
 defer a.connectorRemoteMu.Unlock()
 now:=time.Now().UTC()
 if now.Before(a.connectorRemote.GrantedUntil) {
  return map[string]any{"state":"APPROVED","expires_at":a.connectorRemote.GrantedUntil,"mutation":"NONE"}
 }
 if a.connectorRemote.PendingID!="" && now.Before(a.connectorRemote.PendingUntil) {
  return map[string]any{"state":"PENDING","expires_at":a.connectorRemote.PendingUntil,"request_id":a.connectorRemote.PendingID,"mutation":"NONE"}
 }
 return map[string]any{"state":"DENIED","mutation":"NONE"}
}

func (a *app) connectorRemoteCanRead() bool {
 a.connectorRemoteMu.Lock()
 defer a.connectorRemoteMu.Unlock()
 return !a.connectorRemote.GrantedUntil.IsZero() && time.Now().Before(a.connectorRemote.GrantedUntil)
}

func (a *app) connectorRemoteRevoke() {
 a.connectorRemoteMu.Lock()
 defer a.connectorRemoteMu.Unlock()
 a.connectorRemote=connectorRemoteSession{}
}

func (a *app) handleConnectorRemoteAccessStatus(w http.ResponseWriter,r *http.Request){
 w.Header().Set("Cache-Control","no-store")
 if len(r.URL.Query())!=0 {writeJSON(w,http.StatusBadRequest,map[string]any{"success":false,"error":"parameters forbidden"});return}
 result:=a.connectorRemoteStatus()
 result["success"]=true
 result["transport_configured"]=a.connectorTransportConfigured()
 writeJSON(w,http.StatusOK,result)
}

func (a *app) handleConnectorRemoteApprove(w http.ResponseWriter,r *http.Request){
 w.Header().Set("Cache-Control","no-store")
 if !sameOrigin(r) || !connectorAdminSecureOrigin(r) {
  writeJSON(w,http.StatusForbidden,map[string]any{"success":false,"error":"secure same-origin administration required"});return
 }
 var req struct {Confirm bool `json:"confirm"`; RequestID string `json:"request_id"`; Approve bool `json:"approve"`}
 decoder:=json.NewDecoder(http.MaxBytesReader(w,r.Body,512));decoder.DisallowUnknownFields()
 if err:=decoder.Decode(&req);err!=nil || !req.Confirm || len(req.RequestID)!=64 {
  writeJSON(w,http.StatusBadRequest,map[string]any{"success":false,"error":"explicit request confirmation required"});return
 }
 var extra any
 if err:=decoder.Decode(&extra);err!=io.EOF {writeJSON(w,http.StatusBadRequest,map[string]any{"success":false,"error":"invalid request"});return}
 if _,err:=hex.DecodeString(req.RequestID);err!=nil{writeJSON(w,http.StatusBadRequest,map[string]any{"success":false,"error":"invalid request id"});return}
 a.connectorRemoteMu.Lock()
 now:=time.Now().UTC()
 match:=a.connectorRemote.PendingID!="" && now.Before(a.connectorRemote.PendingUntil) &&
  subtle.ConstantTimeCompare([]byte(req.RequestID),[]byte(a.connectorRemote.PendingID))==1
 if !match {a.connectorRemoteMu.Unlock();writeJSON(w,http.StatusConflict,map[string]any{"success":false,"error":"pending request expired or changed"});return}
 a.connectorRemote.PendingID=""
 a.connectorRemote.PendingUntil=time.Time{}
 if req.Approve {a.connectorRemote.GrantedUntil=now.Add(connectorRemoteApprovalTTL)}else{a.connectorRemote.GrantedUntil=time.Time{}}
 until:=a.connectorRemote.GrantedUntil
 a.connectorRemoteMu.Unlock()
 if req.Approve {v3AppendEvent("connector","success","Временный удалённый read-only доступ выдан на 15 минут.")}else{v3AppendEvent("connector","success","Запрос на удалённый доступ отклонён.")}
 writeJSON(w,http.StatusOK,map[string]any{"success":true,"state":map[bool]string{true:"APPROVED",false:"DENIED"}[req.Approve],"expires_at":until,"mutation":"AUTHORIZATION_ONLY"})
}

func connectorRemoteToolResult(v map[string]any) map[string]any {
 encoded,_:=json.Marshal(v)
 return map[string]any{"content":[]map[string]any{{"type":"text","text":string(encoded)}},"structuredContent":v,"isError":false}
}

// A loopback reverse proxy is trusted only when its peer really is loopback,
// forwarded HTTPS is explicit, and browser Origin matches the forwarded Host.
// LAN-supplied X-Forwarded-Proto alone must never authorize secret delivery.
func connectorAdminSecureOrigin(r *http.Request) bool {
 if r.TLS!=nil {return true}
 if requestFromLoopback(r) && connectorLoopbackHost(r.Host) {return true}
 if !requestFromLoopback(r) || !strings.EqualFold(r.Header.Get("X-Forwarded-Proto"),"https") {return false}
 host:=strings.TrimSpace(r.Header.Get("X-Forwarded-Host"))
 if host=="" || strings.ContainsAny(host,",/\\ ") {return false}
 origin:=strings.TrimSpace(r.Header.Get("Origin"))
 return origin=="https://"+host && sameOrigin(r)
}

// An authenticated administrator can immediately revoke diagnostic approval
// without disrupting the outbound machine transport or VPN/routing.
func (a *app) handleConnectorRemoteRevoke(w http.ResponseWriter,r *http.Request){
 w.Header().Set("Cache-Control","no-store")
 if !connectorConfirm(w,r){return}
 if !connectorAdminSecureOrigin(r){
  writeJSON(w,http.StatusForbidden,map[string]any{"success":false,"error":"secure origin required"});return
 }
 a.connectorRemoteRevoke()
 v3AppendEvent("connector","success","Удалённое диагностическое разрешение отозвано.")
 writeJSON(w,http.StatusOK,map[string]any{"success":true,"state":"DENIED","mutation":"AUTHORIZATION_ONLY"})
}
