package main

import (
	"encoding/binary"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestGeoSiteDetectAndSearchFullAndPartial(t *testing.T) {
	data := testGeoSiteList(
		testGeoSiteEntry("YOUTUBE",
			testDomainRule(2, "youtube.com"),
			testDomainRule(3, "youtu.be"),
			testDomainRule(0, "googlevideo"),
		),
		testGeoSiteEntry("PLATI", testDomainRule(2, "plati.market")),
	)
	kind, err := DetectGeoDataKind(data)
	if err != nil || kind != GeoDataSite {
		t.Fatalf("kind=%q err=%v", kind, err)
	}

	for query, want := range map[string][]string{
		"www.youtube.com": {"youtube"},
		"youtube":         {"youtube"},
		"plati.market":    {"plati"},
		"plati":           {"plati"},
		"foo.googlevideo": {"youtube"},
	} {
		got, err := SearchGeoSiteData(data, query)
		if err != nil {
			t.Fatalf("query %q: %v", query, err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("query %q got=%v want=%v", query, got, want)
		}
	}
}

func TestGeoSiteRegexAndDeterministicSort(t *testing.T) {
	data := testGeoSiteList(
		testGeoSiteEntry("ZZZ", testDomainRule(1, `^api[0-9]+\.example\.com$`)),
		testGeoSiteEntry("AAA", testDomainRule(2, "example.com")),
	)
	got, err := SearchGeoSiteData(data, "api12.example.com")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"aaa", "zzz"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got=%v want=%v", got, want)
	}
}

func TestGeoIPDetectAndSearchIPv4IPv6(t *testing.T) {
	data := testGeoIPList(
		testGeoIPEntry("PRIVATE", false,
			testCIDR(net.ParseIP("10.0.0.0").To4(), 8),
			testCIDR(net.ParseIP("192.168.0.0").To4(), 16),
		),
		testGeoIPEntry("CLOUDFLARE", false, testCIDR(net.ParseIP("1.1.1.0").To4(), 24)),
		testGeoIPEntry("DOCV6", false, testCIDR(net.ParseIP("2001:db8::").To16(), 32)),
	)
	kind, err := DetectGeoDataKind(data)
	if err != nil || kind != GeoDataIP {
		t.Fatalf("kind=%q err=%v", kind, err)
	}

	cases := map[string][]string{
		"10.42.1.9":   {"private"},
		"1.1.1.1":     {"cloudflare"},
		"2001:db8::42": {"docv6"},
		"8.8.8.8":     {},
	}
	for ip, want := range cases {
		got, err := SearchGeoIPData(data, ip)
		if err != nil {
			t.Fatalf("ip %q: %v", ip, err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("ip %q got=%v want=%v", ip, got, want)
		}
	}
}

func TestGeoIPReverseMatch(t *testing.T) {
	data := testGeoIPList(testGeoIPEntry("NOT_PRIVATE", true, testCIDR(net.ParseIP("10.0.0.0").To4(), 8)))
	got, err := SearchGeoIPData(data, "8.8.8.8")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{"not_private"}) {
		t.Fatalf("got=%v", got)
	}
	got, err = SearchGeoIPData(data, "10.1.2.3")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("reverse category matched excluded IP: %v", got)
	}
}

func TestGeoDataRejectsMalformedAndNonLiteralIP(t *testing.T) {
	if _, err := DetectGeoDataKind([]byte{0x0a, 0xff}); err == nil {
		t.Fatal("expected malformed protobuf error")
	}
	if _, err := SearchGeoIPData(testGeoIPList(), "example.com"); err == nil {
		t.Fatal("expected literal IP validation error")
	}
	if _, err := SearchGeoSiteData(testGeoSiteList(), "  "); err == nil {
		t.Fatal("expected empty geosite query error")
	}
}

func TestDiscoverGeoDataFilesOnlyLocalRegularDat(t *testing.T) {
	dir := t.TempDir()
	sitePath := filepath.Join(dir, "geosite.dat")
	ipPath := filepath.Join(dir, "geoip.dat")
	if err := os.WriteFile(sitePath, testGeoSiteList(testGeoSiteEntry("TEST", testDomainRule(2, "example.com"))), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ipPath, testGeoIPList(testGeoIPEntry("TEST", false, testCIDR(net.ParseIP("203.0.113.0").To4(), 24))), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ignore.txt"), []byte("not geodata"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "broken.dat"), []byte{0x0a, 0xff}, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(sitePath, filepath.Join(dir, "linked.dat")); err != nil {
		t.Fatal(err)
	}

	files, err := DiscoverGeoDataFiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 3 {
		t.Fatalf("files=%+v", files)
	}
	if files[0].Name != "broken.dat" || files[0].Kind != GeoDataUnknown || files[0].Error == "" {
		t.Fatalf("broken file not reported safely: %+v", files[0])
	}
	if files[1].Name != "geoip.dat" || files[1].Kind != GeoDataIP || files[1].Error != "" {
		t.Fatalf("geoip=%+v", files[1])
	}
	if files[2].Name != "geosite.dat" || files[2].Kind != GeoDataSite || files[2].Error != "" {
		t.Fatalf("geosite=%+v", files[2])
	}
	for _, file := range files {
		if file.Name == "linked.dat" || file.Name == "ignore.txt" {
			t.Fatalf("unexpected unmanaged file: %+v", file)
		}
	}
}

func TestDiscoverGeoDataFilesReportsOversizeWithoutReading(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "huge.dat")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(maxGeoDataFileSize + 1); err != nil {
		f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	files, err := DiscoverGeoDataFiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Kind != GeoDataUnknown || !strings.Contains(files[0].Error, "exceeds") {
		t.Fatalf("oversize result=%+v", files)
	}
}

func testGeoSiteList(entries ...[]byte) []byte {
	var out []byte
	for _, entry := range entries {
		out = testPBBytes(out, 1, entry)
	}
	return out
}

func testGeoSiteEntry(code string, domains ...[]byte) []byte {
	var out []byte
	out = testPBBytes(out, 1, []byte(code))
	for _, domain := range domains {
		out = testPBBytes(out, 2, domain)
	}
	return out
}

func testDomainRule(domainType uint64, value string) []byte {
	var out []byte
	if domainType != 0 {
		out = testPBVarint(out, 1, domainType)
	}
	out = testPBBytes(out, 2, []byte(value))
	return out
}

func testGeoIPList(entries ...[]byte) []byte {
	var out []byte
	for _, entry := range entries {
		out = testPBBytes(out, 1, entry)
	}
	return out
}

func testGeoIPEntry(code string, reverse bool, cidrs ...[]byte) []byte {
	var out []byte
	out = testPBBytes(out, 1, []byte(code))
	for _, cidr := range cidrs {
		out = testPBBytes(out, 2, cidr)
	}
	if reverse {
		out = testPBVarint(out, 3, 1)
	}
	return out
}

func testCIDR(ip []byte, prefix uint64) []byte {
	var out []byte
	out = testPBBytes(out, 1, ip)
	if prefix != 0 {
		out = testPBVarint(out, 2, prefix)
	}
	return out
}

func testPBBytes(dst []byte, field int, value []byte) []byte {
	dst = testPBAppendUvarint(dst, uint64(field<<3|2))
	dst = testPBAppendUvarint(dst, uint64(len(value)))
	return append(dst, value...)
}

func testPBVarint(dst []byte, field int, value uint64) []byte {
	dst = testPBAppendUvarint(dst, uint64(field<<3))
	return testPBAppendUvarint(dst, value)
}

func testPBAppendUvarint(dst []byte, value uint64) []byte {
	var buf [binary.MaxVarintLen64]byte
	n := binary.PutUvarint(buf[:], value)
	return append(dst, buf[:n]...)
}
