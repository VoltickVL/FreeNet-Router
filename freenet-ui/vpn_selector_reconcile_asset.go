package main

import _ "embed"

//go:embed web/vpn-selector-reconcile.js
var vpnSelectorReconcileBase []byte

//go:embed web/vpn-selector-modal-search.js
var vpnSelectorModalSearchPatch []byte

//go:embed web/topbar-settings-profile-cache.js
var topbarSettingsProfileCachePatch []byte

func combinedVPNSelectorReconcileAsset() []byte {
	asset := append([]byte{}, vpnSelectorReconcileBase...)
	asset = append(asset, '\n')
	asset = append(asset, vpnSelectorModalSearchPatch...)
	asset = append(asset, '\n')
	asset = append(asset, topbarSettingsProfileCachePatch...)
	return asset
}

var vpnSelectorReconcileAsset = combinedVPNSelectorReconcileAsset()
