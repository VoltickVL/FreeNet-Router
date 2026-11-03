package main

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
)

type splitDNSRecordMock struct {
	txt []string
	mx  []*net.MX
	txtErr error
	mxErr error
	txtCalls int
	mxCalls int
}

func (m *splitDNSRecordMock) LookupTXT(_ context.Context, _ string) ([]string, error) {
	m.txtCalls++
	return m.txt, m.txtErr
}
func (m *splitDNSRecordMock) LookupMX(_ context.Context, _ string) ([]*net.MX, error) {
	m.mxCalls++
	return m.mx, m.mxErr
}

func TestSplitDNSNonIPAcceptanceRequiresRealTXTAndMX(t *testing.T) {
	tests := []struct {
		name string
		mock splitDNSRecordMock
		want string
	}{
		{
			name: "real TXT and MX accepted",
			mock: splitDNSRecordMock{txt: []string{"v=DMARC1; p=reject"}, mx: []*net.MX{{Host: "mx.gmail.com."}}},
		},
		{
			name: "empty TXT NOERROR rejected",
			mock: splitDNSRecordMock{txt: nil, mx: []*net.MX{{Host: "mx.gmail.com."}}},
			want: "TXT query to Xray DNS returned no records",
		},
		{
			name: "whitespace TXT rejected",
			mock: splitDNSRecordMock{txt: []string{"  "}, mx: []*net.MX{{Host: "mx.gmail.com."}}},
			want: "TXT query to Xray DNS returned no records",
		},
		{
			name: "TXT error rejected",
			mock: splitDNSRecordMock{txtErr: errors.New("dial timeout"), mx: []*net.MX{{Host: "mx.gmail.com."}}},
			want: "TXT query to Xray DNS failed",
		},
		{
			name: "empty MX NOERROR rejected",
			mock: splitDNSRecordMock{txt: []string{"v=DMARC1"}, mx: nil},
			want: "MX query to Xray DNS returned no records",
		},
		{
			name: "missing MX host rejected",
			mock: splitDNSRecordMock{txt: []string{"v=DMARC1"}, mx: []*net.MX{{Host: ""}}},
			want: "MX query to Xray DNS returned no records",
		},
		{
			name: "MX error rejected",
			mock: splitDNSRecordMock{txt: []string{"v=DMARC1"}, mxErr: errors.New("dial timeout")},
			want: "MX query to Xray DNS failed",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := splitDNSCheckNonIPRecords(context.Background(), &tt.mock)
			if tt.want == "" {
				if err != nil { t.Fatalf("unexpected rejection: %v", err) }
				if tt.mock.txtCalls != 1 || tt.mock.mxCalls != 1 {
					t.Fatalf("missing real record queries: txt=%d mx=%d", tt.mock.txtCalls, tt.mock.mxCalls)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("want %q, got %v", tt.want, err)
			}
			if strings.HasPrefix(tt.want, "TXT") && tt.mock.mxCalls != 0 {
				t.Fatal("TXT error must not be hidden by a later successful MX query")
			}
		})
	}
}
