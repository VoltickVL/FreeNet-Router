package main

import (
    "net"
    "net/http"
    "runtime"
    "strings"
    "time"
)

type connectorTunnelReadiness struct {
    Success             bool   `json:"success"`
    Mutation            string `json:"mutation"`
    RouterOS            string `json:"router_os"`
    RouterArch          string `json:"router_arch"`
    ArchitectureStatus  string `json:"architecture_status"`
    LoopbackMCP         string `json:"loopback_mcp,omitempty"`
    MCPReadyOnRouter    bool   `json:"mcp_ready_on_router"`
    GrantActive         bool   `json:"grant_active"`
    ExternalConnected   bool   `json:"external_connected"`
    ExternalEvidence    string `json:"external_evidence"`
    NextStep            string `json:"next_step"`
    Notes               string `json:"notes"`
}

// This is deliberately a diagnostic, not a tunnel launcher or installation
// heuristic. Official tunnel-client binary availability alone is not enough
// to establish Entware ABI compatibility or device resources.
func connectorReadinessFor(goos, goarch, listener string) connectorTunnelReadiness {
    state := connectorTunnelReadiness{
        Success: true,
        Mutation: "NONE",
        RouterOS: goos,
        RouterArch: goarch,
        ArchitectureStatus: "EXTERNAL_TRUSTED_HOST_REQUIRED",
        ExternalConnected: false,
        ExternalEvidence: "NOT_OBSERVED",
        NextStep: "VERIFY_TRUSTED_BRIDGE",
        Notes: "Порт FreeNet не открывать в WAN; внешний MCP/ChatGPT не подтверждён.",
    }
    host, port, err := net.SplitHostPort(listener)
    if err == nil && port != "" {
        ip := net.ParseIP(host)
        switch {
        case ip != nil && ip.IsLoopback():
            state.LoopbackMCP = "http://" + net.JoinHostPort(host,port) + "/mcp"
        case loopbackListenAddr(listener) != "":
            state.LoopbackMCP = "http://" + net.JoinHostPort("127.0.0.1",port) + "/mcp"
        }
        state.MCPReadyOnRouter = state.LoopbackMCP != ""
    }
    if goos == "linux" && (goarch == "arm64" || goarch == "amd64") {
        state.ArchitectureStatus = "OFFICIAL_BINARY_ARCH_SUPPORTED"
        state.NextStep = "PLATFORM_TUNNEL_AND_CLIENT_SETUP"
        state.Notes = "Архитектура официальной сборки совместима, но Entware ABI, память, фактическая установка и подключение ещё не проверены."
    } else if goos == "linux" && (strings.HasPrefix(goarch, "mips") || goarch == "386" || strings.HasPrefix(goarch, "arm")) {
        state.ArchitectureStatus = "OFFICIAL_BINARY_UNAVAILABLE"
        state.NextStep = "TRUSTED_EXTERNAL_HOST_AND_SECURE_BRIDGE_REQUIRED"
        state.Notes = "Готового официального tunnel-client для архитектуры роутера нет. Прямой доступ с LAN к локальному MCP запрещён; защищённый bridge ещё не реализован."
    }
    if !state.MCPReadyOnRouter {
        state.NextStep = "VERIFY_LOOPBACK_LISTENER"
        state.Notes = "Адрес локального MCP не подтверждён по конфигурации FreeNet. STOP до выяснения безопасного адреса."
    }
    return state
}

func (a *app) handleConnectorTunnelReadiness(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Cache-Control", "no-store")
    if len(r.URL.Query()) != 0 {
        writeJSON(w,http.StatusBadRequest,map[string]any{"success":false,"mutation":"NONE","error":"Параметры для preflight не допускаются"})
        return
    }
    result := connectorReadinessFor(runtime.GOOS, runtime.GOARCH, a.cfg.Listen)
    a.connectorMu.Lock()
    grant,exists,err := a.readConnectorGrant()
    a.connectorMu.Unlock()
    if err != nil {
        writeJSON(w,http.StatusServiceUnavailable,map[string]any{
            "success":false,"mutation":"NONE","error":"Локальное состояние доступа требует read-only диагностики; выдача или подключение запрещены",
        })
        return
    }
    result.GrantActive = exists && time.Now().Before(grant.ExpiresAt)
    writeJSON(w,http.StatusOK,result)
}
