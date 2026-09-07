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
	"sync"
	"time"
)

const (
	bestServerQualityTCPRuns                   = 3
	bestServerQualityTCPRequired               = 2
	bestServerQualityTCPWorkers                = 8
	bestServerQualityTCPTimeout                = 1200 * time.Millisecond
	bestServerQualityShortlist                 = 6
	bestServerQualityWarmupRuns                = 1
	bestServerQualityHTTPRuns                  = 3
	bestServerQualityHTTPRequired              = 2
	bestServerQualityHTTPTimeout               = 5 * time.Second
	bestServerQualityDownloadTimeout           = 10 * time.Second
	bestServerQualityCandidateTimeout          = 30 * time.Second
	bestServerQualityScanTimeout               = 150 * time.Second
	bestServerQualityCacheTTL                  = 3 * time.Minute
	bestServerQualityProbeURL                  = "https://www.gstatic.com/generate_204"
	bestServerQualityDownloadURL               = "https://speed.cloudflare.com/__down?bytes=16777216"
	bestServerQualityDownloadBytes             = 16777216
	bestServerQualityDownloadMinimumBytes      = bestServerQualityDownloadBytes / 8
	bestServerQualityNoSpeedPenalty            = 3000
	bestServerQualityVeryLowSpeedPenalty       = 3200
	bestServerQualityLowSpeedPenalty           = 1600
	bestServerQualityModerateSpeedPenalty      = 500
	bestServerQualityHighJitterMS              = 80
	bestServerQualityHighTCPJitterMS           = 60
)

type bestServerQualityCandidate struct {
	ID           string  `json:"id"`
	Name         string  `json:"name"`
	CountryCode  string  `json:"country_code,omitempty"`
	Endpoint     string  `json:"endpoint"`
	Current      bool    `json:"current"`
	Reachable    bool    `json:"reachable"`
	Available    bool    `json:"available"`
	TCPRTTMS     int     `json:"tcp_rtt_ms,omitempty"`
	TCPJitterMS  int     `json:"tcp_jitter_ms,omitempty"`
	ApplicationMS int    `json:"application_rtt_ms,omitempty"`
	JitterMS     int     `json:"jitter_ms,omitempty"`
	DownloadMbps float64 `json:"download_mbps,omitempty"`
	HTTPSamples  int     `json:"http_samples,omitempty"`
	MediaGrade   string  `json:"media_grade,omitempty"`
	MediaStalls  int     `json:"media_stalls,omitempty"`
	MediaSamples int     `json:"media_samples,omitempty"`
	MediaMbps    float64 `json:"media_mbps,omitempty"`
	ServiceOK    int     `json:"service_ok,omitempty"`
	ServiceTotal int     `json:"service_total,omitempty"`
	Score        int     `json:"score,omitempty"`
	Confidence   string  `json:"confidence,omitempty"`
	Reason       string  `json:"reason"`
}

type bestServerQualityResponse struct {
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
	OK           bool
	HTTP         bestServerProbeResult
	DownloadOK   bool
	DownloadMbps float64
	Media        bestServerMediaQualityResult
}

type bestServerQualityApplicationProbe func(context.Context, bestServerInternalCandidate) bestServerQualityApplicationResult

type bestServerQualityCacheEntry struct {
	Key      string
	StoredAt time.Time
	Response bestServerQualityResponse
}

var bestServerQualityCache struct {
	sync.Mutex
	Entry bestServerQualityCacheEntry
}

func registerBestServerQualityAPI(mux *http.ServeMux, a *app) {
	mux.HandleFunc("GET /api/vpn/best", a.requireAuth(a.handleBestServerQuality))
}

func (a *app) handleBestServerQuality(w http.ResponseWriter, r *http.Request) {
	if len(a.sem) > 0 {
		writeJSON(w, http.StatusConflict, bestServerQualityResponse{
			Success: false, Available: false, Candidates: []bestServerQualityCandidate{}, Mutation: "NONE",
			Error: "VPN operation is active; Best Server scan was not started",
		})
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
	currentEndpoint := readBestServerCurrentEndpoint(a.cfg.OutPath)
	currentFilter := readBestServerCurrentFilter(a.cfg.FilterPath)
	cacheKey := "quality-v5|" + a.bestServerCacheKey(currentEndpoint)
	if !force {
		bestServerQualityCache.Lock()
		entry := bestServerQualityCache.Entry
		bestServerQualityCache.Unlock()
		if entry.Key == cacheKey && !entry.StoredAt.IsZero() && time.Since(entry.StoredAt) < bestServerQualityCacheTTL {
			return cloneBestServerQualityResponse(entry.Response), nil
		}
	}

	all, total, truncated, err := a.discoverBestServerCandidates(ctx)
	if err != nil {
		return bestServerQualityResponse{}, err
	}
	response := rankBestServerQualityCandidates(ctx, all, total, truncated, currentEndpoint, currentFilter, defaultBestServerQualityTCPProbe, a.probeBestServerQualityApplication)
	if ctx.Err() != nil {
		return bestServerQualityResponse{}, ctx.Err()
	}
	response.Success = true
	response.Mutation = "NONE"
	response.ScannedAt = time.Now().UTC().Format(time.RFC3339)
	response.CurrentEndpoint = currentEndpoint
	if response.Available && response.Recommendation != nil {
		if response.Recommendation.Current {
			response.Message = "Текущий VPN имеет лучший подтверждённый баланс скорости, отклика и плавности медиа."
		} else {
			response.Message = "FreeNet нашёл профиль с лучшим подтверждённым балансом скорости, отклика и плавности медиа."
		}
	} else {
		response.Message = "Недостаточно подтверждённых данных о скорости и стабильности; рекомендация не готова, текущий VPN не изменён."
	}

	if after := readBestServerCurrentEndpoint(a.cfg.OutPath); after != currentEndpoint {
		return bestServerQualityResponse{}, errors.New("VPN endpoint changed during Best Server scan")
	}
	if afterFilter := readBestServerCurrentFilter(a.cfg.FilterPath); afterFilter != currentFilter {
		return bestServerQualityResponse{}, errors.New("VPN profile identity changed during Best Server scan")
	}

	bestServerQualityCache.Lock()
	bestServerQualityCache.Entry = bestServerQualityCacheEntry{Key: cacheKey, StoredAt: time.Now(), Response: cloneBestServerQualityResponse(response)}
	bestServerQualityCache.Unlock()
	return response, nil
}

func cloneBestServerQualityResponse(in bestServerQualityResponse) bestServerQualityResponse {
	out := in
	out.Candidates = append([]bestServerQualityCandidate(nil), in.Candidates...)
	if in.Recommendation != nil {
		copyValue := *in.Recommendation
		out.Recommendation = &copyValue
	}
	return out
}

func rankBestServerQualityCandidates(
	ctx context.Context,
	internal []bestServerInternalCandidate,
	total int,
	truncated bool,
	currentEndpoint string,
	currentFilter string,
	tcpProbe bestServerTCPProbe,
	appProbe bestServerQualityApplicationProbe,
) bestServerQualityResponse {
	currentIndex := bestServerCurrentCandidateIndex(internal, currentEndpoint, currentFilter)
	results := make([]bestServerQualityCandidate, len(internal))
	for i, candidate := range internal {
		results[i] = bestServerQualityCandidate{
			ID: candidate.Profile.ID, Name: candidate.Profile.Name, CountryCode: candidate.Profile.CountryCode,
			Endpoint: profileEndpoint(candidate.Profile), Current: i == currentIndex,
			Reason: "endpoint has not been verified",
		}
	}

	endpointIndexes := make(map[string][]int)
	endpointOrder := make([]string, 0, len(internal))
	for i := range internal {
		endpoint := profileEndpoint(internal[i].Profile)
		if _, ok := endpointIndexes[endpoint]; !ok {
			endpointOrder = append(endpointOrder, endpoint)
		}
		endpointIndexes[endpoint] = append(endpointIndexes[endpoint], i)
	}

	jobs := make(chan string)
	var wg sync.WaitGroup
	workers := bestServerQualityTCPWorkers
	if workers > len(endpointOrder) {
		workers = len(endpointOrder)
	}
	for worker := 0; worker < workers; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for endpoint := range jobs {
				if ctx.Err() != nil {
					continue
				}
				indexes := endpointIndexes[endpoint]
				if len(indexes) == 0 {
					continue
				}
				probe := tcpProbe(ctx, internal[indexes[0]].Profile)
				for _, index := range indexes {
					if probe.OK {
						results[index].Reachable = true
						results[index].TCPRTTMS = probe.Median
						results[index].TCPJitterMS = probe.Jitter
						results[index].Reason = "endpoint TCP verified; VPN quality probe pending"
					} else {
						results[index].Reason = "endpoint TCP probe failed"
					}
				}
			}
		}()
	}
	for _, endpoint := range endpointOrder {
		if ctx.Err() != nil {
			break
		}
		jobs <- endpoint
	}
	close(jobs)
	wg.Wait()

	reachable := make([]int, 0, len(results))
	for i := range results {
		if results[i].Reachable {
			reachable = append(reachable, i)
		}
	}
	sort.Slice(reachable, func(i, j int) bool {
		a, b := results[reachable[i]], results[reachable[j]]
		if a.TCPRTTMS != b.TCPRTTMS {
			return a.TCPRTTMS < b.TCPRTTMS
		}
		if a.TCPJitterMS != b.TCPJitterMS {
			return a.TCPJitterMS < b.TCPJitterMS
		}
		return a.ID < b.ID
	})

	shortlist := make([]int, 0, bestServerQualityShortlist+1)
	seenEndpoint := make(map[string]bool)
	if currentIndex >= 0 && results[currentIndex].Reachable {
		shortlist = append(shortlist, currentIndex)
		seenEndpoint[results[currentIndex].Endpoint] = true
	}
	for _, index := range reachable {
		if len(shortlist) >= bestServerQualityShortlist {
			break
		}
		endpoint := results[index].Endpoint
		if seenEndpoint[endpoint] {
			continue
		}
		shortlist = append(shortlist, index)
		seenEndpoint[endpoint] = true
	}

	for _, index := range shortlist {
		if ctx.Err() != nil {
			break
		}
		candidateCtx, cancel := context.WithTimeout(ctx, bestServerQualityCandidateTimeout)
		probe := appProbe(candidateCtx, internal[index])
		cancel()
		if !probe.OK {
			results[index].Reason = "VPN application probe failed; profile is not recommended"
			continue
		}
		results[index].Available = true
		results[index].ApplicationMS = probe.HTTP.Median
		results[index].JitterMS = probe.HTTP.Jitter
		results[index].HTTPSamples = len(probe.HTTP.Samples)
		results[index].MediaGrade = probe.Media.Grade
		results[index].MediaStalls = probe.Media.Stalls
		results[index].MediaSamples = probe.Media.Samples
		results[index].MediaMbps = roundBestServerMediaMbps(probe.Media.MedianMbps)
		results[index].ServiceOK = probe.Media.ServiceOK
		results[index].ServiceTotal = probe.Media.ServiceTotal

		// A stable set of short body-phase media samples is a valid bounded
		// throughput fallback when the larger capacity transfer hits its deadline.
		// This prevents a timeout from erasing all speed evidence while still
		// requiring enough real payload samples before a recommendation is allowed.
		if !probe.DownloadOK && probe.Media.OK && probe.Media.MedianMbps > 0 {
			probe.DownloadOK = true
			probe.DownloadMbps = probe.Media.MedianMbps
		}
		if probe.DownloadOK {
			results[index].DownloadMbps = roundBestServerMbps(probe.DownloadMbps)
		}

		baseScore := bestServerQualityScore(
			probe.HTTP.Median,
			results[index].TCPRTTMS,
			probe.HTTP.Jitter,
			results[index].TCPJitterMS,
			probe.DownloadMbps,
			probe.DownloadOK,
		)
		results[index].Score = baseScore - probe.Media.Penalty
		if results[index].Score < 1 {
			results[index].Score = 1
		}
		mediaStable := probe.Media.OK && (probe.Media.Grade == "excellent" || probe.Media.Grade == "good")
		if len(probe.HTTP.Samples) >= bestServerQualityHTTPRuns && probe.DownloadOK && mediaStable && probe.HTTP.Jitter <= bestServerQualityHighJitterMS && results[index].TCPJitterMS <= bestServerQualityHighTCPJitterMS {
			results[index].Confidence = "high"
		} else {
			results[index].Confidence = "medium"
		}
		if probe.DownloadOK {
			results[index].Reason = fmt.Sprintf("VPN HTTP response median %d ms (%d samples), jitter %d ms, sustained body download %.1f Mbps; media %s, stalls %d/%d, services %d/%d",
				probe.HTTP.Median, len(probe.HTTP.Samples), probe.HTTP.Jitter, roundBestServerMbps(probe.DownloadMbps),
				probe.Media.Grade, probe.Media.Stalls, probe.Media.Samples, probe.Media.ServiceOK, probe.Media.ServiceTotal)
		} else {
			results[index].Reason = fmt.Sprintf("VPN HTTP response median %d ms (%d samples), jitter %d ms; sustained download unavailable; media %s, stalls %d/%d, services %d/%d",
				probe.HTTP.Median, len(probe.HTTP.Samples), probe.HTTP.Jitter,
				probe.Media.Grade, probe.Media.Stalls, probe.Media.Samples, probe.Media.ServiceOK, probe.Media.ServiceTotal)
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
		if results[i].Reachable != results[j].Reachable {
			return results[i].Reachable
		}
		if results[i].TCPRTTMS != results[j].TCPRTTMS {
			return results[i].TCPRTTMS < results[j].TCPRTTMS
		}
		return results[i].ID < results[j].ID
	})

	response := bestServerQualityResponse{
		Available: false, Candidates: results, ProfilesScanned: len(internal), ProfilesTotal: total,
		ProfilesTruncated: truncated, Mutation: "NONE",
	}
	for i := range response.Candidates {
		candidate := response.Candidates[i]
		// v0.2.85 runtime proved that latency-only ranking is not sufficient:
		// three identical scans had no throughput for either profile yet still
		// declared a winner. Recommendation is now fail-closed until speed and a
		// minimum media sample set are both confirmed.
		if candidate.Available && candidate.DownloadMbps > 0 && candidate.MediaSamples >= 4 {
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

func bestServerQualityScore(httpMS, tcpMS, httpJitterMS, tcpJitterMS int, downloadMbps float64, downloadOK bool) int {
	score := 10000
	score -= minInt(httpMS, 2500) * 2
	score -= minInt(tcpMS, 1000) * 2
	score -= minInt(httpJitterMS, 1000) * 3
	score -= minInt(tcpJitterMS, 500)
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

	env := append(os.Environ(), "XRAY_LOCATION_ASSET="+a.geoDataAssetDir())
	testCtx, cancelTest := context.WithTimeout(ctx, 5*time.Second)
	testCmd := exec.CommandContext(testCtx, xrayPath, "run", "-test", "-confdir", tmpDir)
	testCmd.Env = env
	testCmd.Stdout = io.Discard
	testCmd.Stderr = io.Discard
	testErr := testCmd.Run()
	cancelTest()
	if testErr != nil {
		return bestServerQualityApplicationResult{}
	}

	runCtx, cancelRun := context.WithCancel(ctx)
	defer cancelRun()
	cmd := exec.CommandContext(runCtx, xrayPath, "run", "-confdir", tmpDir)
	cmd.Env = env
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
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
		warmCtx, cancel := context.WithTimeout(ctx, bestServerQualityHTTPTimeout)
		_ = exec.CommandContext(warmCtx, curlPath,
			"--socks5-hostname", socks,
			"-sS", "--connect-timeout", "3", "--max-time", "5",
			"-o", "/dev/null", bestServerQualityProbeURL,
		).Run()
		cancel()
	}

	samples := make([]int, 0, bestServerQualityHTTPRuns)
	for i := 0; i < bestServerQualityHTTPRuns; i++ {
		probeCtx, cancel := context.WithTimeout(ctx, bestServerQualityHTTPTimeout)
		output, err := exec.CommandContext(probeCtx, curlPath,
			"--socks5-hostname", socks,
			"-sS", "--connect-timeout", "3", "--max-time", "5",
			"-o", "/dev/null", "-w", "%{http_code}\t%{time_pretransfer}\t%{time_starttransfer}", bestServerQualityProbeURL,
		).Output()
		cancel()
		if err != nil {
			continue
		}
		ms, ok := parseBestServerHTTPResponseMS(string(output))
		if !ok {
			continue
		}
		samples = append(samples, ms)
	}
	httpResult := summarizeBestServerSamples(samples, bestServerQualityHTTPRequired)
	if !httpResult.OK {
		return bestServerQualityApplicationResult{}
	}

	result := bestServerQualityApplicationResult{OK: true, HTTP: httpResult}
	downloadCtx, cancelDownload := context.WithTimeout(ctx, bestServerQualityDownloadTimeout)
	output, _ := exec.CommandContext(downloadCtx, curlPath,
		"--socks5-hostname", socks,
		"-sS", "--connect-timeout", "3", "--max-time", "10",
		"-o", "/dev/null", "-w", "%{http_code}\t%{size_download}\t%{time_starttransfer}\t%{time_total}", bestServerQualityDownloadURL,
	).Output()
	cancelDownload()
	if mbps, ok := parseBestServerDownloadMbpsAtLeast(string(output), bestServerQualityDownloadMinimumBytes); ok {
		result.DownloadMbps = mbps
		result.DownloadOK = true
	}
	result.Media = probeBestServerMediaQuality(ctx, curlPath, socks)
	return result
}
