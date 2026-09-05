package main

import "strings"

func classifyApplyFailure(output string) (string, string) {
	primary := ""
	rollback := "UNKNOWN"
	preflightDetail := ""
	for _, part := range strings.Split(output, " | ") {
		line := strings.TrimSpace(part)
		if i := strings.Index(line, "[FreeNet Network] ERROR:"); i >= 0 {
			detail := strings.TrimSpace(line[i+len("[FreeNet Network] ERROR:"):])
			if detail != "" && !strings.HasPrefix(detail, "PRIMARY ERROR:") && !strings.HasPrefix(detail, "ROLLBACK ERROR/STATE:") {
				preflightDetail = detail
			}
		}
		if i := strings.Index(line, "PRIMARY ERROR:"); i >= 0 {
			primary = strings.TrimSpace(line[i+len("PRIMARY ERROR:"):])
		}
		switch {
		case strings.Contains(line, "ROLLBACK ERROR/STATE: FAILED/UNKNOWN"):
			rollback = "FAILED/UNKNOWN"
		case strings.Contains(line, "ROLLBACK ERROR/STATE: rollback success"):
			rollback = "SUCCESS"
		case strings.Contains(line, "ROLLBACK ERROR/STATE: no live apply"):
			rollback = "NOT_APPLIED"
		case strings.Contains(line, "rollback success/no live apply"):
			rollback = "SUCCESS_OR_NOT_APPLIED"
		}
	}
	if preflightDetail != "" && (primary == "native DNS preflight failed before mutation" || primary == "Split DNS preflight failed before mutation") {
		primary += ": " + preflightDetail
	}
	return primary, rollback
}
