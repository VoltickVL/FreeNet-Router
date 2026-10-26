package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// Only fixed GitHub metadata URLs are accepted by the production fetcher,
// and untrusted catalog responses remain subject to the existing parser.
func TestXrayCoreCatalogFetcherPinnedURLAndLimitedBody(t *testing.T) {
	client := &http.Client{Transport: xrayCoreRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.String() != xrayCoreReleasesURL || req.Method != http.MethodGet ||
			req.Header.Get("Accept") != "application/vnd.github+json" {
			t.Fatal("catalog fetch must use the pinned official read-only request")
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header),
			Body: io.NopCloser(strings.NewReader("[]")), Request: req}, nil
	})}
	body, err := fetchXrayCoreCatalogBodyWithClient(context.Background(), client)
	if err != nil || string(body) != "[]" {
		t.Fatalf("fixed catalog request failed: %v", err)
	}
}

func TestXrayCoreCatalogDirectSuccessNeverStartsVPN(t *testing.T) {
	direct, vpn := 0, 0
	body, err := fetchXrayCoreCatalogResilient(context.Background(),
		func(context.Context) ([]byte, error) {
			direct++
			return []byte("direct-catalog"), nil
		},
		func(context.Context) ([]byte, error) {
			vpn++
			return []byte("unwanted"), nil
		},
	)
	if err != nil || string(body) != "direct-catalog" || direct != 1 || vpn != 0 {
		t.Fatalf("direct path must be preferred, got %q, %v, %d/%d", body, err, direct, vpn)
	}
}

func TestXrayCoreCatalogNetworkFailureAllowsExactlyOneVPNAttempt(t *testing.T) {
	for _, reason := range []error{xrayCoreDownloadError("timeout", true),
		xrayCoreDownloadHTTPError(http.StatusForbidden),
		xrayCoreDownloadHTTPError(http.StatusTooManyRequests),
		xrayCoreDownloadHTTPError(http.StatusBadGateway),
	} {
		direct, vpn := 0, 0
		body, err := fetchXrayCoreCatalogResilient(context.Background(),
			func(context.Context) ([]byte, error) {
				direct++
				return nil, reason
			},
			func(context.Context) ([]byte, error) {
				vpn++
				return []byte("vpn-catalog"), nil
			},
		)
		if err != nil || string(body) != "vpn-catalog" || direct != 1 || vpn != 1 {
			t.Fatalf("network fallback mismatch: %q %v direct=%d vpn=%d", body, err, direct, vpn)
		}
	}
}

func TestXrayCoreCatalogDoesNotRetryInvalidOrBlockedResponse(t *testing.T) {
	for _, reason := range []error{xrayCoreDownloadHTTPError(http.StatusNotFound),
		xrayCoreDownloadHTTPError(http.StatusUnauthorized),
		errors.New("catalog body is invalid or too large"),
		errors.New("invalid Xray release catalog"),
	} {
		vpn := 0
		_, err := fetchXrayCoreCatalogResilient(context.Background(),
			func(context.Context) ([]byte, error) { return nil, reason },
			func(context.Context) ([]byte, error) { vpn++; return []byte("unexpected"), nil },
		)
		if err == nil || vpn != 0 {
			t.Fatalf("invalid catalog must stop before VPN fallback, err=%v retries=%d", err, vpn)
		}
	}
}

func TestXrayCoreCatalogCancelStopsBeforeFallback(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	vpn := 0
	_, err := fetchXrayCoreCatalogResilient(ctx,
		func(context.Context) ([]byte, error) {
			cancel()
			return nil, xrayCoreDownloadError("timeout", true)
		},
		func(context.Context) ([]byte, error) { vpn++; return []byte("unexpected"), nil },
	)
	if err == nil || vpn != 0 {
		t.Fatalf("cancelled catalog must not launch a child Xray: %v %d", err, vpn)
	}
}

func TestXrayCoreCatalogDirectRequestTimeoutIsBounded(t *testing.T) {
	directBudget := time.Duration(0)
	_, err := fetchXrayCoreCatalogResilient(context.Background(),
		func(ctx context.Context) ([]byte, error) {
			if deadline, ok := ctx.Deadline(); ok {
				directBudget = time.Until(deadline)
			}
			return []byte("{}"), nil
		},
		func(context.Context) ([]byte, error) { t.Fatal("VPN must not start"); return nil, nil },
	)
	if err != nil || directBudget <= 0 || directBudget > 7*time.Second {
		t.Fatalf("direct catalog has no finite budget: %v (%s)", err, directBudget)
	}
}

func TestXrayCoreCatalogDualFailureDoesNotLeakSecrets(t *testing.T) {
	_, err := fetchXrayCoreCatalogResilient(context.Background(),
		func(context.Context) ([]byte, error) {
			return nil, xrayCoreDownloadNetworkError(context.Background(),
				errors.New("https://github.invalid/token=secret"))
		},
		func(context.Context) ([]byte, error) {
			return nil, xrayCoreDownloadError("vpn-identity", false)
		},
	)
	if err == nil || !strings.Contains(err.Error(), "reason=xray-catalog-direct-transport; vpn-vpn-identity") ||
		strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "github.invalid") {
		t.Fatalf("dual failure must be finite, secret-free and reasoned: %v", err)
	}
}
