package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
)

func xrayCoreOfficialFeedFixture(link string) string {
	return `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom">
  <entry><title>Release Xray</title>
    <updated>2026-10-31T13:37:42Z</updated>
    <link rel="alternate" type="text/html" href="` + link + `"/>
  </entry>
</feed>`
}

func TestXrayCoreOfficialAtomFallbackPinsUpstreamAndDGST(t *testing.T) {
	tag := "v26.10.31"
	asset := "Xray-linux-arm64-v8a.zip"
	digest := strings.Repeat("a", 64)
	feed := xrayCoreOfficialFeedFixture(xrayCoreOfficialReleaseTagPrefix + tag)
	var called []string
	client := &http.Client{Transport: xrayCoreRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		called = append(called, req.URL.String())
		if req.Method != http.MethodGet || req.Header.Get("User-Agent") != "FreeNet-Router" {
			t.Fatal("fallback must only use pinned read-only official GET")
		}
		var body string
		switch req.URL.String() {
		case xrayCoreOfficialAtomURL:
			body = feed
		case xrayCoreOfficialReleaseDownloadPrefix + tag + "/" + asset + ".dgst":
			body = "MD5 = " + strings.Repeat("0", 32) + "\nSHA1 = " + strings.Repeat("0", 40) +
				"\nSHA2-256 = " + digest + "\nSHA2-512 = " + strings.Repeat("b", 128) + "\n"
		default:
			t.Fatalf("unexpected remote endpoint: %s", req.URL)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{},
			Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	})}
	got, err := xrayCoreOfficialAtomCatalog(context.Background(), client, "v26.9.9", asset)
	if err != nil {
		t.Fatalf("official read-only catalog fallback failed: %v", err)
	}
	if !got.Success || got.LatestVersion != tag || got.CurrentVersion != "v26.9.9" ||
		len(got.Releases) != 1 || !got.Releases[0].Asset.Available ||
		got.Releases[0].Asset.Digest != "sha256:"+digest ||
		got.Releases[0].Asset.URL != xrayCoreOfficialReleaseDownloadPrefix+tag+"/"+asset ||
		got.Releases[0].Asset.Size != 0 || !got.Releases[0].Latest {
		t.Fatalf("incomplete checksum/official identity catalog: %+v", got)
	}
	if len(called) != 2 {
		t.Fatalf("catalog must only download feed + .dgst, never ZIP: %+v", called)
	}
}

func TestXrayCoreOfficialAtomOnlyAcceptsOfficialStableTag(t *testing.T) {
	for _, link := range []string{
		"https://evil.example/XTLS/Xray-core/releases/tag/v26.10.31",
		"https://github.com/XTLS/Other/releases/tag/v26.10.31",
		"https://github.com/XTLS/Xray-core/releases/tag/v26.10.31?raw=1",
		"https://github.com/XTLS/Xray-core/releases/tag/v26.10.31/redirect",
		"https://github.com/XTLS/Xray-core/releases/tag/v26.10.31-rc1",
	} {
		_, _, err := xrayCoreLatestOfficialAtomTag([]byte(xrayCoreOfficialFeedFixture(link)))
		if err == nil {
			t.Fatalf("untrusted/unstable tag accepted: %s", link)
		}
	}
	tag, when, err := xrayCoreLatestOfficialAtomTag([]byte(xrayCoreOfficialFeedFixture(xrayCoreOfficialReleaseTagPrefix+"v26.10.31")))
	if err != nil || tag != "v26.10.31" || when != "2026-10-31T13:37:42Z" {
		t.Fatalf("valid official tag rejected: %s %s %v", tag, when, err)
	}
	if _, _, err = xrayCoreLatestOfficialAtomTag([]byte("<html>not atom</html>")); err == nil {
		t.Fatal("HTML may not be interpreted as official feed")
	}
}

func TestXrayCoreOfficialDGSTMustContainOneValidSHA256(t *testing.T) {
	hash := strings.Repeat("d", 64)
	for _, content := range []string{
		"SHA2-256 = " + hash + "\n",
		"SHA256 = " + hash + "\n",
		"SHA256: " + hash + "\n",
	} {
		actual, err := xrayCoreSHA256FromOfficialDGST([]byte(content))
		if err != nil || actual != hash {
			t.Fatalf("valid official digest rejected: %v / %q", err, actual)
		}
	}
	for _, text := range []string{
		"MD5 = " + strings.Repeat("d", 32) + "\n",
		"SHA2-256 = " + strings.Repeat("x", 64),
		"SHA2-256 = " + hash + "\nSHA2-256 = " + hash,
		"SHA2-256 = " + hash + " extra",
	} {
		if _, err := xrayCoreSHA256FromOfficialDGST([]byte(text)); err == nil {
			t.Fatalf("missing/duplicate/malformed SHA256 unexpectedly accepted")
		}
	}
}

func TestXrayCoreOfficialAtomFailClosedAndNoThirdPartyRedirect(t *testing.T) {
	const asset = "Xray-linux-arm64-v8a.zip"
	tag := "v26.10.31"
	feed := xrayCoreOfficialFeedFixture(xrayCoreOfficialReleaseTagPrefix+tag)
	redirectCount := 0
	client := &http.Client{Transport: xrayCoreRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Hostname() != "github.com" {
			redirectCount++
			t.Fatalf("untrusted redirect contacted: %s", req.URL.Hostname())
		}
		if req.URL.String() == xrayCoreOfficialAtomURL {
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(feed)), Header: http.Header{}, Request: req}, nil
		}
		return &http.Response{StatusCode: http.StatusFound, Body: io.NopCloser(strings.NewReader("")),
			Header: http.Header{"Location": []string{"https://untrusted.example/checksum"}}, Request: req}, nil
	})}
	_, err := xrayCoreOfficialAtomCatalog(context.Background(), client, "v26.9.9", asset)
	if err == nil || redirectCount != 0 {
		t.Fatalf("untrusted digest redirect accepted: %v, redirects=%d", err, redirectCount)
	}
	if _, err := xrayCoreOfficialGET(context.Background(), client, "https://example.invalid/asset", 1024); err == nil {
		t.Fatal("external official-source override accepted")
	}
	for _, badAsset := range []string{"../Xray-linux-arm64-v8a.zip", "https://evil.invalid/xray.zip", "Xray/file.zip"} {
		if _, err := xrayCoreOfficialAtomCatalog(context.Background(), client, "v26.9.9", badAsset); err == nil {
			t.Fatalf("untrusted asset filename accepted: %s", badAsset)
		}
	}
}

func TestXrayCoreOfficialAtomOnlyAfterRateLimitOrForbidden(t *testing.T) {
	for _, code := range []string{"http-403", "http-429"} {
		if !xrayCoreCatalogAtomFallbackEligible(&xrayCoreCatalogDualFailure{directCode: "transport", vpnCode: code}) {
			t.Fatalf("official fallback denied for %s", code)
		}
	}
	for _, code := range []string{"vpn-identity", "transport", "http-404", "http-401", "http-500", "http-200"} {
		if xrayCoreCatalogAtomFallbackEligible(&xrayCoreCatalogDualFailure{directCode: "transport", vpnCode: code}) {
			t.Fatalf("official fallback allowed without verified rate limit/403: %s", code)
		}
	}
	if xrayCoreCatalogAtomFallbackEligible(xrayCoreDownloadError("http-403", true)) {
		t.Fatal("non-dual failure must not start second diagnostic Xray")
	}
}

func TestXrayCoreArchiveWithUnknownAtomSizeStillChecksSHA256(t *testing.T) {
	payload := []byte("A bounded, checksum-verified official archive test body")
	sum := sha256.Sum256(payload)
	hash := hex.EncodeToString(sum[:])
	assetURL := xrayCoreOfficialReleaseDownloadPrefix + "v26.10.31/Xray-linux-arm64-v8a.zip"
	release := xrayCoreRelease{Asset: xrayCoreAsset{Available: true, URL: assetURL, Size: 0, Digest: "sha256:" + hash}}
	client := &http.Client{Transport: xrayCoreRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(payload)),
			Header: http.Header{}, Request: req}, nil
	})}
	path, err := downloadXrayCoreArchiveWithClient(context.Background(), release, t.TempDir(), client)
	if err != nil {
		t.Fatalf("official ZIP from Atom with unknown size rejected: %v", err)
	}
	_ = os.Remove(path)
	release.Asset.Digest = "sha256:" + strings.Repeat("0", 64)
	if _, err := downloadXrayCoreArchiveWithClient(context.Background(), release, t.TempDir(), client); err == nil {
		t.Fatal("unverified official Atom ZIP must fail closed")
	}
}
