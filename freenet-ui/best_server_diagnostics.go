package main

import (
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// Only numeric curl -w fields are exposed. Never return stderr, URLs,
// outbound config, or an arbitrary subprocess error to the API.
func bestServerTransferIssue(output string, err error) string {
	fields := strings.Fields(output)
	code, bytes := 0, int64(0)
	if len(fields) >= 2 {
		if n, e := strconv.Atoi(fields[0]); e == nil && n >= 100 && n <= 599 { code = n }
		if n, e := strconv.ParseFloat(fields[1], 64); e == nil && n >= 0 && n <= 1024*1024*1024 { bytes = int64(n) }
	}
	exitCode := -1
	var exitError *exec.ExitError
	if err == nil { exitCode = 0 } else if errors.As(err, &exitError) { exitCode = exitError.ExitCode() }
	return fmt.Sprintf("HTTP %d; curl %d; получено %d байт", code, exitCode, bytes)
}

func bestServerRejectionReasons(c bestServerQualityCandidate) []string {
	if !c.Tested { return []string{"Полная проверка качества не выполнялась"} }
	if !c.Available { return []string{"Не подтверждён HTTP-отклик через VPN"} }
	var reasons []string
	if c.DownloadMbps <= 0 { reasons = append(reasons, "Скорость не измерена") } else if c.DownloadMbps < 20 { reasons = append(reasons, "Загрузка ниже 20 Мбит/с") }
	if c.MediaSamples != bestServerMediaChunkRuns { reasons = append(reasons, fmt.Sprintf("Загрузок завершено %d/%d", c.MediaSamples, bestServerMediaChunkRuns)) }
	if c.MediaStalls > 0 { reasons = append(reasons, fmt.Sprintf("Провалов загрузки: %d", c.MediaStalls)) }
	if c.MediaGrade != "good" && c.MediaGrade != "excellent" { reasons = append(reasons, "Стабильность загрузки не подтверждена") }
	if c.ServiceTotal < 3 || c.ServiceOK != c.ServiceTotal { reasons = append(reasons, fmt.Sprintf("Проверки сайтов: %d/%d", c.ServiceOK, c.ServiceTotal)) }
	if c.JitterMS > bestServerQualityHighJitterMS || c.TCPJitterMS > bestServerQualityHighTCPJitterMS { reasons = append(reasons, "Высокие колебания задержки") }
	return reasons
}
