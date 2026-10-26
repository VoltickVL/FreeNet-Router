package main

import (
    "net/http"
    "os"
    "strings"
    "testing"
)

func TestXrayCoreHTTPTransportExplicitDirect(t *testing.T) {
    client := newXrayCoreDirectHTTPClient()
    transport, ok := client.Transport.(*http.Transport)
    if !ok || transport.DialContext == nil {
        t.Fatal("Xray core update must use its own marked router-originated dialer")
    }
    if transport.Proxy != nil {
        t.Fatal("Xray core update must never inherit HTTP_PROXY/HTTPS_PROXY")
    }
    if xrayCoreDirectSocketMark != 255 {
        t.Fatal("DIRECT socket mark must match the isolated Xray probe mark")
    }
    // Read-only source contract: a future transport rewrite must not silently
    // remove the kernel bypass mark while keeping the same external shape.
    source, err := os.ReadFile("xray_core_direct.go")
    if err != nil { t.Fatal(err) }
    for _, want := range []string{
        "syscall.SOL_SOCKET, syscall.SO_MARK, xrayCoreDirectSocketMark",
        "tr.Proxy = nil",
        "tr.DialContext = dialer.DialContext",
        "return markErr",
    } {
        if !strings.Contains(string(source), want) {
            t.Fatalf("DIRECT update transport missing %q", want)
        }
    }
}
