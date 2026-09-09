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
	bestServerPreflightCandidateTimeout = 5 * time.Second
	bestServerPreflightPhaseTimeout     = 35 * time.Second
	bestServerPreflightShortlist        = 9
	bestServerPreflightHTTPRuns         = 2
)

type bestServerPreflightResult struct {
	Index int
	Probe bestServerProbeResult
}

// applicationAwareBestServerShortlist measures the real VPN application path
// before the expensive throughput stage. Provider endpoint TCP latency alone is
// not a reliable proxy for the geographic/exit path of an Extra profile.
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
	for result := range results {
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

	selectedIndexes := make([]int, 0, bestServerPreflightShortlist)
	seen := make(map[int]bool)
	for _, result := range measured {
		if len(selectedIndexes) >= bestServerPreflightShortlist {
			break
		}
		selectedIndexes = append(selectedIndexes, result.Index)
		seen[result.Index] = true
	}

	// The current VPN must always get a full quality measurement when it is not
	// represented by a complete fresh cache entry. Without that baseline FreeNet
	// must not recommend a switch.
	currentIndex := bestServerCurrentCandidateIndex(candidates, currentEndpoint, currentFilter)
	if currentIndex >= 0 && !seen[currentIndex] {
		if len(selectedIndexes) >= bestServerPreflightShortlist {
			selectedIndexes[len(selectedIndexes)-1] = currentIndex
		} else {
			selectedIndexes = append(selectedIndexes, currentIndex)
		}
		seen[currentIndex] = true
	}
	if len(selectedIndexes) == 0 {
		return nil
	}

	selected := make([]bestServerInternalCandidate, 0, len(selectedIndexes))
	for _, index := range selectedIndexes {
		selected = append(selected, candidates[index])
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
