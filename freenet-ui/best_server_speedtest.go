package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	bestServerSpeedtestServersURL   = "https://www.speedtest.net/api/js/servers?engine=js&https_functional=1&limit=10"
	bestServerSpeedtestBytes        = int64(8_000_000)
	bestServerSpeedtestListTimeout  = 4 * time.Second
	bestServerSpeedtestRunTimeout   = 6 * time.Second
	bestServerSpeedtestServerTries  = 2
)

type bestServerSpeedtestServer struct {
	URL  string `json:"url"`
	Host string `json:"host"`
	ID   string `json:"id"`
	Name string `json:"name"`
}

type bestServerSpeedtestStreamResult struct {
	Mbps  float64
	Issue string
	OK    bool
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
		"-sS", "--connect-timeout", "3", "--max-time", "4",
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

func probeBestServerSpeedtestConcurrent(ctx context.Context, curlPath, socks string, streams int) ([]float64, string) {
	if streams < 1 {
		return nil, "Speedtest stream count invalid"
	}
	servers, issue := discoverBestServerSpeedtestServers(ctx, curlPath, socks)
	if len(servers) == 0 {
		return nil, issue
	}

	tries := bestServerSpeedtestServerTries
	if tries > len(servers) {
		tries = len(servers)
	}
	bestSpeeds := []float64(nil)
	bestIssues := []string(nil)
	for serverIndex := 0; serverIndex < tries; serverIndex++ {
		if ctx.Err() != nil {
			break
		}
		server := servers[serverIndex]
		results := make(chan bestServerSpeedtestStreamResult, streams)
		var wg sync.WaitGroup
		for stream := 0; stream < streams; stream++ {
			stream := stream
			wg.Add(1)
			go func() {
				defer wg.Done()
				nonce := time.Now().UnixNano() + int64(stream)
				downloadURL := bestServerSpeedtestDownloadURL(server, nonce)
				if downloadURL == "" {
					results <- bestServerSpeedtestStreamResult{Issue: "Speedtest download URL invalid"}
					return
				}
				runCtx, cancel := context.WithTimeout(ctx, bestServerSpeedtestRunTimeout)
				output, transferErr := exec.CommandContext(runCtx, curlPath,
					"--socks5-hostname", socks,
					"-sS", "--connect-timeout", "3", "--max-time", "6",
					"-o", "/dev/null",
					"-w", "%{http_code}\t%{size_download}\t%{time_starttransfer}\t%{time_total}",
					downloadURL,
				).Output()
				cancel()
				minimum := bestServerSpeedtestBytes / 5
				if mbps, ok := parseBestServerDownloadMbpsAtLeast(string(output), minimum); ok {
					results <- bestServerSpeedtestStreamResult{Mbps: mbps, OK: true}
					return
				}
				results <- bestServerSpeedtestStreamResult{Issue: bestServerTransferIssue(string(output), transferErr)}
			}()
		}
		wg.Wait()
		close(results)

		speeds := make([]float64, 0, streams)
		issueCounts := map[string]int{}
		for result := range results {
			if result.OK {
				speeds = append(speeds, result.Mbps)
			} else if result.Issue != "" {
				issueCounts[result.Issue]++
			}
		}
		issues := make([]string, 0, len(issueCounts))
		for text, count := range issueCounts {
			issues = append(issues, strconv.Itoa(count)+"× "+text)
		}
		sort.Strings(issues)
		if len(speeds) > len(bestSpeeds) {
			bestSpeeds = append([]float64(nil), speeds...)
			bestIssues = append([]string(nil), issues...)
		}
		if len(speeds) == streams {
			return speeds, ""
		}
	}

	if len(bestSpeeds) == 0 {
		if len(bestIssues) == 0 {
			return nil, "Speedtest download unavailable"
		}
		return nil, "Speedtest download unavailable: " + strings.Join(bestIssues, "; ")
	}
	message := fmt.Sprintf("Speedtest streams %d/%d", len(bestSpeeds), streams)
	if len(bestIssues) > 0 {
		message += ": " + strings.Join(bestIssues, "; ")
	}
	return bestSpeeds, message
}
