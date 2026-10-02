package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	selfUpdateReleaseCacheTTL = 5 * time.Minute
	selfUpdateReleaseFreshMinAge = 1 * time.Minute
)

var (
	selfUpdateReleasesAPI = "https://api.github.com/repos/VoltickVL/FreeNet-Router/releases"
	selfUpdateReleaseDownload = recoveryDownload
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
	Degraded       bool                `json:"degraded,omitempty"`
	Warning        string              `json:"warning,omitempty"`
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

func selfUpdateReleaseCacheUsable(at time.Time, itemCount int, forceFresh bool, now time.Time) bool {
	if itemCount == 0 || at.IsZero() {
		return false
	}
	age := now.Sub(at)
	if age < 0 {
		return false
	}
	if forceFresh {
		return age < selfUpdateReleaseFreshMinAge
	}
	return age < selfUpdateReleaseCacheTTL
}

func fetchSelfUpdateReleaseCatalog(ctx context.Context, current string, forceFresh bool) ([]selfUpdateRelease, error) {
	selfUpdateReleaseCache.Lock()
	if selfUpdateReleaseCacheUsable(selfUpdateReleaseCache.at, len(selfUpdateReleaseCache.items), forceFresh, time.Now()) {
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
		body, err := selfUpdateReleaseDownload(ctx, url, selfUpdateReleaseBodyLimit)
		if err != nil {
			return nil, fmt.Errorf("release catalog page %d unavailable: %w", page, err)
		}
		var pageItems []githubFreeNetRelease
		if err := json.Unmarshal(body, &pageItems); err != nil {
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

func fallbackSelfUpdateReleaseCatalog(current, latest string) []selfUpdateRelease {
	items := make([]selfUpdateRelease, 0, 2)
	if validReleaseTag(latest) {
		items = append(items, selfUpdateRelease{Version: latest, Current: latest == current, Latest: true})
	}
	if validReleaseTag(current) && current != latest {
		items = append(items, selfUpdateRelease{Version: current, Current: true})
	}
	return items
}

func (a *app) handleSelfUpdateReleases(w http.ResponseWriter, r *http.Request) {
	current := "v" + version
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	forceFresh := r.URL.Query().Get("fresh") == "1"
	items, err := fetchSelfUpdateReleaseCatalog(ctx, current, forceFresh)
	cancel()
	degraded := false
	warning := ""
	if err != nil {
		primary := err.Error()
		v3AppendEvent("freenet_release_catalog", "degraded", "PRIMARY ERROR: "+primary)
		fallbackCtx, fallbackCancel := context.WithTimeout(r.Context(), 15*time.Second)
		latest, fallbackErr := recoveryLatestTag(fallbackCtx)
		fallbackCancel()
		if fallbackErr != nil {
			v3AppendEvent("freenet_release_catalog", "failed", "Latest fallback failed: "+fallbackErr.Error())
			writeJSON(w, http.StatusBadGateway, selfUpdateReleaseCatalogResponse{
				Success: false, CurrentVersion: current, Error: "cannot load FreeNet release catalog",
			})
			return
		}
		items = fallbackSelfUpdateReleaseCatalog(current, latest)
		if len(items) == 0 {
			writeJSON(w, http.StatusBadGateway, selfUpdateReleaseCatalogResponse{
				Success: false, CurrentVersion: current, Error: "cannot load FreeNet release catalog",
			})
			return
		}
		degraded = true
		warning = "Полный каталог версий временно недоступен. Последний релиз получен через резервный канал FreeNet."
	}
	latest := ""
	for _, item := range items {
		if item.Latest {
			latest = item.Version
			break
		}
	}
	if latest == "" && len(items) > 0 {
		latest = items[0].Version
	}
	writeJSON(w, http.StatusOK, selfUpdateReleaseCatalogResponse{
		Success: true,
		CurrentVersion: current,
		LatestVersion: latest,
		Releases: items,
		Degraded: degraded,
		Warning: warning,
	})
}
