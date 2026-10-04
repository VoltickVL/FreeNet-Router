package main

import (
	"context"
	"testing"
	"time"
)

func TestConfirmedVPNPingUsesMedianOfThree(t *testing.T) {
	values := []int{238, 174, 179}
	calls := 0
	runner := func(_ context.Context, _, _, target string, timeout time.Duration) (int, bool) {
		if target != bestServerTransportProbeURL || timeout != bestServerTransportProbeTimeout {
			t.Fatalf("unexpected confirmed ping target=%q timeout=%s", target, timeout)
		}
		if calls >= len(values) {
			t.Fatalf("confirmed ping called too many times: %d", calls+1)
		}
		value := values[calls]
		calls++
		return value, true
	}
	got := probeBestServerConfirmedVPNPingWith(context.Background(), "curl", "127.0.0.1:1080", runner)
	if !got.OK || calls != bestServerConfirmedVPNPingRuns {
		t.Fatalf("confirmed ping result=%+v calls=%d", got, calls)
	}
	if got.Median != 179 || got.Jitter != 64 {
		t.Fatalf("confirmed median/jitter=%d/%d want 179/64 from %#v", got.Median, got.Jitter, values)
	}
}

func TestConfirmedShortlistReordersSingleSampleNoise(t *testing.T) {
	candidates := []bestServerInternalCandidate{
		{Profile: subscriptionProfile{ID: "oslo", Name: "Oslo Extra"}, VPNRTTMS: 164},
		{Profile: subscriptionProfile{ID: "frankfurt", Name: "Frankfurt Extra"}, VPNRTTMS: 169},
		{Profile: subscriptionProfile{ID: "zurich", Name: "Zurich Extra"}, VPNRTTMS: 171},
	}
	probe := func(_ context.Context, candidate bestServerInternalCandidate) bestServerProbeResult {
		switch candidate.Profile.ID {
		case "oslo":
			return bestServerProbeResult{OK: true, Samples: []int{194, 199, 201}, Median: 199, Jitter: 7}
		case "frankfurt":
			return bestServerProbeResult{OK: true, Samples: []int{173, 175, 178}, Median: 175, Jitter: 5}
		default:
			return bestServerProbeResult{OK: true, Samples: []int{186, 188, 191}, Median: 188, Jitter: 5}
		}
	}
	got := confirmBestServerShortlistVPNPingWith(context.Background(), candidates, probe)
	if len(got) != 3 {
		t.Fatalf("confirmed shortlist len=%d", len(got))
	}
	if got[0].Profile.ID != "frankfurt" || got[0].VPNRTTMS != 175 || !got[0].VPNPingConfirmed {
		t.Fatalf("single-sample ordering survived confirmation: %#v", got)
	}
	if got[1].Profile.ID != "zurich" || got[2].Profile.ID != "oslo" {
		t.Fatalf("confirmed order=%s,%s,%s want Frankfurt,Zurich,Oslo",
			got[0].Profile.ID, got[1].Profile.ID, got[2].Profile.ID)
	}
}

func TestRuntimeExamplePrefersFrankfurtOverSmallZurichSpeedGain(t *testing.T) {
	candidates := []bestServerInternalCandidate{
		{Profile: subscriptionProfile{ID: "zurich", Name: "Zurich, Switzerland, Extra"}, VPNRTTMS: 188, VPNJitterMS: 8, VPNPingConfirmed: true},
		{Profile: subscriptionProfile{ID: "frankfurt", Name: "Frankfurt am Main, Germany, Extra"}, VPNRTTMS: 175, VPNJitterMS: 6, VPNPingConfirmed: true},
		{Profile: subscriptionProfile{ID: "amsterdam", Name: "Amsterdam, Netherlands, Extra"}, VPNRTTMS: 198, VPNJitterMS: 7, VPNPingConfirmed: true},
	}
	app := func(_ context.Context, candidate bestServerInternalCandidate) bestServerQualityApplicationResult {
		switch candidate.Profile.ID {
		case "zurich":
			return bestServerQualityApplicationResult{
				OK: true,
				VPN: bestServerProbeResult{OK: true, Samples: []int{186, 188, 191}, Median: 188, Jitter: 5},
				HTTP: bestServerProbeResult{OK: true, Samples: []int{187, 191, 195}, Median: 191, Jitter: 8},
				DownloadOK: true, DownloadMbps: 159, Media: stableTestMedia(159),
			}
		case "frankfurt":
			return bestServerQualityApplicationResult{
				OK: true,
				VPN: bestServerProbeResult{OK: true, Samples: []int{173, 175, 178}, Median: 175, Jitter: 5},
				HTTP: bestServerProbeResult{OK: true, Samples: []int{168, 177, 185}, Median: 177, Jitter: 17},
				DownloadOK: true, DownloadMbps: 151, Media: stableTestMedia(151),
			}
		default:
			return bestServerQualityApplicationResult{
				OK: true,
				VPN: bestServerProbeResult{OK: true, Samples: []int{196, 198, 201}, Median: 198, Jitter: 5},
				HTTP: bestServerProbeResult{OK: true, Samples: []int{181, 187, 194}, Median: 187, Jitter: 13},
				DownloadOK: true, DownloadMbps: 145, Media: stableTestMedia(145),
			}
		}
	}
	result := rankBestServerQualityCandidates(context.Background(), candidates, 3, false, "", "", app)
	if result.Recommendation == nil {
		t.Fatalf("no recommendation: %#v", result)
	}
	if result.Recommendation.ID != "frankfurt" {
		t.Fatalf("runtime latency example winner=%s want Frankfurt; candidates=%#v", result.Recommendation.ID, result.Candidates)
	}
	if result.Candidates[0].VPNRTTMS != 175 || !result.Candidates[0].VPNPingConfirmed {
		t.Fatalf("winner must carry confirmed VPN ping: %#v", result.Candidates[0])
	}
}
