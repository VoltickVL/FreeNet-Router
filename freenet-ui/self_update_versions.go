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
	selfUpdateReleaseNotesLimit = 4000
)

var (
	selfUpdateReleasesAPI = "https://api.github.com/repos/VoltickVL/FreeNet-Router/releases"
	selfUpdateReleaseDownload = recoveryDownload
	selfUpdateReleaseCache = struct {
		sync.Mutex
		at time.Time
		stable []selfUpdateRelease
		beta []selfUpdateRelease
	}{}
)

type selfUpdateRelease struct {
	Version     string `json:"version"`
	PublishedAt string `json:"published_at,omitempty"`
	ReleaseNotes string `json:"release_notes,omitempty"`
	Current     bool   `json:"current"`
	Latest      bool   `json:"latest"`
	Prerelease  bool   `json:"prerelease,omitempty"`
}

type selfUpdateReleaseCatalogResponse struct {
	Success        bool                `json:"success"`
	CurrentVersion string              `json:"current_version"`
	LatestVersion  string              `json:"latest_version,omitempty"`
	Channel        string              `json:"channel,omitempty"`
	Releases       []selfUpdateRelease `json:"releases,omitempty"`
	Degraded       bool                `json:"degraded,omitempty"`
	Warning        string              `json:"warning,omitempty"`
	Error          string              `json:"error,omitempty"`
}

type githubFreeNetRelease struct {
	TagName     string `json:"tag_name"`
	Body        string `json:"body"`
	Draft       bool   `json:"draft"`
	Prerelease  bool   `json:"prerelease"`
	PublishedAt string `json:"published_at"`
}

func releaseVersionParts(tag string) ([3]int, bool) {
	var out [3]int
	if !validSelectableReleaseTag(tag) {
		return out, false
	}
	base := strings.SplitN(strings.TrimPrefix(tag, "v"), "-beta.", 2)[0]
	parts := strings.Split(base, ".")
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
		return false
	}
	for i := 0; i < len(av); i++ {
		if av[i] != bv[i] {
			return av[i] > bv[i]
		}
	}
	// For identical core version, stable > any beta; higher beta number wins.
	aBeta, bBeta := validBetaReleaseTag(a), validBetaReleaseTag(b)
	if aBeta != bBeta {
		return !aBeta
	}
	if aBeta {
		aSeq := strings.TrimPrefix(strings.SplitN(a, "-beta.", 2)[1], "")
		bSeq := strings.SplitN(b, "-beta.", 2)[1]
		var an, bn int
		_, _ = fmt.Sscanf(aSeq, "%d", &an)
		_, _ = fmt.Sscanf(bSeq, "%d", &bn)
		return an > bn
	}
	return false
}

// Release notes are GitHub-published metadata, never input to a shell command.
// Keep them small enough for router memory and render as text only in the browser.
func boundedSelfUpdateReleaseNotes(body string) string {
	body = strings.TrimSpace(strings.ToValidUTF8(strings.ReplaceAll(strings.ReplaceAll(body, "\r\n", "\n"), "\r", "\n"), ""))
	runes := []rune(body)
	if len(runes) > selfUpdateReleaseNotesLimit {
		body = string(runes[:selfUpdateReleaseNotesLimit])
	}
	return strings.TrimSpace(body)
}

func normalizeSelfUpdateReleases(raw []githubFreeNetRelease, current string) []selfUpdateRelease {
	return normalizeSelfUpdateReleasesForChannel(raw, current, "stable")
}

func normalizeSelfUpdateReleasesForChannel(raw []githubFreeNetRelease, current, channel string) []selfUpdateRelease {
	seen := make(map[string]bool)
	out := make([]selfUpdateRelease, 0, len(raw))
	for _, item := range raw {
		tag := strings.TrimSpace(item.TagName)
		if item.Draft || seen[tag] {
			continue
		}
		if channel == "beta" {
			if !item.Prerelease || !validBetaReleaseTag(tag) {
				continue
			}
		} else if item.Prerelease || !validReleaseTag(tag) {
			continue
		}
		seen[tag] = true
		out = append(out, selfUpdateRelease{
			Version: tag, PublishedAt: strings.TrimSpace(item.PublishedAt),
			ReleaseNotes: boundedSelfUpdateReleaseNotes(item.Body),
			Current: tag == current, Prerelease: item.Prerelease,
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
	return fetchSelfUpdateReleaseCatalogForChannel(ctx, current, forceFresh, "stable")
}

func fetchSelfUpdateReleaseCatalogForChannel(ctx context.Context, current string, forceFresh bool, channel string) ([]selfUpdateRelease, error) {
	if channel != "stable" && channel != "beta" {
		return nil, errors.New("unknown FreeNet release channel")
	}
	selfUpdateReleaseCache.Lock()
	// GitHub returns Stable and Beta in one response. Cache both filtered
	// views from that snapshot, so channel switching does not repeat
	// the remote download (important for unreliable router connections).
	usable := selfUpdateReleaseCacheUsable(selfUpdateReleaseCache.at,
		len(selfUpdateReleaseCache.stable)+len(selfUpdateReleaseCache.beta), forceFresh, time.Now())
	if usable {
		selected := selfUpdateReleaseCache.stable
		if channel == "beta" {
			selected = selfUpdateReleaseCache.beta
		}
		cached := append([]selfUpdateRelease(nil), selected...)
		selfUpdateReleaseCache.Unlock()
		if len(cached) == 0 {
			return nil, fmt.Errorf("no published %s FreeNet releases found", channel)
		}
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

	stable := normalizeSelfUpdateReleasesForChannel(all, current, "stable")
	beta := normalizeSelfUpdateReleasesForChannel(all, current, "beta")
	items := stable
	if channel == "beta" {
		items = beta
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("no published %s FreeNet releases found", channel)
	}
	selfUpdateReleaseCache.Lock()
	selfUpdateReleaseCache.at = time.Now()
	selfUpdateReleaseCache.stable = append([]selfUpdateRelease(nil), stable...)
	selfUpdateReleaseCache.beta = append([]selfUpdateRelease(nil), beta...)
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

func selfUpdateRequestCanceled(r *http.Request, err error) bool {
	return r != nil &&
		errors.Is(r.Context().Err(), context.Canceled) &&
		errors.Is(err, context.Canceled)
}

func (a *app) handleSelfUpdateReleases(w http.ResponseWriter, r *http.Request) {
	current := "v" + version
	channel := strings.TrimSpace(r.URL.Query().Get("channel"))
	if channel == "" {
		channel = "stable"
	}
	if channel != "stable" && channel != "beta" {
		writeJSON(w, http.StatusBadRequest, selfUpdateReleaseCatalogResponse{Success:false,CurrentVersion:current,Error:"invalid release channel"})
		return
	}
	deadline := 25 * time.Second
	if channel == "beta" {
		// Cold Beta catalog failures must return a JSON STOP before common
		// remote reverse-proxy timeouts; a warm cache returns immediately.
		deadline = 10 * time.Second
	}
	ctx, cancel := context.WithTimeout(r.Context(), deadline)
	forceFresh := r.URL.Query().Get("fresh") == "1"
	items, err := fetchSelfUpdateReleaseCatalogForChannel(ctx, current, forceFresh, channel)
	cancel()
	degraded := false
	warning := ""
	if err != nil {
		// Closing/navigating away from the browser cancels the request context.
		// That is not a FreeNet/system failure and must not pollute the persistent
		// Journal. Real resolver/API/manifest errors and timeouts remain visible.
		if selfUpdateRequestCanceled(r, err) {
			return
		}
		primary := err.Error()
		v3AppendEvent("freenet_release_catalog", "degraded", "PRIMARY ERROR: "+primary)
		// /releases/latest is always Stable. Do not silently present that as
		// the Beta catalog or install Stable when explicit Beta lookup fails.
		if channel == "beta" {
			writeJSON(w, http.StatusBadGateway, selfUpdateReleaseCatalogResponse{
				Success:false, CurrentVersion:current, Channel:channel,
				Error:"Каталог Beta сейчас недоступен. Обновление не запущено; причина сохранена в Журнале FreeNet.",
			})
			return
		}
		fallbackCtx, fallbackCancel := context.WithTimeout(r.Context(), 15*time.Second)
		latest, fallbackErr := recoveryLatestTag(fallbackCtx)
		fallbackCancel()
		if fallbackErr != nil {
			if selfUpdateRequestCanceled(r, fallbackErr) {
				return
			}
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
		Channel: channel,
		Releases: items,
		Degraded: degraded,
		Warning: warning,
	})
}
