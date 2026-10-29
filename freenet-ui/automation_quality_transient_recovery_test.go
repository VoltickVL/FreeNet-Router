package main

import (
	"context"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

func persistentMediaGuardFixture(t *testing.T) (*automationQualityAdmission, bestServerInternalCandidate) {
	t.Helper()
	guard, selected := automationQualityAdmissionFixture(t)
	guard.trigger.LatencyDegraded = false
	guard.trigger.ServiceBadMask = 0
	guard.trigger.MediaDegraded = true
	baseline := &guard.baseline.Candidates[0]
	baseline.Current = true
	baseline.Tested = true
	baseline.Available = true
	baseline.HTTPSamples = bestServerQualityHTTPRequired
	baseline.Endpoint = guard.baseline.CurrentEndpoint
	c := &guard.candidate
	c.Current = false
	c.Tested = true
	c.Available = true
	c.Eligible = true
	c.ThroughputSource = bestServerThroughputAutoMedia
	c.HTTPSamples = bestServerQualityHTTPRequired
	c.MediaSamples = automationMediaSegmentCount
	c.MediaGrade = "good"
	c.MediaStalls = 0
	c.MediaMbps = 11
	c.DownloadMbps = 11
	return guard, selected
}

func healthyMediaCurrentAfterTransientRecovery(identity string) automationHealthProbe {
	return automationHealthProbe{
		State: automationHealthHealthy, CurrentIdentity: identity,
		ServiceComplete: true, ServiceOKMask: bestServerServiceAllMask,
		MediaComplete: true, MediaDegraded: false,
	}
}

func TestPersistentMediaIncidentNotCanceledBySingleTransientHealthySample(t *testing.T) {
	guard, selected := persistentMediaGuardFixture(t)
	oldCandidate, oldCurrent := automationQualityCandidateProbe, automationQualityCurrentProbe
	t.Cleanup(func() {
		automationQualityCandidateProbe, automationQualityCurrentProbe = oldCandidate, oldCurrent
	})
	calls := []string{}
	automationQualityCandidateProbe = func(_ *app, ctx context.Context, c bestServerInternalCandidate) automationHealthProbe {
		calls = append(calls, "candidate")
		if ctx.Err() != nil || c.Raw != selected.Raw || c.Profile.ID != selected.Profile.ID {
			t.Fatal("candidate confirmation was not bound to the measured profile")
		}
		p := automationQualityFreshCandidate()
		p.MediaComplete = true
		return p
	}
	automationQualityCurrentProbe = func(_ *app, ctx context.Context) automationHealthProbe {
		calls = append(calls, "current")
		if ctx.Err() != nil {
			t.Fatal("current confirmation deadline was exhausted")
		}
		return healthyMediaCurrentAfterTransientRecovery(guard.trigger.CurrentIdentity)
	}
	if err := guard.confirm(selected); err != nil || guard.stopped != nil || guard.confirmedAt.IsZero() {
		t.Fatalf("single transient recovery erased persisted media incident: err=%v stopped=%+v", err, guard.stopped)
	}
	if strings.Join(calls, ",") != "candidate,current" {
		t.Fatalf("unexpected/additional probes during confirmation: %v", calls)
	}
	if err := guard.final(); err != nil {
		t.Fatalf("existing writer identity and rollback admission unexpectedly rejected: %v", err)
	}
}

func TestPersistentMediaIncidentOnlyAllowsFullMeasuredUnchangedCandidate(t *testing.T) {
	now := time.Now().UTC()
	for _, tc := range []struct {
		name string
		mutate func(*automationQualityAdmission, *automationHealthProbe)
	}{
		{"non-media latency trigger", func(g *automationQualityAdmission, c *automationHealthProbe) {
			g.trigger.MediaDegraded = false
			g.trigger.LatencyDegraded = true
		}},
		{"mixed latency and media incident", func(g *automationQualityAdmission, c *automationHealthProbe) {
			g.trigger.LatencyDegraded = true
		}},
		{"mixed named-service and media incident", func(g *automationQualityAdmission, c *automationHealthProbe) {
			g.trigger.ServiceBadMask = 1
		}},
		{"candidate has not passed independent media ranking", func(g *automationQualityAdmission, c *automationHealthProbe) {
			g.candidate.Eligible = false
		}},
		{"candidate measured only aggregate capacity", func(g *automationQualityAdmission, c *automationHealthProbe) {
			g.candidate.ThroughputSource = bestServerThroughputStrictAggregate
		}},
		{"candidate media check incomplete", func(g *automationQualityAdmission, c *automationHealthProbe) {
			g.candidate.MediaSamples = 1
		}},
		{"candidate measured slow", func(g *automationQualityAdmission, c *automationHealthProbe) {
			g.candidate.MediaMbps = 3
		}},
		{"candidate has a media stall", func(g *automationQualityAdmission, c *automationHealthProbe) {
			g.candidate.MediaStalls = 1
		}},
		{"candidate not fully tested over HTTPS", func(g *automationQualityAdmission, c *automationHealthProbe) {
			g.candidate.HTTPSamples = 1
		}},
		{"current baseline unavailable", func(g *automationQualityAdmission, c *automationHealthProbe) {
			g.baseline.Candidates[0].Available = false
		}},
		{"current baseline not fully measured", func(g *automationQualityAdmission, c *automationHealthProbe) {
			g.baseline.Candidates[0].HTTPSamples = 0
		}},
		{"current baseline identity changed", func(g *automationQualityAdmission, c *automationHealthProbe) {
			g.baseline.CurrentIdentity = strings.Repeat("e", 64)
		}},
		{"current recheck identity changed", func(g *automationQualityAdmission, c *automationHealthProbe) {
			c.CurrentIdentity = strings.Repeat("f", 64)
		}},
		{"current media check missing", func(g *automationQualityAdmission, c *automationHealthProbe) {
			c.MediaComplete = false
		}},
		{"current media is still slow, not transient healthy", func(g *automationQualityAdmission, c *automationHealthProbe) {
			c.MediaDegraded = true
		}},
		{"current service explicitly fails", func(g *automationQualityAdmission, c *automationHealthProbe) {
			c.ServiceBadMask = 1
		}},
		{"manual choice still settling", func(g *automationQualityAdmission, c *automationHealthProbe) {
			g.lastSwitch = now.Add(-time.Minute).Format(time.RFC3339)
			g.lastSwitchOrigin = "manual"
		}},
		{"previous auto switch within antiflap interval", func(g *automationQualityAdmission, c *automationHealthProbe) {
			g.lastSwitch = now.Add(-time.Minute).Format(time.RFC3339)
			g.lastSwitchOrigin = "auto"
		}},
		{"unparseable last switch does not authorize recovery", func(g *automationQualityAdmission, c *automationHealthProbe) {
			g.lastSwitch = "not-a-timestamp"
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			guard, _ := persistentMediaGuardFixture(t)
			current := healthyMediaCurrentAfterTransientRecovery(guard.trigger.CurrentIdentity)
			tc.mutate(guard, &current)
			if guard.mediaEpisodeAllowsTransientCurrentRecovery(current, now) {
				t.Fatal("unconfirmed media episode bypassed current-recovered protection")
			}
		})
	}
}

func TestPersistentMediaTransientRecoveryActuallyReachesProtectedProviderApply(t *testing.T) {
	guard, _ := persistentMediaGuardFixture(t)
	callsPath := installAutomationQualityApplyFixture(t, guard)
	oldCandidate, oldCurrent := automationQualityCandidateProbe, automationQualityCurrentProbe
	t.Cleanup(func() {
		automationQualityCandidateProbe, automationQualityCurrentProbe = oldCandidate, oldCurrent
	})
	automationQualityCandidateProbe = func(_ *app, _ context.Context, _ bestServerInternalCandidate) automationHealthProbe {
		p := automationQualityFreshCandidate()
		p.MediaComplete = true
		return p
	}
	automationQualityCurrentProbe = func(_ *app, _ context.Context) automationHealthProbe {
		return healthyMediaCurrentAfterTransientRecovery(guard.trigger.CurrentIdentity)
	}
	status, result := guard.app.executeProviderProfileApplyWithAdmission(networkApplyRequest{
		Operation: "provider", ProfileID: guard.candidate.ID,
		SelectionToken: guard.token, Confirm: true,
	}, nil, &providerApplyAdmission{
		beforeQuiesce: guard.confirm,
		beforeMutation: guard.final,
	})
	calls, err := os.ReadFile(callsPath)
	if err != nil {
		t.Fatal(err)
	}
	if status != http.StatusOK || !result.Success || !result.Applied ||
		string(calls) != "plan\napply-core\n" ||
		len(guard.app.sem) != 0 || len(isolatedXrayProbeGate) != 0 {
		t.Fatalf("persistent media episode never reached normal protected provider writer: status=%d result=%+v calls=%q", status, result, string(calls))
	}
	if guard.stopped != nil || guard.confirmedAt.IsZero() {
		t.Fatalf("guard unexpectedly stopped an accepted recurrent media incident: %+v", guard.stopped)
	}
}
