package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"time"
)

// The network controller invokes this bounded read-only check only AFTER the
// candidate Xray starts and BEFORE committing a Split DNS transaction. BusyBox
// nslookup on Keenetic cannot ask for TXT/MX, and its A-only success was hiding
// empty DNS answers. A local TCP DNS query also avoids firmware LAN interception.
type splitDNSRecordResolver interface {
	LookupTXT(context.Context, string) ([]string, error)
	LookupMX(context.Context, string) ([]*net.MX, error)
}

func splitDNSCheckNonIPRecords(ctx context.Context, resolver splitDNSRecordResolver) error {
	txt, err := resolver.LookupTXT(ctx, "_dmarc.google.com")
	if err != nil {
		return errors.New("TXT query to Xray DNS failed")
	}
	txtFound := false
	for _, value := range txt {
		if strings.TrimSpace(value) != "" {
			txtFound = true
			break
		}
	}
	if !txtFound {
		return errors.New("TXT query to Xray DNS returned no records")
	}

	mx, err := resolver.LookupMX(ctx, "gmail.com")
	if err != nil {
		return errors.New("MX query to Xray DNS failed")
	}
	mxFound := false
	for _, record := range mx {
		if record != nil && strings.TrimSpace(record.Host) != "" {
			mxFound = true
			break
		}
	}
	if !mxFound {
		return errors.New("MX query to Xray DNS returned no records")
	}
	return nil
}

func runSplitDNSRecordAcceptance() int {
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()

	dialer := &net.Dialer{Timeout: 3 * time.Second}
	resolver := &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
			// Xray must own loopback :53; no public resolver/direct fallback.
			// Force TCP to avoid firmware DNS interception on client LAN.
			return dialer.DialContext(ctx, "tcp", "127.0.0.1:53")
		},
	}
	if err := splitDNSCheckNonIPRecords(ctx, resolver); err != nil {
		fmt.Fprintln(os.Stderr, "DNS_NONIP_ACCEPT=FAILED:", err)
		return 1
	}
	fmt.Println("DNS_NONIP_ACCEPT=SUCCESS")
	return 0
}

func init() {
	if len(os.Args) > 1 && os.Args[1] == "dns-rr-accept" {
		if len(os.Args) != 2 {
			os.Exit(2)
		}
		os.Exit(runSplitDNSRecordAcceptance())
	}
}
