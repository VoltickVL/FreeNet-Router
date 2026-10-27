package main

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

const (
	xkeenUpdateMaxDownload = 4 << 20
	xkeenUpdateMaxExpanded = 9 << 20
	xkeenUpdateMaxMembers  = 256
)

type xkeenUpdateAsset struct {
	Version string
	URL     string
	Digest  string
	Size    int64
}

func xkeenReleaseAssetURL(tag string) string {
	return "https://github.com/jameszeroX/XKeen/releases/download/" + tag + "/xkeen.tar.gz"
}

func xkeenTrustedAssetURL(raw, tag string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.Host == "github.com" &&
		u.User == nil && u.RawQuery == "" && u.Fragment == "" &&
		u.Path == "/jameszeroX/XKeen/releases/download/"+tag+"/xkeen.tar.gz"
}

func parseXKeenUpdateAsset(data []byte) (xkeenUpdateAsset, error) {
	var release struct {
		TagName    string `json:"tag_name"`
		Draft      bool   `json:"draft"`
		Prerelease bool   `json:"prerelease"`
		Assets     []struct {
			Name  string `json:"name"`
			URL   string `json:"browser_download_url"`
			Size  int64  `json:"size"`
			Hash  string `json:"digest"`
		} `json:"assets"`
	}
	if len(data) == 0 || len(data) > xkeenCatalogMaxBytes || json.Unmarshal(data, &release) != nil ||
		release.Draft || release.Prerelease {
		return xkeenUpdateAsset{}, errors.New("xkeen-catalog-invalid")
	}
	if _, ok := xkeenVersionParts(release.TagName); !ok {
		return xkeenUpdateAsset{}, errors.New("xkeen-version-invalid")
	}
	for _, asset := range release.Assets {
		if asset.Name != "xkeen.tar.gz" {
			continue
		}
		digest := strings.TrimPrefix(asset.Hash, "sha256:")
		if !xkeenTrustedAssetURL(asset.URL, release.TagName) || asset.Size < 1024 ||
			asset.Size > xkeenUpdateMaxDownload || !strings.HasPrefix(asset.Hash, "sha256:") ||
			len(digest) != sha256.Size*2 {
			return xkeenUpdateAsset{}, errors.New("xkeen-asset-unverified")
		}
		if _, err := hex.DecodeString(digest); err != nil {
			return xkeenUpdateAsset{}, errors.New("xkeen-asset-unverified")
		}
		return xkeenUpdateAsset{
			Version: release.TagName, URL: asset.URL, Digest: digest, Size: asset.Size,
		}, nil
	}
	return xkeenUpdateAsset{}, errors.New("xkeen-asset-unavailable")
}

func newXKeenUpdateHTTPClient() *http.Client {
	return &http.Client{
		Timeout: 35 * time.Second,
		CheckRedirect: func(r *http.Request, via []*http.Request) error {
			if len(via) >= 5 || r.URL.Scheme != "https" ||
				(r.URL.Hostname() != "github.com" && r.URL.Hostname() != "release-assets.githubusercontent.com") {
				return errors.New("xkeen-untrusted-redirect")
			}
			return nil
		},
	}
}

func fetchOfficialXKeenUpdateAsset(ctx context.Context, client *http.Client) (xkeenUpdateAsset, error) {
	if client == nil {
		return xkeenUpdateAsset{}, errors.New("xkeen-client-unavailable")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, xkeenStableReleaseURL, nil)
	if err != nil {
		return xkeenUpdateAsset{}, errors.New("xkeen-catalog-request-invalid")
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "FreeNet-XKeen-GuardedUpdate")
	resp, err := client.Do(req)
	if err != nil {
		return xkeenUpdateAsset{}, errors.New("xkeen-catalog-unreachable")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return xkeenUpdateAsset{}, errors.New("xkeen-catalog-unreachable")
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, xkeenCatalogMaxBytes+1))
	if err != nil || len(data) > xkeenCatalogMaxBytes {
		return xkeenUpdateAsset{}, errors.New("xkeen-catalog-unreadable")
	}
	return parseXKeenUpdateAsset(data)
}

func downloadVerifiedXKeenArchive(ctx context.Context, client *http.Client, asset xkeenUpdateAsset, parent string) (string, error) {
	if client == nil || !xkeenTrustedAssetURL(asset.URL, asset.Version) ||
		asset.Size < 1024 || asset.Size > xkeenUpdateMaxDownload || len(asset.Digest) != sha256.Size*2 {
		return "", errors.New("xkeen-download-unverified")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, asset.URL, nil)
	if err != nil {
		return "", errors.New("xkeen-download-invalid")
	}
	req.Header.Set("User-Agent", "FreeNet-XKeen-GuardedUpdate")
	resp, err := client.Do(req)
	if err != nil {
		return "", errors.New("xkeen-download-unavailable")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", errors.New("xkeen-download-unavailable")
	}
	f, err := os.CreateTemp(parent, ".freenet-xkeen-download-*")
	if err != nil {
		return "", errors.New("xkeen-stage-unavailable")
	}
	filename := f.Name()
	keep := false
	defer func() {
		_ = f.Close()
		if !keep {
			_ = os.Remove(filename)
		}
	}()
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(resp.Body, asset.Size+1))
	if err != nil || n != asset.Size ||
		!strings.EqualFold(hex.EncodeToString(h.Sum(nil)), asset.Digest) ||
		f.Sync() != nil {
		return "", errors.New("xkeen-sha256-or-size-mismatch")
	}
	keep = true
	return filename, nil
}

// Archive content is not executable during validation. Only the two known
// upstream release roots are accepted; links, devices, traversal and extra
// members are rejected even after the GitHub SHA-256 match.
func xkeenArchiveSizeAllowed(size, expanded int64) bool {
	// Subtraction avoids signed overflow from malicious tar header sizes.
	return size >= 0 && expanded >= 0 && expanded <= xkeenUpdateMaxExpanded &&
		size <= xkeenUpdateMaxExpanded-expanded
}

func extractVerifiedXKeenArchive(filename, root, version string) error {
	f, err := os.Open(filename)
	if err != nil {
		return errors.New("xkeen-archive-unavailable")
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return errors.New("xkeen-archive-invalid")
	}
	defer gz.Close()
	t := tar.NewReader(gz)
	seen := make(map[string]bool)
	members := 0
	var expanded int64
	for {
		h, err := t.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil || h == nil {
			return errors.New("xkeen-archive-invalid")
		}
		members++
		if members > xkeenUpdateMaxMembers ||
			!xkeenArchiveSizeAllowed(h.Size, expanded) {
			return errors.New("xkeen-archive-unbounded")
		}
		name := strings.TrimSuffix(h.Name, "/")
		if name == "" || path.Clean(name) != name ||
			strings.HasPrefix(name, "/") || strings.Contains(name, "\\") ||
			(name != "xkeen" && name != "_xkeen" && !strings.HasPrefix(name, "_xkeen/")) ||
			seen[name] {
			return errors.New("xkeen-archive-path-unsafe")
		}
		seen[name] = true
		dest := filepath.Join(root, filepath.FromSlash(name))
		if h.Typeflag != tar.TypeReg && h.Typeflag != tar.TypeRegA && h.Typeflag != tar.TypeDir {
			return errors.New("xkeen-archive-type-unsafe")
		}
		if h.Typeflag == tar.TypeDir {
			if err := os.MkdirAll(dest, 0700); err != nil {
				return errors.New("xkeen-stage-unavailable")
			}
			continue
		}
		if name == "_xkeen" || h.Size == 0 && name == "xkeen" {
			return errors.New("xkeen-archive-invalid")
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0700); err != nil {
			return errors.New("xkeen-stage-unavailable")
		}
		mode := os.FileMode(0600)
		if h.Mode&0111 != 0 || name == "xkeen" {
			mode = 0700
		}
		dst, err := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
		if err != nil {
			return errors.New("xkeen-stage-unavailable")
		}
		n, writeErr := io.CopyN(dst, t, h.Size)
		syncErr := dst.Sync()
		closeErr := dst.Close()
		if writeErr != nil || syncErr != nil || closeErr != nil || n != h.Size {
			return errors.New("xkeen-archive-invalid")
		}
		expanded += n
	}
	for _, required := range []string{"xkeen", "_xkeen/import.sh", "_xkeen/01_info/01_info_variable.sh"} {
		info, err := os.Lstat(filepath.Join(root, filepath.FromSlash(required)))
		if err != nil || !info.Mode().IsRegular() {
			return errors.New("xkeen-candidate-incomplete")
		}
	}
	if info, err := os.Stat(filepath.Join(root, "xkeen")); err != nil || info.Mode()&0111 == 0 {
		return errors.New("xkeen-candidate-not-executable")
	}
	meta, err := os.ReadFile(filepath.Join(root, "_xkeen", "01_info", "01_info_variable.sh"))
	if err != nil {
		return errors.New("xkeen-candidate-version-unavailable")
	}
	got, channel, err := parseXKeenInstalledMetadata(meta)
	if err != nil || got != version || channel != "stable" {
		return errors.New("xkeen-candidate-version-mismatch")
	}
	return nil
}
