package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	bestServerQualityTCPRuns              = 3
	bestServerQualityTCPRequired          = 2
	bestServerQualityTCPTimeout           = 1200 * time.Millisecond
	bestServerQualityWarmupRuns           = 1
	bestServerQualityHTTPRuns             = 3
	bestServerQualityHTTPRequired         = 2
	bestServerQualityHTTPTimeout          = 5 * time.Second
	bestServerQualityCandidateTimeout     = 50 * time.Second
	bestServerQualityScanTimeout          = 420 * time.Second
	bestServerQualityProbeURL             = "https://www.gstatic.com/generate_204"
	bestServerQualityNoSpeedPenalty       = 3000
	bestServerQualityVeryLowSpeedPenalty  = 3200
	bestServerQualityLowSpeedPenalty      = 1600
	bestServerQualityModerateSpeedPenalty = 500
	bestServerQualityHighJitterMS         = 80
	bestServerQualityMaxApplicationMS     = 220
	bestServerThroughputStrictAggregate   = "strict_aggregate"
	bestServerThroughputCurrentFallback   = "current_fallback"
)

type bestServerQualityCandidate struct {
	Tested        bool     `json:"tested"`
	Rejections    []string `json:"rejections,omitempty"`
	DownloadIssue string   `json:"download_issue,omitempty"`
	MediaIssue    string   `json:"media_issue,omitempty"`
	Eligible      bool     `json:"eligible"`
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	CountryCode   string   `json:"country_code,omitempty"`
	Endpoint      string   `json:"endpoint"`
	Current       bool     `json:"current"`
	Reachable     bool     `json:"reachable"`
	Available     bool     `json:"available"`
	TCPRTTMS      int      `json:"tcp_rtt_ms,omitempty"`
	TCPJitterMS   int      `json:"tcp_jitter_ms,omitempty"`
	VPNRTTMS      int      `json:"vpn_rtt_ms,omitempty"`
	VPNJitterMS   int      `json:"vpn_jitter_ms,omitempty"`
	ApplicationMS int      `json:"application_rtt_ms,omitempty"`
	JitterMS      int      `json:"jitter_ms,omitempty"`
	DownloadMbps         float64 `json:"download_mbps,omitempty"`
	FallbackDownloadMbps float64 `json:"fallback_download_mbps,omitempty"`
	ThroughputSource     string  `json:"throughput_source,omitempty"`
	HTTPSamples          int     `json:"http_samples,omitempty"`
	MediaGrade    string   `json:"media_grade,omitempty"`
	MediaStalls   int      `json:"media_stalls,omitempty"`
	MediaSamples  int      `json:"media_samples,omitempty"`
	MediaMbps     float64  `json:"media_mbps,omitempty"`
	ServiceOK     int      `json:"service_ok,omitempty"`
	ServiceTotal  int      `json:"service_total,omitempty"`
	Score         int      `json:"score,omitempty"`
	Confidence    string   `json:"confidence,omitempty"`
	Reason        string   `json:"reason"`
}

type bestServerQualityResponse struct {
	SelectionToken    string                       `json:"selection_token,omitempty"`
	Partial           bool                         `json:"partial,omitempty"`
	Success           bool                         `json:"success"`
	Available         bool                         `json:"available"`
	ScannedAt         string                       `json:"scanned_at,omitempty"`
	CurrentEndpoint   string                       `json:"current_endpoint,omitempty"`
	Recommendation    *bestServerQualityCandidate  `json:"recommendation,omitempty"`
	Candidates        []bestServerQualityCandidate `json:"candidates"`
	ProfilesScanned   int                          `json:"profiles_scanned"`
	ProfilesTotal     int                          `json:"profiles_total"`
	ProfilesTruncated bool                         `json:"profiles_truncated,omitempty"`
	Mutation          string                       `json:"mutation"`
	Message           string                       `json:"message,omitempty"`
	Error             string                       `json:"error,omitempty"`
}

type bestServerQualityApplicationResult struct {
	DownloadIssue        string
	OK                   bool
	HTTP                 bestServerProbeResult
	DownloadOK           bool
	DownloadMbps         float64
	FallbackDownloadMbps float64
	ThroughputSource     string
	Media                bestServerMediaQualityResult
}

type bestServerQualityApplicationProbe func(context.Context, bestServerInternalCandidate) bestServerQualityApplicationResult

func registerBestServerQualityAPI(mux *http.ServeMux, a *app) {
	mux.HandleFunc("GET /api/vpn/best", a.requireAuth(a.handleBestServerQuality))
}

func (a *app) handleBestServerQuality(w http.ResponseWriter, r *http.Request) {
	select {
	case a.sem <- struct{}{}:
		defer func() { <-a.sem }()
	default:
		writeJSON(w, http.StatusConflict, bestServerQualityResponse{
			Success: false, Available: false, Candidates: []bestServerQualityCandidate{}, Mutation: "NONE",
			Error: "VPN operation is active; Best Server scan was not started",
		})
		return
	}

	if !prepareBestServerResponse(w) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), bestServerQualityScanTimeout)
	defer cancel()
	force := r.URL.Query().Get("refresh") == "1"
	response, err := a.scanBestServerQuality(ctx, force)
	if err != nil {
		status := http.StatusServiceUnavailable
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			status = http.StatusGatewayTimeout
		}
		writeJSON(w, status, bestServerQualityResponse{
			Success: false, Available: false, Candidates: []bestServerQualityCandidate{}, Mutation: "NONE",
			Error: safeBestServerError(err),
		})
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (a *app) scanBestServerQuality(ctx context.Context, force bool) (bestServerQualityResponse, error) {
	_ = force // retained for compatibility with the legacy query parameter.
	return a.scanBestServerForeign(ctx)
}

func rankBestServerQualityCandidates(
	ctx context.Context,
	internal []bestServerInternalCandidate,
	total int,
	truncated bool,
	currentEndpoint string,
	currentFilter string,
	appProbe bestServerQualityApplicationProbe,
) bestServerQualityResponse {
	currentIndex := bestServerCurrentCandidateIndex(internal, currentEndpoint, currentFilter)
	results := make([]bestServerQualityCandidate, len(internal))
	for i, candidate := range internal {
		results[i] = bestServerQualityCandidate{
			ID: candidate.Profile.ID, Name: candidate.Profile.Name, CountryCode: candidate.Profile.CountryCode,
			Endpoint: profileEndpoint(candidate.Profile), Current: i == currentIndex,
			VPNRTTMS: candidate.VPNRTTMS, VPNJitterMS: candidate.VPNJitterMS,
			Reason: "VPN quality probe pending",
		}
	}

	partial := false
	for index := range internal {
		if ctx.Err() != nil {
			partial = true
			break
		}
		if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < bestServerQualityCandidateTimeout+2*time.Second {
			partial = true
			break
		}
		reportBestServerProgress(ctx, "quality", index, len(internal))
		candidateCtx, cancel := context.WithTimeout(ctx, bestServerQualityCandidateTimeout)
		probe := appProbe(candidateCtx, internal[index])
		cancel()
		results[index].Tested = true
		if !probe.OK {
			results[index].Reason = "VPN application probe failed; profile is not recommended"
			continue
		}

		results[index].Reachable = true
		results[index].Available = true
		results[index].DownloadIssue = probe.DownloadIssue
		results[index].MediaIssue = probe.Media.Issue
		results[index].ApplicationMS = probe.HTTP.Median
		results[index].JitterMS = probe.HTTP.Jitter
		results[index].HTTPSamples = len(probe.HTTP.Samples)
		results[index].MediaGrade = probe.Media.Grade
		results[index].MediaStalls = probe.Media.Stalls
		results[index].MediaSamples = probe.Media.Samples
		results[index].MediaMbps = roundBestServerMediaMbps(probe.Media.MedianMbps)
		results[index].ServiceOK = probe.Media.ServiceOK
		results[index].ServiceTotal = probe.Media.ServiceTotal
		if probe.DownloadOK {
			results[index].DownloadMbps = roundBestServerMbps(probe.DownloadMbps)
			results[index].ThroughputSource = bestServerThroughputStrictAggregate
		}
		results[index].Eligible = eligibleBestServerQuality(results[index])

		baseScore := bestServerQualityScore(
			probe.HTTP.Median,
			probe.HTTP.Jitter,
			probe.DownloadMbps,
			probe.DownloadOK,
		)
		results[index].Score = baseScore - probe.Media.Penalty
		if results[index].Score < 1 {
			results[index].Score = 1
		}
		stableTransfer := probe.Media.OK && (probe.Media.Grade == "excellent" || probe.Media.Grade == "good")
		if len(probe.HTTP.Samples) >= bestServerQualityHTTPRuns && probe.DownloadOK && stableTransfer &&
			probe.HTTP.Median <= bestServerQualityMaxApplicationMS && probe.HTTP.Jitter <= bestServerQualityHighJitterMS {
			results[index].Confidence = "high"
		} else {
			results[index].Confidence = "medium"
		}
		if probe.DownloadOK {
			results[index].Reason = fmt.Sprintf("VPN HTTP response median %d ms (%d samples), jitter %d ms, Speedtest aggregate capacity %.1f Mbps; transfer %s, stalls %d/%d, services %d/%d",
				probe.HTTP.Median, len(probe.HTTP.Samples), probe.HTTP.Jitter, roundBestServerMbps(probe.DownloadMbps),
				probe.Media.Grade, probe.Media.Stalls, probe.Media.Samples, probe.Media.ServiceOK, probe.Media.ServiceTotal)
		} else {
			results[index].Reason = fmt.Sprintf("VPN HTTP response median %d ms (%d samples), jitter %d ms; Speedtest throughput unavailable; transfer %s, stalls %d/%d, services %d/%d",
				probe.HTTP.Median, len(probe.HTTP.Samples), probe.HTTP.Jitter,
				probe.Media.Grade, probe.Media.Stalls, probe.Media.Samples, probe.Media.ServiceOK, probe.Media.ServiceTotal)
		}
	}

	for i := range results {
		if !results[i].Eligible {
			results[i].Rejections = bestServerRejectionReasons(results[i])
		}
	}
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].Available != results[j].Available {
			return results[i].Available
		}
		if results[i].Available && results[i].Score != results[j].Score {
			return results[i].Score > results[j].Score
		}
		if results[i].Available && results[i].MediaStalls != results[j].MediaStalls {
			return results[i].MediaStalls < results[j].MediaStalls
		}
		if results[i].Available && results[i].ApplicationMS != results[j].ApplicationMS {
			return results[i].ApplicationMS < results[j].ApplicationMS
		}
		if results[i].Available && results[i].DownloadMbps != results[j].DownloadMbps {
			return results[i].DownloadMbps > results[j].DownloadMbps
		}
		if results[i].VPNRTTMS != results[j].VPNRTTMS {
			if results[i].VPNRTTMS == 0 {
				return false
			}
			if results[j].VPNRTTMS == 0 {
				return true
			}
			return results[i].VPNRTTMS < results[j].VPNRTTMS
		}
		return results[i].ID < results[j].ID
	})

	response := bestServerQualityResponse{
		Partial: partial,
		Available: false, Candidates: results, ProfilesScanned: len(internal), ProfilesTotal: total,
		ProfilesTruncated: truncated, Mutation: "NONE",
	}
	for i := range response.Candidates {
		candidate := response.Candidates[i]
		if candidate.Eligible {
			best := candidate
			response.Recommendation = &best
			response.Available = true
			break
		}
	}
	return response
}

func defaultBestServerQualityTCPProbe(ctx context.Context, profile subscriptionProfile) bestServerProbeResult {
	dialer := net.Dialer{Timeout: bestServerQualityTCPTimeout}
	samples := make([]int, 0, bestServerQualityTCPRuns)
	for i := 0; i < bestServerQualityTCPRuns; i++ {
		if ctx.Err() != nil {
			break
		}
		started := time.Now()
		conn, err := dialer.DialContext(ctx, "tcp", profileEndpoint(profile))
		elapsed := int(time.Since(started).Milliseconds())
		if err != nil {
			continue
		}
		_ = conn.Close()
		if elapsed < 1 {
			elapsed = 1
		}
		samples = append(samples, elapsed)
	}
	return summarizeBestServerSamples(samples, bestServerQualityTCPRequired)
}

func bestServerQualityScore(httpMS, httpJitterMS int, downloadMbps float64, downloadOK bool) int {
	score := 10000
	score -= minInt(httpMS, 2500) * 2
	score -= minInt(httpJitterMS, 1000) * 3
	if downloadOK {
		score += int(math.Min(downloadMbps, 200) * 25)
		switch {
		case downloadMbps < 10:
			score -= bestServerQualityVeryLowSpeedPenalty
		case downloadMbps < 25:
			score -= bestServerQualityLowSpeedPenalty
		case downloadMbps < 50:
			score -= bestServerQualityModerateSpeedPenalty
		}
	} else {
		score -= bestServerQualityNoSpeedPenalty
	}
	if score < 1 {
		return 1
	}
	return score
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func roundBestServerMbps(value float64) float64 {
	return math.Round(value*10) / 10
}

func (a *app) probeBestServerQualityApplication(ctx context.Context, candidate bestServerInternalCandidate) bestServerQualityApplicationResult {
	outbound, err := buildBestServerProbeOutbound(candidate.Raw, candidate.Profile)
	if err != nil {
		return bestServerQualityApplicationResult{}
	}
	outbound, err = prepareIsolatedProbeOutbound(outbound)
	if err != nil {
		return bestServerQualityApplicationResult{}
	}
	releaseProbe, ok := acquireIsolatedXrayProbe(ctx)
	if !ok {
		return bestServerQualityApplicationResult{}
	}
	defer releaseProbe()
	xrayPath := strings.TrimSpace(os.Getenv("FREENET_XRAY_BIN"))
	if xrayPath == "" {
		xrayPath = defaultBestServerXrayPath
	}
	if _, err := os.Stat(xrayPath); err != nil {
		return bestServerQualityApplicationResult{}
	}
	curlPath, err := exec.LookPath("curl")
	if err != nil {
		return bestServerQualityApplicationResult{}
	}

	port, err := reserveBestServerPort()
	if err != nil {
		return bestServerQualityApplicationResult{}
	}
	tmpDir, err := os.MkdirTemp("", "freenet-best-quality-")
	if err != nil {
		return bestServerQualityApplicationResult{}
	}
	defer os.RemoveAll(tmpDir)
	_ = os.Chmod(tmpDir, 0700)
	probeXrayPath, err := isolatedXrayProbePath(tmpDir, xrayPath)
	if err != nil {
		return bestServerQualityApplicationResult{}
	}

	config := map[string]any{
		"log": map[string]any{"loglevel": "warning"},
		"inbounds": []any{map[string]any{
			"listen": "127.0.0.1", "port": port, "protocol": "socks",
			"settings": map[string]any{"udp": false}, "tag": "freenet-best-quality",
		}},
		"outbounds": []any{outbound},
		"routing": map[string]any{
			"domainStrategy": "AsIs",
			"rules": []any{map[string]any{
				"type": "field", "inboundTag": []string{"freenet-best-quality"}, "outboundTag": "vless-reality",
			}},
		},
	}
	encoded, err := json.Marshal(config)
	if err != nil {
		return bestServerQualityApplicationResult{}
	}
	configPath := filepath.Join(tmpDir, "00_probe.json")
	if err := os.WriteFile(configPath, encoded, 0600); err != nil {
		return bestServerQualityApplicationResult{}
	}

	env := isolatedXrayProbeEnv(append(os.Environ(), "XRAY_LOCATION_ASSET="+a.geoDataAssetDir()))
	testCtx, cancelTest := context.WithTimeout(ctx, 5*time.Second)
	testCmd := exec.CommandContext(testCtx, probeXrayPath, "run", "-test", "-confdir", tmpDir)
	testCmd.Env = env
	testCmd.Stdout = io.Discard
	testCmd.Stderr = io.Discard
	testCmd.WaitDelay = 2 * time.Second
	testErr := testCmd.Run()
	cancelTest()
	if testErr != nil {
		return bestServerQualityApplicationResult{}
	}

	runCtx, cancelRun := context.WithCancel(ctx)
	defer cancelRun()
	cmd := exec.CommandContext(runCtx, probeXrayPath, "run", "-confdir", tmpDir)
	cmd.Env = env
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	cmd.WaitDelay = 2 * time.Second
	if err := cmd.Start(); err != nil {
		return bestServerQualityApplicationResult{}
	}
	defer func() {
		cancelRun()
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
	}()
	if !waitBestServerSOCKS(ctx, port) {
		return bestServerQualityApplicationResult{}
	}

	socks := fmt.Sprintf("127.0.0.1:%d", port)
	for i := 0; i < bestServerQualityWarmupRuns; i++ {
		_, _, _ = probeBestServerHTTPAny(ctx, curlPath, socks)
	}

	samples := make([]int, 0, bestServerQualityHTTPRuns)
	for i := 0; i < bestServerQualityHTTPRuns; i++ {
		if ms, _, ok := probeBestServerHTTPAny(ctx, curlPath, socks); ok {
			samples = append(samples, ms)
		}
	}
	httpResult := summarizeBestServerSamples(samples, bestServerQualityHTTPRequired)
	if !httpResult.OK {
		return bestServerQualityApplicationResult{}
	}

	result := bestServerQualityApplicationResult{OK: true, HTTP: httpResult}
	result.Media = probeBestServerMediaQuality(ctx, curlPath, socks)
	if result.Media.OK && result.Media.MedianMbps > 0 {
		result.DownloadOK = true
		result.DownloadMbps = result.Media.MedianMbps
		result.ThroughputSource = bestServerThroughputStrictAggregate
	} else {
		result.DownloadIssue = result.Media.Issue
		if result.DownloadIssue == "" {
			result.DownloadIssue = "Speedtest throughput unavailable"
		}
	}
	return result
}

func eligibleBestServerQuality(c bestServerQualityCandidate) bool {
	return c.Available && c.ApplicationMS > 0 && c.ApplicationMS <= bestServerQualityMaxApplicationMS &&
		c.DownloadMbps >= 20 && c.MediaSamples >= bestServerMediaRequiredRuns && c.MediaStalls == 0 && (c.MediaGrade == "good" || c.MediaGrade == "excellent") &&
		c.ServiceTotal >= 3 && c.ServiceOK == c.ServiceTotal &&
		c.JitterMS <= bestServerQualityHighJitterMS
}
