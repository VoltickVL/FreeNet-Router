package main

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRoutingApplyUIClosesCanonicalRouteRace(t *testing.T) {
	automation, err := automationWebFS.ReadFile("web/automation.js")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(automation), `routingPage.dataset.pageView = 'routing'`) {
		t.Fatal("accepted shell race precondition changed: automation no longer renames network to routing")
	}

	js := string(routingApplyUIAsset)
	for _, required := range []string{
		`[data-page-view="routing"]`,
		`[data-page-view="network"]`,
		`/routing-v2.js?canonical-retry=1`,
		`routingV2Workspace`,
		`policyBuilderPreview`,
	} {
		if !strings.Contains(js, required) {
			t.Fatalf("routing canonical compatibility layer missing %q", required)
		}
	}
}

func TestRoutingApplyUIRequiresValidatedCandidateAndFailClosedStop(t *testing.T) {
	js := string(routingApplyUIAsset)
	for _, required := range []string{
		`/api/routing/config`,
		`/api/routing/validate`,
		`/api/routing/apply`,
		`validatedCandidate`,
		`xray_valid === true`,
		`ROLLBACK FAILED/UNKNOWN = STOP`,
		`rollback === 'FAILED'`,
		`rollback === 'UNKNOWN'`,
		`mutation === 'STOP'`,
		`не повторяйте mutation`,
	} {
		if !strings.Contains(js, required) {
			t.Fatalf("routing apply lifecycle missing %q", required)
		}
	}
	for _, forbidden := range []string{"vless://", "privateKey", "shortId", "subscription_url"} {
		if strings.Contains(js, forbidden) {
			t.Fatalf("routing UI must not expose secret-bearing token %q", forbidden)
		}
	}
}

func TestCanonicalIndexEmbedsRoutingApplyUI(t *testing.T) {
	a := &app{}
	req := httptest.NewRequest("GET", "http://router.local/", nil)
	rr := httptest.NewRecorder()
	a.handleIndex(rr, req)
	if rr.Code != 200 {
		t.Fatalf("base index status=%d", rr.Code)
	}
	html, err := canonicalizeControlCenterIndex(rr.Body.String())
	if err != nil {
		t.Fatal(err)
	}
	routingAt := strings.Index(html, `<script src="/routing-v2.js"></script>`)
	applyAt := strings.Index(html, `<script id="freenetRoutingApplyUI">`)
	releaseAt := strings.Index(html, `id="freenetCanonicalBootRelease"`)
	if routingAt < 0 || applyAt < 0 || releaseAt < 0 {
		t.Fatalf("canonical routing scripts missing: routing=%d apply=%d release=%d", routingAt, applyAt, releaseAt)
	}
	if !(routingAt < applyAt && applyAt < releaseAt) {
		t.Fatalf("routing apply compatibility must load after Routing v2 and before boot release: routing=%d apply=%d release=%d", routingAt, applyAt, releaseAt)
	}
}
