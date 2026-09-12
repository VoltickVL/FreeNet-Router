package main

// enforceSafeDNSProductPolicy отделяет выбор интернет-провайдера от DNS.
// Безопасный product default одинаков для любого ISP: штатный DNS роутера.
// Раздельный DNS остаётся доступным только как явный выбор пользователя.
func init() {
	for id, meta := range ispProfiles {
		meta.RecommendedDNSMode = "firmware"
		ispProfiles[id] = meta
	}

	// Внутренние/API labels сохраняются ради обратной совместимости старых
	// контрактов. Settings v2 переводит только user-facing UI в два названия:
	// `DNS через роутер` (firmware) и `Раздельный DNS` (xkeen).
	dnsModes["auto"] = "Авто (штатный DNS)"
	dnsModes["firmware"] = "Штатный DNS роутера"
	dnsModes["xkeen"] = "Split DNS через VPN (XKeen/Xray)"
}
