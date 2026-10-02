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
	bestServerPreflightPhaseTimeout       = 50 * time.Second
	bestServerPreflightShortlist          = 10
	bestServerProfilePingHTTPRuns         = 1
	bestServerProfilePingTimeout          = 2 * time.Second
	bestServerProfilePingPerTargetTimeout = 700 * time.Millisecond
)

// applicationAwareBestServerShortlist uses the same per-logical-profile
// application RTT sweep as the VPN picker. The quick RTT is ranking-only
// evidence: strict HTTP/throughput/services/stability acceptance remains in the
// deep quality probe. Every profile gets the same bounded chance; subscription
// position and shared provider IP:port never rank a logical VPN.
func (a *app) applicationAwareBestServerShortlist(ctx context.Context, candidates []bestServerInternalCandidate, currentEndpoint, currentFilter string) []bestServerInternalCandidate {
	if len(candidates) <= 1 {
		return candidates
	}

	phaseCtx, cancelPhase := context.WithTimeout(ctx, bestServerPreflightPhaseTimeout)
	defer cancelPhase()
	reportBestServerProgress(ctx, "preflight", 0, len(candidates))
	items := measureProviderProfileRTT(phaseCtx, candidates, a.probeBestServerProfilePing)

	currentIndex := bestServerCurrentCandidateIndex(candidates, currentEndpoint, currentFilter)
	selectedIndexes := selectBestServerRTTShortlistIndexes(candidates, items, currentIndex)
	selected := make([]bestServerInternalCandidate, 0, len(selectedIndexes))
	for _, index := range selectedIndexes {
		selected = append(selected, candidates[index])
	}
	return selected
}

// selectBestServerRTTShortlistIndexes builds the bounded deep-check queue from
// canonical VPN application RTT evidence. Confirmed reachable profiles are
// ordered by RTT, UNKNOWN is only reserve evidence, transport-only comes after
// UNKNOWN, and explicit application failures are last. The hard deep maximum is
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

func (a *app) probeBestServerProfilePing(ctx context.Context, candidate bestServerInternalCandidate) bestServerProbeResult {
	return a.probeBestServerProxyHTTP(ctx, candidate, bestServerProfilePingHTTPRuns, bestServerProfilePingPerTargetTimeout)
}

func (a *app) probeBestServerProxyHTTP(ctx context.Context, candidate bestServerInternalCandidate, runs int, perTarget time.Duration) bestServerProbeResult {
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

	socks := fmt.Sprintf("127.0.0.1:%d", port)
	if runs < 1 {
		runs = 1
	}
	samples := make([]int, 0, runs)
	for run := 0; run < runs; run++ {
		if ctx.Err() != nil {
			break
		}
		if perTarget <= 0 {
			perTarget = bestServerApplicationProbePerTargetTimeout
		}
		if ms, _, ok := probeBestServerHTTPAnyWith(ctx, curlPath, socks, bestServerApplicationProbeURLs, perTarget, runBestServerHTTPProbeURL); ok {
			samples = append(samples, ms)
		}
	}
	result := summarizeBestServerSamples(samples, 1)
	if !result.OK && probeBestServerTransportIP(ctx, curlPath, socks) {
		result.TransportOnly = true
	}
	return result
}
