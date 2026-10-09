package main

import (
    "errors"
    "os"
    "strings"
)

// This is a router-local, protected, credential-bearing transaction directory.
// It is NOT an API resource and must never be served, logged or cleared from a
// browser request. A pending checkpoint is a safety STOP regardless of the
// original shell helper PID (which may have died on Go timeout or reboot).
const defaultProviderTransactionDir = "/opt/var/lib/freenet/provider-transaction"

func providerTransactionDir() string {
    if s := strings.TrimSpace(os.Getenv("FREENET_PROVIDER_TX_DIR")); s != "" {
        return s
    }
    return defaultProviderTransactionDir
}

func providerTransactionPending() bool {
    _, err := os.Lstat(providerTransactionDir())
    return err == nil || !errors.Is(err, os.ErrNotExist)
}
