package main

import (
    "context"
    "errors"
    "net/url"
    "os"
    "strings"
    "time"
)

// bestServerCatalogSource is safe public provenance. Never include the URL,
// raw VLESS lines, credentials, or provider-side response details.
type bestServerCatalogSource struct {
    Fresh     bool
    Source    string
    UpdatedAt string
}

func bestServerCatalogFromBody(body []byte, source, secretURL string) ([]bestServerInternalCandidate, int, bool, bestServerCatalogSource, error) {
    candidates, total, truncated, err := parseBestServerCandidates(body)
    if err != nil {
        return nil, 0, false, bestServerCatalogSource{}, err
    }
    if err := saveProviderSubscriptionCache(secretURL, body); err != nil {
        return nil, 0, false, bestServerCatalogSource{}, errors.New("cannot persist source-bound protected VPN catalog")
    }
    return candidates, total, truncated, bestServerCatalogSource{
        Fresh: true, Source: source, UpdatedAt: time.Now().UTC().Format(time.RFC3339),
    }, nil
}

// A source-matched router-local LKG is allowed only as a labeled stale read
// for diagnostics/Best/RTT. No silent fetch success and no live VPN mutation.
func loadBestServerProtectedCatalog(secretURL string) ([]bestServerInternalCandidate, int, bool, bestServerCatalogSource, error) {
    if !providerSubscriptionCacheMatches(secretURL) {
        return nil, 0, false, bestServerCatalogSource{}, errors.New("fresh VPN subscription unavailable; protected catalog is absent or belongs to another subscription")
    }
    cacheInfo, err := os.Stat(providerSubscriptionCachePath())
    if err != nil || !cacheInfo.Mode().IsRegular() || cacheInfo.Mode().Perm()&0077 != 0 {
        return nil, 0, false, bestServerCatalogSource{}, errors.New("protected VPN catalog has invalid file permissions")
    }
    sourceInfo, err := os.Stat(providerSubscriptionSourcePath())
    if err != nil || !sourceInfo.Mode().IsRegular() || sourceInfo.Mode().Perm()&0077 != 0 {
        return nil, 0, false, bestServerCatalogSource{}, errors.New("protected VPN catalog source has invalid permissions")
    }
    body, err := os.ReadFile(providerSubscriptionCachePath())
    if err != nil || len(body) == 0 || len(body) > maxSubscriptionBytes {
        return nil, 0, false, bestServerCatalogSource{}, errors.New("protected VPN catalog cannot be read")
    }
    candidates, total, truncated, err := parseBestServerCandidates(body)
    if err != nil {
        return nil, 0, false, bestServerCatalogSource{}, errors.New("protected VPN catalog is invalid")
    }
    return candidates, total, truncated, bestServerCatalogSource{
        Fresh: false, Source: "protected_cache", UpdatedAt: cacheInfo.ModTime().UTC().Format(time.RFC3339),
    }, nil
}

// Best and picker RTT share subscription freshness policy with Settings:
// direct, active VPN fallback, then explicit source-bound LKG, never guesses.
// Preserve the original simple discoverBestServerCandidates signature for
// targeted refresh callers that do not expose catalog provenance.
func (a *app) discoverBestServerCandidatesWithSource(ctx context.Context) ([]bestServerInternalCandidate, int, bool, bestServerCatalogSource, error) {
    if err := ctx.Err(); err != nil {
        return nil, 0, false, bestServerCatalogSource{}, err
    }
    rawURL, err := os.ReadFile(a.cfg.SubPath)
    if err != nil {
        return nil, 0, false, bestServerCatalogSource{}, errors.New("subscription is not configured")
    }
    secretURL := strings.TrimSpace(string(rawURL))
    if err := validateSubscriptionURL(secretURL); err != nil {
        return nil, 0, false, bestServerCatalogSource{}, errors.New("stored subscription URL is invalid")
    }
    u, err := url.Parse(secretURL)
    if err != nil {
        return nil, 0, false, bestServerCatalogSource{}, errors.New("stored subscription URL is invalid")
    }

    directCtx, cancelDirect := context.WithTimeout(ctx, 12*time.Second)
    body, fetchErr := directSubscriptionBodyFetch(directCtx, u)
    cancelDirect()
    if fetchErr == nil {
        if candidates, total, truncated, source, err := bestServerCatalogFromBody(body, "direct", secretURL); err == nil {
            return candidates, total, truncated, source, nil
        }
    }
    if err := ctx.Err(); err != nil {
        return nil, 0, false, bestServerCatalogSource{}, err
    }

    vpnCtx, cancelVPN := context.WithTimeout(ctx, 18*time.Second)
    body, fetchErr = activeVPNSubscriptionBodyFetch(a, vpnCtx, u)
    cancelVPN()
    if fetchErr == nil {
        if candidates, total, truncated, source, err := bestServerCatalogFromBody(body, "active_vpn", secretURL); err == nil {
            return candidates, total, truncated, source, nil
        }
    }
    if err := ctx.Err(); err != nil {
        return nil, 0, false, bestServerCatalogSource{}, err
    }
    return loadBestServerProtectedCatalog(secretURL)
}
