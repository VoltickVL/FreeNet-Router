package main

import (
	"context"
	"net"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	bestServerExpressRuns             = 2
	bestServerExpressWorkers          = 24
	bestServerExpressDialTimeout      = 900 * time.Millisecond
	bestServerCascadeShortlist        = 8
	bestServerQuickWorkers            = 4
	bestServerQuickCandidateTimeout   = 8 * time.Second
	bestServerFreshCatalogTimeout     = 5 * time.Second
	bestServerCascadeVisible          = 3
)

type bestServerExpressEvidence struct {
	Probe bestServerProbeResult
}

type bestServerCascadeOutcome struct {
	Response   bestServerQualityResponse
	InternalByID map[string]bestServerInternalCandidate
}

var bestServerExpressProbe = defaultBestServerExpressProbe
var bestServerQuickProbe = func(a *app, ctx context.Context, candidate bestServerInternalCandidate) bestServerProbeResult {
	return a.probeBestServerApplicationPreflight(ctx, candidate)
}
var bestServerFreshCatalog = func(a *app, ctx context.Context) ([]bestServerInternalCandidate, int, bool, error) {
	return a.discoverBestServerCandidates(ctx)
}

// defaultBestServerExpressProbe is intentionally DIRECT-only. It never starts
// Xray and never downloads Speedtest/media payloads. Two bounded TCP connects
// are launched in parallel so a normal 49-profile catalog can be measured in a
// few seconds even when many endpoints time out.
func defaultBestServerExpressProbe(parent context.Context, profile subscriptionProfile) bestServerProbeResult {
	endpoint := strings.TrimSpace(profileEndpoint(profile))
	if endpoint == "" {
		return bestServerProbeResult{}
	}
	ctx, cancel := context.WithTimeout(parent, bestServerExpressDialTimeout)
	defer cancel()

	type sample struct {
		ms int
		ok bool
	}
	results := make(chan sample, bestServerExpressRuns)
	for i := 0; i < bestServerExpressRuns; i++ {
		go func() {
			dialer := net.Dialer{Timeout: bestServerExpressDialTimeout}
			started := time.Now()
			conn, err := dialer.DialContext(ctx, "tcp", endpoint)
			elapsed := int(time.Since(started).Milliseconds())
			if err != nil {
				results <- sample{}
				return
			}
			_ = conn.Close()
			if elapsed < 1 {
				elapsed = 1
			}
			results <- sample{ms: elapsed, ok: true}
		}()
	}
	samples := make([]int, 0, bestServerExpressRuns)
	for i := 0; i < bestServerExpressRuns; i++ {
		result := <-results
		if result.ok {
			samples = append(samples, result.ms)
		}
	}
	return summarizeBestServerSamples(samples, 1)
}

// expressBestServerPool measures every unique endpoint and maps the terminal
// evidence back to every profile that uses it. Jobs are fully queued before
// workers start consuming them; subscription order therefore cannot make tail
// profiles silently miss the express stage.
func expressBestServerPool(ctx context.Context, candidates []bestServerInternalCandidate) ([]bestServerExpressEvidence, int) {
	evidence := make([]bestServerExpressEvidence, len(candidates))
	if len(candidates) == 0 {
		return evidence, 0
	}
	endpointIndexes := make(map[string][]int)
	endpoints := make([]string, 0, len(candidates))
	for i, candidate := range candidates {
		endpoint := strings.TrimSpace(profileEndpoint(candidate.Profile))
		if _, exists := endpointIndexes[endpoint]; !exists {
			endpoints = append(endpoints, endpoint)
		}
		endpointIndexes[endpoint] = append(endpointIndexes[endpoint], i)
	}
	sort.Strings(endpoints)

	jobs := make(chan string, len(endpoints))
	for _, endpoint := range endpoints {
		jobs <- endpoint
	}
	close(jobs)

	workers := bestServerExpressWorkers
	if workers > len(endpoints) {
		workers = len(endpoints)
	}
	if workers < 1 {
		workers = 1
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	completedProfiles := 0
	for worker := 0; worker < workers; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for endpoint := range jobs {
				indexes := endpointIndexes[endpoint]
				probe := bestServerProbeResult{}
				if len(indexes) > 0 && ctx.Err() == nil {
					probe = bestServerExpressProbe(ctx, candidates[indexes[0]].Profile)
				}
				mu.Lock()
				for _, index := range indexes {
					evidence[index] = bestServerExpressEvidence{Probe: probe}
					completedProfiles++
				}
				done := completedProfiles
				mu.Unlock()
				reportBestServerProgress(ctx, "express", done, len(candidates))
			}
		}()
	}
	wg.Wait()
	return evidence, completedProfiles
}

func bestServerExpressShortlist(candidates []bestServerInternalCandidate, evidence []bestServerExpressEvidence) []bestServerInternalCandidate {
	indexes := make([]int, 0, len(candidates))
	for i := range candidates {
		if i < len(evidence) && evidence[i].Probe.OK {
			indexes = append(indexes, i)
		}
	}
	sort.SliceStable(indexes, func(i, j int) bool {
		aIndex, bIndex := indexes[i], indexes[j]
		aProbe, bProbe := evidence[aIndex].Probe, evidence[bIndex].Probe
		if aProbe.Median != bProbe.Median {
			return aProbe.Median < bProbe.Median
		}
		if aProbe.Jitter != bProbe.Jitter {
			return aProbe.Jitter < bProbe.Jitter
		}
		return candidates[aIndex].Profile.ID < candidates[bIndex].Profile.ID
	})

	selected := make([]int, 0, bestServerCascadeShortlist)
	selectedSet := make(map[int]struct{})
	seenLocation := make(map[string]struct{})
	locationKey := func(candidate bestServerInternalCandidate) string {
		if code := strings.ToLower(strings.TrimSpace(candidate.Profile.CountryCode)); code != "" {
			return code
		}
		if name := strings.ToLower(strings.TrimSpace(sanitizeProfileName(candidate.Profile.Name))); name != "" {
			return name
		}
		return candidate.Profile.ID
	}
	// First pass gives strong countries/locations one slot each, so several
	// endpoints from one country cannot crowd a historically useful location
	// out of the bounded quick shortlist.
	for _, index := range indexes {
		key := locationKey(candidates[index])
		if _, exists := seenLocation[key]; exists {
			continue
		}
		seenLocation[key] = struct{}{}
		selected = append(selected, index)
		selectedSet[index] = struct{}{}
		if len(selected) >= bestServerCascadeShortlist {
			break
		}
	}
	for _, index := range indexes {
		if len(selected) >= bestServerCascadeShortlist {
			break
		}
		if _, exists := selectedSet[index]; exists {
			continue
		}
		selected = append(selected, index)
		selectedSet[index] = struct{}{}
	}

	out := make([]bestServerInternalCandidate, 0, len(selected))
	for _, index := range selected {
		out = append(out, candidates[index])
	}
	return out
}

func expressProbeForCandidate(candidate bestServerInternalCandidate, all []bestServerInternalCandidate, evidence []bestServerExpressEvidence) bestServerProbeResult {
	endpoint := strings.TrimSpace(profileEndpoint(candidate.Profile))
	for i := range all {
		if i < len(evidence) && strings.TrimSpace(profileEndpoint(all[i].Profile)) == endpoint {
			return evidence[i].Probe
		}
	}
	return bestServerProbeResult{}
}

func (a *app) quickBestServerProbe(ctx context.Context, candidates []bestServerInternalCandidate, expressAll []bestServerInternalCandidate, expressEvidence []bestServerExpressEvidence) []bestServerQualityCandidate {
	if len(candidates) == 0 {
		return nil
	}
	type result struct {
		index int
		value bestServerQualityCandidate
	}
	jobs := make(chan int, len(candidates))
	for i := range candidates {
		jobs <- i
	}
	close(jobs)
	results := make(chan result, len(candidates))
	workers := bestServerQuickWorkers
	if workers > len(candidates) {
		workers = len(candidates)
	}
	var wg sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for index := range jobs {
				candidate := candidates[index]
				express := expressProbeForCandidate(candidate, expressAll, expressEvidence)
				value := bestServerQualityCandidate{
					Tested: true,
					Validation: "quick",
					ID: candidate.Profile.ID,
					Name: candidate.Profile.Name,
					CountryCode: candidate.Profile.CountryCode,
					Endpoint: profileEndpoint(candidate.Profile),
					Reachable: express.OK,
					TCPRTTMS: express.Median,
					TCPJitterMS: express.Jitter,
					Reason: "DIRECT express completed; isolated VPN quick probe pending",
				}
				probeCtx, cancel := context.WithTimeout(ctx, bestServerQuickCandidateTimeout)
				probe := bestServerQuickProbe(a, probeCtx, candidate)
				cancel()
				if probe.OK {
					value.Available = true
					value.ApplicationMS = probe.Median
					value.JitterMS = probe.Jitter
					value.HTTPSamples = len(probe.Samples)
					value.Score = bestServerScore(probe.Median, express.Median, probe.Jitter)
					value.Confidence = "quick"
					value.Reason = "isolated VPN quick probe passed; strict acceptance required before apply"
					if probe.Median > bestServerQualityMaxApplicationMS {
						value.Rejections = []string{"Отклик сайтов выше 180 мс"}
					}
				} else {
					value.Reason = "isolated VPN quick probe failed"
					value.Rejections = []string{"Быстрая VPN-проверка не пройдена"}
				}
				// Quick evidence is ranking-only. Never set Eligible here.
				value.Eligible = false
				results <- result{index: index, value: value}
			}
		}()
	}
	wg.Wait()
	close(results)

	out := make([]bestServerQualityCandidate, len(candidates))
	completed := 0
	for item := range results {
		out[item.index] = item.value
		completed++
		reportBestServerProgress(ctx, "quick", completed, len(candidates))
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		aHealthy := a.Available && a.ApplicationMS > 0 && a.ApplicationMS <= bestServerQualityMaxApplicationMS
		bHealthy := b.Available && b.ApplicationMS > 0 && b.ApplicationMS <= bestServerQualityMaxApplicationMS
		if aHealthy != bHealthy {
			return aHealthy
		}
		if a.Available != b.Available {
			return a.Available
		}
		if a.ApplicationMS != b.ApplicationMS {
			if a.ApplicationMS == 0 {
				return false
			}
			if b.ApplicationMS == 0 {
				return true
			}
			return a.ApplicationMS < b.ApplicationMS
		}
		if a.JitterMS != b.JitterMS {
			return a.JitterMS < b.JitterMS
		}
		if a.TCPRTTMS != b.TCPRTTMS {
			return a.TCPRTTMS < b.TCPRTTMS
		}
		return a.ID < b.ID
	})
	return out
}

func bestServerLogicalKey(candidate bestServerInternalCandidate) string {
	return strings.ToLower(strings.TrimSpace(sanitizeProfileName(candidate.Profile.Name))) + "|" +
		strings.ToLower(strings.TrimSpace(candidate.Profile.CountryCode))
}

func qualityLogicalKey(candidate bestServerQualityCandidate) string {
	return strings.ToLower(strings.TrimSpace(sanitizeProfileName(candidate.Name))) + "|" +
		strings.ToLower(strings.TrimSpace(candidate.CountryCode))
}

// retryBestServerFreshEndpoints performs at most one extra subscription fetch
// for the whole scan. Only degraded quick candidates are considered, and only
// an unambiguous exact logical match with a different endpoint is probed.
func (a *app) retryBestServerFreshEndpoints(ctx context.Context, initial []bestServerInternalCandidate, quick []bestServerQualityCandidate) ([]bestServerInternalCandidate, []bestServerQualityCandidate) {
	needsRetry := make(map[string]bestServerQualityCandidate)
	for _, candidate := range quick {
		if candidate.Available && candidate.ApplicationMS > 0 && candidate.ApplicationMS <= bestServerQualityMaxApplicationMS {
			continue
		}
		needsRetry[qualityLogicalKey(candidate)] = candidate
	}
	if len(needsRetry) == 0 || ctx.Err() != nil {
		return nil, quick
	}

	fetchCtx, cancel := context.WithTimeout(ctx, bestServerFreshCatalogTimeout)
	fresh, _, _, err := bestServerFreshCatalog(a, fetchCtx)
	cancel()
	if err != nil || len(fresh) == 0 {
		return nil, quick
	}
	initialEndpoint := make(map[string]string)
	for _, candidate := range initial {
		key := bestServerLogicalKey(candidate)
		if _, exists := initialEndpoint[key]; !exists {
			initialEndpoint[key] = strings.TrimSpace(profileEndpoint(candidate.Profile))
		}
	}
	freshMatches := make(map[string][]bestServerInternalCandidate)
	for _, candidate := range filterForeignBestServerCandidates(fresh) {
		key := bestServerLogicalKey(candidate)
		if _, wanted := needsRetry[key]; !wanted {
			continue
		}
		if endpointsEqual(profileEndpoint(candidate.Profile), initialEndpoint[key]) {
			continue
		}
		freshMatches[key] = append(freshMatches[key], candidate)
	}
	retries := make([]bestServerInternalCandidate, 0, len(freshMatches))
	for _, matches := range freshMatches {
		if len(matches) == 1 {
			retries = append(retries, matches[0])
		}
	}
	if len(retries) == 0 {
		return nil, quick
	}
	sort.Slice(retries, func(i, j int) bool { return retries[i].Profile.ID < retries[j].Profile.ID })
	retryEvidence, _ := expressBestServerPool(ctx, retries)
	retryQuick := a.quickBestServerProbe(ctx, retries, retries, retryEvidence)
	if len(retryQuick) == 0 {
		return retries, quick
	}

	replacement := make(map[string]bestServerQualityCandidate)
	for _, candidate := range retryQuick {
		key := qualityLogicalKey(candidate)
		old, exists := needsRetry[key]
		if !exists {
			continue
		}
		better := candidate.Available && (!old.Available ||
			(candidate.ApplicationMS > 0 && (old.ApplicationMS == 0 || candidate.ApplicationMS < old.ApplicationMS)))
		if better {
			candidate.FreshEndpointRetry = true
			replacement[key] = candidate
		}
	}
	for i := range quick {
		if candidate, ok := replacement[qualityLogicalKey(quick[i])]; ok {
			quick[i] = candidate
		}
	}
	sort.SliceStable(quick, func(i, j int) bool {
		a, b := quick[i], quick[j]
		aHealthy := a.Available && a.ApplicationMS > 0 && a.ApplicationMS <= bestServerQualityMaxApplicationMS
		bHealthy := b.Available && b.ApplicationMS > 0 && b.ApplicationMS <= bestServerQualityMaxApplicationMS
		if aHealthy != bHealthy {
			return aHealthy
		}
		if a.Available != b.Available {
			return a.Available
		}
		if a.ApplicationMS != b.ApplicationMS {
			if a.ApplicationMS == 0 {
				return false
			}
			if b.ApplicationMS == 0 {
				return true
			}
			return a.ApplicationMS < b.ApplicationMS
		}
		return a.ID < b.ID
	})
	return retries, quick
}

func (a *app) scanBestServerCascade(ctx context.Context, candidates []bestServerInternalCandidate, poolSize int, truncated bool, currentEndpoint, currentFilter string) bestServerCascadeOutcome {
	outcome := bestServerCascadeOutcome{InternalByID: make(map[string]bestServerInternalCandidate)}
	for _, candidate := range candidates {
		outcome.InternalByID[candidate.Profile.ID] = candidate
	}

	reportBestServerProgress(ctx, "express", 0, len(candidates))
	express, expressMeasured := expressBestServerPool(ctx, candidates)
	shortlist := bestServerExpressShortlist(candidates, express)
	reportBestServerProgress(ctx, "quick", 0, len(shortlist))
	quick := a.quickBestServerProbe(ctx, shortlist, candidates, express)
	retryInternal, quick := a.retryBestServerFreshEndpoints(ctx, candidates, quick)
	for _, candidate := range retryInternal {
		outcome.InternalByID[candidate.Profile.ID] = candidate
	}

	quickMeasured := len(quick)
	visible := quick
	if len(visible) > bestServerCascadeVisible {
		visible = visible[:bestServerCascadeVisible]
	}
	response := bestServerQualityResponse{
		Success: true,
		Available: false,
		Candidates: append([]bestServerQualityCandidate(nil), visible...),
		ProfilesScanned: expressMeasured,
		ProfilesTotal: poolSize,
		ProfilesTruncated: truncated,
		ExpressMeasured: expressMeasured,
		QuickMeasured: quickMeasured,
		StrictTested: 0,
		Mutation: "NONE",
	}
	for _, candidate := range response.Candidates {
		if candidate.Available && candidate.ApplicationMS > 0 && candidate.ApplicationMS <= bestServerQualityMaxApplicationMS {
			best := candidate
			response.Recommendation = &best
			response.Available = true
			break
		}
	}
	if expressMeasured < len(candidates) || quickMeasured < len(shortlist) {
		response.Partial = true
	}
	outcome.Response = response
	return outcome
}

func strictBestServerCandidate(ctx context.Context, a *app, candidate bestServerInternalCandidate, currentEndpoint, currentFilter string) (bestServerQualityCandidate, bool) {
	response := rankBestServerQualityCandidates(
		ctx,
		[]bestServerInternalCandidate{candidate},
		1,
		false,
		currentEndpoint,
		currentFilter,
		defaultBestServerQualityTCPProbe,
		a.probeBestServerQualityApplication,
	)
	if len(response.Candidates) == 0 || ctx.Err() != nil {
		return bestServerQualityCandidate{}, false
	}
	value := response.Candidates[0]
	value.Validation = "strict"
	return value, true
}

// strictBestServerFinalists is used by AUTO VPN only. Discovery stays fast,
// while mutation eligibility is still based exclusively on full strict
// evidence. It stops on the first Eligible finalist.
func strictBestServerFinalists(ctx context.Context, a *app, outcome bestServerCascadeOutcome, currentEndpoint, currentFilter string) bestServerQualityResponse {
	response := outcome.Response
	response.Candidates = nil
	response.Recommendation = nil
	response.Available = false
	response.StrictTested = 0
	for _, quick := range outcome.Response.Candidates {
		if ctx.Err() != nil {
			break
		}
		internal, ok := outcome.InternalByID[quick.ID]
		if !ok {
			continue
		}
		strict, ok := strictBestServerCandidate(ctx, a, internal, currentEndpoint, currentFilter)
		if !ok {
			continue
		}
		response.StrictTested++
		response.Candidates = append(response.Candidates, strict)
		if strict.Eligible && strict.Available {
			best := strict
			response.Recommendation = &best
			response.Available = true
			break
		}
	}
	return response
}
