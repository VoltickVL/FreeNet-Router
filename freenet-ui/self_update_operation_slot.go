package main

import "sync"

// Read-only HTTP endpoints remain available while update admission excludes
// current and future in-process Xray/network mutations. The shell also owns
// the shared cross-process network lock for cron/other helper processes.
func (a *app) reserveApplicationUpdateSlot() (func(), bool) {
	if a.sem == nil {
		return func() {}, true
	}
	select {
	case a.sem <- struct{}{}:
		var once sync.Once
		return func() { once.Do(func() { <-a.sem }) }, true
	default:
		return nil, false
	}
}
