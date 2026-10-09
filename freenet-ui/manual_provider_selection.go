package main

import (
    "errors"
    "os"
    "strings"
)

// loadManualProviderCandidate reads ONLY the locally accepted, source-bound
// subscription cache. Explicit manual selection must never fetch the provider,
// ping the endpoint, or infer credentials from the browser's public catalog.
// The credentials remain exclusively in the router's 0600 temporary files.
func (a *app) loadManualProviderCandidate(profileID string) (bestServerInternalCandidate, error) {
    if !validProfileID(profileID) {
        return bestServerInternalCandidate{}, errors.New("invalid provider profile id")
    }
    rawURL, err := os.ReadFile(a.cfg.SubPath)
    if err != nil || validateSubscriptionURL(strings.TrimSpace(string(rawURL))) != nil {
        return bestServerInternalCandidate{}, errors.New("VPN subscription is not configured")
    }
    if !providerSubscriptionCacheMatches(strings.TrimSpace(string(rawURL))) {
        return bestServerInternalCandidate{}, errors.New("Нет защищённого локального списка VPN для этой подписки. Обновите подписку отдельно; ручное подключение не выполнялось.")
    }
    data, err := os.ReadFile(providerSubscriptionCachePath())
    if err != nil || len(data) == 0 || len(data) > maxSubscriptionBytes {
        return bestServerInternalCandidate{}, errors.New("Локальный список VPN недоступен; ручное подключение не выполнялось.")
    }
    lines, err := decodedProviderSubscriptionLines(data)
    if err != nil {
        return bestServerInternalCandidate{}, errors.New("Локальный список VPN повреждён; ручное подключение не выполнялось.")
    }
    for _, raw := range lines {
        profile, ok := parseSafeVLESSProfile(raw)
        if !ok {
            return bestServerInternalCandidate{}, errors.New("Локальный список VPN некорректен; ручное подключение не выполнялось.")
        }
        if profile.ID == profileID {
            return bestServerInternalCandidate{Profile: profile, Raw: raw}, nil
        }
    }
    return bestServerInternalCandidate{}, errors.New("Выбранный сервер отсутствует в защищённом списке текущей подписки. Обновите список отдельно.")
}
