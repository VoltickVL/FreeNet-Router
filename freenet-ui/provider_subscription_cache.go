package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const providerSubscriptionCachePathDefault = "/opt/var/lib/freenet/provider-subscription.lkg"

func providerSubscriptionCachePath() string {
	if path := strings.TrimSpace(os.Getenv("FREENET_PROVIDER_SUBSCRIPTION_CACHE")); path != "" {
		return path
	}
	return providerSubscriptionCachePathDefault
}

func providerSubscriptionSourcePath() string {
	if path := strings.TrimSpace(os.Getenv("FREENET_PROVIDER_SUBSCRIPTION_SOURCE")); path != "" {
		return path
	}
	return providerSubscriptionCachePath() + ".source.sha256"
}

func providerSubscriptionSourceFingerprint(rawURL string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(rawURL)))
	return hex.EncodeToString(sum[:])
}

func decodedProviderSubscriptionLines(body []byte) ([]string, error) {
	text := strings.ReplaceAll(string(body), "\r", "")
	if !strings.Contains(strings.ToLower(text), "vless://") {
		decoded, err := decodeSubscriptionBase64(text)
		if err != nil {
			return nil, errors.New("subscription format is unsupported")
		}
		text = strings.ReplaceAll(string(decoded), "\r", "")
	}

	lines := make([]string, 0, 16)
	seen := map[string]bool{}
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if !strings.HasPrefix(strings.ToLower(line), "vless://") {
			continue
		}
		profile, ok := parseSafeVLESSProfile(line)
		if !ok || seen[profile.ID] {
			continue
		}
		if len(selectableSubscriptionProfiles([]subscriptionProfile{profile})) != 1 {
			continue
		}
		seen[profile.ID] = true
		lines = append(lines, line)
		if len(lines) >= 100 {
			break
		}
	}
	if len(lines) == 0 {
		return nil, errors.New("no selectable Extra profiles found")
	}
	return lines, nil
}

func saveProviderSubscriptionCache(rawURL string, body []byte) error {
	rawURL = strings.TrimSpace(rawURL)
	if err := validateSubscriptionURL(rawURL); err != nil {
		return errors.New("provider cache source is invalid")
	}
	lines, err := decodedProviderSubscriptionLines(body)
	if err != nil {
		return err
	}
	cachePath := providerSubscriptionCachePath()
	sourcePath := providerSubscriptionSourcePath()
	if err := os.MkdirAll(filepath.Dir(cachePath), 0700); err != nil {
		return err
	}
	if filepath.Dir(sourcePath) != filepath.Dir(cachePath) {
		if err := os.MkdirAll(filepath.Dir(sourcePath), 0700); err != nil {
			return err
		}
	}
	if err := atomicWrite(cachePath, []byte(strings.Join(lines, "\n")+"\n"), 0600); err != nil {
		return err
	}
	return atomicWrite(sourcePath, []byte(providerSubscriptionSourceFingerprint(rawURL)+"\n"), 0600)
}

func providerCachedBestServerCandidate(profileID string) (bestServerInternalCandidate, error) {
	profileID = strings.TrimSpace(profileID)
	if !validProfileID(profileID) {
		return bestServerInternalCandidate{}, errors.New("invalid provider profile id")
	}
	data, err := os.ReadFile(providerSubscriptionCachePath())
	if err != nil || len(data) == 0 || len(data) > maxSubscriptionBytes {
		return bestServerInternalCandidate{}, errors.New("secure provider cache is unavailable")
	}
	candidates, _, _, err := parseBestServerCandidates(data)
	if err != nil {
		return bestServerInternalCandidate{}, errors.New("secure provider cache is invalid")
	}
	for _, candidate := range candidates {
		if strings.TrimSpace(candidate.Profile.ID) == profileID {
			return candidate, nil
		}
	}
	return bestServerInternalCandidate{}, errors.New("selected logical VPN is absent from prepared subscription")
}

func providerSubscriptionCacheMatches(rawURL string) bool {
	rawURL = strings.TrimSpace(rawURL)
	if validateSubscriptionURL(rawURL) != nil {
		return false
	}
	source, err := os.ReadFile(providerSubscriptionSourcePath())
	if err != nil || strings.TrimSpace(string(source)) != providerSubscriptionSourceFingerprint(rawURL) {
		return false
	}
	data, err := os.ReadFile(providerSubscriptionCachePath())
	if err != nil || len(data) == 0 || len(data) > maxSubscriptionBytes {
		return false
	}
	_, err = decodedProviderSubscriptionLines(data)
	return err == nil
}

func (a *app) ensureProviderSubscriptionCache(ctx context.Context) error {
	rawURL, err := os.ReadFile(a.cfg.SubPath)
	if err != nil {
		return errors.New("subscription is not configured")
	}
	secretURL := strings.TrimSpace(string(rawURL))
	if err := validateSubscriptionURL(secretURL); err != nil {
		return errors.New("stored subscription URL is invalid")
	}
	u, err := url.Parse(secretURL)
	if err != nil {
		return errors.New("stored subscription URL is invalid")
	}

	// Credential-bearing provider data must be refreshed before a plan/apply
	// whenever a fresh source is reachable. A source-bound LKG is only a
	// fallback: returning it first would make same-endpoint UUID/Reality
	// rotations invisible indefinitely.
	directCtx, cancelDirect := context.WithTimeout(ctx, 12*time.Second)
	body, directErr := directSubscriptionBodyFetch(directCtx, u)
	cancelDirect()
	if directErr != nil {
		vpnCtx, cancelVPN := context.WithTimeout(ctx, 18*time.Second)
		body, err = activeVPNSubscriptionBodyFetch(a, vpnCtx, u)
		cancelVPN()
		if err != nil {
			if providerSubscriptionCacheMatches(secretURL) {
				return nil
			}
			return errors.New("fresh subscription unavailable and secure provider cache is missing")
		}
	}
	if _, err := parseSubscriptionBody(body); err != nil {
		if providerSubscriptionCacheMatches(secretURL) {
			return nil
		}
		return errors.New("subscription response is invalid")
	}
	if err := saveProviderSubscriptionCache(secretURL, body); err != nil {
		return errors.New("cannot persist secure provider cache")
	}
	return nil
}
