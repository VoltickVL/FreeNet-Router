package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func curlConfigQuote(value string) string {
	value = strings.ReplaceAll(value, "\\", "\\\\")
	value = strings.ReplaceAll(value, """, "\\"")
	return """ + value + """
}

func (a *app) fetchSubscriptionBodyViaActiveVPN(ctx context.Context, subscriptionURL *url.URL) ([]byte, error) {
	if subscriptionURL == nil || subscriptionURL.Scheme != "https" || subscriptionURL.Hostname() == "" || subscriptionURL.User != nil {
		return nil, errors.New("stored subscription URL is invalid")
	}
	outbound, _, ok := readBestServerActiveOutbound(a.cfg.OutPath)
	if !ok {
		return nil, errors.New("active VPN outbound is unavailable")
	}
	xrayPath := strings.TrimSpace(os.Getenv("FREENET_XRAY_BIN"))
	if xrayPath == "" {
		xrayPath = defaultBestServerXrayPath
	}
	if _, err := os.Stat(xrayPath); err != nil {
		return nil, errors.New("Xray binary is unavailable")
	}
	curlPath, err := exec.LookPath("curl")
	if err != nil {
		return nil, errors.New("curl is unavailable")
	}
	port, err := reserveBestServerPort()
	if err != nil {
		return nil, errors.New("cannot reserve active VPN fetch port")
	}
	tmpDir, err := os.MkdirTemp("", "freenet-subscription-vpn-")
	if err != nil {
		return nil, errors.New("cannot prepare active VPN subscription fetch")
	}
	defer os.RemoveAll(tmpDir)
	_ = os.Chmod(tmpDir, 0700)

	config := map[string]any{
		"log": map[string]any{"loglevel": "warning"},
		"inbounds": []any{map[string]any{
			"listen": "127.0.0.1", "port": port, "protocol": "socks",
			"settings": map[string]any{"udp": false}, "tag": "freenet-subscription-vpn",
		}},
		"outbounds": []any{outbound},
		"routing": map[string]any{
			"domainStrategy": "AsIs",
			"rules": []any{map[string]any{
				"type": "field", "inboundTag": []string{"freenet-subscription-vpn"}, "outboundTag": "vless-reality",
			}},
		},
	}
	encoded, err := json.Marshal(config)
	if err != nil {
		return nil, errors.New("cannot encode active VPN subscription fetch")
	}
	configPath := filepath.Join(tmpDir, "00_fetch.json")
	if err := os.WriteFile(configPath, encoded, 0600); err != nil {
		return nil, errors.New("cannot stage active VPN subscription fetch")
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
		return nil, errors.New("active VPN subscription fetch validation failed")
	}

	runCtx, cancelRun := context.WithCancel(ctx)
	defer cancelRun()
	xrayCmd := exec.CommandContext(runCtx, xrayPath, "run", "-confdir", tmpDir)
	xrayCmd.Env = env
	xrayCmd.Stdout = io.Discard
	xrayCmd.Stderr = io.Discard
	if err := xrayCmd.Start(); err != nil {
		return nil, errors.New("cannot start active VPN subscription fetch")
	}
	defer func() {
		cancelRun()
		if xrayCmd.Process != nil {
			_ = xrayCmd.Process.Kill()
		}
		_ = xrayCmd.Wait()
	}()
	if !waitBestServerSOCKS(ctx, port) {
		return nil, errors.New("active VPN subscription fetch did not become ready")
	}

	socks := "127.0.0.1:" + strconv.Itoa(port)
	curlCtx, cancelCurl := context.WithTimeout(ctx, 15*time.Second)
	defer cancelCurl()
	curlCmd := exec.CommandContext(curlCtx, curlPath,
		"--socks5-hostname", socks,
		"-4", "-fsS", "--location", "--max-redirs", "5",
		"--proto", "=https", "--proto-redir", "=https",
		"--connect-timeout", "5", "--max-time", "15",
		"-H", "Cache-Control: no-cache",
		"-H", "Pragma: no-cache",
		"-A", "FreeNet-Router/profile-discovery",
		"--config", "-",
	)
	curlCmd.Stdin = strings.NewReader("url = " + curlConfigQuote(subscriptionURL.String()) + "\n")
	var stdout bytes.Buffer
	curlCmd.Stdout = &stdout
	curlCmd.Stderr = io.Discard
	if err := curlCmd.Run(); err != nil {
		return nil, errors.New("subscription fetch through active VPN failed")
	}
	if stdout.Len() == 0 {
		return nil, errors.New("subscription response is empty")
	}
	if stdout.Len() > maxSubscriptionBytes {
		return nil, errors.New("subscription response is too large")
	}
	return append([]byte(nil), stdout.Bytes()...), nil
}
