package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	bestServerPreflightWorkers          = 4
	bestServerPreflightCandidateTimeout = 8 * time.Second
	bestServerPreflightPhaseTimeout     = 20 * time.Second
	bestServerPreflightShortlist        = 12
	bestServerPreflightHTTPRuns         = 2
)

type bestServerPreflightResult struct {
	Index int
	Probe bestServerProbeResult
}

// applicationAwareBestServerShortlist measures the real VPN application path
// before the expensive throughput stage. This is ranking-only evidence: two
// bounded HTTP samples reduce sensitivity to a single transient result while
// strict acceptance is still performed later by the deep quality probe.
// Provider endpoint TCP latency
// alone is not a reliable proxy for the geographic/exit path of an Extra profile.
func (a *app) applicationAwareBestServerShortlist(ctx context.Context, candidates []bestServerInternalCandidate, currentEndpoint, currentFilter string) []bestServerInternalCandidate {
	if len(candidates) <= bestServerPreflightShortlist {
		return candidates
	}

	phaseCtx, cancelPhase := context.WithTimeout(ctx, bestServerPreflightPhaseTimeout)
	defer cancelPhase()
	reportBestServerProgress(ctx, "preflight", 0, len(candidates))
	jobs := make(chan int)
	results := make(chan bestServerPreflightResult, len(candidates))
	workers := bestServerPreflightWorkers
	if workers > len(candidates) {
		workers = len(candidates)
	}
	var wg sync.WaitGroup
	var completed atomic.Int32
	for worker := 0; worker < workers; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for index := range jobs {
				if phaseCtx.Err() != nil {
					continue
				}
				probeCtx, cancel := context.WithTimeout(phaseCtx, bestServerPreflightCandidateTimeout)
				probe := a.probeBestServerApplicationPreflight(probeCtx, candidates[index])
				cancel()
				results <- bestServerPreflightResult{Index: index, Probe: probe}
				done := int(completed.Add(1))
				reportBestServerProgress(ctx, "preflight", done, len(candidates))
			}
		}()
	}
	go func() {
		defer close(jobs)
		for index := range candidates {
			select {
			case jobs <- index:
			case <-phaseCtx.Done():
				return
			}
		}
	}()
	wg.Wait()
	close(results)

	measured := make([]bestServerPreflightResult, 0, len(candidates))
	attempted := make(map[int]bool, len(candidates))
	for result := range results {
		attempted[result.Index] = true
		if result.Probe.OK {
			measured = append(measured, result)
		}
	}
	sort.Slice(measured, func(i, j int) bool {
		aResult, bResult := measured[i], measured[j]
		if aResult.Probe.Median != bResult.Probe.Median {
			return aResult.Probe.Median < bResult.Probe.Median
		}
		if aResult.Probe.Jitter != bResult.Probe.Jitter {
			return aResult.Probe.Jitter < bResult.Probe.Jitter
		}
		return candidates[aResult.Index].Profile.ID < candidates[bResult.Index].Profile.ID
	})

	currentIndex := bestServerCurrentCandidateIndex(candidates, currentEndpoint, currentFilter)
	selectedIndexes := selectBestServerPreflightIndexes(candidates, measured, attempted, currentIndex)
	if len(selectedIndexes) == 0 {
		return nil
	}

	selected := make([]bestServerInternalCandidate, 0, len(selectedIndexes))
	for _, index := range selectedIndexes {
		selected = append(selected, candidates[index])
	}
	return selected
}

// Preflight is ranking-only. A candidate that did not finish before the bounded
// phase deadline is UNKNOWN, not failed. Successful measurements stay first,
// then unattempted candidates fill the deep-check reserve. Explicit preflight
// failures are only used as a last resort. This prevents a busy/slow router
// from collapsing a large subscription pool to one (or zero) deep candidates.
func selectBestServerPreflightIndexes(
	candidates []bestServerInternalCandidate,
	measured []bestServerPreflightResult,
	attempted map[int]bool,
	currentIndex int,
) []int {
	if len(candidates) == 0 {
		return nil
	}
	limit := bestServerPreflightShortlist
	if limit > len(candidates) {
		limit = len(candidates)
	}
	selected := make([]int, 0, limit)
	seen := make(map[int]bool, limit)
	add := func(index int) {
		if index < 0 || index >= len(candidates) || len(selected) >= limit || seen[index] {
			return
		}
		selected = append(selected, index)
		seen[index] = true
	}

	for _, result := range measured {
		if result.Probe.OK {
			add(result.Index)
		}
	}

	// Phase timeout means "unknown". Give those profiles a strict deep-check
	// chance before retrying endpoints that already produced a negative preflight.
	for index := range candidates {
		if attempted[index] {
			continue
		}
		add(index)
	}
	for index := range candidates {
		if !attempted[index] {
			continue
		}
		add(index)
	}

	// Generic caller contract: current VPN must remain representable when this
	// helper is used outside the foreign-only path.
	if currentIndex >= 0 && currentIndex < len(candidates) && !seen[currentIndex] {
		if len(selected) >= limit && limit > 0 {
			replaced := selected[len(selected)-1]
			delete(seen, replaced)
			selected[len(selected)-1] = currentIndex
			seen[currentIndex] = true
		} else {
			add(currentIndex)
		}
	}
	return selected
}

func (a *app) probeBestServerApplicationPreflight(ctx context.Context, candidate bestServerInternalCandidate) bestServerProbeResult {
	outbound, err := buildBestServerProbeOutbound(candidate.Raw, candidate.Profile)
	if err != nil {
		return bestServerProbeResult{}
	}
	xrayPath := strings.TrimSpace(os.Getenv("FREENET_XRAY_BIN"))
	if xrayPath == "" {
		xrayPath = defaultBestServerXrayPath
	}
	if _, err := os.Stat(xrayPath); err != nil {
		return bestServerProbeResult{}
	}
	curlPath, err := exec.LookPath("curl")
	if err != nil {
		return bestServerProbeResult{}
	}
	port, err := reserveBestServerPort()
	if err != nil {
		return bestServerProbeResult{}
	}
	tmpDir, err := os.MkdirTemp("", "freenet-best-preflight-")
	if err != nil {
		return bestServerProbeResult{}
	}
	defer os.RemoveAll(tmpDir)
	_ = os.Chmod(tmpDir, 0700)

	config := map[string]any{
		"log": map[string]any{"loglevel": "warning"},
		"inbounds": []any{map[string]any{
			"listen": "127.0.0.1", "port": port, "protocol": "socks",
			"settings": map[string]any{"udp": false}, "tag": "freenet-best-preflight",
		}},
		"outbounds": []any{outbound},
		"routing": map[string]any{
			"domainStrategy": "AsIs",
			"rules": []any{map[string]any{
				"type": "field", "inboundTag": []string{"freenet-best-preflight"}, "outboundTag": "vless-reality",
			}},
		},
	}
	encoded, err := json.Marshal(config)
	if err != nil {
		return bestServerProbeResult{}
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "00_probe.json"), encoded, 0600); err != nil {
		return bestServerProbeResult{}
	}

	runCtx, cancelRun := context.WithCancel(ctx)
	defer cancelRun()
	cmd := exec.CommandContext(runCtx, xrayPath, "run", "-confdir", tmpDir)
	cmd.Env = append(os.Environ(), "XRAY_LOCATION_ASSET="+a.geoDataAssetDir())
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return bestServerProbeResult{}
	}
	defer func() {
		cancelRun()
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
	}()
	if !waitBestServerSOCKS(ctx, port) {
		return bestServerProbeResult{}
	}

	socks := fmt.Sprintf("127.0.0.1:%d", port)
	samples := make([]int, 0, bestServerPreflightHTTPRuns)
	for run := 0; run < bestServerPreflightHTTPRuns; run++ {
		if ctx.Err() != nil {
			break
		}
		probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		output, err := exec.CommandContext(probeCtx, curlPath,
			"--socks5-hostname", socks,
			"-sS", "--connect-timeout", "2", "--max-time", "2",
			"-o", "/dev/null", "-w", "%{http_code}\t%{time_pretransfer}\t%{time_starttransfer}", bestServerQualityProbeURL,
		).Output()
		cancel()
		if err != nil {
			continue
		}
		ms, ok := parseBestServerHTTPResponseMS(string(output))
		if ok {
			samples = append(samples, ms)
		}
	}
	return summarizeBestServerSamples(samples, 1)
}
