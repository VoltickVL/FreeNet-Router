package main

import (
	"context"
	"net/http"
	"sort"
	"strings"
	"time"
)

const settingsCountryCatalogTimeout = 18 * time.Second

type settingsCountryOption struct {
	Code      string `json:"code"`
	Name      string `json:"name"`
	Available bool   `json:"available"`
}

type settingsCountryCatalogResponse struct {
	Success   bool                    `json:"success"`
	Countries []settingsCountryOption `json:"countries"`
	Selected  []string                `json:"selected"`
	Fresh     bool                    `json:"fresh"`
	Warning   string                  `json:"warning,omitempty"`
}

func registerSettingsCountryCatalogAPI(mux *http.ServeMux, a *app) {
	mux.HandleFunc("GET /api/settings-v3/countries", a.requireAuth(a.handleSettingsCountryCatalog))
}

func settingsCountryNameFromProfile(profile subscriptionProfile) string {
	name := strings.TrimSpace(profileDisplayName(profile.Name))
	parts := strings.Split(name, ",")
	if len(parts) >= 3 {
		country := strings.TrimSpace(parts[len(parts)-2])
		if country != "" && !strings.EqualFold(country, "extra") {
			return country
		}
	}
	return "Страна VPN"
}

func settingsCountryOptions(profiles []subscriptionProfile, selected []string) []settingsCountryOption {
	byCode := map[string]settingsCountryOption{}
	for _, profile := range profiles {
		code := strings.ToLower(strings.TrimSpace(profile.CountryCode))
		if len(code) != 2 || code == "ru" || !isBaseExtraProfileName(profile.Name) {
			continue
		}
		if _, exists := byCode[code]; exists {
			continue
		}
		byCode[code] = settingsCountryOption{Code: code, Name: settingsCountryNameFromProfile(profile), Available: true}
	}

	for _, raw := range normalizeAutomationCountries(selected) {
		code := strings.ToLower(strings.TrimSpace(raw))
		if len(code) != 2 || code == "ru" {
			continue
		}
		if _, exists := byCode[code]; !exists {
			byCode[code] = settingsCountryOption{Code: code, Name: "Ранее выбранная страна", Available: false}
		}
	}

	options := make([]settingsCountryOption, 0, len(byCode))
	for _, option := range byCode {
		options = append(options, option)
	}
	sort.Slice(options, func(i, j int) bool {
		if options[i].Available != options[j].Available {
			return options[i].Available
		}
		if options[i].Name != options[j].Name {
			return options[i].Name < options[j].Name
		}
		return options[i].Code < options[j].Code
	})
	return options
}

func (a *app) handleSettingsCountryCatalog(w http.ResponseWriter, r *http.Request) {
	selected := normalizeAutomationCountries(a.automationSnapshot().Settings.Countries)
	ctx, cancel := context.WithTimeout(r.Context(), settingsCountryCatalogTimeout)
	defer cancel()

	profiles, err := a.discoverSubscriptionProfiles(ctx)
	if err != nil {
		writeJSON(w, http.StatusOK, settingsCountryCatalogResponse{
			Success:   true,
			Countries: settingsCountryOptions(nil, selected),
			Selected:  selected,
			Fresh:     false,
			Warning:   "Каталог стран временно недоступен. Сохранённый выбор не изменён.",
		})
		return
	}

	writeJSON(w, http.StatusOK, settingsCountryCatalogResponse{
		Success:   true,
		Countries: settingsCountryOptions(profiles, selected),
		Selected:  selected,
		Fresh:     true,
	})
}
