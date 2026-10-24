package main

import (
	"context"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// An optional, identity-bound HTTPS edge diagnostic only. It must never
// participate in the four-service QoE mask, Best Server eligibility, AUTO
// optimization, outage classification or a VPN mutation.
const (
	bestServerChatGPTHTTPSURL     = "https://chatgpt.com/"
	bestServerChatGPTHTTPSDeadline = 3 * time.Second

	bestServerChatGPTHTTPSResponded  = "https_response"
	bestServerChatGPTHTTPSRestricted = "http_restricted"
	bestServerChatGPTHTTPError       = "http_error"
	bestServerChatGPTHTTPSUnknown    = "unknown"
)

func sanitizeBestServerChatGPTHTTPSStatus(value string) string {
	switch value {
	case bestServerChatGPTHTTPSResponded, bestServerChatGPTHTTPSRestricted,
		bestServerChatGPTHTTPError, bestServerChatGPTHTTPSUnknown:
		return value
	default:
		return ""
	}
}

// Browser verification, login and interactive ChatGPT availability are not
// inferred from an HTTP status. In particular, 403/429 are *not* VPN failures.
func classifyBestServerChatGPTHTTPS(output []byte, runErr, probeErr, parentErr error, started bool) string {
	if !started || runErr != nil || probeErr != nil || parentErr != nil {
		return bestServerChatGPTHTTPSUnknown
	}
	if _, valid := parseAutomationVPNHTTPResponse(string(output)); !valid {
		return bestServerChatGPTHTTPSUnknown
	}
	fields := strings.Fields(string(output))
	if len(fields) != 3 {
		return bestServerChatGPTHTTPSUnknown
	}
	status, err := strconv.Atoi(fields[0])
	if err != nil {
		return bestServerChatGPTHTTPSUnknown
	}
	switch {
	case status >= 200 && status < 400:
		return bestServerChatGPTHTTPSResponded
	case status == 401 || status == 403 || status == 405 || status == 429:
		return bestServerChatGPTHTTPSRestricted
	case status >= 400 && status < 600:
		return bestServerChatGPTHTTPError
	default:
		return bestServerChatGPTHTTPSUnknown
	}
}

// Through the same isolated SOCKS route as the current/candidate VPN checks.
// No session cookies, authorization, user input or response body are sent or
// stored. The result is a fixed enum, not a leaked URL or HTTP response.
func probeBestServerChatGPTHTTPS(ctx context.Context, curlPath, socks string) string {
	if ctx == nil || ctx.Err() != nil || strings.TrimSpace(curlPath) == "" || strings.TrimSpace(socks) == "" {
		return bestServerChatGPTHTTPSUnknown
	}
	probeCtx, cancel := context.WithTimeout(ctx, bestServerChatGPTHTTPSDeadline)
	defer cancel()
	cmd := exec.CommandContext(probeCtx, curlPath,
		"--socks5-hostname", socks,
		"-sS", "-I", "-L", "--max-redirs", "2",
		"--connect-timeout", "2", "--max-time", "3",
		"-o", "/dev/null", "-w", "%{http_code}\t%{time_pretransfer}\t%{time_starttransfer}",
		bestServerChatGPTHTTPSURL,
	)
	cmd.WaitDelay = 2 * time.Second
	out, runErr := cmd.Output()
	return classifyBestServerChatGPTHTTPS(out, runErr, probeCtx.Err(), ctx.Err(), cmd.ProcessState != nil)
}
