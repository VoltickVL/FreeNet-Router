package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestXrayCoreDownloadHTTPReasonsAndFailClosed(t *testing.T) {
	tests := []struct {
		status int
		code   string
		retry  bool
	}{
		{http.StatusForbidden, "http-403", true},
		{http.StatusTooManyRequests, "http-429", true},
		{http.StatusUnavailableForLegalReasons, "http-451", true},
		{http.StatusBadGateway, "http-502", true},
		{http.StatusNotFound, "http-404", false},
		{http.StatusUnauthorized, "http-401", false},
		{http.StatusBadRequest, "http-400", false},
	}
	for _, tc := range tests {
		err := xrayCoreDownloadHTTPError(tc.status)
		if code := xrayCoreDownloadSafeCode(err); code != tc.code {
			t.Fatalf("HTTP %d code = %q", tc.status, code)
		}
		if shouldFallbackXrayCoreDownload(err) != tc.retry {
			t.Fatalf("HTTP %d fallback mismatch", tc.status)
		}
		if strings.Contains(err.Error(), "https://") {
			t.Fatal("download error must not leak URLs")
		}
	}
	if !shouldFallbackXrayCoreDownload(xrayCoreDownloadNetworkError(context.Background(), errors.New("raw error https://secret.invalid/path?token=secret"))) {
		t.Fatal("network failure must support a controlled fallback")
	}
	if strings.Contains(xrayCoreDownloadNetworkError(context.Background(), errors.New("token=secret")).Error(), "secret") {
		t.Fatal("network error must not contain raw source details")
	}
}

func TestXrayCoreDownloadArchiveVerifiedBeforeUse(t *testing.T) {
	payload := []byte("test-zip-payload")
	hash := sha256.Sum256(payload)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(payload)
	}))
	defer server.Close()
	release := xrayCoreRelease{Asset: xrayCoreAsset{
		Name: "Xray-linux-arm64-v8a.zip", URL: server.URL, Digest: "sha256:" + hex.EncodeToString(hash[:]),
		Size: int64(len(payload)), Available: true,
	}}
	dir := t.TempDir()
	path, err := downloadXrayCoreArchiveWithClient(context.Background(), release, dir, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	downloaded, err := os.ReadFile(path)
	if err != nil || string(downloaded) != string(payload) {
		t.Fatalf("verified download mismatch: %v", err)
	}
	_ = os.Remove(path)
	release.Asset.Size++
	if _, err = downloadXrayCoreArchiveWithClient(context.Background(), release, dir, server.Client()); err == nil || shouldFallbackXrayCoreDownload(err) {
		t.Fatalf("invalid asset size must fail without fallback: %v", err)
	}
	release.Asset.Size--
	release.Asset.Digest = "sha256:" + strings.Repeat("f", 64)
	if _, err = downloadXrayCoreArchiveWithClient(context.Background(), release, dir, server.Client()); err == nil || shouldFallbackXrayCoreDownload(err) {
		t.Fatalf("SHA mismatch must fail without fallback: %v", err)
	}
	files, _ := os.ReadDir(dir)
	if len(files) != 0 {
		t.Fatalf("unverified download left staged archive behind: %d", len(files))
	}
}

func TestXrayCoreDownloadArchiveHTTPFailureUsesFiniteReason(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("secret-host-detail-should-not-escape"))
	}))
	defer server.Close()
	release := xrayCoreRelease{Asset: xrayCoreAsset{
		URL: server.URL, Digest: "sha256:" + strings.Repeat("e", 64),
		Size: 10, Available: true,
	}}
	_, err := downloadXrayCoreArchiveWithClient(context.Background(), release, t.TempDir(), server.Client())
	if xrayCoreDownloadSafeCode(err) != "http-403" || !shouldFallbackXrayCoreDownload(err) {
		t.Fatalf("HTTP failure must preserve a safe retryable reason, got %v", err)
	}
	if strings.Contains(err.Error(), "secret-host-detail") {
		t.Fatal("response body leaked")
	}
}

func TestXrayCoreDownloadResilientUsesOnlyOneBoundedFallback(t *testing.T) {
	for _, reason := range []error{xrayCoreDownloadHTTPError(403), xrayCoreDownloadError("timeout", true)} {
		directCount, fallbackCount := 0, 0
		direct := func(context.Context, xrayCoreRelease, string) (string, error) {
			directCount++
			return "", reason
		}
		fallback := func(context.Context, xrayCoreRelease, string) (string, error) {
			fallbackCount++
			return "/tmp/verified-fixture.zip", nil
		}
		got, err := downloadXrayCoreArchiveResilient(context.Background(), xrayCoreRelease{}, "", direct, fallback)
		if err != nil || got != "/tmp/verified-fixture.zip" || directCount != 1 || fallbackCount != 1 {
			t.Fatalf("fallback call contract: %q %v direct=%d vpn=%d", got, err, directCount, fallbackCount)
		}
	}
}

func TestXrayCoreDownloadResilientNeverRetriesUnsafeInputs(t *testing.T) {
	for _, issue := range []error{
		xrayCoreDownloadHTTPError(404),
		errors.New("Xray archive SHA-256 verification failed"),
		xrayCoreDownloadError("vpn-identity", false),
	} {
		calls := 0
		_, err := downloadXrayCoreArchiveResilient(context.Background(), xrayCoreRelease{}, "",
			func(context.Context, xrayCoreRelease, string) (string, error) { return "", issue },
			func(context.Context, xrayCoreRelease, string) (string, error) {
				calls++
				return "", nil
			},
		)
		if err == nil || calls != 0 {
			t.Fatalf("non-retryable direct failure must preserve binary: %v (%d calls)", err, calls)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	calls := 0
	_, err := downloadXrayCoreArchiveResilient(ctx, xrayCoreRelease{}, "",
		func(context.Context, xrayCoreRelease, string) (string, error) { return "", xrayCoreDownloadError("timeout", true) },
		func(context.Context, xrayCoreRelease, string) (string, error) {
			calls++
			return "", nil
		},
	)
	if err == nil || calls != 0 {
		t.Fatal("cancelled production transaction must never start fallback")
	}
}

func TestXrayCoreDownloadResilientReportsBothSafeFailureStages(t *testing.T) {
	_, err := downloadXrayCoreArchiveResilient(context.Background(), xrayCoreRelease{}, "",
		func(context.Context, xrayCoreRelease, string) (string, error) { return "", xrayCoreDownloadHTTPError(403) },
		func(context.Context, xrayCoreRelease, string) (string, error) { return "", xrayCoreDownloadError("vpn-validation", false) },
	)
	if err == nil || err.Error() != "reason=xray-download-direct-http-403; vpn-vpn-validation" {
		t.Fatalf("must report both bounded failures without raw data: %v", err)
	}
}

func TestXrayCoreVPNClientIsExplicitLocalSocks(t *testing.T) {
	t.Setenv("HTTPS_PROXY", "http://untrusted.example.invalid:8888")
	port := 19077
	client := newXrayCoreVPNProxyClient(port)
	tr, ok := client.Transport.(*http.Transport)
	if !ok || tr.Proxy == nil || tr.DialContext == nil {
		t.Fatal("isolated VPN client must have its own explicit loopback SOCKS transport")
	}
	req, err := http.NewRequest(http.MethodGet, "https://github.com", nil)
	if err != nil {
		t.Fatal(err)
	}
	proxy, err := tr.Proxy(req)
	if err != nil || proxy == nil || proxy.Scheme != "socks5" || proxy.Host != "127.0.0.1:19077" {
		t.Fatalf("must ignore proxy env and use local SOCKS only, got %v %v", proxy, err)
	}
}
