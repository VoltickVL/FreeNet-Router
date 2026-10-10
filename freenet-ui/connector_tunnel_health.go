package main

import (
    "context"
    "encoding/json"
    "io"
    "net/http"
    "strings"
    "time"
)

const connectorTunnelHealthURL = "http://127.0.0.1:12031/health?details=true"
const connectorTunnelHealthMaxBytes = 64 << 10

// All values returned by this endpoint are derived from an explicit whitelist.
// Upstream raw health JSON, URLs, identifiers, logs and error messages are never
// forwarded to the browser or included in FreeNet's diagnostic journal.
type connectorTunnelComponent struct {
    Status string `json:"status"`
    State string `json:"state"`
    ReasonCode string `json:"reason_code,omitempty"`
    FailureCategory string `json:"failure_category,omitempty"`
    HTTPStatus int `json:"http_status,omitempty"`
    ConsecutiveFailures uint64 `json:"consecutive_failures,omitempty"`
    StartupProbe string `json:"startup_probe,omitempty"`
}
type connectorTunnelHealthReport struct {
    Success bool `json:"success"`
    Mutation string `json:"mutation"`
    HealthState string `json:"health_state"`
    ClientRunning bool `json:"client_running"`
    ClientReady bool `json:"client_ready"`
    ControlPlane connectorTunnelComponent `json:"control_plane"`
    MCP connectorTunnelComponent `json:"mcp"`
    OAuth connectorTunnelComponent `json:"oauth"`
    Cloudflared connectorTunnelComponent `json:"cloudflared"`
    ResponseDelivery connectorTunnelComponent `json:"response_delivery"`
    ChatGPTConnected bool `json:"chatgpt_connected"`
}
type connectorTunnelRawComponent struct {
    Status string `json:"status"`
    State string `json:"state"`
    ReasonCode string `json:"reason_code"`
    Details struct {
        FailureCategory string `json:"failure_category"`
        HTTPStatus int `json:"http_status"`
        ConsecutiveFailures uint64 `json:"consecutive_failures"`
        StartupProbe struct { State string `json:"state"` } `json:"startup_probe"`
    } `json:"details"`
}
type connectorTunnelRawHealth struct {
    SchemaVersion int `json:"schema_version"`
    Ready *bool `json:"ready"`
    Components map[string]connectorTunnelRawComponent `json:"components"`
}

func connectorTunnelKnownStatus(v string) string {
    switch v {
    case "ok", "degraded", "unknown", "disabled":
        return v
    default:
        return "unknown"
    }
}
func connectorTunnelSafeCode(v string) string {
    // Upstream v0.0.16 uses fixed ASCII state/reason identifiers. Strings with
    // punctuation, separators, whitespace, token prefixes or long payloads
    // are never forwarded, including on an unexpected upstream schema change.
    if v == "" {return ""}
    if len(v)>48 || v[0]<'a' || v[0]>'z' {return "OTHER_REDACTED"}
    for _,c:=range v {
        if !((c>='a'&&c<='z')||(c>='0'&&c<='9')||c=='_') {return "OTHER_REDACTED"}
    }
    if strings.Contains(v,"token") || strings.Contains(v,"secret") || strings.Contains(v,"key") || strings.Contains(v,"credential") || strings.Contains(v,"uuid") {return "OTHER_REDACTED"}
    return v
}
func connectorTunnelProjectComponent(raw connectorTunnelRawComponent) connectorTunnelComponent {
    out:=connectorTunnelComponent{
        Status:connectorTunnelKnownStatus(raw.Status),
        State:connectorTunnelSafeCode(raw.State),
        ReasonCode:connectorTunnelSafeCode(raw.ReasonCode),
    }
    out.FailureCategory=connectorTunnelSafeCode(raw.Details.FailureCategory)
    if raw.Details.HTTPStatus>=100 && raw.Details.HTTPStatus<=599 {out.HTTPStatus=raw.Details.HTTPStatus}
    if raw.Details.ConsecutiveFailures<=1000000000 {out.ConsecutiveFailures=raw.Details.ConsecutiveFailures}
    out.StartupProbe=connectorTunnelSafeCode(raw.Details.StartupProbe.State)
    return out
}
func connectorTunnelParseHealth(raw []byte) (connectorTunnelHealthReport,bool) {
    report:=connectorTunnelHealthReport{Success:true,Mutation:"NONE",HealthState:"INVALID_HEALTH",ChatGPTConnected:false,ControlPlane:connectorTunnelComponent{Status:"unknown"},MCP:connectorTunnelComponent{Status:"unknown"},OAuth:connectorTunnelComponent{Status:"unknown"},Cloudflared:connectorTunnelComponent{Status:"unknown"},ResponseDelivery:connectorTunnelComponent{Status:"unknown"}}
    if len(raw)==0||len(raw)>connectorTunnelHealthMaxBytes{return report,false}
    var data connectorTunnelRawHealth
    if json.Unmarshal(raw,&data)!=nil||data.SchemaVersion!=1||data.Ready==nil||data.Components==nil{return report,false}
    report.HealthState="OBSERVED"
    report.ClientReady=*data.Ready
    if component,ok:=data.Components["control-plane"];ok{report.ControlPlane=connectorTunnelProjectComponent(component)}
    if component,ok:=data.Components["mcp"];ok{report.MCP=connectorTunnelProjectComponent(component)}
    if component,ok:=data.Components["oauth"];ok{report.OAuth=connectorTunnelProjectComponent(component)}
    if component,ok:=data.Components["cloudflared"];ok{report.Cloudflared=connectorTunnelProjectComponent(component)}
    if component,ok:=data.Components["response-delivery"];ok{report.ResponseDelivery=connectorTunnelProjectComponent(component)}
    return report,true
}

// URL is an internal test seam only; production uses the literal loopback URL.
// A separate transport disables outbound proxies and redirect following.
func connectorTunnelProbeHealth(ctx context.Context, target string) connectorTunnelHealthReport {
    initial,_:=connectorTunnelParseHealth(nil)
    initial.HealthState="UNAVAILABLE"
    client:=&http.Client{
        Timeout:1600*time.Millisecond,
        Transport:&http.Transport{DisableKeepAlives:true},
        CheckRedirect:func(_ *http.Request,_ []*http.Request) error{return http.ErrUseLastResponse},
    }
    // Never inherit HTTP_PROXY for a local-only health endpoint.
    req,err:=http.NewRequestWithContext(ctx,http.MethodGet,target,nil)
    if err!=nil {return initial}
    res,err:=client.Do(req)
    if err!=nil{return initial}
    defer res.Body.Close()
    if res.StatusCode!=http.StatusOK{
        initial.HealthState="HTTP_UNAVAILABLE"
        return initial
    }
    buf,err:=io.ReadAll(io.LimitReader(res.Body,connectorTunnelHealthMaxBytes+1))
    if err!=nil{return initial}
    report,valid:=connectorTunnelParseHealth(buf)
    if !valid{return report}
    return report
}

func (a *app) handleTunnelHealthDiagnostics(w http.ResponseWriter,r *http.Request) {
    w.Header().Set("Cache-Control","no-store")
    if len(r.URL.Query())!=0 {
        writeJSON(w,http.StatusBadRequest,map[string]any{"success":false,"mutation":"NONE","error":"Parameters not permitted"})
        return
    }
    a.connectorTunnelMu.Lock()
    running:=a.connectorTunnelCmd!=nil && a.connectorTunnelState!="STOP_UNKNOWN"
    a.connectorTunnelMu.Unlock()
    if !running {
        report,_:=connectorTunnelParseHealth(nil)
        report.HealthState="NOT_RUNNING"
        writeJSON(w,http.StatusOK,report)
        return
    }
    report:=connectorTunnelProbeHealth(r.Context(),connectorTunnelHealthURL)
    report.ClientRunning=true
    writeJSON(w,http.StatusOK,report)
}
