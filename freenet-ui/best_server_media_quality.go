package main

import (
	"context"
	"math"
	"os/exec"
	"sort"
	"time"
)

const (
	bestServerMediaChunkRuns     = 4
	bestServerMediaRequiredRuns  = 3
	bestServerMediaTimeout       = 10 * time.Second
	bestServerServiceTimeout     = 6 * time.Second
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
	if len(speeds) < bestServerMediaRequiredRuns {
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

func summarizeBestServerAggregateMediaQuality(speeds []float64, serviceOK, serviceTotal int) bestServerMediaQualityResult {
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
	mediaCtx, cancelMedia := context.WithTimeout(ctx, bestServerMediaTimeout)
	speeds, issue := probeBestServerSpeedtestConcurrent(mediaCtx, curlPath, socks, bestServerMediaChunkRuns)
	cancelMedia()

	serviceCtx, cancelServices := context.WithTimeout(ctx, bestServerServiceTimeout)
	defer cancelServices()
	serviceOK := 0
	serviceResults := make(chan bool, len(bestServerMediaServiceURLs))
	for _, serviceURL := range bestServerMediaServiceURLs {
		serviceURL := serviceURL
		go func() {
			probeCtx, cancelProbe := context.WithTimeout(serviceCtx, 3*time.Second)
			out, _ := exec.CommandContext(probeCtx, curlPath,
				"--socks5-hostname", socks,
				"-sS", "-I", "-L", "--max-redirs", "2",
				"--connect-timeout", "2", "--max-time", "3",
				"-o", "/dev/null", "-w", "%{http_code}\t%{time_pretransfer}\t%{time_starttransfer}", serviceURL,
			).Output()
			cancelProbe()
			_, ok := parseBestServerHTTPResponseMS(string(out))
			serviceResults <- ok
		}()
	}
	for range bestServerMediaServiceURLs {
		if <-serviceResults {
			serviceOK++
		}
	}

	result := summarizeBestServerAggregateMediaQuality(speeds, serviceOK, len(bestServerMediaServiceURLs))
	result.Issue = issue
	// A single failed transfer is diagnostic noise, not an automatic stall.
	// 3/4 completed streams are enough for bounded aggregate evidence.
	if len(speeds) < bestServerMediaRequiredRuns {
		result.Stalls += bestServerMediaRequiredRuns - len(speeds)
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
