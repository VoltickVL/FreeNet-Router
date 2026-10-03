package main

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestMeasureProviderProfileRTTMeasuresSharedEndpointPerLogicalProfile(t *testing.T) {
	candidates := []bestServerInternalCandidate{
		{Profile: subscriptionProfile{ID: "aaaaaaaaaaaaaaaa", Name: "DE Frankfurt, Germany, Extra", CountryCode: "de", Address: "203.0.113.10", Port: 443}, Raw: "de"},
		{Profile: subscriptionProfile{ID: "bbbbbbbbbbbbbbbb", Name: "US New York, United States, Extra", CountryCode: "us", Address: "203.0.113.10", Port: 443}, Raw: "us"},
		{Profile: subscriptionProfile{ID: "cccccccccccccccc", Name: "FR Paris, France, Extra", CountryCode: "fr", Address: "203.0.113.20", Port: 443}, Raw: "fr"},
	}
	var mu sync.Mutex
	calls := map[string]int{}
	probe := func(_ context.Context, candidate bestServerInternalCandidate) bestServerProbeResult {
		mu.Lock()
		calls[candidate.Profile.ID]++
		mu.Unlock()
		switch candidate.Profile.ID {
		case "aaaaaaaaaaaaaaaa":
			return bestServerProbeResult{OK: true, Median: 82, Jitter: 4}
		case "bbbbbbbbbbbbbbbb":
			return bestServerProbeResult{OK: true, Median: 238, Jitter: 9}
		default:
			return bestServerProbeResult{OK: true, Median: 116, Jitter: 5}
		}
	}

	got := measureProviderProfileRTT(context.Background(), candidates, probe)
	for _, candidate := range candidates {
		if calls[candidate.Profile.ID] != 1 {
			t.Fatalf("logical profile %s calls=%d want=1; shared IP:port must not deduplicate proxy ping", candidate.Profile.ID, calls[candidate.Profile.ID])
		}
	}
	if len(got) != 3 {
		t.Fatalf("results=%d want=3", len(got))
	}
	if got[0].ProfileID != "aaaaaaaaaaaaaaaa" || got[0].RTTMS != 82 {
		t.Fatalf("fastest logical profile not first: %#v", got)
	}
	if got[2].ProfileID != "bbbbbbbbbbbbbbbb" || got[2].RTTMS != 238 {
		t.Fatalf("shared-ingress US profile must keep its own slower proxy RTT: %#v", got)
	}
}

func TestMeasureProviderProfileRTTPutsUnreachableLast(t *testing.T) {
	candidates := []bestServerInternalCandidate{
		{Profile: subscriptionProfile{ID: "aaaaaaaaaaaaaaaa", Name: "DE Frankfurt, Germany, Extra", CountryCode: "de", Address: "203.0.113.10", Port: 443}},
		{Profile: subscriptionProfile{ID: "bbbbbbbbbbbbbbbb", Name: "FR Paris, France, Extra", CountryCode: "fr", Address: "203.0.113.20", Port: 443}},
	}
	probe := func(_ context.Context, candidate bestServerInternalCandidate) bestServerProbeResult {
		if candidate.Profile.CountryCode == "fr" {
			return bestServerProbeResult{}
		}
		return bestServerProbeResult{OK: true, Median: 180, Jitter: 4}
	}
	got := measureProviderProfileRTT(context.Background(), candidates, probe)
	if len(got) != 2 || !got[0].Reachable || got[1].Reachable {
		t.Fatalf("unreachable result must be last: %#v", got)
	}
}

func TestUkraineExcludedFromReplacementPolicy(t *testing.T) {
	if !isUserExcludedVPNCountry("UA") {
		t.Fatal("Ukraine must be excluded from user replacement pool")
	}
	if got := normalizeAutomationCountries([]string{"de", "UA", "pl"}); len(got) != 2 || got[0] != "de" || got[1] != "pl" {
		t.Fatalf("automation country normalization kept Ukraine: %#v", got)
	}
	settings := automationSettings{CountryScope: automationCountryAllowlist, Countries: []string{"de", "ua", "pl"}}
	if automationCountryAllowed(settings, "de", "ua") {
		t.Fatal("AUTO VPN must not allow Ukraine as replacement")
	}
	candidates := []bestServerInternalCandidate{
		{Profile: subscriptionProfile{ID: "aaaaaaaaaaaaaaaa", Name: "UA Kyiv, Ukraine, Extra", CountryCode: "ua", Address: "203.0.113.1", Port: 443}},
		{Profile: subscriptionProfile{ID: "bbbbbbbbbbbbbbbb", Name: "DE Berlin, Germany, Extra", CountryCode: "de", Address: "203.0.113.2", Port: 443}},
	}
	filtered := filterForeignBestServerCandidates(candidates)
	if len(filtered) != 1 || filtered[0].Profile.CountryCode != "de" {
		t.Fatalf("Best Server replacement pool kept Ukraine: %#v", filtered)
	}
	options := settingsCountryOptions([]subscriptionProfile{
		{ID: "aaaaaaaaaaaaaaaa", Name: "UA Kyiv, Ukraine, Extra", CountryCode: "ua", Address: "203.0.113.1", Port: 443},
		{ID: "bbbbbbbbbbbbbbbb", Name: "DE Berlin, Germany, Extra", CountryCode: "de", Address: "203.0.113.2", Port: 443},
	}, []string{"ua", "de"})
	for _, option := range options {
		if option.Code == "ua" {
			t.Fatalf("settings country catalog kept Ukraine: %#v", options)
		}
	}
}


func TestMeasureProviderProfileRTTBoundsConcurrentProbes(t *testing.T) {
	candidates := []bestServerInternalCandidate{
		{Profile: subscriptionProfile{ID: "aaaaaaaaaaaaaaaa"}},
		{Profile: subscriptionProfile{ID: "bbbbbbbbbbbbbbbb"}},
		{Profile: subscriptionProfile{ID: "cccccccccccccccc"}},
		{Profile: subscriptionProfile{ID: "dddddddddddddddd"}},
	}
	started := make(chan struct{}, len(candidates))
	release := make(chan struct{})
	done := make(chan struct{})
	probe := func(_ context.Context, _ bestServerInternalCandidate) bestServerProbeResult {
		started <- struct{}{}
		<-release
		return bestServerProbeResult{OK: true, Median: 100}
	}
	go func() {
		_ = measureProviderProfileRTT(context.Background(), candidates, probe)
		close(done)
	}()

	for i := 0; i < providerProfileRTTWorkers; i++ {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("expected bounded workers to start")
		}
	}
	select {
	case <-started:
		t.Fatalf("more than %d isolated Xray probes started concurrently", providerProfileRTTWorkers)
	case <-time.After(30 * time.Millisecond):
	}
	close(release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("bounded probe sweep did not finish")
	}
}

func TestMeasureProviderProfileRTTDeadlineMarksUnstartedUnknown(t *testing.T) {
	candidates := []bestServerInternalCandidate{
		{Profile: subscriptionProfile{ID: "aaaaaaaaaaaaaaaa"}},
		{Profile: subscriptionProfile{ID: "bbbbbbbbbbbbbbbb"}},
		{Profile: subscriptionProfile{ID: "cccccccccccccccc"}},
		{Profile: subscriptionProfile{ID: "dddddddddddddddd"}},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	probe := func(ctx context.Context, _ bestServerInternalCandidate) bestServerProbeResult {
		<-ctx.Done()
		return bestServerProbeResult{}
	}
	got := measureProviderProfileRTT(ctx, candidates, probe)
	attempted, unknown := 0, 0
	for _, item := range got {
		if item.Attempted {
			attempted++
		}
		if !item.Attempted && item.Status == "unknown" {
			unknown++
		}
	}
	if attempted != 0 {
		t.Fatalf("deadline-expired probes must remain UNKNOWN rather than explicit failures: attempted=%d got=%#v", attempted, got)
	}
	if unknown != len(candidates) {
		t.Fatalf("unknown=%d want=%d; timed-out or unstarted profiles must not be labeled unreachable: %#v", unknown, len(candidates), got)
	}
}

func TestProviderProfileRTTSortUsesUnknownAsReserve(t *testing.T) {
	items := []providerProfileRTTItem{
		{ProfileID: "unknown", Status: "unknown"},
		{ProfileID: "slow", Reachable: true, Attempted: true, Status: "reachable", RTTMS: 210},
		{ProfileID: "fast", Reachable: true, Attempted: true, Status: "reachable", RTTMS: 150},
		{ProfileID: "dead", Attempted: true, Status: "unreachable"},
	}
	sortProviderProfileRTTItems(items)
	want := []string{"fast", "slow", "unknown", "dead"}
	for i, id := range want {
		if items[i].ProfileID != id {
			t.Fatalf("RTT order=%#v want=%#v", items, want)
		}
	}
}


func TestProviderProfileRTTScanSingleFlight(t *testing.T) {
	endProviderProfileRTTScan()
	if !beginProviderProfileRTTScan() {
		t.Fatal("first RTT scan must acquire gate")
	}
	if beginProviderProfileRTTScan() {
		endProviderProfileRTTScan()
		t.Fatal("second concurrent RTT scan must be rejected")
	}
	endProviderProfileRTTScan()
	if !beginProviderProfileRTTScan() {
		t.Fatal("gate must be reusable after release")
	}
	endProviderProfileRTTScan()
}

func TestProviderProfileRTTGuardsAcquireOnceAndReleaseBoth(t *testing.T) {
	t.Setenv("FREENET_AUTO_HEALTH_LOCK", t.TempDir()+"/auto-health.lock")
	endProviderProfileRTTScan()
	a := &app{sem: make(chan struct{}, 1)}

	release, reason := acquireProviderProfileRTTGuards(a)
	if release == nil || reason != "" {
		t.Fatalf("first guard acquisition failed: release=%v reason=%q", release != nil, reason)
	}
	if len(a.sem) != 1 {
		t.Fatalf("FreeNet operation semaphore occupancy=%d want=1", len(a.sem))
	}
	if beginProviderProfileRTTScan() {
		endProviderProfileRTTScan()
		t.Fatal("RTT single-flight gate was not held with operation semaphore")
	}

	if fenceRelease, err := acquireAutomationHealthLock(); err == nil {
		fenceRelease()
		t.Fatal("provider RTT scan must hold AUTO health fence")
	}

	other := &app{sem: make(chan struct{}, 1)}
	if secondRelease, secondReason := acquireProviderProfileRTTGuards(other); secondRelease != nil || secondReason != "AUTO VPN health/recovery operation is already running" {
		if secondRelease != nil {
			secondRelease()
		}
		t.Fatalf("second app guard result release=%v reason=%q", secondRelease != nil, secondReason)
	}

	release()
	if len(a.sem) != 0 {
		t.Fatalf("FreeNet operation semaphore was not released: occupancy=%d", len(a.sem))
	}
	if !beginProviderProfileRTTScan() {
		t.Fatal("RTT single-flight gate was not released")
	}
	endProviderProfileRTTScan()
	fenceRelease, err := acquireAutomationHealthLock()
	if err != nil {
		t.Fatalf("AUTO health fence was not released: %v", err)
	}
	fenceRelease()
}



func resetProviderProfileRTTCacheForTest() {
	providerProfileRTTCache.Lock()
	providerProfileRTTCache.Entry = providerProfileRTTCacheEntry{}
	providerProfileRTTCache.Unlock()
}

func TestProviderProfileRTTCacheReusesCompleteCanonicalSweep(t *testing.T) {
	resetProviderProfileRTTCacheForTest()
	defer resetProviderProfileRTTCacheForTest()
	candidates := []bestServerInternalCandidate{
		{Profile: subscriptionProfile{ID: "aaaaaaaaaaaaaaaa", Address: "203.0.113.10", Port: 443}, Raw: "vless://credential-a@203.0.113.10:443#A"},
		{Profile: subscriptionProfile{ID: "bbbbbbbbbbbbbbbb", Address: "203.0.113.20", Port: 443}, Raw: "vless://credential-b@203.0.113.20:443#B"},
	}
	items := []providerProfileRTTItem{
		{ProfileID: "bbbbbbbbbbbbbbbb", Reachable: true, Attempted: true, Status: "reachable", RTTMS: 180},
		{ProfileID: "aaaaaaaaaaaaaaaa", Reachable: true, Attempted: true, Status: "reachable", RTTMS: 120},
	}
	storeProviderProfileRTTCache(candidates, items)
	got, measuredAt, ok := loadProviderProfileRTTCache(candidates)
	if !ok || measuredAt.IsZero() {
		t.Fatal("complete RTT sweep was not cached")
	}
	if len(got) != 2 || got[0].ProfileID != "aaaaaaaaaaaaaaaa" || got[0].RTTMS != 120 {
		t.Fatalf("cached RTT sweep is not canonical/sorted: %#v", got)
	}
}

func TestProviderProfileRTTCacheRejectsPartialOrRotatedCatalog(t *testing.T) {
	resetProviderProfileRTTCacheForTest()
	defer resetProviderProfileRTTCacheForTest()
	candidates := []bestServerInternalCandidate{
		{Profile: subscriptionProfile{ID: "aaaaaaaaaaaaaaaa", Address: "203.0.113.10", Port: 443}},
		{Profile: subscriptionProfile{ID: "bbbbbbbbbbbbbbbb", Address: "203.0.113.20", Port: 443}},
	}
	partial := []providerProfileRTTItem{
		{ProfileID: "aaaaaaaaaaaaaaaa", Reachable: true, Attempted: true, Status: "reachable", RTTMS: 120},
		{ProfileID: "bbbbbbbbbbbbbbbb", Attempted: false, Status: "unknown"},
	}
	storeProviderProfileRTTCache(candidates, partial)
	if _, _, ok := loadProviderProfileRTTCache(candidates); ok {
		t.Fatal("partial RTT sweep must never become canonical selector cache")
	}

	complete := []providerProfileRTTItem{
		{ProfileID: "aaaaaaaaaaaaaaaa", Reachable: true, Attempted: true, Status: "reachable", RTTMS: 120},
		{ProfileID: "bbbbbbbbbbbbbbbb", Reachable: false, Attempted: true, Status: "unreachable"},
	}
	storeProviderProfileRTTCache(candidates, complete)
	rotated := append([]bestServerInternalCandidate(nil), candidates...)
	rotated[0].Profile.Address = "198.51.100.77"
	if _, _, ok := loadProviderProfileRTTCache(rotated); ok {
		t.Fatal("RTT cache must invalidate when subscription endpoint snapshot rotates")
	}
	credentialRotated := append([]bestServerInternalCandidate(nil), candidates...)
	credentialRotated[0].Raw = "vless://credential-a-rotated@203.0.113.10:443#A"
	if _, _, ok := loadProviderProfileRTTCache(credentialRotated); ok {
		t.Fatal("RTT cache must invalidate when credentials rotate under the same logical ID/endpoint")
	}
}
