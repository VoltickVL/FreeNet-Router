package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNormalizeSelfUpdateReleasesStableSemanticOrder(t *testing.T) {
	raw := []githubFreeNetRelease{
		{TagName:"v0.3.9", PublishedAt:"2026-01-01T00:00:00Z"},
		{TagName:"v0.3.10", PublishedAt:"2026-01-02T00:00:00Z"},
		{TagName:"v0.3.8", Draft:true},
		{TagName:"v0.4.0", Prerelease:true},
		{TagName:"v0.3.10", PublishedAt:"2026-01-02T00:00:00Z"},
		{TagName:"not-a-release"},
	}
	got := normalizeSelfUpdateReleases(raw, "v0.3.9")
	if len(got) != 2 {
		t.Fatalf("stable release count=%d want=2: %+v", len(got), got)
	}
	if got[0].Version != "v0.3.10" || !got[0].Latest {
		t.Fatalf("latest stable ordering wrong: %+v", got)
	}
	if got[1].Version != "v0.3.9" || !got[1].Current {
		t.Fatalf("current version marker wrong: %+v", got)
	}
}

func TestReleaseCatalogCarriesBoundedPublishedNotes(t *testing.T) {
	long := strings.Repeat("П", selfUpdateReleaseNotesLimit+80)
	out := normalizeSelfUpdateReleases([]githubFreeNetRelease{
		{TagName:"v0.7.2", Body:"## What's Changed\n* Giga Stage-0\n* Обзор без ложного индикатора", PublishedAt:"2026-10-09T23:44:49Z"},
		{TagName:"v0.7.1", Body:long},
		{TagName:"v0.7.3", Body:"NEVER", Draft:true},
	}, "v0.7.1")
	if len(out)!=2 || out[0].Version!="v0.7.2" || out[1].Version!="v0.7.1" {t.Fatalf("unexpected catalog %+v",out)}
	if !strings.Contains(out[0].ReleaseNotes,"Giga Stage-0") || !strings.Contains(out[0].ReleaseNotes,"Обзор") {t.Fatalf("release body lost: %+v",out[0])}
	if len([]rune(out[1].ReleaseNotes))!=selfUpdateReleaseNotesLimit {t.Fatalf("unbounded release notes: %d",len([]rune(out[1].ReleaseNotes)))}
	if boundedSelfUpdateReleaseNotes("\r\n")!="" {t.Fatal("empty release body must remain absent")}
}

func TestReleaseVersionGreaterIsNumeric(t *testing.T) {
	if !releaseVersionGreater("v0.4.0", "v0.3.99") {
		t.Fatal("v0.4.0 must sort after v0.3.99")
	}
	if releaseVersionGreater("v0.3.9", "v0.3.10") {
		t.Fatal("v0.3.9 must not sort after v0.3.10")
	}
}

func TestReleaseTagPatchRolloverContract(t *testing.T) {
	if !validReleaseTag("v0.3.99") {
		t.Fatal("v0.3.99 must remain valid")
	}
	if !validReleaseTag("v0.4.0") {
		t.Fatal("v0.4.0 must be valid after v0.3.99")
	}
	if validReleaseTag("v0.3.100") {
		t.Fatal("v0.3.100 must be rejected; FreeNet rolls over to v0.4.0 after v0.3.99")
	}
}


func TestSelfUpdateReleaseCachePolicy(t *testing.T) {
	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	if selfUpdateReleaseCacheUsable(time.Time{}, 1, false, now) {
		t.Fatal("zero cache timestamp must not be usable")
	}
	if selfUpdateReleaseCacheUsable(now.Add(-10*time.Second), 0, false, now) {
		t.Fatal("empty cache must not be usable")
	}
	if !selfUpdateReleaseCacheUsable(now.Add(-4*time.Minute), 3, false, now) {
		t.Fatal("background request should use cache inside 5-minute TTL")
	}
	if selfUpdateReleaseCacheUsable(now.Add(-6*time.Minute), 3, false, now) {
		t.Fatal("background request must refresh after 5-minute TTL")
	}
	if !selfUpdateReleaseCacheUsable(now.Add(-30*time.Second), 3, true, now) {
		t.Fatal("explicit fresh request should reuse very recent cache to protect GitHub API")
	}
	if selfUpdateReleaseCacheUsable(now.Add(-90*time.Second), 3, true, now) {
		t.Fatal("explicit fresh request must bypass stale catalog after bounded minimum age")
	}
}


func resetSelfUpdateReleaseCacheForTest() {
	selfUpdateReleaseCache.Lock()
	selfUpdateReleaseCache.at = time.Time{}
	selfUpdateReleaseCache.items = nil
	selfUpdateReleaseCache.Unlock()
}

func TestSelfUpdateReleaseCatalogUsesResilientDownloader(t *testing.T) {
	oldDownload := selfUpdateReleaseDownload
	oldAPI := selfUpdateReleasesAPI
	defer func() {
		selfUpdateReleaseDownload = oldDownload
		selfUpdateReleasesAPI = oldAPI
		resetSelfUpdateReleaseCacheForTest()
	}()
	resetSelfUpdateReleaseCacheForTest()
	selfUpdateReleasesAPI = "https://catalog.test/releases"
	calls := 0
	selfUpdateReleaseDownload = func(_ context.Context, rawURL string, maxBytes int64) ([]byte, error) {
		calls++
		if maxBytes != selfUpdateReleaseBodyLimit {
			t.Fatalf("catalog maxBytes=%d want=%d", maxBytes, selfUpdateReleaseBodyLimit)
		}
		if !strings.Contains(rawURL, "per_page=100&page=1") {
			t.Fatalf("unexpected catalog URL %q", rawURL)
		}
		return []byte(`[{"tag_name":"v0.4.51","published_at":"2026-10-02T00:00:00Z","body":"## What\u0027s Changed\n* Обновление интерфейса"}]`), nil
	}
	items, err := fetchSelfUpdateReleaseCatalog(context.Background(), "v0.4.50", true)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("resilient downloader calls=%d want=1", calls)
	}
	if len(items) != 1 || items[0].Version != "v0.4.51" || !items[0].Latest || !strings.Contains(items[0].ReleaseNotes,"Обновление интерфейса") {
		t.Fatalf("unexpected catalog: %+v", items)
	}
}

func TestSelfUpdateReleaseHandlerFallsBackToRecoveryLatest(t *testing.T) {
	oldDownload := selfUpdateReleaseDownload
	oldAPI := selfUpdateReleasesAPI
	defer func() {
		selfUpdateReleaseDownload = oldDownload
		selfUpdateReleasesAPI = oldAPI
		resetSelfUpdateReleaseCacheForTest()
	}()
	resetSelfUpdateReleaseCacheForTest()
	selfUpdateReleaseDownload = func(context.Context, string, int64) ([]byte, error) {
		return nil, errors.New("primary resolver path failed")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/latest" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"tag_name":"v9.9.9"}`)
	}))
	defer server.Close()
	t.Setenv("FREENET_RECOVERY_LATEST_URL", server.URL+"/latest")
	history := filepath.Join(t.TempDir(), "history.tsv")
	t.Setenv("FREENET_SETTINGS_V3_HISTORY", history)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://router.test/api/system/update/releases?fresh=1", nil)
	(&app{}).handleSelfUpdateReleases(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	for _, want := range []string{`"success":true`, `"latest_version":"v9.9.9"`, `"degraded":true`, "резервный канал FreeNet"} {
		if !strings.Contains(body, want) {
			t.Fatalf("fallback response missing %q: %s", want, body)
		}
	}
	data, err := os.ReadFile(history)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "PRIMARY ERROR: release catalog page 1 unavailable: primary resolver path failed") {
		t.Fatalf("primary catalog error not journaled: %s", data)
	}
}


func TestSelfUpdateCanceledBrowserRequestDoesNotPolluteJournal(t *testing.T) {
	oldDownload := selfUpdateReleaseDownload
	oldAPI := selfUpdateReleasesAPI
	defer func() {
		selfUpdateReleaseDownload = oldDownload
		selfUpdateReleasesAPI = oldAPI
		resetSelfUpdateReleaseCacheForTest()
	}()
	resetSelfUpdateReleaseCacheForTest()
	selfUpdateReleasesAPI = "https://catalog.test/releases"
	selfUpdateReleaseDownload = func(ctx context.Context, _ string, _ int64) ([]byte, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	history := filepath.Join(t.TempDir(), "history.tsv")
	t.Setenv("FREENET_SETTINGS_V3_HISTORY", history)

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "http://router.test/api/system/update/releases?fresh=1", nil).WithContext(ctx)
	cancel()
	rr := httptest.NewRecorder()
	(&app{}).handleSelfUpdateReleases(rr, req)

	if data, err := os.ReadFile(history); err == nil && strings.TrimSpace(string(data)) != "" {
		t.Fatalf("browser cancellation leaked into persistent Journal: %s", data)
	}
}

func TestSelfUpdateRealCatalogFailureStillJournalsPrimaryError(t *testing.T) {
	oldDownload := selfUpdateReleaseDownload
	defer func() {
		selfUpdateReleaseDownload = oldDownload
		resetSelfUpdateReleaseCacheForTest()
	}()
	resetSelfUpdateReleaseCacheForTest()
	selfUpdateReleaseDownload = func(context.Context, string, int64) ([]byte, error) {
		return nil, errors.New("resolver failure")
	}
	history := filepath.Join(t.TempDir(), "history.tsv")
	t.Setenv("FREENET_SETTINGS_V3_HISTORY", history)
	// Force fallback to fail without involving request cancellation.
	t.Setenv("FREENET_RECOVERY_LATEST_URL", "http://127.0.0.1:1/unavailable")

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://router.test/api/system/update/releases?fresh=1", nil)
	(&app{}).handleSelfUpdateReleases(rr, req)

	data, err := os.ReadFile(history)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "PRIMARY ERROR: release catalog page 1 unavailable: resolver failure") {
		t.Fatalf("real catalog failure must remain visible: %s", data)
	}
}
