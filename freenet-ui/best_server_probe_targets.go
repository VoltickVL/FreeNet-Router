package main

import (
	"context"
	"fmt"
	"os/exec"
	"time"
)

const (
	bestServerApplicationProbePerTargetTimeout = 1500 * time.Millisecond
	bestServerTransportProbeTimeout            = 1500 * time.Millisecond
	bestServerTransportProbeURL                = "https://1.1.1.1/cdn-cgi/trace"
	bestServerConfirmedVPNPingRuns              = 3
	bestServerConfirmedVPNPingRequired          = 2
)

var bestServerApplicationProbeURLs = []string{
	bestServerQualityProbeURL,
	"https://cp.cloudflare.com/generate_204",
}

type bestServerHTTPProbeRunner func(context.Context, string, string, string, time.Duration) (int, bool)

func runBestServerHTTPProbeURL(ctx context.Context, curlPath, socks, target string, timeout time.Duration) (int, bool) {
	if timeout <= 0 || ctx.Err() != nil {
		return 0, false
	}
	seconds := fmt.Sprintf("%.3f", timeout.Seconds())
	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	output, err := exec.CommandContext(probeCtx, curlPath,
		"--socks5-hostname", socks,
		"-sS", "--connect-timeout", seconds, "--max-time", seconds,
		"-o", "/dev/null", "-w", "%{http_code}\t%{time_pretransfer}\t%{time_starttransfer}", target,
	).Output()
	if err != nil {
		return 0, false
	}
	return parseBestServerHTTPResponseMS(string(output))
}

func probeBestServerHTTPAnyWith(
	ctx context.Context,
	curlPath, socks string,
	targets []string,
	perTarget time.Duration,
	runner bestServerHTTPProbeRunner,
) (int, string, bool) {
	if runner == nil {
		return 0, "", false
	}
	for _, target := range targets {
		if ctx.Err() != nil {
			break
		}
		if ms, ok := runner(ctx, curlPath, socks, target, perTarget); ok {
			return ms, target, true
		}
	}
	return 0, "", false
}

func probeBestServerHTTPAny(ctx context.Context, curlPath, socks string) (int, string, bool) {
	return probeBestServerHTTPAnyWith(
		ctx,
		curlPath,
		socks,
		bestServerApplicationProbeURLs,
		bestServerApplicationProbePerTargetTimeout,
		runBestServerHTTPProbeURL,
	)
}

// runBestServerTransportRTT deliberately bypasses proxy-side hostname
// resolution but still performs a real HTTPS request through the candidate
// VLESS/Reality path. This is the canonical quick ranking signal: DNS/named
// origins are strict deep-quality concerns, not shortlist gates.
func runBestServerTransportRTT(ctx context.Context, curlPath, socks, target string, timeout time.Duration) (int, bool) {
	if timeout <= 0 || ctx.Err() != nil {
		return 0, false
	}
	seconds := fmt.Sprintf("%.3f", timeout.Seconds())
	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	output, err := exec.CommandContext(probeCtx, curlPath,
		"--socks5", socks,
		"-k", "-sS", "--connect-timeout", seconds, "--max-time", seconds,
		"-o", "/dev/null", "-w", "%{http_code}\t%{time_pretransfer}\t%{time_starttransfer}", target,
	).Output()
	if err != nil {
		return 0, false
	}
	return parseBestServerHTTPResponseMS(string(output))
}

func probeBestServerTransportRTTWith(
	ctx context.Context,
	curlPath, socks, target string,
	timeout time.Duration,
	runner bestServerHTTPProbeRunner,
) (int, bool) {
	if runner == nil {
		return 0, false
	}
	return runner(ctx, curlPath, socks, target, timeout)
}

func probeBestServerTransportRTT(ctx context.Context, curlPath, socks string) (int, bool) {
	return probeBestServerTransportRTTWith(
		ctx,
		curlPath,
		socks,
		bestServerTransportProbeURL,
		bestServerTransportProbeTimeout,
		runBestServerTransportRTT,
	)
}

// probeBestServerCanonicalVPNPing is the single user-facing VPN-ping owner.
// Selector RTT, Best Server Stage-0 and Current VPN must all consume this
// exact fixed-IP HTTPS signal so their displayed milliseconds are comparable.
func probeBestServerCanonicalVPNPing(ctx context.Context, curlPath, socks string) bestServerProbeResult {
	ms, ok := probeBestServerTransportRTT(ctx, curlPath, socks)
	if !ok {
		return bestServerProbeResult{}
	}
	return bestServerProbeResult{OK: true, Samples: []int{ms}, Median: ms}
}

// probeBestServerConfirmedVPNPing keeps the same canonical fixed-IP HTTPS
// signal but repeats it only for the bounded finalist set. Full-pool discovery
// stays fast (one sample per profile); finalists use median evidence so a
// single transient RTT spike cannot decide AUTO/Best Server ordering.
func probeBestServerConfirmedVPNPing(ctx context.Context, curlPath, socks string) bestServerProbeResult {
	samples := make([]int, 0, bestServerConfirmedVPNPingRuns)
	for i := 0; i < bestServerConfirmedVPNPingRuns; i++ {
		if ctx.Err() != nil {
			break
		}
		if ms, ok := probeBestServerTransportRTT(ctx, curlPath, socks); ok {
			samples = append(samples, ms)
		}
	}
	return summarizeBestServerSamples(samples, bestServerConfirmedVPNPingRequired)
}

// probeBestServerTransportIP remains diagnostic-only for VPN Outbound Doctor.
// It reuses the same fixed-IP HTTPS path but does not participate in deep
// eligibility or final ranking.
func probeBestServerTransportIP(ctx context.Context, curlPath, socks string) bool {
	_, ok := probeBestServerTransportRTT(ctx, curlPath, socks)
	return ok
}
