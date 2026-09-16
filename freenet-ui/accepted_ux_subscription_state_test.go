package main

import (
	"strings"
	"testing"
)

func TestAcceptedUXSubscriptionConfiguredUsesBackendStatus(t *testing.T) {
	if strings.Contains(acceptedUXJS, acceptedUXSubscriptionConfiguredLegacy) {
		t.Fatal("served accepted UX still derives subscription state from its own DOM text")
	}
	if !strings.Contains(acceptedUXJS, acceptedUXSubscriptionConfiguredAuthoritative) {
		t.Fatal("served accepted UX does not use authoritative subscription_configured status")
	}
}

func TestAcceptedUXSubscriptionConfiguredPatchIsExact(t *testing.T) {
	source := "before\n" + acceptedUXSubscriptionConfiguredLegacy + "\nafter"
	patched := patchAcceptedUXSubscriptionConfigured(source)
	if strings.Count(patched, acceptedUXSubscriptionConfiguredAuthoritative) != 1 {
		t.Fatalf("expected exactly one authoritative subscription state function: %q", patched)
	}
	if strings.Contains(patched, acceptedUXSubscriptionConfiguredLegacy) {
		t.Fatal("legacy self-referential subscription state function survived patch")
	}
	if patched != "before\n"+acceptedUXSubscriptionConfiguredAuthoritative+"\nafter" {
		t.Fatal("patch changed content outside the targeted function")
	}
}

func TestAcceptedUXSubscriptionConfiguredPatchFailsClosedOnDrift(t *testing.T) {
	source := "unrelated accepted UX"
	if got := patchAcceptedUXSubscriptionConfigured(source); got != source {
		t.Fatalf("unexpected mutation when canonical source guard does not match: %q", got)
	}
}
