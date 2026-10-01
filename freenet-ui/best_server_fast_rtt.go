package main

import (
	"context"
	"net"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	bestServerFastRTTWorkers        = 16
	bestServerFastRTTAttempts       = 2
	bestServerFastRTTAttemptTimeout = 900 * time.Millisecond
	bestServerFastRTTPhaseTimeout   = 5 * time.Second
	bestServerFastRTTShortlist      = 20
	bestServerFastRTTPreferredMS    = 200
	bestServerFastRTTReserveMS      = 300

	bestServerFastRTTBucketPreferred = 0
	bestServerFastRTTBucketReserve   = 1
	bestServerFastRTTBucketUnknown   = 2
	bestServerFastRTTBucketSlow      = 3
	bestServerFastRTTBucketFailed    = 4
)

type bestServerFastRTTEndpointResult struct {
	Endpoint string
	Probe    bestServerProbeResult
}

type bestServerFastRTTCandidateRank struct {
	Index    int
	Bucket   int
	Median   int
	Jitter   int
	Country  string
	Endpoint string
	ID       string
}

func defaultBestServerFastRTTProbe(ctx context.Context, profile subscriptionProfile) bestServerProbeResult {
	samples := make([]int, 0, bestServerFastRTTAttempts)
	for attempt := 0; attempt < bestServerFastRTTAttempts; attempt++ {
		if ctx.Err() != nil {
			break
		}
		attemptCtx, cancel := context.WithTimeout(ctx, bestServerFastRTTAttemptTimeout)
		dialer := net.Dialer{Timeout: bestServerFastRTTAttemptTimeout}
		started := time.Now()
		conn, err := dialer.DialContext(attemptCtx, "tcp", profileEndpoint(profile))
		elapsed := int(time.Since(started).Milliseconds())
		cancel()
		if err != nil {
			continue
		}
		_ = conn.Close()
		if elapsed < 1 {
			elapsed = 1
		}
		samples = append(samples, elapsed)
	}
	return summarizeBestServerSamples(samples, 1)
}

// bestServerFastRTTShortlist is a ranking-only Stage 0. It never starts Xray
// and never mutates live VPN state. Direct provider-endpoint TCP RTT is useful
// for prioritization but is not proof of the VLESS/Reality application path,
// therefore >200 ms is deprioritized rather than hard-failed.
func bestServerFastRTTShortlist(
	ctx context.Context,
	candidates []bestServerInternalCandidate,
	currentEndpoint, currentFilter string,
	probe bestServerTCPProbe,
) []bestServerInternalCandidate {
	if len(candidates) <= 1 {
		return candidates
	}
	if probe == nil {
		probe = defaultBestServerFastRTTProbe
	}

	type endpointGroup struct {
		Endpoint string
		Profile  subscriptionProfile
	}
	groups := make([]endpointGroup, 0, len(candidates))
	seenEndpoint := make(map[string]bool, len(candidates))
	for _, candidate := range candidates {
		endpoint := profileEndpoint(candidate.Profile)
		if endpoint == "" || seenEndpoint[endpoint] {
			continue
		}
		seenEndpoint[endpoint] = true
		groups = append(groups, endpointGroup{Endpoint: endpoint, Profile: candidate.Profile})
	}
	if len(groups) == 0 {
		return candidates
	}

	phaseCtx, cancel := context.WithTimeout(ctx, bestServerFastRTTPhaseTimeout)
	defer cancel()
	reportBestServerProgress(ctx, "fast_rtt", 0, len(groups))

	jobs := make(chan endpointGroup)
	results := make(chan bestServerFastRTTEndpointResult, len(groups))
	workers := bestServerFastRTTWorkers
	if workers > len(groups) {
		workers = len(groups)
	}
	var wg sync.WaitGroup
	var completed atomic.Int32
	for worker := 0; worker < workers; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for group := range jobs {
				if phaseCtx.Err() != nil {
					continue
				}
				result := probe(phaseCtx, group.Profile)
				results <- bestServerFastRTTEndpointResult{Endpoint: group.Endpoint, Probe: result}
				done := int(completed.Add(1))
				reportBestServerProgress(ctx, "fast_rtt", done, len(groups))
			}
		}()
	}
	go func() {
		defer close(jobs)
		for _, group := range groups {
			select {
			case jobs <- group:
			case <-phaseCtx.Done():
				return
			}
		}
	}()
	wg.Wait()
	close(results)

	measured := make(map[string]bestServerProbeResult, len(groups))
	attempted := make(map[string]bool, len(groups))
	for result := range results {
		attempted[result.Endpoint] = true
		measured[result.Endpoint] = result.Probe
	}

	currentIndex := bestServerCurrentCandidateIndex(candidates, currentEndpoint, currentFilter)
	selectedIndexes := selectBestServerFastRTTIndexes(candidates, measured, attempted, currentIndex)
	out := make([]bestServerInternalCandidate, 0, len(selectedIndexes))
	for _, index := range selectedIndexes {
		out = append(out, candidates[index])
	}
	return out
}

func selectBestServerFastRTTIndexes(
	candidates []bestServerInternalCandidate,
	measured map[string]bestServerProbeResult,
	attempted map[string]bool,
	currentIndex int,
) []int {
	if len(candidates) == 0 {
		return nil
	}
	ranks := make([]bestServerFastRTTCandidateRank, 0, len(candidates))
	for index, candidate := range candidates {
		endpoint := profileEndpoint(candidate.Profile)
		probe, hasResult := measured[endpoint]
		bucket := bestServerFastRTTBucketUnknown
		median, jitter := 0, 0
		switch {
		case hasResult && probe.OK && probe.Median <= bestServerFastRTTPreferredMS:
			bucket, median, jitter = bestServerFastRTTBucketPreferred, probe.Median, probe.Jitter
		case hasResult && probe.OK && probe.Median <= bestServerFastRTTReserveMS:
			bucket, median, jitter = bestServerFastRTTBucketReserve, probe.Median, probe.Jitter
		case hasResult && probe.OK:
			bucket, median, jitter = bestServerFastRTTBucketSlow, probe.Median, probe.Jitter
		case attempted[endpoint]:
			bucket = bestServerFastRTTBucketFailed
		default:
			bucket = bestServerFastRTTBucketUnknown
		}
		ranks = append(ranks, bestServerFastRTTCandidateRank{
			Index: index, Bucket: bucket, Median: median, Jitter: jitter,
			Country: strings.ToLower(strings.TrimSpace(candidate.Profile.CountryCode)),
			Endpoint: endpoint, ID: candidate.Profile.ID,
		})
	}

	sort.SliceStable(ranks, func(i, j int) bool {
		a, b := ranks[i], ranks[j]
		if a.Bucket != b.Bucket {
			return a.Bucket < b.Bucket
		}
		if a.Median != b.Median {
			if a.Median == 0 {
				return false
			}
			if b.Median == 0 {
				return true
			}
			return a.Median < b.Median
		}
		if a.Jitter != b.Jitter {
			return a.Jitter < b.Jitter
		}
		return a.ID < b.ID
	})

	limit := bestServerFastRTTShortlist
	if limit > len(candidates) {
		limit = len(candidates)
	}
	selected := make([]int, 0, limit)
	seenIndex := make(map[int]bool, limit)
	seenSelectedEndpoint := make(map[string]bool, limit)
	add := func(rank bestServerFastRTTCandidateRank, requireUniqueEndpoint bool) {
		if len(selected) >= limit || seenIndex[rank.Index] {
			return
		}
		if requireUniqueEndpoint && rank.Endpoint != "" && seenSelectedEndpoint[rank.Endpoint] {
			return
		}
		selected = append(selected, rank.Index)
		seenIndex[rank.Index] = true
		if rank.Endpoint != "" {
			seenSelectedEndpoint[rank.Endpoint] = true
		}
	}

	// Keep one <=300 ms candidate per country before filling globally. This
	// preserves geography diversity without forcing slow/failed endpoints into
	// the shortlist when enough good alternatives exist.
	seenCountry := map[string]bool{}
	for _, rank := range ranks {
		if rank.Bucket > bestServerFastRTTBucketReserve || rank.Country == "" || seenCountry[rank.Country] {
			continue
		}
		before := len(selected)
		add(rank, true)
		if len(selected) > before {
			seenCountry[rank.Country] = true
		}
	}

	// Unique endpoints first. UNKNOWN ranks ahead of confirmed >300 ms or failed
	// evidence, because a bounded phase timeout is not proof of a bad server.
	for _, rank := range ranks {
		add(rank, true)
	}
	// Shared endpoints may still represent different logical profiles. Allow
	// those only after every unique endpoint has had a chance.
	for _, rank := range ranks {
		add(rank, false)
	}

	if currentIndex >= 0 && currentIndex < len(candidates) && !seenIndex[currentIndex] {
		if len(selected) >= limit && limit > 0 {
			selected[len(selected)-1] = currentIndex
		} else {
			selected = append(selected, currentIndex)
		}
	}
	return selected
}
