package main

// automationUIBinary keeps the Settings v3 scheduler on the same canonical
// executable path as the existing AUTO VPN scheduler.
func automationUIBinary() string {
	return automationRunnerPath()
}
