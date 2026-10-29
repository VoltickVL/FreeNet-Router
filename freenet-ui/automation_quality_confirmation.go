package main

import (
	"context"
	"errors"
	"math/bits"
	"strings"
	"time"
)

const (
	automationQualityConfirmationTimeout = 2 * automationHealthProbeTimeout
	// The provider plan precedes the paired check; the exclusive drain follows
	// it. Only reachable-quality admission reserves these read-only stages.
	automationQualityAdmissionReserve = 35*time.Second + automationQualityConfirmationTimeout + 12*time.Second
)

var automationQualityCurrentProbe = func(a *app, ctx context.Context) automationHealthProbe {
	return a.probeAutomationCurrentVPN(ctx)
}

var automationQualityCandidateProbe = func(a *app, ctx context.Context, candidate bestServerInternalCandidate) automationHealthProbe {
	return a.probeAutomationQualityCandidate(ctx, candidate)
}

func validAutomationQualityTrigger(trigger automationQualityTrigger) bool {
	return validAutomationQualityIdentity(trigger.CurrentIdentity) &&
		trigger.ServiceBadMask&^bestServerServiceAllMask == 0 &&
		(trigger.LatencyDegraded || trigger.ServiceBadMask != 0 || trigger.MediaDegraded)
}

func (a *app) probeAutomationQualityCandidate(ctx context.Context, candidate bestServerInternalCandidate) automationHealthProbe {
	result := automationHealthProbe{State: automationHealthUncertain}
	probe := a.withBestServerCandidateSOCKS(ctx, candidate, func(probeCtx context.Context, curlPath, socks string) bestServerProbeResult {
		applicationMS, ok, _ := probeAutomationNamedHTTPSDetailed(probeCtx, curlPath, socks)
		if !ok || probeCtx.Err() != nil {
			return bestServerProbeResult{}
		}
		services := probeBestServerServiceReachabilityDetailed(probeCtx, curlPath, socks)
		if probeCtx.Err() != nil {
			return bestServerProbeResult{}
		}
		known := bits.OnesCount8(services.OKMask | services.BadMask)
		result = classifyAutomationReachableQuality(applicationMS, bits.OnesCount8(services.OKMask), known)
		result.ServiceComplete = services.Complete
		result.ServiceOKMask = services.OKMask
		result.ServiceBadMask = services.BadMask
		// Admission verifies the same bounded transfer against the candidate,
		// not just that Instagram's HEAD endpoint returned an HTTP response.
		media := probeAutomationMediaSegments(probeCtx, curlPath, socks)
		if probeCtx.Err() != nil {
			return bestServerProbeResult{}
		}
		result = classifyAutomationMediaQuality(result, media)
		return bestServerProbeResult{OK: true}
	})
	if !probe.OK || ctx.Err() != nil {
		return automationHealthProbe{State: automationHealthUncertain}
	}
	return result
}

// These guards run inside the provider writer's existing semaphore. The paired
// shared probes finish before the exclusive probe drain; the final guard does
// only bounded local reads and never reacquires either ownership gate.
type automationQualityAdmission struct {
	app              *app
	ctx              context.Context
	baseline         bestServerQualityResponse
	candidate        bestServerQualityCandidate
	token            string
	trigger          automationQualityTrigger
	lastSwitch       string
	lastSwitchOrigin string
	selectedRaw      string
	confirmedAt      time.Time
	stopped          *automationBestCycleResult
	// Only a beforeQuiesce candidate-only rejection may try a separately
	// measured fallback. An unknown apply, current drift or failed rollback
	// must never be retried.
	candidateRejected bool
}

func (guard *automationQualityAdmission) stop(result, reason string) error {
	guard.stopped = &automationBestCycleResult{
		Result: result, Reason: reason, ProfileID: guard.candidate.ID, RollbackState: "NOT_APPLIED",
	}
	return errors.New(reason)
}

func (guard *automationQualityAdmission) facts() error {
	if len(guard.baseline.Candidates) != 1 {
		return guard.stop("uncertain", "Полный замер текущего VPN отсутствует; автоматическое переключение отменено.")
	}
	if guard.ctx.Err() != nil {
		return guard.stop("uncertain", "Время безопасного сравнения истекло; автоматическое переключение отменено.")
	}
	if !validAutomationQualityTrigger(guard.trigger) ||
		guard.baseline.CurrentIdentity != guard.trigger.CurrentIdentity {
		return guard.stop("uncertain", "Исходная причина сравнения текущего VPN не подтверждена; переключение отменено.")
	}
	endpoint := readBestServerCurrentEndpoint(guard.app.cfg.OutPath)
	filter := readBestServerCurrentFilter(guard.app.cfg.FilterPath)
	if endpoint == "" || !endpointsEqual(endpoint, guard.baseline.CurrentEndpoint) ||
		bestServerCurrentQualityIdentityHash(endpoint, filter) != guard.trigger.CurrentIdentity {
		return guard.stop("uncertain", "Текущий VPN изменился во время сравнения; автоматическое переключение отменено.")
	}
	if automationMutationBlockedState() {
		return guard.stop("uncertain", "Защита восстановления VPN активна; автоматическое переключение отменено.")
	}
	updating, _, _ := guard.app.currentUpdateActivity()
	if automationPendingPostUpdateTarget(guard.app) != "" || updating || guard.app.updateLockHeld() {
		return guard.stop("uncertain", "Обновление FreeNet или его проверка ещё не завершены; автоматическое переключение отменено.")
	}
	settings := readAutomationSettings(guard.app.cfg.ConfigPath)
	if !settings.Enabled || settings.Mode != automationModeBest || !settings.AutoApply {
		return guard.stop("same", "Автоматическое переключение больше не разрешено текущими настройками.")
	}
	currentCountry := guard.baseline.Candidates[0].CountryCode
	if currentCountry == "" {
		currentCountry = automationCurrentCountry(guard.app)
	}
	if !automationCountryAllowed(settings, currentCountry, guard.candidate.CountryCode) {
		return guard.stop("same", "Выбранный VPN больше не разрешён текущими настройками стран; переключение отменено.")
	}
	state := parseAutomationState(automationStatePath())
	if state["LAST_SWITCH"] != guard.lastSwitch || state["LAST_SWITCH_ORIGIN"] != guard.lastSwitchOrigin {
		return guard.stop("same", "После начала сравнения уже выбрано другое подключение; автоматическое переключение отменено.")
	}
	return nil
}

// A media-quality alarm already requires two distinct, identity-bound slow
// observations. A several-minute Best Server comparison may briefly relieve
// congestion or encounter a fast CDN sample at its end. One healthy sample is
// therefore not proof that a recurrent video stall has been resolved.
//
// This exception is deliberately narrow: only the original MEDIA-ONLY alarm,
// a fully measured AUTO-media candidate, an intact current baseline, a fresh
// healthy final probe, and a cooldown-free exact profile can use it. Latency
// and named-service incidents retain the normal current-recovered STOP. No
// new probe, Xray restart or weakened provider/rollback admission is added.
func (guard *automationQualityAdmission) mediaEpisodeAllowsTransientCurrentRecovery(current automationHealthProbe, now time.Time) bool {
	if !guard.trigger.MediaDegraded || guard.trigger.LatencyDegraded ||
		guard.trigger.ServiceBadMask != 0 || !current.MediaComplete ||
		current.MediaDegraded || current.State != automationHealthHealthy ||
		current.QualityDegraded || current.ServiceBadMask != 0 ||
		current.CurrentIdentity != guard.trigger.CurrentIdentity ||
		guard.baseline.CurrentIdentity != guard.trigger.CurrentIdentity ||
		len(guard.baseline.Candidates) != 1 {
		return false
	}
	baseline := guard.baseline.Candidates[0]
	if !baseline.Current || !baseline.Tested || !baseline.Available ||
		baseline.HTTPSamples < bestServerQualityHTTPRequired ||
		baseline.Endpoint == "" ||
		!endpointsEqual(baseline.Endpoint, guard.baseline.CurrentEndpoint) {
		return false
	}
	c := guard.candidate
	if !c.Tested || !c.Available || !c.Eligible || c.Current ||
		c.ThroughputSource != bestServerThroughputAutoMedia ||
		c.HTTPSamples < bestServerQualityHTTPRequired ||
		c.MediaSamples != automationMediaSegmentCount ||
		c.MediaGrade != "good" || c.MediaStalls != 0 ||
		c.MediaMbps < automationMediaSlowMbps ||
		c.DownloadMbps < automationMediaSlowMbps {
		return false
	}
	// Even a persistent media episode cannot override recent manual intent or
	// immediately flip back after an automatic switch. Original GID/identity,
	// transaction and rollback gates still apply independently in the writer.
	if raw := strings.TrimSpace(guard.lastSwitch); raw != "" {
		last, err := time.Parse(time.RFC3339, raw)
		if err != nil || now.Before(last) {
			return false
		}
		cooldown := automationQualitySevereCooldown
		if guard.lastSwitchOrigin == "manual" {
			cooldown = automationManualSettlingWindow
		}
		if now.Before(last.Add(cooldown)) {
			return false
		}
	}
	return true
}

func (guard *automationQualityAdmission) confirm(selected bestServerInternalCandidate) error {
	if len(guard.baseline.Candidates) != 1 {
		return guard.stop("uncertain", "Полный замер текущего VPN отсутствует; автоматическое переключение отменено.")
	}
	if err := guard.facts(); err != nil {
		return err
	}
	if selected.Profile.ID != guard.candidate.ID || strings.TrimSpace(selected.Raw) == "" ||
		!endpointsEqual(profileEndpoint(selected.Profile), guard.candidate.Endpoint) ||
		selected.Profile.CountryCode != guard.candidate.CountryCode {
		return guard.stop("uncertain", "Измеренный VPN не совпадает с защищённым выбором; переключение отменено.")
	}
	guard.selectedRaw = selected.Raw
	ctx, cancel := context.WithTimeout(guard.ctx, automationQualityConfirmationTimeout)
	defer cancel()

	// Check the candidate first and the current route last. A current route that
	// recovered while the alternatives were scanned must keep its connection.
	candidateCtx, cancelCandidate := context.WithTimeout(ctx, automationHealthProbeTimeout)
	candidate := automationQualityCandidateProbe(guard.app, candidateCtx, selected)
	candidateErr := candidateCtx.Err()
	cancelCandidate()
	// Media-triggered replacement can pass a restricted HEAD, but never a
	// confirmed service failure or a slow/unknown data transfer. Existing
	// latency/service triggers retain their exact 4/4 service requirement.
	namedServicesOK := candidate.ServiceComplete && candidate.ServiceOKMask == bestServerServiceAllMask
	if guard.trigger.MediaDegraded {
		namedServicesOK = candidate.ServiceBadMask == 0 && candidate.MediaComplete && !candidate.MediaDegraded
	}
	if candidateErr != nil || ctx.Err() != nil || candidate.State != automationHealthHealthy ||
		candidate.QualityDegraded || !namedServicesOK {
		// This check occurs before quiescing the production Xray and before
		// any helper apply. Only this failure can permit one fallback attempt,
		// and only if the parent comparison budget remains alive.
		guard.candidateRejected = ctx.Err() == nil
		reason := "Кандидат не подтвердил качество; переключение не выполнялось."
		switch {
		case candidateErr != nil:
			reason = "Проверка качества кандидата не завершилась в отведённое время; переключение не выполнялось."
		case candidate.ServiceBadMask != 0:
			reason = "У кандидата обнаружены подтверждённые сбои сервисного HTTPS; переключение не выполнялось."
		case guard.trigger.MediaDegraded && !candidate.MediaComplete:
			reason = "Медиапроверка кандидата не дала двух достоверных сегментов; переключение не выполнялось."
		case guard.trigger.MediaDegraded && candidate.MediaDegraded:
			reason = "Медиапередача кандидата также замедлена; переключение не выполнялось."
		case candidate.QualityDegraded || candidate.State != automationHealthHealthy:
			reason = "Кандидат не подтвердил стабильную задержку и сервисы; переключение не выполнялось."
		}
		return guard.stop("uncertain", reason)
	}
	if err := guard.facts(); err != nil {
		return err
	}
	currentCtx, cancelCurrent := context.WithTimeout(ctx, automationHealthProbeTimeout)
	current := automationQualityCurrentProbe(guard.app, currentCtx)
	currentErr := currentCtx.Err()
	cancelCurrent()
	// Media evidence is a separate success/failure channel from anti-bot HEAD:
	// a complete media sample with the same VPN identity is sufficient for
	// that cause, while non-media triggers keep strict service completeness.
	currentEvidence := automationServiceProbeComplete(current)
	if guard.trigger.MediaDegraded && !guard.trigger.LatencyDegraded && guard.trigger.ServiceBadMask == 0 {
		currentEvidence = current.MediaComplete && validAutomationQualityIdentity(current.CurrentIdentity)
	}
	if currentErr != nil || ctx.Err() != nil || current.CurrentIdentity != guard.trigger.CurrentIdentity ||
		!currentEvidence {
		return guard.stop("uncertain", "Повторная проверка текущего VPN неоднозначна; переключение отменено.")
	}
	if guard.trigger.MediaDegraded && !current.MediaComplete {
		return guard.stop("uncertain", "Повторная медиапроверка текущего VPN не завершилась; нельзя подтвердить исчезновение или сохранение лагов.")
	}
	if current.State == automationHealthHealthy {
		if guard.mediaEpisodeAllowsTransientCurrentRecovery(current, time.Now().UTC()) {
			// A complete current sample is required above. It is not enough
			// to erase the persisted two-observation media episode when the
			// measured replacement is independently confirmed as healthy.
			// Existing exact-profile, writer, post-check and rollback guards
			// remain in charge of the actual cutover.
			appendAutomationRecoveryStage("quality_optimization", "transient_healthy",
				"Текущий VPN ответил на одной проверке, но повторная подтверждённая медиадеградация ещё актуальна; продолжаем безопасное применение измеренного кандидата.")
			guard.confirmedAt = time.Now()
			return nil
		}
		return guard.stop("same", "Текущий VPN восстановился на повторной проверке; переключение не требуется.")
	}
	if current.State != automationHealthUncertain || !current.QualityDegraded {
		return guard.stop("uncertain", "Текущий VPN не подтвердил исходное ухудшение качества; переключение отменено.")
	}
	sameLatency := guard.trigger.LatencyDegraded && current.QualityPoints > 0
	sameService := guard.trigger.ServiceBadMask != 0 && current.ServiceBadMask&guard.trigger.ServiceBadMask != 0
	sameMedia := guard.trigger.MediaDegraded && current.MediaComplete && current.MediaDegraded
	if !sameLatency && !sameService && !sameMedia {
		return guard.stop("same", "Исходное ухудшение качества текущего VPN больше не подтверждается; переключение не требуется.")
	}
	if err := guard.facts(); err != nil {
		return err
	}
	if ctx.Err() != nil {
		return guard.stop("uncertain", "Время повторной проверки VPN истекло; автоматическое переключение отменено.")
	}
	guard.confirmedAt = time.Now()
	return nil
}

func (guard *automationQualityAdmission) final() error {
	if guard.confirmedAt.IsZero() || time.Since(guard.confirmedAt) > automationHealthProbeTimeout {
		return guard.stop("uncertain", "Повторная проверка VPN устарела до применения; переключение отменено.")
	}
	if err := guard.facts(); err != nil {
		return err
	}
	endpoint := readBestServerCurrentEndpoint(guard.app.cfg.OutPath)
	filter := readBestServerCurrentFilter(guard.app.cfg.FilterPath)
	selected, manualRTT, err := guard.app.loadBestServerSelectionCandidateWithPurpose(guard.token, guard.candidate.ID, endpoint, filter)
	if err != nil || manualRTT || selected.Raw != guard.selectedRaw ||
		!endpointsEqual(profileEndpoint(selected.Profile), guard.candidate.Endpoint) {
		return guard.stop("uncertain", "Защищённый выбор VPN изменился или истёк до применения; переключение отменено.")
	}
	if guard.ctx.Err() != nil {
		return guard.stop("uncertain", "Время безопасного сравнения истекло до применения; переключение отменено.")
	}
	return nil
}
