package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNativeDNSProviderDefaultsToYandexForLegacyConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "freenet.conf")
	if err := os.WriteFile(path, []byte("ISP_ID=rostelecom\nDNS_MODE=firmware\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := readNativeDNSProvider(path); got != nativeDNSProviderYandexBasic {
		t.Fatalf("provider=%q", got)
	}
}

func TestWriteNetworkProfileConfigWithNativeProviderPreservesUnknownKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "freenet.conf")
	before := "# keep\nISP_ID=vladlink\nDNS_MODE=xkeen\nSOME_RUNTIME_FLAG=preserved\n"
	if err := os.WriteFile(path, []byte(before), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeNetworkProfileConfigWithNativeProvider(path, "rostelecom", "firmware", nativeDNSProviderRouterCurrent); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	for _, want := range []string{
		"ISP_ID=rostelecom",
		"DNS_MODE=firmware",
		"NATIVE_DNS_PROVIDER=router-current",
		"SOME_RUNTIME_FLAG=preserved",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %q", want, got)
		}
	}
}

func TestNativeDNSProviderCatalogContainsOnlyConfirmedInitialChoices(t *testing.T) {
	options := nativeDNSProviderOptions()
	if len(options) != 2 {
		t.Fatalf("options=%v", options)
	}
	if options[0].ID != nativeDNSProviderRouterCurrent || options[1].ID != nativeDNSProviderYandexBasic {
		t.Fatalf("unexpected catalog=%v", options)
	}
	if strings.Join(options[1].Addresses, ",") != "77.88.8.8,77.88.8.1" {
		t.Fatalf("Yandex addresses=%v", options[1].Addresses)
	}
}
