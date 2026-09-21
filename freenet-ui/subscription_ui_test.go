package main

import (
	"os"
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
		"/api/settings-v3/action",
		"profiles_available",
		"applySubscriptionActionCatalog",
		"refreshSubscriptionScheduleState",
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
		"await window.loadNetworkPlan()",
		"await window.refreshProfiles()",
	} {
		if strings.Contains(js, forbidden) {
			t.Fatalf("subscription UX contains forbidden secret surface %q", forbidden)
		}
	}
}


func TestSubscriptionUpdaterCardUsesAuthoritativeScheduleResult(t *testing.T) {
	index, err := webFS.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(index), "subscriptionUpdaterState').textContent=s.updater_busy?'обновляется':'готово'") {
		t.Fatal("subscription updater card must not map generic updater idle state to «готово»")
	}
	cacheJS, err := os.ReadFile("web/topbar-settings-profile-cache.js")
	if err != nil {
		t.Fatal(err)
	}
	text := string(cacheJS)
	for _, want := range []string{"#subscriptionUpdaterState", "Актуально", "Ожидает проверки", "Ошибка"} {
		if !strings.Contains(text, want) {
			t.Fatalf("authoritative subscription state renderer missing %q", want)
		}
	}
}
