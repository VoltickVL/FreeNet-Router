package main

import (
	_ "embed"
	"net/http"
)

//go:embed web/settings-dns-ui.js
var settingsDNSUI []byte

func serveSettingsDNSUIAsset(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(settingsDNSUI)
}
