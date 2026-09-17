package main

import _ "embed"

//go:embed web/config-studio-parity.js
var configStudioParityAsset []byte

//go:embed web/config-studio-ux.js
var configStudioUXAsset []byte

//go:embed web/xray-core-manager.js
var xrayCoreManagerAsset []byte

func init() {
	configStudioParityAsset = append(configStudioParityAsset, '\n')
	configStudioParityAsset = append(configStudioParityAsset, configStudioUXAsset...)
	configStudioParityAsset = append(configStudioParityAsset, '\n')
	configStudioParityAsset = append(configStudioParityAsset, xrayCoreManagerAsset...)
}
