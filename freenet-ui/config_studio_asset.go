package main

import _ "embed"

//go:embed web/config-studio-parity.js
var configStudioParityAsset []byte

//go:embed web/config-studio-ux.js
var configStudioUXAsset []byte
