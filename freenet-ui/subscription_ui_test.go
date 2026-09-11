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
		"https://••••••••••••••••••••",
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
