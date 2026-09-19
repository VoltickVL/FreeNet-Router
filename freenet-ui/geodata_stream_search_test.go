package main

import (
	"context"
	"encoding/binary"
	"errors"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestGeoDataStreamSearchMatchesLegacySemantics(t *testing.T) {
	dir := t.TempDir()
	siteData := testGeoSiteList(
		testGeoSiteEntry("YOUTUBE",
			testDomainRule(2, "youtube.com"),
			testDomainRule(3, "youtu.be"),
			testDomainRule(0, "googlevideo"),
		),
		testGeoSiteEntry("PLATI", testDomainRule(2, "plati.market")),
	)
	sitePath := filepath.Join(dir, "geosite.dat")
	if err := os.WriteFile(sitePath, siteData, 0600); err != nil {
		t.Fatal(err)
	}

	for query, want := range map[string][]string{
		"www.youtube.com": {"youtube"},
		"youtube":         {"youtube"},
		"plati.market":    {"plati"},
		"foo.googlevideo": {"youtube"},
	} {
		got, err := searchGeoDataFileStream(context.Background(), sitePath, GeoDataSite, query, 128)
		if err != nil {
			t.Fatalf("query %q: %v", query, err)
		}
		if !got.Observed || got.Truncated || !reflect.DeepEqual(got.Categories, want) {
			t.Fatalf("query %q got=%+v want=%v", query, got, want)
		}
	}

	ipPath := filepath.Join(dir, "geoip.dat")
	ipData := testGeoIPList(
		testGeoIPEntry("TEST-NET", false, testCIDR(net.ParseIP("203.0.113.0").To4(), 24)),
		testGeoIPEntry("NOT-PRIVATE", true, testCIDR(net.ParseIP("10.0.0.0").To4(), 8)),
	)
	if err := os.WriteFile(ipPath, ipData, 0600); err != nil {
		t.Fatal(err)
	}
	got, err := searchGeoDataFileStream(context.Background(), ipPath, GeoDataIP, "203.0.113.9", 128)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"not-private", "test-net"}
	if !reflect.DeepEqual(got.Categories, want) {
		t.Fatalf("geoip got=%v want=%v", got.Categories, want)
	}
}

func TestGeoDataStreamSearchRejectsKindMismatchWithoutPathLeak(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mystery.dat")
	if err := os.WriteFile(path, testGeoSiteList(testGeoSiteEntry("YOUTUBE", testDomainRule(2, "youtube.com"))), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := searchGeoDataFileStream(context.Background(), path, GeoDataIP, "1.1.1.1", 128)
	if !errors.Is(err, errGeoDataStreamKindMismatch) {
		t.Fatalf("err=%v want kind mismatch", err)
	}
	if strings.Contains(err.Error(), dir) {
		t.Fatalf("filesystem path leaked: %v", err)
	}
}

func TestGeoDataStreamSearchHonorsCancellation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "geosite.dat")
	if err := os.WriteFile(path, testGeoSiteList(testGeoSiteEntry("YOUTUBE", testDomainRule(2, "youtube.com"))), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := searchGeoDataFileStream(ctx, path, GeoDataSite, "youtube", 128)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v want context canceled", err)
	}
}

func TestGeoDataStreamSearchRejectsOversizeEntryBeforeAllocation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "geosite.dat")
	var data []byte
	data = testPBAppendUvarint(data, uint64(1<<3|2))
	data = testPBAppendUvarint(data, uint64(maxGeoDataStreamEntrySize+1))
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	_, err := searchGeoDataFileStream(context.Background(), path, GeoDataSite, "youtube", 128)
	if err == nil || !strings.Contains(err.Error(), "entry exceeds safe size") {
		t.Fatalf("err=%v", err)
	}
}

func TestGeoDataStreamSearchCapsCategories(t *testing.T) {
	entries := make([][]byte, 0, 8)
	for i := 0; i < 8; i++ {
		entries = append(entries, testGeoSiteEntry(
			"CAT"+string(rune('A'+i)),
			testDomainRule(0, "example"),
		))
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "geosite.dat")
	if err := os.WriteFile(path, testGeoSiteList(entries...), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := searchGeoDataFileStream(context.Background(), path, GeoDataSite, "example.com", 3)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Truncated || len(got.Categories) != 3 {
		t.Fatalf("result=%+v", got)
	}
}

func TestGeoDataStreamSearchRejectsSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.dat")
	link := filepath.Join(dir, "linked.dat")
	if err := os.WriteFile(target, testGeoSiteList(testGeoSiteEntry("TEST", testDomainRule(2, "example.com"))), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, err := searchGeoDataFileStream(context.Background(), link, GeoDataSite, "example.com", 128); err == nil {
		t.Fatal("symlink search unexpectedly succeeded")
	}
}

func TestGeoDataStreamUvarintRejectsOverflow(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "broken.dat")
	data := make([]byte, binary.MaxVarintLen64)
	for i := range data {
		data[i] = 0xff
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	_, err := searchGeoDataFileStream(context.Background(), path, GeoDataSite, "example.com", 128)
	if err == nil {
		t.Fatal("overflow varint unexpectedly succeeded")
	}
}
