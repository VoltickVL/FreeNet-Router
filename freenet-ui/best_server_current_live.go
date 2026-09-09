package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func readBestServerActiveOutbound(outPath string) (map[string]any, string, bool) {
	data, err := os.ReadFile(outPath)
	if err != nil {
		return nil, "", false
	}
	var cfg struct {
		Outbounds []map[string]any `json:"outbounds"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, "", false
	}
	for _, outbound := range cfg.Outbounds {
		if strings.TrimSpace(fmt.Sprint(outbound["tag"])) != "vless-reality" {
			continue
		}
		settings, _ := outbound["settings"].(map[string]any)
		vnext, _ := settings["vnext"].([]any)
		if len(vnext) == 0 {
			continue
		}
		first, _ := vnext[0].(map[string]any)
		address := strings.TrimSpace(fmt.Sprint(first["address"]))
		portFloat, _ := first["port"].(float64)
		port := int(portFloat)
		if address == "" || port <= 0 || port > 65535 {
			continue
		}
		return outbound, net.JoinHostPort(address, strconv.Itoa(port)), true
	}
	return nil, "", false
}

func bestServerCountryCodeFromLabel(label string) string {
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

func bestServerProfileFromEndpoint(endpoint string) (subscriptionProfile, bool) {
	host, portText, err := net.SplitHostPort(strings.TrimSpace(endpoint))
	if err != nil || strings.TrimSpace(host) == "" {
		return subscriptionProfile{}, false
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port <= 0 || port > 65535 {
		return subscriptionProfile{}, false
	}
	return subscriptionProfile{Address: host, Port: port}, true
}

func (a *app) probeBestServerActiveOutbound(ctx context.Context, outbound map[string]any) bestServerQualityApplicationResult {
	if len(outbound) == 0 {
		return bestServerQualityApplicationResult{}
	}
	xrayPath := strings.TrimSpace(os.Getenv("FREENET_XRAY_BIN"))
	if xrayPath == "" {
		xrayPath = defaultBestServerXrayPath
	}
	if _, err := os.Stat(xrayPath); err != nil {
		return bestServerQualityApplicationResult{}
	}
	curlPath, err := exec.LookPath("curl")
	if err != nil {
		return bestServerQualityApplicationResult{}
	}
	port, err := reserveBestServerPort()
	if err != nil {
		return bestServerQualityApplicationResult{}
	}
	tmpDir, err := os.MkdirTemp("", "freenet-current-quality-")
	if err != nil {
		return bestServerQualityApplicationResult{}
	}
	defer os.RemoveAll(tmpDir)
	_ = os.Chmod(tmpDir, 0700)

	config := map[string]any{
		"log": map[string]any{"loglevel": "warning"},
		"inbounds": []any{map[string]any{
			"listen": "127.0.0.1", "port": port, "protocol": "socks",
			"settings": map[string]any{"udp": false}, "tag": "freenet-current-quality",
		}},
		"outbounds": []any{outbound},
		"routing": map[string]any{
			"domainStrategy": "AsIs",
			"rules": []any{map[string]any{
				"type": "field", "inboundTag": []string{"freenet-current-quality"}, "outboundTag": "vless-reality",
			}},
		},
	}
	encoded, err := json.Marshal(config)
	if err != nil {
		return bestServerQualityApplicationResult{}
	}
	configPath := filepath.Join(tmpDir, "00_probe.json")
	if err := os.WriteFile(configPath, encoded, 0600); err != nil {
		return bestServerQualityApplicationResult{}
	}

	env := append(os.Environ(), "XRAY_LOCATION_ASSET="+a.geoDataAssetDir())
	testCtx, cancelTest := context.WithTimeout(ctx, 5*time.Second)
	testCmd := exec.CommandContext(testCtx, xrayPath, "run", "-test", "-confdir", tmpDir)
	testCmd.Env = env
	testCmd.Stdout = io.Discard
	testCmd.Stderr = io.Discard
	testErr := testCmd.Run()
	cancelTest()
	if testErr != nil {
		return bestServerQualityApplicationResult{}
	}

	runCtx, cancelRun := context.WithCancel(ctx)
	defer cancelRun()
	cmd := exec.CommandContext(runCtx, xrayPath, "run", "-confdir", tmpDir)
	cmd.Env = env
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return bestServerQualityApplicationResult{}
	}
	defer func() {
		cancelRun()
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
	}()
	if !waitBestServerSOCKS(ctx, port) {
		return bestServerQualityApplicationResult{}
	}

	socks := fmt.Sprintf("127.0.0.1:%d", port)
	for i := 0; i < bestServerQualityWarmupRuns; i++ {
		warmCtx, cancel := context.WithTimeout(ctx, bestServerQualityHTTPTimeout)
		_ = exec.CommandContext(warmCtx, curlPath,
			"--socks5-hostname", socks,
			"-sS", "--connect-timeout", "3", "--max-time", "5",
			"-o", "/dev/null", bestServerQualityProbeURL,
		).Run()
		cancel()
	}

	samples := make([]int, 0, bestServerQualityHTTPRuns)
	for i := 0; i < bestServerQualityHTTPRuns; i++ {
		probeCtx, cancel := context.WithTimeout(ctx, bestServerQualityHTTPTimeout)
		output, err := exec.CommandContext(probeCtx, curlPath,
			"--socks5-hostname", socks,
			"-sS", "--connect-timeout", "3", "--max-time", "5",
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
	httpResult := summarizeBestServerSamples(samples, bestServerQualityHTTPRequired)
	if !httpResult.OK {
		return bestServerQualityApplicationResult{}
	}

	result := bestServerQualityApplicationResult{OK: true, HTTP: httpResult}
	result.Media = probeBestServerMediaQuality(ctx, curlPath, socks)
	if result.Media.OK && result.Media.MedianMbps > 0 {
		result.DownloadOK = true
		result.DownloadMbps = result.Media.MedianMbps
	} else {
		result.DownloadIssue = result.Media.Issue
		if result.DownloadIssue == "" {
			result.DownloadIssue = "Speedtest throughput unavailable"
		}
	}
	return result
}

func (a *app) scanActiveCurrentVPNQuality(ctx context.Context, currentEndpoint, currentFilter string) bestServerQualityResponse {
	outbound, activeEndpoint, ok := readBestServerActiveOutbound(a.cfg.OutPath)
	if !ok || !endpointsEqual(activeEndpoint, currentEndpoint) {
		return bestServerQualityResponse{
			Success: true, Available: false, Candidates: []bestServerQualityCandidate{}, ProfilesScanned: 0, ProfilesTotal: 1,
			Mutation: "NONE", ScannedAt: time.Now().UTC().Format(time.RFC3339), CurrentEndpoint: currentEndpoint,
			Message: "Активный VPN-профиль не удалось безопасно прочитать; другие VPN не проверялись.",
		}
	}
	label := currentExactProfileLabel(a.cfg.FilterPath)
	if label == "" {
		label = "Текущий VPN"
	}
	candidate := bestServerQualityCandidate{
		Tested: true, ID: "current-live", Name: label, CountryCode: bestServerCountryCodeFromLabel(label),
		Endpoint: currentEndpoint, Current: true, Reachable: true,
	}
	if tcpProfile, ok := bestServerProfileFromEndpoint(currentEndpoint); ok {
		tcpProbe := defaultBestServerQualityTCPProbe(ctx, tcpProfile)
		if tcpProbe.OK {
			candidate.TCPRTTMS = tcpProbe.Median
			candidate.TCPJitterMS = tcpProbe.Jitter
		}
	}
	probeCtx, cancel := context.WithTimeout(ctx, bestServerQualityCandidateTimeout)
	probe := a.probeBestServerActiveOutbound(probeCtx, outbound)
	cancel()
	if !probe.OK {
		candidate.Reason = "Проверка активного VPN-пути не завершена"
		return bestServerQualityResponse{
			Success: true, Available: false, Candidates: []bestServerQualityCandidate{candidate}, ProfilesScanned: 1, ProfilesTotal: 1,
			Mutation: "NONE", ScannedAt: time.Now().UTC().Format(time.RFC3339), CurrentEndpoint: currentEndpoint,
			Message: "Текущий VPN найден, но проверка качества не завершена; другие VPN не проверялись.",
		}
	}
	candidate.Available = true
	candidate.ApplicationMS = probe.HTTP.Median
	candidate.JitterMS = probe.HTTP.Jitter
	candidate.HTTPSamples = len(probe.HTTP.Samples)
	candidate.MediaGrade = probe.Media.Grade
	candidate.MediaStalls = probe.Media.Stalls
	candidate.MediaSamples = probe.Media.Samples
	candidate.MediaMbps = roundBestServerMediaMbps(probe.Media.MedianMbps)
	candidate.ServiceOK = probe.Media.ServiceOK
	candidate.ServiceTotal = probe.Media.ServiceTotal
	candidate.DownloadIssue = probe.DownloadIssue
	candidate.MediaIssue = probe.Media.Issue
	if probe.DownloadOK {
		candidate.DownloadMbps = roundBestServerMbps(probe.DownloadMbps)
	}
	candidate.Eligible = eligibleBestServerQuality(candidate)
	candidate.Reason = "Проверен фактический активный VPN-путь"
	response := bestServerQualityResponse{
		Success: true, Available: candidate.Eligible, Candidates: []bestServerQualityCandidate{candidate}, ProfilesScanned: 1, ProfilesTotal: 1,
		Mutation: "NONE", ScannedAt: time.Now().UTC().Format(time.RFC3339), CurrentEndpoint: currentEndpoint,
		Message: "Проверен фактический текущий VPN; поиск других серверов не запускался.",
	}
	storeBestServerCurrentQuality(currentEndpoint, currentFilter, candidate)
	return response
}
