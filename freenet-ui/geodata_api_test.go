package main

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testGeoDataAPIApp(t *testing.T) (*app, *http.ServeMux, *http.Cookie, string) {
	t.Helper()
	a := testAuthApp(t)
	dir := t.TempDir()
	a.cfg.GeoDataDir = dir
	if err := a.createCredential("correct horse battery staple"); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	registerGeoDataAPI(mux, a)
	loginReq := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
	loginW := httptest.NewRecorder()
	if err := a.newSession(loginW, loginReq); err != nil {
		t.Fatal(err)
	}
	return a, mux, loginW.Result().Cookies()[0], dir
}

func writeTestGeoDataFiles(t *testing.T, dir string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "geosite.dat"), testGeoSiteList(
		testGeoSiteEntry("YOUTUBE", testDomainRule(2, "youtube.com")),
		testGeoSiteEntry("PLATI", testDomainRule(2, "plati.market")),
	), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "geoip.dat"), testGeoIPList(
		testGeoIPEntry("CLOUDFLARE", false, testCIDR(net.ParseIP("1.1.1.0").To4(), 24)),
	), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "broken.dat"), []byte{0x0a, 0xff}, 0600); err != nil {
		t.Fatal(err)
	}
}

func doGeoDataAPIRequest(mux *http.ServeMux, cookie *http.Cookie, rawURL string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodGet, rawURL, nil)
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

func TestGeoDataAPIRequiresAuthentication(t *testing.T) {
	_, mux, _, dir := testGeoDataAPIApp(t)
	writeTestGeoDataFiles(t, dir)
	w := doGeoDataAPIRequest(mux, nil, "/api/geodata/files")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous code=%d body=%s", w.Code, w.Body.String())
	}
}

func TestGeoDataFilesAPIIsMetadataOnlyAndDeterministic(t *testing.T) {
	_, mux, cookie, dir := testGeoDataAPIApp(t)
	writeTestGeoDataFiles(t, dir)

	// The old discovery path parsed every .dat file and would mark this invalid
	// protobuf as an error. The containment path must list metadata only.
	w := doGeoDataAPIRequest(mux, cookie, "/api/geodata/files")
	if w.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	var resp geoDataFilesResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.Success || !resp.SearchEnabled || len(resp.Files) != 3 {
		t.Fatalf("response=%+v", resp)
	}
	if resp.Files[0].Name != "broken.dat" || resp.Files[1].Name != "geoip.dat" || resp.Files[2].Name != "geosite.dat" {
		t.Fatalf("files are not sorted: %+v", resp.Files)
	}
	if resp.Files[0].Error != "" || resp.Files[0].Kind != GeoDataUnknown {
		t.Fatalf("file contents were unexpectedly interpreted: %+v", resp.Files[0])
	}
	if resp.Files[1].Kind != GeoDataIP || resp.Files[2].Kind != GeoDataSite {
		t.Fatalf("filename hints missing: %+v", resp.Files)
	}
	if strings.Contains(w.Body.String(), dir) {
		t.Fatalf("filesystem path leaked: %s", w.Body.String())
	}
}

func TestGeoDataFilesAPIDoesNotRejectOversizeContent(t *testing.T) {
	_, mux, cookie, dir := testGeoDataAPIApp(t)
	path := filepath.Join(dir, "geosite-large.dat")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(maxGeoDataFileSize + 1); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	w := doGeoDataAPIRequest(mux, cookie, "/api/geodata/files")
	if w.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	var resp geoDataFilesResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Files) != 1 || resp.Files[0].Size != maxGeoDataFileSize+1 || resp.Files[0].Error != "" {
		t.Fatalf("metadata-only listing failed: %+v", resp.Files)
	}
}

func TestGeoDataSearchAPIReturnsBoundedResultsAndWarnings(t *testing.T) {
	_, mux, cookie, dir := testGeoDataAPIApp(t)
	writeTestGeoDataFiles(t, dir)

	w := doGeoDataAPIRequest(mux, cookie, "/api/geodata/search?kind=geosite&q=youtube")
	if w.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	var resp geoDataSearchResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.Success || resp.Mutation != "NONE" || resp.Kind != GeoDataSite || resp.Query != "youtube" {
		t.Fatalf("response=%+v", resp)
	}
	if len(resp.Matches) != 1 || resp.Matches[0].File != "geosite.dat" || len(resp.Matches[0].Categories) != 1 || resp.Matches[0].Categories[0] != "youtube" {
		t.Fatalf("matches=%+v", resp.Matches)
	}
	if len(resp.Warnings) != 1 || !strings.Contains(resp.Warnings[0], "broken.dat") {
		t.Fatalf("broken optional file warning missing: %+v", resp.Warnings)
	}
	if strings.Contains(w.Body.String(), dir) {
		t.Fatalf("filesystem path leaked: %s", w.Body.String())
	}
}

func TestGeoDataSearchAPIGeoIPAndExplicitFile(t *testing.T) {
	_, mux, cookie, dir := testGeoDataAPIApp(t)
	writeTestGeoDataFiles(t, dir)

	w := doGeoDataAPIRequest(mux, cookie, "/api/geodata/search?kind=geoip&q=1.1.1.7&file=geoip.dat")
	if w.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	var resp geoDataSearchResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.Success || len(resp.Warnings) != 0 || len(resp.Matches) != 1 {
		t.Fatalf("response=%+v", resp)
	}
	if got := resp.Matches[0].Categories; len(got) != 1 || got[0] != "cloudflare" {
		t.Fatalf("categories=%v", got)
	}
}

func TestGeoDataSearchAPIRejectsTraversalAndInvalidQuery(t *testing.T) {
	_, mux, cookie, dir := testGeoDataAPIApp(t)
	writeTestGeoDataFiles(t, dir)

	cases := []string{
		"/api/geodata/search?kind=geosite&q=youtube&file=../geosite.dat",
		"/api/geodata/search?kind=geosite&q=",
		"/api/geodata/search?kind=geoip&q=example.com",
		"/api/geodata/search?kind=unknown&q=youtube",
	}
	for _, rawURL := range cases {
		w := doGeoDataAPIRequest(mux, cookie, rawURL)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("url=%s code=%d body=%s", rawURL, w.Code, w.Body.String())
		}
		if strings.Contains(w.Body.String(), dir) {
			t.Fatalf("filesystem path leaked: %s", w.Body.String())
		}
	}
}

func TestGeoDataSearchAPISkipsOversizeWithoutReading(t *testing.T) {
	_, mux, cookie, dir := testGeoDataAPIApp(t)
	path := filepath.Join(dir, "geosite-large.dat")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(maxGeoDataFileSize + 1); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	w := doGeoDataAPIRequest(mux, cookie, "/api/geodata/search?kind=geosite&q=youtube&file=geosite-large.dat")
	if w.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	var resp geoDataSearchResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.Success || len(resp.Matches) != 0 || len(resp.Warnings) != 1 || !strings.Contains(resp.Warnings[0], "safe search size") {
		t.Fatalf("response=%+v", resp)
	}
	if strings.Contains(w.Body.String(), dir) {
		t.Fatalf("filesystem path leaked: %s", w.Body.String())
	}
}

func TestGeoDataSearchAPIRequiresAuthentication(t *testing.T) {
	_, mux, _, dir := testGeoDataAPIApp(t)
	writeTestGeoDataFiles(t, dir)
	w := doGeoDataAPIRequest(mux, nil, "/api/geodata/search?kind=geosite&q=youtube")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
}

