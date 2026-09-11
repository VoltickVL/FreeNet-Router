package main

import "testing"

func TestFilterForeignBestServerCandidatesExcludesRussia(t *testing.T) {
	input := []bestServerInternalCandidate{
		{Profile: subscriptionProfile{ID: "ru", Name: "RU Russia, Extra Whitelist", CountryCode: "ru", Address: "10.0.0.1", Port: 443}},
		{Profile: subscriptionProfile{ID: "pl", Name: "PL Warsaw, Extra", CountryCode: "pl", Address: "10.0.0.2", Port: 443}},
		{Profile: subscriptionProfile{ID: "ru-fallback", Name: "Russia, Extra", Address: "10.0.0.3", Port: 443}},
	}
	got := filterForeignBestServerCandidates(input)
	if len(got) != 1 || got[0].Profile.ID != "pl" {
		t.Fatalf("foreign suggestions = %#v, want only PL", got)
	}
}

func TestFilterMeasuredBestServerResultsKeepsRejectedDeepProbe(t *testing.T) {
	input := []bestServerQualityCandidate{
		{ID: "good", Tested: true, Available: true, Eligible: true, DownloadMbps: 120, MediaSamples: bestServerMediaRequiredRuns},
		{ID: "near-miss", Tested: true, Available: true, Eligible: false, DownloadMbps: 0, MediaSamples: 2, Rejections: []string{"Скорость Speedtest не измерена"}},
		{ID: "app-failed", Tested: true, Reachable: true, Available: false, Eligible: false, Rejections: []string{"Не подтверждён HTTP-отклик через VPN"}},
		{ID: "untested", Reachable: true, Tested: false},
	}
	got := filterMeasuredBestServerResults(input)
	if len(got) != 3 {
		t.Fatalf("visible deep-probe results = %#v, want good + two rejected diagnostics", got)
	}
	if got[0].ID != "good" || got[1].ID != "near-miss" || got[2].ID != "app-failed" {
		t.Fatalf("unexpected visible result order: %#v", got)
	}
}

func TestMeasuredAlternativeTargetMatchesBrowserVisibleEndpoints(t *testing.T) {
	current := "203.0.113.9:443"
	input := []bestServerQualityCandidate{
		{ID: "eligible-current-listener", Endpoint: current, Tested: true, Available: true, Eligible: true, DownloadMbps: 115, MediaSamples: bestServerMediaRequiredRuns},
		{ID: "eligible-a", Endpoint: "203.0.113.10:443", Tested: true, Available: true, Eligible: true, DownloadMbps: 110, MediaSamples: bestServerMediaRequiredRuns},
		{ID: "eligible-same-listener", Endpoint: "203.0.113.10:443", Tested: true, Available: true, Eligible: true, DownloadMbps: 108, MediaSamples: bestServerMediaRequiredRuns},
		{ID: "eligible-b", Endpoint: "203.0.113.11:443", Tested: true, Available: true, Eligible: true, DownloadMbps: 100, MediaSamples: bestServerMediaRequiredRuns},
		{ID: "near-miss", Endpoint: "203.0.113.12:443", Tested: true, Available: true, Eligible: false, DownloadMbps: 130, MediaSamples: bestServerMediaRequiredRuns},
	}
	if got := measuredBestServerAlternativeCount(input, current); got != bestServerVisibleAlternatives {
		t.Fatalf("browser-visible measured endpoint count=%d want=%d; diagnostic deep result must occupy the third comparison slot without becoming switchable", got, bestServerVisibleAlternatives)
	}
}

func TestMeasuredAlternativeTargetSkipsUntestedAndDuplicateEndpoints(t *testing.T) {
	current := "203.0.113.9:443"
	input := []bestServerQualityCandidate{
		{ID: "a", Endpoint: "203.0.113.10:443", Tested: true, Available: true, Eligible: true},
		{ID: "duplicate-a", Endpoint: "203.0.113.10:443", Tested: true, Reachable: true},
		{ID: "untested", Endpoint: "203.0.113.11:443", Tested: false, Reachable: true},
		{ID: "current-listener", Endpoint: current, Tested: true, Reachable: true},
		{ID: "b", Endpoint: "203.0.113.12:443", Tested: true, Reachable: true},
	}
	if got := measuredBestServerAlternativeCount(input, current); got != 2 {
		t.Fatalf("browser-visible measured endpoint count=%d want=2", got)
	}
}

func TestBestServerCompletionPartialClearsAfterVisibleTop3(t *testing.T) {
	current := "203.0.113.9:443"
	input := []bestServerQualityCandidate{
		{ID: "a", Endpoint: "203.0.113.10:443", Tested: true, Available: true, Eligible: true},
		{ID: "b", Endpoint: "203.0.113.11:443", Tested: true, Available: true, Eligible: true},
		{ID: "c", Endpoint: "203.0.113.12:443", Tested: true, Reachable: true, Eligible: false},
	}
	if bestServerCompletionPartial(true, input, current) {
		t.Fatal("completed browser-visible Top-3 must not retain a time-limit partial warning")
	}
}

func TestBestServerCompletionPartialOnlyWhenBudgetBlocksTarget(t *testing.T) {
	current := "203.0.113.9:443"
	input := []bestServerQualityCandidate{
		{ID: "a", Endpoint: "203.0.113.10:443", Tested: true, Available: true, Eligible: true},
		{ID: "b", Endpoint: "203.0.113.11:443", Tested: true, Reachable: true, Eligible: false},
	}
	if !bestServerCompletionPartial(true, input, current) {
		t.Fatal("budget-limited scan below the Top-3 target must remain partial")
	}
	if bestServerCompletionPartial(false, input, current) {
		t.Fatal("natural candidate exhaustion below three is complete, not a time-limit partial")
	}
}

func TestMeasuredBestServerBatchIsSingleLogicalProfile(t *testing.T) {
	if bestServerMeasuredBatchSize != 1 {
		t.Fatalf("deep batch size=%d want=1 so profiles sharing a listener are checked independently", bestServerMeasuredBatchSize)
	}
}

func TestSortMeasuredBestServerResultsKeepsEligibleAheadOfDiagnostic(t *testing.T) {
	input := []bestServerQualityCandidate{
		{ID: "failed-fast", Tested: true, Reachable: true, Score: 9999, ApplicationMS: 90},
		{ID: "eligible", Tested: true, Available: true, Eligible: true, Score: 500, ApplicationMS: 170, DownloadMbps: 100},
		{ID: "failed-app", Tested: true, Available: true, Score: 800, ApplicationMS: 140},
	}
	sortMeasuredBestServerResults(input)
	if input[0].ID != "eligible" || input[1].ID != "failed-app" || input[2].ID != "failed-fast" {
		t.Fatalf("unexpected UX order: %#v", input)
	}
}

func TestCurrentBestServerCandidateSelectsExactlyOneCurrentProfile(t *testing.T) {
	input := []bestServerInternalCandidate{
		{Profile: subscriptionProfile{ID: "a", Name: "PL Warsaw, Extra", CountryCode: "pl", Address: "10.0.0.1", Port: 443}},
		{Profile: subscriptionProfile{ID: "b", Name: "DE Frankfurt, Extra", CountryCode: "de", Address: "10.0.0.2", Port: 443}},
	}
	got, ok := currentBestServerCandidate(input, "10.0.0.2:443", "DE Frankfurt")
	if !ok || len(got) != 1 || got[0].Profile.ID != "b" {
		t.Fatalf("current-only selection = %#v, ok=%v; want only profile b", got, ok)
	}
}

func TestCurrentBestServerCandidateFailsClosedOnAmbiguousIdentity(t *testing.T) {
	input := []bestServerInternalCandidate{
		{Profile: subscriptionProfile{ID: "a", Name: "PL Warsaw A, Extra", CountryCode: "pl", Address: "10.0.0.1", Port: 443}},
		{Profile: subscriptionProfile{ID: "b", Name: "PL Warsaw B, Extra", CountryCode: "pl", Address: "10.0.0.1", Port: 443}},
	}
	if got, ok := currentBestServerCandidate(input, "10.0.0.1:443", "PL Warsaw"); ok || len(got) != 0 {
		t.Fatalf("ambiguous current identity must fail closed, got %#v ok=%v", got, ok)
	}
}
