package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func revisionFixture(sni, sid string) bestServerInternalCandidate {
	return bestServerInternalCandidate{
		Profile: subscriptionProfile{
			Address: "203.0.113.7", Port: 443, Name: "Extra test profile",
		},
		Raw: "vless://00000000-0000-4000-8000-000000000001@203.0.113.7:443" +
			"?type=tcp&security=reality&fp=chrome&flow=xtls-rprx-vision&pbk=test-public-key" +
			"&sni=" + sni + "&sid=" + sid + "&spx=%2F#Extra",
	}
}

func TestVPNRevisionDiffReportsOnlyAllowlistedFieldNames(t *testing.T) {
	activeCandidate := revisionFixture("old.example", "aaaa")
	freshCandidate := revisionFixture("new.example", "bbbb")
	active, err := buildBestServerProbeOutbound(activeCandidate.Raw, activeCandidate.Profile)
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := buildBestServerProbeOutbound(freshCandidate.Raw, freshCandidate.Profile)
	if err != nil {
		t.Fatal(err)
	}
	got := vlessOutboundChangedFields(active, fresh)
	want := []string{"reality.serverName", "reality.shortId"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("changed field names=%v, want=%v", got, want)
	}
	if vlessOutboundParity(active, fresh) {
		t.Fatal("rotated Reality settings must never match the current outbound")
	}
	b, _ := json.Marshal(vpnOutboundDoctorResponse{ChangedFields: got})
	for _, forbidden := range []string{"old.example", "new.example", "aaaa", "bbbb", "test-public-key", "00000000-0000-4000-8000-000000000001"} {
		if strings.Contains(string(b), forbidden) {
			t.Fatal("doctor response unexpectedly includes a credential-bearing value")
		}
	}
	if !vlessOutboundParity(active, active) || len(vlessOutboundChangedFields(active, active)) != 0 {
		t.Fatal("identical outbounds should match without changed fields")
	}
	if !reflect.DeepEqual(vlessOutboundChangedFields(map[string]any{}, fresh), []string{"vless.outbound_structure"}) {
		t.Fatal("unreadable active outbound must report a safe structural mismatch")
	}
}

func TestCurrentRefreshRequiresActuallyFreshProviderCatalog(t *testing.T) {
	cases := []struct {
		name string
		source bestServerCatalogSource
		want bool
	}{
		{"direct_fresh", bestServerCatalogSource{Fresh: true, Source: "direct"}, true},
		{"vpn_fresh", bestServerCatalogSource{Fresh: true, Source: "active_vpn"}, true},
		{"protected_stale", bestServerCatalogSource{Fresh: false, Source: "protected_cache"}, false},
		{"mislabelled_protected", bestServerCatalogSource{Fresh: true, Source: "protected_cache"}, false},
		{"unverified_source", bestServerCatalogSource{Fresh: true, Source: ""}, false},
		{"direct_but_not_fresh", bestServerCatalogSource{Fresh: false, Source: "direct"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := bestServerRefreshCatalogAdmitted(tc.source); got != tc.want {
				t.Fatalf("source=%+v admitted=%v want=%v", tc.source, got, tc.want)
			}
		})
	}
}

func TestCurrentRefreshPostAcceptsOnlyExactVLESSRevision(t *testing.T) {
	fresh := revisionFixture("new.example", "bbbb")
	expected, err := buildBestServerProbeOutbound(fresh.Raw, fresh.Profile)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "04_outbounds.json")
	write := func(outbound map[string]any) {
		t.Helper()
		payload, err := json.Marshal(map[string]any{"outbounds": []any{outbound}})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, payload, 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(expected)
	if !bestServerRefreshPostExactIdentity(path, fresh) {
		t.Fatal("exact active outbound should pass post-apply acceptance")
	}
	staleCandidate := revisionFixture("old.example", "aaaa")
	stale, err := buildBestServerProbeOutbound(staleCandidate.Raw, staleCandidate.Profile)
	if err != nil {
		t.Fatal(err)
	}
	write(stale)
	if bestServerRefreshPostExactIdentity(path, fresh) {
		t.Fatal("stale Reality SNI/shortId at identical endpoint must fail closed")
	}
	if bestServerRefreshPostExactIdentity(filepath.Join(t.TempDir(), "missing.json"), fresh) {
		t.Fatal("missing active config must not be accepted")
	}
	if bestServerRefreshPostExactIdentity(path, bestServerInternalCandidate{}) {
		t.Fatal("invalid expected candidate must not be accepted")
	}
}

func TestVLESSWebSocketRevisionIdentity(t *testing.T) {
	makeCandidate := func(sni, host, path string) bestServerInternalCandidate {
		return bestServerInternalCandidate{
			Profile: subscriptionProfile{Address: "203.0.113.8", Port: 443, Name: "Extra WS test"},
			Raw: "vless://00000000-0000-4000-8000-000000000002@203.0.113.8:443" +
				"?security=tls&type=ws&fp=chrome&sni=" + sni +
				"&host=" + host + "&path=" + path + "#Extra",
		}
	}
	old := makeCandidate("old.example", "old-host.example", "%2Fold")
	revised := makeCandidate("new.example", "new-host.example", "%2Fnew")
	left, err := buildBestServerProbeOutbound(old.Raw, old.Profile)
	if err != nil {
		t.Fatal(err)
	}
	right, err := buildBestServerProbeOutbound(revised.Raw, revised.Profile)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"tls.serverName", "ws.path", "ws.host"}
	if got := vlessOutboundChangedFields(left, right); !reflect.DeepEqual(got, want) {
		t.Fatalf("changed fields=%v want=%v", got, want)
	}
	if vlessOutboundParity(left, right) {
		t.Fatal("TLS/WebSocket SNI, Host and Path revision cannot be silently accepted")
	}
}
