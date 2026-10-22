package main

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func automationQualityAdmissionFixture(t *testing.T) (*automationQualityAdmission, bestServerInternalCandidate) {
	t.Helper()
	a := automationQoECooldownFixture(t, time.Hour, "auto")
	dir := filepath.Dir(a.cfg.ConfigPath)
	a.cfg.SubPath = filepath.Join(dir, "subscription.url")
	a.cfg.UpdateState = filepath.Join(dir, "update.state")
	a.cfg.UpdateLock = filepath.Join(dir, "update.lock")
	a.cfg.Timeout = 2 * time.Second
	a.cfg.ProviderTimeout = 5 * time.Second
	t.Setenv("FREENET_BEST_SELECTION_DIR", filepath.Join(dir, "selections"))
	const filter = "^de Current$"
	for path, value := range map[string]string{
		a.cfg.ConfigPath: "AUTO_VPN_V1=yes\nAUTO_VPN_MODE=best\nAUTO_VPN_AUTO_APPLY=yes\nAUTO_VPN_COUNTRY_SCOPE=region\n",
		a.cfg.SubPath: "https://provider.example.invalid/test-subscription\n",
		a.cfg.FilterPath: filter + "\n",
		a.cfg.OutPath: "{\"outbounds\":[{\"tag\":\"vless-reality\",\"settings\":{\"vnext\":[{\"address\":\"192.0.2.10\",\"port\":443}]}}]}\n",
	} {
		if err := os.WriteFile(path, []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	raw := strings.Split(strings.TrimSpace(testSubscriptionPlain), "\n")[0]
	profile, ok := parseSafeVLESSProfile(raw)
	if !ok {
		t.Fatal("invalid canonical test subscription profile")
	}
	selected := bestServerInternalCandidate{Profile: profile, Raw: raw}
	candidate := automationMeasuredCurrentFixture()
	candidate.ID, candidate.Name, candidate.CountryCode = profile.ID, profile.Name, profile.CountryCode
	candidate.Endpoint, candidate.Current = profileEndpoint(profile), false
	current := automationMeasuredCurrentFixture()
	current.Eligible, current.ApplicationMS = false, 350
	identity := bestServerCurrentQualityIdentityHash(current.Endpoint, filter)
	token, err := a.storeBestServerSelectionSnapshot(current.Endpoint, filter,
		[]bestServerInternalCandidate{selected}, []bestServerQualityCandidate{candidate})
	if err != nil {
		t.Fatal(err)
	}
	state := parseAutomationState(automationStatePath())
	return &automationQualityAdmission{
		app: a, ctx: context.Background(),
		baseline: bestServerQualityResponse{
			Success: true, CurrentEndpoint: current.Endpoint, CurrentIdentity: identity,
			Candidates: []bestServerQualityCandidate{current},
		},
		candidate: candidate, token: token,
		trigger: automationQualityTrigger{CurrentIdentity: identity, LatencyDegraded: true},
		lastSwitch: state["LAST_SWITCH"], lastSwitchOrigin: state["LAST_SWITCH_ORIGIN"],
	}, selected
}

func automationQualityFreshCandidate() automationHealthProbe {
	return automationHealthProbe{
		State: automationHealthHealthy, ServiceComplete: true, ServiceOKMask: bestServerServiceAllMask,
	}
}

func automationQualityFreshCurrent(identity string) automationHealthProbe {
	return automationHealthProbe{
		State: automationHealthUncertain, QualityDegraded: true, QualityPoints: 2,
		CurrentIdentity: identity, ServiceComplete: true, ServiceOKMask: bestServerServiceAllMask,
	}
}

func TestAutomationQualityPairedConfirmationRequiresTheSameOriginalCause(t *testing.T) {
	oldCandidate, oldCurrent := automationQualityCandidateProbe, automationQualityCurrentProbe
	t.Cleanup(func() { automationQualityCandidateProbe, automationQualityCurrentProbe = oldCandidate, oldCurrent })
	for _, tc := range []struct {
		name, want string
		change     func(*automationQualityAdmission, *automationHealthProbe, *automationHealthProbe)
		current    bool
	}{
		{"still slow", "", func(*automationQualityAdmission, *automationHealthProbe, *automationHealthProbe) {}, true},
		{"candidate service regression", "uncertain", func(_ *automationQualityAdmission, c, _ *automationHealthProbe) {
			c.ServiceOKMask, c.ServiceBadMask = 14, 1
		}, false},
		{"candidate incomplete", "uncertain", func(_ *automationQualityAdmission, c, _ *automationHealthProbe) {
			c.ServiceComplete = false
		}, false},
		{"candidate generic unknown", "uncertain", func(_ *automationQualityAdmission, c, _ *automationHealthProbe) {
			c.State = automationHealthUncertain
		}, false},
		{"current recovered after candidate", "same", func(_ *automationQualityAdmission, _, c *automationHealthProbe) {
			c.State, c.QualityDegraded, c.QualityPoints = automationHealthHealthy, false, 0
		}, true},
		{"current incomplete", "uncertain", func(_ *automationQualityAdmission, _, c *automationHealthProbe) {
			c.ServiceComplete = false
		}, true},
		{"current generic failure", "uncertain", func(_ *automationQualityAdmission, _, c *automationHealthProbe) {
			c.State, c.QualityDegraded = automationHealthFailed, false
		}, true},
		{"current identity changed", "uncertain", func(_ *automationQualityAdmission, _, c *automationHealthProbe) {
			c.CurrentIdentity = strings.Repeat("b", 64)
		}, true},
		{"same persistent service", "", func(g *automationQualityAdmission, _, c *automationHealthProbe) {
			g.trigger.LatencyDegraded, g.trigger.ServiceBadMask = false, 1
			c.QualityPoints, c.ServiceOKMask, c.ServiceBadMask = 0, 14, 1
		}, true},
		{"different service is not original evidence", "same", func(g *automationQualityAdmission, _, c *automationHealthProbe) {
			g.trigger.LatencyDegraded, g.trigger.ServiceBadMask = false, 1
			c.QualityPoints, c.ServiceOKMask, c.ServiceBadMask = 0, 13, 2
		}, true},
		{"service replaces vanished latency", "same", func(_ *automationQualityAdmission, _, c *automationHealthProbe) {
			c.QualityPoints, c.ServiceOKMask, c.ServiceBadMask = 0, 14, 1
		}, true},
		{"latency replaces vanished service", "same", func(g *automationQualityAdmission, _, _ *automationHealthProbe) {
			g.trigger.LatencyDegraded, g.trigger.ServiceBadMask = false, 1
		}, true},
		{"one of both original causes remains", "", func(g *automationQualityAdmission, _, c *automationHealthProbe) {
			g.trigger.ServiceBadMask = 1
			c.QualityPoints, c.ServiceOKMask, c.ServiceBadMask = 0, 14, 1
		}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			guard, selected := automationQualityAdmissionFixture(t)
			candidate, current := automationQualityFreshCandidate(), automationQualityFreshCurrent(guard.trigger.CurrentIdentity)
			tc.change(guard, &candidate, &current)
			calls := []string{}
			automationQualityCandidateProbe = func(_ *app, ctx context.Context, got bestServerInternalCandidate) automationHealthProbe {
				calls = append(calls, "candidate")
				if got.Raw != selected.Raw || got.Profile.ID != selected.Profile.ID {
					t.Fatal("confirmation changed the exact measured snapshot")
				}
				if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) > automationHealthProbeTimeout {
					t.Fatal("candidate confirmation lacks its bounded child deadline")
				}
				return candidate
			}
			automationQualityCurrentProbe = func(_ *app, ctx context.Context) automationHealthProbe {
				calls = append(calls, "current")
				if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) > automationHealthProbeTimeout {
					t.Fatal("current confirmation lacks its bounded child deadline")
				}
				return current
			}
			err := guard.confirm(selected)
			wantCalls := "candidate"
			if tc.current {
				wantCalls += ",current"
			}
			if strings.Join(calls, ",") != wantCalls {
				t.Fatalf("confirmation order=%v want %s", calls, wantCalls)
			}
			if tc.want == "" {
				if err != nil || guard.stopped != nil || guard.confirmedAt.IsZero() || guard.final() != nil {
					t.Fatalf("confirmed pair was rejected: err=%v stopped=%+v", err, guard.stopped)
				}
			} else if err == nil || guard.stopped == nil || guard.stopped.Result != tc.want || !guard.confirmedAt.IsZero() {
				t.Fatalf("err=%v stopped=%+v; want %s without confirmation", err, guard.stopped, tc.want)
			}
		})
	}
}

func TestAutomationQualityFinalAdmissionRechecksMutableFacts(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*testing.T, *automationQualityAdmission)
	}{
		{"disabled", func(t *testing.T, g *automationQualityAdmission) {
			if err := writeAutomationConfigValues(g.app.cfg.ConfigPath, map[string]string{"AUTO_VPN_V1": "no"}); err != nil { t.Fatal(err) }
		}},
		{"mode changed", func(t *testing.T, g *automationQualityAdmission) {
			if err := writeAutomationConfigValues(g.app.cfg.ConfigPath, map[string]string{"AUTO_VPN_MODE": "endpoint"}); err != nil { t.Fatal(err) }
		}},
		{"auto apply disabled", func(t *testing.T, g *automationQualityAdmission) {
			if err := writeAutomationConfigValues(g.app.cfg.ConfigPath, map[string]string{"AUTO_VPN_AUTO_APPLY": "no"}); err != nil { t.Fatal(err) }
		}},
		{"country no longer allowed", func(t *testing.T, g *automationQualityAdmission) {
			if err := writeAutomationConfigValues(g.app.cfg.ConfigPath, map[string]string{"AUTO_VPN_COUNTRY_SCOPE": "allowlist", "AUTO_VPN_COUNTRIES": "pl"}); err != nil { t.Fatal(err) }
		}},
		{"same endpoint different profile", func(t *testing.T, g *automationQualityAdmission) {
			if err := os.WriteFile(g.app.cfg.FilterPath, []byte("^de Other$\n"), 0600); err != nil { t.Fatal(err) }
		}},
		{"switch cooldown baseline changed", func(_ *testing.T, g *automationQualityAdmission) {
			state := parseAutomationState(automationStatePath())
			state["LAST_SWITCH"], state["LAST_SWITCH_ORIGIN"] = time.Now().UTC().Format(time.RFC3339), "manual"
			writeAutomationStatePayload(automationStatePath(), state)
		}},
		{"rollback guard appeared", func(_ *testing.T, _ *automationQualityAdmission) { setAutomationMutationBlocked(true) }},
		{"post update acceptance appeared", func(t *testing.T, g *automationQualityAdmission) {
			if err := os.WriteFile(g.app.cfg.UpdateState, []byte("STATE=SUCCESS\nTARGET_VERSION=v"+version+"\n"), 0600); err != nil { t.Fatal(err) }
		}},
		{"updater launching", func(_ *testing.T, g *automationQualityAdmission) {
			g.app.updateMu.Lock()
			g.app.updateLaunching = true
			g.app.updateMu.Unlock()
		}},
		{"updater lock appeared", func(t *testing.T, g *automationQualityAdmission) {
			if err := os.Mkdir(g.app.cfg.UpdateLock, 0700); err != nil { t.Fatal(err) }
		}},
		{"snapshot expired", func(t *testing.T, g *automationQualityAdmission) {
			replaceAutomationQualitySnapshot(t, g.token, func(snapshot *bestServerSelectionSnapshot) {
				snapshot.CreatedAt = time.Now().Add(-bestServerSelectionSnapshotTTL-time.Minute).UTC().Format(time.RFC3339)
			})
		}},
		{"snapshot changed to manual RTT purpose", func(t *testing.T, g *automationQualityAdmission) {
			replaceAutomationQualitySnapshot(t, g.token, func(snapshot *bestServerSelectionSnapshot) {
				snapshot.Purpose = "manual_rtt"
			})
		}},
		{"same ID and endpoint different snapshot credentials", func(t *testing.T, g *automationQualityAdmission) {
			replaceAutomationQualitySnapshot(t, g.token, func(snapshot *bestServerSelectionSnapshot) {
				snapshot.Candidates[0].Raw = strings.Replace(snapshot.Candidates[0].Raw, "TEST-ID-A", "TEST-ID-CHANGED", 1)
			})
		}},
		{"subscription source changed", func(t *testing.T, g *automationQualityAdmission) {
			if err := os.WriteFile(g.app.cfg.SubPath, []byte("https://provider.example.invalid/changed\n"), 0600); err != nil { t.Fatal(err) }
		}},
		{"parent canceled", func(_ *testing.T, g *automationQualityAdmission) {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			g.ctx = ctx
		}},
		{"confirmation aged beyond drain", func(_ *testing.T, g *automationQualityAdmission) {
			g.confirmedAt = time.Now().Add(-automationHealthProbeTimeout-time.Second)
		}},
		{"missing baseline", func(_ *testing.T, g *automationQualityAdmission) { g.baseline.Candidates = nil }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			guard, selected := automationQualityAdmissionFixture(t)
			guard.selectedRaw, guard.confirmedAt = selected.Raw, time.Now()
			if err := guard.final(); err != nil {
				t.Fatalf("fixture must start with a valid final admission: %v", err)
			}
			tc.change(t, guard)
			if err := guard.final(); err == nil || guard.stopped == nil || guard.stopped.Mutated || guard.stopped.RollbackState != "NOT_APPLIED" {
				t.Fatalf("changed admission facts allowed mutation: err=%v result=%+v", err, guard.stopped)
			}
		})
	}
}

func replaceAutomationQualitySnapshot(t *testing.T, token string, change func(*bestServerSelectionSnapshot)) {
	t.Helper()
	path := bestServerSelectionSnapshotPath(token)
	data, err := os.ReadFile(path)
	if err != nil { t.Fatal(err) }
	var snapshot bestServerSelectionSnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil { t.Fatal(err) }
	change(&snapshot)
	data, err = json.Marshal(snapshot)
	if err != nil { t.Fatal(err) }
	if err := os.WriteFile(path, data, 0600); err != nil { t.Fatal(err) }
}

func installAutomationQualityApplyFixture(t *testing.T, guard *automationQualityAdmission) string {
	t.Helper()
	dir := filepath.Dir(guard.app.cfg.ConfigPath)
	callsPath := filepath.Join(dir, "provider-calls")
	t.Setenv("FREENET_TEST_QUALITY_CALLS", callsPath)
	t.Setenv("FREENET_TEST_QUALITY_OUT", guard.app.cfg.OutPath)
	provider := writeFakeNetworkHelper(t, "\nprintf '%s\\n' \"$1\" >> \"$FREENET_TEST_QUALITY_CALLS\"\ncat <<EOF\n========== FreeNet Provider Plan ==========\nPROFILE_ID=$2\nPROFILE_NAME=Frankfurt, Germany, Extra\nENDPOINT=203.0.113.10:443\nCURRENT_OUTBOUND=present\nXRAY_RUNNING=yes\nCANDIDATE_XRAY_VALID=yes\nCANDIDATE_ROUTE_OK=yes\nEXPECTED_DELTA=replace exactly one vless-reality outbound\nEXPECTED_NO_DELTA=ISP/DNS/routing unchanged\nMUTATION=NONE\n========== END ==========\nEOF\ncase \"$1\" in\n  plan) exit 0 ;;\n  apply-core)\n    printf '%s\\n' '{\"outbounds\":[{\"tag\":\"vless-reality\",\"settings\":{\"vnext\":[{\"address\":\"203.0.113.10\",\"port\":443}]}}]}' > \"$FREENET_TEST_QUALITY_OUT\"\n    printf '%s\\n' '[FreeNet Provider] RESULT=SUCCESS'\n    exit 0 ;;\n  *) exit 9 ;;\nesac")
	network := writeFakeNetworkHelper(t, "[ \"$1\" = plan ] || exit 9\ncat <<'EOF'\n"+supportedPlanOutput()+"\nEOF")
	t.Setenv("FREENET_PROVIDER_HELPER", provider)
	t.Setenv("FREENET_NETWORK_HELPER", network)
	return callsPath
}

func TestAutomationQualityWriterAdmissionOrdersSharedAndExclusiveChecks(t *testing.T) {
	oldCandidate, oldCurrent := automationQualityCandidateProbe, automationQualityCurrentProbe
	t.Cleanup(func() { automationQualityCandidateProbe, automationQualityCurrentProbe = oldCandidate, oldCurrent })
	for _, outcome := range []string{"accepted", "current recovered", "canceled before mutation"} {
		t.Run(outcome, func(t *testing.T) {
			guard, _ := automationQualityAdmissionFixture(t)
			callsPath := installAutomationQualityApplyFixture(t, guard)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			guard.ctx = ctx
			probeCalls, finalCalls := 0, 0
			probeSlot := func(ctx context.Context) {
				t.Helper()
				if len(guard.app.sem) != 1 {
					t.Fatal("paired probe ran outside the provider writer ownership")
				}
				if len(isolatedXrayProbeGate) != 0 {
					t.Fatal("paired shared probe ran under the exclusive probe fence")
				}
				release, ok := acquireIsolatedXrayProbe(ctx)
				if !ok { t.Fatal("paired probe could not acquire its shared slot") }
				release()
				probeCalls++
			}
			automationQualityCandidateProbe = func(_ *app, ctx context.Context, _ bestServerInternalCandidate) automationHealthProbe {
				probeSlot(ctx)
				return automationQualityFreshCandidate()
			}
			automationQualityCurrentProbe = func(_ *app, ctx context.Context) automationHealthProbe {
				probeSlot(ctx)
				current := automationQualityFreshCurrent(guard.trigger.CurrentIdentity)
				if outcome == "current recovered" {
					current.State, current.QualityDegraded, current.QualityPoints = automationHealthHealthy, false, 0
				}
				return current
			}
			status, result := guard.app.executeProviderProfileApplyWithAdmission(networkApplyRequest{
				Operation: "provider", ProfileID: guard.candidate.ID, SelectionToken: guard.token, Confirm: true,
			}, nil, &providerApplyAdmission{
				beforeQuiesce: guard.confirm,
				beforeMutation: func() error {
					finalCalls++
					if len(guard.app.sem) != 1 || len(isolatedXrayProbeGate) != isolatedXrayProbeLimit {
						t.Fatal("final facts ran outside existing writer/exclusive ownership")
					}
					if outcome == "canceled before mutation" { cancel() }
					return guard.final()
				},
			})
			calls, err := os.ReadFile(callsPath)
			if err != nil { t.Fatal(err) }
			wantCalls, wantFinal := "plan\n", 1
			if outcome == "accepted" { wantCalls = "plan\napply-core\n" }
			if outcome == "current recovered" { wantFinal = 0 }
			if string(calls) != wantCalls || probeCalls != 2 || finalCalls != wantFinal ||
				len(guard.app.sem) != 0 || len(isolatedXrayProbeGate) != 0 {
				t.Fatalf("provider=%q probes=%d final=%d; expected %q/2/%d and released gates", calls, probeCalls, finalCalls, wantCalls, wantFinal)
			}
			if outcome == "accepted" {
				if status != http.StatusOK || !result.Success || !result.Applied { t.Fatalf("accepted result=%+v status=%d", result, status) }
			} else if status != http.StatusConflict || result.Success || result.Applied || result.RollbackState != "NOT_APPLIED" ||
				readBestServerCurrentEndpoint(guard.app.cfg.OutPath) != guard.baseline.CurrentEndpoint {
				t.Fatalf("admission rejection changed VPN: status=%d result=%+v", status, result)
			}
		})
	}
}

func TestAutomationQualityEntryUsesPairedWriterAdmission(t *testing.T) {
	oldRefresh, oldCurrent, oldForeign := automationBestCurrentRefresh, automationBestCurrentQuality, automationBestForeignScan
	oldCandidateProbe, oldCurrentProbe := automationQualityCandidateProbe, automationQualityCurrentProbe
	t.Cleanup(func() {
		automationBestCurrentRefresh, automationBestCurrentQuality = oldRefresh, oldCurrent
		automationBestForeignScan = oldForeign
		automationQualityCandidateProbe, automationQualityCurrentProbe = oldCandidateProbe, oldCurrentProbe
	})
	guard, _ := automationQualityAdmissionFixture(t)
	callsPath := installAutomationQualityApplyFixture(t, guard)
	calls := []string{}
	automationBestCurrentRefresh = func(*app, context.Context) (int, bestServerRefreshResponse) {
		t.Fatal("quality alarm entered endpoint-refresh mutation")
		return 0, bestServerRefreshResponse{}
	}
	automationBestCurrentQuality = func(*app, context.Context) (bestServerQualityResponse, error) {
		calls = append(calls, "full current")
		return guard.baseline, nil
	}
	automationBestForeignScan = func(*app, context.Context, automationSettings, string) (bestServerQualityResponse, error) {
		calls = append(calls, "foreign")
		return bestServerQualityResponse{Candidates: []bestServerQualityCandidate{guard.candidate}, SelectionToken: guard.token}, nil
	}
	automationQualityCandidateProbe = func(a *app, _ context.Context, _ bestServerInternalCandidate) automationHealthProbe {
		calls = append(calls, "fresh candidate")
		if len(a.sem) != 1 { t.Fatal("quality entry bypassed writer admission") }
		return automationQualityFreshCandidate()
	}
	automationQualityCurrentProbe = func(a *app, _ context.Context) automationHealthProbe {
		calls = append(calls, "fresh current")
		if len(a.sem) != 1 { t.Fatal("quality entry bypassed writer admission") }
		return automationQualityFreshCurrent(guard.trigger.CurrentIdentity)
	}
	result, err := guard.app.runAutomationQualityCycle(context.Background(), readAutomationSettings(guard.app.cfg.ConfigPath), guard.trigger)
	helperCalls, readErr := os.ReadFile(callsPath)
	if err != nil || readErr != nil || result.Result != "switched" || !result.Mutated ||
		strings.Join(calls, ",") != "full current,foreign,fresh candidate,fresh current" || string(helperCalls) != "plan\napply-core\n" {
		t.Fatalf("quality cycle=%+v err=%v readErr=%v stages=%v helper=%q", result, err, readErr, calls, helperCalls)
	}
	state := parseAutomationState(automationStatePath())
	if state["LAST_SWITCH_ORIGIN"] != "auto" || state["LAST_SWITCH"] == guard.lastSwitch {
		t.Fatal("successful quality apply did not set the existing switch cooldown baseline")
	}
}

func TestAutomationQualityMissingTriggerAndCanceledPairStayReadOnly(t *testing.T) {
	guard, selected := automationQualityAdmissionFixture(t)
	oldCandidate := automationQualityCandidateProbe
	t.Cleanup(func() { automationQualityCandidateProbe = oldCandidate })
	ctx, cancel := context.WithCancel(context.Background())
	guard.ctx = ctx
	automationQualityCandidateProbe = func(*app, context.Context, bestServerInternalCandidate) automationHealthProbe {
		cancel()
		return automationQualityFreshCandidate()
	}
	if err := guard.confirm(selected); err == nil || guard.stopped == nil || guard.stopped.Result != "uncertain" {
		t.Fatalf("canceled pair was admitted: err=%v result=%+v", err, guard.stopped)
	}
	result, err := guard.app.runAutomationQualityCycle(context.Background(), readAutomationSettings(guard.app.cfg.ConfigPath), automationQualityTrigger{})
	if err != nil || result.Mutated || result.Result != "uncertain" {
		t.Fatalf("missing trigger was admitted: result=%+v err=%v", result, err)
	}
	if ctx.Err() != context.Canceled {
		t.Fatalf("fixture did not cancel its real parent context: %v", ctx.Err())
	}
}
