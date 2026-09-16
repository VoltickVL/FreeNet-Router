package main

import (
	"os"
	"strings"
	"testing"
)

func TestAcceptedUXSubscriptionConfiguredUsesBackendStatus(t *testing.T) {
	data, err := os.ReadFile("web/accepted-ux.js")
	if err != nil {
		t.Fatal(err)
	}
	source := string(data)
	authoritative := `function subscriptionConfigured() {
    return !!(lastStatus && lastStatus.subscription_configured === true);
  }`
	legacy := `const text = qs('#subscriptionState')?.textContent || '';`
	if strings.Count(source, authoritative) != 1 {
		t.Fatal("accepted UX must contain exactly one authoritative subscriptionConfigured function")
	}
	if strings.Contains(source, legacy) {
		t.Fatal("accepted UX still derives subscription configured state from its own DOM text")
	}
}
