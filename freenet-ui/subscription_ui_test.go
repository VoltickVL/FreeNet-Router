package main

import (
	"strings"
	"testing"
)

func TestAcceptedSubscriptionUXContract(t *testing.T) {
	b, err := webFS.ReadFile("web/accepted-ux.js")
	if err != nil {
		t.Fatal(err)
	}
	js := string(b)
	for _, required := range []string{
		"subscription-approved",
		"Статус подписки",
		"Extra-профили",
		"Следующее обновление",
		"Ключ-подписка",
		"Сохранить изменения",
		"Проверить подписку",
		"Обновить сейчас",
		"Последние обновления",
		"Информация",
		"Ключ сохранён — вставьте новый для замены",
		"Сохранённое значение никогда не выводится обратно",
		"AUTO VPN будет настраиваться отдельно",
		"input.type = 'password'",
		"input.removeAttribute('value')",
		"await window.loadNetworkPlan()",
	} {
		if !strings.Contains(js, required) {
			t.Fatalf("accepted subscription UX missing %q", required)
		}
	}
	if strings.Contains(js, "MutationObserver") {
		t.Fatal("subscription UX must not use MutationObserver")
	}
	for _, forbidden := range []string{
		"subscription_url",
		"storedSubscriptionURL",
		"copySubscription",
		"revealSubscription",
	} {
		if strings.Contains(js, forbidden) {
			t.Fatalf("subscription UX contains forbidden secret surface %q", forbidden)
		}
	}
}

func TestAcceptedShellDNSIconContract(t *testing.T) {
	b, err := webFS.ReadFile("web/accepted-ux.js")
	if err != nil {
		t.Fatal(err)
	}
	js := string(b)
	const acceptedDNSPath = `M3 12h18M12 3c2.4 2.5 3.6 5.5 3.6 9S14.4 18.5 12 21M12 3c-2.4 2.5-3.6 5.5-3.6 9S9.6 18.5 12 21`
	if !strings.Contains(js, acceptedDNSPath) {
		t.Fatal("accepted topbar DNS icon path changed")
	}
}
