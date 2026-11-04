package main

import (
	"bufio"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"os"
	"strings"
)

const qoeConntrackMaxBytes = 256 << 10

// This is a bounded observation of existing conntrack entries, not a capture,
// packet count, proof of QUIC, VPN egress or application playback.
func qoeClient443Peer(r *http.Request) (netip.Addr, bool) {
	// A reverse proxy can make its own private address look like the client.
	// Do not trust forwarded identities, even when the socket peer is private.
	for _, header := range []string{"Forwarded", "X-Forwarded-For", "X-Real-IP", "X-Forwarded-Host"} {
		if r.Header.Get(header) != "" {
			return netip.Addr{}, false
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return netip.Addr{}, false
	}
	ip, err := netip.ParseAddr(host)
	if err != nil || !ip.Is4() || !ip.IsPrivate() || ip.IsLoopback() || ip.Is4In6() {
		return netip.Addr{}, false
	}
	return ip, true
}

func qoeConntrackLine443(line string, peer netip.Addr) string {
	fields := strings.Fields(line)
	if len(fields) < 8 {
		return ""
	}
	proto := ""
	for _, field := range fields[:min(len(fields), 5)] {
		if field == "tcp" || field == "udp" {
			proto = field
			break
		}
	}
	if proto == "" {
		return ""
	}
	// Conntrack records contain an original tuple, then a reply tuple.
	// Only the original direction is meaningful for the HTTP socket peer.
	var src, dst, dport string
	for _, field := range fields {
		if strings.HasPrefix(field, "src=") && src == "" {
			src = strings.TrimPrefix(field, "src=")
		}
		if strings.HasPrefix(field, "dst=") && dst == "" {
			dst = strings.TrimPrefix(field, "dst=")
		}
		if strings.HasPrefix(field, "dport=") && dport == "" {
			dport = strings.TrimPrefix(field, "dport=")
		}
		if src != "" && dst != "" && dport != "" {
			break
		}
	}
	if src != peer.String() || dport != "443" {
		return ""
	}
	target, err := netip.ParseAddr(dst)
	if err != nil || !target.Is4() || !target.IsGlobalUnicast() || target.IsPrivate() ||
		target.IsLoopback() || target.IsLinkLocalUnicast() {
		return ""
	}
	return proto
}

func qoeClassifyConntrack443(reader io.Reader, peer netip.Addr) string {
	// Read the whole bounded snapshot: a partial table must not masquerade as
	// "no flows", and malformed/truncated kernel output fails closed.
	data, err := io.ReadAll(io.LimitReader(reader, qoeConntrackMaxBytes+1))
	if err != nil || len(data) > qoeConntrackMaxBytes {
		return "unknown"
	}
	if len(data) > 0 && data[len(data)-1] != '\n' {
		return "unknown"
	}
	var tcp, udp bool
	s := bufio.NewScanner(strings.NewReader(string(data)))
	s.Buffer(make([]byte, 4096), qoeConntrackMaxBytes)
	for s.Scan() {
		switch qoeConntrackLine443(s.Text(), peer) {
		case "tcp":
			tcp = true
		case "udp":
			udp = true
		}
	}
	if s.Err() != nil {
		return "unknown"
	}
	switch {
	case tcp && udp:
		return "tcp-and-udp"
	case tcp:
		return "tcp-only"
	case udp:
		return "udp-only"
	default:
		return "no-443-flows"
	}
}

func qoeReadClient443(r *http.Request, open func(string) (io.ReadCloser, error)) string {
	peer, ok := qoeClient443Peer(r)
	if !ok {
		return "unknown"
	}
	for _, path := range []string{"/proc/net/nf_conntrack", "/proc/net/ip_conntrack"} {
		fd, err := open(path)
		if err != nil {
			continue
		}
		state := qoeClassifyConntrack443(fd, peer)
		closeErr := fd.Close()
		if closeErr != nil || state == "unknown" {
			return "unknown"
		}
		return state
	}
	return "unknown"
}

func qoeReadProcConntrack(path string) (io.ReadCloser, error) {
	if path != "/proc/net/nf_conntrack" && path != "/proc/net/ip_conntrack" {
		return nil, errors.New("conntrack path not allowed")
	}
	return os.Open(path)
}
