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
	"time"
)

const (
	bestServerPreflightShortlist  = 10
	bestServerDiagnosticHTTPRuns  = 2
	bestServerProfilePingTimeout  = 5 * time.Second
	bestServerRTTSweepSlack       = 5 * time.Second
)

func bestServerRTTSweepTimeout(candidateCount int) time.Duration {
	if candidateCount <= 0 {
		return bestServerRTTSweepSlack
	}
	workers := providerProfileRTTWorkers
	if workers < 1 {
		workers = 1
	}
	if workers > candidateCount {
		workers = candidateCount
	}
	waves := (candidateCount + workers - 1) / workers
	return time.Duration(waves)*bestServerProfilePingTimeout + bestServerRTTSweepSlack
}

// applicationAwareBestServerShortlist uses the same per-logical-profile
// fixed-IP HTTPS RTT sweep as the VPN picker. Quick RTT has exactly one job:
// rank the actual logical VPN path. Named DNS/HTTPS, throughput, services and
// stability are strict deep-quality concerns. Every profile gets the same
// bounded chance; subscription position and shared ingress never rank a VPN.
func (a *app) applicationAwareBestServerShortlist(ctx context.Context, candidates []bestServerInternalCandidate, currentEndpoint, currentFilter string) []bestServerInternalCandidate {
	if len(candidates) == 0 {
		return candidates
	}

	phaseCtx, cancelPhase := context.WithTimeout(ctx, bestServerRTTSweepTimeout(len(candidates)))
	defer cancelPhase()
	reportBestServerProgress(ctx, "preflight", 0, len(candidates))
	items := measureProviderProfileRTT(phaseCtx, candidates, a.probeBestServerProfilePing)

	currentIndex := bestServerCurrentCandidateIndex(candidates, currentEndpoint, currentFilter)
	selectedIndexes := selectBestServerRTTShortlistIndexes(candidates, items, currentIndex)
	evidenceByID := make(map[string]providerProfileRTTItem, len(items))
	for _, item := range items {
		evidenceByID[strings.TrimSpace(item.ProfileID)] = item
	}
	selected := make([]bestServerInternalCandidate, 0, len(selectedIndexes))
	for _, index := range selectedIndexes {
		candidate := candidates[index]
		if item, ok := evidenceByID[strings.TrimSpace(candidate.Profile.ID)]; ok && item.Reachable {
			candidate.VPNRTTMS = item.RTTMS
			candidate.VPNJitterMS = item.JitterMS
		}
		selected = append(selected, candidate)
	}
	return selected
}

// selectBestServerRTTShortlistIndexes builds the bounded deep-check queue from
// canonical VPN application RTT evidence. Confirmed reachable profiles are
// ordered by RTT, UNKNOWN is reserve evidence, and explicit quick-path failures
// are last. The hard deep maximum is
// ten profiles; rankMeasuredBestServerBatches normally stops much earlier as
// soon as the requested Eligible target is reached.
func selectBestServerRTTShortlistIndexes(candidates []bestServerInternalCandidate, items []providerProfileRTTItem, currentIndex int) []int {
	if len(candidates) == 0 {
		return nil
	}
	limit := bestServerPreflightShortlist
	if limit > len(candidates) {
		limit = len(candidates)
	}

	ordered := append([]providerProfileRTTItem(nil), items...)
	sortProviderProfileRTTItems(ordered)
	indexByID := make(map[string]int, len(candidates))
	for index := range candidates {
		id := strings.TrimSpace(candidates[index].Profile.ID)
		if id != "" {
			if _, exists := indexByID[id]; !exists {
				indexByID[id] = index
			}
		}
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
	for _, item := range ordered {
		if index, ok := indexByID[strings.TrimSpace(item.ProfileID)]; ok {
			add(index)
		}
	}

	// Malformed/legacy rows without a usable profile id are deterministic reserve
	// only; never fall back to subscription order.
	remaining := make([]int, 0, len(candidates))
	for index := range candidates {
		if !seen[index] {
			remaining = append(remaining, index)
		}
	}
	sort.SliceStable(remaining, func(i, j int) bool {
		left := strings.TrimSpace(candidates[remaining[i]].Profile.ID)
		right := strings.TrimSpace(candidates[remaining[j]].Profile.ID)
		if left != right {
			return left < right
		}
		return profileEndpoint(candidates[remaining[i]].Profile) < profileEndpoint(candidates[remaining[j]].Profile)
	})
	for _, index := range remaining {
		add(index)
	}

	// Generic helper contract: keep current representable when a non-foreign
	// caller uses this function.
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

// probeBestServerApplicationPreflight is retained only for VPN Outbound Doctor.
// Diagnostics keeps two named-origin application samples. Canonical Best Server
// ranking intentionally does not duplicate those deep acceptance checks.
func (a *app) probeBestServerApplicationPreflight(ctx context.Context, candidate bestServerInternalCandidate) bestServerProbeResult {
	return a.withBestServerCandidateSOCKS(ctx, candidate, func(ctx context.Context, curlPath, socks string) bestServerProbeResult {
		samples := make([]int, 0, bestServerDiagnosticHTTPRuns)
		for run := 0; run < bestServerDiagnosticHTTPRuns; run++ {
			if ctx.Err() != nil {
				break
			}
			if ms, _, ok := probeBestServerHTTPAny(ctx, curlPath, socks); ok {
				samples = append(samples, ms)
			}
		}
		result := summarizeBestServerSamples(samples, 1)
		if !result.OK && probeBestServerTransportIP(ctx, curlPath, socks) {
			result.TransportOnly = true
		}
		return result
	})
}

// probeBestServerProfilePing measures exactly one signal: fixed-IP HTTPS RTT
// through the selected logical VPN profile. DNS/named-origin acceptance is
// intentionally deferred to strict deep quality.
func (a *app) probeBestServerProfilePing(ctx context.Context, candidate bestServerInternalCandidate) bestServerProbeResult {
	return a.withBestServerCandidateSOCKS(ctx, candidate, func(ctx context.Context, curlPath, socks string) bestServerProbeResult {
		ms, ok := probeBestServerTransportRTT(ctx, curlPath, socks)
		if !ok {
			return bestServerProbeResult{}
		}
		return bestServerProbeResult{OK: true, Samples: []int{ms}, Median: ms}
	})
}

type bestServerCandidateSOCKSProbe func(context.Context, string, string) bestServerProbeResult

func (a *app) withBestServerCandidateSOCKS(ctx context.Context, candidate bestServerInternalCandidate, probe bestServerCandidateSOCKSProbe) bestServerProbeResult {
	if probe == nil || ctx.Err() != nil {
		return bestServerProbeResult{}
	}
	outbound, err := buildBestServerProbeOutbound(candidate.Raw, candidate.Profile)
	if err != nil {
		return bestServerProbeResult{}
	}
	outbound, err = prepareIsolatedProbeOutbound(outbound)
	if err != nil {
		return bestServerProbeResult{}
	}
	releaseProbe, ok := acquireIsolatedXrayProbe(ctx)
	if !ok {
		return bestServerProbeResult{}
	}
	defer releaseProbe()
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
	probeXrayPath, err := isolatedXrayProbePath(tmpDir, xrayPath)
	if err != nil {
		return bestServerProbeResult{}
	}

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
	cmd := exec.CommandContext(runCtx, probeXrayPath, "run", "-confdir", tmpDir)
	cmd.Env = isolatedXrayProbeEnv(append(os.Environ(), "XRAY_LOCATION_ASSET="+a.geoDataAssetDir()))
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	cmd.WaitDelay = 2 * time.Second
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
	return probe(ctx, curlPath, fmt.Sprintf("127.0.0.1:%d", port))
}
