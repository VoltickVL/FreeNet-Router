package main

import (
	"context"
	"testing"
)

func TestAutoMediaPairedAdmissionBlocksBadCandidateAndRecoveredCurrent(t *testing.T) {
	oldCandidate, oldCurrent := automationQualityCandidateProbe, automationQualityCurrentProbe
	t.Cleanup(func(){
		automationQualityCandidateProbe,automationQualityCurrentProbe=oldCandidate,oldCurrent
	})
	for _,tc:=range []struct {
		name,want string
		candidate, current automationHealthProbe
	}{
		{"candidate and current paired", "",func()automationHealthProbe{
			c:=automationQualityFreshCandidate();c.MediaComplete=true;return c
		}(),autoMediaHealth("fixture",true,true)},
		{"candidate same stalls","uncertain",func()automationHealthProbe{
			c:=automationQualityFreshCandidate();c.MediaComplete,c.MediaDegraded=true,true;return c
		}(),autoMediaHealth("fixture",true,true)},
		{"candidate unknown","uncertain",automationQualityFreshCandidate(),autoMediaHealth("fixture",true,true)},
		{"current unknown","uncertain",func()automationHealthProbe{
			c:=automationQualityFreshCandidate();c.MediaComplete=true;return c
		}(),autoMediaHealth("fixture",false,false)},
		{"current recovered","same",func()automationHealthProbe{
			c:=automationQualityFreshCandidate();c.MediaComplete=true;return c
		}(),autoMediaHealth("fixture",false,true)},
	}{
		t.Run(tc.name,func(t *testing.T){
			guard,selected:=automationQualityAdmissionFixture(t)
			guard.trigger.LatencyDegraded=false
			guard.trigger.ServiceBadMask=0
			guard.trigger.MediaDegraded=true
			tc.current.CurrentIdentity=guard.trigger.CurrentIdentity
			if !validAutomationQualityTrigger(guard.trigger) {
				t.Fatal("identity-bound media-only trigger not admitted as comparison reason")
			}
			calls:=[]string{}
			automationQualityCandidateProbe=func(_ *app, ctx context.Context, candidate bestServerInternalCandidate) automationHealthProbe{
				calls=append(calls,"candidate")
				if candidate.Profile.ID!=selected.Profile.ID || ctx.Err()!=nil {t.Fatal("candidate is not exact or context expired")}
				return tc.candidate
			}
			automationQualityCurrentProbe=func(_ *app,ctx context.Context)automationHealthProbe{
				calls=append(calls,"current")
				if ctx.Err()!=nil {t.Fatal("current confirmation timeout")}
				return tc.current
			}
			err:=guard.confirm(selected)
			if tc.want==""{
				if err!=nil || guard.stopped!=nil || guard.confirmedAt.IsZero(){
					t.Fatalf("confirmed media route comparison stopped: err=%v stopped=%+v",err,guard.stopped)
				}
				if len(calls)!=2 || calls[0]!="candidate" || calls[1]!="current"{
					t.Fatalf("paired confirmation order changed: %v",calls)
				}
			}else{
				if err==nil || guard.stopped==nil || guard.stopped.Result!=tc.want || guard.stopped.RollbackState!="NOT_APPLIED"{
					t.Fatalf("unsafe media transition passed: err=%v stopped=%+v",err,guard.stopped)
				}
			}
		})
	}
}

func TestAutoMediaPairedAdmissionNeverTreatsHTTPStatusAsPlayback(t *testing.T) {
	guard,selected:=automationQualityAdmissionFixture(t)
	guard.trigger.LatencyDegraded=false
	guard.trigger.MediaDegraded=true
	oldCandidate:=automationQualityCandidateProbe
	t.Cleanup(func(){automationQualityCandidateProbe=oldCandidate})
	// Service HEAD 4/4 and low HTTP latency are insufficient: without two
	// completed data segments this route is unknown and must not be applied.
	automationQualityCandidateProbe=func(*app,context.Context,bestServerInternalCandidate)automationHealthProbe{
		return automationQualityFreshCandidate()
	}
	if err:=guard.confirm(selected);err==nil || guard.stopped==nil || guard.stopped.Result!="uncertain" ||
		guard.stopped.RollbackState!="NOT_APPLIED"{
		t.Fatalf("positive HEAD masked absent stream transfer: %+v err=%v",guard.stopped,err)
	}
}
