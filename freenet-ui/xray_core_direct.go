package main

import (
    "net"
    "net/http"
    "syscall"
    "time"
)

const xrayCoreDirectSocketMark = 255

// Core/catalog downloads originate ON the router, not inside a VPN tunnel.
// Explicitly bypass HTTP_PROXY/HTTPS_PROXY and stamp the socket with the same
// bypass mark as FreeNet's isolated Xray probes. If the OS cannot establish a
// marked socket, do not silently fall back to an unmarked connection.
func newXrayCoreDirectHTTPClient() *http.Client {
    dialer := &net.Dialer{
        Timeout: 15 * time.Second,
        KeepAlive: 30 * time.Second,
        Control: func(_ string, _ string, raw syscall.RawConn) error {
            var markErr error
            err := raw.Control(func(fd uintptr) {
                markErr = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_MARK, xrayCoreDirectSocketMark)
            })
            if err != nil { return err }
            return markErr
        },
    }
    tr := http.DefaultTransport.(*http.Transport).Clone()
    tr.Proxy = nil
    tr.DialContext = dialer.DialContext
    return &http.Client{Timeout: 55 * time.Second, Transport: tr}
}
