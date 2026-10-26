package main

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

const xrayCoreCatalogDirectBudget = 6 * time.Second

type xrayCoreCatalogRequest func(context.Context) ([]byte, error)

// Fetch only the pinned official GitHub API endpoint. A response body that
// fails size/JSON/asset validation must not trigger an alternative network
// path: only a bounded direct network failure permits one VPN attempt.
func fetchXrayCoreCatalogBodyWithClient(ctx context.Context, client *http.Client) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, xrayCoreReleasesURL, nil)
	if err != nil {
		return nil, xrayCoreDownloadError("request", false)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "FreeNet-Router")
	response, err := client.Do(req)
	if err != nil {
		return nil, xrayCoreDownloadNetworkError(ctx, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, xrayCoreDownloadHTTPError(response.StatusCode)
	}
	return readXrayCoreCatalogBody(response.Body)
}

// Keep the main direct request short enough to leave a bounded window for
// the existing isolated active-VPN worker before the HTTP handler deadline.
// A cancellation, invalid body, or non-retryable status stops immediately.
func fetchXrayCoreCatalogResilient(ctx context.Context, direct, viaVPN xrayCoreCatalogRequest) ([]byte, error) {
	directCtx, cancel := context.WithTimeout(ctx, xrayCoreCatalogDirectBudget)
	body, directErr := direct(directCtx)
	cancel()
	if directErr == nil {
		return body, nil
	}
	if ctx.Err() != nil || !shouldFallbackXrayCoreDownload(directErr) {
		return nil, directErr
	}
	body, vpnErr := viaVPN(ctx)
	if vpnErr != nil {
		return nil, fmt.Errorf("reason=xray-catalog-direct-%s; vpn-%s",
			xrayCoreDownloadSafeCode(directErr), xrayCoreDownloadSafeCode(vpnErr))
	}
	return body, nil
}

func (a *app) fetchXrayCoreCatalogResilient(ctx context.Context) ([]byte, error) {
	return fetchXrayCoreCatalogResilient(ctx,
		func(directCtx context.Context) ([]byte, error) {
			return fetchXrayCoreCatalogBodyWithClient(directCtx, xrayCoreHTTPClient)
		},
		func(vpnCtx context.Context) ([]byte, error) {
			var body []byte
			err := a.withXrayCoreActiveVPNClient(vpnCtx, func(client *http.Client) error {
				var fetchErr error
				body, fetchErr = fetchXrayCoreCatalogBodyWithClient(vpnCtx, client)
				return fetchErr
			})
			return body, err
		},
	)
}
