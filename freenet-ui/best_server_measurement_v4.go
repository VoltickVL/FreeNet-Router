package main

import (
	"math"
	"strconv"
	"strings"
)

func bestServerHTTPStatusOK(code string) bool {
	return len(code) == 3 && code[0] == '2' && code[1] >= '0' && code[1] <= '9' && code[2] >= '0' && code[2] <= '9'
}

// parseBestServerHTTPResponseMS measures only the HTTP response phase after
// connection/proxy/TLS setup has completed. That keeps high-RTT connection
// establishment from being mislabeled as HTTP responsiveness.
func parseBestServerHTTPResponseMS(output string) (int, bool) {
	fields := strings.Fields(output)
	if len(fields) != 3 || !bestServerHTTPStatusOK(fields[0]) {
		return 0, false
	}
	pretransfer, err := strconv.ParseFloat(fields[1], 64)
	if err != nil || pretransfer < 0 {
		return 0, false
	}
	startTransfer, err := strconv.ParseFloat(fields[2], 64)
	if err != nil || startTransfer <= pretransfer {
		return 0, false
	}
	ms := int(math.Round((startTransfer - pretransfer) * 1000))
	if ms < 1 {
		ms = 1
	}
	return ms, true
}

// parseBestServerDownloadMbps measures a completed response body. It remains
// strict for short media segments where a truncated object must not look like a
// clean sample.
func parseBestServerDownloadMbps(output string, expectedBytes int64) (float64, bool) {
	if expectedBytes <= 0 {
		return 0, false
	}
	return parseBestServerDownloadMbpsAtLeast(output, int64(math.Ceil(float64(expectedBytes)*0.99)))
}

// parseBestServerDownloadMbpsAtLeast accepts a bounded capacity transfer even
// when curl hit its overall deadline, provided enough response body bytes were
// received to form a useful sustained-throughput sample. curl still emits the
// -w timing fields on timeout, so discarding that sample made v0.2.85 show
// "speed unavailable" despite a healthy VPN path.
func parseBestServerDownloadMbpsAtLeast(output string, minimumBytes int64) (float64, bool) {
	fields := strings.Fields(output)
	if len(fields) != 4 || !bestServerHTTPStatusOK(fields[0]) || minimumBytes <= 0 {
		return 0, false
	}
	size, err := strconv.ParseFloat(fields[1], 64)
	if err != nil || size < float64(minimumBytes) {
		return 0, false
	}
	startTransfer, err := strconv.ParseFloat(fields[2], 64)
	if err != nil || startTransfer < 0 {
		return 0, false
	}
	total, err := strconv.ParseFloat(fields[3], 64)
	if err != nil || total <= startTransfer {
		return 0, false
	}
	bodySeconds := total - startTransfer
	mbps := size * 8 / bodySeconds / 1_000_000
	if mbps <= 0 || math.IsNaN(mbps) || math.IsInf(mbps, 0) {
		return 0, false
	}
	return mbps, true
}
