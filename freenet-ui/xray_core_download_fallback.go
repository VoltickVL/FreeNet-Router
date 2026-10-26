package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// Only fixed reason codes may leave the router: never expose a URL, proxy,
// credentials, active outbound, DNS target, or a raw transport exception.
type xrayCoreDownloadFailure struct {
	code      string
	retryable bool
}

func (e *xrayCoreDownloadFailure) Error() string {
	return "reason=xray-download-" + e.code
}

func xrayCoreDownloadError(code string, retryable bool) error {
	return &xrayCoreDownloadFailure{code: code, retryable: retryable}
}

func xrayCoreDownloadNetworkError(ctx context.Context, err error) error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
		return xrayCoreDownloadError("timeout", true)
	}
	var networkErr net.Error
	if errors.As(err, &networkErr) && networkErr.Timeout() {
		return xrayCoreDownloadError("timeout", true)
	}
	return xrayCoreDownloadError("transport", true)
}

func xrayCoreDownloadHTTPError(code int) error {
	// A CDN may reject direct access while the same official asset is reachable
	// through the user's already-active VPN. Never retry missing or malformed
	// upstream assets as if they were routing failures.
	retryable := code == http.StatusForbidden || code == http.StatusTooManyRequests ||
		code == http.StatusUnavailableForLegalReasons || (code >= 500 && code <= 599)
	if code < 100 || code > 599 {
		return xrayCoreDownloadError("http-invalid", false)
	}
	return xrayCoreDownloadError(fmt.Sprintf("http-%d", code), retryable)
}

func shouldFallbackXrayCoreDownload(err error) bool {
	var failure *xrayCoreDownloadFailure
	return errors.As(err, &failure) && failure.retryable
}

func xrayCoreDownloadSafeCode(err error) string {
	var failure *xrayCoreDownloadFailure
	if errors.As(err, &failure) {
		return failure.code
	}
	return "unavailable"
}

type xrayCoreArchiveDownloader func(context.Context, xrayCoreRelease, string) (string, error)

// A bounded DIRECT attempt retains the existing router-owned SO_MARK=255
// path. Only an actual download transport/HTTP failure permits a single
// second attempt through an isolated, verified current VPN outbound.
// Neither path may accept a bad hash or alter the production Xray binary.
func downloadXrayCoreArchiveResilient(ctx context.Context, release xrayCoreRelease, dir string, direct, viaVPN xrayCoreArchiveDownloader) (string, error) {
	directCtx, cancelDirect := context.WithTimeout(ctx, 22*time.Second)
	path, directErr := direct(directCtx, release, dir)
	cancelDirect()
	if directErr == nil {
		return path, nil
	}
	if ctx.Err() != nil || !shouldFallbackXrayCoreDownload(directErr) {
		return "", directErr
	}
	path, proxyErr := viaVPN(ctx, release, dir)
	if proxyErr != nil {
		return "", fmt.Errorf("reason=xray-download-direct-%s; vpn-%s", xrayCoreDownloadSafeCode(directErr), xrayCoreDownloadSafeCode(proxyErr))
	}
	return path, nil
}

// Go's HTTP transport explicitly supports SOCKS5. The only unmarked socket
// here connects to the private 127.0.0.1 temporary Xray instance; all external
// traffic leaves via its pinned active VLESS outbound, not HTTP_PROXY.
func newXrayCoreVPNProxyClient(port int) *http.Client {
	proxyURL := &url.URL{Scheme: "socks5", Host: net.JoinHostPort("127.0.0.1", fmt.Sprint(port))}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = http.ProxyURL(proxyURL)
	transport.DialContext = (&net.Dialer{Timeout: 8 * time.Second, KeepAlive: 15 * time.Second}).DialContext
	transport.DisableKeepAlives = true
	return &http.Client{Timeout: 55 * time.Second, Transport: transport}
}

func (a *app) downloadXrayCoreArchiveResilient(ctx context.Context, release xrayCoreRelease, dir string) (string, error) {
	return downloadXrayCoreArchiveResilient(ctx, release, dir,
		downloadXrayCoreArchive,
		a.downloadXrayCoreArchiveViaActiveVPN)
}

// This fallback reuses the exact established read-only VPN quality probe
// machinery. It does not alter the running Xray/XKeen instance or global VPN,
// DNS, filter, routing or credentials, and cannot run for a mismatched endpoint.
func (a *app) downloadXrayCoreArchiveViaActiveVPN(ctx context.Context, release xrayCoreRelease, dir string) (string, error) {
	var path string
	err := a.withXrayCoreActiveVPNClient(ctx, func(client *http.Client) error {
		var downloadErr error
		path, downloadErr = downloadXrayCoreArchiveWithClient(ctx, release, dir, client)
		return downloadErr
	})
	if err != nil {
		return "", err
	}
	return path, nil
}

// The callback runs only while the private loopback SOCKS and its pinned
// active-VPN Xray are running. The same guarded session supports catalog
// fetches and verified binary downloads; no production configuration changes.
func (a *app) withXrayCoreActiveVPNClient(ctx context.Context, use func(*http.Client) error) error {
	if ctx.Err() != nil {
		return xrayCoreDownloadError("vpn-cancelled", false)
	}
	outbound, endpoint, ok := readBestServerActiveOutbound(a.cfg.OutPath)
	status := a.status()
	if !ok || !status.XrayOnline || !endpointsEqual(endpoint, status.Endpoint) {
		return xrayCoreDownloadError("vpn-identity", false)
	}
	safeOutbound, err := prepareIsolatedProbeOutbound(outbound)
	if err != nil {
		return xrayCoreDownloadError("vpn-config", false)
	}
	releaseProbe, acquired := acquireIsolatedXrayProbe(ctx)
	if !acquired {
		return xrayCoreDownloadError("vpn-probe-busy", false)
	}
	defer releaseProbe()

	binary, _, err := a.resolvedXrayCoreBinary()
	if err != nil {
		return xrayCoreDownloadError("vpn-binary", false)
	}
	tmpDir, err := os.MkdirTemp("", "freenet-core-download-")
	if err != nil {
		return xrayCoreDownloadError("vpn-stage", false)
	}
	defer os.RemoveAll(tmpDir)
	if err := os.Chmod(tmpDir, 0700); err != nil {
		return xrayCoreDownloadError("vpn-stage", false)
	}
	ownedBinary, err := isolatedXrayProbePath(tmpDir, binary)
	if err != nil {
		return xrayCoreDownloadError("vpn-binary", false)
	}
	port, err := reserveBestServerPort()
	if err != nil {
		return xrayCoreDownloadError("vpn-port", false)
	}
	config := map[string]any{
		"log": map[string]any{"loglevel": "warning"},
		"inbounds": []any{map[string]any{
			"listen": "127.0.0.1", "port": port, "protocol": "socks",
			"settings": map[string]any{"udp": false}, "tag": "freenet-core-download",
		}},
		"outbounds": []any{safeOutbound},
		"routing": map[string]any{
			"domainStrategy": "AsIs",
			"rules": []any{map[string]any{
				"type": "field", "inboundTag": []string{"freenet-core-download"}, "outboundTag": "vless-reality",
			}},
		},
	}
	data, err := json.Marshal(config)
	if err != nil {
		return xrayCoreDownloadError("vpn-config", false)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "00_download.json"), data, 0600); err != nil {
		return xrayCoreDownloadError("vpn-stage", false)
	}

	env := isolatedXrayProbeEnv(append(os.Environ(), "XRAY_LOCATION_ASSET="+a.geoDataAssetDir()))
	testCtx, stopTest := context.WithTimeout(ctx, 7*time.Second)
	testCmd := exec.CommandContext(testCtx, ownedBinary, "run", "-test", "-confdir", tmpDir)
	testCmd.Env = env
	testCmd.Stdout = io.Discard
	testCmd.Stderr = io.Discard
	testCmd.WaitDelay = 2 * time.Second
	testErr := testCmd.Run()
	stopTest()
	if testErr != nil {
		return xrayCoreDownloadError("vpn-validation", false)
	}

	runCtx, stopRun := context.WithCancel(ctx)
	defer stopRun()
	cmd := exec.CommandContext(runCtx, ownedBinary, "run", "-confdir", tmpDir)
	cmd.Env = env
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	cmd.WaitDelay = 2 * time.Second
	if err := cmd.Start(); err != nil {
		return xrayCoreDownloadError("vpn-start", false)
	}
	defer func() {
		stopRun()
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
	}()
	if !waitBestServerSOCKS(ctx, port) {
		return xrayCoreDownloadError("vpn-startup-timeout", false)
	}
	client := newXrayCoreVPNProxyClient(port)
	return use(client)
}
