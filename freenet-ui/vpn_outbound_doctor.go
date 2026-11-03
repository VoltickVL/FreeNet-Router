package main

import (
	"context"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

type vpnOutboundDoctorResponse struct {
	Success               bool     `json:"success"`
	Mutation              string   `json:"mutation"`
	CurrentLabel          string   `json:"current_label,omitempty"`
	FreshProfiles         int      `json:"fresh_profiles"`
	CatalogFresh          bool     `json:"catalog_fresh"`
	CatalogSource         string   `json:"catalog_source,omitempty"`
	ChangedFields         []string `json:"changed_fields,omitempty"`
	FreshCurrentFound     bool     `json:"fresh_current_found"`
	Transport             string   `json:"transport,omitempty"`
	Security              string   `json:"security,omitempty"`
	QueryKeys             []string `json:"query_keys,omitempty"`
	TransportModel        string   `json:"transport_model,omitempty"`
	BuilderValid          bool     `json:"builder_valid"`
	ActiveMatchesFresh    bool     `json:"active_matches_fresh"`
	EndpointTCP           bool     `json:"endpoint_tcp"`
	FreshHTTPS            bool     `json:"fresh_https"`
	FreshTransportOnly    bool     `json:"fresh_transport_only"`
	Diagnosis             string   `json:"diagnosis"`
	Error                 string   `json:"error,omitempty"`
}

type vpnOutboundFingerprint struct {
	Address, ID, Flow, Encryption string
	Network, Security             string
	Fingerprint, ServerName       string
	PublicKey, ShortID, SpiderX   string
	Port                          int
}

func registerVPNOutboundDoctor(mux *http.ServeMux, a *app) {
	mux.HandleFunc("GET /api/vpn/runtime-doctor", a.requireAuth(a.handleVPNOutboundDoctor))
}

func safeVLESSQuerySchema(raw string) (transport, security string, keys []string) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", "", nil
	}
	q := u.Query()
	transport = strings.TrimSpace(q.Get("type"))
	if transport == "" {
		transport = "tcp"
	}
	security = strings.TrimSpace(q.Get("security"))
	if security == "" {
		security = "reality"
	}
	for key := range q {
		key = strings.TrimSpace(key)
		if key != "" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return transport, security, keys
}

func vpnTransportModel(transport string) string {
	switch strings.ToLower(strings.TrimSpace(transport)) {
	case "", "tcp", "raw":
		return "fully-modeled"
	default:
		return "generic-network-only"
	}
}

func anyString(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	return strings.TrimSpace(toString(m[key]))
}

func toString(v any) string {
	switch value := v.(type) {
	case string:
		return value
	case []byte:
		return string(value)
	default:
		return ""
	}
}

func anyInt(v any) int {
	switch value := v.(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	case jsonNumber:
		n, _ := strconv.Atoi(string(value))
		return n
	default:
		return 0
	}
}

// jsonNumber is deliberately local: active outbounds are decoded through
// encoding/json as float64 today, while fresh builder maps contain ints.
type jsonNumber string

func fingerprintVLESSOutbound(outbound map[string]any) (vpnOutboundFingerprint, bool) {
	var fp vpnOutboundFingerprint
	if anyString(outbound, "protocol") != "vless" {
		return fp, false
	}
	settings, _ := outbound["settings"].(map[string]any)
	vnext, _ := settings["vnext"].([]any)
	if len(vnext) != 1 {
		return fp, false
	}
	server, _ := vnext[0].(map[string]any)
	fp.Address = anyString(server, "address")
	fp.Port = anyInt(server["port"])
	users, _ := server["users"].([]any)
	if len(users) != 1 {
		return fp, false
	}
	user, _ := users[0].(map[string]any)
	fp.ID = anyString(user, "id")
	fp.Flow = anyString(user, "flow")
	fp.Encryption = anyString(user, "encryption")

	stream, _ := outbound["streamSettings"].(map[string]any)
	fp.Network = anyString(stream, "network")
	fp.Security = anyString(stream, "security")
	reality, _ := stream["realitySettings"].(map[string]any)
	fp.Fingerprint = anyString(reality, "fingerprint")
	fp.ServerName = anyString(reality, "serverName")
	fp.PublicKey = anyString(reality, "publicKey")
	fp.ShortID = anyString(reality, "shortId")
	fp.SpiderX = anyString(reality, "spiderX")
	return fp, fp.Address != "" && fp.Port > 0 && fp.ID != ""
}

// vlessOutboundChangedFields returns only a fixed allowlist of field names.
// Never serialize or log fingerprint values, endpoints, UUID or Reality keys.
func vlessOutboundChangedFields(active, fresh map[string]any) []string {
	a, okA := fingerprintVLESSOutbound(active)
	f, okF := fingerprintVLESSOutbound(fresh)
	if !okA || !okF {
		return []string{"vless.outbound_structure"}
	}
	checks := []struct {
		name string
		different bool
	}{
		{"endpoint.address", a.Address != f.Address},
		{"endpoint.port", a.Port != f.Port},
		{"vless.id", a.ID != f.ID},
		{"vless.flow", a.Flow != f.Flow},
		{"vless.encryption", a.Encryption != f.Encryption},
		{"transport.network", a.Network != f.Network},
		{"transport.security", a.Security != f.Security},
		{"reality.fingerprint", a.Fingerprint != f.Fingerprint},
		{"reality.serverName", a.ServerName != f.ServerName},
		{"reality.publicKey", a.PublicKey != f.PublicKey},
		{"reality.shortId", a.ShortID != f.ShortID},
		{"reality.spiderX", a.SpiderX != f.SpiderX},
	}
	out := make([]string, 0, len(checks))
	for _, check := range checks {
		if check.different {
			out = append(out, check.name)
		}
	}
	return out
}

func vlessOutboundParity(active, fresh map[string]any) bool {
	a, okA := fingerprintVLESSOutbound(active)
	f, okF := fingerprintVLESSOutbound(fresh)
	return okA && okF && a == f
}

func (a *app) handleVPNOutboundDoctor(w http.ResponseWriter, r *http.Request) {
	resp := vpnOutboundDoctorResponse{Mutation: "NONE"}
	ctx, cancel := context.WithTimeout(r.Context(), 35*time.Second)
	defer cancel()

	active, endpoint, ok := readBestServerActiveOutbound(a.cfg.OutPath)
	if !ok {
		resp.Diagnosis = "active_outbound_unreadable"
		resp.Error = "Активный VLESS outbound не удалось безопасно прочитать."
		writeJSON(w, http.StatusConflict, resp)
		return
	}
	resp.CurrentLabel = currentExactProfileLabel(a.cfg.FilterPath)

	candidates, _, _, source, err := a.discoverBestServerCandidatesWithSource(ctx)
	if err != nil {
		resp.Diagnosis = "fresh_subscription_unavailable"
		resp.Error = "Свежую подписку не удалось получить для read-only диагностики."
		writeJSON(w, http.StatusServiceUnavailable, resp)
		return
	}
	resp.FreshProfiles = len(candidates)
	resp.CatalogFresh = source.Fresh
	resp.CatalogSource = source.Source
	index := bestServerCurrentCandidateIndex(candidates, endpoint, readBestServerCurrentFilter(a.cfg.FilterPath))
	if index < 0 {
		resp.Diagnosis = "active_profile_not_in_fresh_subscription"
		resp.Error = "Активный профиль не найден однозначно в свежей подписке."
		writeJSON(w, http.StatusOK, resp)
		return
	}
	resp.FreshCurrentFound = true
	current := candidates[index]
	resp.Transport, resp.Security, resp.QueryKeys = safeVLESSQuerySchema(current.Raw)
	resp.TransportModel = vpnTransportModel(resp.Transport)

	freshOutbound, buildErr := buildBestServerProbeOutbound(current.Raw, current.Profile)
	resp.BuilderValid = buildErr == nil
	if buildErr == nil {
		resp.ActiveMatchesFresh = vlessOutboundParity(active, freshOutbound)
		resp.ChangedFields = vlessOutboundChangedFields(active, freshOutbound)
	}

	tcpCtx, cancelTCP := context.WithTimeout(ctx, 3*time.Second)
	tcp := defaultBestServerTCPProbe(tcpCtx, current.Profile)
	cancelTCP()
	resp.EndpointTCP = tcp.OK

	if buildErr == nil {
		probeCtx, cancelProbe := context.WithTimeout(ctx, 12*time.Second)
		probe := a.probeBestServerApplicationPreflight(probeCtx, current)
		cancelProbe()
		resp.FreshHTTPS = probe.OK
		resp.FreshTransportOnly = probe.TransportOnly
	}

	switch {
	case !resp.CatalogFresh:
		resp.Diagnosis = "protected_subscription_cache_only"
	case buildErr != nil:
		resp.Diagnosis = "fresh_profile_builder_rejected"
	case resp.TransportModel != "fully-modeled":
		resp.Diagnosis = "fresh_transport_not_fully_modeled"
	case !resp.EndpointTCP:
		resp.Diagnosis = "provider_endpoint_tcp_failed"
	case resp.FreshHTTPS:
		if !resp.ActiveMatchesFresh {
			resp.Diagnosis = "active_outbound_stale_or_mismatched"
		} else {
			resp.Diagnosis = "isolated_profile_ok_live_path_failed"
		}
	case resp.FreshTransportOnly:
		resp.Diagnosis = "vless_transport_ok_named_https_failed"
	default:
		resp.Diagnosis = "fresh_vless_session_or_payload_failed"
	}
	resp.Success = true
	writeJSON(w, http.StatusOK, resp)
}
