package main

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode"
)

const maxSubscriptionBytes = 2 * 1024 * 1024

type subscriptionProfile struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Address string `json:"address"`
	Port    int    `json:"port"`
}

func (a *app) discoverSubscriptionProfiles(ctx context.Context) ([]subscriptionProfile, error) {
	rawURL, err := os.ReadFile(a.cfg.SubPath)
	if err != nil {
		return nil, errors.New("subscription is not configured")
	}
	secretURL := strings.TrimSpace(string(rawURL))
	if err := validateSubscriptionURL(secretURL); err != nil {
		return nil, errors.New("stored subscription URL is invalid")
	}
	u, err := url.Parse(secretURL)
	if err != nil {
		return nil, errors.New("stored subscription URL is invalid")
	}

	body, err := fetchSubscriptionBody(ctx, u)
	if err != nil {
		return nil, err
	}
	return parseSubscriptionBody(body)
}

func fetchSubscriptionBody(ctx context.Context, subscriptionURL *url.URL) ([]byte, error) {
	dialer := &net.Dialer{Timeout: 12 * time.Second, KeepAlive: 20 * time.Second}
	transport := &http.Transport{
		Proxy:               nil,
		ForceAttemptHTTP2:   true,
		TLSHandshakeTimeout: 12 * time.Second,
		TLSClientConfig:     &tls.Config{MinVersion: tls.VersionTLS12},
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, err
			}
			if ip := net.ParseIP(host); ip != nil {
				return dialer.DialContext(ctx, network, address)
			}
			ips, err := lookupBootstrapIPv4(ctx, host)
			if err != nil || len(ips) == 0 {
				return nil, errors.New("bootstrap DNS lookup failed")
			}
			return dialer.DialContext(ctx, network, net.JoinHostPort(ips[0].String(), port))
		},
	}
	defer transport.CloseIdleConnections()

	client := &http.Client{
		Transport: transport,
		Timeout:   30 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("too many redirects")
			}
			if req.URL.Scheme != "https" || req.URL.Hostname() == "" || req.URL.User != nil {
				return errors.New("unsafe subscription redirect")
			}
			return nil
		},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, subscriptionURL.String(), nil)
	if err != nil {
		return nil, errors.New("cannot create subscription request")
	}
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("Pragma", "no-cache")
	req.Header.Set("User-Agent", "FreeNet-Router/profile-discovery")

	resp, err := client.Do(req)
	if err != nil {
		return nil, errors.New("subscription fetch failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("subscription fetch HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxSubscriptionBytes+1))
	if err != nil {
		return nil, errors.New("cannot read subscription response")
	}
	if len(body) == 0 {
		return nil, errors.New("subscription response is empty")
	}
	if len(body) > maxSubscriptionBytes {
		return nil, errors.New("subscription response is too large")
	}
	return body, nil
}

func lookupBootstrapIPv4(ctx context.Context, host string) ([]net.IP, error) {
	var lastErr error
	for _, dnsServer := range []string{"77.88.8.8:53", "8.8.8.8:53"} {
		resolver := &net.Resolver{
			PreferGo: true,
			Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
				d := net.Dialer{Timeout: 5 * time.Second}
				return d.DialContext(ctx, "udp", dnsServer)
			},
		}
		ips, err := resolver.LookupIP(ctx, "ip4", host)
		if err == nil && len(ips) > 0 {
			return ips, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = errors.New("no IPv4 address")
	}
	return nil, lastErr
}

func parseSubscriptionBody(body []byte) ([]subscriptionProfile, error) {
	text := strings.ReplaceAll(string(body), "\r", "")
	if !strings.Contains(text, "vless://") {
		decoded, err := decodeSubscriptionBase64(text)
		if err != nil {
			return nil, errors.New("subscription is neither plain VLESS nor valid base64")
		}
		text = strings.ReplaceAll(string(decoded), "\r", "")
	}

	profiles := make([]subscriptionProfile, 0, 16)
	seen := map[string]bool{}
	for _, rawLine := range strings.Split(text, "\n") {
		line := strings.TrimSpace(rawLine)
		lower := strings.ToLower(line)
		if !strings.HasPrefix(lower, "vless://") || !strings.Contains(lower, "extra") || strings.Contains(lower, "expired") {
			continue
		}
		p, ok := parseSafeVLESSProfile(line)
		if !ok || seen[p.ID] {
			continue
		}
		seen[p.ID] = true
		profiles = append(profiles, p)
		if len(profiles) >= 100 {
			break
		}
	}
	if len(profiles) == 0 {
		return nil, errors.New("no active Extra profiles found")
	}
	return profiles, nil
}

func decodeSubscriptionBase64(text string) ([]byte, error) {
	compact := strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, text)
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if decoded, err := enc.DecodeString(compact); err == nil {
			return decoded, nil
		}
	}
	return nil, errors.New("base64 decode failed")
}

func parseSafeVLESSProfile(line string) (subscriptionProfile, bool) {
	u, err := url.Parse(line)
	if err != nil || !strings.EqualFold(u.Scheme, "vless") || u.Hostname() == "" {
		return subscriptionProfile{}, false
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil || port < 1 || port > 65535 {
		return subscriptionProfile{}, false
	}
	address := u.Hostname()
	if len(address) > 255 || strings.ContainsAny(address, "\r\n\x00 /?#@") {
		return subscriptionProfile{}, false
	}

	name := sanitizeProfileName(u.Fragment)
	if name == "" {
		name = "Extra profile"
	}
	sum := sha256.Sum256([]byte(name + "|" + strings.ToLower(address) + "|" + strconv.Itoa(port)))
	return subscriptionProfile{
		ID:      hex.EncodeToString(sum[:8]),
		Name:    name,
		Address: address,
		Port:    port,
	}, true
}

func sanitizeProfileName(name string) string {
	name = strings.TrimSpace(strings.Map(func(r rune) rune {
		if r == '\r' || r == '\n' || r == '\t' || r == 0 || unicode.IsControl(r) {
			return ' '
		}
		return r
	}, name))
	lower := strings.ToLower(name)
	for _, marker := range []string{"vless://", "https://", "http://", "uuid=", "pbk=", "sid=", "publickey="} {
		if strings.Contains(lower, marker) {
			return "Extra profile"
		}
	}
	runes := []rune(name)
	if len(runes) > 120 {
		name = string(runes[:120])
	}
	return name
}

func profileEndpoint(p subscriptionProfile) string {
	return net.JoinHostPort(p.Address, strconv.Itoa(p.Port))
}
