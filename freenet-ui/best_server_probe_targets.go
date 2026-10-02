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

// probeBestServerTransportIP deliberately bypasses proxy-side hostname
// resolution. It is not a quality/ranking sample; it only distinguishes a
// broken DNS/application-origin path from a dead VPN transport. AUTO recovery
// must never mutate a working VPN merely because one or more named probe
// origins are unavailable.
func probeBestServerTransportIP(ctx context.Context, curlPath, socks string) bool {
	if ctx.Err() != nil {
		return false
	}
	seconds := fmt.Sprintf("%.3f", bestServerTransportProbeTimeout.Seconds())
	probeCtx, cancel := context.WithTimeout(ctx, bestServerTransportProbeTimeout)
	defer cancel()
	return exec.CommandContext(probeCtx, curlPath,
		"--socks5", socks,
		"-k", "-sS", "--connect-timeout", seconds, "--max-time", seconds,
		"-o", "/dev/null", bestServerTransportProbeURL,
	).Run() == nil
}
