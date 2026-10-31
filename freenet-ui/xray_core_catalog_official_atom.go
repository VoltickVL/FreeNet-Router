package main

import (
	"context"
	"encoding/xml"
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

const (
	xrayCoreOfficialAtomURL = "https://github.com/XTLS/Xray-core/releases.atom"
	xrayCoreOfficialReleaseTagPrefix = "https://github.com/XTLS/Xray-core/releases/tag/"
	xrayCoreOfficialReleaseDownloadPrefix = "https://github.com/XTLS/Xray-core/releases/download/"
	xrayCoreAtomMaxBytes int64 = 4 << 20
	xrayCoreDGSTMaxBytes int64 = 4096
)

type xrayCoreOfficialAtomFeed struct {
	XMLName xml.Name `xml:"feed"`
	Entries []struct {
		Updated string `xml:"updated"`
		Links []struct {
			Rel string `xml:"rel,attr"`
			Href string `xml:"href,attr"`
		} `xml:"link"`
	} `xml:"entry"`
}

var xrayCoreOfficialSHA256Line = regexp.MustCompile(`(?i)^\s*(?:SHA2-256|SHA256)\s*(?:=|:)\s*([0-9a-f]{64})\s*$`)

// Do not trust a release title or a link supplied by another server. Only
// GitHub's official XTLS tag path can select the ZIP and its .dgst sidecar.
func xrayCoreLatestOfficialAtomTag(raw []byte) (string, string, error) {
	var feed xrayCoreOfficialAtomFeed
	if err := xml.Unmarshal(raw, &feed); err != nil {
		return "", "", errors.New("invalid Xray release Atom feed")
	}
	for _, entry := range feed.Entries {
		for _, link := range entry.Links {
			if link.Rel != "alternate" || !strings.HasPrefix(link.Href, xrayCoreOfficialReleaseTagPrefix) {
				continue
			}
			tag := strings.TrimPrefix(link.Href, xrayCoreOfficialReleaseTagPrefix)
			if !validXrayCoreTag(tag) || strings.Contains(tag, "-") || strings.ContainsAny(tag, "/?#%") {
				continue
			}
			return tag, entry.Updated, nil
		}
	}
	return "", "", errors.New("no verified official Xray release tag")
}

func xrayCoreSHA256FromOfficialDGST(raw []byte) (string, error) {
	sha := ""
	for _, line := range strings.Split(string(raw), "\n") {
		m := xrayCoreOfficialSHA256Line.FindStringSubmatch(strings.TrimSpace(line))
		if len(m) != 2 {
			continue
		}
		value, ok := normalizeXrayCoreDigest("sha256:" + m[1])
		if !ok || sha != "" {
			return "", errors.New("invalid or duplicate Xray SHA-256 line")
		}
		sha = value
	}
	if sha == "" {
		return "", errors.New("official Xray SHA-256 missing")
	}
	return sha, nil
}

func officialGitHubRedirectHost(host string) bool {
	switch strings.ToLower(host) {
	case "github.com", "release-assets.githubusercontent.com", "objects.githubusercontent.com":
		return true
	default:
		return false
	}
}

// Follow only the official GitHub release-CDN redirects, never a third-party
// mirror. A blocked/replaced sidecar is STOP, never a checksum bypass.
func xrayCoreOfficialClient(client *http.Client) *http.Client {
	copy := *client
	copy.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) > 5 || req.URL.Scheme != "https" || !officialGitHubRedirectHost(req.URL.Hostname()) {
			return http.ErrUseLastResponse
		}
		return nil
	}
	return &copy
}

func xrayCoreOfficialGET(ctx context.Context, client *http.Client, endpoint string, limit int64) ([]byte, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "https" || u.Hostname() != "github.com" {
		return nil, xrayCoreDownloadError("official-origin", false)
	}
	if endpoint != xrayCoreOfficialAtomURL && !strings.HasPrefix(endpoint, xrayCoreOfficialReleaseDownloadPrefix) {
		return nil, xrayCoreDownloadError("official-origin", false)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, xrayCoreDownloadError("request", false)
	}
	req.Header.Set("User-Agent", "FreeNet-Router")
	resp, err := xrayCoreOfficialClient(client).Do(req)
	if err != nil {
		return nil, xrayCoreDownloadNetworkError(ctx, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, xrayCoreDownloadHTTPError(resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, xrayCoreDownloadError("stream", true)
	}
	if len(raw) == 0 || int64(len(raw)) > limit {
		return nil, xrayCoreDownloadError("official-size", false)
	}
	return raw, nil
}

// A GitHub API 403 on a shared VPN exit must not prevent an official update
// when github.com/releases.atom and the matching official .zip.dgst work.
// No arbitrary version/URL, no unverified ZIP and no process mutation here.
func xrayCoreOfficialAtomCatalog(ctx context.Context, client *http.Client, current, assetName string) (xrayCoreCatalogResponse, error) {
	if !strings.HasPrefix(assetName, "Xray-") || !strings.HasSuffix(assetName, ".zip") ||
		strings.ContainsAny(assetName, "/\\?%#") || strings.Contains(assetName, "..") {
		return xrayCoreCatalogResponse{}, xrayCoreDownloadError("official-asset", false)
	}
	body, err := xrayCoreOfficialGET(ctx, client, xrayCoreOfficialAtomURL, xrayCoreAtomMaxBytes)
	if err != nil {
		return xrayCoreCatalogResponse{}, err
	}
	tag, updated, err := xrayCoreLatestOfficialAtomTag(body)
	if err != nil {
		return xrayCoreCatalogResponse{}, xrayCoreDownloadError("official-feed", false)
	}
	assetURL := xrayCoreOfficialReleaseDownloadPrefix + tag + "/" + assetName
	dgst, err := xrayCoreOfficialGET(ctx, client, assetURL+".dgst", xrayCoreDGSTMaxBytes)
	if err != nil {
		return xrayCoreCatalogResponse{}, err
	}
	sha, err := xrayCoreSHA256FromOfficialDGST(dgst)
	if err != nil {
		return xrayCoreCatalogResponse{}, xrayCoreDownloadError("official-digest", false)
	}
	release := xrayCoreRelease{
		Version: tag, PublishedAt: updated, Latest: true, Current: current == tag,
		Description: "Официальный выпуск Xray; SHA-256 проверен по файлу .dgst из GitHub Releases.",
		Asset: xrayCoreAsset{
			Name: assetName, URL: assetURL, Digest: "sha256:" + sha,
			// Github Atom does not provide Content-Length. The actual ZIP is
			// bounded and SHA-256 verified before staging/semantic checks.
			Size: 0, Available: true,
		},
	}
	return xrayCoreCatalogResponse{
		Success: true, CurrentVersion: current, LatestVersion: tag,
		Architecture: "", AssetName: assetName, Releases: []xrayCoreRelease{release},
	}, nil
}

func (a *app) fetchXrayCoreOfficialAtomFallback(ctx context.Context, current, assetName string) (xrayCoreCatalogResponse, error) {
	var catalog xrayCoreCatalogResponse
	err := a.withXrayCoreActiveVPNClient(ctx, func(client *http.Client) error {
		var fetchErr error
		catalog, fetchErr = xrayCoreOfficialAtomCatalog(ctx, client, current, assetName)
		return fetchErr
	})
	return catalog, err
}
