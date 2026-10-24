package main

import (
	"context"
	"errors"
	"math"
	"math/bits"
	"os/exec"
	"sort"
	"strings"
	"time"
)

const (
	bestServerMediaChunkRuns     = 4
	bestServerMediaRequiredRuns  = 3
	bestServerMediaTimeout       = 32 * time.Second
	bestServerServiceTimeout     = 8 * time.Second
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
	ChatGPTHTTPSStatus string
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

type bestServerMediaStageResult struct {
	speeds       []float64
	issue        string
	serviceOK    int
	serviceTotal int
}

func runBestServerMediaStages(
	ctx context.Context,
	speedStage func(context.Context) ([]float64, string),
	serviceStage func(context.Context) (int, int),
) bestServerMediaStageResult {
	speedCh := make(chan struct {
		speeds []float64
		issue  string
	}, 1)
	serviceCh := make(chan struct {
		ok    int
		total int
	}, 1)

	go func() {
		speeds, issue := speedStage(ctx)
		speedCh <- struct {
			speeds []float64
			issue  string
		}{speeds: speeds, issue: issue}
	}()
	go func() {
		ok, total := serviceStage(ctx)
		serviceCh <- struct {
			ok    int
			total int
		}{ok: ok, total: total}
	}()

	speed := <-speedCh
	services := <-serviceCh
	return bestServerMediaStageResult{
		speeds: speed.speeds, issue: speed.issue,
		serviceOK: services.ok, serviceTotal: services.total,
	}
}

const bestServerServiceAllMask uint8 = 15

// Masks describe only the four fixed public service checks above. A negative
// HTTP response or a bounded target failure is QoE evidence, never evidence
// that the VPN itself is down. An unfinished/local failure stays unknown.
type bestServerServiceReachabilityResult struct {
	OKMask   uint8
	BadMask  uint8
	Complete bool
}

func classifyBestServerServiceObservation(output []byte, err, probeErr, parentErr error, started bool) (bool, bool) {
	if parentErr != nil || !started {
		return false, false
	}
	if probeErr != nil {
		return false, errors.Is(probeErr, context.DeadlineExceeded)
	}
	if err == nil {
		if _, valid := parseAutomationVPNHTTPResponse(string(output)); !valid {
			return false, false
		}
		fields := strings.Fields(string(output))
		ok := bestServerHTTPStatusOK(fields[0])
		return ok, !ok
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		switch exit.ExitCode() {
		case 7, 28, 35, 52, 56:
			return false, true
		case 47:
			// A completed response followed by the existing redirect cap is a
			// service restriction. Missing/malformed output is still unknown.
			_, valid := parseAutomationVPNHTTPResponse(string(output))
			return false, valid
		}
	}
	return false, false
}

// AUTO keeps the original one-observation-per-service contract. A repeated
// signal must not silently change outage/recovery evidence or AUTO cadence.
func probeBestServerServiceReachabilityDetailed(ctx context.Context, curlPath, socks string) bestServerServiceReachabilityResult {
	return probeBestServerServiceReachabilityMode(ctx, curlPath, socks, false)
}

// Manual Best Server and explicit Current VPN quality recheck only confirmed
// transport errors once, without relaxing the strict 4/4 admission criterion.
func probeBestServerServiceReachabilityForMedia(ctx context.Context, curlPath, socks string) (int, int) {
	result := probeBestServerServiceReachabilityMode(ctx, curlPath, socks, true)
	return bits.OnesCount8(result.OKMask), len(bestServerMediaServiceURLs)
}

func bestServerServiceTransientRetryable(err, probeErr error) bool {
	if errors.Is(probeErr, context.DeadlineExceeded) {
		return true
	}
	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		return false
	}
	switch exit.ExitCode() {
	case 7, 28, 35, 52, 56:
		return true
	default:
		// A completed HTTP restriction (including redirect limit 47) is not
		// a transient transport failure and must never be retried here.
		return false
	}
}

func probeBestServerServiceReachabilityMode(ctx context.Context, curlPath, socks string, retryTransient bool) bestServerServiceReachabilityResult {
	result := bestServerServiceReachabilityResult{}
	if ctx == nil || ctx.Err() != nil || len(bestServerMediaServiceURLs) != 4 {
		return result
	}
	serviceCtx, cancelServices := context.WithTimeout(ctx, bestServerServiceTimeout)
	defer cancelServices()
	type serviceObservation struct {
		bit     uint8
		ok, bad bool
	}
	serviceResults := make(chan serviceObservation, len(bestServerMediaServiceURLs))
	for index, serviceURL := range bestServerMediaServiceURLs {
		index, serviceURL := index, serviceURL
		go func() {
			// Per-target attempts remain bounded to 3 seconds each. Even the
			// optional second attempt fits the original 8s service-stage cap.
			// The second attempt is only for an observed transport failure;
			// HTTP 4xx/5xx, unknown startup, parent cancellation never retry.
			attempt := func() (bool, bool, bool) {
				probeCtx, cancelProbe := context.WithTimeout(serviceCtx, 3*time.Second)
				cmd := exec.CommandContext(probeCtx, curlPath,
					"--socks5-hostname", socks,
					"-sS", "-I", "-L", "--max-redirs", "2",
					"--connect-timeout", "2", "--max-time", "3",
					"-o", "/dev/null", "-w", "%{http_code}\t%{time_pretransfer}\t%{time_starttransfer}", serviceURL,
				)
				cmd.WaitDelay = 2 * time.Second
				out, err := cmd.Output()
				ok, bad := classifyBestServerServiceObservation(out, err, probeCtx.Err(), serviceCtx.Err(), cmd.ProcessState != nil)
				canRetry := bad && bestServerServiceTransientRetryable(err, probeCtx.Err())
				cancelProbe()
				return ok, bad, canRetry
			}
			ok, bad, canRetry := attempt()
			if retryTransient && canRetry && serviceCtx.Err() == nil && ctx.Err() == nil {
				ok, bad, _ = attempt()
			}
			serviceResults <- serviceObservation{bit: uint8(1 << index), ok: ok, bad: bad}
		}()
	}
	for range bestServerMediaServiceURLs {
		observation := <-serviceResults
		if observation.ok {
			result.OKMask |= observation.bit
		}
		if observation.bad {
			result.BadMask |= observation.bit
		}
	}
	result.Complete = serviceCtx.Err() == nil && ctx.Err() == nil &&
		result.OKMask&result.BadMask == 0 && result.OKMask|result.BadMask == bestServerServiceAllMask
	return result
}

func probeBestServerServiceReachability(ctx context.Context, curlPath, socks string) (int, int) {
	result := probeBestServerServiceReachabilityDetailed(ctx, curlPath, socks)
	return bits.OnesCount8(result.OKMask), len(bestServerMediaServiceURLs)
}

func probeBestServerMediaQuality(ctx context.Context, curlPath, socks string) bestServerMediaQualityResult {
	// ChatGPT is observational only; overlap its bounded HEAD with existing
	// media work rather than adding a fifth QoE strike or extending the scan.
	chatGPT := make(chan string, 1)
	go func() { chatGPT <- probeBestServerChatGPTHTTPS(ctx, curlPath, socks) }()
	stages := runBestServerMediaStages(
		ctx,
		func(parent context.Context) ([]float64, string) {
			mediaCtx, cancelMedia := context.WithTimeout(parent, bestServerMediaTimeout)
			defer cancelMedia()
			return probeBestServerSpeedtestConcurrent(mediaCtx, curlPath, socks, bestServerMediaChunkRuns)
		},
		func(parent context.Context) (int, int) {
			return probeBestServerServiceReachabilityForMedia(parent, curlPath, socks)
		},
	)

	result := summarizeBestServerAggregateMediaQuality(stages.speeds, stages.serviceOK, stages.serviceTotal)
	result.Issue = stages.issue
	result.ChatGPTHTTPSStatus = <-chatGPT
	// A single failed transfer is diagnostic noise, not an automatic stall.
	// 3/4 completed streams are enough for bounded aggregate evidence.
	if len(stages.speeds) < bestServerMediaRequiredRuns {
		result.Stalls += bestServerMediaRequiredRuns - len(stages.speeds)
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
