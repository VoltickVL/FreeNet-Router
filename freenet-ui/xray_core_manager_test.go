package main

import (
	"archive/zip"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestXrayCoreAssetName(t *testing.T) {
	cases := map[string]string{
		"amd64":  "Xray-linux-64.zip",
		"arm64":  "Xray-linux-arm64-v8a.zip",
		"mipsle": "Xray-linux-mips32le.zip",
		"mips":   "Xray-linux-mips32.zip",
	}
	for arch, want := range cases {
		got, ok := xrayCoreAssetName(arch)
		if !ok || got != want {
			t.Fatalf("asset mapping %s: got %q ok=%v want %q", arch, got, ok, want)
		}
	}
	if _, ok := xrayCoreAssetName("riscv64"); ok {
		t.Fatal("unsupported architecture must fail closed")
	}
}

func TestNormalizeXrayCoreTag(t *testing.T) {
	for raw, want := range map[string]string{
		"Xray 26.9.9 (Xray, Penetrates Everything.)": "v26.9.9",
		"Xray-core v25.8.3":                       "v25.8.3",
		"v24.12.31":                               "v24.12.31",
	} {
		if got := normalizeXrayCoreTag(raw); got != want {
			t.Fatalf("normalize %q: got %q want %q", raw, got, want)
		}
	}
	if got := normalizeXrayCoreTag("not-a-version"); got != "" {
		t.Fatalf("invalid version must be rejected, got %q", got)
	}
}

func TestParseXrayCoreCatalog(t *testing.T) {
	digest := strings.Repeat("a", 64)
	upstream := []xrayCoreGitHubRelease{
		{
			TagName: "v26.9.9", PublishedAt: "2026-09-09T00:00:00Z", Body: "Latest stable release\nwith notes.",
			Assets: []struct {
				Name string `json:"name"`; URL string `json:"browser_download_url"`; Digest string `json:"digest"`; Size int64 `json:"size"`
			}{{Name: "Xray-linux-arm64-v8a.zip", URL: "https://github.com/XTLS/Xray-core/releases/download/v26.9.9/Xray-linux-arm64-v8a.zip", Digest: "sha256:" + digest, Size: 12345}},
		},
		{
			TagName: "v26.8.1", PublishedAt: "2026-08-01T00:00:00Z", Body: "Previous stable", Prerelease: false,
			Assets: []struct {
				Name string `json:"name"`; URL string `json:"browser_download_url"`; Digest string `json:"digest"`; Size int64 `json:"size"`
			}{{Name: "Xray-linux-arm64-v8a.zip", URL: "https://github.com/XTLS/Xray-core/releases/download/v26.8.1/Xray-linux-arm64-v8a.zip", Digest: "sha256:" + strings.Repeat("b", 64), Size: 12000}},
		},
		{
			TagName: "v26.10.0-beta.1", PublishedAt: "2026-09-10T00:00:00Z", Body: "Preview", Prerelease: true,
			Assets: []struct {
				Name string `json:"name"`; URL string `json:"browser_download_url"`; Digest string `json:"digest"`; Size int64 `json:"size"`
			}{{Name: "Xray-linux-arm64-v8a.zip", URL: "https://github.com/XTLS/Xray-core/releases/download/v26.10.0-beta.1/Xray-linux-arm64-v8a.zip", Digest: "sha256:" + strings.Repeat("c", 64), Size: 13000}},
		},
	}
	raw, err := json.Marshal(upstream)
	if err != nil { t.Fatal(err) }
	catalog, err := parseXrayCoreCatalog(raw, "v26.8.1", "Xray-linux-arm64-v8a.zip")
	if err != nil { t.Fatal(err) }
	if catalog.LatestVersion != "v26.9.9" {
		t.Fatalf("latest: got %q", catalog.LatestVersion)
	}
	if len(catalog.Releases) != 3 {
		t.Fatalf("release count: got %d", len(catalog.Releases))
	}
	if !catalog.Releases[0].Latest || !catalog.Releases[0].Asset.Available {
		t.Fatal("latest stable release must be installable and marked latest")
	}
	if !catalog.Releases[1].Current {
		t.Fatal("installed version must be marked current")
	}
	if !catalog.Releases[2].Prerelease {
		t.Fatal("prerelease flag must be preserved")
	}
	if strings.Contains(catalog.Releases[0].Description, "\n") {
		t.Fatal("release description must be compact for browser UI")
	}
}

func TestParseXrayCoreCatalogRejectsUntrustedAsset(t *testing.T) {
	raw := []byte(`[{"tag_name":"v26.9.9","published_at":"2026-09-09T00:00:00Z","assets":[{"name":"Xray-linux-arm64-v8a.zip","browser_download_url":"https://example.invalid/Xray.zip","digest":"sha256:` + strings.Repeat("a", 64) + `","size":1234}]}]`)
	catalog, err := parseXrayCoreCatalog(raw, "v26.8.1", "Xray-linux-arm64-v8a.zip")
	if err != nil { t.Fatal(err) }
	if catalog.Releases[0].Asset.Available {
		t.Fatal("non-XTLS download URL must never be installable")
	}
}

func TestExtractXrayCoreCandidate(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "xray.zip")
	f, err := os.Create(archive)
	if err != nil { t.Fatal(err) }
	zw := zip.NewWriter(f)
	entry, err := zw.Create("xray")
	if err != nil { t.Fatal(err) }
	if _, err := entry.Write([]byte("fixture-xray-binary")); err != nil { t.Fatal(err) }
	if err := zw.Close(); err != nil { t.Fatal(err) }
	if err := f.Close(); err != nil { t.Fatal(err) }
	candidate, err := extractXrayCoreCandidate(archive, dir)
	if err != nil { t.Fatal(err) }
	defer os.Remove(candidate)
	data, err := os.ReadFile(candidate)
	if err != nil { t.Fatal(err) }
	if string(data) != "fixture-xray-binary" {
		t.Fatalf("candidate content mismatch: %q", string(data))
	}
	info, err := os.Stat(candidate)
	if err != nil { t.Fatal(err) }
	if info.Mode().Perm()&0100 == 0 {
		t.Fatal("candidate must be executable")
	}
}

func TestCopyXrayCoreBinaryPreservesSnapshot(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "xray")
	dst := filepath.Join(dir, "xray.backup")
	if err := os.WriteFile(src, []byte("old-xray"), 0755); err != nil { t.Fatal(err) }
	info, err := os.Stat(src)
	if err != nil { t.Fatal(err) }
	if err := copyXrayCoreBinary(src, dst, info.Mode()); err != nil { t.Fatal(err) }
	data, err := os.ReadFile(dst)
	if err != nil { t.Fatal(err) }
	if string(data) != "old-xray" {
		t.Fatalf("backup mismatch: %q", string(data))
	}
}