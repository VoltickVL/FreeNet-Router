package main

import (
	"errors"
	"os"
	"strings"
	"time"
)

// localManualProviderCatalog reads only the URL-bound, credential-bearing LKG.
// Display and manual admission must derive IDs from the same parser and source.
// Callers serving HTTP may project Profile only; Raw must stay on the router.
// This reader performs no provider fetch, probe, cache write or runtime change.
func (a *app) localManualProviderCatalog() ([]bestServerInternalCandidate, string, error) {
	rawURL, err := os.ReadFile(a.cfg.SubPath)
	if err != nil || !providerSubscriptionCacheMatches(strings.TrimSpace(string(rawURL))) {
		return nil, "", errors.New("защищённый список VPN отсутствует или принадлежит другой подписке; обновите список подписки")
	}
	info, err := os.Stat(providerSubscriptionCachePath())
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return nil, "", errors.New("защищённый список VPN недоступен или имеет небезопасные права; обновите список подписки")
	}
	body, err := os.ReadFile(providerSubscriptionCachePath())
	if err != nil || len(body) == 0 || len(body) > maxSubscriptionBytes {
		return nil, "", errors.New("защищённый список VPN недоступен; обновите список подписки")
	}
	lines, err := decodedProviderSubscriptionLines(body)
	if err != nil {
		return nil, "", errors.New("защищённый список VPN повреждён; обновите список подписки")
	}
	candidates := make([]bestServerInternalCandidate, 0, len(lines))
	for _, raw := range lines {
		profile, ok := parseSafeVLESSProfile(raw)
		if ok {
			candidates = append(candidates, bestServerInternalCandidate{Profile: profile, Raw: raw})
		}
	}
	if len(candidates) == 0 {
		return nil, "", errors.New("защищённый список VPN повреждён; обновите список подписки")
	}
	return candidates, info.ModTime().UTC().Format(time.RFC3339), nil
}

// localManualProviderCandidate checks the exact cached profile, never browser
// credentials or a display-only cache. Endpoint drift still needs a new choice.
func (a *app) localManualProviderCandidate(profileID, expectedEndpoint string) (bestServerInternalCandidate, error) {
	if !validProfileID(profileID) || strings.TrimSpace(expectedEndpoint) == "" {
		return bestServerInternalCandidate{}, errors.New("выбранный VPN-сервер не определён; обновите список подписки")
	}
	candidates, _, err := a.localManualProviderCatalog()
	if err != nil {
		return bestServerInternalCandidate{}, err
	}
	for _, candidate := range candidates {
		if candidate.Profile.ID != profileID {
			continue
		}
		if profileEndpoint(candidate.Profile) != strings.TrimSpace(expectedEndpoint) {
			return bestServerInternalCandidate{}, errors.New("адрес выбранного VPN изменился с момента отображения; обновите список подписки")
		}
		return candidate, nil
	}
	return bestServerInternalCandidate{}, errors.New("выбранного VPN нет в защищённом списке этой подписки; обновите список подписки")
}
