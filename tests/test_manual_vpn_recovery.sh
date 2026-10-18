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
