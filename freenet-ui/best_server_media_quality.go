package main

import (
	"context"
	"math"
	"os/exec"
	"sort"
	"strings"
	"time"
)

const (
	bestServerMediaChunkBytes = 1048576
	bestServerMediaChunkRuns  = 6
	bestServerMediaTimeout    = 14 * time.Second
)

var bestServerMediaServiceURLs = []string{
	"https://www.instagram.com/",
	"https://web.telegram.org/",
	"https://web.whatsapp.com/",
}

type bestServerMediaQualityResult struct {
	OK             bool
	Samples        int
	MedianMbps     float64
	MinMbps        float64
	Stalls         int
	ServiceOK      int
	ServiceTotal   int
	Grade          string
	Penalty        int
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
	return result
}

func probeBestServerMediaQuality(ctx context.Context, curlPath, socks string) bestServerMediaQualityResult {
	mediaCtx, cancel := context.WithTimeout(ctx, bestServerMediaTimeout)
	defer cancel()
	args := []string{
		"--socks5-hostname", socks,
		"-sS", "--connect-timeout", "3", "--max-time", "14",
		"-w", "%{http_code}\t%{size_download}\t%{time_starttransfer}\t%{time_total}\n",
	}
	for i := 0; i < bestServerMediaChunkRuns; i++ {
		args = append(args,
			"-o", "/dev/null",
			"https://speed.cloudflare.com/__down?bytes=1048576&freenet_segment="+string(rune('a'+i)),
		)
	}
	output, err := exec.CommandContext(mediaCtx, curlPath, args...).Output()
	speeds := make([]float64, 0, bestServerMediaChunkRuns)
	if err == nil {
		for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
			if mbps, ok := parseBestServerDownloadMbps(line, bestServerMediaChunkBytes); ok {
				speeds = append(speeds, mbps)
			}
		}
	}

	serviceOK := 0
	for _, url := range bestServerMediaServiceURLs {
		if mediaCtx.Err() != nil {
			break
		}
		probeCtx, cancelProbe := context.WithTimeout(mediaCtx, 4*time.Second)
		out, probeErr := exec.CommandContext(probeCtx, curlPath,
			"--socks5-hostname", socks,
			"-sS", "-I", "-L", "--max-redirs", "2",
			"--connect-timeout", "3", "--max-time", "4",
			"-o", "/dev/null", "-w", "%{http_code}\t%{time_pretransfer}\t%{time_starttransfer}", url,
		).Output()
		cancelProbe()
		if probeErr == nil {
			if _, ok := parseBestServerHTTPResponseMS(string(out)); ok {
				serviceOK++
			}
		}
	}
	return summarizeBestServerMediaQuality(speeds, serviceOK, len(bestServerMediaServiceURLs))
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
