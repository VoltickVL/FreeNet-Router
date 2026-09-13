package main

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

const (
	settingsDNSDirectProviderYandex = "yandex-doh"
	settingsDNSVPNProviderGoogle    = "google-doh"
	settingsDNSProviderGoogle       = "google-doh"
	settingsDNSProviderYandex       = "yandex-doh"

	settingsDNSYandexDoH = "https://dns.yandex.ru/dns-query"
	settingsDNSGoogleDoH = "https://dns.google/dns-query"
)

type settingsDNSProviderOption struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Endpoint string `json:"endpoint"`
}

type settingsDNSResponse struct {
	Success            bool                        `json:"success"`
	Mode               string                      `json:"mode"`
	ActiveMode         string                      `json:"active_mode"`
	DirectProvider     string                      `json:"direct_provider"`
	VPNProvider        string                      `json:"vpn_provider"`
	ActiveDirect       string                      `json:"active_direct_provider,omitempty"`
	ActiveVPN          string                      `json:"active_vpn_provider,omitempty"`
	DirectOptions      []settingsDNSProviderOption `json:"direct_options"`
	VPNOptions         []settingsDNSProviderOption `json:"vpn_options"`
	SplitRuntimeKnown  bool                        `json:"split_runtime_known"`
	ApplySupported     bool                        `json:"apply_supported"`
	Warning            string                      `json:"warning,omitempty"`
}

type settingsDNSConfig struct {
	DNS struct {
		Servers []struct {
			Address string `json:"address"`
			Tag     string `json:"tag"`
		} `json:"servers"`
	} `json:"dns"`
}

func settingsDNSProviderOptions() []settingsDNSProviderOption {
	return []settingsDNSProviderOption{
		{ID: settingsDNSProviderYandex, Label: "Яндекс DoH", Endpoint: settingsDNSYandexDoH},
		{ID: settingsDNSProviderGoogle, Label: "Google DoH", Endpoint: settingsDNSGoogleDoH},
	}
}

func validSettingsDNSProvider(value string) bool {
	switch strings.TrimSpace(value) {
	case settingsDNSProviderYandex, settingsDNSProviderGoogle:
		return true
	default:
		return false
	}
}

func settingsDNSProviderEndpoint(value string) string {
	switch strings.TrimSpace(value) {
	case settingsDNSProviderYandex:
		return settingsDNSYandexDoH
	case settingsDNSProviderGoogle:
		return settingsDNSGoogleDoH
	default:
		return ""
	}
}

func settingsDNSProviderFromEndpoint(value string) string {
	value = strings.TrimSpace(value)
	switch value {
	case settingsDNSYandexDoH:
		return settingsDNSProviderYandex
	case settingsDNSGoogleDoH:
		return settingsDNSProviderGoogle
	default:
		return ""
	}
}

func settingsDNSConfigDir() string {
	if value := strings.TrimSpace(os.Getenv("FREENET_XRAY_CONFIG_DIR")); value != "" {
		return value
	}
	return "/opt/etc/xray/configs"
}

func readSettingsSplitDNSRuntime() (direct, vpn string, known bool) {
	data, err := os.ReadFile(filepath.Join(settingsDNSConfigDir(), "02_dns.json"))
	if err != nil {
		return "", "", false
	}
	var cfg settingsDNSConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return "", "", false
	}
	var directSeen, vpnSeen bool
	for _, server := range cfg.DNS.Servers {
		switch server.Tag {
		case "dns-direct":
			provider := settingsDNSProviderFromEndpoint(server.Address)
			if provider == "" || (direct != "" && direct != provider) {
				return "", "", false
			}
			direct = provider
			directSeen = true
		case "dns-vless":
			provider := settingsDNSProviderFromEndpoint(server.Address)
			if provider == "" || (vpn != "" && vpn != provider) {
				return "", "", false
			}
			vpn = provider
			vpnSeen = true
		}
	}
	return direct, vpn, directSeen && vpnSeen
}

func settingsDNSDesiredProvider(configPath, key, fallback string) string {
	value := strings.TrimSpace(automationConfigValue(configPath, key, fallback))
	if validSettingsDNSProvider(value) {
		return value
	}
	return fallback
}

func registerSettingsDNSAPI(mux *http.ServeMux, a *app) {
	mux.HandleFunc("GET /api/settings-v3/dns", a.requireAuth(a.handleSettingsDNSGet))
}

func (a *app) handleSettingsDNSGet(w http.ResponseWriter, _ *http.Request) {
	control := settingsDNSControlSnapshot(a.cfg.ConfigPath)
	response := settingsDNSResponse{
		Success:           control.Success,
		Mode:              control.Mode,
		ActiveMode:        control.ActiveMode,
		DirectProvider:    control.DirectProvider,
		VPNProvider:       control.VPNProvider,
		ActiveDirect:      control.ActiveDirect,
		ActiveVPN:         control.ActiveVPN,
		DirectOptions:     control.DirectOptions,
		VPNOptions:        control.VPNOptions,
		SplitRuntimeKnown: control.ActiveMode != "xkeen" || control.RuntimeState != "unknown",
		ApplySupported:    control.ApplySupported,
		Warning:           control.Warning,
	}
	writeJSON(w, http.StatusOK, response)
}
