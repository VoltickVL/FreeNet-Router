package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	bestServerMaxCandidates    = 64
	bestServerTCPTimeout       = 1200 * time.Millisecond
	bestServerApplicationRuns  = 2
	bestServerApplicationLimit = 6 * time.Second
	bestServerProbeURL              = "https://www.gstatic.com/generate_204"
	bestServerSOCKSStartupTimeout    = 3 * time.Second
	defaultBestServerXrayPath       = "/opt/sbin/xray"
)

type bestServerInternalCandidate struct {
	Profile          subscriptionProfile
	Raw              string
	VPNRTTMS         int
	VPNJitterMS      int
	VPNPingConfirmed bool
}

type bestServerProbeResult struct {
	OK            bool
	TransportOnly bool
	Samples       []int
	Median        int
	Jitter        int
}

func safeBestServerError(err error) string {
	if err == nil {
		return ""
	}
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "Best Server scan timed out"
	case errors.Is(err, context.Canceled):
		return "Best Server scan was canceled"
	default:
		return "Best Server recommendation is unavailable"
	}
}

func endpointsEqual(a, b string) bool {
	return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b)) && strings.TrimSpace(a) != ""
}

func (a *app) discoverBestServerCandidates(ctx context.Context) ([]bestServerInternalCandidate, int, bool, error) {
    candidates, total, truncated, _, err := a.discoverBestServerCandidatesWithSource(ctx)
    return candidates, total, truncated, err
}

func parseBestServerCandidates(body []byte) ([]bestServerInternalCandidate, int, bool, error) {
	text := strings.ReplaceAll(string(body), "\r", "")
	if !strings.Contains(strings.ToLower(text), "vless://") {
		decoded, err := decodeSubscriptionBase64(text)
		if err != nil {
			return nil, 0, false, errors.New("subscription format is unsupported")
		}
		text = strings.ReplaceAll(string(decoded), "\r", "")
	}

	all := make([]bestServerInternalCandidate, 0, bestServerMaxCandidates)
	seen := map[string]bool{}
	total := 0
	truncated := false
	for _, rawLine := range strings.Split(text, "\n") {
		line := strings.TrimSpace(rawLine)
		lower := strings.ToLower(line)
		if !strings.HasPrefix(lower, "vless://") || !strings.Contains(lower, "extra") || strings.Contains(lower, "expired") {
			continue
		}
		profile, ok := parseSafeVLESSProfile(line)
		if !ok || seen[profile.ID] {
			continue
		}
		seen[profile.ID] = true
		total++
		if len(all) >= bestServerMaxCandidates {
			truncated = true
			continue
		}
		all = append(all, bestServerInternalCandidate{Profile: profile, Raw: line})
	}
	if total == 0 || len(all) == 0 {
		return nil, 0, false, errors.New("no active Extra profiles found")
	}
	return all, total, truncated, nil
}

func withoutBestServerCurrentLogicalAlternatives(candidates []bestServerInternalCandidate, currentEndpoint, currentFilter, exactLabel string) []bestServerInternalCandidate {
	currentFilter = strings.TrimSpace(currentFilter)
	if currentFilter == "" {
		return candidates
	}
	matcher, err := regexp.Compile(currentFilter)
	if err != nil {
		return candidates
	}
	exactLabel = sanitizeProfileName(exactLabel)
	filterMatches := make([]int, 0, 2)
	exactMatches := make([]int, 0, 2)
	for i, candidate := range candidates {
		if endpointsEqual(profileEndpoint(candidate.Profile), currentEndpoint) || !matcher.MatchString(candidate.Profile.Name) {
			continue
		}
		filterMatches = append(filterMatches, i)
		if exactLabel != "" && sanitizeProfileName(candidate.Profile.Name) == exactLabel {
			exactMatches = append(exactMatches, i)
		}
	}
	removeIndexes := exactMatches
	if len(removeIndexes) == 0 {
		if len(filterMatches) != 1 {
			return candidates
		}
		removeIndexes = filterMatches
	}
	remove := make(map[int]struct{}, len(removeIndexes))
	for _, index := range removeIndexes {
		remove[index] = struct{}{}
	}
	out := make([]bestServerInternalCandidate, 0, len(candidates)-len(removeIndexes))
	for i, candidate := range candidates {
		if _, drop := remove[i]; !drop {
			out = append(out, candidate)
		}
	}
	return out
}

func bestServerCurrentCandidateIndex(internal []bestServerInternalCandidate, currentEndpoint, currentFilter string) int {
	endpointMatches := make([]int, 0, 2)
	for i, candidate := range internal {
		if endpointsEqual(profileEndpoint(candidate.Profile), currentEndpoint) {
			endpointMatches = append(endpointMatches, i)
		}
	}
	if len(endpointMatches) == 0 {
		return -1
	}

	currentFilter = strings.TrimSpace(currentFilter)
	if currentFilter == "" {
		if len(endpointMatches) == 1 {
			return endpointMatches[0]
		}
		return -1
	}

	matcher, err := regexp.Compile(currentFilter)
	if err != nil {
		return -1
	}
	matched := -1
	count := 0
	for _, index := range endpointMatches {
		if matcher.MatchString(internal[index].Profile.Name) {
			matched = index
			count++
		}
	}
	if count == 1 {
		return matched
	}
	return -1
}

func defaultBestServerTCPProbe(ctx context.Context, profile subscriptionProfile) bestServerProbeResult {
	dialer := net.Dialer{Timeout: bestServerTCPTimeout}
	samples := make([]int, 0, 1)
	started := time.Now()
	conn, err := dialer.DialContext(ctx, "tcp", profileEndpoint(profile))
	elapsed := int(time.Since(started).Milliseconds())
	if err == nil {
		_ = conn.Close()
		if elapsed < 1 {
			elapsed = 1
		}
		samples = append(samples, elapsed)
	}
	return summarizeBestServerSamples(samples, 1)
}

func summarizeBestServerSamples(samples []int, required int) bestServerProbeResult {
	clean := make([]int, 0, len(samples))
	for _, sample := range samples {
		if sample > 0 {
			clean = append(clean, sample)
		}
	}
	if len(clean) < required {
		return bestServerProbeResult{OK: false, Samples: clean}
	}
	sort.Ints(clean)
	median := clean[len(clean)/2]
	if len(clean)%2 == 0 {
		median = (clean[len(clean)/2-1] + clean[len(clean)/2]) / 2
	}
	jitter := 0
	if len(clean) > 1 {
		jitter = clean[len(clean)-1] - clean[0]
	}
	return bestServerProbeResult{OK: true, Samples: clean, Median: median, Jitter: jitter}
}

func readBestServerCurrentEndpoint(outPath string) string {
	data, err := os.ReadFile(outPath)
	if err != nil {
		return ""
	}
	var cfg xrayConfig
	if json.Unmarshal(data, &cfg) != nil {
		return ""
	}
	for _, outbound := range cfg.Outbounds {
		if outbound.Tag != "vless-reality" || len(outbound.Settings.VNext) == 0 {
			continue
		}
		vnext := outbound.Settings.VNext[0]
		if vnext.Address == "" || vnext.Port <= 0 {
			continue
		}
		return net.JoinHostPort(vnext.Address, strconv.Itoa(vnext.Port))
	}
	return ""
}

func readBestServerCurrentFilter(filterPath string) string {
	data, err := os.ReadFile(filterPath)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func (a *app) probeBestServerApplication(ctx context.Context, candidate bestServerInternalCandidate) bestServerProbeResult {
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
	tmpDir, err := os.MkdirTemp("", "freenet-best-server-")
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
			"settings": map[string]any{"udp": false}, "tag": "freenet-best-probe",
		}},
		"outbounds": []any{outbound},
		"routing": map[string]any{
			"domainStrategy": "AsIs",
			"rules": []any{map[string]any{
				"type": "field", "inboundTag": []string{"freenet-best-probe"}, "outboundTag": "vless-reality",
			}},
		},
	}
	encoded, err := json.Marshal(config)
	if err != nil {
		return bestServerProbeResult{}
	}
	configPath := filepath.Join(tmpDir, "00_probe.json")
	if err := os.WriteFile(configPath, encoded, 0600); err != nil {
		return bestServerProbeResult{}
	}

	env := isolatedXrayProbeEnv(append(os.Environ(), "XRAY_LOCATION_ASSET="+a.geoDataAssetDir()))
	testCtx, cancelTest := context.WithTimeout(ctx, 5*time.Second)
	testCmd := exec.CommandContext(testCtx, probeXrayPath, "run", "-test", "-confdir", tmpDir)
	testCmd.Env = env
	testCmd.Stdout = io.Discard
	testCmd.Stderr = io.Discard
	testCmd.WaitDelay = 2 * time.Second
	testErr := testCmd.Run()
	cancelTest()
	if testErr != nil {
		return bestServerProbeResult{}
	}

	runCtx, cancelRun := context.WithCancel(ctx)
	defer cancelRun()
	cmd := exec.CommandContext(runCtx, probeXrayPath, "run", "-confdir", tmpDir)
	cmd.Env = env
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

	samples := make([]int, 0, bestServerApplicationRuns)
	for i := 0; i < bestServerApplicationRuns; i++ {
		probeCtx, cancel := context.WithTimeout(ctx, bestServerApplicationLimit)
		output, err := exec.CommandContext(probeCtx, curlPath,
			"--socks5-hostname", fmt.Sprintf("127.0.0.1:%d", port),
			"-sS", "--connect-timeout", "3", "--max-time", "6",
			"-o", "/dev/null", "-w", "%{http_code}\t%{time_total}", bestServerProbeURL,
		).Output()
		cancel()
		if err != nil {
			continue
		}
		fields := strings.Fields(string(output))
		if len(fields) != 2 || len(fields[0]) != 3 || fields[0][0] < '2' || fields[0][0] > '4' {
			continue
		}
		seconds, err := strconv.ParseFloat(fields[1], 64)
		if err != nil || seconds <= 0 {
			continue
		}
		ms := int(math.Round(seconds * 1000))
		if ms < 1 {
			ms = 1
		}
		samples = append(samples, ms)
	}
	return summarizeBestServerSamples(samples, bestServerApplicationRuns)
}

func buildBestServerProbeOutbound(raw string, profile subscriptionProfile) (map[string]any, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || !strings.EqualFold(u.Scheme, "vless") || u.User == nil {
		return nil, errors.New("invalid VLESS profile")
	}
	spec, ok := parseSupportedVLESSTransport(u)
	if !ok {
		return nil, errors.New("unsupported VLESS transport")
	}
	uuid := strings.TrimSpace(u.User.Username())
	query := u.Query()
	fingerprint := strings.TrimSpace(query.Get("fp"))
	if fingerprint == "" {
		fingerprint = "firefox"
	}
	flow := strings.TrimSpace(query.Get("flow"))
	if spec.Security == "reality" && flow == "" {
		flow = "xtls-rprx-vision"
	}
	if spec.Network == "ws" && flow != "" {
		return nil, errors.New("VLESS WS profile must not use XTLS flow")
	}
	if uuid == "" || profile.Address == "" || profile.Port <= 0 || spec.ServerName == "" {
		return nil, errors.New("incomplete VLESS profile")
	}

	user := map[string]any{"id": uuid, "encryption": "none", "level": 0}
	if flow != "" {
		user["flow"] = flow
	}
	stream := map[string]any{"network": spec.Network, "security": spec.Security}
	switch spec.Security {
	case "reality":
		publicKey := strings.TrimSpace(query.Get("pbk"))
		shortID := strings.TrimSpace(query.Get("sid"))
		spiderX := strings.TrimSpace(query.Get("spx"))
		if spiderX == "" {
			spiderX = "/"
		}
		if publicKey == "" || shortID == "" {
			return nil, errors.New("incomplete VLESS Reality profile")
		}
		stream["realitySettings"] = map[string]any{
			"fingerprint": fingerprint, "serverName": spec.ServerName, "publicKey": publicKey,
			"shortId": shortID, "spiderX": spiderX,
		}
	case "tls":
		stream["tlsSettings"] = map[string]any{
			"fingerprint": fingerprint,
			"serverName": spec.ServerName,
		}
		if spec.Network == "ws" {
			stream["wsSettings"] = map[string]any{
				"path": spec.Path,
				"headers": map[string]any{"Host": spec.Host},
			}
		}
	default:
		return nil, errors.New("unsupported VLESS transport")
	}

	return map[string]any{
		"tag": "vless-reality",
		"protocol": "vless",
		"settings": map[string]any{"vnext": []any{map[string]any{
			"address": profile.Address, "port": profile.Port,
			"users": []any{user},
		}}},
		"streamSettings": stream,
	}, nil
}

func reserveBestServerPort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer listener.Close()
	address, ok := listener.Addr().(*net.TCPAddr)
	if !ok || address.Port <= 0 {
		return 0, errors.New("cannot reserve local probe port")
	}
	return address.Port, nil
}

func waitBestServerSOCKS(ctx context.Context, port int) bool {
	deadline := time.NewTimer(bestServerSOCKSStartupTimeout)
	defer deadline.Stop()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	address := fmt.Sprintf("127.0.0.1:%d", port)
	for {
		select {
		case <-ctx.Done():
			return false
		case <-deadline.C:
			return false
		case <-ticker.C:
			conn, err := net.DialTimeout("tcp", address, 150*time.Millisecond)
			if err == nil {
				_ = conn.Close()
				return true
			}
		}
	}
}
