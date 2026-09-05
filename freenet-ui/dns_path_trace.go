package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"time"
)

const (
	dnsTracePayloadInbound = "freenet-trace-payload"
	dnsTraceDNSInbound     = "freenet-trace-dns"
	dnsTraceResolveOutbound = "freenet-trace-resolve"
)

type dnsPathTraceResponse struct {
	Success             bool     `json:"success"`
	Host                string   `json:"host"`
	DNSMode             string   `json:"dns_mode"`
	ActiveSplit         bool     `json:"active_split"`
	ManagedPolicyMirror bool     `json:"managed_policy_mirror"`
	PayloadAction       string   `json:"payload_action,omitempty"`
	PayloadOutbound     string   `json:"payload_outbound,omitempty"`
	DNSSelector         string   `json:"dns_selector,omitempty"`
	DNSUpstream         string   `json:"dns_upstream,omitempty"`
	DNSExpectedOutbound string   `json:"dns_expected_outbound,omitempty"`
	DNSObservedOutbound string   `json:"dns_observed_outbound,omitempty"`
	DNSMatchedRules     []string `json:"dns_matched_rules,omitempty"`
	PolicyParity        bool     `json:"policy_parity"`
	Reason              string   `json:"reason,omitempty"`
	Mutation            string   `json:"mutation"`
	Error               string   `json:"error,omitempty"`
}

type dnsTraceServer struct {
	Tag     string
	Address string
	Host    string
}

func registerDNSPathTrace(mux *http.ServeMux, a *app) {
	mux.HandleFunc("GET /api/dns/path-trace", a.requireAuth(a.handleDNSPathTrace))
}

func (a *app) handleDNSPathTrace(w http.ResponseWriter, r *http.Request) {
	host, err := normalizeDNSPathTraceHost(r.URL.Query().Get("host"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, dnsPathTraceResponse{Success: false, Host: host, Mutation: "NONE", Error: err.Error()})
		return
	}

	_, dnsMode := readNetworkProfileConfig(a.cfg.ConfigPath)
	if dnsMode != "xkeen" {
		writeJSON(w, http.StatusConflict, dnsPathTraceResponse{Success: false, Host: host, DNSMode: dnsMode, Mutation: "NONE", Error: "DNS path trace доступен только при активном XKeen/Xray DNS"})
		return
	}

	plan, err := a.runNetworkPlan()
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, dnsPathTraceResponse{Success: false, Host: host, DNSMode: dnsMode, Mutation: "NONE", Error: "не удалось подтвердить текущий read-only сетевой план"})
		return
	}
	if !plan.Active || plan.EffectiveDNSMode != "xkeen" {
		reason := networkPlanActiveMismatch(plan)
		if reason == "" {
			reason = "Split DNS runtime не подтверждён"
		}
		writeJSON(w, http.StatusConflict, dnsPathTraceResponse{Success: false, Host: host, DNSMode: dnsMode, Mutation: "NONE", Error: reason})
		return
	}

	currentDNS, routing, err := readManagedSplitTraceObjects()
	if err != nil {
		writeJSON(w, http.StatusConflict, dnsPathTraceResponse{Success: false, Host: host, DNSMode: dnsMode, ActiveSplit: true, Mutation: "NONE", Error: err.Error()})
		return
	}
	expectedDNS, err := expectedFreeNetManagedSplitDNS(routing)
	if err != nil || !reflect.DeepEqual(currentDNS, expectedDNS) {
		writeJSON(w, http.StatusConflict, dnsPathTraceResponse{Success: false, Host: host, DNSMode: dnsMode, ActiveSplit: true, Mutation: "NONE", Error: "current 02_dns не является точным зеркалом текущей FreeNet routing policy; trace STOP без догадки"})
		return
	}

	logText, err := runReadOnlyXrayPathProbe(host, currentDNS, routing)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, dnsPathTraceResponse{Success: false, Host: host, DNSMode: dnsMode, ActiveSplit: true, ManagedPolicyMirror: true, Mutation: "NONE", Error: err.Error()})
		return
	}

	trace, err := parseDNSPathTrace(host, logText, currentDNS, routing)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, dnsPathTraceResponse{Success: false, Host: host, DNSMode: dnsMode, ActiveSplit: true, ManagedPolicyMirror: true, Mutation: "NONE", Error: err.Error()})
		return
	}
	trace.Success = true
	trace.DNSMode = dnsMode
	trace.ActiveSplit = true
	trace.ManagedPolicyMirror = true
	trace.Mutation = "NONE"
	writeJSON(w, http.StatusOK, trace)
}

func normalizeDNSPathTraceHost(raw string) (string, error) {
	host := strings.ToLower(strings.TrimSpace(raw))
	host = strings.TrimSuffix(host, ".")
	if host == "" || len(host) > 253 || strings.Contains(host, "://") || strings.ContainsAny(host, "/\\?#@") {
		return host, errors.New("укажите корректное DNS-имя")
	}
	if net.ParseIP(host) != nil {
		return host, errors.New("trace ожидает hostname, а не IP-адрес")
	}
	labels := strings.Split(host, ".")
	if len(labels) < 2 {
		return host, errors.New("укажите полное DNS-имя")
	}
	for _, label := range labels {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return host, errors.New("укажите корректное DNS-имя")
		}
		for _, c := range label {
			if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' {
				continue
			}
			return host, errors.New("укажите корректное DNS-имя")
		}
	}
	return host, nil
}

func readManagedSplitTraceObjects() (map[string]any, map[string]any, error) {
	dir := legacyNativeConfigDir()
	currentDNS, err := legacyNativeReadJSONObject(filepath.Join(dir, "02_dns.json"))
	if err != nil {
		return nil, nil, errors.New("не удалось безопасно прочитать current Split 02_dns")
	}
	routing, err := legacyNativeReadJSONObject(filepath.Join(dir, "05_routing.json"))
	if err != nil {
		return nil, nil, errors.New("не удалось безопасно прочитать current Split routing")
	}
	return currentDNS, routing, nil
}

func cloneJSONObject(src map[string]any) (map[string]any, error) {
	data, err := json.Marshal(src)
	if err != nil {
		return nil, err
	}
	var dst map[string]any
	if err := json.Unmarshal(data, &dst); err != nil {
		return nil, err
	}
	return dst, nil
}

func routingRuleObjects(routing map[string]any) ([]map[string]any, error) {
	routingObj, ok := routing["routing"].(map[string]any)
	if !ok {
		return nil, errors.New("routing object отсутствует")
	}
	return legacyNativeObjectSlice(routingObj, "rules")
}

func traceOutboundTags(routing map[string]any) ([]string, error) {
	rules, err := routingRuleObjects(routing)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var tags []string
	for _, rule := range rules {
		tag := legacyNativeString(rule["outboundTag"])
		if tag == "" || tag == dnsTraceResolveOutbound || seen[tag] {
			continue
		}
		seen[tag] = true
		tags = append(tags, tag)
	}
	for _, tag := range []string{"direct", "vless-reality", "dns-out"} {
		if !seen[tag] {
			seen[tag] = true
			tags = append(tags, tag)
		}
	}
	return tags, nil
}

func buildReadOnlyXrayTraceConfig(payloadPort, dnsPort int, currentDNS, routing map[string]any) (map[string]any, error) {
	dnsObj, ok := currentDNS["dns"].(map[string]any)
	if !ok {
		return nil, errors.New("current Split DNS object отсутствует")
	}
	routingClone, err := cloneJSONObject(routing)
	if err != nil {
		return nil, err
	}
	routingObj, ok := routingClone["routing"].(map[string]any)
	if !ok {
		return nil, errors.New("routing object отсутствует")
	}
	rawRules, ok := routingObj["rules"].([]any)
	if !ok {
		return nil, errors.New("routing rules отсутствуют")
	}
	forcedDNS := map[string]any{"type": "field", "inboundTag": []any{dnsTraceDNSInbound}, "outboundTag": dnsTraceResolveOutbound}
	routingObj["rules"] = append([]any{forcedDNS}, rawRules...)

	tags, err := traceOutboundTags(routing)
	if err != nil {
		return nil, err
	}
	outbounds := make([]any, 0, len(tags)+1)
	outbounds = append(outbounds, map[string]any{
		"tag":      dnsTraceResolveOutbound,
		"protocol": "freedom",
		"settings": map[string]any{"domainStrategy": "UseIPv4"},
	})
	for _, tag := range tags {
		outbounds = append(outbounds, map[string]any{"tag": tag, "protocol": "blackhole", "settings": map[string]any{}})
	}

	return map[string]any{
		"log": map[string]any{"loglevel": "debug"},
		"dns": dnsObj,
		"inbounds": []any{
			map[string]any{"tag": dnsTracePayloadInbound, "listen": "127.0.0.1", "port": payloadPort, "protocol": "http", "settings": map[string]any{}},
			map[string]any{"tag": dnsTraceDNSInbound, "listen": "127.0.0.1", "port": dnsPort, "protocol": "http", "settings": map[string]any{}},
		},
		"outbounds": outbounds,
		"routing":   routingObj,
	}, nil
}

func reserveTraceTCPPort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port, nil
}

func waitTraceTCPPort(port int, deadline time.Time) error {
	address := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", address, 80*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return nil
		}
		time.Sleep(40 * time.Millisecond)
	}
	return errors.New("temporary Xray trace listener did not start")
}

func sendTraceHTTPProxyRequest(port int, host string) {
	proxyURL := &url.URL{Scheme: "http", Host: net.JoinHostPort("127.0.0.1", strconv.Itoa(port))}
	transport := &http.Transport{Proxy: http.ProxyURL(proxyURL), DisableKeepAlives: true}
	client := &http.Client{Transport: transport, Timeout: 900 * time.Millisecond}
	req, err := http.NewRequest(http.MethodGet, "http://"+host+"/", nil)
	if err != nil {
		return
	}
	resp, err := client.Do(req)
	if err == nil && resp != nil {
		_ = resp.Body.Close()
	}
	transport.CloseIdleConnections()
}

func runReadOnlyXrayPathProbe(host string, currentDNS, routing map[string]any) (string, error) {
	payloadPort, err := reserveTraceTCPPort()
	if err != nil {
		return "", errors.New("не удалось выделить локальный trace port")
	}
	dnsPort, err := reserveTraceTCPPort()
	if err != nil || dnsPort == payloadPort {
		return "", errors.New("не удалось выделить второй локальный trace port")
	}
	cfg, err := buildReadOnlyXrayTraceConfig(payloadPort, dnsPort, currentDNS, routing)
	if err != nil {
		return "", err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return "", errors.New("не удалось построить read-only trace config")
	}
	tmp, err := os.MkdirTemp("", "freenet-dns-trace-*")
	if err != nil {
		return "", errors.New("не удалось создать временный trace workspace")
	}
	defer os.RemoveAll(tmp)
	if err := os.WriteFile(filepath.Join(tmp, "trace.json"), append(data, '\n'), 0o600); err != nil {
		return "", errors.New("не удалось записать временный trace config")
	}

	testCtx, testCancel := context.WithTimeout(context.Background(), 8*time.Second)
	testCmd := exec.CommandContext(testCtx, legacyNativeXrayBin(), "run", "-test", "-confdir", tmp)
	testCmd.Env = append(os.Environ(), "XRAY_LOCATION_ASSET="+legacyNativeXrayAssetDir())
	testErr := testCmd.Run()
	testCancel()
	if testErr != nil {
		return "", errors.New("временный read-only Xray trace candidate не прошёл validation")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, legacyNativeXrayBin(), "run", "-confdir", tmp)
	cmd.Env = append(os.Environ(), "XRAY_LOCATION_ASSET="+legacyNativeXrayAssetDir())
	var logs bytes.Buffer
	cmd.Stdout = &logs
	cmd.Stderr = &logs
	if err := cmd.Start(); err != nil {
		return "", errors.New("не удалось запустить временный read-only Xray trace")
	}
	started := time.Now().Add(1800 * time.Millisecond)
	if err := waitTraceTCPPort(payloadPort, started); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return "", err
	}
	if err := waitTraceTCPPort(dnsPort, started); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return "", err
	}

	// The first request exercises the live ordered payload routing policy. All
	// candidate outbounds are blackholes, so no payload leaves the router.
	sendTraceHTTPProxyRequest(payloadPort, host)
	// The second request is forced through a temporary freedom/UseIPv4 outbound.
	// That makes Xray's own DNS matcher select the resolver, while the real
	// dns-direct/dns-vless egress tags are blackholed. This observes the decision
	// without touching live config, credentials, :53 ownership or persistent state.
	sendTraceHTTPProxyRequest(dnsPort, host)
	time.Sleep(250 * time.Millisecond)
	_ = cmd.Process.Kill()
	_ = cmd.Wait()
	return logs.String(), nil
}

func dnsTraceServers(currentDNS map[string]any) ([]dnsTraceServer, error) {
	dnsObj, ok := currentDNS["dns"].(map[string]any)
	if !ok {
		return nil, errors.New("DNS object отсутствует")
	}
	serversRaw, ok := dnsObj["servers"].([]any)
	if !ok {
		return nil, errors.New("DNS servers отсутствуют")
	}
	servers := make([]dnsTraceServer, 0, len(serversRaw))
	for _, raw := range serversRaw {
		server, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		address := legacyNativeString(server["address"])
		tag := legacyNativeString(server["tag"])
		if address == "" || tag == "" {
			continue
		}
		host := address
		if u, err := url.Parse(address); err == nil && u.Hostname() != "" {
			host = u.Hostname()
		} else if parsedHost, _, err := net.SplitHostPort(address); err == nil {
			host = parsedHost
		}
		servers = append(servers, dnsTraceServer{Tag: tag, Address: address, Host: host})
	}
	if len(servers) == 0 {
		return nil, errors.New("DNS server catalog пуст")
	}
	return servers, nil
}

func outboundForInboundTag(routing map[string]any, inboundTag string) string {
	rules, err := routingRuleObjects(routing)
	if err != nil {
		return ""
	}
	for _, rule := range rules {
		tags, ok := rule["inboundTag"].([]any)
		if !ok {
			continue
		}
		for _, raw := range tags {
			if legacyNativeString(raw) == inboundTag {
				return legacyNativeString(rule["outboundTag"])
			}
		}
	}
	return ""
}

func extractDetourTag(line string) string {
	const marker = "taking detour ["
	lower := strings.ToLower(line)
	idx := strings.Index(lower, marker)
	if idx < 0 {
		return ""
	}
	start := idx + len(marker)
	end := strings.Index(line[start:], "]")
	if end < 0 {
		return ""
	}
	return strings.TrimSpace(line[start : start+end])
}

func parseDNSMatchedRules(text string) []string {
	text = strings.TrimSpace(text)
	if len(text) > 800 {
		text = text[:800]
	}
	text = strings.Trim(text, "[]")
	if text == "" {
		return nil
	}
	parts := strings.Split(text, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func parseDNSPathTrace(host, logText string, currentDNS, routing map[string]any) (dnsPathTraceResponse, error) {
	trace := dnsPathTraceResponse{Host: host, Mutation: "NONE"}
	lowerHost := strings.ToLower(host)
	var dnsDecision string
	for _, raw := range strings.Split(strings.ReplaceAll(logText, "\r", ""), "\n") {
		line := strings.TrimSpace(raw)
		lower := strings.ToLower(line)
		if strings.Contains(lower, "taking detour [") && strings.Contains(lower, "for [tcp:"+lowerHost+":") {
			tag := extractDetourTag(line)
			if tag != "" && tag != dnsTraceResolveOutbound && trace.PayloadOutbound == "" {
				trace.PayloadOutbound = tag
			}
		}
		decisionMarker := "domain " + lowerHost + " will use dns in order:"
		if idx := strings.Index(lower, decisionMarker); idx >= 0 {
			dnsDecision = strings.TrimSpace(line[idx+len(decisionMarker):])
		}
		rulesMarker := "domain " + lowerHost + " matches following rules:"
		if idx := strings.Index(lower, rulesMarker); idx >= 0 {
			trace.DNSMatchedRules = parseDNSMatchedRules(line[idx+len(rulesMarker):])
		}
	}
	if trace.PayloadOutbound == "" {
		return trace, errors.New("Xray trace не определил payload outbound для hostname")
	}
	switch trace.PayloadOutbound {
	case "direct":
		trace.PayloadAction = "DIRECT"
	case "vless-reality":
		trace.PayloadAction = "VPN"
	case "block":
		trace.PayloadAction = "BLOCK"
	default:
		trace.PayloadAction = strings.ToUpper(trace.PayloadOutbound)
	}
	if dnsDecision == "" {
		return trace, errors.New("Xray trace не зафиксировал DNS selector decision")
	}

	servers, err := dnsTraceServers(currentDNS)
	if err != nil {
		return trace, err
	}
	decisionLower := strings.ToLower(dnsDecision)
	for _, server := range servers {
		if strings.Contains(decisionLower, strings.ToLower(server.Address)) || strings.Contains(decisionLower, strings.ToLower(server.Host)) {
			trace.DNSSelector = server.Tag
			trace.DNSUpstream = server.Address
			break
		}
	}
	if trace.DNSSelector == "" {
		return trace, errors.New("DNS selector decision не сопоставился с current 02_dns")
	}
	trace.DNSExpectedOutbound = outboundForInboundTag(routing, trace.DNSSelector)
	if trace.DNSExpectedOutbound == "" {
		return trace, errors.New("для выбранного DNS selector не найден routing outbound")
	}

	upstreamHost := ""
	for _, server := range servers {
		if server.Tag == trace.DNSSelector && server.Address == trace.DNSUpstream {
			upstreamHost = strings.ToLower(server.Host)
			break
		}
	}
	if upstreamHost != "" {
		for _, raw := range strings.Split(strings.ReplaceAll(logText, "\r", ""), "\n") {
			line := strings.TrimSpace(raw)
			lower := strings.ToLower(line)
			if !strings.Contains(lower, "taking detour [") || !strings.Contains(lower, upstreamHost) {
				continue
			}
			tag := extractDetourTag(line)
			if tag == "direct" || tag == "vless-reality" {
				trace.DNSObservedOutbound = tag
				break
			}
		}
	}
	if trace.DNSObservedOutbound == "" {
		return trace, errors.New("Xray trace не подтвердил observed DNS outbound")
	}

	expectedSelector := ""
	switch trace.PayloadOutbound {
	case "direct":
		expectedSelector = "dns-direct"
	case "vless-reality":
		expectedSelector = "dns-vless"
	}
	trace.PolicyParity = expectedSelector != "" && trace.DNSSelector == expectedSelector && trace.DNSObservedOutbound == trace.DNSExpectedOutbound
	if trace.PolicyParity {
		trace.Reason = fmt.Sprintf("payload %s -> %s; DNS %s -> %s -> %s", trace.PayloadAction, trace.PayloadOutbound, trace.DNSSelector, trace.DNSUpstream, trace.DNSObservedOutbound)
	} else {
		trace.Reason = fmt.Sprintf("policy mismatch: payload=%s/%s; expected DNS selector=%s; observed DNS=%s -> %s", trace.PayloadAction, trace.PayloadOutbound, expectedSelector, trace.DNSSelector, trace.DNSObservedOutbound)
	}
	return trace, nil
}
