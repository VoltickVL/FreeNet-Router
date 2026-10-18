from pathlib import Path

def replace(path, old, new):
 p=Path(path);s=p.read_text();assert s.count(old)==1,(path,s.count(old),old[:100]);p.write_text(s.replace(old,new))

p='freenet-ui/vpn_transaction_doctor.go'
replace(p,'\tDecision        string `json:"decision"`','\tDecision        string `json:"decision"`\n\tRecoveryAttempt string `json:"recovery_attempt,omitempty"`')
replace(p,'\t\t_, err := os.Lstat(live)\n','\t\tinfo, err := os.Lstat(live)\n')
replace(p,'\t\tif err != nil {\n\t\t\treturn "unknown"\n\t\t}\n\t\treturn "different"','\t\tif err != nil || !info.Mode().IsRegular() || info.Size() > 2*1024*1024 {\n\t\t\treturn "unknown"\n\t\t}\n\t\treturn "different"')
replace(p,'\t\tXrayCore: "stopped-or-unknown",\n\t}\n\tif a != nil && a.liveXrayRunning() {\n\t\tresp.XrayCore = "running"\n\t}','\t\tXrayCore: a.manualRecoveryCoreState(),\n\t}')
replace(p,'''	if _, recoveryErr := os.Lstat(filepath.Join(checkpoint, "recovery.pending")); recoveryErr == nil || !os.IsNotExist(recoveryErr) {
		resp.Decision = "STOP_RECOVERY_INTERRUPTED"
		return resp
	}''','''	if _, recoveryErr := os.Lstat(filepath.Join(checkpoint, "recovery.pending")); recoveryErr == nil || !os.IsNotExist(recoveryErr) {
		resp.RecoveryAttempt = "unresolved"
		if !a.manualRecoveryAttemptReadable(checkpoint) {
			resp.Decision = "STOP_RECOVERY_INTERRUPTED"
			return resp
		}
		resp.RecoveryAttempt = "complete-readable"
	}''')
replace(p,'if resp.Checkpoint != "pending" || resp.XrayCore != "running" {','if resp.Checkpoint != "pending" || (resp.XrayCore != "running" && resp.XrayCore != "stopped") {')
replace(p,'if resp.Decision != "STOP_VPN_UNVERIFIED" && resp.Decision != "STOP_FILES_UNRESOLVED" {','if resp.Decision != "STOP_VPN_UNVERIFIED" && resp.Decision != "STOP_FILES_UNRESOLVED" && !(resp.Decision == "STOP_XRAY_NOT_CONFIRMED" && resp.XrayCore == "stopped") {')
p='freenet-ui/network_apply_api.go'
replace(p,'\tcurrentFilter := readBestServerCurrentFilter(a.cfg.FilterPath)\n\tif req.ManualOverride {','''	currentFilter := readBestServerCurrentFilter(a.cfg.FilterPath)
	if req.ManualOverride && a.manualRecoveryCoreState() == "unknown" {
		return http.StatusConflict, networkApplyResponse{Success:false,Operation:"provider",ProfileID:profileID,RollbackState:"NOT_APPLIED",Error:"production Xray process inventory is ambiguous; no config was changed"}
	}
	if req.ManualOverride {''')
replace(p,'\t\tif err := recordAutomationManualSwitchWithRecovery(time.Now().UTC(), manualRecovery); err != nil {','''		if err := a.setXrayRuntimeDesiredState(xrayRuntimeDesiredRunning); err != nil {
			setAutomationMutationBlocked(true)
			return http.StatusBadGateway, networkApplyResponse{Success:false, Operation:"provider", ProfileID:profileID, ProviderPlan:&appliedPlan, PrimaryError:err.Error(), RollbackState:"FAILED/UNKNOWN", Error:"selected VPN connected but persistent Xray running intent is not confirmed"}
		}
		if err := recordAutomationManualSwitchWithRecovery(time.Now().UTC(), manualRecovery); err != nil {''')
p='scripts/apply_provider_profile.sh'
replace(p,'    [ ! -e "$CHECKPOINT_DIR/recovery.pending" ] && [ ! -L "$CHECKPOINT_DIR/recovery.pending" ] || return 1','''    if [ -e "$CHECKPOINT_DIR/recovery.pending" ] || [ -L "$CHECKPOINT_DIR/recovery.pending" ]; then
        valid_recovery_attempt || return 1
    fi''')
anchor='# A second durable checkpoint captures ACTUAL live bytes.'
helpers=r'''# A previous fully written recovery snapshot is preserved before the next
# explicit attempt. Incomplete snapshots and unknown writers remain STOP.
valid_recovery_attempt() {
    ATTEMPT="$CHECKPOINT_DIR/recovery.pending"
    [ -d "$ATTEMPT" ] && [ ! -L "$ATTEMPT" ] || return 1
    [ -f "$ATTEMPT/state" ] && [ ! -L "$ATTEMPT/state" ] || return 1
    [ "$(cat "$ATTEMPT/state")" = PENDING ] || return 1
    if [ -e "$ATTEMPT/core.before" ] || [ -L "$ATTEMPT/core.before" ]; then
        [ -f "$ATTEMPT/core.before" ] && [ ! -L "$ATTEMPT/core.before" ] || return 1
        case "$(cat "$ATTEMPT/core.before")" in running|stopped) ;; *) return 1 ;; esac
    fi
    for KIND in out profile filter; do
        BEFORE="$ATTEMPT/$KIND.before"; ABSENT="$ATTEMPT/$KIND.absent"
        [ ! -L "$BEFORE" ] && [ ! -L "$ABSENT" ] || return 1
        if [ -f "$BEFORE" ]; then
            [ ! -e "$ABSENT" ] && [ -r "$BEFORE" ] || return 1
            [ "$(wc -c < "$BEFORE")" -le 2097152 ] || return 1
        else
            [ ! -e "$BEFORE" ] && [ -f "$ABSENT" ] || return 1
            [ "$(wc -c < "$ABSENT")" -eq 0 ] || return 1
        fi
    done
}

archive_recovery_attempt() {
    ATTEMPT_RESULT="$1"
    ATTEMPT_ROOT="$(dirname "$CHECKPOINT_DIR")/vpn-transaction.attempts"
    if [ -e "$ATTEMPT_ROOT" ] || [ -L "$ATTEMPT_ROOT" ]; then
        [ -d "$ATTEMPT_ROOT" ] && [ ! -L "$ATTEMPT_ROOT" ] || return 1
    else
        (umask 077; mkdir "$ATTEMPT_ROOT") || return 1
    fi
    chmod 700 "$ATTEMPT_ROOT" || return 1
    ATTEMPT_DEST="$ATTEMPT_ROOT/$(date -u '+%Y%m%dT%H%M%SZ').$$"
    [ ! -e "$ATTEMPT_DEST" ] && [ ! -L "$ATTEMPT_DEST" ] || return 1
    # Do not rewrite either generation of saved configuration bytes.
    printf '%s\n' "$ATTEMPT_RESULT" > "$CHECKPOINT_DIR/recovery.pending/result" || return 1
    mv "$CHECKPOINT_DIR/recovery.pending" "$ATTEMPT_DEST" || return 1
    if command -v sync >/dev/null 2>&1; then sync || return 1; fi
    [ ! -e "$CHECKPOINT_DIR/recovery.pending" ] && [ -d "$ATTEMPT_DEST" ]
}

'''
replace(p,anchor,helpers+anchor)
replace(p,"    printf 'PENDING\\n' > \"$RECOVERY_DIR/state\" || return 1",'''    if [ "$WAS_RUNNING" -eq 1 ]; then BEFORE_CORE=running; else BEFORE_CORE=stopped; fi
    printf '%s\\n' "$BEFORE_CORE" > "$RECOVERY_DIR/core.before" || return 1
    chmod 600 "$RECOVERY_DIR/core.before" || return 1
    printf 'PENDING\\n' > "$RECOVERY_DIR/state" || return 1''')
replace(p,'''    else
        jq -n --slurpfile replacement "$VLESS_OBJECT" '
          {outbounds:''','''    else
        [ ! -e "$OUT_FILE" ] && [ ! -L "$OUT_FILE" ] || { err 'existing outbound is unreadable or invalid; candidate did not replace it'; return 1; }
        jq -n --slurpfile replacement "$VLESS_OBJECT" '
          {outbounds:''')
replace(p,'    single_xray_pid >/dev/null 2>&1\n}', '''    if single_xray_pid >/dev/null 2>&1; then return 0; fi
    [ "${FREENET_PROVIDER_RTT_MANUAL:-0}" = 1 ] && [ -z "$(pidof xray 2>/dev/null || true)" ]
}''')
replace(p,'''restart_if_needed() {
    [ "$WAS_RUNNING" -eq 1 ] || return 0
    if [ "$MODE" = apply-core ] || [ "$MODE" = recover-core ]; then
        restart_xray_core no
        return $?
    fi''','''restart_if_needed() {
    if [ "$MODE" = apply-core ] || [ "$MODE" = recover-core ]; then
        if [ "${FREENET_PROVIDER_RTT_MANUAL:-0}" = 1 ]; then
            restart_xray_core yes
        else
            restart_xray_core no
        fi
        return $?
    fi
    [ "$WAS_RUNNING" -eq 1 ] || return 0''')
replace(p,'rollback_state() {\n',r'''restore_previously_stopped_core() {
    if [ -n "$CORE_RESTART_HELPER" ]; then
        "$CORE_RESTART_HELPER" stop || return 1
        [ -z "$(pidof xray 2>/dev/null || true)" ]
        return $?
    fi
    PIDS="$(pidof xray 2>/dev/null || true)"
    set -- $PIDS
    [ "$#" -eq 0 ] && return 0
    [ "$#" -eq 1 ] || return 1
    STOP_PID="$1"
    kill "$STOP_PID" 2>/dev/null || return 1
    I=0
    while kill -0 "$STOP_PID" 2>/dev/null && [ "$I" -lt 40 ]; do short_pause; I=$((I + 1)); done
    if kill -0 "$STOP_PID" 2>/dev/null; then
        kill -9 "$STOP_PID" 2>/dev/null || return 1
        I=0
        while kill -0 "$STOP_PID" 2>/dev/null && [ "$I" -lt 10 ]; do short_pause; I=$((I + 1)); done
    fi
    [ -z "$(pidof xray 2>/dev/null || true)" ]
}

rollback_state() {
''')
replace(p,'''    else
        ROLLBACK_CORE_STATUS="PREVIOUSLY_STOPPED"
    fi
    if [ "$RB" -eq 0 ]; then''','''    else
        if { [ "$MODE" = apply-core ] || [ "$MODE" = recover-core ]; } && [ "${FREENET_PROVIDER_RTT_MANUAL:-0}" = 1 ]; then
            if restore_previously_stopped_core; then
                ROLLBACK_CORE_STATUS="PREVIOUSLY_STOPPED"
            else
                ROLLBACK_CORE_STATUS="FAILED"
                RB=1
            fi
        else
            ROLLBACK_CORE_STATUS="PREVIOUSLY_STOPPED"
        fi
    fi
    if [ "$RB" -eq 0 ]; then''')
replace(p,'            ROLLBACK_VPN_STATUS="UNREACHABLE"\n            RB=1','            ROLLBACK_VPN_STATUS="UNREACHABLE"\n            # An already-dead endpoint does not invalidate restored files/core.')
replace(p,'''            if [ "$MODE" = recover-core ]; then
                err 'ROLLBACK ERROR/STATE: FAILED/UNKNOWN (original checkpoint retained after manual recovery)'
                exit 2
            fi
            if clear_checkpoint; then''','''            if [ "$MODE" = recover-core ]; then
                if archive_recovery_attempt ROLLED_BACK; then
                    err 'ROLLBACK ERROR/STATE: rollback success (original checkpoint retained; another explicit candidate is allowed)'
                    exit 1
                fi
                err 'ROLLBACK ERROR/STATE: FAILED/UNKNOWN (attempt archive not confirmed)'
                exit 2
            fi
            if [ "$ROLLBACK_VPN_STATUS" != REACHABLE ] || { [ "$ROLLBACK_CORE_STATUS" = PREVIOUSLY_STOPPED ] && [ "${FREENET_PROVIDER_RTT_MANUAL:-0}" = 1 ]; }; then
                err 'ROLLBACK ERROR/STATE: rollback success (files/core restored, old VPN unavailable; protected manual recovery is available)'
                exit 1
            fi
            if clear_checkpoint; then''')
replace(p,"    [ \"$WAS_RUNNING\" -eq 1 ] || fail_apply 'safe core-only Xray restart requires exactly one running Xray process'", "    [ \"$WAS_RUNNING\" -eq 1 ] || [ \"${FREENET_PROVIDER_RTT_MANUAL:-0}\" = 1 ] || fail_apply 'safe core-only Xray restart requires exactly one running Xray process'")
replace(p,'''if [ "$MODE" = recover-core ]; then
    begin_recovery_checkpoint || fail_apply''','''if [ "$MODE" = recover-core ]; then
    if [ -e "$CHECKPOINT_DIR/recovery.pending" ]; then
        # Candidate/config validation and the writer fence precede reconciliation.
        archive_recovery_attempt RECONCILED || fail_apply 'cannot archive complete prior recovery evidence; STOP before mutation'
    fi
    begin_recovery_checkpoint || fail_apply''')
replace(p,'core_restart_preflight() {\n    if [ -n "$CORE_RESTART_HELPER" ]; then','''core_restart_preflight() {
    PIDS="$(pidof xray 2>/dev/null || true)"
    set -- $PIDS
    [ "$#" -le 1 ] || return 1
    if [ "$#" -eq 0 ]; then
        [ "${FREENET_PROVIDER_RTT_MANUAL:-0}" = 1 ] || return 1
    else
        case "$1" in ''|0|*[!0-9]*) return 1 ;; esac
    fi
    if [ -n "$CORE_RESTART_HELPER" ]; then''')
net=Path('scripts/apply_network_profile.sh').read_text()
normalizer=net[net.index('jsonc_normalize() {'):net.index('\nxray_dns_inbound_count()',net.index('jsonc_normalize() {'))]
replace(p,'build_candidate() {\n',normalizer+'\n'+'build_candidate() {\n')
replace(p,'    if [ -f "$OUT_FILE" ] && jq -e \'(.outbounds | type) == "array"\' "$OUT_FILE" >/dev/null 2>&1; then','''    if [ -e "$OUT_FILE" ] || [ -L "$OUT_FILE" ]; then
        [ -f "$OUT_FILE" ] && [ ! -L "$OUT_FILE" ] && [ -r "$OUT_FILE" ] || return 1
        NORMALIZED_OUT="$TMP_DIR/current-outbounds.json"
        jsonc_normalize "$OUT_FILE" > "$NORMALIZED_OUT" || return 1
        jq -e '(.outbounds | type) == "array"' "$NORMALIZED_OUT" >/dev/null 2>&1 || return 1''')
replace(p,'        \' "$OUT_FILE" > "$CANDIDATE_OUT" || return 1','        \' "$NORMALIZED_OUT" > "$CANDIDATE_OUT" || return 1')
p='tests/test_provider_profile_apply.sh'
replace(p,"grep -Fq 'ROLLBACK ERROR/STATE: FAILED/UNKNOWN' \"$TMP/old-down.err\" || fail 'old-down rollback incorrectly cleared STOP'", "grep -Fq 'ROLLBACK ERROR/STATE: rollback success' \"$TMP/old-down.err\" || fail 'verified file/core restoration conflated with old VPN reachability'")
p='freenet-ui/manual_provider_selection_test.go'
replace(p,'    a.cfg.SubPath = sourcePath\n    a.cfg.OutPath = filepath.Join(dir, "04_outbounds.json")','    a.cfg.SubPath = sourcePath\n    a.cfg.OutPath = filepath.Join(dir, "04_outbounds.json")\n    t.Setenv("FREENET_XRAY_PROC_ROOT", t.TempDir())\n    if err := a.setXrayRuntimeDesiredState(xrayRuntimeDesiredStopped); err != nil { t.Fatal(err) }')
replace(p,'    // Successful manual apply updates the AUTO baseline, not the AUTO mode.\n','    // Successful manual apply updates the AUTO baseline, not the AUTO mode.\n    if a.xrayRuntimeDesiredState() != xrayRuntimeDesiredRunning { t.Fatal("manual connection did not re-enable controlled Xray runtime intent") }\n')
p='tests/test_manual_vpn_recovery.sh'
replace(p,'[ "$(cat "$TMP/vpn-mutation.lock/pid")" ] = 9001','[ "$(cat "$TMP/vpn-mutation.lock/pid")" = 9001 ]')
