package main

import (
    "os"
    "strings"
    "testing"
)

func TestOperationCoordinatorHasNoSelfMutatingGlobalObserver(t *testing.T) {
    data, err := os.ReadFile("web/operation-coordinator.js")
    if err != nil {
        t.Fatal(err)
    }
    src := string(data)
    bad := "new MutationObserver(run).observe(document.documentElement, {subtree:true, childList:true, characterData:true})"
    if strings.Contains(src, bad) {
        t.Fatal("global self-mutating MutationObserver regression detected")
    }
    if strings.Contains(src, "Issue #353: v0.3.8 final UI polish") {
        t.Fatal("unsafe v0.3.8 runtime polish block must not be shipped")
    }
}
