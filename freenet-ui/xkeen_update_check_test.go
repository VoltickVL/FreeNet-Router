package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func xkeenReleaseFixture(tag string, draft, pre bool, digest string, size int64) []byte {
	body, _ := json.Marshal(map[string]any{
		"tag_name": tag, "draft": draft, "prerelease": pre,
		"assets": []map[string]any{{"name": "xkeen.tar.gz", "digest": digest, "size": size}},
	})
	return body
}

func TestXKeenReadOnlyMetadataAndVersionComparison(t *testing.T) {
	for _, tc := range []struct {
		raw, version, channel string
		valid bool
	}{
		{`xkeen_current_version="2.0"
xkeen_build="Stable"
`, "2.0", "stable", true},
		{`xkeen_current_version='2.1.1'
xkeen_build='Beta'
`, "2.1.1", "beta", true},
		{`xkeen_current_version=1.1.3.9
`, "1.1.3.9", "unknown", true},
		{`xkeen_current_version="2.0"
xkeen_current_version="2.1"
`, "", "", false},
		{`xkeen_current_version="$(touch /tmp/shell-must-not-run)"
xkeen_build="Stable"
`, "", "", false},
		{`xkeen_current_version="2.0"
xkeen_build="$(echo fake)"
`, "", "", false},
		{`xkeen_current_version="2.0"
xkeen_build="Canary"
`, "", "", false},
		{`xkeen_current_version=2.0;touch /tmp/never-execute
`, "", "", false},
	} {
		got, channel, err := parseXKeenInstalledMetadata([]byte(tc.raw))
		if (err == nil) != tc.valid || (tc.valid && (got != tc.version || channel != tc.channel)) {
			t.Fatalf("invalid XKeen installed-version metadata acceptance: got %q/%q err=%v valid=%v", got, channel, err, tc.valid)
		}
	}
	for _, tc := range []struct{ from, to string; want int; ok bool }{
		{"2.0", "2.1", -1, true},
		{"2.1", "2.1.0", 0, true},
		{"2.1.1", "2.1", 1, true},
		{"1.1.3.9", "2.0", -1, true},
		{"2.1_Beta", "2.1", 0, false},
		{"$(evil)", "2.1", 0, false},
		{"2.1", "garbage", 0, false},
	} {
		cmp, ok := compareXKeenVersion(tc.from, tc.to)
		if cmp != tc.want || ok != tc.ok {
			t.Fatalf("version compare %q vs %q: got=%d/%v want=%d/%v", tc.from,tc.to,cmp,ok,tc.want,tc.ok)
		}
	}
}

func TestXKeenInstalledVersionReadWithoutExecutingShell(t *testing.T) {
	root := t.TempDir()
	binary := filepath.Join(root, "xkeen")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nexit 9\n"), 0700); err != nil { t.Fatal(err) }
	info := filepath.Join(root, ".xkeen", "01_info", "01_info_variable.sh")
	if err := os.MkdirAll(filepath.Dir(info), 0700); err != nil { t.Fatal(err) }
	// Executing the source would fail or create a file. The scanner only reads.
	marker := filepath.Join(root, "should-not-exist")
	raw := "xkeen_current_version=\"2.0\"\nxkeen_build=\"Stable\"\n" +
		"touch " + marker + "\nexit 8\n"
	if err := os.WriteFile(info, []byte(raw), 0600); err != nil { t.Fatal(err) }
	version, channel, err := readXKeenInstalledMetadata(binary)
	if err != nil || version != "2.0" || channel != "stable" { t.Fatalf("read only metadata failed: %s %s %v",version,channel,err) }
	if _, err := os.Stat(marker); !os.IsNotExist(err) { t.Fatal("installed metadata was executed") }
	if err := os.Remove(info); err != nil { t.Fatal(err) }
	if _, _, err := readXKeenInstalledMetadata(binary); err == nil { t.Fatal("missing metadata cannot be interpreted as up-to-date") }
}

func TestXKeenStableCatalogRequiresVerifiedReleaseAsset(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	for _, tc := range []struct {
		tag string
		draft, pre bool
		digest string
		size int64
		ok bool
	}{
		{"2.1", false, false, digest, 129691, true},
		{"2.1.1_Beta", false, true, digest, 129691, false},
		{"2.1", true, false, digest, 129691, false},
		{"2.1", false, false, "", 129691, false},
		{"2.1", false, false, digest, 0, false},
		{"2.1", false, false, "sha256:"+strings.Repeat("x", 64), 129691, false},
		{"https://evil.invalid", false, false, digest, 129691, false},
	} {
		version, err := parseXKeenStableRelease(xkeenReleaseFixture(tc.tag,tc.draft,tc.pre,tc.digest,tc.size))
		if (err == nil) != tc.ok || (tc.ok && version != tc.tag) {
			t.Fatalf("unsafe upstream XKeen release accepted: tag=%q got=%q err=%v",tc.tag,version,err)
		}
	}
	if _, err := parseXKeenStableRelease([]byte("not-json")); err == nil {t.Fatal("malformed release was trusted")}
	if _, err := parseXKeenStableRelease(make([]byte,xkeenCatalogMaxBytes+1)); err == nil {t.Fatal("unbounded upstream body was trusted")}
}

func TestXKeenReleaseCheckerOnlyGetsVerifiedMetadata(t *testing.T) {
	digest := "sha256:"+strings.Repeat("a",64)
	expected := xkeenReleaseFixture("2.1",false,false,digest,129691)
	sawGET := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
			t.Errorf("checker attempted mutation or leaked browser credentials")
			w.WriteHeader(http.StatusForbidden)
			return
		}
		sawGET = true
		w.Header().Set("Content-Type","application/json")
		_,_ = w.Write(expected)
	}))
	defer srv.Close()
	tag,err := fetchXKeenStableRelease(context.Background(),srv.Client(),srv.URL)
	if err != nil || tag != "2.1" || !sawGET {t.Fatalf("read-only stable checker failed: %q %v GET=%v",tag,err,sawGET)}
}

func TestXKeenUpdateUIAndEndpointHaveNoApplyAction(t *testing.T) {
	api,err := os.ReadFile("xray_service_api.go")
	if err != nil {t.Fatal(err)}
	if !strings.Contains(string(api), `mux.HandleFunc("GET /api/xkeen/update-check", a.requireAuth(a.handleXKeenUpdateCheck))`) ||
		strings.Contains(string(api), "POST /api/xkeen/update-check") {
		t.Fatal("XKeen version check must be read-only and authenticated")
	}
	ui,err := os.ReadFile("web/settings-v3-core.js")
	if err != nil {t.Fatal(err)}
	for _, want := range []string{"fn3_xkeen_check", "/api/xkeen/update-check", "xkeen_check", "installed_version", "latest_stable_version"} {
		if !strings.Contains(string(ui),want) {t.Fatalf("XKeen UI missing field %s",want)}
	}
}
