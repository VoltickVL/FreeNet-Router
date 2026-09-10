package main

import "testing"

func TestBestServerSpeedtestPreflightAcceptsOnlyUseful2xxPayload(t *testing.T) {
	cases := []struct {
		name string
		out  string
		want bool
	}{
		{name: "ok", out: "200\t64000", want: true},
		{name: "too small", out: "200\t1024", want: false},
		{name: "redirect", out: "302\t64000", want: false},
		{name: "forbidden", out: "403\t64000", want: false},
		{name: "garbage", out: "oops", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseBestServerSpeedtestPreflight(tc.out); got != tc.want {
				t.Fatalf("parseBestServerSpeedtestPreflight(%q)=%v want %v", tc.out, got, tc.want)
			}
		})
	}
}

func TestBestServerSpeedtestPreflightUsesBoundedSmallerTransfer(t *testing.T) {
	server := bestServerSpeedtestServer{URL: "https://speed.example:8080/speedtest/upload.php"}
	got := bestServerSpeedtestDownloadURLForSize(server, bestServerSpeedtestPreflightBytes, 9)
	if got != "https://speed.example:8080/download?size=256000&nocache=9" {
		t.Fatalf("unexpected preflight URL: %s", got)
	}
	full := bestServerSpeedtestDownloadURL(server, 9)
	if got == full {
		t.Fatal("preflight transfer must be smaller than aggregate measurement transfer")
	}
}
