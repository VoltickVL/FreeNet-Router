package main

import (
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
