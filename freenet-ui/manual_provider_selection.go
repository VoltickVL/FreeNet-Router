package main

import (
    "errors"
    "os"
    "strings"
)

// localManualProviderCandidate only reads the URL-bound, credential-bearing
// on-router LKG. Manual clicks never refresh the subscription or probe a VPN.
// IDs and endpoints are checked against the exact cached VLESS profile, not
// against browser-supplied credentials or a stale display-only catalog.
func (a *app) localManualProviderCandidate(profileID, expectedEndpoint string) (bestServerInternalCandidate, error) {
    if !validProfileID(profileID) || strings.TrimSpace(expectedEndpoint) == "" {
        return bestServerInternalCandidate{}, errors.New("выбранный VPN-сервер не определён; обновите список подписки")
    }
    rawURL, err := os.ReadFile(a.cfg.SubPath)
    if err != nil || !providerSubscriptionCacheMatches(strings.TrimSpace(string(rawURL))) {
        return bestServerInternalCandidate{}, errors.New("защищённый список VPN отсутствует или принадлежит другой подписке; обновите список подписки")
    }
    info, err := os.Stat(providerSubscriptionCachePath())
    if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
        return bestServerInternalCandidate{}, errors.New("защищённый список VPN недоступен или имеет небезопасные права; обновите список подписки")
    }
    body, err := os.ReadFile(providerSubscriptionCachePath())
    if err != nil || len(body) == 0 || len(body) > maxSubscriptionBytes {
        return bestServerInternalCandidate{}, errors.New("защищённый список VPN недоступен; обновите список подписки")
    }
    lines, err := decodedProviderSubscriptionLines(body)
    if err != nil {
        return bestServerInternalCandidate{}, errors.New("защищённый список VPN повреждён; обновите список подписки")
    }
    for _, raw := range lines {
        profile, ok := parseSafeVLESSProfile(raw)
        if !ok || profile.ID != profileID {
            continue
        }
        if profileEndpoint(profile) != strings.TrimSpace(expectedEndpoint) {
            return bestServerInternalCandidate{}, errors.New("адрес выбранного VPN изменился с момента отображения; обновите список подписки")
        }
        return bestServerInternalCandidate{Profile: profile, Raw: raw}, nil
    }
    return bestServerInternalCandidate{}, errors.New("выбранного VPN нет в защищённом списке этой подписки; обновите список подписки")
}
