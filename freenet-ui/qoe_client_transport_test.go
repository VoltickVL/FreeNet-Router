package main

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
)

const qoeCTTCP = "ipv4 2 tcp 6 430000 ESTABLISHED src=192.168.50.22 dst=157.240.1.1 sport=51000 dport=443 src=157.240.1.1 dst=192.168.50.22 sport=443 dport=51000 [ASSURED] mark=0 use=1\n"
const qoeCTUDP = "ipv4 2 udp 17 115 src=192.168.50.22 dst=149.154.167.99 sport=51001 dport=443 src=149.154.167.99 dst=192.168.50.22 sport=443 dport=51001 mark=0 use=1\n"

func TestQoEConntrack443Classification(t *testing.T) {
	peer := netip.MustParseAddr("192.168.50.22")
	for _, tc := range []struct { name, raw, want string } {
		{"tcp", qoeCTTCP, "tcp-only"},
		{"udp", qoeCTUDP, "udp-only"},
		{"both", qoeCTTCP + qoeCTUDP, "tcp-and-udp"},
		{"none", "", "no-443-flows"},
		{"different-client", strings.ReplaceAll(qoeCTTCP, "192.168.50.22", "192.168.50.23"), "no-443-flows"},
		{"non-443", strings.Replace(qoeCTUDP, "dport=443", "dport=8443", 1), "no-443-flows"},
		{"private-destination", strings.Replace(qoeCTUDP, "dst=149.154.167.99", "dst=192.168.10.2", 1), "no-443-flows"},
		{"malformed", "udp src=bad dst=bad dport=443\n", "no-443-flows"},
		{"unfinished", strings.TrimSuffix(qoeCTTCP, "\n"), "unknown"},
		{"over-budget", strings.Repeat("x", qoeConntrackMaxBytes+1), "unknown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := qoeClassifyConntrack443(strings.NewReader(tc.raw),peer); got != tc.want {
				t.Fatalf("got %s, want %s", got, tc.want)
			}
		})
	}
	if got := qoeClassifyConntrack443(errorReader{},peer); got!="unknown" { t.Fatalf("read error should be UNKNOWN: %s",got) }
}

type errorReader struct{}
func (errorReader) Read([]byte)(int,error) { return 0,errors.New("read failed") }

func TestQoEClientPeerRejectsForwardedAndUntrusted(t *testing.T) {
	for _, peer := range []string{
		"8.8.8.8:123", "127.0.0.1:123", "[::1]:123",
		"[::ffff:192.168.50.22]:123", "invalid", "192.168.50.22",
	} {
		r:=httptest.NewRequest(http.MethodGet,"/api/qoe/incident?service=instagram",nil)
		r.RemoteAddr=peer
		if _,ok:=qoeClient443Peer(r);ok {t.Fatalf("untrusted peer accepted: %s",peer)}
	}
	for _,h:=range []string{"Forwarded","X-Forwarded-For","X-Real-IP","X-Forwarded-Host"} {
		r:=httptest.NewRequest(http.MethodGet,"/api/qoe/incident",nil)
		r.RemoteAddr="192.168.50.22:123"
		r.Header.Set(h,"192.168.50.22")
		if _,ok:=qoeClient443Peer(r);ok {t.Fatalf("forwarded header accepted: %s",h)}
	}
}

func TestQoEReadClient443FailuresAndNoIPLeak(t *testing.T){
	r:=httptest.NewRequest(http.MethodGet,"/api/qoe/incident?service=instagram",nil)
	r.RemoteAddr="192.168.50.22:123"
	for _,tc:=range []struct{
		name string; open func(string)(io.ReadCloser,error); want string
	}{
		{"missing",func(string)(io.ReadCloser,error){return nil,errors.New("missing")},"unknown"},
		{"fallback",func(p string)(io.ReadCloser,error){if strings.HasSuffix(p,"nf_conntrack"){return nil,errors.New("missing")};return io.NopCloser(strings.NewReader(qoeCTTCP)),nil},"tcp-only"},
		{"normal",func(string)(io.ReadCloser,error){return io.NopCloser(strings.NewReader(qoeCTTCP+qoeCTUDP)),nil},"tcp-and-udp"},
		{"partial",func(string)(io.ReadCloser,error){return io.NopCloser(strings.NewReader("incomplete")),nil},"unknown"},
		{"read-error",func(string)(io.ReadCloser,error){return io.NopCloser(errorReader{}),nil},"unknown"},
	} {
		t.Run(tc.name,func(t *testing.T){
			got:=qoeReadClient443(r,tc.open)
			if got!=tc.want {t.Fatalf("got %s want %s",got,tc.want)}
			j,_:=json.Marshal(map[string]string{"client_443_flows":got})
			if strings.Contains(string(j),"192.168.")||strings.Contains(string(j),"157.240."){t.Fatalf("address leaked: %s",j)}
		})
	}
}

