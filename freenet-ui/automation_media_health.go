package main

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

const (
	// A media-like body transfer is not a user Instagram session. Its two
	// independent samples are a bounded throughput/TTFB *risk signal*, never
	// proof that a Reel played or that the VPN is disconnected.
	automationMediaSegmentBytes      int64 = 512 * 1024
	automationMediaSegmentCount            = 2
	automationMediaSegmentTimeout          = 3 * time.Second
	// Never send more than 1 MiB per regular health observation. A pair of
	// completed samples is required to suppress single-transfer fluctuations.
	automationMediaSlowMbps                = 8.0
	automationMediaHighTTFB                = 900 * time.Millisecond
	automationMediaMinSpan                 = 30 * time.Second
	automationMediaRetryCooldown           = 10 * time.Minute
)

type automationMediaSegment struct {
	Mbps      float64
	TTFB      time.Duration
	Valid     bool
	Degraded  bool
}

type automationMediaEvidence struct {
	Complete  bool
	Degraded  bool
	MinMbps   float64
	Samples   int
}

// Only accept a fully received 512 KiB body from HTTP 200 with consistent
// transfer timings. HTTP 4xx, redirects, partial bodies and a timed-out tool
// are UNKNOWN, not confirmation of a bad route or an automatic-switch trigger.
func parseAutomationMediaSegment(output string, expectedBytes int64) automationMediaSegment {
	fields := strings.Fields(output)
	if len(fields) != 5 || fields[0] != "200" || expectedBytes <= 0 {
		return automationMediaSegment{}
	}
	bytes, err := strconv.ParseFloat(fields[1], 64)
	if err != nil || bytes < float64(expectedBytes)*0.99 || bytes > float64(expectedBytes)*1.01 {
		return automationMediaSegment{}
	}
	pre, errPre := strconv.ParseFloat(fields[2], 64)
	start, errStart := strconv.ParseFloat(fields[3], 64)
	total, errTotal := strconv.ParseFloat(fields[4], 64)
	if errPre != nil || errStart != nil || errTotal != nil || pre < 0 || start < pre || total <= start ||
		math.IsNaN(pre) || math.IsNaN(start) || math.IsNaN(total) ||
		math.IsInf(pre, 0) || math.IsInf(start, 0) || math.IsInf(total, 0) {
		return automationMediaSegment{}
	}
	mbps := bytes * 8 / (total-start) / 1_000_000
	if mbps <= 0 || math.IsNaN(mbps) || math.IsInf(mbps, 0) {
		return automationMediaSegment{}
	}
	ttfb := time.Duration(math.Round((start-pre)*float64(time.Second)))
	if ttfb < 0 {
		return automationMediaSegment{}
	}
	return automationMediaSegment{
		Mbps: mbps,
		TTFB: ttfb,
		Valid: true,
		Degraded: mbps < automationMediaSlowMbps || ttfb >= automationMediaHighTTFB,
	}
}

type automationMediaSegmentRunner func(context.Context, string, string, int) automationMediaSegment

// This GET is routed only through the isolated SOCKS outbound of the exact
// currently measured VPN. It deliberately does NOT use a PC default route
// or claim to sample Instagram's private per-session video CDN.
func runAutomationMediaSegment(ctx context.Context, curlPath, socks string, index int) automationMediaSegment {
	if ctx == nil || ctx.Err() != nil || index < 0 || index >= automationMediaSegmentCount {
		return automationMediaSegment{}
	}
	target := fmt.Sprintf("%s?bytes=%d&nonce=%d",
		bestServerFallbackSpeedURL, automationMediaSegmentBytes, time.Now().UTC().UnixNano()+int64(index))
	out, err := runBestServerSpeedtestCurl(ctx, curlPath, automationMediaSegmentTimeout,
		"--socks5-hostname", socks,
		"-sS", "--connect-timeout", "2",
		"-H", "Cache-Control: no-cache",
		"-o", "/dev/null",
		"-w", "%{http_code}\t%{size_download}\t%{time_pretransfer}\t%{time_starttransfer}\t%{time_total}",
		target)
	if err != nil || ctx.Err() != nil {
		return automationMediaSegment{}
	}
	return parseAutomationMediaSegment(string(out), automationMediaSegmentBytes)
}

func probeAutomationMediaSegmentsWith(ctx context.Context, curlPath, socks string, runner automationMediaSegmentRunner) automationMediaEvidence {
	if ctx == nil || ctx.Err() != nil || runner == nil || strings.TrimSpace(curlPath) == "" || strings.TrimSpace(socks) == "" {
		return automationMediaEvidence{}
	}
	evidence := automationMediaEvidence{}
	slow := 0
	for i := 0; i < automationMediaSegmentCount; i++ {
		if ctx.Err() != nil {
			return automationMediaEvidence{}
		}
		sample := runner(ctx, curlPath, socks, i)
		if !sample.Valid || sample.Mbps <= 0 || math.IsNaN(sample.Mbps) || math.IsInf(sample.Mbps, 0) || ctx.Err() != nil {
			return automationMediaEvidence{}
		}
		if evidence.MinMbps == 0 || sample.Mbps < evidence.MinMbps {
			evidence.MinMbps = sample.Mbps
		}
		if sample.Degraded {
			slow++
		}
		evidence.Samples++
	}
	// A mixed sample is not a known-good or confirmed-degraded media path.
	// Never transform a one-off dip into an automatic candidate scan.
	if slow > 0 && slow < automationMediaSegmentCount {
		return automationMediaEvidence{}
	}
	evidence.Complete = true
	evidence.Degraded = slow == automationMediaSegmentCount
	return evidence
}

func probeAutomationMediaSegments(ctx context.Context, curlPath, socks string) automationMediaEvidence {
	return probeAutomationMediaSegmentsWith(ctx, curlPath, socks, runAutomationMediaSegment)
}

 
// Current VPN "reachable" and video-like transfer quality are independent.
// A media signal can start a measured comparison, never global outage recovery.
func classifyAutomationMediaQuality(base automationHealthProbe, media automationMediaEvidence) automationHealthProbe {
	base.MediaComplete = media.Complete && media.Samples == automationMediaSegmentCount
	if !base.MediaComplete {
		// Preserve a genuine named HTTPS/latency observation, but never turn
		// the absence of a download sample into "video works" or "VPN dead".
		if base.State == automationHealthHealthy {
			base.Reason = "VPN HTTPS доступен; качество передачи видеосегментов пока не подтверждено. Это не доказательство отсутствия буферизации Instagram."
		}
		return base
	}
	base.MediaDegraded = media.Degraded
	if !media.Degraded {
		return base
	}
	// This is a surrogate media transfer over the exact VPN, not an
	// authenticated Instagram playback test. It cannot label the app "down".
	base.State = automationHealthUncertain
	base.QualityDegraded = true
	base.Reason = fmt.Sprintf("VPN отвечает, но два контрольных видеосегмента передаются медленно или с высоким TTFB (минимум %.1f Мбит/с). AUTO VPN требует повторных наблюдений и проверенной замены; воспроизведение Instagram напрямую не измерялось.", media.MinMbps)
	return base
}


const (
	automationMediaMinObservations = 4
	automationMediaMinimumSpan = 2 * time.Minute
)

func clearAutomationMediaEvidence(updates map[string]string) {
	updates["QUALITY_MEDIA_IDENTITY"] = ""
	updates["QUALITY_MEDIA_COUNT"] = "0"
	updates["QUALITY_MEDIA_SINCE"] = ""
	updates["QUALITY_MEDIA_LAST"] = ""
}

// The media trigger is a persisted, identity-bound, four-observation signal.
// One short transfer, a 403, a failed curl, or an unrelated service mismatch
// must not select a new VPN. Unknown samples preserve evidence but cannot
// contribute to its count. A verified good sample clears the old sequence.
func automationMediaQualityOptimizationPlan(state map[string]string, now time.Time, probe automationHealthProbe) (map[string]string, bool) {
	updates := map[string]string{}
	if probe.MediaComplete && !probe.MediaDegraded {
		clearAutomationMediaEvidence(updates)
		return updates, false
	}
	if !probe.MediaComplete || !probe.MediaDegraded ||
		probe.State != automationHealthUncertain || !probe.QualityDegraded ||
		!automationServiceProbeComplete(probe) || now.IsZero() {
		return updates, false
	}
	now = now.UTC()
	count, countErr := strconv.Atoi(strings.TrimSpace(state["QUALITY_MEDIA_COUNT"]))
	since, sinceErr := time.Parse(time.RFC3339Nano, state["QUALITY_MEDIA_SINCE"])
	last, lastErr := time.Parse(time.RFC3339Nano, state["QUALITY_MEDIA_LAST"])
	valid := state["QUALITY_MEDIA_IDENTITY"] == probe.CurrentIdentity &&
		countErr == nil && count > 0 && count <= automationMediaMinObservations &&
		sinceErr == nil && lastErr == nil && !last.Before(since) &&
		!now.Before(since) && now.Sub(since) <= automationQualityStrikeWindow
	if valid && !now.After(last) {
		return updates, false // replay, stale probe, and future timestamps
	}
	if !valid {
		count = 0
		since = now
	}
	count++
	if count > automationMediaMinObservations {
		count = automationMediaMinObservations
	}
	updates["QUALITY_MEDIA_IDENTITY"] = probe.CurrentIdentity
	updates["QUALITY_MEDIA_COUNT"] = strconv.Itoa(count)
	updates["QUALITY_MEDIA_SINCE"] = since.Format(time.RFC3339Nano)
	updates["QUALITY_MEDIA_LAST"] = now.Format(time.RFC3339Nano)

	if raw := strings.TrimSpace(state["QUALITY_OPTIMIZATION_LAST"]); raw != "" {
		previous, err := time.Parse(time.RFC3339, raw)
		if err != nil || now.Before(previous.Add(automationQualityOptimizationCooldown)) {
			return updates, false
		}
	}
	if count < automationMediaMinObservations || now.Sub(since) < automationMediaMinimumSpan {
		return updates, false
	}
	clearAutomationMediaEvidence(updates)
	updates["QUALITY_OPTIMIZATION_LAST"] = now.Format(time.RFC3339)
	return updates, true
}
