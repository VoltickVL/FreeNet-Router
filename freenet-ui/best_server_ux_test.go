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
