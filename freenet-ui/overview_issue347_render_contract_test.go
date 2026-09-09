package main

import (
    "os"
    "strings"
    "testing"
)

func TestIssue347ApprovedOverviewRenderContract(t *testing.T) {
    data, err := os.ReadFile("web/operation-coordinator.js")
    if err != nil {
        t.Fatal(err)
    }
    s := string(data)
    mustContain := []string{
        "Issue #347: approved Overview render contract",
        "rememberedEndpoint",
        "rememberedFlag",
        "Ручной выбор Extra-профиля",
        "Обновить endpoint",
        "Новый IP в этой же локации",
        "Подобрать варианты",
        "fn-current-connected",
        "fn-incomplete",
        "Проверка не завершена",
        "Использовать",
        ".vpn-option.fn-incomplete",
        ".vpn-best .vpn-option-apply",
        "Проверка VPN, сравнение серверов и рекомендации",
    }
    for _, needle := range mustContain {
        if !strings.Contains(s, needle) {
            t.Fatalf("issue #347 Overview render contract missing %q", needle)
        }
    }
}
