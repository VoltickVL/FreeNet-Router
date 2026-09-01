package main

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDetectCountry(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "filter")
	cases := map[string]string{
		"Frankfurt|Germany|Германия": "de",
		"Warsaw|Poland|Польша":      "pl",
		"Helsinki|Finland":          "fi",
		"Amsterdam|Netherlands":     "nl",
	}
	for input, want := range cases {
		if err := os.WriteFile(p, []byte(input), 0600); err != nil {
			t.Fatal(err)
		}
		if got := detectCountry(p); got != want {
			t.Fatalf("detectCountry(%q)=%q want %q", input, got, want)
		}
	}
}

func TestReadOutbound(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "04_outbounds.json")
	data := `{"outbounds":[{"tag":"vless-reality","settings":{"vnext":[{"address":"1.2.3.4","port":443}]}},{"tag":"direct"},{"tag":"dns-out"}]}`
	if err := os.WriteFile(p, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	endpoint, dns := readOutbound(p)
	if endpoint != "1.2.3.4:443" {
		t.Fatalf("endpoint=%q", endpoint)
	}
	if !dns {
		t.Fatal("dns-out should be present")
	}
}

func TestSameOrigin(t *testing.T) {
	r := httptest.NewRequest("POST", "http://192.168.50.1:1001/api/action", nil)
	r.Host = "192.168.50.1:1001"
	r.Header.Set("Origin", "http://192.168.50.1:1001")
	if !sameOrigin(r) {
		t.Fatal("same origin rejected")
	}
	r.Header.Set("Origin", "https://evil.example")
	if sameOrigin(r) {
		t.Fatal("cross origin accepted")
	}
}

func TestLoopbackListenAddr(t *testing.T) {
	if got := loopbackListenAddr("192.168.50.1:1001"); got != "127.0.0.1:1001" {
		t.Fatalf("loopbackListenAddr()=%q", got)
	}
	for _, addr := range []string{"127.0.0.1:1001", "0.0.0.0:1001", "[::]:1001"} {
		if got := loopbackListenAddr(addr); got != "" {
			t.Fatalf("loopbackListenAddr(%q)=%q want empty", addr, got)
		}
	}
}

func TestRunCommandDoesNotHangOnInheritedOutputPipe(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()

	started := time.Now()
	out, err := runCommand(ctx, "/bin/sh", "-c", "(sleep 5; echo late) & echo done")
	elapsed := time.Since(started)
	if err != nil {
		t.Fatalf("runCommand error: %v", err)
	}
	if elapsed >= 4*time.Second {
		t.Fatalf("runCommand waited for descendant output pipe: %s", elapsed)
	}
	if len(out) == 0 {
		t.Fatal("expected direct command output")
	}
}

func TestStatusJSONDoesNotExposeSecrets(t *testing.T) {
	s := statusResponse{Version: "0.2.1", CountryCode: "de", Country: "Германия", City: "Frankfurt", Endpoint: "1.2.3.4:443"}
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	text := string(b)
	for _, forbidden := range []string{"uuid", "publicKey", "shortId", "subscription"} {
		if containsInsensitive(text, forbidden) {
			t.Fatalf("status JSON contains forbidden key %q", forbidden)
		}
	}
}

func containsInsensitive(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || containsFold(s, sub))
}

func containsFold(s, sub string) bool {
	ls, lsub := []rune(s), []rune(sub)
	for i := 0; i+len(lsub) <= len(ls); i++ {
		ok := true
		for j := range lsub {
			a, b := ls[i+j], lsub[j]
			if a >= 'A' && a <= 'Z' {
				a += 'a' - 'A'
			}
			if b >= 'A' && b <= 'Z' {
				b += 'a' - 'A'
			}
			if a != b {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}
