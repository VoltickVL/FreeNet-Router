#!/bin/sh
set -eu
unset FREENET_VPN_TRANSACTION_DIR FREENET_AUTOMATION_STATE FREENET_UPDATE_MUTATION_LOCK
ROOT_DIR="$(CDPATH= cd "$(dirname "$0")/.." && pwd)"
# Reuse the existing synthetic root/release fixture, not its test sequence.
# All paths resolve under mktemp. No real /opt, network or service is touched.
eval "$(sed '/^# Release numbering contract/,$d' "$ROOT_DIR/tests/test_self_update.sh")"

make_protocol_release() {
    make_release "$1" "$2"
    case "$2" in *-beta.*) sed -i 's/"prerelease":false/"prerelease":true/' "$1/release.json" ;; esac
    printf '{"tag":"%s","protocol":2}\n' "$2" > "$1/recovery-protocol.json"
    cp "$1/release.json" "$1/release-metadata.json"
    (cd "$1" && sha256sum recovery-protocol.json release-metadata.json >> SHA256SUMS)
}
R="$TMP/lifecycle-root"
D="$TMP/lifecycle-release"
make_root "$R"
make_protocol_release "$D" v0.8.0-beta.6
P="$R/var/lib/freenet/vpn-transaction.pending"
mkdir -p "$P/recovery.pending"
printf 'PENDING\n' > "$P/state"
printf 'PENDING\n' > "$P/recovery.pending/state"
printf 'PRIVATE_SNAPSHOT_SENTINEL\n' > "$P/out.before"
printf 'MUTATION_BLOCKED=yes\n' > "$R/var/run/freenet-automation.state"
BEFORE="$(sha256sum "$P/state" "$P/recovery.pending/state" "$P/out.before")"
FREENET_UPDATE_BETA_OPT_IN=yes run_plan "$R" "$D" v0.8.0-beta.5 v0.8.0-beta.6 v0.8.0-beta.6 > "$TMP/pending-plan.out" || fail 'protocol-aware plan refused pending VPN'
run_apply "$R" "$D" 'FREENET_UPDATE_BETA_OPT_IN=yes' v0.8.0-beta.5 v0.8.0-beta.6 > "$TMP/pending-apply.out" 2>&1 || { cat "$TMP/pending-apply.out"; fail 'protocol-aware apply refused pending VPN'; }
[ "$BEFORE" = "$(sha256sum "$P/state" "$P/recovery.pending/state" "$P/out.before")" ] || fail 'app update changed protected VPN evidence'
[ "$(cat "$R/var/run/freenet-automation.state")" = 'MUTATION_BLOCKED=yes' ] || fail 'app update cleared VPN latch'
[ ! -e "$R/var/run/vpn-mutation.lock" ] || fail 'app update did not release own writer fence'

after="$(sha256sum "$R/sbin/freenet-ui" "$R/var/run/update.state")"
make_release "$D" v0.7.4
if run_plan "$R" "$D" v0.8.0-beta.6 v0.7.4 v0.7.4 > "$TMP/unsafe-old.out"; then fail 'legacy downgrade ignored pending state'; fi
grep -Fq 'target cannot preserve pending VPN recovery state' "$TMP/unsafe-old.out" || fail 'missing compatibility reason'
[ "$after" = "$(sha256sum "$R/sbin/freenet-ui" "$R/var/run/update.state")" ] || fail 'read-only compatibility plan mutated state'

# The reviewed .18 bridge keeps all old Beta VPN scripts, unlike a full downgrade.
make_release "$D" v0.7.18
VPN_BEFORE="$(sha256sum "$R/bin/vpn" "$R/lib/freenet/apply_provider_profile.sh")"
run_apply "$R" "$D" '' v0.8.0-beta.6 v0.7.18 > "$TMP/bridge.out" 2>&1 || { cat "$TMP/bridge.out"; fail 'reviewed bridge target unavailable'; }
[ "$VPN_BEFORE" = "$(sha256sum "$R/bin/vpn" "$R/lib/freenet/apply_provider_profile.sh")" ] || fail 'bridge downgraded VPN helpers'
[ "$BEFORE" = "$(sha256sum "$P/state" "$P/recovery.pending/state" "$P/out.before")" ] || fail 'bridge changed VPN checkpoint'

# A clean downgrade remains available, no blanket ban on old Stable or Beta.
make_root "$R"; make_release "$D" v0.7.4
run_apply "$R" "$D" '' v0.8.0-beta.6 v0.7.4 > "$TMP/clean-old.out" 2>&1 || fail 'clean exact Stable downgrade blocked'

# A competing invocation must never overwrite the first updater's receipt.
make_root "$R"; make_release "$D" v0.2.28
mkdir "$R/var/run/update.lock"
printf 'STATE=CHECKING\nTARGET_VERSION=v0.2.28\nROLLBACK_STATE=NOT_NEEDED\n' > "$R/var/run/update.state"
BEFORE_STATE="$(cat "$R/var/run/update.state")"
if FREENET_ROOT="$R" FREENET_CURRENT_VERSION=v0.2.27 FREENET_ARCH=arm64-v8a FREENET_TEST_RELEASE_DIR="$D" FREENET_UPDATE_STATE_FILE="$R/var/run/update.state" FREENET_UPDATE_LOCK_DIR="$R/var/run/update.lock" FREENET_SELF_UPDATE_TEST_MODE=yes sh "$SCRIPT" apply v0.2.28 > "$TMP/busy.out" 2>&1; then fail 'overlapping updater accepted'; fi
[ "$BEFORE_STATE" = "$(cat "$R/var/run/update.state")" ] || fail 'busy request destroyed active update receipt'

# A different network writer cannot be displaced by application update.
make_root "$R"; mkdir "$R/var/run/vpn-mutation.lock"; echo VPN_OWNER > "$R/var/run/vpn-mutation.lock/pid"
if run_apply "$R" "$D" '' > "$TMP/net-busy.out" 2>&1; then fail 'update ignored live network writer'; fi
[ "$(cat "$R/var/run/vpn-mutation.lock/pid")" = VPN_OWNER ] || fail 'update stole network lock'
[ "$(cat "$R/sbin/freenet-ui")" = OLD_UI ] || fail 'network-busy update mutated UI'

# TERM after replacement is a controlled rollback, not cleanup-and-continue.
make_root "$R"
printf 'v0.2.27\n' > "$R/etc/freenet/version"
if run_apply "$R" "$D" 'FREENET_TEST_FAIL_STAGE=interrupt-after-replace' > "$TMP/signal.out" 2>&1; then fail 'interrupted updater reported success'; fi
[ "$(cat "$R/sbin/freenet-ui")" = OLD_UI ] || fail 'interrupted update did not restore binary'
[ "$(cat "$R/etc/freenet/version")" = v0.2.27 ] || fail 'rollback lost installed-version marker'
grep -Fq 'ROLLBACK_STATE=SUCCESS' "$R/var/run/update.state" || fail 'signal rollback not confirmed'
[ ! -e "$R/var/run/vpn-mutation.lock" ] || fail 'confirmed rollback left network fence'

# Unknown rollback retains ownership/evidence; owner.pid is a real numeric PID.
make_root "$R"
if run_apply "$R" "$D" 'FREENET_TEST_FAIL_STAGE=accept FREENET_TEST_ROLLBACK_FAIL=yes' > "$TMP/unknown.out" 2>&1; then fail 'failed rollback reported success'; fi
grep -Eq '^[1-9][0-9]*$' "$R/var/run/update.lock/owner.pid" || fail 'owner is not a PID'
cmp "$R/var/run/update.lock/owner.pid" "$R/var/run/vpn-mutation.lock/pid" || fail 'writer fence belongs to different updater'
# Even a missing update lock is not authorization to overwrite unknown state.
rm -rf "$R/var/run/update.lock"
UNKNOWN_BEFORE="$(cat "$R/var/run/update.state")"
if run_apply "$R" "$D" '' > "$TMP/missing-lock.out" 2>&1; then fail 'missing lock bypassed unresolved rollback'; fi
[ "$UNKNOWN_BEFORE" = "$(cat "$R/var/run/update.state")" ] || fail 'retry erased unresolved rollback evidence'

# Unknown textual rollback is not permission to start over.
make_root "$R"
printf 'STATE=FAILED\nROLLBACK_STATE=UNKNOWN\n' > "$R/var/run/update.state"
BEFORE_UNKNOWN="$(cat "$R/var/run/update.state")"
if run_apply "$R" "$D" '' > "$TMP/unknown-receipt.out" 2>&1; then fail 'unknown receipt accepted'; fi
[ "$BEFORE_UNKNOWN" = "$(cat "$R/var/run/update.state")" ] || fail 'unknown receipt overwritten'

echo 'self-update lifecycle: pending-safe update, compatibility, writer fencing, PID, signal rollback PASS'
