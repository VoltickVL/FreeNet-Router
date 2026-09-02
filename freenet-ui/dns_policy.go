package main

// enforceSafeDNSProductPolicy отделяет выбор интернет-провайдера от DNS.
// После v0.2.8 безопасный продуктовый default одинаков для любого ISP:
// штатный DNS Keenetic. Split DNS остаётся доступным только как явный выбор.
func init() {
	for id, meta := range ispProfiles {
		meta.RecommendedDNSMode = "firmware"
		ispProfiles[id] = meta
	}

	// API-метки также должны описывать фактическую семантику режима.
	dnsModes["auto"] = "Авто (штатный DNS)"
	dnsModes["firmware"] = "Штатный DNS роутера"
	dnsModes["xkeen"] = "Split DNS через VPN (XKeen/Xray)"
}
