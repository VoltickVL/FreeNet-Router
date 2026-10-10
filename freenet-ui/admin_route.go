package main

import (
    "encoding/json"
    "errors"
    "net"
    "net/http"
    "os"
    "path/filepath"
    "strconv"
    "strings"
)

type adminMatch uint8

const (
    adminNo adminMatch = iota
    adminYes
    adminUnknown
)

const maxAdminRouteRules = 256

var adminRouteReadSlots = make(chan struct{}, 2)

func adminRouteList(raw any, match func(string) adminMatch) adminMatch {
    entries, ok := raw.([]any)
    if !ok || len(entries) == 0 || len(entries) > maxAdminRouteRules { return adminUnknown }
    unknown := false
    for _, entry := range entries {
        value, ok := entry.(string)
        if !ok { unknown = true; continue }
        switch match(value) {
        case adminYes: return adminYes
        case adminUnknown: unknown = true
        }
    }
    if unknown { return adminUnknown }
    return adminNo
}

func adminRouteDomain(host, selector string) adminMatch {
    selector = strings.ToLower(strings.TrimSpace(selector))
    switch {
    case strings.HasPrefix(selector, "full:"):
        if host == strings.TrimPrefix(selector, "full:") { return adminYes }
        return adminNo
    case strings.HasPrefix(selector, "domain:"):
        d := strings.TrimPrefix(selector, "domain:")
        if d == "" { return adminUnknown }
        if host == d || strings.HasSuffix(host, "."+d) { return adminYes }
        return adminNo
    case strings.HasPrefix(selector, "keyword:"):
        d := strings.TrimPrefix(selector, "keyword:")
        if d == "" { return adminUnknown }
        if strings.Contains(host, d) { return adminYes }
        return adminNo
    default:
        // GeoSite and regexp require exact Xray matcher semantics, not
        // the fuzzy GeoData category-discovery API.
        return adminUnknown
    }
}

func adminRouteIP(target net.IP, selector string) adminMatch {
    if target == nil { return adminUnknown }
    if ip := net.ParseIP(selector); ip != nil {
        if ip.Equal(target) { return adminYes }
        return adminNo
    }
    _, network, err := net.ParseCIDR(selector)
    if err != nil { return adminUnknown }
    if network.Contains(target) { return adminYes }
    return adminNo
}

func adminRoutePort(port int, raw any) adminMatch {
    var text string
    switch v := raw.(type) {
    case string: text = v
    case float64:
        if v < 1 || v > 65535 || float64(int(v)) != v { return adminUnknown }
        text = strconv.Itoa(int(v))
    default: return adminUnknown
    }
    unknown := false
    for _, part := range strings.Split(text, ",") {
        part = strings.TrimSpace(part)
        bounds := strings.Split(part, "-")
        if len(bounds) > 2 || len(bounds) == 0 { unknown = true; continue }
        low, err := strconv.Atoi(bounds[0])
        if err != nil || low < 1 || low > 65535 { unknown = true; continue }
        high := low
        if len(bounds) == 2 {
            high, err = strconv.Atoi(bounds[1])
            if err != nil || high < low || high > 65535 { unknown = true; continue }
        }
        if port >= low && port <= high { return adminYes }
    }
    if unknown { return adminUnknown }
    return adminNo
}

func adminRouteRule(rule map[string]any, host, inbound, network string, destIP, clientIP net.IP, port int) adminMatch {
    if _, hasDomain := rule["domain"]; hasDomain {
        if _, hasIP := rule["ip"]; hasIP { return adminUnknown }
    }
    result := adminYes
    for key, raw := range rule {
        current := adminUnknown
        switch key {
        case "type":
            if kind, ok := raw.(string); ok && kind == "field" { current = adminYes }
        case "outboundTag", "balancerTag":
            current = adminYes
        case "domain":
            if destIP != nil { current = adminUnknown } else {
                current = adminRouteList(raw, func(s string) adminMatch { return adminRouteDomain(host, s) })
            }
        case "ip":
            current = adminRouteList(raw, func(s string) adminMatch { return adminRouteIP(destIP, s) })
        case "source":
            current = adminRouteList(raw, func(s string) adminMatch { return adminRouteIP(clientIP, s) })
        case "inboundTag":
            if inbound == "" { current = adminUnknown } else {
                current = adminRouteList(raw, func(s string) adminMatch {
                    if inbound == s { return adminYes }
                    return adminNo
                })
            }
        case "network":
            if values, ok := raw.(string); ok {
                current = adminNo
                for _, value := range strings.Split(values, ",") {
                    if strings.TrimSpace(value) == network { current = adminYes; break }
                }
            }
        case "port":
            current = adminRoutePort(port, raw)
        default:
            // App-layer protocol, attrs, sourcePort or future Xray conditions
            // must never be guessed from destination/transport alone.
            current = adminUnknown
        }
        if current == adminNo { return adminNo }
        if current == adminUnknown { result = adminUnknown }
    }
    if _, ok := rule["balancerTag"]; ok { return adminUnknown }
    if _, ok := rule["outboundTag"]; !ok { return adminUnknown }
    return result
}

func adminRouteReadJSON(path string, limit int64) (map[string]any, []byte, error) {
    info, err := os.Lstat(path)
    if err != nil { return nil, nil, err }
    if !info.Mode().IsRegular() || info.Size() < 0 || info.Size() > limit {
        return nil, nil, errors.New("unsafe or oversized Xray config")
    }
    data, err := os.ReadFile(path)
    if err != nil || int64(len(data)) > limit { return nil, nil, errors.New("cannot read safe Xray config") }
    var root map[string]any
    if err := json.Unmarshal(data, &root); err != nil || root == nil {
        return nil, nil, errors.New("invalid Xray JSON")
    }
    return root, data, nil
}

func adminRouteSafeTag(tag string) bool {
    if len(tag) == 0 || len(tag) > 64 { return false }
    for _, c := range tag {
        if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
            (c >= '0' && c <= '9') || c == '-' || c == '_' || c == '.' { continue }
        return false
    }
    return true
}

func adminRouteOutboundAction(outRoot map[string]any, tag string) string {
    entries, ok := outRoot["outbounds"].([]any)
    if !ok || len(entries) > maxAdminRouteRules { return "UNKNOWN" }
    count, action := 0, "UNKNOWN"
    for _, item := range entries {
        ob, ok := item.(map[string]any)
        if !ok || ob["tag"] != tag { continue }
        count++
        if _, chained := ob["proxySettings"]; chained { return "UNKNOWN" }
        protocol, _ := ob["protocol"].(string)
        switch protocol {
        case "freedom": action = "DIRECT"
        case "blackhole": action = "BLOCK"
        case "vless", "vmess", "trojan", "shadowsocks", "wireguard": action = "VPN"
        default: action = "UNKNOWN"
        }
    }
    if count != 1 { return "UNKNOWN" }
    return action
}

func (a *app) handleAdminRoute(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Cache-Control", "no-store")
    host := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("host")))
    destination := net.ParseIP(host)
    if destination == nil {
        validated, ok := validateAdminPublicDomain(host)
        if !ok {
            writeJSON(w, http.StatusBadRequest, map[string]any{"success":false,"error":"Укажите FQDN или IP назначения","mutation":"NONE"})
            return
        }
        host = validated
    } else { host = destination.String() }
    clientText := strings.TrimSpace(r.URL.Query().Get("client"))
    var client net.IP
    if clientText != "" {
        client = net.ParseIP(clientText)
        if client == nil || !client.IsPrivate() {
            writeJSON(w, http.StatusBadRequest, map[string]any{"success":false,"error":"LAN-клиент должен иметь частный IP-адрес","mutation":"NONE"})
            return
        }
    }
    network := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("network")))
    if network == "" { network = "tcp" }
    if network != "tcp" && network != "udp" {
        writeJSON(w, http.StatusBadRequest, map[string]any{"success":false,"error":"Допустимы только TCP и UDP","mutation":"NONE"})
        return
    }
    port := 443
    if raw := strings.TrimSpace(r.URL.Query().Get("port")); raw != "" {
        var err error
        port, err = strconv.Atoi(raw)
        if err != nil || port < 1 || port > 65535 {
            writeJSON(w, http.StatusBadRequest, map[string]any{"success":false,"error":"Порт должен быть от 1 до 65535","mutation":"NONE"})
            return
        }
    }
    inbound := strings.TrimSpace(r.URL.Query().Get("inbound"))
    if len(inbound) > 64 || strings.ContainsAny(inbound, " /\\:\t\r\n") {
        writeJSON(w, http.StatusBadRequest, map[string]any{"success":false,"error":"Некорректный inbound tag","mutation":"NONE"})
        return
    }
    response := map[string]any{
        "success":true, "mutation":"NONE", "host":host, "client":clientText,
        "port":port, "network":network, "expected_action":"UNKNOWN",
        "observed_route":"NOT_OBSERVED", "confidence":"UNKNOWN",
        "source":"on-disk Xray routing (not running-core proof)",
        "note":"Расчёт по файлу, не наблюдение LAN-клиента. Наличие процесса Xray и DNS не подтверждают реальный outbound.",
    }
    select {
    case adminRouteReadSlots <- struct{}{}:
        defer func() { <-adminRouteReadSlots }()
    default:
        writeJSON(w, http.StatusTooManyRequests, map[string]any{"success":false,"error":"Проверка маршрута занята, повторите позже","mutation":"NONE"})
        return
    }
    routingRoot, raw, err := adminRouteReadJSON(filepath.Join(a.routingConfigDir(), "05_routing.json"), maxRoutingSourceFileBytes)
    if err != nil {
        response["reason"] = "Правила Xray недоступны для безопасного чтения"
        writeJSON(w, http.StatusOK, response)
        return
    }
    response["rules_sha256"] = sha256Hex(raw)
    section, ok := routingRoot["routing"].(map[string]any)
    if !ok {
        response["reason"] = "Структура routing неизвестна"
        writeJSON(w, http.StatusOK, response)
        return
    }
    strategy, _ := section["domainStrategy"].(string)
    if strategy == "" { strategy = "AsIs" }
    response["domain_strategy"] = strategy
    if strategy != "AsIs" {
        response["reason"] = "Режим разрешения IP в Xray требует отдельной проверки"
        writeJSON(w, http.StatusOK, response)
        return
    }
    rules, ok := section["rules"].([]any)
    if !ok || len(rules) == 0 || len(rules) > maxAdminRouteRules {
        response["reason"] = "Правила отсутствуют или их слишком много"
        writeJSON(w, http.StatusOK, response)
        return
    }
    response["rules_count"] = len(rules)
    outRoot, _, err := adminRouteReadJSON(filepath.Join(a.routingConfigDir(), "04_outbounds.json"), maxRoutingSourceFileBytes)
    if err != nil {
        response["reason"] = "Исходящие каналы Xray недоступны"
        writeJSON(w, http.StatusOK, response)
        return
    }
    uncertainEarlier := false
    for i, rawRule := range rules {
        rule, ok := rawRule.(map[string]any)
        if !ok {
            uncertainEarlier = true
            continue
        }
        match := adminRouteRule(rule, host, inbound, network, destination, client, port)
        if match == adminUnknown { uncertainEarlier = true; continue }
        if match != adminYes { continue }
        if uncertainEarlier {
            response["reason"] = "Ранее по порядку есть правило с неопределённым совпадением"
            response["candidate_rule_order"] = i+1
            break
        }
        tag, _ := rule["outboundTag"].(string)
        if !adminRouteSafeTag(tag) {
            response["reason"] = "Невозможно безопасно показать outbound tag"
            break
        }
        action := adminRouteOutboundAction(outRoot, tag)
        response["matched_rule_order"] = i+1
        response["outbound_tag"] = tag
        response["expected_action"] = action
        if action == "UNKNOWN" {
            response["reason"] = "Невозможно подтвердить тип outbound из текущей конфигурации"
        } else {
            response["confidence"] = "CONFIG_ONLY"
            response["reason"] = "Совпадение в файле при условии, что Xray получил указанное назначение и inbound"
        }
        break
    }
    if _, known := response["matched_rule_order"]; !known {
        if _, exists := response["reason"]; !exists {
            response["reason"] = "Нет доказанного полного совпадения: возможен fallback или неподдерживаемое правило"
        }
    }
    writeJSON(w, http.StatusOK, response)
}
