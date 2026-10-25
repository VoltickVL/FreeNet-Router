package main

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

const betaAtomFixture = `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom">
  <entry><id>tag:github.com,2008:Repository/1353743848/v0.8.0-beta.29</id>
    <updated>2026-10-25T08:06:35Z</updated><link rel="alternate" type="text/html"
    href="https://github.com/VoltickVL/FreeNet-Router/releases/tag/v0.8.0-beta.29"/></entry>
  <entry><id>tag:github.com,2008:Repository/1353743848/v0.8.0-beta.28</id>
    <updated>2026-10-25T00:28:36Z</updated><link rel="alternate"
    href="https://github.com/VoltickVL/FreeNet-Router/releases/tag/v0.8.0-beta.28"/></entry>
  <entry><id>tag:github.com,2008:Repository/1353743848/v0.7.18</id>
    <updated>2026-10-24T00:00:00Z</updated><link rel="alternate"
    href="https://github.com/VoltickVL/FreeNet-Router/releases/tag/v0.7.18"/></entry>
  <entry><id>tag:github.com,2008:Repository/1353743848/v0.8.0-beta.30</id>
    <updated>2026-10-26T00:00:00Z</updated><link rel="alternate"
    href="https://github.com/foreign/repo/releases/tag/v0.8.0-beta.30"/></entry>
  <entry><id>tag:github.com,2008:Repository/1353743848/v0.8.0-beta.31</id>
    <updated>not-a-date</updated><link rel="alternate"
    href="https://github.com/VoltickVL/FreeNet-Router/releases/tag/v0.8.0-beta.31"/></entry>
</feed>`

func TestPublishedBetaAtomFiltersExactRepositoryAndStable(t *testing.T) {
	items, err := parsePublishedBetaReleaseFeed([]byte(betaAtomFixture), "v0.8.0-beta.28")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("accepted a Stable/foreign/invalid entry: %+v", items)
	}
	if items[0].Version != "v0.8.0-beta.29" || !items[0].Latest || !items[0].Prerelease ||
		items[0].ReleaseNotes != "" || items[1].Version != "v0.8.0-beta.28" || !items[1].Current {
		t.Fatalf("published-only Beta ordering and no unverified notes: %+v", items)
	}
}

func TestPublishedBetaAtomRejectsMalformedAndOversized(t *testing.T) {
	for _, data := range [][]byte{
		nil, []byte("<feed>"), []byte("<feed/>"),
		[]byte(strings.Repeat("x", selfUpdateBetaAtomBodyLimit+1)),
		[]byte(`<feed><entry><id>x</id><updated>2026-10-25T08:06:35Z</updated><link rel="alternate" href="https://other.test/releases/tag/v0.8.0-beta.99"/></entry></feed>`),
	} {
		if got, err := parsePublishedBetaReleaseFeed(data, "v0.8.0-beta.29"); err == nil || len(got) != 0 {
			t.Fatalf("malformed/untrusted Beta feed accepted: %d bytes, %+v", len(data), got)
		}
	}
}

func TestBetaCatalogUsesSmallPublishedFeedAndCaches(t *testing.T) {
	oldDownload, oldFeed := selfUpdateReleaseDownload, selfUpdateBetaFeedURL
	t.Cleanup(func() {
		selfUpdateReleaseDownload, selfUpdateBetaFeedURL = oldDownload, oldFeed
		resetSelfUpdateReleaseCacheForTest()
	})
	resetSelfUpdateReleaseCacheForTest()
	selfUpdateBetaFeedURL = "https://github.com/VoltickVL/FreeNet-Router/releases.atom"
	calls := 0
	selfUpdateReleaseDownload = func(_ context.Context, rawURL string, maxBytes int64) ([]byte, error) {
		calls++
		if rawURL != selfUpdateBetaFeedURL || maxBytes != selfUpdateBetaAtomBodyLimit {
			t.Fatalf("Beta fetched heavy REST releases or unbounded bytes: %q, %d", rawURL, maxBytes)
		}
		return []byte(betaAtomFixture), nil
	}
	for i := 0; i < 2; i++ {
		items, err := fetchSelfUpdateReleaseCatalogForChannel(context.Background(), "v0.8.0-beta.28", true, "beta")
		if err != nil || len(items) != 2 || !items[0].Latest {
			t.Fatalf("published Beta snapshot: %+v %v", items, err)
		}
	}
	if calls != 1 {
		t.Fatalf("warm cache should not repeat Atom download: %d", calls)
	}
}

func TestSlowBetaFeedRespectsCancelAndNeverUsesStableLatest(t *testing.T) {
	oldDownload := selfUpdateReleaseDownload
	t.Cleanup(func() { selfUpdateReleaseDownload = oldDownload; resetSelfUpdateReleaseCacheForTest() })
	resetSelfUpdateReleaseCacheForTest()
	selfUpdateReleaseDownload = func(ctx context.Context, url string, maxBytes int64) ([]byte, error) {
		if strings.Contains(url, "releases/latest") || strings.Contains(url, "?per_page=") {
			t.Fatalf("Beta degraded into Stable/latest or heavy REST: %q", url)
		}
		<-ctx.Done()
		return nil, ctx.Err()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Millisecond)
	defer cancel()
	items, err := fetchSelfUpdateReleaseCatalogForChannel(ctx, "v0.8.0-beta.29", true, "beta")
	if !errors.Is(err, context.DeadlineExceeded) || len(items) != 0 {
		t.Fatalf("slow Beta must stop without invented releases or mutation: %+v %v", items, err)
	}
}

func TestBetaExactPickerHasFailClosedPlan(t *testing.T) {
	src, err := os.ReadFile("web/self-update.js")
	if err != nil {
		t.Fatal(err)
	}
	js := string(src)
	for _, required := range []string{
		"fnVersionExactBeta", "Проверить тег",
		"if (!betaTag(tag))", "selectVersionTarget({version: tag, prerelease: true}, detail)",
		"p.manifest_verified", "p.target_tag !== release.version",
		"option.id === 'beta'", "releases: [], degraded: true",
		"p.release_notes",
	} {
		if !strings.Contains(js, required) {
			t.Fatalf("exact Beta fail-closed picker contract missing: %q", required)
		}
	}
}
