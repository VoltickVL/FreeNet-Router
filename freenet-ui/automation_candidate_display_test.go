package main

import "testing"

func TestBestAutomationCandidateUsesHumanReadableProfileName(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  string
		want string
	}{
		{name: "iso prefix", raw: "NL Амстердам, Нидерланды, Extra", want: "Амстердам, Нидерланды, Extra"},
		{name: "regional flag", raw: "🇦🇹 Вена, Австрия, Extra", want: "Вена, Австрия, Extra"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := bestServerQualityResponse{Candidates: []bestServerQualityCandidate{{
				ID: "0123456789abcdef", Name: tc.raw, CountryCode: "nl", Eligible: true, Available: true,
			}}}
			candidate, ok := bestAutomationCandidate(response)
			if !ok {
				t.Fatal("expected eligible AUTO VPN candidate")
			}
			if candidate.Name != tc.want {
				t.Fatalf("candidate name=%q want=%q", candidate.Name, tc.want)
			}
		})
	}
}
