package main

import (
 "os"
 "strings"
 "testing"
)

// The journal must surface the *existing* authenticated read-only doctor.
// It must never invent a browser-side unlock or a stylesheet override.
func TestJournalVPNTransactionDoctorReadOnlyUI(t *testing.T) {
 data, err := os.ReadFile("web/settings-v3-core.js")
 if err != nil { t.Fatal(err) }
 src := string(data)
 for _, expected := range []string{
  "fn3VPNTransactionDoctor",
  "/api/vpn/transaction-doctor",
  "function loadVPNTransactionDoctor()",
  "void loadVPNTransactionDoctor();",
  "data.mutation !== 'NONE'",
  "data.mutation_blocked === true",
  "data.checkpoint !== 'absent'",
  "Никакие переключения VPN не выполняйте",
  "Исходящие:",
  "Xray:",
  "Решение:",
  "role=\"status\"",
 } {
  if !strings.Contains(src, expected) { t.Fatalf("missing doctor UI contract: %q", expected) }
 }
 if strings.Contains(src, "fetch('/api/vpn/transaction-doctor', {method:'POST'") {
  t.Fatal("transaction doctor must not mutate")
 }
 if strings.Contains(src, "vpn-transaction.pending") {
  t.Fatal("private checkpoint path leaked into browser code")
 }
}
