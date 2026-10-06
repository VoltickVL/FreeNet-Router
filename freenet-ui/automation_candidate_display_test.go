package main

import (
	"strings"
	"testing"
)

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


func TestAutomationSelectionSummaryExplainsTopThreeWithoutEndpointSecrets(t *testing.T) {
	response := bestServerQualityResponse{Candidates: []bestServerQualityCandidate{
		{ID: "aaaaaaaaaaaaaaaa", Name: "DE Frankfurt, Germany, Extra", Available: true, Eligible: true, VPNRTTMS: 175, ApplicationMS: 177, DownloadMbps: 151, JitterMS: 17},
		{ID: "bbbbbbbbbbbbbbbb", Name: "CH Zurich, Switzerland, Extra", Available: true, Eligible: true, VPNRTTMS: 188, ApplicationMS: 191, DownloadMbps: 159, JitterMS: 8},
		{ID: "cccccccccccccccc", Name: "NL Amsterdam, Netherlands, Extra", Available: true, Eligible: true, VPNRTTMS: 198, ApplicationMS: 187, DownloadMbps: 145, JitterMS: 13},
	}}
	selected, ok := bestAutomationCandidate(response)
	if !ok {
		t.Fatal("expected selected candidate")
	}
	got := automationBestSelectionSummary(response, selected)
	for _, want := range []string{"Frankfurt", "VPN 175 мс", "сайты 177 мс", "скорость 151 Мбит/с", "Выбран:"} {
		if !strings.Contains(got, want) {
			t.Fatalf("selection summary missing %q: %s", want, got)
		}
	}
	if strings.Contains(got, ":443") || strings.Contains(got, "vless://") {
		t.Fatalf("selection summary leaked endpoint/credentials: %s", got)
	}
}


func TestAutomationEmergencySelectionSummaryDescribesSingleReplacement(t *testing.T) {
	selected := bestServerQualityCandidate{
		ID: "aaaaaaaaaaaaaaaa", Name: "CZ Prague, Czechia, Extra",
		Available: true, Eligible: true, VPNRTTMS: 174, ApplicationMS: 173, DownloadMbps: 97, JitterMS: 25,
	}
	got := automationBestEmergencySelectionSummary(selected)
	for _, want := range []string{"первый fully measured Eligible replacement", "Prague", "VPN 174 мс", "сайты 173 мс", "скорость 97 Мбит/с", "стабильность 25 мс"} {
		if !strings.Contains(got, want) {
			t.Fatalf("emergency selection summary missing %q: %s", want, got)
		}
	}
	if strings.Contains(got, "Top-3") {
		t.Fatalf("single emergency replacement mislabeled as Top-3: %s", got)
	}
	if strings.Contains(got, ":443") || strings.Contains(got, "vless://") {
		t.Fatalf("emergency selection summary leaked endpoint/credentials: %s", got)
	}
}


func TestAutomationCooldownReasonShowsCandidateMetricsWithoutSecrets(t *testing.T) {
	candidate := bestServerQualityCandidate{
		ID: "aaaaaaaaaaaaaaaa", Name: "CZ Prague, Czechia, Extra",
		Available: true, Eligible: true, VPNRTTMS: 169, ApplicationMS: 179, DownloadMbps: 283, JitterMS: 10,
	}
	got := automationCooldownReason(candidate)
	for _, want := range []string{"Prague", "VPN 169 мс", "сайты 179 мс", "скорость 283 Мбит/с", "стабильность 10 мс", "6-часовая защита"} {
		if !strings.Contains(got, want) {
			t.Fatalf("cooldown reason missing %q: %s", want, got)
		}
	}
	for _, forbidden := range []string{":443", "vless://", "publicKey=", "shortId="} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("cooldown reason leaked endpoint/credentials %q: %s", forbidden, got)
		}
	}
}
