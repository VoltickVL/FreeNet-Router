package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

const (
	bestServerSpeedtestServersURL  = "https://www.speedtest.net/api/js/servers?engine=js&https_functional=true&limit=10"
	bestServerSpeedtestBytes       = int64(4_000_000)
	bestServerSpeedtestListTimeout = 5 * time.Second
	bestServerSpeedtestRunTimeout  = 5 * time.Second
)

type bestServerSpeedtestServer struct {
	URL  string `json:"url"`
	Host string `json:"host"`
	ID   string `json:"id"`
	Name string `json:"name"`
}

func bestServerSpeedtestDownloadURL(server bestServerSpeedtestServer, nonce int64) string {
	raw := strings.TrimSpace(server.URL)
	if raw != "" {
		if parsed, err := url.Parse(raw); err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != "" {
			parsed.Path = "/download"
			parsed.RawQuery = "size=" + strconv.FormatInt(bestServerSpeedtestBytes, 10) + "&nocache=" + strconv.FormatInt(nonce, 10)
			parsed.Fragment = ""
			return parsed.String()
		}
	}
	host := strings.TrimSpace(server.Host)
	if host == "" || strings.ContainsAny(host, " /?#") {
		return ""
	}
	return "https://" + host + "/download?size=" + strconv.FormatInt(bestServerSpeedtestBytes, 10) + "&nocache=" + strconv.FormatInt(nonce, 10)
}

func discoverBestServerSpeedtestServers(ctx context.Context, curlPath, socks string) ([]bestServerSpeedtestServer, string) {
	listCtx, cancel := context.WithTimeout(ctx, bestServerSpeedtestListTimeout)
	defer cancel()
	output, err := exec.CommandContext(listCtx, curlPath,
		"--socks5-hostname", socks,
		"-sS", "--connect-timeout", "3", "--max-time", "5",
		bestServerSpeedtestServersURL,
	).Output()
	if err != nil {
		return nil, "Speedtest server list unavailable"
	}
	if len(output) == 0 || len(output) > 512*1024 {
		return nil, "Speedtest server list invalid"
	}
	var servers []bestServerSpeedtestServer
	if err := json.Unmarshal(output, &servers); err != nil {
		return nil, "Speedtest server list invalid"
	}
	filtered := make([]bestServerSpeedtestServer, 0, len(servers))
	for _, server := range servers {
		if bestServerSpeedtestDownloadURL(server, 1) != "" {
			filtered = append(filtered, server)
		}
		if len(filtered) >= 5 {
			break
		}
	}
	if len(filtered) == 0 {
		return nil, "Speedtest server list empty"
	}
	return filtered, ""
}

func probeBestServerSpeedtestSingleStream(ctx context.Context, curlPath, socks string, runs int) ([]float64, string) {
	if runs < 1 {
		return nil, "Speedtest run count invalid"
	}
	servers, issue := discoverBestServerSpeedtestServers(ctx, curlPath, socks)
	if len(servers) == 0 {
		return nil, issue
	}

	var selected *bestServerSpeedtestServer
	speeds := make([]float64, 0, runs)
	issues := make([]string, 0, 3)
	for run := 0; run < runs; run++ {
		if ctx.Err() != nil {
			break
		}
		candidates := servers
		if selected != nil {
			candidates = []bestServerSpeedtestServer{*selected}
		}
		measured := false
		for _, server := range candidates {
			nonce := time.Now().UnixNano() + int64(run)
			downloadURL := bestServerSpeedtestDownloadURL(server, nonce)
			if downloadURL == "" {
				continue
			}
			runCtx, cancel := context.WithTimeout(ctx, bestServerSpeedtestRunTimeout)
			output, transferErr := exec.CommandContext(runCtx, curlPath,
				"--socks5-hostname", socks,
				"-sS", "--connect-timeout", "3", "--max-time", "5",
				"-o", "/dev/null",
				"-w", "%{http_code}\t%{size_download}\t%{time_starttransfer}\t%{time_total}",
				downloadURL,
			).Output()
			cancel()
			minimum := bestServerSpeedtestBytes / 5
			if mbps, ok := parseBestServerDownloadMbpsAtLeast(string(output), minimum); ok {
				speeds = append(speeds, mbps)
				copyServer := server
				selected = &copyServer
				measured = true
				break
			}
			if len(issues) < 3 {
				issues = append(issues, bestServerTransferIssue(string(output), transferErr))
			}
		}
		if !measured && selected != nil {
			break
		}
	}
	if len(speeds) == 0 {
		if len(issues) == 0 {
			return nil, "Speedtest download unavailable"
		}
		return nil, "Speedtest download unavailable: " + strings.Join(issues, "; ")
	}
	if len(speeds) < runs {
		return speeds, fmt.Sprintf("Speedtest samples %d/%d", len(speeds), runs)
	}
	return speeds, ""
}
