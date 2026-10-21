#!/bin/sh
set -eu
ROOT_DIR="$(CDPATH= cd "$(dirname "$0")/.." && pwd)"
eval "$(sed '/^# plan must be read-only/,$d' "$ROOT_DIR/tests/test_provider_profile_apply.sh")"

cat > "$TMP/bin/pidof" <<EOFPID
#!/bin/sh
case "\$(cat '$TMP/core.state')" in
    running) echo 4242; exit 0 ;;
    ambiguous) echo '4242 4243'; exit 0 ;;
    *) exit 1 ;;
esac
EOFPID
cat > "$TMP/bin/core-control" <<EOFCORE
#!/bin/sh
printf '%s\n' "\$1" >> '$TMP/core.calls'
if [ "\$1" = stop ]; then
    echo stopped > '$TMP/core.state'
else
    echo running > '$TMP/core.state'
fi
EOFCORE
cat > "$TMP/bin/xkeen" <<EOFXKEEN
#!/bin/sh
printf '%s\n' "\$*" >> '$TMP/xkeen.calls'
exit 77
EOFXKEEN
chmod 755 "$TMP/bin/pidof" "$TMP/bin/core-control" "$TMP/bin/xkeen"
CORE_HELPER="$TMP/bin/core-control"; export CORE_HELPER
P="$TMP/provider-transaction.pending"
echo stopped > "$TMP/core.state"
# Existing valid JSONC must preserve other outbound objects and string URLs.
cat > "$TMP/configs/04_outbounds.json" <<'JSONC'
{
 // Existing user-managed direct policy.
 "outbounds":[
  {"tag":"direct","protocol":"freedom"},
  {"tag":"block","protocol":"blackhole"},
  {"tag":"keep-me","protocol":"freedom","test":"https://fixture.invalid/a//b",},
 ],
}
JSONC

# Unattended core-only selection still cannot bootstrap a stopped core.
BASE_HASH="$(sha256sum "$TMP/configs/04_outbounds.json")"
if run_helper apply-core "$PROFILE_ID" > "$TMP/auto-stopped.out" 2>&1; then fail 'AUTO selected a server with stopped core'; fi
[ "$BASE_HASH" = "$(sha256sum "$TMP/configs/04_outbounds.json")" ] || fail 'AUTO changed stopped-core files'
[ ! -e "$P" ] || fail 'rejected preflight created pending state'

# A deliberate manual choice starts the stopped production core.
FREENET_PROVIDER_RTT_MANUAL=1 run_helper apply-core "$PROFILE_ID" > "$TMP/manual-start.out" 2>&1 || { cat "$TMP/manual-start.out"; fail 'manual stopped-core start failed'; }
[ "$(cat "$TMP/core.state")" = running ] || fail 'manual choice failed to start core'
grep -Fq '[FreeNet Provider] RESULT=SUCCESS' "$TMP/manual-start.out" || fail 'manual start missing acceptance'
[ ! -e "$P" ] || fail 'manual start did not close checkpoint'
jq -e 'any(.outbounds[]; .tag=="keep-me" and .test=="https://fixture.invalid/a//b")' "$TMP/configs/04_outbounds.json" >/dev/null || fail 'JSONC conversion lost a non-VLESS outbound'
[ ! -e "$TMP/xkeen.calls" ] || fail 'manual start invoked full XKeen lifecycle'

make_pending() {
    mkdir "$P"
    echo PENDING > "$P/state"
    cp "$TMP/configs/04_outbounds.json" "$P/out.before"
    cp "$TMP/etc/vpn_profile_name" "$P/profile.before"
    cp "$TMP/profile.filter" "$P/filter.before"
}
manual_recover() {
    FREENET_PROVIDER_RECOVERY_MANUAL=1 FREENET_PROVIDER_RTT_MANUAL=1 run_helper recover-core "$1"
}

# Failed selected server returns to a known stopped state; another choice stays available.
make_pending
echo stopped > "$TMP/core.state"
ORIGINAL="$(sha256sum "$P/out.before" "$P/profile.before" "$P/filter.before")"
LIVE_BEFORE="$(sha256sum "$TMP/configs/04_outbounds.json")"
echo fail-live-always > "$TMP/route-probe.mode"
if manual_recover "$WS_PROFILE_ID" > "$TMP/fail-stopped.out" 2>&1; then fail 'unreachable candidate accepted'; fi
grep -Fq 'ROLLBACK ERROR/STATE: rollback success' "$TMP/fail-stopped.out" || fail 'known restored state called UNKNOWN'
[ "$(cat "$TMP/core.state")" = stopped ] || fail 'rollback failed to return to originally stopped core'
[ "$ORIGINAL" = "$(sha256sum "$P/out.before" "$P/profile.before" "$P/filter.before")" ] || fail 'original evidence changed'
[ "$LIVE_BEFORE" = "$(sha256sum "$TMP/configs/04_outbounds.json")" ] || fail 'rollback bytes changed'
[ ! -e "$P/recovery.pending" ] || fail 'completed failed attempt blocks next choice'
find "$TMP/vpn-transaction.attempts" -name result -exec grep -l ROLLED_BACK '{}' ';' | grep -q . || fail 'completed attempt evidence lost'
echo pass > "$TMP/route-probe.mode"
manual_recover "$PROFILE_ID" > "$TMP/next-start.out" 2>&1 || { cat "$TMP/next-start.out"; fail 'next explicit candidate was blocked'; }
[ "$(cat "$TMP/core.state")" = running ] || fail 'second candidate did not start core'
[ ! -e "$P" ] || fail 'accepted retry left original pending marker'

# A complete interrupted attempt is reconciled, archived and never overwritten.
make_pending
mkdir "$P/recovery.pending"
cp "$P/out.before" "$P/recovery.pending/out.before"
cp "$P/profile.before" "$P/recovery.pending/profile.before"
cp "$P/filter.before" "$P/recovery.pending/filter.before"
echo PENDING > "$P/recovery.pending/state"
echo running > "$P/recovery.pending/core.before"
INTERRUPTED_HASH="$(sha256sum "$P/recovery.pending/out.before" | awk '{print $1}')"
manual_recover "$WS_PROFILE_ID" > "$TMP/interrupted.out" 2>&1 || { cat "$TMP/interrupted.out"; fail 'complete interrupted attempt cannot be reconciled'; }
FOUND=no
for RESULT in "$TMP"/vpn-transaction.attempts/*/result; do
    if [ "$(cat "$RESULT")" = RECONCILED ]; then
        [ "$(sha256sum "$(dirname "$RESULT")/out.before" | awk '{print $1}')" = "$INTERRUPTED_HASH" ] || fail 'prior attempt snapshot was overwritten'
        FOUND=yes
    fi
done
[ "$FOUND" = yes ] || fail 'prior interrupted attempt not archived'

# Managed terminal receipts must follow the real helper's archive sync and
# checked cleanup. Only sync/rm are controlled here; the helper phase writer,
# recovery transaction, live-byte rollback and receipt paths are unchanged.
FINALIZATION_NONCE=0123456789abcdef0123456789abcdef
export FREENET_PROVIDER_FINALIZATION_STATUS="$TMP/finalization.status"
export FREENET_PROVIDER_FINALIZATION_NONCE="$FINALIZATION_NONCE"
cat > "$TMP/bin/sync" <<EOFSYNC
#!/bin/sh
[ -f '$TMP/finalization.sync.block' ] || exit 0
[ -f '$TMP/finalization.status' ] || exit 0
awk -F '\t' 'NF==5 && \$5=="pending" { found=1 } END { exit !found }' '$TMP/finalization.status' || exit 0
printf entered > '$TMP/finalization.sync.entered'
FINALIZATION_WAIT=0
while [ ! -e '$TMP/finalization.sync.release' ] && [ "\$FINALIZATION_WAIT" -lt 200 ]; do
    sleep 0.02
    FINALIZATION_WAIT=\$((FINALIZATION_WAIT + 1))
done
[ -e '$TMP/finalization.sync.release' ]
EOFSYNC
chmod 755 "$TMP/bin/sync"
wait_finalization_sync() {
    FINALIZATION_WAIT=0
    while [ ! -f "$TMP/finalization.sync.entered" ] && [ "$FINALIZATION_WAIT" -lt 200 ]; do
        sleep 0.02
        FINALIZATION_WAIT=$((FINALIZATION_WAIT + 1))
    done
    [ -f "$TMP/finalization.sync.entered" ] || fail 'actual helper did not enter archive sync'
}
check_finalization_record() {
    awk -F '\t' -v nonce="$FINALIZATION_NONCE" -v pid="$FINALIZATION_OWNER" -v phase="$1" -v outcome="$2" '
        NR!=1 || NF!=5 { bad=1; next }
        $1!="FREENET_PROVIDER_FINALIZATION_V1" || $2!=nonce || $3!=pid || $4!=phase || $5!=outcome { bad=1 }
        END { exit (NR!=1 || bad) }
    ' "$TMP/finalization.status" || fail 'owned finalization record does not match exact invocation/phase'
}

# Rollback has restored all live bytes, but archive rename alone is not a
# terminal receipt. The owned VPN lock must remain held while sync is pending.
make_pending
FINALIZATION_ORIGINAL="$(sha256sum "$P/out.before" "$P/profile.before" "$P/filter.before")"
FINALIZATION_LIVE="$(sha256sum "$TMP/configs/04_outbounds.json" "$TMP/etc/vpn_profile_name" "$TMP/profile.filter")"
echo fail-live-once > "$TMP/route-probe.mode"
rm -f "$TMP/route-probe-live.count"
printf block > "$TMP/finalization.sync.block"
manual_recover "$WS_PROFILE_ID" > "$TMP/finalization-rollback.out" 2>&1 &
FINALIZATION_JOB=$!
wait_finalization_sync
[ -d "$TMP/vpn-mutation.lock" ] || fail 'rollback released writer lock before archive sync'
FINALIZATION_OWNER="$(cat "$TMP/vpn-mutation.lock/pid")"
check_finalization_record rollback-finalizing pending
[ ! -e "$P/recovery.pending" ] || fail 'fixture has not reached post-rename rollback sync'
[ "$FINALIZATION_ORIGINAL" = "$(sha256sum "$P/out.before" "$P/profile.before" "$P/filter.before")" ] || fail 'rollback finalization changed original evidence'
[ "$FINALIZATION_LIVE" = "$(sha256sum "$TMP/configs/04_outbounds.json" "$TMP/etc/vpn_profile_name" "$TMP/profile.filter")" ] || fail 'rollback finalization has not restored live bytes'
if grep -Fq 'ROLLBACK ERROR/STATE: rollback success' "$TMP/finalization-rollback.out"; then fail 'rollback success was emitted before sync'; fi
printf release > "$TMP/finalization.sync.release"
FINALIZATION_RC=0
wait "$FINALIZATION_JOB" || FINALIZATION_RC=$?
[ "$FINALIZATION_RC" -eq 1 ] || fail 'known rollback did not preserve failure exit 1'
check_finalization_record rollback-finalizing rolled-back
[ ! -e "$TMP/vpn-mutation.lock" ] && [ ! -L "$TMP/vpn-mutation.lock" ] || fail 'rollback receipt preceded verified lock cleanup'
grep -Fq 'ROLLBACK ERROR/STATE: rollback success' "$TMP/finalization-rollback.out" || fail 'terminal rollback marker missing after sync'

# Accepted recovery likewise cannot publish SUCCESS before the existing
# archive sync; only the fixed completion section may run after its phase.
rm -f "$TMP/finalization.status" "$TMP/finalization.sync.entered" "$TMP/finalization.sync.release"
echo pass > "$TMP/route-probe.mode"
manual_recover "$PROFILE_ID" > "$TMP/finalization-accepted.out" 2>&1 &
FINALIZATION_JOB=$!
wait_finalization_sync
[ -d "$TMP/vpn-mutation.lock" ] || fail 'accepted recovery released writer lock before archive sync'
FINALIZATION_OWNER="$(cat "$TMP/vpn-mutation.lock/pid")"
check_finalization_record accepted-finalizing pending
if grep -Fq '[FreeNet Provider] RESULT=SUCCESS' "$TMP/finalization-accepted.out"; then fail 'accepted receipt was emitted before sync'; fi
printf release > "$TMP/finalization.sync.release"
FINALIZATION_RC=0
wait "$FINALIZATION_JOB" || FINALIZATION_RC=$?
[ "$FINALIZATION_RC" -eq 0 ] || fail 'accepted finalization did not exit successfully after sync'
check_finalization_record accepted-finalizing accepted
[ ! -e "$P" ] || fail 'accepted finalization left a pending transaction'
[ ! -e "$TMP/vpn-mutation.lock" ] && [ ! -L "$TMP/vpn-mutation.lock" ] || fail 'accepted receipt preceded verified lock cleanup'
grep -Fq '[FreeNet Provider] RESULT=SUCCESS' "$TMP/finalization-accepted.out" || fail 'accepted receipt missing after sync'

# The historical EXIT trap ignores rm failures. A managed terminal receipt
# must not inherit that assumption: refusing lock removal remains UNKNOWN.
rm -f "$TMP/finalization.status" "$TMP/finalization.sync.block"
make_pending
FINALIZATION_REAL_RM="$(command -v rm)"
cat > "$TMP/bin/rm" <<EOFRM
#!/bin/sh
for FINALIZATION_RM_ARG in "\$@"; do
    [ "\$FINALIZATION_RM_ARG" != '$TMP/vpn-mutation.lock' ] || exit 1
done
exec '$FINALIZATION_REAL_RM' "\$@"
EOFRM
chmod 755 "$TMP/bin/rm"
FINALIZATION_RC=0
manual_recover "$WS_PROFILE_ID" > "$TMP/finalization-cleanup-failed.out" 2>&1 || FINALIZATION_RC=$?
[ "$FINALIZATION_RC" -eq 2 ] || fail 'failed lock cleanup did not return UNKNOWN exit 2'
[ -d "$TMP/vpn-mutation.lock" ] || fail 'cleanup failure fixture did not retain the lock'
FINALIZATION_OWNER="$(cat "$TMP/vpn-mutation.lock/pid")"
check_finalization_record accepted-finalizing pending
grep -Fq 'ROLLBACK ERROR/STATE: FAILED/UNKNOWN' "$TMP/finalization-cleanup-failed.out" || fail 'cleanup failure lost UNKNOWN classification'
if grep -Fq '[FreeNet Provider] RESULT=SUCCESS' "$TMP/finalization-cleanup-failed.out"; then fail 'cleanup failure published accepted success'; fi
"$FINALIZATION_REAL_RM" -f "$TMP/bin/rm"
"$FINALIZATION_REAL_RM" -rf "$TMP/vpn-mutation.lock"
unset FREENET_PROVIDER_FINALIZATION_STATUS FREENET_PROVIDER_FINALIZATION_NONCE
echo 'managed finalization: actual delayed rollback/accepted sync and cleanup-failure receipts PASS'

# Incomplete snapshots, ambiguous processes and a live writer remain real STOPs.
make_pending
mkdir "$P/recovery.pending"; echo PENDING > "$P/recovery.pending/state"
BEFORE="$(sha256sum "$TMP/configs/04_outbounds.json")"
if manual_recover "$PROFILE_ID" > "$TMP/incomplete.out" 2>&1; then fail 'incomplete snapshot accepted'; fi
[ "$BEFORE" = "$(sha256sum "$TMP/configs/04_outbounds.json")" ] || fail 'incomplete snapshot mutated config'
rm -rf "$P/recovery.pending"
echo ambiguous > "$TMP/core.state"
if manual_recover "$PROFILE_ID" > "$TMP/ambiguous.out" 2>&1; then fail 'multiple core processes accepted'; fi
[ "$BEFORE" = "$(sha256sum "$TMP/configs/04_outbounds.json")" ] || fail 'ambiguous runtime mutated config'
echo running > "$TMP/core.state"
mkdir "$TMP/vpn-mutation.lock"; echo 9001 > "$TMP/vpn-mutation.lock/pid"
if manual_recover "$PROFILE_ID" > "$TMP/busy.out" 2>&1; then fail 'foreign writer bypassed'; fi
[ "$(cat "$TMP/vpn-mutation.lock/pid")" = 9001 ] || fail 'writer lock stolen'
[ "$BEFORE" = "$(sha256sum "$TMP/configs/04_outbounds.json")" ] || fail 'busy recovery mutated config'

if grep -Eq 'TEST-ID-|TEST-PBK-|TEST-SID-|private-token|vless://' "$TMP/manual-start.out" "$TMP/fail-stopped.out" "$TMP/next-start.out" "$TMP/interrupted.out"; then fail 'recovery output leaked synthetic secrets'; fi
echo 'manual recovery: stopped core, failed candidate, retry, evidence, interrupted attempt and STOP gates PASS'
