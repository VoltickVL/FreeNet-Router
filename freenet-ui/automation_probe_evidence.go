package main

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Availability probes need a different timeout and outcome model than the
// 1.5-second shortlist ranking probes. This is a bounded watchdog-only path.
const (
	automationHealthNamedTimeout     = 3 * time.Second
	automationHealthTransportTimeout = 3 * time.Second
)

type automationVPNHTTPObservation struct {
	MS                int
	Reachable         bool
	ConclusiveFailure bool
}

// Any returned HTTP response is evidence that the isolated VPN transported a
// complete HTTP exchange, including geo-restricted 403 and unsupported 405.
// Such responses are service QoE, not evidence that the VPN is offline.
func parseAutomationVPNHTTPResponse(output string) (int, bool) {
	fields := strings.Fields(output)
	if len(fields) != 3 || len(fields[0]) != 3 {
		return 0, false
	}
	code, err := strconv.Atoi(fields[0])
	if err != nil || code < 200 || code > 599 {
		return 0, false
	}
	pretransfer, err1 := strconv.ParseFloat(fields[1], 64)
	start, err2 := strconv.ParseFloat(fields[2], 64)
	if err1 != nil || err2 != nil || math.IsNaN(pretransfer) || math.IsNaN(start) ||
		math.IsInf(pretransfer, 0) || math.IsInf(start, 0) || pretransfer < 0 || start <= pretransfer {
		return 0, false
	}
	ms := int(math.Round((start - pretransfer) * 1000))
	if ms < 1 {
		ms = 1
	}
	return ms, true
}

func automationHTTPConclusiveCurlExit(code int) bool {
	// Bounded, explicit transport failures. Curl 28 (timeout), 6 (DNS), 60
	// (certificate) and startup/unknown errors may reflect slow paths or
	// probing environment, so none of them authorize emergency replacement.
	return code == 7 || code == 35 || code == 56
}

func runAutomationVPNHTTPProbe(ctx context.Context, curlPath, socks, target string, timeout time.Duration, hostname bool) automationVPNHTTPObservation {
	if ctx == nil || ctx.Err() != nil || timeout <= 0 {
		return automationVPNHTTPObservation{}
	}
	seconds := fmt.Sprintf("%.3f", timeout.Seconds())
	args := []string{"--socks5", socks}
	if hostname {
		args = []string{"--socks5-hostname", socks}
	} else {
		// Fixed IP diagnostic only: TLS name validation does not apply to
		// this transport liveness signal.
		args = append(args, "-k")
	}
	args = append(args, "-sS", "--connect-timeout", seconds, "--max-time", seconds,
		"-o", "/dev/null", "-w", "%{http_code}\t%{time_pretransfer}\t%{time_starttransfer}", target)
	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	output, err := exec.CommandContext(probeCtx, curlPath, args...).Output()
	if probeCtx.Err() != nil || ctx.Err() != nil {
		return automationVPNHTTPObservation{} // timeout and cancel = UNKNOWN
	}
	if err == nil {
		if ms, ok := parseAutomationVPNHTTPResponse(string(output)); ok {
			return automationVPNHTTPObservation{MS: ms, Reachable: true}
		}
		return automationVPNHTTPObservation{}
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) && automationHTTPConclusiveCurlExit(exit.ExitCode()) {
		return automationVPNHTTPObservation{ConclusiveFailure: true}
	}
	return automationVPNHTTPObservation{}
}

type automationVPNHTTPRunner func(context.Context, string, string, string, time.Duration, bool) automationVPNHTTPObservation

func probeAutomationNamedHTTPSDetailedWith(ctx context.Context, curlPath, socks string, runner automationVPNHTTPRunner) (int, bool, bool) {
	if ctx == nil || runner == nil || len(bestServerApplicationProbeURLs) < 2 {
		return 0, false, false
	}
	conclusiveFailures := 0
	for _, target := range bestServerApplicationProbeURLs {
		if ctx.Err() != nil {
			break
		}
		observation := runner(ctx, curlPath, socks, target, automationHealthNamedTimeout, true)
		if observation.Reachable {
			return observation.MS, true, false
		}
		if observation.ConclusiveFailure {
			conclusiveFailures++
		}
	}
	return 0, false, ctx.Err() == nil && conclusiveFailures == len(bestServerApplicationProbeURLs)
}

func probeAutomationNamedHTTPSDetailed(ctx context.Context, curlPath, socks string) (int, bool, bool) {
	return probeAutomationNamedHTTPSDetailedWith(ctx, curlPath, socks, runAutomationVPNHTTPProbe)
}
