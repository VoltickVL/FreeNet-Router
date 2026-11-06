package main

// Apply one generated, tested *official Xray fragment* and tell the official
// XKeen service to reload it synchronously. Never touch a legacy FreeNet 0.8
// controller, native routing, DNS, GeoData, user fragments or XKeen UI.
import (
    "bytes"
    "context"
    "encoding/json"
    "errors"
    "os"
    "path/filepath"
    "time"
)

func subscriptionOperation(ctx context.Context,root,procRoot,submittedURL string,refresh bool)nativeApplyResult{
    result:=applyStopped()
    if observedPath(root,"etc/init.d/S99freenet-ui",false)!="MISSING" {
        // 0.8 is still present; there may be another native writer/cron.
        return result
    }
    private,err:=nativePrivateDir(root)
    if err!=nil{return result}
    source,err:=readNativeConfdir(root)
    if err!=nil{return result}
    oldState,err:=readNativeSubscriptionState(root,source)
    if err!=nil{return result}
    subscriptionURL:=submittedURL
    if refresh {
        if oldState.URL=="" || submittedURL!="" {return result}
        subscriptionURL=oldState.URL
    }
    if _,err=validateSubscriptionURL(subscriptionURL);err!=nil{return result}
    body,err:=fetchNativeSubscription(ctx,subscriptionURL)
    if err!=nil {return result}
    nodes,err:=nativeSubscriptionNodes(body)
    if err!=nil{return result}
    candidate,err:=nativeSubscriptionCandidate(nodes)
    if err!=nil{return result}
    valid,err:=stageNativeSubscription(root,source,candidate)
    if err!=nil || !valid {
        return nativeApplyResult{State:"REJECTED",Runtime:"NOT_VERIFIED",Applied:false}
    }
    current,err:=readNativeConfdir(root)
    if err!=nil || !sameNativeConfdir(source,current) {
        // Someone modified native Xray during stage; we do not compete.
        return result
    }
    if _,err:=readNativeSubscriptionState(root,current);err!=nil{return result}
    if ctx.Err()!=nil {return result}

    managedPath:=filepath.Join(root,"etc/xray/configs",subscriptionFragmentName)
    statePath:=filepath.Join(private,subscriptionStateName)
    previousFragment,hadFragment:=source[subscriptionFragmentName]
    oldStateData,hadState:=[]byte(nil),false
    if hadFragment {
        oldStateData,err=os.ReadFile(statePath)
        if err!=nil{return result}
        hadState=true
    }
    // After this point we MUST finish the short transaction even if the
    // browser disconnects. No separate user backup system is created.
    if err:=atomicWritePrivate(managedPath,candidate);err!=nil{return result}
    rollback:=func()nativeApplyResult {
        restored:=false
        if hadFragment {
            restored=atomicWritePrivate(managedPath,previousFragment)==nil
        } else {
            restored=os.Remove(managedPath)==nil
        }
        if hadState {
            restored=atomicWritePrivate(statePath,oldStateData)==nil && restored
        } else {
            if err:=os.Remove(statePath);err!=nil && !errors.Is(err,os.ErrNotExist) {
                restored=false
            }
        }
        if !restored {
            return nativeApplyResult{State:"ROLLBACK_FAILED",Runtime:"UNKNOWN",Applied:false}
        }
        // Restart official XKeen, not a FreeNet-defined second VPN.
        if !synchronousXKeen(context.Background(),root,"-restart") {
            return nativeApplyResult{State:"ROLLBACK_FAILED",Runtime:"UNKNOWN",Applied:false}
        }
        return nativeApplyResult{State:"ROLLED_BACK",Runtime:"NOT_VERIFIED",Applied:false}
    }
    if !synchronousXKeen(context.Background(),root,"-restart") {
        return rollback()
    }
    // A synchronous restart exit code is insufficient for VPN success;
    // use the official service status and Xray process as minimum evidence.
    if !synchronousXKeen(context.Background(),root,"-status") ||
        !xrayProcessSeenSoon(procRoot) {
        return rollback()
    }
    newState:=nativeSubscriptionState{URL:subscriptionURL,SHA256:hashConfig(candidate)}
    stateData,err:=json.Marshal(newState)
    if err!=nil {return rollback()}
    if bytes.Contains(stateData,[]byte("\n")) {return rollback()}
    if err:=atomicWritePrivate(statePath,stateData);err!=nil {
        return rollback()
    }
    return nativeApplyResult{State:"CONFIG_APPLIED_NOT_EGRESS_VERIFIED",
        Runtime:"NOT_VERIFIED",Count:len(nodes),Applied:true}
}

func xrayProcessSeenSoon(procRoot string)bool {
    deadline:=time.Now().Add(3*time.Second)
    for {
        if xrayProcessEvidence(procRoot)=="PROCESS_DETECTED" {return true}
        if time.Now().After(deadline){return false}
        time.Sleep(100*time.Millisecond)
    }
}
