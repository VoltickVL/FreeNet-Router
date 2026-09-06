package main

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
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

func TestGeoDataFilesAPIIsDeterministicAndSanitizesErrors(t *testing.T) {
	_, mux, cookie, dir := testGeoDataAPIApp(t)
	writeTestGeoDataFiles(t, dir)
	w := doGeoDataAPIRequest(mux, cookie, "/api/geodata/files")
	if w.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	var resp geoDataFilesResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.Success || len(resp.Files) != 3 {
		t.Fatalf("response=%+v", resp)
	}
	if resp.Files[0].Name != "broken.dat" || resp.Files[1].Name != "geoip.dat" || resp.Files[2].Name != "geosite.dat" {
		t.Fatalf("files are not sorted: %+v", resp.Files)
	}
	if resp.Files[0].Error != geoDataGenericFileError {
		t.Fatalf("raw parse error leaked: %+v", resp.Files[0])
	}
	if strings.Contains(w.Body.String(), dir) {
		t.Fatalf("filesystem path leaked: %s", w.Body.String())
	}
}

func TestGeoDataSearchAPIFindsGeoSiteAndGeoIP(t *testing.T) {
	_, mux, cookie, dir := testGeoDataAPIApp(t)
	writeTestGeoDataFiles(t, dir)

	w := doGeoDataAPIRequest(mux, cookie, "/api/geodata/search?kind=geosite&q=youtube")
	if w.Code != http.StatusOK {
		t.Fatalf("geosite code=%d body=%s", w.Code, w.Body.String())
	}
	var site geoDataSearchResponse
	if err := json.Unmarshal(w.Body.Bytes(), &site); err != nil {
		t.Fatal(err)
	}
	if !site.Success || len(site.Matches) != 1 || site.Matches[0].File != "geosite.dat" || len(site.Matches[0].Categories) != 1 || site.Matches[0].Categories[0] != "youtube" {
		t.Fatalf("geosite response=%+v", site)
	}
	if len(site.Warnings) != 1 || !strings.HasPrefix(site.Warnings[0], "broken.dat:") {
		t.Fatalf("expected isolated broken-file warning: %+v", site.Warnings)
	}

	w = doGeoDataAPIRequest(mux, cookie, "/api/geodata/search?kind=geoip&q=1.1.1.1")
	if w.Code != http.StatusOK {
		t.Fatalf("geoip code=%d body=%s", w.Code, w.Body.String())
	}
	var ip geoDataSearchResponse
	if err := json.Unmarshal(w.Body.Bytes(), &ip); err != nil {
		t.Fatal(err)
	}
	if !ip.Success || len(ip.Matches) != 1 || ip.Matches[0].File != "geoip.dat" || len(ip.Matches[0].Categories) != 1 || ip.Matches[0].Categories[0] != "cloudflare" {
		t.Fatalf("geoip response=%+v", ip)
	}
}

func TestGeoDataSearchAPIRejectsTraversalUnknownAndWrongKind(t *testing.T) {
	_, mux, cookie, dir := testGeoDataAPIApp(t)
	writeTestGeoDataFiles(t, dir)
	cases := []string{
		"/api/geodata/search?kind=geosite&q=youtube&file=../geosite.dat",
		"/api/geodata/search?kind=geosite&q=youtube&file=missing.dat",
		"/api/geodata/search?kind=geosite&q=youtube&file=geoip.dat",
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

func TestGeoDataSearchAPIBoundsFileSelectionAndQuery(t *testing.T) {
	_, mux, cookie, dir := testGeoDataAPIApp(t)
	writeTestGeoDataFiles(t, dir)
	values := url.Values{"kind": {"geosite"}, "q": {"youtube"}}
	for i := 0; i < maxGeoDataSelectedFiles+1; i++ {
		values.Add("file", "geosite.dat")
	}
	w := doGeoDataAPIRequest(mux, cookie, "/api/geodata/search?"+values.Encode())
	if w.Code != http.StatusBadRequest {
		t.Fatalf("too many files code=%d body=%s", w.Code, w.Body.String())
	}

	longQuery := strings.Repeat("a", maxGeoDataQueryLength+1)
	w = doGeoDataAPIRequest(mux, cookie, "/api/geodata/search?kind=geosite&q="+longQuery)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("long query code=%d body=%s", w.Code, w.Body.String())
	}
	w = doGeoDataAPIRequest(mux, cookie, "/api/geodata/search?kind=geoip&q=example.com")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("non-literal geoip query code=%d body=%s", w.Code, w.Body.String())
	}

	for i := 0; i < maxGeoDataSelectedFiles; i++ {
		name := "extra-" + strconv.Itoa(i) + ".dat"
		if err := os.WriteFile(filepath.Join(dir, name), []byte{0x0a, 0xff}, 0600); err != nil {
			t.Fatal(err)
		}
	}
	w = doGeoDataAPIRequest(mux, cookie, "/api/geodata/search?kind=geosite&q=youtube")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("implicit unbounded search code=%d body=%s", w.Code, w.Body.String())
	}
}

func TestGeoDataSearchAPIExplicitFileAndNoRawDataLeak(t *testing.T) {
	_, mux, cookie, dir := testGeoDataAPIApp(t)
	writeTestGeoDataFiles(t, dir)
	w := doGeoDataAPIRequest(mux, cookie, "/api/geodata/search?kind=geosite&q=plati&file=geosite.dat")
	if w.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if strings.Contains(body, dir) || strings.Contains(body, "youtube.com") {
		t.Fatalf("raw file/path leaked: %s", body)
	}
	if !strings.Contains(body, `"plati"`) {
		t.Fatalf("expected category missing: %s", body)
	}
}
