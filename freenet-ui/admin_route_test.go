package main

import (
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "os"
    "path/filepath"
    "strings"
    "testing"
)

func adminRouteFixture(t *testing.T, rules string) (*app, string) {
    t.Helper()
    dir := t.TempDir()
    t.Setenv("FREENET_ROUTING_CONFIG_DIR", dir)
    if err := os.WriteFile(filepath.Join(dir, "05_routing.json"), []byte(rules), 0600); err != nil { t.Fatal(err) }
    out := "{\"outbounds\":[{\"tag\":\"direct\",\"protocol\":\"freedom\"},{\"tag\":\"vpn-link\",\"protocol\":\"vless\",\"settings\":{\"private_key\":\"LEAK-SECRET\"}},{\"tag\":\"block\",\"protocol\":\"blackhole\"}]}"
    if err := os.WriteFile(filepath.Join(dir, "04_outbounds.json"), []byte(out), 0600); err != nil { t.Fatal(err) }
    return &app{}, dir
}

func adminRouteRequest(t *testing.T, a *app, query string) map[string]any {
    t.Helper()
    w := httptest.NewRecorder()
    a.handleAdminRoute(w, httptest.NewRequest(http.MethodGet, "http://router/api/admin/route?"+query, nil))
    if w.Code != http.StatusOK { t.Fatalf("status=%d body=%s", w.Code, w.Body.String()) }
    if !strings.Contains(w.Body.String(), "\"mutation\":\"NONE\"") && !strings.Contains(w.Body.String(), "\"mutation\": \"NONE\"") {
        t.Fatalf("read-only contract missing: %s", w.Body.String())
    }
    if strings.Contains(w.Body.String(), "LEAK-SECRET") { t.Fatal("Xray credential leaked") }
    var result map[string]any
    if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil { t.Fatal(err) }
    return result
}

func TestAdminRouteStaticRulesAreConfigOnly(t *testing.T) {
    rules := "{\"routing\":{\"domainStrategy\":\"AsIs\",\"rules\":[{\"type\":\"field\",\"domain\":[\"full:sg.api.io.mi.com\"],\"outboundTag\":\"direct\"},{\"type\":\"field\",\"network\":\"tcp,udp\",\"outboundTag\":\"vpn-link\"}]}}"
    a, dir := adminRouteFixture(t, rules)
    before, _ := os.ReadFile(filepath.Join(dir, "05_routing.json"))
    exact := adminRouteRequest(t, a, "host=sg.api.io.mi.com&client=192.168.50.144&port=443&network=tcp")
    if exact["expected_action"] != "DIRECT" || exact["confidence"] != "CONFIG_ONLY" || exact["observed_route"] != "NOT_OBSERVED" || exact["matched_rule_order"] != float64(1) {
        t.Fatalf("incorrect on-disk direct match: %+v", exact)
    }
    if _, ok := exact["rules_sha256"].(string); !ok { t.Fatal("missing rules provenance hash") }
    fallback := adminRouteRequest(t, a, "host=another.example.com&port=444&network=udp")
    if fallback["expected_action"] != "VPN" || fallback["matched_rule_order"] != float64(2) {
        t.Fatalf("incorrect explicit fallback: %+v", fallback)
    }
    after, _ := os.ReadFile(filepath.Join(dir, "05_routing.json"))
    if string(before) != string(after) { t.Fatal("route preview modified live configuration") }
}

func TestAdminRouteAmbiguousRuleNeverInventsDirect(t *testing.T) {
    for _, earlier := range []string{
        "{\"type\":\"field\",\"domain\":[\"ext:geosite.dat:xiaomi\"],\"outboundTag\":\"vpn-link\"}",
        "{\"type\":\"field\",\"inboundTag\":[\"mystery\"],\"outboundTag\":\"vpn-link\"}",
        "{\"type\":\"field\",\"sourcePort\":\"445-500\",\"outboundTag\":\"vpn-link\"}",
        "{\"type\":\"field\",\"ip\":[\"ext:geoip.dat:private\"],\"outboundTag\":\"vpn-link\"}",
    } {
        rules := "{\"routing\":{\"rules\":["+earlier+",{\"type\":\"field\",\"domain\":[\"full:sg.api.io.mi.com\"],\"outboundTag\":\"direct\"}]}}"
        a, _ := adminRouteFixture(t, rules)
        result := adminRouteRequest(t, a, "host=sg.api.io.mi.com&client=192.168.50.144")
        if result["expected_action"] != "UNKNOWN" || result["confidence"] != "UNKNOWN" {
            t.Fatalf("earlier ambiguous rule was bypassed: %+v", result)
        }
    }
}

func TestAdminRouteNoMatchingRuleIsUnknownNotDirect(t *testing.T) {
    a, _ := adminRouteFixture(t, "{\"routing\":{\"rules\":[{\"type\":\"field\",\"domain\":[\"full:elsewhere.example.com\"],\"outboundTag\":\"direct\"}]}}")
    result := adminRouteRequest(t, a, "host=sg.api.io.mi.com")
    if result["expected_action"] != "UNKNOWN" { t.Fatalf("invented fallback: %+v", result) }
}

func TestAdminRouteIPCIDRAndTransport(t *testing.T) {
    a, _ := adminRouteFixture(t, "{\"routing\":{\"rules\":[{\"type\":\"field\",\"ip\":[\"203.0.113.0/24\"],\"port\":\"443,8443\",\"network\":\"tcp\",\"outboundTag\":\"block\"},{\"type\":\"field\",\"network\":\"tcp,udp\",\"outboundTag\":\"direct\"}]}}")
    result := adminRouteRequest(t, a, "host=203.0.113.25&port=8443&network=tcp")
    if result["expected_action"] != "BLOCK" || result["matched_rule_order"] != float64(1) {
        t.Fatalf("IP CIDR block did not match: %+v", result)
    }
    udp := adminRouteRequest(t, a, "host=203.0.113.25&port=8443&network=udp")
    if udp["expected_action"] != "DIRECT" || udp["matched_rule_order"] != float64(2) {
        t.Fatalf("transport filter changed ordering: %+v", udp)
    }
}

func TestAdminRouteRejectsUnsafeInputsAndRequiresAuth(t *testing.T) {
    a, _ := adminRouteFixture(t, "{\"routing\":{\"rules\":[]}}")
    mux := http.NewServeMux()
    registerAdminDiagnostics(mux, a)
    w := httptest.NewRecorder()
    mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "http://router/api/admin/route?host=sg.api.io.mi.com", nil))
    if w.Code == http.StatusOK { t.Fatal("anonymous route access was accepted") }
    for _, query := range []string{
        "host=router.lan", "host=http://sg.api.io.mi.com", "host=sg.api.io.mi.com&client=8.8.8.8",
        "host=sg.api.io.mi.com&port=65536", "host=sg.api.io.mi.com&network=icmp",
    } {
        w := httptest.NewRecorder()
        a.handleAdminRoute(w, httptest.NewRequest(http.MethodGet, "http://router/api/admin/route?"+query, nil))
        if w.Code != http.StatusBadRequest { t.Fatalf("%s: expected 400 got %d", query, w.Code) }
    }
}

func TestAdminRouteMalformedConfigUnknown(t *testing.T) {
    a, dir := adminRouteFixture(t, "{\"routing\":{\"rules\":[]}}")
    if err := os.WriteFile(filepath.Join(dir, "05_routing.json"), []byte("{invalid"), 0600); err != nil { t.Fatal(err) }
    result := adminRouteRequest(t, a, "host=sg.api.io.mi.com")
    if result["expected_action"] != "UNKNOWN" || result["observed_route"] != "NOT_OBSERVED" {
        t.Fatalf("malformed config guessed route: %+v", result)
    }
}

func TestAdminRouteUISeparatesExpectedFromObserved(t *testing.T) {
    raw, err := webFS.ReadFile("web/index.html")
    if err != nil { t.Fatal(err) }
    ui := string(raw)
    for _, want := range []string{
        "adminRouteForm", "adminRouteHost", "adminRouteClient",
        "adminRouteNetwork", "adminRoutePort", "adminRouteOutput",
        "/api/admin/route?", "Факт реального соединения: НЕ НАБЛЮДАЛСЯ",
        "GeoSite, GeoIP и неизвестные условия",
        "window.FreeNetFlags",
    } {
        if !strings.Contains(ui, want) { t.Fatalf("Administration route view missing %q", want) }
    }
    if !strings.Contains(ui, "const known=d.confidence==='CONFIG_ONLY'") {
        t.Fatal("Administration must distinguish known config rule from unknown result")
    }
}

func TestAdminRouteRejectsChainedOrUnsafeOutboundTags(t *testing.T) {
    if adminRouteSafeTag("my-token/secret") || adminRouteSafeTag("credential@example.com") {
        t.Fatal("unsafe outbound tags were permitted in admin API")
    }
    if !adminRouteSafeTag("vless-reality") { t.Fatal("safe Xray tag was rejected") }
    root := map[string]any{"outbounds":[]any{
        map[string]any{"tag":"direct","protocol":"freedom","proxySettings":map[string]any{"tag":"vless-reality"}},
    }}
    if got := adminRouteOutboundAction(root, "direct"); got != "UNKNOWN" {
        t.Fatalf("chained freedom is not proven DIRECT: %s", got)
    }
    root["outbounds"] = []any{
        map[string]any{"tag":"direct","protocol":"freedom"},
        map[string]any{"tag":"direct","protocol":"blackhole"},
    }
    if got := adminRouteOutboundAction(root, "direct"); got != "UNKNOWN" {
        t.Fatalf("ambiguous duplicated outbound tag must be UNKNOWN: %s", got)
    }
}
