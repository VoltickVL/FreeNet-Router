package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	selfUpdateReleasePageSize = 100
	selfUpdateReleaseMaxPages = 5
	selfUpdateReleaseBodyLimit = 4 << 20
)

var (
	selfUpdateReleasesAPI = "https://api.github.com/repos/VoltickVL/FreeNet-Router/releases"
	selfUpdateReleaseClient = &http.Client{Timeout: 12 * time.Second}
	selfUpdateReleaseCache = struct {
		sync.Mutex
		at time.Time
		items []selfUpdateRelease
	}{}
)

type selfUpdateRelease struct {
	Version     string `json:"version"`
	PublishedAt string `json:"published_at,omitempty"`
	Current     bool   `json:"current"`
	Latest      bool   `json:"latest"`
}

type selfUpdateReleaseCatalogResponse struct {
	Success        bool                `json:"success"`
	CurrentVersion string              `json:"current_version"`
	LatestVersion  string              `json:"latest_version,omitempty"`
	Releases       []selfUpdateRelease `json:"releases,omitempty"`
	Error          string              `json:"error,omitempty"`
}

type githubFreeNetRelease struct {
	TagName     string `json:"tag_name"`
	Draft       bool   `json:"draft"`
	Prerelease  bool   `json:"prerelease"`
	PublishedAt string `json:"published_at"`
}

func releaseVersionParts(tag string) ([3]int, bool) {
	var out [3]int
	if !validReleaseTag(tag) {
		return out, false
	}
	parts := strings.Split(strings.TrimPrefix(tag, "v"), ".")
	for i := range parts {
		var n int
		if _, err := fmt.Sscanf(parts[i], "%d", &n); err != nil {
			return [3]int{}, false
		}
		out[i] = n
	}
	return out, true
}

func releaseVersionGreater(a, b string) bool {
	av, aok := releaseVersionParts(a)
	bv, bok := releaseVersionParts(b)
	if !aok || !bok {
		return a > b
	}
	for i := 0; i < len(av); i++ {
		if av[i] != bv[i] {
			return av[i] > bv[i]
		}
	}
	return false
}

func normalizeSelfUpdateReleases(raw []githubFreeNetRelease, current string) []selfUpdateRelease {
	seen := make(map[string]bool)
	out := make([]selfUpdateRelease, 0, len(raw))
	for _, item := range raw {
		tag := strings.TrimSpace(item.TagName)
		if item.Draft || item.Prerelease || !validReleaseTag(tag) || seen[tag] {
			continue
		}
		seen[tag] = true
		out = append(out, selfUpdateRelease{
			Version: tag,
			PublishedAt: strings.TrimSpace(item.PublishedAt),
			Current: tag == current,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return releaseVersionGreater(out[i].Version, out[j].Version)
	})
	if len(out) > 0 {
		out[0].Latest = true
	}
	return out
}

func fetchSelfUpdateReleaseCatalog(ctx context.Context, current string) ([]selfUpdateRelease, error) {
	selfUpdateReleaseCache.Lock()
	if time.Since(selfUpdateReleaseCache.at) < 5*time.Minute && len(selfUpdateReleaseCache.items) > 0 {
		cached := append([]selfUpdateRelease(nil), selfUpdateReleaseCache.items...)
		selfUpdateReleaseCache.Unlock()
		for i := range cached {
			cached[i].Current = cached[i].Version == current
		}
		return cached, nil
	}
	selfUpdateReleaseCache.Unlock()

	all := make([]githubFreeNetRelease, 0, selfUpdateReleasePageSize)
	for page := 1; page <= selfUpdateReleaseMaxPages; page++ {
		url := fmt.Sprintf("%s?per_page=%d&page=%d", selfUpdateReleasesAPI, selfUpdateReleasePageSize, page)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("User-Agent", "FreeNet-Router/v"+version)
		resp, err := selfUpdateReleaseClient.Do(req)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusOK {
			_ = resp.Body.Close()
			return nil, fmt.Errorf("release catalog returned HTTP %d", resp.StatusCode)
		}
		var pageItems []githubFreeNetRelease
		dec := json.NewDecoder(io.LimitReader(resp.Body, selfUpdateReleaseBodyLimit))
		err = dec.Decode(&pageItems)
		_ = resp.Body.Close()
		if err != nil {
			return nil, errors.New("invalid release catalog response")
		}
		all = append(all, pageItems...)
		if len(pageItems) < selfUpdateReleasePageSize {
			break
		}
	}

	items := normalizeSelfUpdateReleases(all, current)
	if len(items) == 0 {
		return nil, errors.New("no published stable FreeNet releases found")
	}
	selfUpdateReleaseCache.Lock()
	selfUpdateReleaseCache.at = time.Now()
	selfUpdateReleaseCache.items = append([]selfUpdateRelease(nil), items...)
	selfUpdateReleaseCache.Unlock()
	return items, nil
}

func (a *app) handleSelfUpdateReleases(w http.ResponseWriter, r *http.Request) {
	current := "v" + version
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	items, err := fetchSelfUpdateReleaseCatalog(ctx, current)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, selfUpdateReleaseCatalogResponse{
			Success: false, CurrentVersion: current, Error: "cannot load FreeNet release catalog",
		})
		return
	}
	latest := ""
	if len(items) > 0 {
		latest = items[0].Version
	}
	writeJSON(w, http.StatusOK, selfUpdateReleaseCatalogResponse{
		Success: true,
		CurrentVersion: current,
		LatestVersion: latest,
		Releases: items,
	})
}
