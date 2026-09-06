package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func testPolicyPreviewAPI(t *testing.T) (*app, *http.ServeMux, *http.Cookie) {
	t.Helper()
	a := testAuthApp(t)
	if err := a.createCredential("correct horse battery staple"); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	registerPolicyPreviewAPI(mux, a)
	loginReq := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
	loginW := httptest.NewRecorder()
	if err := a.newSession(loginW, loginReq); err != nil {
		t.Fatal(err)
	}
	return a, mux, loginW.Result().Cookies()[0]
}

func doPolicyPreviewRequest(mux *http.ServeMux, cookie *http.Cookie, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, "/api/policy/compile", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

func TestPolicyPreviewAPIRequiresAuthentication(t *testing.T) {
	_, mux, _ := testPolicyPreviewAPI(t)
	w := doPolicyPreviewRequest(mux, nil, `{"rules":[{"selector":{"kind":"domain","value":"example.com"},"action":"DIRECT"}]}`)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
}

func TestPolicyPreviewAPIUsesCanonicalCompiler(t *testing.T) {
	_, mux, cookie := testPolicyPreviewAPI(t)
	body := `{"rules":[` +
		`{"selector":{"kind":"domain","value":"Plati.Market."},"action":"direct"},` +
		`{"selector":{"kind":"geoip","value":"RU"},"action":"VPN"}` +
		`]}`
	w := doPolicyPreviewRequest(mux, cookie, body)
	if w.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	var resp policyPreviewResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.Success || resp.Mutation != "NONE" || len(resp.Compiled.Rules) != 2 {
		t.Fatalf("response=%+v", resp)
	}
	first := resp.Compiled.Rules[0]
	if first.Selector.Value != "plati.market" || first.PayloadOutbound != "direct" || first.DNSLeg != "dns-direct" {
		t.Fatalf("domain compiler mismatch: %+v", first)
	}
	second := resp.Compiled.Rules[1]
	if second.PayloadOutbound != "vless-reality" || second.DNSLeg != "" {
		t.Fatalf("geoip compiler mismatch: %+v", second)
	}
}

func TestPolicyPreviewAPIRejectsInvalidRuleBeforeMutation(t *testing.T) {
	_, mux, cookie := testPolicyPreviewAPI(t)
	w := doPolicyPreviewRequest(mux, cookie, `{"rules":[{"selector":{"kind":"domain","value":"not-a-domain"},"action":"VPN"}]}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("code=%d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"mutation":"NONE"`) {
		t.Fatalf("missing no-mutation contract: %s", w.Body.String())
	}
}

func TestPolicyPreviewAPIRejectsCrossOriginUnknownFieldsAndTrailingJSON(t *testing.T) {
	_, mux, cookie := testPolicyPreviewAPI(t)

	r := httptest.NewRequest(http.MethodPost, "/api/policy/compile", strings.NewReader(`{"rules":[{"selector":{"kind":"domain","value":"example.com"},"action":"DIRECT"}]}`))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Sec-Fetch-Site", "cross-site")
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("cross-origin code=%d body=%s", w.Code, w.Body.String())
	}

	w = doPolicyPreviewRequest(mux, cookie, `{"rules":[{"selector":{"kind":"domain","value":"example.com"},"action":"DIRECT"}],"unexpected":true}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("unknown field code=%d body=%s", w.Code, w.Body.String())
	}

	w = doPolicyPreviewRequest(mux, cookie, `{"rules":[{"selector":{"kind":"domain","value":"example.com"},"action":"DIRECT"}]} {}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("trailing JSON code=%d body=%s", w.Code, w.Body.String())
	}
}

func TestPolicyPreviewAPIBoundsRulesAndBody(t *testing.T) {
	_, mux, cookie := testPolicyPreviewAPI(t)
	w := doPolicyPreviewRequest(mux, cookie, `{"rules":[]}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("empty code=%d body=%s", w.Code, w.Body.String())
	}

	var b strings.Builder
	b.WriteString(`{"rules":[`)
	for i := 0; i < maxPolicyPreviewRules+1; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(`{"selector":{"kind":"domain","value":"d`)
		b.WriteString(strings.Repeat("x", 3))
		b.WriteString(`.example.com"},"action":"DIRECT"}`)
	}
	b.WriteString(`]}`)
	w = doPolicyPreviewRequest(mux, cookie, b.String())
	if w.Code != http.StatusBadRequest {
		t.Fatalf("too many rules code=%d body=%s", w.Code, w.Body.String())
	}

	over := `{"rules":[{"selector":{"kind":"domain","value":"example.com"},"action":"DIRECT"}],"padding":"` + strings.Repeat("x", maxPolicyPreviewBodyBytes) + `"}`
	w = doPolicyPreviewRequest(mux, cookie, over)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("oversize code=%d body=%s", w.Code, w.Body.String())
	}
}
