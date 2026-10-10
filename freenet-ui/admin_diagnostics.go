package main

import (
  "context"
  "net"
  "net/http"
  "strings"
  "time"
)

// Only two concurrent read-only resolver requests. Never run a shell command,
// dial an arbitrary TCP endpoint or touch live Xray/DNS configuration.
var adminDNSProbeSlots = make(chan struct{}, 2)

type adminDNSResponse struct {
  Success bool `json:"success"`
  Host string `json:"host,omitempty"`
  IPs []string `json:"ips,omitempty"`
  Resolver string `json:"resolver"`
  Mutation string `json:"mutation"`
  Note string `json:"note,omitempty"`
  Error string `json:"error,omitempty"`
}

func validateAdminPublicDomain(input string) (string, bool) {
  host := strings.ToLower(strings.TrimSpace(input))
  if len(host) < 4 || len(host) > 253 || strings.HasSuffix(host, ".") || net.ParseIP(host) != nil || !strings.Contains(host, ".") {
    return "", false
  }
  for _, suffix := range []string{".local", ".localhost", ".lan", ".home", ".internal", ".test", ".invalid", ".arpa", ".onion", ".router", ".keenetic", ".example"} {
    if strings.HasSuffix(host, suffix) { return "", false }
  }
  for _, label := range strings.Split(host, ".") {
    if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' { return "", false }
    for _, c := range label {
      if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' { return "", false }
    }
  }
  return host, true
}

func registerAdminDiagnostics(mux *http.ServeMux, a *app) {
  mux.HandleFunc("GET /api/admin/dns", a.requireAuth(a.handleAdminDNS))
  mux.HandleFunc("GET /api/admin/route", a.requireAuth(a.handleAdminRoute))
  mux.HandleFunc("GET /api/admin/connector/diagnostics", a.requireAuth(a.handleConnectorDiagnostics))
  mux.HandleFunc("GET /api/admin/connector/grant", a.requireAuth(a.handleConnectorGrantStatus))
  mux.HandleFunc("GET /api/admin/connector/readiness", a.requireAuth(a.handleConnectorTunnelReadiness))
  mux.HandleFunc("GET /api/admin/connector/install/plan", a.requireAuth(a.handleTunnelInstallPlan))
  mux.HandleFunc("POST /api/admin/connector/install/apply", a.requireAuth(a.handleTunnelInstall))
  mux.HandleFunc("POST /api/admin/connector/pair", a.requireAuth(a.handleConnectorPair))
  mux.HandleFunc("POST /api/admin/connector/revoke", a.requireAuth(a.handleConnectorRevoke))
  mux.HandleFunc("GET /api/connector/diagnostics", a.handleConnectorMachineDiagnostics)
  mux.HandleFunc("POST /mcp", a.handleConnectorMCP)
}

func (a *app) handleAdminDNS(w http.ResponseWriter, r *http.Request) {
  host, ok := validateAdminPublicDomain(r.URL.Query().Get("host"))
  result := adminDNSResponse{Success:false, Resolver:"system", Mutation:"NONE"}
  if !ok {
    result.Error = "Укажи публичное доменное имя, без URL, IP-адреса и локальных имён"
    writeJSON(w, http.StatusBadRequest, result)
    return
  }
  result.Host = host
  select {
  case adminDNSProbeSlots <- struct{}{}:
    defer func(){<-adminDNSProbeSlots}()
  default:
    result.Error = "Диагностика DNS занята, повтори проверку позднее"
    writeJSON(w, http.StatusTooManyRequests, result)
    return
  }
  ctx,cancel := context.WithTimeout(r.Context(),3*time.Second)
  defer cancel()
  addresses,err := net.DefaultResolver.LookupIPAddr(ctx,host)
  if err != nil {
    result.Error="Не удалось разрешить домен системным DNS роутера или превышено время ожидания"
    writeJSON(w,http.StatusBadGateway,result)
    return
  }
  for _, addr := range addresses {
    ip:=addr.IP
    if ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsMulticast() || ip.IsUnspecified() { continue }
    if len(result.IPs)>=8 {break}
    result.IPs=append(result.IPs,ip.String())
  }
  result.Success=true
  result.Note="Системное DNS-разрешение. Не доказывает Xray routing или доступность сервиса"
  writeJSON(w,http.StatusOK,result)
}
