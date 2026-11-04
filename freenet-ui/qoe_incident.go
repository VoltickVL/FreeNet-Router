package main

import (
	"context"
	"net/http"
	"os"
	"strings"
	"time"
)

// A passive, privacy-safe incident receipt. Kernel rules and router health
// cannot prove how a particular LAN client reaches Instagram or Telegram.
// Neither this handler nor the UI runs a media probe or changes VPN/DNS/routing.
type qoeIncidentSnapshot struct {
	Success         bool   `json:"success"`
	Mutation        string `json:"mutation"`
	Service         string `json:"service"`
	ObservedAt      string `json:"observed_at"`
	CaptureState    string `json:"capture_state"`
	TCPRedirect     string `json:"tcp_redirect"`
	TCPPrerouting   string `json:"tcp_prerouting"`
	KernelTPROXY    string `json:"kernel_tproxy"`
	PolicyRule      string `json:"policy_rule"`
	LocalRoute      string `json:"local_route"`
	LANExclude      string `json:"lan_exclude"`
	DNSConfigured   string `json:"dns_configured"`
	DNSRuntime      string `json:"dns_runtime"`
	VPNHealth       string `json:"vpn_health"`
	VPNHealthAt     string `json:"vpn_health_at,omitempty"`
	ClientEgress    string `json:"client_egress"`
	ClientTransport string `json:"client_transport"`
	Playback        string `json:"playback"`
	NextEvidence    string `json:"next_evidence"`
}

func qoeIncidentService(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "instagram":
		return "instagram"
	case "telegram":
		return "telegram"
	}
	return ""
}

func qoeIncidentEnum(raw string, valid ...string) string {
	for _, item := range valid {
		if raw == item {
			return item
		}
	}
	return "unknown"
}

func qoeIncidentHealthAt(raw string, now time.Time) string {
	when, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(raw))
	if err != nil || when.After(now.Add(time.Minute)) || when.IsZero() {
		return ""
	}
	return when.UTC().Format(time.RFC3339)
}

func buildQoEIncidentSnapshot(service, dns string, capture captureDoctorResponse, state map[string]string, now time.Time) qoeIncidentSnapshot {
	return qoeIncidentSnapshot{
		Success: true, Mutation: "NONE", Service: service,
		ObservedAt: now.UTC().Format(time.RFC3339),
		CaptureState: qoeIncidentEnum(capture.State, "tcp-redirect-missing", "tcp-hook-missing", "capture-rule-missing", "policy-rule-missing", "local-route-missing", "installed-unverified"),
		TCPRedirect: qoeIncidentEnum(capture.TCPRedirect, "tcp-redirect-present", "tcp-redirect-absent"),
		TCPPrerouting: qoeIncidentEnum(capture.TCPPrerouting, "tcp-prerouting-hook-present", "tcp-prerouting-hook-absent", "tcp-prerouting-invalid-only"),
		KernelTPROXY: qoeIncidentEnum(capture.Netfilter, "tproxy-rule-present", "tproxy-rule-absent"),
		PolicyRule: qoeIncidentEnum(capture.PolicyRule, "fwmark-rule-present", "fwmark-rule-absent"),
		LocalRoute: qoeIncidentEnum(capture.LocalRoute, "local-default-present", "local-default-absent"),
		LANExclude: qoeIncidentEnum(capture.LANExclude, "both", "disk-only", "runtime-only", "disk-risk-runtime-unknown", "runtime-risk-disk-unknown", "no-overlap", "unverified"),
		DNSConfigured: qoeIncidentEnum(dns, "auto", "firmware", "xkeen", "custom"),
		DNSRuntime: "unknown",
		VPNHealth: qoeIncidentEnum(strings.ToLower(strings.TrimSpace(state["HEALTH_RESULT"])), "healthy", "failed", "uncertain", "critical"),
		VPNHealthAt: qoeIncidentHealthAt(state["HEALTH_LAST"], now),
		ClientEgress: "unknown",
		ClientTransport: "unknown",
		Playback: "unknown",
		NextEvidence: "client-egress-dns-cdn-udp-quic-playback",
	}
}

func registerQoEIncident(mux *http.ServeMux, a *app) {
	mux.HandleFunc("GET /api/qoe/incident", a.requireAuth(a.handleQoEIncident))
}

func (a *app) handleQoEIncident(w http.ResponseWriter, r *http.Request) {
	service := qoeIncidentService(r.URL.Query().Get("service"))
	if service == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"success": false, "error": "Выберите Instagram или Telegram.", "mutation": "NONE",
		})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()
	capture := captureDoctorClassify(ctx, captureDoctorRead, xrayDiagnosticHookState("/opt/etc/ndm/netfilter.d/proxy.sh"))
	capture.LANExclude, capture.LANExcludeReason = captureDoctorLANExclude(ctx, captureDoctorRead, os.ReadFile)
	writeJSON(w, http.StatusOK, buildQoEIncidentSnapshot(
		service, readDNSModeConfig(a.cfg.ConfigPath), capture,
		v3ParseState(settingsV3StatePath()), time.Now().UTC(),
	))
}
