#!/bin/sh
set -eu

ROOT_DIR="$(CDPATH= cd "$(dirname "$0")/.." && pwd)"
SCRIPT="$ROOT_DIR/scripts/self_update.sh"
TMP="$(mktemp -d /tmp/freenet-self-update-test.XXXXXX)"
trap 'rm -rf "$TMP"' EXIT HUP INT TERM

fail() {
    echo "self update contract FAIL: $*" >&2
    exit 1
}

make_root() {
    R="$1"
    rm -rf "$R"
    mkdir -p \
        "$R/sbin" "$R/bin" "$R/lib/freenet" "$R/etc/freenet" \
        "$R/etc/xray/configs" "$R/var/run" "$R/backups"

    printf '%s\n' 'OLD_UI' > "$R/sbin/freenet-ui"
    printf '%s\n' 'OLD_MANAGER' > "$R/bin/freenet"
    printf '%s\n' 'OLD_VPN' > "$R/bin/vpn"
    printf '%s\n' 'OLD_UPDATER' > "$R/bin/blanc_xkeen_update_outbounds.sh"
    printf '%s\n' 'OLD_MIGRATE' > "$R/lib/freenet/migrate_split_dns.sh"
    printf '%s\n' 'OLD_NETWORK' > "$R/lib/freenet/apply_network_profile.sh"
    printf '%s\n' 'OLD_PROVIDER' > "$R/lib/freenet/apply_provider_profile.sh"
    printf '%s\n' 'OLD_FINALIZE' > "$R/lib/freenet/finalize_setup.sh"
    printf '%s\n' 'OLD_BOOTSTRAP' > "$R/lib/freenet/bootstrap_entware.sh"
    printf '%s\n' 'OLD_PINS' > "$R/etc/freenet/upstream-pins.env"
    printf '%s\n' 'OLD_SELF_UPDATE' > "$R/lib/freenet/self_update.sh"
    printf '%s\n' 'UI_PORT=1001' > "$R/etc/freenet/freenet.conf"
    printf '%s\n' 'SUBSCRIPTION_SENTINEL' > "$R/etc/freenet/subscription-sentinel"
    printf '%s\n' '{"outbounds":[{"tag":"dns-out"}]}' > "$R/etc/xray/configs/04_outbounds.json"
    chmod 755 "$R/sbin/freenet-ui" "$R/bin/freenet" "$R/bin/vpn" "$R/bin/blanc_xkeen_update_outbounds.sh" \
        "$R/lib/freenet/migrate_split_dns.sh" "$R/lib/freenet/apply_network_profile.sh" \
        "$R/lib/freenet/apply_provider_profile.sh" "$R/lib/freenet/finalize_setup.sh" \
        "$R/lib/freenet/bootstrap_entware.sh" "$R/lib/freenet/self_update.sh"
}

make_release() {
    D="$1"
    rm -rf "$D"
    mkdir -p "$D"

    cat > "$D/freenet-ui-arm64-v8a" <<'EOF'
#!/bin/sh
exit 0
EOF
    for NAME in freenet vpn blanc_xkeen_update_outbounds.sh migrate_split_dns.sh apply_network_profile.sh apply_provider_profile.sh finalize_setup.sh bootstrap_entware.sh; do
        cat > "$D/$NAME" <<'EOF'
#!/bin/sh
exit 0
EOF
    done
    cp "$SCRIPT" "$D/self_update.sh"
    printf '%s\n' 'PIN_POLICY_VERSION=TEST' > "$D/upstream-pins.env"
    chmod 755 "$D/freenet-ui-arm64-v8a" "$D/freenet" "$D/vpn" "$D/blanc_xkeen_update_outbounds.sh" \
        "$D/migrate_split_dns.sh" "$D/apply_network_profile.sh" "$D/apply_provider_profile.sh" \
        "$D/finalize_setup.sh" "$D/bootstrap_entware.sh" "$D/self_update.sh"

    (
        cd "$D"
        sha256sum \
            freenet-ui-arm64-v8a freenet vpn blanc_xkeen_update_outbounds.sh \
            migrate_split_dns.sh apply_network_profile.sh apply_provider_profile.sh \
            finalize_setup.sh bootstrap_entware.sh upstream-pins.env self_update.sh \
            > SHA256SUMS
    )
}

run_plan() {
    R="$1"
    D="$2"
    CURRENT="$3"
    LATEST="$4"
    FREENET_ROOT="$R" \
    FREENET_CURRENT_VERSION="$CURRENT" \
    FREENET_ARCH=arm64-v8a \
    FREENET_LATEST_TAG="$LATEST" \
    FREENET_TEST_RELEASE_DIR="$D" \
    FREENET_UPDATE_STATE_FILE="$R/var/run/update.state" \
    FREENET_UPDATE_LOCK_DIR="$R/var/run/update.lock" \
    FREENET_SELF_UPDATE_TEST_MODE=yes \
    sh "$SCRIPT" plan
}

run_apply() {
    R="$1"
    D="$2"
    EXTRA_ENV="$3"
    rm -rf "$R/var/run/update.lock"
    env \
        FREENET_ROOT="$R" \
        FREENET_CURRENT_VERSION=v0.2.27 \
        FREENET_ARCH=arm64-v8a \
        FREENET_LATEST_TAG=v0.2.28 \
        FREENET_TEST_RELEASE_DIR="$D" \
        FREENET_UPDATE_STATE_FILE="$R/var/run/update.state" \
        FREENET_UPDATE_LOCK_DIR="$R/var/run/update.lock" \
        FREENET_SELF_UPDATE_TEST_MODE=yes \
        $EXTRA_ENV \
        sh "$SCRIPT" apply v0.2.28
}

R="$TMP/root"
D="$TMP/release"
make_root "$R"
make_release "$D"

# Read-only plan: newer exact release is READY and persistent files stay unchanged.
BEFORE_UI="$(cat "$R/sbin/freenet-ui")"
BEFORE_XRAY="$(sha256sum "$R/etc/xray/configs/04_outbounds.json" | awk '{print $1}')"
run_plan "$R" "$D" v0.2.27 v0.2.28 > "$TMP/plan.out" || fail 'newer release plan should succeed'
grep -Fq 'SUCCESS=yes' "$TMP/plan.out" || fail 'plan success missing'
grep -Fq 'READY=yes' "$TMP/plan.out" || fail 'plan ready missing'
grep -Fq 'CURRENT_VERSION=v0.2.27' "$TMP/plan.out" || fail 'current version missing'
grep -Fq 'LATEST_VERSION=v0.2.28' "$TMP/plan.out" || fail 'latest version missing'
grep -Fq 'UPDATE_AVAILABLE=yes' "$TMP/plan.out" || fail 'update availability missing'
grep -Fq 'MANIFEST_VERIFIED=yes' "$TMP/plan.out" || fail 'manifest verification missing'
grep -Fq 'MUTATION=NONE' "$TMP/plan.out" || fail 'plan must be read-only'
[ "$(cat "$R/sbin/freenet-ui")" = "$BEFORE_UI" ] || fail 'plan mutated live UI'
[ "$(sha256sum "$R/etc/xray/configs/04_outbounds.json" | awk '{print $1}')" = "$BEFORE_XRAY" ] || fail 'plan mutated Xray'

# Already-current is a valid plan but never offers mutation.
run_plan "$R" "$D" v0.2.28 v0.2.28 > "$TMP/current.out" || fail 'already-current plan should succeed'
grep -Fq 'UPDATE_AVAILABLE=no' "$TMP/current.out" || fail 'already-current must be no-op'
grep -Fq 'EXPECTED_DELTA=NONE' "$TMP/current.out" || fail 'already-current delta must be none'

# Checksum mismatch stops before live mutation.
make_root "$R"
make_release "$D"
printf '%s\n' 'CORRUPTED' >> "$D/vpn"
if run_apply "$R" "$D" "" > "$TMP/checksum.out" 2>&1; then
    fail 'checksum mismatch unexpectedly succeeded'
fi
[ "$(cat "$R/sbin/freenet-ui")" = OLD_UI ] || fail 'checksum failure mutated UI'
[ "$(cat "$R/bin/vpn")" = OLD_VPN ] || fail 'checksum failure mutated VPN helper'
grep -Fq 'STATE=FAILED' "$R/var/run/update.state" || fail 'checksum failure state missing'
grep -Fq 'ROLLBACK_STATE=NOT_NEEDED' "$R/var/run/update.state" || fail 'checksum failure should not need rollback'

# Staging failure also stops before mutation.
make_root "$R"
make_release "$D"
if run_apply "$R" "$D" "FREENET_TEST_FAIL_STAGE=staging" > "$TMP/stage.out" 2>&1; then
    fail 'staging failure unexpectedly succeeded'
fi
[ "$(cat "$R/sbin/freenet-ui")" = OLD_UI ] || fail 'staging failure mutated UI'
grep -Fq 'PRIMARY_ERROR=staging validation failed' "$R/var/run/update.state" || fail 'staging primary error missing'

# Successful exact-tag update replaces FreeNet-owned assets only and preserves Xray/config sentinels.
make_root "$R"
make_release "$D"
XRAY_BEFORE="$(sha256sum "$R/etc/xray/configs/04_outbounds.json" | awk '{print $1}')"
CONF_BEFORE="$(sha256sum "$R/etc/freenet/freenet.conf" | awk '{print $1}')"
SUB_BEFORE="$(sha256sum "$R/etc/freenet/subscription-sentinel" | awk '{print $1}')"
run_apply "$R" "$D" "" > "$TMP/success.out" 2>&1 || { cat "$TMP/success.out" >&2; fail 'successful apply failed'; }
grep -Fq 'STATE=SUCCESS' "$R/var/run/update.state" || fail 'success state missing'
grep -Fq 'TARGET_VERSION=v0.2.28' "$R/var/run/update.state" || fail 'target state missing'
cmp "$R/sbin/freenet-ui" "$D/freenet-ui-arm64-v8a" >/dev/null || fail 'new UI asset not installed'
cmp "$R/lib/freenet/self_update.sh" "$D/self_update.sh" >/dev/null || fail 'self updater did not update itself'
[ "$(sha256sum "$R/etc/xray/configs/04_outbounds.json" | awk '{print $1}')" = "$XRAY_BEFORE" ] || fail 'success changed Xray config'
[ "$(sha256sum "$R/etc/freenet/freenet.conf" | awk '{print $1}')" = "$CONF_BEFORE" ] || fail 'success changed FreeNet config'
[ "$(sha256sum "$R/etc/freenet/subscription-sentinel" | awk '{print $1}')" = "$SUB_BEFORE" ] || fail 'success changed subscription sentinel'
[ ! -e "$R/var/run/update.lock" ] || fail 'success left update lock'

# Post-replace acceptance failure must restore the exact prior app files.
make_root "$R"
make_release "$D"
if run_apply "$R" "$D" "FREENET_TEST_FAIL_STAGE=accept" > "$TMP/rollback.out" 2>&1; then
    fail 'acceptance failure unexpectedly succeeded'
fi
[ "$(cat "$R/sbin/freenet-ui")" = OLD_UI ] || fail 'rollback did not restore UI'
[ "$(cat "$R/bin/vpn")" = OLD_VPN ] || fail 'rollback did not restore VPN helper'
grep -Fq 'STATE=FAILED' "$R/var/run/update.state" || fail 'rollback state missing'
grep -Fq 'ROLLBACK_STATE=SUCCESS' "$R/var/run/update.state" || fail 'rollback success not reported'
[ ! -e "$R/var/run/update.lock" ] || fail 'successful rollback left update lock'

# ROLLBACK FAILED/UNKNOWN is terminal and deliberately leaves the lock in place.
make_root "$R"
make_release "$D"
if run_apply "$R" "$D" "FREENET_TEST_FAIL_STAGE=accept FREENET_TEST_ROLLBACK_FAIL=yes" > "$TMP/rollback-fail.out" 2>&1; then
    fail 'rollback-failed case unexpectedly succeeded'
else
    RC=$?
    [ "$RC" -eq 2 ] || fail "rollback-failed case returned rc=$RC instead of 2"
fi
grep -Fq 'STATE=ROLLBACK_FAILED' "$R/var/run/update.state" || fail 'terminal rollback state missing'
grep -Fq 'ROLLBACK_STATE=FAILED_UNKNOWN' "$R/var/run/update.state" || fail 'rollback failed/unknown marker missing'
[ -d "$R/var/run/update.lock" ] || fail 'rollback failed/unknown must keep stop lock'
rm -rf "$R/var/run/update.lock"

# Invalid/downgrade target is rejected before mutation.
make_root "$R"
make_release "$D"
if env \
    FREENET_ROOT="$R" FREENET_CURRENT_VERSION=v0.2.27 FREENET_ARCH=arm64-v8a \
    FREENET_TEST_RELEASE_DIR="$D" FREENET_UPDATE_STATE_FILE="$R/var/run/update.state" \
    FREENET_UPDATE_LOCK_DIR="$R/var/run/update.lock" FREENET_SELF_UPDATE_TEST_MODE=yes \
    sh "$SCRIPT" apply v0.2.26 > "$TMP/downgrade.out" 2>&1; then
    fail 'downgrade unexpectedly succeeded'
fi
[ "$(cat "$R/sbin/freenet-ui")" = OLD_UI ] || fail 'downgrade rejection mutated UI'

echo 'web self update transactional contract: PASS'
