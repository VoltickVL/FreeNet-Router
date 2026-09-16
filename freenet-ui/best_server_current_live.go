package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	bestServerCurrentFallbackDownloadURL  = "https://speed.cloudflare.com/__down?bytes=4000000"
	bestServerCurrentFallbackMinimumBytes = int64(512_000)
	bestServerCurrentFallbackTimeout      = 6 * time.Second
)

func readBestServerActiveOutbound(path string) (map[string]any, string, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", false
	}
	var doc map[string]any
	if json.Unmarshal(data, &doc) != nil {
		return nil, "", false
	}
	outbounds, _ := doc["outbounds"].([]any)
	for _, raw := range outbounds {
		outbound, _ := raw.(map[string]any)
		if outbound == nil || strings.TrimSpace(fmt.Sprint(outbound["tag"])) != "vless-reality" {
			continue
		}
		settings, _ := outbound["settings"].(map[string]any)
		vnext, _ := settings["vnext"].([]any)
		if len(vnext) == 0 {
			return nil, "", false
		}
		server, _ := vnext[0].(map[string]any)
		address := strings.TrimSpace(fmt.Sprint(server["address"]))
		port := strings.TrimSpace(fmt.Sprint(server["port"]))
		if address == "" || port == "" || address == "<nil>" || port == "<nil>" {
			return nil, "", false
		}
		return outbound, address + ":" + port, true
	}
	return nil, "", false
}

func probeBestServerActiveHTTP(ctx context.Context, curlPath, socks string) bestServerProbeResult {
	samples := make([]int, 0, bestServerQualityHTTPRuns)
	attempts := bestServerQualityHTTPRuns + bestServerQualityWarmupRuns
	for i := 0; i < attempts; i++ {
		probeCtx, cancel := context.WithTimeout(ctx, bestServerQualityHTTPTimeout)
		out, _ := exec.CommandContext(probeCtx, curlPath,
			"--socks5-hostname", socks,
			"-sS", "-o", "/dev/null",
			"--connect-timeout", "3", "--max-time", "5",
			"-w", "%{http_code}\t%{time_pretransfer}\t%{time_starttransfer}",
			bestServerQualityProbeURL,
		).Output()
		cancel()
		ms, ok := parseBestServerHTTPResponseMS(string(out))
		if i < bestServerQualityWarmupRuns {
			continue
		}
		if ok {
			samples = append(samples, ms)
		}
	}
	if len(samples) < bestServerQualityHTTPRequired {
		return bestServerProbeResult{Samples: samples}
	}
	median, jitter := medianAndJitter(samples)
	return bestServerProbeResult{OK: true, Median: median, Jitter: jitter, Samples: samples}
}

func probeBestServerCurrentFallbackDownload(ctx context.Context, curlPath, socks string) (float64, string) {
	if strings.TrimSpace(curlPath) == "" || strings.TrimSpace(socks) == "" {
		return 0, "current VPN fallback throughput probe unavailable"
	}
	probeCtx, cancel := context.WithTimeout(ctx, bestServerCurrentFallbackTimeout)
	defer cancel()
	out, transferErr := exec.CommandContext(probeCtx, curlPath,
		"--socks5-hostname", socks,
		"-sS", "--connect-timeout", "3", "--max-time", "6",
		"-o", "/dev/null",
		"-w", "%{http_code}\t%{size_download}\t%{time_starttransfer}\t%{time_total}",
		bestServerCurrentFallbackDownloadURL,
	).Output()
	if mbps, ok := parseBestServerDownloadMbpsAtLeast(string(out), bestServerCurrentFallbackMinimumBytes); ok {
		return mbps, ""
	}
	issue := bestServerTransferIssue(string(out), transferErr)
	if strings.TrimSpace(issue) == "" {
		issue = "current VPN fallback throughput unavailable"
	}
	return 0, issue
}

func probeBestServerActiveOutbound(ctx context.Context, a *app, outbound map[string]any) bestServerQualityApplicationResult {
	result := bestServerQualityApplicationResult{}
	if outbound == nil {
		return result
	}
	curlPath, err := exec.LookPath("curl")
	if err != nil {
		return result
	}
	proxyDir, err := os.MkdirTemp("", "freenet-current-vpn-")
	if err != nil {
		return result
	}
	defer os.RemoveAll(proxyDir)
	configDir := filepath.Join(proxyDir, "config")
	if err := os.MkdirAll(configDir, 0700); err != nil {
		return result
	}
	port, err := freeLocalPort()
	if err != nil {
		return result
	}
	config := map[string]any{
		"log": map[string]any{"loglevel": "warning"},
		"inbounds": []any{map[string]any{
			"listen": "127.0.0.1", "port": port, "protocol": "socks", "tag": "freenet-current-socks",
			"settings": map[string]any{"auth": "noauth", "udp": false},
		}},
		"outbounds": []any{outbound},
		"routing": map[string]any{
			"domainStrategy": "AsIs",
			"rules": []any{map[string]any{
				"type": "field", "inboundTag": []string{"freenet-current-socks"}, "outboundTag": "vless-reality",
			}},
		},
	}
	body, err := json.Marshal(config)
	if err != nil || os.WriteFile(filepath.Join(configDir, "00_current.json"), body, 0600) != nil {
		return result
	}
	assetDir := a.cfg.GeoDataDir
	if strings.TrimSpace(assetDir) == "" {
		assetDir = defaultGeoDataAssetDir
	}
	cmd := exec.CommandContext(ctx, defaultXrayPath, "run", "-confdir", configDir)
	cmd.Env = append(os.Environ(), "XRAY_LOCATION_ASSET="+assetDir)
	logFile, err := os.Create(filepath.Join(proxyDir, "xray.log"))
	if err != nil {
		return result
	}
	defer logFile.Close()
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		return result
	}
	defer func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
	}()
	if !waitForTCPPort(ctx, "127.0.0.1:"+strconv.Itoa(port), 4*time.Second) {
		return result
	}
	socks := "127.0.0.1:" + strconv.Itoa(port)
	httpResult := probeBestServerActiveHTTP(ctx, curlPath, socks)
	if !httpResult.OK {
		return result
	}
	result.OK = true
	result.HTTP = httpResult
	result.Media = probeBestServerMediaQuality(ctx, curlPath, socks)
	if result.Media.OK && result.Media.MedianMbps > 0 {
		result.DownloadOK = true
		result.DownloadMbps = result.Media.MedianMbps
		return result
	}
	result.DownloadIssue = result.Media.Issue
	if fallbackMbps, fallbackIssue := probeBestServerCurrentFallbackDownload(ctx, curlPath, socks); fallbackMbps > 0 {
		// Current-only observability may use a bounded fallback throughput sample,
		// while Media.OK remains false so Best Server/AUTO eligibility stays strict.
		result.DownloadOK = true
		result.DownloadMbps = fallbackMbps
		result.DownloadIssue = ""
	} else if strings.TrimSpace(result.DownloadIssue) == "" {
		result.DownloadIssue = fallbackIssue
	}
	return result
}

func (a *app) scanActiveCurrentVPNQuality(ctx context.Context, currentEndpoint, currentFilter string) bestServerQualityResponse {
	response := bestServerQualityResponse{
		Success: true, Candidates: []bestServerQualityCandidate{}, Mutation: "NONE",
		ScannedAt: time.Now().UTC().Format(time.RFC3339), CurrentEndpoint: currentEndpoint,
		Message: "Проверен только текущий VPN. Поиск альтернатив не выполнялся.",
	}
	outbound, endpoint, ok := readBestServerActiveOutbound(a.cfg.OutPath)
	if !ok {
		response.Message = "Текущий VPN не удалось определить; поиск альтернатив не запускался."
		return response
	}
	if strings.TrimSpace(currentEndpoint) == "" {
		currentEndpoint = endpoint
		response.CurrentEndpoint = endpoint
	}
	status := a.status()
	name := strings.TrimSpace(status.ProfileLabel)
	if name == "" {
		name = strings.TrimSpace(status.Country + " " + status.City)
	}
	if name == "" {
		name = "Текущий VPN"
	}
	countryCode := strings.ToLower(strings.TrimSpace(status.CountryCode))
	if countryCode == "" {
		countryCode = bestServerCountryCodeFromLabel(name)
	}
	candidate := bestServerQualityCandidate{
		ID: "current-active", Name: name, CountryCode: countryCode, Endpoint: endpoint, Current: true,
		Reachable: true, Available: false, Tested: true, Reason: "Текущий VPN проверяется через активный outbound.",
	}
	probeCtx, cancel := context.WithTimeout(ctx, bestServerQualityCandidateTimeout)
	probe := probeBestServerActiveOutbound(probeCtx, a, outbound)
	cancel()
	if ctx.Err() != nil {
		response.Partial = true
		response.Candidates = append(response.Candidates, candidate)
		return response
	}
	if !probe.OK {
		markBestServerCurrentProbeFailure(&candidate)
		response.Candidates = append(response.Candidates, candidate)
		response.Message = "Текущий VPN не прошёл application probe; альтернативы не искались."
		return response
	}
	candidate.Available = true
	candidate.ApplicationMS = probe.HTTP.Median
	candidate.JitterMS = probe.HTTP.Jitter
	candidate.HTTPSamples = len(probe.HTTP.Samples)
	candidate.DownloadIssue = probe.DownloadIssue
	candidate.MediaIssue = probe.Media.Issue
	candidate.MediaGrade = probe.Media.Grade
	candidate.MediaStalls = probe.Media.Stalls
	candidate.MediaSamples = probe.Media.Samples
	candidate.MediaMbps = roundBestServerMediaMbps(probe.Media.MedianMbps)
	candidate.ServiceOK = probe.Media.ServiceOK
	candidate.ServiceTotal = probe.Media.ServiceTotal
	if probe.DownloadOK && probe.DownloadMbps > 0 {
		candidate.DownloadMbps = roundBestServerMbps(probe.DownloadMbps)
	}
	candidate.Eligible = eligibleBestServerQuality(candidate)
	candidate.Score = bestServerQualityScore(candidate.ApplicationMS, 0, candidate.JitterMS, 0, probe.DownloadMbps, probe.DownloadOK) - probe.Media.Penalty
	if candidate.Score < 1 {
		candidate.Score = 1
	}
	candidate.Confidence = "medium"
	if candidate.Eligible {
		candidate.Confidence = "high"
		candidate.Reason = fmt.Sprintf("Текущий VPN: HTTP %d мс, jitter %d мс, скорость %.1f Мбит/с, сервисы %d/%d.", candidate.ApplicationMS, candidate.JitterMS, candidate.DownloadMbps, candidate.ServiceOK, candidate.ServiceTotal)
		response.Available = true
		response.Message = "Текущий VPN полностью проверен. Альтернативы не искались."
	} else {
		candidate.Rejections = bestServerQualityRejections(candidate)
		candidate.Reason = "Текущий VPN работает, но для полной оценки качества данных пока недостаточно."
		response.Message = "Текущий VPN работает; полной оценки качества недостаточно. Альтернативы не искались."
	}
	response.Candidates = append(response.Candidates, candidate)
	storeBestServerCurrentQuality(currentEndpoint, currentFilter, candidate)
	return response
}

func markBestServerCurrentProbeFailure(candidate *bestServerQualityCandidate) {
	if candidate == nil {
		return
	}
	candidate.Available = false
	candidate.Eligible = false
	candidate.DownloadMbps = 0
	candidate.MediaMbps = 0
	candidate.DownloadIssue = "Текущий VPN не прошёл application probe"
	candidate.MediaIssue = candidate.DownloadIssue
	candidate.Rejections = []string{"Текущий VPN не отвечает через защищённый VPN path"}
	candidate.Reason = "Текущий VPN не прошёл application probe; состояние не считается рабочим."
}

func bestServerCountryCodeFromLabel(label string) string {
	// Reuse the canonical subscription parser so regional-indicator labels such as
	// 🇧🇪/🇮🇹 resolve exactly like the profiles that produced the active outbound.
	if code := profileCountryCode(label); code != "" {
		return code
	}
	// Keep accepting the old plain ISO prefix for backward-compatible labels.
	fields := strings.Fields(strings.TrimSpace(label))
	if len(fields) == 0 || len(fields[0]) != 2 {
		return ""
	}
	code := strings.ToLower(fields[0])
	for _, r := range code {
		if r < 'a' || r > 'z' {
			return ""
		}
	}
	return code
}

func currentBestServerActiveIdentity(outPath, filterPath string) (endpoint, filter string) {
	return readBestServerCurrentEndpoint(outPath), readBestServerCurrentFilter(filterPath)
}
