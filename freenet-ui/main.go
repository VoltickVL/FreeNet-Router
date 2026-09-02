package main

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const version = "0.2.1"

const (
	defaultListen     = "192.168.50.1:1001"
	defaultVPNPath    = "/opt/bin/vpn"
	defaultFilterPath = "/opt/etc/xray/blanc_profile_filter.regex"
	defaultOutPath    = "/opt/etc/xray/configs/04_outbounds.json"
	defaultXKeenPath  = "/opt/sbin/xkeen"
	defaultLockPath   = "/tmp/blanc_xkeen_update.lock"
	defaultConfigPath = "/opt/etc/freenet/freenet.conf"
	defaultSubPath    = "/opt/etc/xray/blanc_subscription.url"
)

//go:embed web/index.html
var webFS embed.FS

type config struct {
	Listen     string
	VPNPath    string
	FilterPath string
	OutPath    string
	XKeenPath  string
	LockPath   string
	ConfigPath string
	SubPath    string
	Timeout    time.Duration
}

type app struct {
	cfg  config
	sem  chan struct{}
	mu   sync.RWMutex
	last actionResult
}

type statusResponse struct {
	Version                string       `json:"version"`
	CountryCode            string       `json:"country_code"`
	Country                string       `json:"country"`
	City                   string       `json:"city"`
	ProfileLabel           string       `json:"profile_label"`
	Endpoint               string       `json:"endpoint"`
	XrayOnline             bool         `json:"xray_online"`
	XKeenUI                bool         `json:"xkeen_ui_online"`
	DNSOut                 bool         `json:"dns_out_present"`
	ISP                    string       `json:"isp"`
	ISPLabel               string       `json:"isp_label"`
	DNSMode                string       `json:"dns_mode"`
	RecommendedDNSMode     string       `json:"recommended_dns_mode"`
	InstallScenario        string       `json:"install_scenario"`
	SetupComplete          bool         `json:"setup_complete"`
	SubscriptionConfigured bool         `json:"subscription_configured"`
	Busy                   bool         `json:"busy"`
	UpdaterBusy            bool         `json:"updater_busy"`
	Last                   actionResult `json:"last_action"`
}

type actionRequest struct {
	Action string `json:"action"`
}

type actionResult struct {
	Action    string `json:"action,omitempty"`
	Success   bool   `json:"success"`
	Message   string `json:"message,omitempty"`
	Error     string `json:"error,omitempty"`
	StartedAt string `json:"started_at,omitempty"`
	EndedAt   string `json:"ended_at,omitempty"`
}

type networkProfileRequest struct {
	ISP     string `json:"isp"`
	DNSMode string `json:"dns_mode"`
}

type networkProfileResponse struct {
	Success             bool   `json:"success"`
	ISP                 string `json:"isp"`
	ISPLabel            string `json:"isp_label"`
	DNSMode             string `json:"dns_mode"`
	DNSModeLabel        string `json:"dns_mode_label"`
	RecommendedDNSMode  string `json:"recommended_dns_mode"`
	RecommendedDNSLabel string `json:"recommended_dns_label"`
	Applied             bool   `json:"applied"`
	Message             string `json:"message,omitempty"`
	Error               string `json:"error,omitempty"`
}

type subscriptionRequest struct {
	URL string `json:"url"`
}

type subscriptionResponse struct {
	Success    bool   `json:"success"`
	Configured bool   `json:"configured"`
	Message    string `json:"message,omitempty"`
	Error      string `json:"error,omitempty"`
}

type xrayConfig struct {
	Outbounds []struct {
		Tag      string `json:"tag"`
		Settings struct {
			VNext []struct {
				Address string `json:"address"`
				Port    int    `json:"port"`
			} `json:"vnext"`
		} `json:"settings"`
	} `json:"outbounds"`
}

type snapshot struct {
	filterExists bool
	filter       []byte
	out          []byte
	outMode      os.FileMode
}

var profiles = map[string]struct {
	Country string
	City    string
	Label   string
}{
	"de": {Country: "Германия", City: "Frankfurt", Label: "Germany · Frankfurt"},
	"pl": {Country: "Польша", City: "Warsaw", Label: "Poland · Warsaw"},
	"fi": {Country: "Финляндия", City: "Helsinki", Label: "Finland · Helsinki"},
	"nl": {Country: "Нидерланды", City: "Amsterdam", Label: "Netherlands · Amsterdam"},
}

var ispProfiles = map[string]struct {
	Label              string
	RecommendedDNSMode string
}{
	"auto":            {Label: "Авто", RecommendedDNSMode: "auto"},
	"vladlink":        {Label: "Владлинк", RecommendedDNSMode: "xkeen"},
	"alliancetelecom": {Label: "АльянсТелеком", RecommendedDNSMode: "xkeen"},
	"rostelecom":      {Label: "Ростелеком", RecommendedDNSMode: "firmware"},
	"podryad":         {Label: "Подряд", RecommendedDNSMode: "firmware"},
	"custom":          {Label: "Свой", RecommendedDNSMode: "custom"},
}

var dnsModes = map[string]string{
	"auto":     "Авто",
	"firmware": "Штатный DNS роутера",
	"xkeen":    "XKeen/Xray DNS",
	"custom":   "Свой",
}

func main() {
	cfg := config{}
	flag.StringVar(&cfg.Listen, "listen", defaultListen, "listen address")
	flag.StringVar(&cfg.VPNPath, "vpn", defaultVPNPath, "vpn helper path")
	flag.StringVar(&cfg.FilterPath, "filter", defaultFilterPath, "profile filter path")
	flag.StringVar(&cfg.OutPath, "outbound", defaultOutPath, "Xray outbound config path")
	flag.StringVar(&cfg.XKeenPath, "xkeen", defaultXKeenPath, "XKeen executable path")
	flag.StringVar(&cfg.LockPath, "updater-lock", defaultLockPath, "updater lock path")
	flag.StringVar(&cfg.ConfigPath, "config", defaultConfigPath, "FreeNet local config path")
	flag.StringVar(&cfg.SubPath, "subscription", defaultSubPath, "BlancVPN subscription URL path")
	flag.DurationVar(&cfg.Timeout, "action-timeout", 95*time.Second, "action timeout")
	flag.Parse()

	a := &app{cfg: cfg, sem: make(chan struct{}, 1)}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /", a.handleIndex)
	mux.HandleFunc("GET /api/status", a.handleStatus)
	mux.HandleFunc("GET /api/network-profile", a.handleNetworkProfileGet)
	mux.HandleFunc("POST /api/network-profile", a.handleNetworkProfilePost)
	mux.HandleFunc("GET /api/network-profile/plan", a.handleNetworkProfilePlan)
	mux.HandleFunc("POST /api/network-profile/apply", a.handleNetworkProfileApply)
	mux.HandleFunc("GET /api/subscription", a.handleSubscriptionGet)
	mux.HandleFunc("POST /api/subscription", a.handleSubscriptionPost)
	mux.HandleFunc("POST /api/action", a.handleAction)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = io.WriteString(w, "ok\n")
	})

	srv := &http.Server{
		Handler:           securityHeaders(mux),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      110 * time.Second,
		IdleTimeout:       30 * time.Second,
	}

	lanListener, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		log.Fatalf("cannot listen on %s: %v", cfg.Listen, err)
	}
	listeners := []net.Listener{lanListener}

	if loopbackAddr := loopbackListenAddr(cfg.Listen); loopbackAddr != "" {
		loopbackListener, err := net.Listen("tcp", loopbackAddr)
		if err != nil {
			_ = lanListener.Close()
			log.Fatalf("cannot listen on %s: %v", loopbackAddr, err)
		}
		listeners = append(listeners, loopbackListener)
	}

	errCh := make(chan error, len(listeners))
	for _, listener := range listeners {
		ln := listener
		log.Printf("FreeNet UI %s listening on http://%s", version, ln.Addr().String())
		go func() {
			errCh <- srv.Serve(ln)
		}()
	}

	if err := <-errCh; !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

func loopbackListenAddr(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil || port == "" {
		return ""
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		return ""
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return ""
	}
	return net.JoinHostPort("127.0.0.1", port)
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; script-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		next.ServeHTTP(w, r)
	})
}

func (a *app) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	data, err := webFS.ReadFile("web/index.html")
	if err != nil {
		http.Error(w, "UI unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(data)
}

func (a *app) handleStatus(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, a.status())
}

func (a *app) handleNetworkProfileGet(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, a.networkProfileView(true, ""))
}

func (a *app) handleNetworkProfilePost(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		writeJSON(w, http.StatusForbidden, networkProfileResponse{Success: false, Error: "cross-origin request rejected"})
		return
	}
	if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(strings.ToLower(ct), "application/json") {
		writeJSON(w, http.StatusUnsupportedMediaType, networkProfileResponse{Success: false, Error: "application/json required"})
		return
	}

	body := http.MaxBytesReader(w, r.Body, 1024)
	defer body.Close()
	dec := json.NewDecoder(body)
	dec.DisallowUnknownFields()
	var req networkProfileRequest
	if err := dec.Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, networkProfileResponse{Success: false, Error: "invalid request"})
		return
	}
	if _, ok := ispProfiles[req.ISP]; !ok {
		writeJSON(w, http.StatusBadRequest, networkProfileResponse{Success: false, Error: "unsupported ISP"})
		return
	}
	if _, ok := dnsModes[req.DNSMode]; !ok {
		writeJSON(w, http.StatusBadRequest, networkProfileResponse{Success: false, Error: "unsupported DNS mode"})
		return
	}
	if err := writeNetworkProfileConfig(a.cfg.ConfigPath, req.ISP, req.DNSMode); err != nil {
		writeJSON(w, http.StatusInternalServerError, networkProfileResponse{Success: false, Error: "cannot save network profile"})
		return
	}
	writeJSON(w, http.StatusOK, a.networkProfileView(true, "Профиль сохранён. Xray не изменялся."))
}

func (a *app) handleSubscriptionGet(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, subscriptionResponse{Success: true, Configured: subscriptionConfigured(a.cfg.SubPath)})
}

func (a *app) handleSubscriptionPost(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		writeJSON(w, http.StatusForbidden, subscriptionResponse{Success: false, Error: "cross-origin request rejected"})
		return
	}
	if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(strings.ToLower(ct), "application/json") {
		writeJSON(w, http.StatusUnsupportedMediaType, subscriptionResponse{Success: false, Error: "application/json required"})
		return
	}

	body := http.MaxBytesReader(w, r.Body, 8192)
	defer body.Close()
	dec := json.NewDecoder(body)
	dec.DisallowUnknownFields()
	var req subscriptionRequest
	if err := dec.Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, subscriptionResponse{Success: false, Error: "invalid request"})
		return
	}
	if err := writeSubscriptionURL(a.cfg.SubPath, req.URL); err != nil {
		writeJSON(w, http.StatusBadRequest, subscriptionResponse{Success: false, Configured: subscriptionConfigured(a.cfg.SubPath), Error: "invalid subscription URL"})
		return
	}
	writeJSON(w, http.StatusOK, subscriptionResponse{Success: true, Configured: true, Message: "Подписка сохранена локально. Секрет не отображается."})
}

func (a *app) handleAction(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		writeJSON(w, http.StatusForbidden, actionResult{Success: false, Error: "cross-origin request rejected"})
		return
	}
	if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(strings.ToLower(ct), "application/json") {
		writeJSON(w, http.StatusUnsupportedMediaType, actionResult{Success: false, Error: "application/json required"})
		return
	}

	body := http.MaxBytesReader(w, r.Body, 1024)
	defer body.Close()
	dec := json.NewDecoder(body)
	dec.DisallowUnknownFields()
	var req actionRequest
	if err := dec.Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, actionResult{Success: false, Error: "invalid request"})
		return
	}

	if req.Action != "update" {
		if _, ok := profiles[req.Action]; !ok {
			writeJSON(w, http.StatusBadRequest, actionResult{Success: false, Error: "unsupported action"})
			return
		}
	}

	select {
	case a.sem <- struct{}{}:
		defer func() { <-a.sem }()
	default:
		writeJSON(w, http.StatusConflict, actionResult{Success: false, Error: "another FreeNet operation is already running"})
		return
	}

	result := a.runAction(req.Action)
	code := http.StatusOK
	if !result.Success {
		code = http.StatusBadGateway
	}
	a.mu.Lock()
	a.last = result
	a.mu.Unlock()
	writeJSON(w, code, result)
}

func runCommand(ctx context.Context, path string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Env = append(os.Environ(), "PATH=/opt/bin:/opt/sbin:/opt/usr/bin:/opt/usr/sbin:/bin:/sbin:/usr/bin:/usr/sbin")
	cmd.WaitDelay = 2 * time.Second
	output, err := cmd.CombinedOutput()
	if errors.Is(err, exec.ErrWaitDelay) && ctx.Err() == nil {
		err = nil
	}
	return output, err
}

func (a *app) runAction(action string) actionResult {
	started := time.Now()
	result := actionResult{Action: action, StartedAt: started.Format(time.RFC3339)}

	if _, err := os.Stat(a.cfg.LockPath); err == nil {
		result.Error = "updater is already busy"
		result.EndedAt = time.Now().Format(time.RFC3339)
		return result
	}

	var snap snapshot
	var err error
	if action != "update" {
		snap, err = a.takeSnapshot()
		if err != nil {
			result.Error = "cannot create switch snapshot: " + err.Error()
			result.EndedAt = time.Now().Format(time.RFC3339)
			return result
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), a.cfg.Timeout)
	defer cancel()

	output, cmdErr := runCommand(ctx, a.cfg.VPNPath, action)
	safeOutput := sanitizeOutput(string(output))

	if ctx.Err() == context.DeadlineExceeded {
		cmdErr = fmt.Errorf("operation timed out after %s", a.cfg.Timeout)
	}

	if cmdErr != nil {
		if action != "update" {
			if rbErr := a.restoreSnapshot(snap); rbErr != nil {
				result.Error = fmt.Sprintf("%v; rollback failed: %v", cmdErr, rbErr)
				if safeOutput != "" {
					result.Error += "; " + safeOutput
				}
				result.EndedAt = time.Now().Format(time.RFC3339)
				return result
			}
		}
		result.Error = cmdErr.Error()
		if safeOutput != "" {
			result.Error += "; " + safeOutput
		}
		result.EndedAt = time.Now().Format(time.RFC3339)
		return result
	}

	if !processRunning("xray") {
		if action != "update" {
			if rbErr := a.restoreSnapshot(snap); rbErr != nil {
				result.Error = "Xray is offline after switch; rollback failed: " + rbErr.Error()
				result.EndedAt = time.Now().Format(time.RFC3339)
				return result
			}
		}
		result.Error = "Xray is offline after operation"
		result.EndedAt = time.Now().Format(time.RFC3339)
		return result
	}

	if action != "update" {
		s := a.status()
		if s.CountryCode != action {
			if rbErr := a.restoreSnapshot(snap); rbErr != nil {
				result.Error = "selected profile did not match requested country; rollback failed: " + rbErr.Error()
				result.EndedAt = time.Now().Format(time.RFC3339)
				return result
			}
			result.Error = "selected profile did not match requested country"
			result.EndedAt = time.Now().Format(time.RFC3339)
			return result
		}
	}

	s := a.status()
	if !s.DNSOut {
		if action != "update" {
			if rbErr := a.restoreSnapshot(snap); rbErr != nil {
				result.Error = "dns-out missing after operation; rollback failed: " + rbErr.Error()
				result.EndedAt = time.Now().Format(time.RFC3339)
				return result
			}
		}
		result.Error = "dns-out missing after operation"
		result.EndedAt = time.Now().Format(time.RFC3339)
		return result
	}

	result.Success = true
	if action == "update" {
		result.Message = "Текущий VPN-сервер обновлён"
	} else {
		p := profiles[action]
		result.Message = p.Country + " · " + p.City + " активирован"
	}
	result.EndedAt = time.Now().Format(time.RFC3339)
	return result
}

func (a *app) takeSnapshot() (snapshot, error) {
	var s snapshot
	if b, err := os.ReadFile(a.cfg.FilterPath); err == nil {
		s.filterExists = true
		s.filter = b
	} else if !os.IsNotExist(err) {
		return s, err
	}

	info, err := os.Stat(a.cfg.OutPath)
	if err != nil {
		return s, err
	}
	s.outMode = info.Mode().Perm()
	s.out, err = os.ReadFile(a.cfg.OutPath)
	if err != nil {
		return s, err
	}
	return s, nil
}

func (a *app) restoreSnapshot(s snapshot) error {
	if s.filterExists {
		if err := atomicWrite(a.cfg.FilterPath, s.filter, 0644); err != nil {
			return fmt.Errorf("restore filter: %w", err)
		}
	} else if err := os.Remove(a.cfg.FilterPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove filter: %w", err)
	}

	mode := s.outMode
	if mode == 0 {
		mode = 0600
	}
	if err := atomicWrite(a.cfg.OutPath, s.out, mode); err != nil {
		return fmt.Errorf("restore outbound: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if out, err := runCommand(ctx, a.cfg.XKeenPath, "-restart"); err != nil {
		return fmt.Errorf("restart XKeen: %v (%s)", err, sanitizeOutput(string(out)))
	}
	time.Sleep(4 * time.Second)
	if !processRunning("xray") {
		return errors.New("Xray offline after rollback")
	}
	return nil
}

func atomicWrite(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, ".freenet-ui-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Chmod(mode); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

func (a *app) status() statusResponse {
	code := detectCountry(a.cfg.FilterPath)
	p := profiles[code]
	endpoint, dnsOut := readOutbound(a.cfg.OutPath)
	isp, dnsMode := readNetworkProfileConfig(a.cfg.ConfigPath)
	installScenario, setupComplete := readSetupState(a.cfg.ConfigPath)
	ispMeta := ispProfiles[isp]

	a.mu.RLock()
	last := a.last
	a.mu.RUnlock()

	busy := len(a.sem) > 0
	_, lockErr := os.Stat(a.cfg.LockPath)

	return statusResponse{
		Version:                version,
		CountryCode:            code,
		Country:                p.Country,
		City:                   p.City,
		ProfileLabel:           p.Label,
		Endpoint:               endpoint,
		XrayOnline:             processRunning("xray"),
		XKeenUI:                processRunning("xkeen-ui"),
		DNSOut:                 dnsOut,
		ISP:                    isp,
		ISPLabel:               ispMeta.Label,
		DNSMode:                dnsMode,
		RecommendedDNSMode:     ispMeta.RecommendedDNSMode,
		InstallScenario:        installScenario,
		SetupComplete:          setupComplete,
		SubscriptionConfigured: subscriptionConfigured(a.cfg.SubPath),
		Busy:                   busy,
		UpdaterBusy:            lockErr == nil,
		Last:                   last,
	}
}

func (a *app) networkProfileView(success bool, message string) networkProfileResponse {
	isp, dnsMode := readNetworkProfileConfig(a.cfg.ConfigPath)
	meta := ispProfiles[isp]
	return networkProfileResponse{
		Success:             success,
		ISP:                 isp,
		ISPLabel:            meta.Label,
		DNSMode:             dnsMode,
		DNSModeLabel:        dnsModes[dnsMode],
		RecommendedDNSMode:  meta.RecommendedDNSMode,
		RecommendedDNSLabel: dnsModes[meta.RecommendedDNSMode],
		Applied:             false,
		Message:             message,
	}
}

func readNetworkProfileConfig(path string) (string, string) {
	isp := "auto"
	dnsMode := "auto"
	b, err := os.ReadFile(path)
	if err != nil {
		return isp, dnsMode
	}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		if len(value) >= 2 {
			if (value[0] == '\'' && value[len(value)-1] == '\'') || (value[0] == '"' && value[len(value)-1] == '"') {
				value = value[1 : len(value)-1]
			}
		}
		switch strings.TrimSpace(key) {
		case "ISP_ID":
			isp = value
		case "DNS_MODE":
			dnsMode = value
		}
	}
	if _, ok := ispProfiles[isp]; !ok {
		isp = "auto"
	}
	if _, ok := dnsModes[dnsMode]; !ok {
		dnsMode = "auto"
	}
	return isp, dnsMode
}

func readSetupState(path string) (string, bool) {
	installScenario := "unknown"
	setupComplete := false
	b, err := os.ReadFile(path)
	if err != nil {
		return installScenario, setupComplete
	}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		value = strings.Trim(strings.TrimSpace(value), "'\"")
		switch strings.TrimSpace(key) {
		case "INSTALL_SCENARIO":
			if value == "existing_stack" || value == "fresh_entware" {
				installScenario = value
			}
		case "SETUP_COMPLETE":
			setupComplete = value == "yes"
		}
	}
	return installScenario, setupComplete
}

func writeNetworkProfileConfig(path, isp, dnsMode string) error {
	if _, ok := ispProfiles[isp]; !ok {
		return errors.New("unsupported ISP")
	}
	if _, ok := dnsModes[dnsMode]; !ok {
		return errors.New("unsupported DNS mode")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}

	var lines []string
	if b, err := os.ReadFile(path); err == nil {
		lines = strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
	} else if !os.IsNotExist(err) {
		return err
	}

	seenISP := false
	seenDNS := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "ISP_ID=") {
			lines[i] = "ISP_ID=" + isp
			seenISP = true
		}
		if strings.HasPrefix(trimmed, "DNS_MODE=") {
			lines[i] = "DNS_MODE=" + dnsMode
			seenDNS = true
		}
	}
	if !seenISP {
		lines = append(lines, "ISP_ID="+isp)
	}
	if !seenDNS {
		lines = append(lines, "DNS_MODE="+dnsMode)
	}
	content := strings.Join(lines, "\n") + "\n"
	return atomicWrite(path, []byte(content), 0600)
}

func validateSubscriptionURL(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > 4096 || strings.ContainsAny(raw, " \t\r\n") {
		return errors.New("invalid subscription URL")
	}
	u, err := url.ParseRequestURI(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
		return errors.New("invalid subscription URL")
	}
	return nil
}

func writeSubscriptionURL(path, raw string) error {
	raw = strings.TrimSpace(raw)
	if err := validateSubscriptionURL(raw); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	return atomicWrite(path, []byte(raw+"\n"), 0600)
}

func subscriptionConfigured(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular() && info.Size() > 0
}

func detectCountry(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	s := strings.ToLower(string(b))
	switch {
	case strings.Contains(s, "frankfurt") || strings.Contains(s, "germany") || strings.Contains(s, "германия"):
		return "de"
	case strings.Contains(s, "warsaw") || strings.Contains(s, "poland") || strings.Contains(s, "польша"):
		return "pl"
	case strings.Contains(s, "helsinki") || strings.Contains(s, "finland") || strings.Contains(s, "финляндия"):
		return "fi"
	case strings.Contains(s, "amsterdam") || strings.Contains(s, "netherlands") || strings.Contains(s, "нидерланды"):
		return "nl"
	default:
		return ""
	}
}

func readOutbound(path string) (string, bool) {
	f, err := os.Open(path)
	if err != nil {
		return "—", false
	}
	defer f.Close()
	var cfg xrayConfig
	if err := json.NewDecoder(f).Decode(&cfg); err != nil {
		return "—", false
	}
	endpoint := "—"
	dnsOut := false
	for _, ob := range cfg.Outbounds {
		switch ob.Tag {
		case "dns-out":
			dnsOut = true
		case "vless-reality":
			if len(ob.Settings.VNext) > 0 {
				v := ob.Settings.VNext[0]
				endpoint = net.JoinHostPort(v.Address, strconv.Itoa(v.Port))
			}
		}
	}
	return endpoint, dnsOut
}

func processRunning(name string) bool {
	dirs, err := os.ReadDir("/proc")
	if err != nil {
		return false
	}
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		if _, err := strconv.Atoi(d.Name()); err != nil {
			continue
		}
		b, err := os.ReadFile(filepath.Join("/proc", d.Name(), "comm"))
		if err == nil && strings.TrimSpace(string(b)) == name {
			return true
		}
	}
	return false
}

func sameOrigin(r *http.Request) bool {
	if site := strings.ToLower(strings.TrimSpace(r.Header.Get("Sec-Fetch-Site"))); site == "cross-site" {
		return false
	}
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	return strings.EqualFold(u.Host, r.Host)
}

func sanitizeOutput(s string) string {
	s = strings.ReplaceAll(s, "\r", "")
	lines := strings.Split(s, "\n")
	safe := make([]string, 0, len(lines))
	for _, line := range lines {
		low := strings.ToLower(line)
		if strings.Contains(low, "uuid=") || strings.Contains(low, "pbk=") || strings.Contains(low, "sid=") || strings.Contains(low, "subscription") && strings.Contains(low, "http") {
			continue
		}
		line = strings.TrimSpace(line)
		if line != "" {
			safe = append(safe, line)
		}
	}
	joined := strings.Join(safe, " | ")
	if len(joined) > 2000 {
		joined = joined[len(joined)-2000:]
	}
	return joined
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func fileSHA256(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return ""
	}
	return hex.EncodeToString(h.Sum(nil))
}
