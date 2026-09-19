package main

import "testing"

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
	if !releaseVersionGreater("v0.3.100", "v0.3.99") {
		t.Fatal("numeric patch comparison must not be lexical")
	}
	if releaseVersionGreater("v0.3.9", "v0.3.10") {
		t.Fatal("v0.3.9 must not sort after v0.3.10")
	}
}
