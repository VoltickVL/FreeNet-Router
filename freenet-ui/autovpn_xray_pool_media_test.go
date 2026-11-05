package main

import (
    "testing"
    "time"
)

func freeNetAutoPoolApplyMediaUpdates(state map[string]string, updates map[string]string) {
    for key,value:=range updates {state[key]=value}
}

func TestFreeNetPoolMediaRequiresRepeatedCompletedTwoSourceEvidence(t *testing.T) {
    identity:=sha256Hex([]byte("pool-profile-1"))
    start:=time.Date(2026,11,5,8,0,0,0,time.UTC)
    state:=map[string]string{}
    slow:=automationMediaEvidence{Complete:true,Degraded:true,Samples:automationMediaSegmentCount,MinMbps:2.5}
    first:=freeNetAutoPoolPlanMedia(state,start,identity,slow)
    if first.Due || first.Status!="media-observing" {t.Fatalf("first sample must not switch: %+v",first)}
    freeNetAutoPoolApplyMediaUpdates(state,first.Updates)
    duplicate:=freeNetAutoPoolPlanMedia(state,start,identity,slow)
    if duplicate.Due {t.Fatal("replayed observation triggered failover")}
    second:=freeNetAutoPoolPlanMedia(state,start.Add(90*time.Second),identity,slow)
    if !second.Due || second.Status!="media-confirmed" {t.Fatalf("persistent slow transfer should enable only measured comparison: %+v",second)}
    freeNetAutoPoolApplyMediaUpdates(state,second.Updates)
    quickRetry:=freeNetAutoPoolPlanMedia(state,start.Add(180*time.Second),identity,slow)
    if quickRetry.Due {t.Fatal("cooldown lost after second sample")}
}

func TestFreeNetPoolMediaUnknownHealthyAndIdentityDrift(t *testing.T) {
    identity:=sha256Hex([]byte("pool-original"))
    now:=time.Date(2026,11,5,8,0,0,0,time.UTC)
    state:=map[string]string{}
    incomplete:=automationMediaEvidence{Complete:false,Degraded:true,Samples:1}
    if result:=freeNetAutoPoolPlanMedia(state,now,identity,incomplete);result.Due || result.Status!="media-unverified" {
        t.Fatalf("partial body must be UNKNOWN: %+v",result)
    }
    if result:=freeNetAutoPoolPlanMedia(state,now,identity,automationMediaEvidence{Complete:true,Degraded:false,Samples:2});result.Due || result.Status!="media-healthy" {
        t.Fatalf("verified fast media should not switch: %+v",result)
    }
    slow:=automationMediaEvidence{Complete:true,Degraded:true,Samples:2}
    first:=freeNetAutoPoolPlanMedia(state,now,identity,slow)
    freeNetAutoPoolApplyMediaUpdates(state,first.Updates)
    other:=sha256Hex([]byte("pool-reserve-other"))
    changed:=freeNetAutoPoolPlanMedia(state,now.Add(90*time.Second),other,slow)
    if changed.Due {t.Fatal("media evidence was inherited across a live target change")}
    future:=freeNetAutoPoolPlanMedia(state,now.Add(180*time.Second),"not-an-identity",slow)
    if future.Due {t.Fatal("invalid profile identity admitted")}
}

func TestFreeNetPoolMediaQuietWindowAfterManualOrEmergencySwitch(t *testing.T) {
    identity:=sha256Hex([]byte("pool-reserve"))
    now:=time.Date(2026,11,5,8,0,0,0,time.UTC)
    state:=map[string]string{"POOL_MEDIA_LAST_SWITCH":now.Format(time.RFC3339)}
    slow:=automationMediaEvidence{Complete:true,Degraded:true,Samples:2}
    first:=freeNetAutoPoolPlanMedia(state,now.Add(2*time.Minute),identity,slow)
    if first.Due || first.Status!="media-settling" {t.Fatalf("recent switch must suppress media switch: %+v",first)}
    freeNetAutoPoolApplyMediaUpdates(state,first.Updates)
    second:=freeNetAutoPoolPlanMedia(state,now.Add(61*time.Minute),identity,slow)
    if second.Due {t.Fatal("quiet window expiry reused stale strikes")}
    freeNetAutoPoolApplyMediaUpdates(state,second.Updates)
    third:=freeNetAutoPoolPlanMedia(state,now.Add(63*time.Minute),identity,slow)
    if !third.Due {t.Fatalf("new repeated evidence after quiet window should be allowed: %+v",third)}
    future:=map[string]string{"POOL_MEDIA_LAST_SWITCH":now.Add(24*time.Hour).Format(time.RFC3339)}
    blocked:=freeNetAutoPoolPlanMedia(future,now,identity,slow)
    if blocked.Due || blocked.Status!="media-settling" {t.Fatal("future/unknown switch timestamp bypassed STOP")}
}

func TestFreeNetPoolMediaIntermittentNeedsExtraObservations(t *testing.T) {
    identity:=sha256Hex([]byte("pool-intermittent"))
    now:=time.Date(2026,11,5,8,0,0,0,time.UTC)
    state:=map[string]string{}
    slow:=automationMediaEvidence{Complete:true,Degraded:true,Samples:2}
    fast:=automationMediaEvidence{Complete:true,Degraded:false,Samples:2}
    for _,sample:=range []struct{minutes int;media automationMediaEvidence}{
        {0,slow},{3,fast},{5,slow},
    } {
        observation:=freeNetAutoPoolPlanMedia(state,now.Add(time.Duration(sample.minutes)*time.Minute),identity,sample.media)
        if observation.Due{t.Fatalf("mixed media before third slow should not switch: %+v",observation)}
        freeNetAutoPoolApplyMediaUpdates(state,observation.Updates)
    }
    next:=freeNetAutoPoolPlanMedia(state,now.Add(11*time.Minute),identity,slow)
    if !next.Due {t.Fatalf("three independent slow observations in full window should permit comparison: %+v",next)}
}
