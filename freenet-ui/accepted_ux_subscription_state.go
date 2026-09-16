package main

import "strings"

const acceptedUXSubscriptionConfiguredLegacy = `  function subscriptionConfigured() {
    const text = qs('#subscriptionState')?.textContent || '';
    return /Настроена|Активна/i.test(text) && !/Не настроена/i.test(text);
  }`

const acceptedUXSubscriptionConfiguredAuthoritative = `  function subscriptionConfigured() {
    return !!(lastStatus && lastStatus.subscription_configured === true);
  }`

// accepted-ux.js is embedded as one canonical browser asset. Keep the accepted
// subscription card tied to the backend status instead of re-interpreting the
// text that the card itself rendered on the previous sync. The exact-source
// guard is covered by tests so asset drift cannot silently regress this fix.
func patchAcceptedUXSubscriptionConfigured(source string) string {
	if strings.Count(source, acceptedUXSubscriptionConfiguredLegacy) != 1 {
		return source
	}
	return strings.Replace(source, acceptedUXSubscriptionConfiguredLegacy, acceptedUXSubscriptionConfiguredAuthoritative, 1)
}

func init() {
	acceptedUXJS = patchAcceptedUXSubscriptionConfigured(acceptedUXJS)
}
