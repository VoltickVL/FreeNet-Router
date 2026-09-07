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
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	bestServerMaxCandidates    = 64
	bestServerTCPWorkers       = 8
	bestServerTCPTimeout       = 1200 * time.Millisecond
	bestServerShortlist        = 4
	bestServerApplicationRuns  = 2
	bestServerApplicationLimit = 6 * time.Second
	bestServerScanTimeout      = 75 * time.Second
	bestServerCacheTTL         = 3 * time.Minute
	bestServerProbeURL         = "https://www.gstatic.com/generate_204"
	defaultBestServerXrayPath  = "/opt/sbin/xray"
)

type bestServerInternalCandidate struct {
	Profile subscriptionProfile
	Raw     string
}

type bestServerCandidate struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	CountryCode   string `json:"country_code,omitempty"`
	Endpoint      string `json:"endpoint"`
	Current       bool   `json:"current"`
	Reachable     bool   `json:"reachable"`
	Available     bool   `json:"available"`
	TCPRTTMS      int    `json:"tcp_rtt_ms,omitempty"`
	ApplicationMS int    `json:"application_rtt_ms,omitempty"`
	JitterMS      int    `json:"jitter_ms,omitempty"`
	Score         int    `json:"score,omitempty"`
	Confidence    string `json:"confidence,omitempty"`
	Reason        string `json:"reason"`
}

type bestServerResponse struct {
	Success          bool                  `json:"success"`
	Available        bool                  `json:"available"`
	ScannedAt        string                `json:"scanned_at,omitempty"`
	CurrentEndpoint  string                `json:"current_endpoint,omitempty"`
	Recommendation   *bestServerCandidate  `json:"recommendation,omitempty"`
	Candidates       []bestServerCandidate `json:"candidates"`
	ProfilesScanned  int                   `json:"profiles_scanned"`
	ProfilesTotal    int                   `json:"profiles_total"`
	ProfilesTruncated bool                 `json:"profiles_truncated,omitempty"`
	Mutation         string                `json:"mutation"`
	Message          string                `json:"message,omitempty"`
	Error            string                `json:"error,omitempty"`
}

type bestServerProbeResult struct {
	OK      bool
	Samples []int
	Median  int
	Jitter  int
}

type bestServerTCPProbe func(context.Context, subscriptionProfile) bestServerProbeResult
type bestServerApplicationProbe func(context.Context, bestServerInternalCandidate) bestServerProbeResult

type bestServerCacheEntry struct {
	Key      string
	StoredAt time.Time
	Response bestServerResponse
}

var bestServerCache struct {
	sync.Mutex
	Entry bestServerCacheEntry
}

func registerBestServerAPI(mux *http.ServeMux, a *app) {
	mux.HandleFunc("GET /api/vpn/best", a.requireAuth(a.handleBestServer))
}

func (a *app) handleBestServer(w http.ResponseWriter, r *http.Request) {
	if len(a.sem) > 0 {
		writeJSON(w, http.StatusConflict, bestServerResponse{
			Success: false, Available: false, Candidates: []bestServerCandidate{}, Mutation: "NONE",
			Error: "VPN operation is active; Best Server scan was not started",
		})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), bestServerScanTimeout)
	defer cancel()
	force := r.URL.Query().Get("refresh") == "1"
	response, err := a.scanBestServer(ctx, force)
	if err != nil {
		status := http.StatusServiceUnavailable
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			status = http.StatusGatewayTimeout
		}
		writeJSON(w, status, bestServerResponse{
			Success: false, Available: false, Candidates: []bestServerCandidate{}, Mutation: "NONE",
			Error: safeBestServerError(err),
		})
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func safeBestServerError(err error) string {
	if err == nil {
		return ""
	}
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "Best Server scan timed out"
	case errors.Is(err, context.Canceled):
		return "Best Server scan was canceled"
	default:
		return "Best Server recommendation is unavailable"
	}
}

func (a *app) scanBestServer(ctx context.Context, force bool) (bestServerResponse, error) {
	currentEndpoint := readBestServerCurrentEndpoint(a.cfg.OutPath)
	cacheKey := a.bestServerCacheKey(currentEndpoint)
	if !force {
		bestServerCache.Lock()
		entry := bestServerCache.Entry
		bestServerCache.Unlock()
		if entry.Key == cacheKey && !entry.StoredAt.IsZero() && time.Since(entry.StoredAt) < bestServerCacheTTL {
			return cloneBestServerResponse(entry.Response), nil
		}
	}

	all, total, truncated, err := a.discoverBestServerCandidates(ctx)
	if err != nil {
		return bestServerResponse{}, err
	}
	response := rankBestServerCandidates(ctx, all, total, truncated, currentEndpoint, defaultBestServerTCPProbe, a.probeBestServerApplication)
	if ctx.Err() != nil {
		return bestServerResponse{}, ctx.Err()
	}
	response.Success = true
	response.Mutation = "NONE"
	response.ScannedAt = time.Now().UTC().Format(time.RFC3339)
	response.CurrentEndpoint = currentEndpoint
	if response.Available && response.Recommendation != nil {
		if response.Recommendation.Current {
			response.Message = "Текущий VPN уже лучший из проверенных профилей."
		} else {
			response.Message = "FreeNet нашёл лучший доступный VPN-профиль."
		}
	} else {
		response.Message = "Достоверная рекомендация сейчас недоступна; текущий VPN не изменён."
	}

	if after := readBestServerCurrentEndpoint(a.cfg.OutPath); after != currentEndpoint {
		return bestServerResponse{}, errors.New("VPN endpoint changed during Best Server scan")
	}

	bestServerCache.Lock()
	bestServerCache.Entry = bestServerCacheEntry{Key: cacheKey, StoredAt: time.Now(), Response: cloneBestServerResponse(response)}
	bestServerCache.Unlock()
	return response, nil
}

func cloneBestServerResponse(in bestServerResponse) bestServerResponse {
	out := in
	out.Candidates = append([]bestServerCandidate(nil), in.Candidates...)
	if in.Recommendation != nil {
		copyValue := *in.Recommendation
		out.Recommendation = &copyValue
	}
	return out
}

func (a *app) bestServerCacheKey(currentEndpoint string) string {
	info, err := os.Stat(a.cfg.SubPath)
	if err != nil {
		return currentEndpoint + "|subscription-unavailable"
	}
	return fmt.Sprintf("%s|%d|%d", currentEndpoint, info.Size(), info.ModTime().UnixNano())
}

func (a *app) discoverBestServerCandidates(ctx context.Context) ([]bestServerInternalCandidate, int, bool, error) {
	rawURL, err := os.ReadFile(a.cfg.SubPath)
	if err != nil {
		return nil, 0, false, errors.New("subscription is not configured")
	}
	secretURL := strings.TrimSpace(string(rawURL))
	if err := validateSubscriptionURL(secretURL); err != nil {
		return nil, 0, false, errors.New("stored subscription URL is invalid")
	}
	u, err := url.Parse(secretURL)
	if err != nil {
		return nil, 0, false, errors.New("stored subscription URL is invalid")
	}
	body, err := fetchSubscriptionBody(ctx, u)
	if err != nil {
		return nil, 0, false, errors.New("subscription fetch failed")
	}
	return parseBestServerCandidates(body)
}

func parseBestServerCandidates(body []byte) ([]bestServerInternalCandidate, int, bool, error) {
	text := strings.ReplaceAll(string(body), "\r", "")
	if !strings.Contains(strings.ToLower(text), "vless://") {
		decoded, err := decodeSubscriptionBase64(text)
		if err != nil {
			return nil, 0, false, errors.New("subscription format is unsupported")
		}
		text = strings.ReplaceAll(string(decoded), "\r", "")
	}

	all := make([]bestServerInternalCandidate, 0, bestServerMaxCandidates)
	seen := map[string]bool{}
	total := 0
	truncated := false
	for _, rawLine := range strings.Split(text, "\n") {
		line := strings.TrimSpace(rawLine)
		lower := strings.ToLower(line)
		if !strings.HasPrefix(lower, "vless://") || !strings.Contains(lower, "extra") || strings.Contains(lower, "expired") {
			continue
		}
		profile, ok := parseSafeVLESSProfile(line)
		if !ok || seen[profile.ID] {
			continue
		}
		seen[profile.ID] = true
		total++
		if len(all) >= bestServerMaxCandidates {
			truncated = true
			continue
		}
		all = append(all, bestServerInternalCandidate{Profile: profile, Raw: line})
	}
	if total == 0 || len(all) == 0 {
		return nil, 0, false, errors.New("no active Extra profiles found")
	}
	return all, total, truncated, nil
}

func rankBestServerCandidates(
	ctx context.Context,
	internal []bestServerInternalCandidate,
	total int,
	truncated bool,
	currentEndpoint string,
	tcpProbe bestServerTCPProbe,
	appProbe bestServerApplicationProbe,
) bestServerResponse {
	results := make([]bestServerCandidate, len(internal))
	for i, candidate := range internal {
		results[i] = bestServerCandidate{
			ID: candidate.Profile.ID, Name: candidate.Profile.Name, CountryCode: candidate.Profile.CountryCode,
			Endpoint: profileEndpoint(candidate.Profile), Current: endpointsEqual(profileEndpoint(candidate.Profile), currentEndpoint),
			Reason: "endpoint has not been verified",
		}
	}

	type tcpJob struct{ index int }
	jobs := make(chan tcpJob)
	var wg sync.WaitGroup
	workers := bestServerTCPWorkers
	if workers > len(internal) {
		workers = len(internal)
	}
	for worker := 0; worker < workers; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobs {
				if ctx.Err() != nil {
					continue
				}
				probe := tcpProbe(ctx, internal[job.index].Profile)
				if probe.OK {
					results[job.index].Reachable = true
					results[job.index].TCPRTTMS = probe.Median
					results[job.index].Reason = "endpoint TCP reachable; application probe pending"
				} else {
					results[job.index].Reason = "endpoint TCP probe failed"
				}
			}
		}()
	}
	for i := range internal {
		if ctx.Err() != nil {
			break
		}
		jobs <- tcpJob{index: i}
	}
	close(jobs)
	wg.Wait()

	shortlist := make([]int, 0, bestServerShortlist+1)
	for i := range results {
		if results[i].Reachable {
			shortlist = append(shortlist, i)
		}
	}
	sort.Slice(shortlist, func(i, j int) bool {
		a, b := results[shortlist[i]], results[shortlist[j]]
		if a.TCPRTTMS != b.TCPRTTMS {
			return a.TCPRTTMS < b.TCPRTTMS
		}
		return a.ID < b.ID
	})
	if len(shortlist) > bestServerShortlist {
		shortlist = shortlist[:bestServerShortlist]
	}
	currentIndex := -1
	for i := range results {
		if results[i].Current && results[i].Reachable {
			currentIndex = i
			break
		}
	}
	if currentIndex >= 0 && !containsBestServerIndex(shortlist, currentIndex) {
		shortlist = append(shortlist, currentIndex)
	}

	for _, index := range shortlist {
		if ctx.Err() != nil {
			break
		}
		probe := appProbe(ctx, internal[index])
		if !probe.OK {
			results[index].Reason = "VPN application probe failed; profile is not recommended"
			continue
		}
		results[index].Available = true
		results[index].ApplicationMS = probe.Median
		results[index].JitterMS = probe.Jitter
		results[index].Score = bestServerScore(probe.Median, results[index].TCPRTTMS, probe.Jitter)
		if probe.Jitter <= 100 {
			results[index].Confidence = "high"
		} else {
			results[index].Confidence = "medium"
		}
		results[index].Reason = fmt.Sprintf("VPN application probe passed twice; median %d ms, jitter %d ms", probe.Median, probe.Jitter)
	}

	sort.SliceStable(results, func(i, j int) bool {
		if results[i].Available != results[j].Available {
			return results[i].Available
		}
		if results[i].Available && results[i].Score != results[j].Score {
			return results[i].Score > results[j].Score
		}
		if results[i].Available && results[i].ApplicationMS != results[j].ApplicationMS {
			return results[i].ApplicationMS < results[j].ApplicationMS
		}
		if results[i].Reachable != results[j].Reachable {
			return results[i].Reachable
		}
		if results[i].TCPRTTMS != results[j].TCPRTTMS {
			return results[i].TCPRTTMS < results[j].TCPRTTMS
		}
		return results[i].ID < results[j].ID
	})

	response := bestServerResponse{
		Available: false, Candidates: results, ProfilesScanned: len(internal), ProfilesTotal: total,
		ProfilesTruncated: truncated, Mutation: "NONE",
	}
	for i := range response.Candidates {
		if response.Candidates[i].Available {
			best := response.Candidates[i]
			response.Recommendation = &best
			response.Available = true
			break
		}
	}
	return response
}

func containsBestServerIndex(values []int, target int) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func bestServerScore(appMS, tcpMS, jitterMS int) int {
	score := 2000 - appMS - tcpMS/4 - jitterMS/2
	if score < 1 {
		return 1
	}
	return score
}

func endpointsEqual(a, b string) bool {
	return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b)) && strings.TrimSpace(a) != ""
}

func defaultBestServerTCPProbe(ctx context.Context, profile subscriptionProfile) bestServerProbeResult {
	dialer := net.Dialer{Timeout: bestServerTCPTimeout}
	samples := make([]int, 0, 1)
	started := time.Now()
	conn, err := dialer.DialContext(ctx, "tcp", profileEndpoint(profile))
	elapsed := int(time.Since(started).Milliseconds())
	if err == nil {
		_ = conn.Close()
		if elapsed < 1 {
			elapsed = 1
		}
		samples = append(samples, elapsed)
	}
	return summarizeBestServerSamples(samples, 1)
}

func summarizeBestServerSamples(samples []int, required int) bestServerProbeResult {
	clean := make([]int, 0, len(samples))
	for _, sample := range samples {
		if sample > 0 {
			clean = append(clean, sample)
		}
	}
	if len(clean) < required {
		return bestServerProbeResult{OK: false, Samples: clean}
	}
	sort.Ints(clean)
	median := clean[len(clean)/2]
	if len(clean)%2 == 0 {
		median = (clean[len(clean)/2-1] + clean[len(clean)/2]) / 2
	}
	jitter := 0
	if len(clean) > 1 {
		jitter = clean[len(clean)-1] - clean[0]
	}
	return bestServerProbeResult{OK: true, Samples: clean, Median: median, Jitter: jitter}
}

func readBestServerCurrentEndpoint(outPath string) string {
	data, err := os.ReadFile(outPath)
	if err != nil {
		return ""
	}
	var cfg xrayConfig
	if json.Unmarshal(data, &cfg) != nil {
		return ""
	}
	for _, outbound := range cfg.Outbounds {
		if outbound.Tag != "vless-reality" || len(outbound.Settings.VNext) == 0 {
			continue
		}
		vnext := outbound.Settings.VNext[0]
		if vnext.Address == "" || vnext.Port <= 0 {
			continue
		}
		return net.JoinHostPort(vnext.Address, strconv.Itoa(vnext.Port))
	}
	return ""
}

func (a *app) probeBestServerApplication(ctx context.Context, candidate bestServerInternalCandidate) bestServerProbeResult {
	outbound, err := buildBestServerProbeOutbound(candidate.Raw, candidate.Profile)
	if err != nil {
		return bestServerProbeResult{}
	}
	xrayPath := strings.TrimSpace(os.Getenv("FREENET_XRAY_BIN"))
	if xrayPath == "" {
		xrayPath = defaultBestServerXrayPath
	}
	if _, err := os.Stat(xrayPath); err != nil {
		return bestServerProbeResult{}
	}
	curlPath, err := exec.LookPath("curl")
	if err != nil {
		return bestServerProbeResult{}
	}

	port, err := reserveBestServerPort()
	if err != nil {
		return bestServerProbeResult{}
	}
	tmpDir, err := os.MkdirTemp("", "freenet-best-server-")
	if err != nil {
		return bestServerProbeResult{}
	}
	defer os.RemoveAll(tmpDir)
	_ = os.Chmod(tmpDir, 0700)

	config := map[string]any{
		"log": map[string]any{"loglevel": "warning"},
		"inbounds": []any{map[string]any{
			"listen": "127.0.0.1", "port": port, "protocol": "socks",
			"settings": map[string]any{"udp": false}, "tag": "freenet-best-probe",
		}},
		"outbounds": []any{outbound},
		"routing": map[string]any{
			"domainStrategy": "AsIs",
			"rules": []any{map[string]any{
				"type": "field", "inboundTag": []string{"freenet-best-probe"}, "outboundTag": "vless-reality",
			}},
		},
	}
	encoded, err := json.Marshal(config)
	if err != nil {
		return bestServerProbeResult{}
	}
	configPath := filepath.Join(tmpDir, "00_probe.json")
	if err := os.WriteFile(configPath, encoded, 0600); err != nil {
		return bestServerProbeResult{}
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
		return bestServerProbeResult{}
	}

	runCtx, cancelRun := context.WithCancel(ctx)
	defer cancelRun()
	cmd := exec.CommandContext(runCtx, xrayPath, "run", "-confdir", tmpDir)
	cmd.Env = env
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return bestServerProbeResult{}
	}
	defer func() {
		cancelRun()
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
	}()
	if !waitBestServerSOCKS(ctx, port) {
		return bestServerProbeResult{}
	}

	samples := make([]int, 0, bestServerApplicationRuns)
	for i := 0; i < bestServerApplicationRuns; i++ {
		probeCtx, cancel := context.WithTimeout(ctx, bestServerApplicationLimit)
		output, err := exec.CommandContext(probeCtx, curlPath,
			"--socks5-hostname", fmt.Sprintf("127.0.0.1:%d", port),
			"-sS", "--connect-timeout", "3", "--max-time", "6",
			"-o", "/dev/null", "-w", "%{http_code}\t%{time_total}", bestServerProbeURL,
		).Output()
		cancel()
		if err != nil {
			continue
		}
		fields := strings.Fields(string(output))
		if len(fields) != 2 || len(fields[0]) != 3 || fields[0][0] < '2' || fields[0][0] > '4' {
			continue
		}
		seconds, err := strconv.ParseFloat(fields[1], 64)
		if err != nil || seconds <= 0 {
			continue
		}
		ms := int(math.Round(seconds * 1000))
		if ms < 1 {
			ms = 1
		}
		samples = append(samples, ms)
	}
	return summarizeBestServerSamples(samples, bestServerApplicationRuns)
}

func buildBestServerProbeOutbound(raw string, profile subscriptionProfile) (map[string]any, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || !strings.EqualFold(u.Scheme, "vless") || u.User == nil {
		return nil, errors.New("invalid VLESS profile")
	}
	uuid := u.User.Username()
	query := u.Query()
	security := strings.TrimSpace(query.Get("security"))
	if security == "" {
		security = "reality"
	}
	if !strings.EqualFold(security, "reality") {
		return nil, errors.New("unsupported VLESS security")
	}
	flow := strings.TrimSpace(query.Get("flow"))
	if flow == "" {
		flow = "xtls-rprx-vision"
	}
	network := strings.TrimSpace(query.Get("type"))
	if network == "" {
		network = "tcp"
	}
	fingerprint := strings.TrimSpace(query.Get("fp"))
	if fingerprint == "" {
		fingerprint = "firefox"
	}
	serverName := strings.TrimSpace(query.Get("sni"))
	publicKey := strings.TrimSpace(query.Get("pbk"))
	shortID := strings.TrimSpace(query.Get("sid"))
	spiderX := strings.TrimSpace(query.Get("spx"))
	if spiderX == "" {
		spiderX = "/"
	}
	if uuid == "" || profile.Address == "" || profile.Port <= 0 || serverName == "" || publicKey == "" || shortID == "" {
		return nil, errors.New("incomplete VLESS Reality profile")
	}
	return map[string]any{
		"tag": "vless-reality",
		"protocol": "vless",
		"settings": map[string]any{"vnext": []any{map[string]any{
			"address": profile.Address, "port": profile.Port,
			"users": []any{map[string]any{"id": uuid, "flow": flow, "encryption": "none", "level": 0}},
		}}},
		"streamSettings": map[string]any{
			"network": network, "security": "reality",
			"realitySettings": map[string]any{
				"fingerprint": fingerprint, "serverName": serverName, "publicKey": publicKey,
				"shortId": shortID, "spiderX": spiderX,
			},
		},
	}, nil
}

func reserveBestServerPort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer listener.Close()
	address, ok := listener.Addr().(*net.TCPAddr)
	if !ok || address.Port <= 0 {
		return 0, errors.New("cannot reserve local probe port")
	}
	return address.Port, nil
}

func waitBestServerSOCKS(ctx context.Context, port int) bool {
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	address := fmt.Sprintf("127.0.0.1:%d", port)
	for {
		select {
		case <-ctx.Done():
			return false
		case <-deadline.C:
			return false
		case <-ticker.C:
			conn, err := net.DialTimeout("tcp", address, 150*time.Millisecond)
			if err == nil {
				_ = conn.Close()
				return true
			}
		}
	}
}
