package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestGitHubStableTagFromLocationRequiresOfficialStableRedirect(t *testing.T) {
	good := []string{
		"https://github.com/VoltickVL/FreeNet-Router/releases/tag/v0.6.24",
		"/VoltickVL/FreeNet-Router/releases/tag/v0.6.24",
	}
	for _, item := range good {
		tag, err := githubStableTagFromLocation(item)
		if err != nil || tag != "v0.6.24" {
			t.Fatalf("official latest Location %q: tag=%q err=%v", item, tag, err)
		}
	}
	bad := []string{
		"https://evil.example/VoltickVL/FreeNet-Router/releases/tag/v0.6.24",
		"https://github.com.evil.example/VoltickVL/FreeNet-Router/releases/tag/v0.6.24",
		"http://github.com/VoltickVL/FreeNet-Router/releases/tag/v0.6.24",
		"https://user@github.com/VoltickVL/FreeNet-Router/releases/tag/v0.6.24",
		"https://github.com/Other/FreeNet-Router/releases/tag/v0.6.24",
		"https://github.com/VoltickVL/FreeNet-Router/releases/download/v0.6.24/SHA256SUMS",
		"https://github.com/VoltickVL/FreeNet-Router/releases/tag/v0.6.24?token=test",
		"https://github.com/VoltickVL/FreeNet-Router/releases/tag/v0.6.24#fragment",
		"https://github.com/VoltickVL/FreeNet-Router/releases/tag/v0.6.100",
		"https://github.com/VoltickVL/FreeNet-Router/releases/tag/v0.6.24-rc",
		"https://github.com/VoltickVL/FreeNet-Router/releases/tag/v0.6.24/extra",
		"https://github.com/VoltickVL/FreeNet-Router/releases/tag/%76%30%2e%36%2e%32%34",
		"//evil.example/VoltickVL/FreeNet-Router/releases/tag/v0.6.24",
		"",
	}
	for _, item := range bad {
		if tag, err := githubStableTagFromLocation(item); err == nil {
			t.Fatalf("untrusted redirect accepted: location=%q tag=%q", item, tag)
		}
	}
}

func TestGitHubWebsiteLatestBypassesRESTAndNeverFollowsExternalRedirect(t *testing.T) {
	old := githubReleaseWebLatestURL
	t.Cleanup(func() { githubReleaseWebLatestURL = old })
	var heads, gets atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodHead:
			heads.Add(1)
			w.Header().Set("Location", "https://github.com/VoltickVL/FreeNet-Router/releases/tag/v0.6.24")
			w.WriteHeader(http.StatusFound)
		case http.MethodGet:
			gets.Add(1)
			http.Error(w, "this is not a release document", http.StatusInternalServerError)
		}
	}))
	defer srv.Close()
	githubReleaseWebLatestURL = srv.URL + "/latest"
	t.Setenv("FREENET_RECOVERY_LATEST_URL", "")
	tag, err := recoveryLatestTag(context.Background())
	if err != nil || tag != "v0.6.24" {
		t.Fatalf("official GitHub redirect should be enough without REST: tag=%q err=%v", tag, err)
	}
	if heads.Load() != 1 || gets.Load() != 0 {
		t.Fatalf("web latest must be a single HEAD, not page scraping or unsafe following: head=%d get=%d", heads.Load(), gets.Load())
	}
}

func TestGitHubWebLatestRejectsUnexpectedHTMLAndForeignRedirect(t *testing.T) {
	old := githubReleaseWebLatestURL
	t.Cleanup(func() { githubReleaseWebLatestURL = old })
	for _, result := range []struct{
		status int
		location string
	}{
		{http.StatusOK, ""},
		{http.StatusFound, "https://untrusted.example/releases/tag/v0.6.24"},
		{http.StatusFound, "https://github.com/VoltickVL/FreeNet-Router/releases/tag/v0.6.24?fake=1"},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Location", result.location)
			w.WriteHeader(result.status)
			fmt.Fprint(w, "<html>v0.6.24</html>")
		}))
		githubReleaseWebLatestURL = srv.URL + "/latest"
		if tag, err := githubLatestStableWebTag(context.Background()); err == nil {
			t.Fatalf("unsafe web response %d / %q accepted as tag %q", result.status, result.location, tag)
		}
		srv.Close()
	}
}

func TestRecoveryDownloadStopsImmediatelyOnRateLimit(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.Header().Set("X-RateLimit-Reset", "1910000000")
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, `{"message":"API rate limit exceeded for some other user private-key"}`)
	}))
	defer srv.Close()
	_, err := recoveryDownload(context.Background(), srv.URL+"/releases/latest", 4096)
	if err == nil || !strings.Contains(err.Error(), "GitHub API rate limit exceeded (HTTP 403)") ||
		!strings.Contains(err.Error(), "reset ") {
		t.Fatalf("rate limit must be classified and include safe reset: %v", err)
	}
	if strings.Contains(err.Error(), "private-key") {
		t.Fatalf("rate-limit details must not leak provider response body: %v", err)
	}
	if requests.Load() != 1 {
		t.Fatalf("rate-limited REST must not be retried through alternate resolvers: calls=%d", requests.Load())
	}
}
