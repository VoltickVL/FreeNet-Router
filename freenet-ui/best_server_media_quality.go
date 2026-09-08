package main

import (
	"context"
	"math"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	bestServerMediaChunkBytes   = 1048576
	bestServerMediaChunkRuns    = 6
	bestServerMediaTimeout      = 10 * time.Second
	bestServerServiceTimeout    = 8 * time.Second
	bestServerMediaStreamTimeout = 8 * time.Second
)

var bestServerMediaServiceURLs = []string{
	"https://www.instagram.com/",
	"https://web.telegram.org/",
	"https://web.whatsapp.com/",
	"https://www.youtube.com/",
}

type bestServerMediaQualityResult struct {
	Issue        string
	OK           bool
	Samples      int
	MedianMbps   float64
	MinMbps      float64
	Stalls       int
	ServiceOK    int
	ServiceTotal int
	Grade        string
	Penalty      int
}

func summarizeBestServerMediaQuality(speeds []float64, serviceOK, serviceTotal int) bestServerMediaQualityResult {
	result := bestServerMediaQualityResult{Samples: len(speeds), ServiceOK: serviceOK, ServiceTotal: serviceTotal, Grade: "unknown"}
	if len(speeds) < 4 {
		result.Penalty = 2600
		return result
	}
	clean := append([]float64(nil), speeds...)
	sort.Float64s(clean)
	mid := len(clean) / 2
	if len(clean)%2 == 0 {
		result.MedianMbps = (clean[mid-1] + clean[mid]) / 2
	} else {
		result.MedianMbps = clean[mid]
	}
	result.MinMbps = clean[0]
	stallThreshold := result.MedianMbps * 0.35
	if stallThreshold < 3 {
		stallThreshold = 3
	}
	for _, speed := range clean {
		if speed < stallThreshold {
			result.Stalls++
		}
	}

	applyBestServerMediaGrade(&result, serviceOK, serviceTotal)
	return result
}

// summarizeBestServerConcurrentMediaQuality is used by the real router probe.
// Browser speed tests and normal web/video traffic open several TCP/TLS streams;
// measuring six 1 MiB objects serially made v0.2.84-v0.2.86 understate a healthy
// 250-300 Mbps VPN path by tens of times because every object paid slow-start.
// The probe now downloads the same bounded 6 MiB concurrently and treats the sum
// of body-phase stream rates as the aggregate capacity signal. Per-stream values
// are still retained for stall detection.
func summarizeBestServerConcurrentMediaQuality(speeds []float64, serviceOK, serviceTotal int) bestServerMediaQualityResult {
	result := summarizeBestServerMediaQuality(speeds, serviceOK, serviceTotal)
	if !result.OK {
		return result
	}
	aggregate := 0.0
	for _, speed := range speeds {
		if speed > 0 {
			aggregate += speed
		}
	}
	if aggregate <= 0 {
		result.OK = false
		result.Grade = "unknown"
		result.Penalty = 2600
		return result
	}
	result.MedianMbps = aggregate
	applyBestServerMediaGrade(&result, serviceOK, serviceTotal)
	return result
}

func applyBestServerMediaGrade(result *bestServerMediaQualityResult, serviceOK, serviceTotal int) {
	penalty := result.Stalls * 1800
	switch {
	case result.MedianMbps < 10:
		penalty += 2600
	case result.MedianMbps < 25:
		penalty += 1400
	case result.MedianMbps < 50:
		penalty += 500
	}
	if serviceTotal > 0 && serviceOK < serviceTotal {
		penalty += (serviceTotal - serviceOK) * 900
	}
	result.Penalty = penalty
	result.OK = true

	switch {
	case result.Stalls == 0 && serviceOK == serviceTotal && result.MedianMbps >= 50:
		result.Grade = "excellent"
	case result.Stalls == 0 && serviceOK == serviceTotal && result.MedianMbps >= 20:
		result.Grade = "good"
	case result.Stalls <= 1 && serviceOK >= maxInt(0, serviceTotal-1):
		result.Grade = "fair"
	default:
		result.Grade = "poor"
	}
}

func probeBestServerMediaQuality(ctx context.Context, curlPath, socks string) bestServerMediaQualityResult {
	started := time.Now()
	mediaCtx, cancelMedia := context.WithTimeout(ctx, bestServerMediaTimeout)
	defer cancelMedia()

	type streamResult struct {
		issue string
		mbps float64
		ok   bool
	}
	results := make(chan streamResult, bestServerMediaChunkRuns)
	var wg sync.WaitGroup
	for i := 0; i < bestServerMediaChunkRuns; i++ {
		segment := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			streamCtx, cancelStream := context.WithTimeout(mediaCtx, bestServerMediaStreamTimeout)
			defer cancelStream()
			url := "https://speed.cloudflare.com/__down?bytes=1048576&freenet_segment=" + string(rune('a'+segment))
			output, transferErr := exec.CommandContext(streamCtx, curlPath,
				"--socks5-hostname", socks,
				"-sS", "--connect-timeout", "3", "--max-time", "8",
				"-o", "/dev/null",
				"-w", "%{http_code}\t%{size_download}\t%{time_starttransfer}\t%{time_total}",
				url,
			).Output()
			mbps, ok := parseBestServerDownloadMbps(strings.TrimSpace(string(output)), bestServerMediaChunkBytes)
			issue := ""
			if !ok { issue = bestServerTransferIssue(string(output), transferErr) }
			results <- streamResult{mbps: mbps, ok: ok, issue: issue}
		}()
	}
	wg.Wait()
	elapsed := time.Since(started).Seconds()
	close(results)

	speeds := make([]float64, 0, bestServerMediaChunkRuns)
	issues := make(map[string]int)
	for result := range results {
		if result.ok {
			speeds = append(speeds, result.mbps)
		} else {
			issues[result.issue]++
		}
	}

	// Service reachability has its own bounded budget. Reusing mediaCtx here made
	// the checks inherit an already-expired 14s download deadline, producing 0/3
	// service health even when the VPN itself was healthy.
	serviceCtx, cancelServices := context.WithTimeout(ctx, bestServerServiceTimeout)
	defer cancelServices()
	serviceOK := 0
	serviceResults := make(chan bool, len(bestServerMediaServiceURLs))
	for _, url := range bestServerMediaServiceURLs {
		url := url
		go func() {
		probeCtx, cancelProbe := context.WithTimeout(serviceCtx, 3*time.Second)
		out, _ := exec.CommandContext(probeCtx, curlPath,
			"--socks5-hostname", socks,
			"-sS", "-I", "-L", "--max-redirs", "2",
			"--connect-timeout", "2", "--max-time", "3",
			"-o", "/dev/null", "-w", "%{http_code}\t%{time_pretransfer}\t%{time_starttransfer}", url,
		).Output()
		cancelProbe()
		_, ok := parseBestServerHTTPResponseMS(string(out))
		serviceResults <- ok
		}()
	}
	for range bestServerMediaServiceURLs {
		if <-serviceResults { serviceOK++ }
	}
	result := summarizeBestServerMediaQuality(speeds, serviceOK, len(bestServerMediaServiceURLs))
	issueLines := make([]string, 0, len(issues))
	for issue, count := range issues { issueLines = append(issueLines, strconv.Itoa(count)+"× "+issue) }
	sort.Strings(issueLines)
	result.Issue = strings.Join(issueLines, "; ")
	if result.OK {
		// Failed streams are failures, not invisible missing samples. Real
		// aggregate throughput uses one shared wall clock, not summed rates
		// from different stream intervals (which can overstate capacity).
		result.Stalls += bestServerMediaChunkRuns - len(speeds)
		if elapsed > 0 { result.MedianMbps = float64(len(speeds)*bestServerMediaChunkBytes*8) / elapsed / 1000000 }
		applyBestServerMediaGrade(&result, serviceOK, len(bestServerMediaServiceURLs))
	}
	return result
}

func roundBestServerMediaMbps(value float64) float64 {
	return math.Round(value*10) / 10
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
