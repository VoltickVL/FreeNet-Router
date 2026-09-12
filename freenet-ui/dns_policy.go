package main

// enforceSafeDNSProductPolicy отделяет выбор интернет-провайдера от DNS.
// Безопасный product default одинаков для любого ISP: DNS через роутер.
// Раздельный DNS остаётся доступным только как явный выбор пользователя.
func init() {
	for id, meta := range ispProfiles {
		meta.RecommendedDNSMode = "firmware"
		ispProfiles[id] = meta
	}

	// `auto` остаётся legacy-внутренним alias штатного DNS. Пользовательский UI
	// Settings v2 показывает только два фактических режима: firmware и xkeen.
	dnsModes["auto"] = "DNS через роутер"
	dnsModes["firmware"] = "DNS через роутер"
	dnsModes["xkeen"] = "Раздельный DNS"
}
