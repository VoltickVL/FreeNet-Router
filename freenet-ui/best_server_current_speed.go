package main

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

const (
	bestServerCurrentFallbackDownloadURL   = "https://speed.cloudflare.com/__down?bytes=4000000"
	bestServerCurrentFallbackMinimumBytes = int64(512_000)
	bestServerCurrentFallbackTimeout      = 6 * time.Second
)

// probeBestServerCurrentFallbackDownload is an observability-only fallback for
// the explicit current-VPN check. It runs through the same temporary SOCKS/VPN
// path as the strict media probe. Its result may be displayed, but it does not
// change Media.OK or any Best Server/AUTO eligibility requirement.
func probeBestServerCurrentFallbackDownload(ctx context.Context, curlPath, socks string) (float64, string) {
	if strings.TrimSpace(curlPath) == "" || strings.TrimSpace(socks) == "" {
		return 0, "current VPN fallback throughput probe unavailable"
	}
	probeCtx, cancel := context.WithTimeout(ctx, bestServerCurrentFallbackTimeout)
	defer cancel()
	output, transferErr := exec.CommandContext(probeCtx, curlPath,
		"--socks5-hostname", socks,
		"-sS", "--connect-timeout", "3", "--max-time", "6",
		"-o", "/dev/null",
		"-w", "%{http_code}\t%{size_download}\t%{time_starttransfer}\t%{time_total}",
		bestServerCurrentFallbackDownloadURL,
	).Output()
	if mbps, ok := parseBestServerDownloadMbpsAtLeast(string(output), bestServerCurrentFallbackMinimumBytes); ok {
		return mbps, ""
	}
	issue := bestServerTransferIssue(string(output), transferErr)
	if strings.TrimSpace(issue) == "" {
		issue = "current VPN fallback throughput unavailable"
	}
	return 0, issue
}
