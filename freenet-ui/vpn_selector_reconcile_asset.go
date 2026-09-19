package main

import _ "embed"

//go:embed web/vpn-selector-reconcile.js
var vpnSelectorReconcileBase []byte

//go:embed web/vpn-selector-modal-search.js
var vpnSelectorModalSearchPatch []byte

var vpnSelectorReconcileAsset = append(append(append([]byte{}, vpnSelectorReconcileBase...), '\n'), vpnSelectorModalSearchPatch...)
