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
    printf '%s\n' '{"outbounds":[{"tag":"vless-reality","protocol":"vless"}]}' > "$R/etc/xray/configs/04_outbounds.json"
    chmod 755 "$R/sbin/freenet-ui" "$R/bin/freenet" "$R/bin/vpn" "$R/bin/blanc_xkeen_update_outbounds.sh" \
        "$R/lib/freenet/migrate_split_dns.sh" "$R/lib/freenet/apply_network_profile.sh" \
        "$R/lib/freenet/apply_provider_profile.sh" "$R/lib/freenet/finalize_setup.sh" \
        "$R/lib/freenet/bootstrap_entware.sh" "$R/lib/freenet/self_update.sh"
}

make_release() {
    D="$1"
    RELEASE_TAG="${2:-v0.2.28}"
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
    printf '%s\n' "{\"tag_name\":\"$RELEASE_TAG\",\"draft\":false,\"prerelease\":false,\"body\":\"## What's Changed\\n* Исправлено отображение release notes by @VoltickVL in https://github.com/VoltickVL/FreeNet-Router/pull/551\\n* release: $RELEASE_TAG by @VoltickVL in https://github.com/VoltickVL/FreeNet-Router/pull/552\\n\\n**Full Changelog**: https://example.invalid\"}" > "$D/release.json"
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
    TARGET="${5:-}"
    FREENET_ROOT="$R" \
    FREENET_CURRENT_VERSION="$CURRENT" \
    FREENET_ARCH=arm64-v8a \
    FREENET_LATEST_TAG="$LATEST" \
    FREENET_TEST_RELEASE_DIR="$D" \
    FREENET_UPDATE_STATE_FILE="$R/var/run/update.state" \
    FREENET_UPDATE_LOCK_DIR="$R/var/run/update.lock" \
    FREENET_SELF_UPDATE_TEST_MODE=yes \
    sh "$SCRIPT" plan "$TARGET"
}

run_apply() {
    R="$1"
    D="$2"
    EXTRA_ENV="$3"
    CURRENT="${4:-v0.2.27}"
    TARGET="${5:-v0.2.28}"
    LATEST="${6:-v0.2.28}"
    rm -rf "$R/var/run/update.lock"
    env \
        FREENET_ROOT="$R" \
        FREENET_CURRENT_VERSION="$CURRENT" \
        FREENET_ARCH=arm64-v8a \
        FREENET_LATEST_TAG="$LATEST" \
        FREENET_TEST_RELEASE_DIR="$D" \
        FREENET_UPDATE_STATE_FILE="$R/var/run/update.state" \
        FREENET_UPDATE_LOCK_DIR="$R/var/run/update.lock" \
        FREENET_SELF_UPDATE_TEST_MODE=yes \
        $EXTRA_ENV \
        sh "$SCRIPT" apply "$TARGET"
}

# Release numbering contract: patch stops at 99, then minor rolls over.
if env FREENET_CURRENT_VERSION=v0.3.99 FREENET_ARCH=arm64-v8a FREENET_LATEST_TAG=v0.3.100 FREENET_SELF_UPDATE_TEST_MODE=yes sh "$SCRIPT" plan > "$TMP/invalid-100.out" 2>&1; then
    fail 'v0.3.100 must be rejected by release tag validation'
fi
grep -Fq 'latest release tag is invalid' "$TMP/invalid-100.out" || fail 'v0.3.100 rejection reason missing'

# Runtime update downloads must stay bounded and expose exact CHECKING progress.
grep -Fq 'DOWNLOAD_RETRIES="${FREENET_UPDATE_DOWNLOAD_RETRIES:-2}"' "$SCRIPT" || fail 'apply download retries must default to 2'
grep -Fq 'CONNECT_TIMEOUT=10' "$SCRIPT" || fail 'apply connect timeout contract missing'
grep -Fq 'MAX_TIME=60' "$SCRIPT" || fail 'apply per-hop max-time contract missing'
grep -Fq 'Загружаем и проверяем $NAME · попытка $ATTEMPT/$TOTAL' "$SCRIPT" || fail 'per-asset CHECKING progress contract missing'

# Regression: application self-update acceptance must not require Split-DNS dns-out.
if grep -Fq 'select(.tag == "dns-out")' "$SCRIPT"; then
    fail 'self-update acceptance must be independent from dns-out topology'
fi

# Regression: application self-update acceptance must not require Xray to be online.
# Xray process state belongs to VPN runtime and may legitimately be offline on a
# Direct-DNS router. Update safety is provided by exact Xray config hash compare.
if grep -Fq 'pidof xray' "$SCRIPT"; then
    fail 'self-update acceptance must be independent from Xray process state'
fi

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
grep -Fq 'RELEASE_NOTES=Исправлено отображение release notes' "$TMP/plan.out" || fail 'human release notes missing'
if grep -Fq 'release: v0.2.28' "$TMP/plan.out"; then fail 'version-bump PR must not pollute release notes'; fi
grep -Fq 'MUTATION=NONE' "$TMP/plan.out" || fail 'plan must be read-only'
[ "$(cat "$R/sbin/freenet-ui")" = "$BEFORE_UI" ] || fail 'plan mutated live UI'
[ "$(sha256sum "$R/etc/xray/configs/04_outbounds.json" | awk '{print $1}')" = "$BEFORE_XRAY" ] || fail 'plan mutated Xray'

# Exact target plan must not need a separate latest-release lookup.
# The fixture deliberately omits FREENET_LATEST_TAG; success proves that the
# selected exact release is validated directly instead of performing redundant
# /releases/latest discovery first.
env \
    FREENET_ROOT="$R" \
    FREENET_CURRENT_VERSION=v0.2.27 \
    FREENET_ARCH=arm64-v8a \
    FREENET_TEST_RELEASE_DIR="$D" \
    FREENET_UPDATE_STATE_FILE="$R/var/run/update.state" \
    FREENET_UPDATE_LOCK_DIR="$R/var/run/update.lock" \
    FREENET_SELF_UPDATE_TEST_MODE=yes \
    sh "$SCRIPT" plan v0.2.28 > "$TMP/exact-target.out" || fail 'exact target plan should not require latest lookup'
grep -Fq 'TARGET_TAG=v0.2.28' "$TMP/exact-target.out" || fail 'exact target plan target missing'
grep -Fq 'MANIFEST_VERIFIED=yes' "$TMP/exact-target.out" || fail 'exact target plan must still verify manifest'

# Already-current is a valid plan but never offers mutation.
run_plan "$R" "$D" v0.2.28 v0.2.28 > "$TMP/current.out" || fail 'already-current plan should succeed'
grep -Fq 'UPDATE_AVAILABLE=no' "$TMP/current.out" || fail 'already-current must be no-op'
grep -Fq 'EXPECTED_DELTA=NONE' "$TMP/current.out" || fail 'already-current delta must be none'

# Exact older target plan is read-only, manifest-verified, and classified as downgrade.
make_release "$D" v0.2.26
run_plan "$R" "$D" v0.2.27 v0.2.28 v0.2.26 > "$TMP/downgrade-plan.out" || fail 'downgrade target plan should succeed'
grep -Fq 'TARGET_TAG=v0.2.26' "$TMP/downgrade-plan.out" || fail 'downgrade target tag missing'
grep -Fq 'DIRECTION=downgrade' "$TMP/downgrade-plan.out" || fail 'downgrade direction missing'
grep -Fq 'UPDATE_AVAILABLE=yes' "$TMP/downgrade-plan.out" || fail 'downgrade target must be actionable'
grep -Fq 'MANIFEST_VERIFIED=yes' "$TMP/downgrade-plan.out" || fail 'downgrade manifest verification missing'
grep -Fq 'MUTATION=NONE' "$TMP/downgrade-plan.out" || fail 'downgrade plan mutated state'

# Restore normal newer fixture for update-path regressions.
make_release "$D" v0.2.28

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
grep -Fq 'PRIMARY_ERROR=SHA-256 mismatch for vpn after 2 attempts' "$R/var/run/update.state" || fail 'checksum failure must identify the concrete asset and retry count'

# Transient asset download failure is retried before declaring a pre-mutation failure.
make_root "$R"
make_release "$D"
run_apply "$R" "$D" "FREENET_TEST_DOWNLOAD_FAIL_ONCE=vpn" > "$TMP/transient-download.out" 2>&1 || {
    cat "$TMP/transient-download.out" >&2
    fail 'single transient asset download failure was not recovered'
}
grep -Fq 'STATE=SUCCESS' "$R/var/run/update.state" || fail 'transient download retry did not finish successfully'

# Transient SHA mismatch is also retried from a fresh copy of the same exact release asset.
make_root "$R"
make_release "$D"
run_apply "$R" "$D" "FREENET_TEST_VERIFY_FAIL_ONCE=vpn" > "$TMP/transient-sha.out" 2>&1 || {
    cat "$TMP/transient-sha.out" >&2
    fail 'single transient SHA verification failure was not recovered'
}
grep -Fq 'STATE=SUCCESS' "$R/var/run/update.state" || fail 'transient SHA retry did not finish successfully'

# Staging failure also stops before mutation.
make_root "$R"
make_release "$D"
if run_apply "$R" "$D" "FREENET_TEST_FAIL_STAGE=staging" > "$TMP/stage.out" 2>&1; then
    fail 'staging failure unexpectedly succeeded'
fi
[ "$(cat "$R/sbin/freenet-ui")" = OLD_UI ] || fail 'staging failure mutated UI'
grep -Fq 'PRIMARY_ERROR=staging validation failed' "$R/var/run/update.state" || fail 'staging primary error missing'

# Successful exact-tag update replaces FreeNet-owned assets only and preserves Xray/config sentinels.
# Test mode has no Xray process at all, so this is also the offline-Xray success regression.
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

# Exact published stable downgrade uses the same transactional engine and preserves user/Xray state.
make_root "$R"
make_release "$D" v0.2.26
XRAY_BEFORE="$(sha256sum "$R/etc/xray/configs/04_outbounds.json" | awk '{print $1}')"
run_apply "$R" "$D" "" v0.2.27 v0.2.26 v0.2.28 > "$TMP/downgrade.out" 2>&1 || {
    cat "$TMP/downgrade.out" >&2
    fail 'compatible exact-tag downgrade failed'
}
grep -Fq 'STATE=SUCCESS' "$R/var/run/update.state" || fail 'downgrade success state missing'
grep -Fq 'TARGET_VERSION=v0.2.26' "$R/var/run/update.state" || fail 'downgrade target state missing'
cmp "$R/sbin/freenet-ui" "$D/freenet-ui-arm64-v8a" >/dev/null || fail 'downgrade target UI not installed'
[ "$(sha256sum "$R/etc/xray/configs/04_outbounds.json" | awk '{print $1}')" = "$XRAY_BEFORE" ] || fail 'downgrade changed Xray config'
[ ! -e "$R/var/run/update.lock" ] || fail 'downgrade left update lock'

# Same-version reinstall is rejected before mutation.
make_root "$R"
make_release "$D" v0.2.27
if run_apply "$R" "$D" "" v0.2.27 v0.2.27 v0.2.28 > "$TMP/same.out" 2>&1; then
    fail 'same-version reinstall unexpectedly succeeded'
fi
[ "$(cat "$R/sbin/freenet-ui")" = OLD_UI ] || fail 'same-version rejection mutated UI'
grep -Fq 'target FreeNet version is already installed' "$R/var/run/update.state" || fail 'same-version reason missing'

# Prerelease metadata is rejected before mutation even when the tag is syntactically valid.
make_root "$R"
make_release "$D" v0.2.26
sed -i 's/"prerelease":false/"prerelease":true/' "$D/release.json"
if run_apply "$R" "$D" "" v0.2.27 v0.2.26 v0.2.28 > "$TMP/prerelease.out" 2>&1; then
    fail 'prerelease target unexpectedly succeeded'
fi
[ "$(cat "$R/sbin/freenet-ui")" = OLD_UI ] || fail 'prerelease rejection mutated UI'
grep -Fq 'target release metadata or Beta channel is not verified' "$R/var/run/update.state" || fail 'prerelease rejection reason missing'

# Beta is never implicit: no opt-in, wrong GitHub metadata, malformed tag,
# or wrong source channel must stop BEFORE touching any live FreeNet asset.
make_root "$R"
make_release "$D" v0.8.0-beta.1
sed -i 's/"prerelease":false/"prerelease":true/' "$D/release.json"
if run_plan "$R" "$D" v0.7.4 v0.7.14 v0.8.0-beta.1 > "$TMP/beta-no-opt-in.out" 2>&1; then
    fail 'Beta plan unexpectedly accepted without opt-in'
fi
grep -Fq 'explicit Beta opt-in required' "$TMP/beta-no-opt-in.out" || fail 'Beta opt-in rejection reason missing'
[ "$(cat "$R/sbin/freenet-ui")" = OLD_UI ] || fail 'Beta plan mutated live UI'

FREENET_UPDATE_BETA_OPT_IN=yes run_plan "$R" "$D" v0.7.4 v0.7.14 v0.8.0-beta.1 > "$TMP/beta-plan.out" || {
    cat "$TMP/beta-plan.out" >&2
    fail 'explicit exact Beta plan failed'
}
grep -Fq 'TARGET_TAG=v0.8.0-beta.1' "$TMP/beta-plan.out" || fail 'Beta exact target changed'
grep -Fq 'MANIFEST_VERIFIED=yes' "$TMP/beta-plan.out" || fail 'Beta manifest verification missing'
grep -Fq 'DIRECTION=upgrade' "$TMP/beta-plan.out" || fail 'Beta upgrade version order incorrect'
grep -Fq 'MUTATION=NONE' "$TMP/beta-plan.out" || fail 'Beta plan mutated state'

# Published tag and prerelease field MUST agree even after explicit opt-in.
sed -i 's/"prerelease":true/"prerelease":false/' "$D/release.json"
if FREENET_UPDATE_BETA_OPT_IN=yes run_plan "$R" "$D" v0.7.4 v0.7.14 v0.8.0-beta.1 > "$TMP/beta-mislabel.out" 2>&1; then
    fail 'mislabelled prerelease metadata was accepted'
fi
grep -Fq 'target release metadata or Beta channel is not verified' "$TMP/beta-mislabel.out" || fail 'mislabel rejection reason missing'
sed -i 's/"prerelease":false/"prerelease":true/' "$D/release.json"

# A verified Beta can be applied transactionally with explicit consent, and
# Xray, subscription and routing config MUST remain unchanged.
make_root "$R"
XRAY_BEFORE="$(sha256sum "$R/etc/xray/configs/04_outbounds.json" | awk '{print $1}')"
SUB_BEFORE="$(sha256sum "$R/etc/freenet/subscription-sentinel" | awk '{print $1}')"
run_apply "$R" "$D" "FREENET_UPDATE_BETA_OPT_IN=yes" v0.7.4 v0.8.0-beta.1 v0.7.14 > "$TMP/beta-apply.out" 2>&1 || {
    cat "$TMP/beta-apply.out" >&2
    fail 'explicit Beta apply/rollback contract failed'
}
grep -Fq 'STATE=SUCCESS' "$R/var/run/update.state" || fail 'Beta not accepted'
grep -Fq 'TARGET_VERSION=v0.8.0-beta.1' "$R/var/run/update.state" || fail 'Beta version not persisted'
[ "$(sha256sum "$R/etc/xray/configs/04_outbounds.json" | awk '{print $1}')" = "$XRAY_BEFORE" ] || fail 'Beta apply mutated Xray'
[ "$(sha256sum "$R/etc/freenet/subscription-sentinel" | awk '{print $1}')" = "$SUB_BEFORE" ] || fail 'Beta apply mutated subscription'
[ ! -e "$R/var/run/update.lock" ] || fail 'Beta apply left a lock'

# Explicit return from Beta to the same-core Stable is an UPGRADE, not a
# downgrade. Stable remains compatible with the regular no-Beta plan.
make_root "$R"
make_release "$D" v0.8.0
run_plan "$R" "$D" v0.8.0-beta.1 v0.8.0 v0.8.0 > "$TMP/beta-to-stable.out" || fail 'Beta-to-Stable plan unavailable'
grep -Fq 'DIRECTION=upgrade' "$TMP/beta-to-stable.out" || fail 'stable must sort after same-core prerelease'
if FREENET_INSTALL_RELEASE_TAG=v0.8.0-beta.1 sh "$ROOT_DIR/install.sh" > "$TMP/beta-install-refused.out" 2>&1; then
    fail 'Beta installer did not require explicit FREENET_INSTALL_BETA=yes'
fi
grep -Fq 'Beta installation requires explicit' "$TMP/beta-install-refused.out" || fail 'Beta installer refusal reason missing'


# P0: verified updater bridge quarantines only a proven orphaned VPN lock.
# No VPN restoration happens here; both original generations remain PENDING.
make_orphan_fixture() {
    make_root "$R"
    mkdir -p "$R/proc" "$R/var/lib/freenet/vpn-transaction.pending/recovery.pending"
    P="$R/var/lib/freenet/vpn-transaction.pending"
    echo PENDING > "$P/state"
    echo PENDING > "$P/recovery.pending/state"
    echo running > "$P/recovery.pending/core.before"
    echo LIVE-PROFILE > "$R/etc/freenet/vpn_profile_name"
    echo LIVE-FILTER > "$R/etc/xray/blanc_profile_filter.regex"
    for kind in out profile filter; do
        case "$kind" in
            out) live="$R/etc/xray/configs/04_outbounds.json" ;;
            profile) live="$R/etc/freenet/vpn_profile_name" ;;
            filter) live="$R/etc/xray/blanc_profile_filter.regex" ;;
        esac
        cp "$live" "$P/$kind.before"
        echo ORIGINAL-SNAPSHOT >> "$P/$kind.before"
        cp "$live" "$P/recovery.pending/$kind.before"
        echo NESTED-SNAPSHOT >> "$P/recovery.pending/$kind.before"
        if cmp -s "$P/$kind.before" "$live" || cmp -s "$P/recovery.pending/$kind.before" "$live"; then
            fail 'fixture must have live bytes different from both protected generations'
        fi
    done
    mkdir "$R/var/run/vpn-mutation.lock"
    echo 9001 > "$R/var/run/vpn-mutation.lock/pid"
}

make_release "$D" v0.8.0-beta.14
sed -i 's/"prerelease":false/"prerelease":true/' "$D/release.json"
echo '{"protocol":2,"tag":"v0.8.0-beta.14"}' > "$D/recovery-protocol.json"
(cd "$D" && sha256sum recovery-protocol.json >> SHA256SUMS)
P0_ENV="FREENET_UPDATE_BETA_OPT_IN=yes FREENET_UPDATE_PROC_ROOT=$R/proc"

make_orphan_fixture
BASE_PENDING="$(find "$P" -type f -exec sha256sum '{}' ';' | sort | sha256sum)"
BASE_LIVE="$(sha256sum "$R/etc/xray/configs/04_outbounds.json" "$R/etc/freenet/vpn_profile_name" "$R/etc/xray/blanc_profile_filter.regex")"
if ! run_apply "$R" "$D" "$P0_ENV" v0.8.0-beta.11 v0.8.0-beta.14 v0.8.0-beta.14 > "$TMP/orphan-ok.out" 2>&1; then
    cat "$TMP/orphan-ok.out" >&2
    fail 'verified exact Beta bridge rejected complete orphaned dual checkpoint'
fi
grep -Fq 'VPN_LOCK_RECOVERY=ORPHAN_QUARANTINED' "$TMP/orphan-ok.out" || fail 'orphan lock transfer receipt absent'
grep -Fxq 'STATE=SUCCESS' "$R/var/run/update.state" || fail 'application bridge failed'
[ "$BASE_PENDING" = "$(find "$P" -type f -exec sha256sum '{}' ';' | sort | sha256sum)" ] || fail 'bridge changed protected original or nested snapshot'
[ "$BASE_LIVE" = "$(sha256sum "$R/etc/xray/configs/04_outbounds.json" "$R/etc/freenet/vpn_profile_name" "$R/etc/xray/blanc_profile_filter.regex")" ] || fail 'bridge changed live VPN'
[ -d "$P/recovery.pending" ] && [ "$(cat "$P/state")" = PENDING ] || fail 'bridge cleared pending checkpoint'
[ ! -e "$R/var/run/vpn-mutation.lock" ] || fail 'bridge left its network lock'
[ ! -e "$R/var/run/update.lock" ] || fail 'bridge left update lock'
QUARANTINE="$(find "$R/var/run" -maxdepth 1 -name 'vpn-mutation.lock.orphaned.*' -type d -print)"
[ -n "$QUARANTINE" ] && [ "$(cat "$QUARANTINE/pid")" = 9001 ] || fail 'original orphaned owner not preserved'
find "$R/var/lib/freenet/vpn-lock-recovery-history" -name '*.state' -exec grep -l 'STATE=CLAIMED' '{}' ';' | grep -q . || fail 'persistent lock recovery evidence missing'

make_orphan_fixture
mkdir "$R/proc/9001"
if run_apply "$R" "$D" "$P0_ENV" v0.8.0-beta.11 v0.8.0-beta.14 v0.8.0-beta.14 > "$TMP/orphan-live-pid.out" 2>&1; then
    fail 'live/reused owner PID bypassed lock'
fi
[ "$(cat "$R/var/run/vpn-mutation.lock/pid")" = 9001 ] || fail 'live owner lost lock'
[ "$(cat "$R/sbin/freenet-ui")" = OLD_UI ] || fail 'live owner guard changed application'
[ ! -d "$R/var/lib/freenet/vpn-lock-recovery-history" ] || fail 'live owner case wrote persistent reclaim record'

make_orphan_fixture
echo /tmp/foreign-updater > "$R/var/run/vpn-mutation.lock/freenet-update-owner"
if run_apply "$R" "$D" "$P0_ENV" v0.8.0-beta.11 v0.8.0-beta.14 v0.8.0-beta.14 > "$TMP/orphan-updater-owner.out" 2>&1; then
    fail 'other updater fence bypassed'
fi
[ -d "$R/var/run/vpn-mutation.lock" ] || fail 'other updater lock removed'

make_orphan_fixture
rm "$P/recovery.pending/filter.before"
if run_apply "$R" "$D" "$P0_ENV" v0.8.0-beta.11 v0.8.0-beta.14 v0.8.0-beta.14 > "$TMP/orphan-incomplete.out" 2>&1; then
    fail 'incomplete nested recovery accepted'
fi
[ -d "$R/var/run/vpn-mutation.lock" ] && [ "$(cat "$R/sbin/freenet-ui")" = OLD_UI ] || fail 'incomplete snapshot had mutation'

make_orphan_fixture
echo corrupt > "$R/var/run/vpn-mutation.lock/pid"
if run_apply "$R" "$D" "$P0_ENV" v0.8.0-beta.11 v0.8.0-beta.14 v0.8.0-beta.14 > "$TMP/orphan-invalid-pid.out" 2>&1; then
    fail 'corrupt lock owner accepted'
fi
[ "$(cat "$R/var/run/vpn-mutation.lock/pid")" = corrupt ] || fail 'corrupt owner evidence changed'

make_orphan_fixture
mkdir "$R/proc/8123"
echo '/opt/lib/freenet/apply_provider_profile.sh apply-core' > "$R/proc/8123/cmdline"
if run_apply "$R" "$D" "$P0_ENV" v0.8.0-beta.11 v0.8.0-beta.14 v0.8.0-beta.14 > "$TMP/orphan-other-writer.out" 2>&1; then
    fail 'another active FreeNet writer accepted'
fi
[ -d "$R/var/run/vpn-mutation.lock" ] || fail 'active writer lock archived'

# Entware BusyBox can have stat -t while FEATURE_STAT_FORMAT is off:
# stat -c exits 1 (invalid option), despite the lock and owner being readable.
# Use host stat only to supply real metadata in that exact terse14 schema.
STAT_BIN="$TMP/stat-bin"
STAT_REAL="$(command -v stat)"
STAT_TRACE="$TMP/stat-trace"
mkdir "$STAT_BIN"
cat > "$STAT_BIN/stat" <<'EOF'
#!/bin/sh
set -eu
case "$1" in
    -c)
        printf 'format\n' >> "$FREENET_TEST_STAT_TRACE"
        case "$FREENET_TEST_STAT_MODE" in
            format_bad)
                printf 'd:42:not-a-time:1700000000\n'
                exit 0
                ;;
            format_read_fail)
                [ "$(grep -c '^format$' "$FREENET_TEST_STAT_TRACE")" = 1 ] || exit 1
                exec "$FREENET_TEST_REAL_STAT" "$@"
                ;;
        esac
        printf "stat: invalid option -- 'c'\nBusyBox multi-call binary.\nUsage: stat [-lt] FILE...\n" >&2
        exit 1
        ;;
    -t)
        [ "$#" = 2 ] || exit 1
        printf 'terse\n' >> "$FREENET_TEST_STAT_TRACE"
        STAT_N="$(grep -c '^terse$' "$FREENET_TEST_STAT_TRACE")"
        case "$FREENET_TEST_STAT_MODE" in
            unsupported) exit 1 ;;
            terse_read_fail) [ "$STAT_N" = 1 ] || exit 1 ;;
            empty) exit 0 ;;
        esac
        STAT_META="$("$FREENET_TEST_REAL_STAT" -c '%s %b %f %u %g %D %i %h %t %T %X %Y %Z %o' "$2")" || exit 1
        case "$FREENET_TEST_STAT_MODE" in
            bad_hex) STAT_META="$(printf '%s\n' "$STAT_META" | awk '{$6="zz"; print}')" ;;
            extra_field) STAT_META="$STAT_META 0" ;;
            changed_inode)
                STAT_META="$(printf '%s\n' "$STAT_META" | awk -v n="$STAT_N" '{$7=(n==1 ? "9007199254740993" : "9007199254740992"); print}')"
                ;;
            changed_device)
                STAT_META="$(printf '%s\n' "$STAT_META" | awk -v n="$STAT_N" '{$6=(n==1 ? "fffffffffffffffe" : "ffffffffffffffff"); print}')"
                ;;
            changed_mtime|changed_ctime)
                STAT_META="$(printf '%s\n' "$STAT_META" | awk -v n="$STAT_N" -v m="$FREENET_TEST_STAT_MODE" '{i=(m=="changed_mtime" ? 12 : 13); $i=(n==1 ? "1700000000" : "1700000001"); print}')"
                ;;
        esac
        case "$FREENET_TEST_STAT_MODE" in
            wrong_path) printf '/other-lock %s\n' "$STAT_META" ;;
            multiline) printf '%s %s\n%s %s\n' "$2" "$STAT_META" "$2" "$STAT_META" ;;
            *) printf '%s %s\n' "$2" "$STAT_META" ;;
        esac
        ;;
    *) exit 1 ;;
esac
EOF
chmod 755 "$STAT_BIN/stat"

stat_apply() {
    : > "$STAT_TRACE"
    PATH="$STAT_BIN:$PATH" \
    FREENET_TEST_REAL_STAT="$STAT_REAL" \
    FREENET_TEST_STAT_MODE="$1" \
    FREENET_TEST_STAT_TRACE="$STAT_TRACE" \
        run_apply "$R" "$D" "$P0_ENV" v0.8.0-beta.11 v0.8.0-beta.14 v0.8.0-beta.14
}

protected_stat_fixture_hash() {
    find "$R/etc" "$R/sbin" "$R/bin" "$R/lib" "$P" -type f -exec sha256sum '{}' ';' | sort | sha256sum
}

make_orphan_fixture
BASE_PENDING="$(find "$P" -type f -exec sha256sum '{}' ';' | sort | sha256sum)"
BASE_LIVE="$(sha256sum "$R/etc/xray/configs/04_outbounds.json" "$R/etc/freenet/vpn_profile_name" "$R/etc/xray/blanc_profile_filter.regex")"
# Literal-prefix parsing must preserve paths containing spaces and glob chars.
STAT_SPACE_LOCK="$R/var/run/vpn mutation [metadata].lock"
mv "$R/var/run/vpn-mutation.lock" "$STAT_SPACE_LOCK"
if ! FREENET_UPDATE_MUTATION_LOCK="$STAT_SPACE_LOCK" stat_apply terse > "$TMP/stat-terse-ok.out" 2>&1; then
    cat "$TMP/stat-terse-ok.out" >&2
    fail 'BusyBox feature-off stat rejected a complete protected app update'
fi
grep -Fxq 'STATE=SUCCESS' "$R/var/run/update.state" || fail 'terse stat update did not complete'
grep -Fq 'VPN_LOCK_RECOVERY=ORPHAN_QUARANTINED' "$TMP/stat-terse-ok.out" || fail 'terse stat lost protected claim receipt'
[ "$(grep -c '^format$' "$STAT_TRACE")" = 1 ] || fail 'terse provider was not pinned after selection'
QUARANTINE=""
for STAT_ARCHIVE in "$STAT_SPACE_LOCK".orphaned.*; do
    [ -d "$STAT_ARCHIVE" ] || continue
    [ "$(cat "$STAT_ARCHIVE/pid")" = 9001 ] || fail 'terse stat lost original lock owner'
    QUARANTINE="$STAT_ARCHIVE"
done
[ -n "$QUARANTINE" ] && [ ! -e "$STAT_SPACE_LOCK" ] || fail 'terse stat did not preserve archive/release its own lock'
# Successful app update changes owned assets, but not live VPN or checkpoints.
[ "$BASE_PENDING" = "$(find "$P" -type f -exec sha256sum '{}' ';' | sort | sha256sum)" ] || fail 'terse stat changed either protected checkpoint'
[ "$BASE_LIVE" = "$(sha256sum "$R/etc/xray/configs/04_outbounds.json" "$R/etc/freenet/vpn_profile_name" "$R/etc/xray/blanc_profile_filter.regex")" ] || fail 'terse stat changed live VPN'
[ "$(cat "$P/state")" = PENDING ] && [ "$(cat "$P/recovery.pending/state")" = PENDING ] || fail 'terse stat cleared pending recovery'

for STAT_CASE in unsupported empty bad_hex extra_field wrong_path multiline format_bad \
    format_read_fail terse_read_fail changed_inode changed_device changed_mtime changed_ctime; do
    make_orphan_fixture
    BASE_PROTECTED="$(protected_stat_fixture_hash)"
    if stat_apply "$STAT_CASE" > "$TMP/stat-$STAT_CASE.out" 2>&1; then
        fail "unsafe stat metadata accepted: $STAT_CASE"
    fi
    grep -Fq 'VPN/network lock ownership could not be proved safe; STOP' "$TMP/stat-$STAT_CASE.out" || fail "stat STOP missing: $STAT_CASE"
    [ "$(cat "$R/var/run/vpn-mutation.lock/pid")" = 9001 ] || fail "stat failure lost original owner: $STAT_CASE"
    [ "$BASE_PROTECTED" = "$(protected_stat_fixture_hash)" ] || fail "stat failure changed protected/app files: $STAT_CASE"
    [ ! -e "$R/var/lib/freenet/vpn-lock-recovery-history" ] || fail "stat failure wrote claim history: $STAT_CASE"
    [ ! -e "$R/var/run/vpn-mutation.lock.recovery-guard" ] || fail "stat failure retained its guard: $STAT_CASE"
    [ ! -e "$R/var/run/update.lock" ] || fail "stat failure retained its updater lock: $STAT_CASE"
    case "$STAT_CASE" in
        format_bad|format_read_fail)
            if grep -q '^terse$' "$STAT_TRACE"; then
                fail "malformed/pinned formatted provider switched to terse: $STAT_CASE"
            fi
            ;;
    esac
done
echo 'BusyBox stat feature-off: protected apply, exact path/schema, pinned provider and changed identity/time STOP gates PASS'

# A completed rollback can have moved recovery.pending into attempts before
# its owner exited without releasing the VPN lock. Only the newest, complete,
# owner-bound ROLLED_BACK attempt may supply evidence for an app-only claim.
make_archived_fixture() {
    make_orphan_fixture
    rm -rf "$P/recovery.pending"
    A_ROOT="$R/var/lib/freenet/vpn-transaction.attempts"
    A="$A_ROOT/20261021T155153Z.9001"
    mkdir -p "$A_ROOT/20261021T155113Z.9001" "$A"
    echo RECONCILED > "$A_ROOT/20261021T155113Z.9001/result"
    # Create all selected archive files after the fixture lock exists.
    echo PENDING > "$A/state"
    echo ROLLED_BACK > "$A/result"
    echo running > "$A/core.before"
    cp "$R/etc/xray/configs/04_outbounds.json" "$A/out.before"
    cp "$R/etc/freenet/vpn_profile_name" "$A/profile.before"
    cp "$R/etc/xray/blanc_profile_filter.regex" "$A/filter.before"
}

archive_protected_hash() {
    find "$R/etc" "$R/sbin" "$R/bin" "$R/lib" "$R/var/lib/freenet" \
        -type f ! -path '*/vpn-lock-recovery-history/*' -exec sha256sum '{}' ';' | sort | sha256sum
}

archive_assert_stopped() {
    [ "$(cat "$R/var/run/vpn-mutation.lock/pid")" = 9001 ] || fail "archive STOP lost orphan owner: $1"
    [ "$BASE_ARCHIVE_PROTECTED" = "$(archive_protected_hash)" ] || fail "archive STOP changed protected/app files: $1"
    [ ! -e "$R/var/run/vpn-mutation.lock.recovery-guard" ] || fail "archive STOP retained its guard: $1"
    [ ! -e "$R/var/run/update.lock" ] || fail "archive STOP retained its app lock: $1"
    for ARCHIVE_QUARANTINE in "$R/var/run/vpn-mutation.lock".orphaned.*; do
        [ ! -e "$ARCHIVE_QUARANTINE" ] && [ ! -L "$ARCHIVE_QUARANTINE" ] || fail "archive STOP quarantined owner: $1"
    done
    if [ -d "$R/var/lib/freenet/vpn-lock-recovery-history" ]; then
        [ "${2:-no}" = observed ] || fail "archive STOP wrote claim history: $1"
        for ARCHIVE_RECEIPT in "$R/var/lib/freenet/vpn-lock-recovery-history"/*.state; do
            [ -f "$ARCHIVE_RECEIPT" ] || continue
            grep -Fxq STATE=OBSERVED "$ARCHIVE_RECEIPT" || fail "archive STOP advanced claim history: $1"
        done
    fi
}

for ARCHIVE_STAT_PROVIDER in format terse; do
    make_archived_fixture
    [ "$ARCHIVE_STAT_PROVIDER" != terse ] || echo stopped > "$A/core.before"
    BASE_PENDING="$(find "$P" "$A_ROOT" -type f -exec sha256sum '{}' ';' | sort | sha256sum)"
    BASE_LIVE="$(sha256sum "$R/etc/xray/configs/04_outbounds.json" "$R/etc/freenet/vpn_profile_name" "$R/etc/xray/blanc_profile_filter.regex")"
    if [ "$ARCHIVE_STAT_PROVIDER" = terse ]; then
        stat_apply terse > "$TMP/archive-$ARCHIVE_STAT_PROVIDER.out" 2>&1 || fail 'terse archived rollback bridge rejected complete evidence'
        [ "$(grep -c '^format$' "$STAT_TRACE")" = 1 ] || fail 'archive file proof changed the pinned terse provider'
    else
        run_apply "$R" "$D" "$P0_ENV" v0.8.0-beta.11 v0.8.0-beta.14 v0.8.0-beta.14 > "$TMP/archive-$ARCHIVE_STAT_PROVIDER.out" 2>&1 || fail 'formatted archived rollback bridge rejected complete evidence'
    fi
    grep -Fxq STATE=SUCCESS "$R/var/run/update.state" || fail 'archived rollback app update did not complete'
    grep -Fq VPN_LOCK_RECOVERY=ORPHAN_QUARANTINED "$TMP/archive-$ARCHIVE_STAT_PROVIDER.out" || fail 'archived rollback quarantine receipt missing'
    [ "$BASE_PENDING" = "$(find "$P" "$A_ROOT" -type f -exec sha256sum '{}' ';' | sort | sha256sum)" ] || fail 'app bridge changed original or archived evidence'
    [ "$BASE_LIVE" = "$(sha256sum "$R/etc/xray/configs/04_outbounds.json" "$R/etc/freenet/vpn_profile_name" "$R/etc/xray/blanc_profile_filter.regex")" ] || fail 'archived rollback app bridge changed live VPN'
    [ "$(cat "$P/state")" = PENDING ] && [ ! -e "$P/recovery.pending" ] || fail 'app bridge recreated/cleared a checkpoint'
    [ ! -e "$R/var/run/vpn-mutation.lock" ] && [ ! -e "$R/var/run/update.lock" ] || fail 'archived rollback bridge retained its locks'
done

for ARCHIVE_CASE in live_owner other_writer wrong_owner missing_result unknown_result noncanonical_marker missing_core unknown_core \
    incomplete_archive incomplete_original original_absent archive_absent mismatched_live incomplete_nested \
    root_symlink archive_symlink file_symlink live_symlink hidden_root hidden_archive unexpected_root \
    invalid_name newer_other_owner newer_unknown latest_tie over_limit; do
    make_archived_fixture
    case "$ARCHIVE_CASE" in
        live_owner) mkdir "$R/proc/9001" ;;
        other_writer)
            mkdir "$R/proc/8123"
            echo '/opt/lib/freenet/apply_provider_profile.sh recover-core' > "$R/proc/8123/cmdline"
            ;;
        wrong_owner) mv "$A" "$A_ROOT/20261021T155153Z.8123" ;;
        missing_result) rm "$A/result" ;;
        unknown_result) echo UNKNOWN > "$A/result" ;;
        noncanonical_marker) printf 'ROLLED_BACK\000\n' > "$A/result" ;;
        missing_core) rm "$A/core.before" ;;
        unknown_core) echo unknown > "$A/core.before" ;;
        incomplete_archive) rm "$A/filter.before" ;;
        incomplete_original) rm "$P/filter.before" ;;
        original_absent) : > "$P/filter.absent" ;;
        archive_absent) : > "$A/filter.absent" ;;
        mismatched_live) echo CHANGED >> "$R/etc/freenet/vpn_profile_name" ;;
        incomplete_nested) mkdir "$P/recovery.pending"; echo PENDING > "$P/recovery.pending/state" ;;
        root_symlink) mv "$A_ROOT" "$A_ROOT.saved"; ln -s "$A_ROOT.saved" "$A_ROOT" ;;
        archive_symlink) mv "$A" "$R/var/lib/freenet/saved-attempt"; ln -s "$R/var/lib/freenet/saved-attempt" "$A" ;;
        file_symlink) mv "$A/out.before" "$R/etc/freenet/saved-out"; ln -s "$R/etc/freenet/saved-out" "$A/out.before" ;;
        live_symlink) mv "$R/etc/freenet/vpn_profile_name" "$R/etc/freenet/saved-profile"; ln -s "$R/etc/freenet/saved-profile" "$R/etc/freenet/vpn_profile_name" ;;
        hidden_root) mkdir "$A_ROOT/.hidden" ;;
        hidden_archive) : > "$A/.hidden" ;;
        unexpected_root) echo UNKNOWN > "$A_ROOT/unexpected" ;;
        invalid_name) mkdir "$A_ROOT/20260230T120000Z.8123" ;;
        newer_other_owner) cp -R "$A" "$A_ROOT/20261021T155154Z.8123" ;;
        newer_unknown) cp -R "$A" "$A_ROOT/20261021T155154Z.9001"; echo UNKNOWN > "$A_ROOT/20261021T155154Z.9001/result" ;;
        latest_tie) cp -R "$A" "$A_ROOT/20261021T155153Z.8123" ;;
        over_limit)
            ARCHIVE_I=0
            while [ "$ARCHIVE_I" -lt 62 ]; do
                ARCHIVE_I=$((ARCHIVE_I + 1))
                mkdir "$A_ROOT/20261020T000000Z.$((10000 + ARCHIVE_I))"
            done
            # The first 64 sorted entries end in the good archive. A reader
            # that truncates there would miss this newer, unsafe 65th entry.
            cp -R "$A" "$A_ROOT/20261021T155154Z.8123"
            echo UNKNOWN > "$A_ROOT/20261021T155154Z.8123/result"
            ;;
    esac
    BASE_ARCHIVE_PROTECTED="$(archive_protected_hash)"
    if run_apply "$R" "$D" "$P0_ENV" v0.8.0-beta.11 v0.8.0-beta.14 v0.8.0-beta.14 > "$TMP/archive-$ARCHIVE_CASE.out" 2>&1; then
        fail "unsafe archived rollback accepted: $ARCHIVE_CASE"
    fi
    archive_assert_stopped "$ARCHIVE_CASE"
done

# Ctime freshness must distinguish adjacent integers above awk's exact numeric
# range; failure on a regular file must not switch the already selected mode.
ARCHIVE_TIME_BIN="$TMP/archive-time-bin"
mkdir "$ARCHIVE_TIME_BIN"
cat > "$ARCHIVE_TIME_BIN/stat" <<'EOF'
#!/bin/sh
set -eu
[ "$1" = -c ] && [ "$#" = 3 ] || { echo terse >> "$FREENET_TEST_STAT_TRACE"; exit 1; }
echo format >> "$FREENET_TEST_STAT_TRACE"
value="$("$FREENET_TEST_REAL_STAT" "$@")"
ctime=9007199254740993
case "$FREENET_TEST_ARCHIVE_TIME" in
    stale_dir) [ "$3" != "$FREENET_TEST_ARCHIVE_DIR" ] || ctime=9007199254740992 ;;
    stale_file) [ "$3" != "$FREENET_TEST_ARCHIVE_DIR/filter.before" ] || ctime=9007199254740992 ;;
    negative_lock) [ "$3" != "$FREENET_ROOT/var/run/vpn-mutation.lock" ] || ctime=-1 ;;
    negative_file) [ "$3" != "$FREENET_TEST_ARCHIVE_DIR/filter.before" ] || ctime=-1 ;;
    malformed_file) [ "$3" != "$FREENET_TEST_ARCHIVE_DIR/filter.before" ] || ctime=unknown ;;
esac
printf '%s:%s\n' "${value%:*}" "$ctime"
EOF
chmod 755 "$ARCHIVE_TIME_BIN/stat"
archive_time_apply() {
    : > "$STAT_TRACE"
    PATH="$ARCHIVE_TIME_BIN:$PATH" FREENET_TEST_REAL_STAT="$STAT_REAL" FREENET_TEST_STAT_TRACE="$STAT_TRACE" \
        FREENET_TEST_ARCHIVE_DIR="$A" FREENET_TEST_ARCHIVE_TIME="$1" \
        run_apply "$R" "$D" "$P0_ENV" v0.8.0-beta.11 v0.8.0-beta.14 v0.8.0-beta.14
}
make_archived_fixture
archive_time_apply equal > "$TMP/archive-time-equal.out" 2>&1 || fail 'exact large equal ctimes rejected'
grep -Fxq STATE=SUCCESS "$R/var/run/update.state" || fail 'large equal ctime fixture did not complete the app update'
for ARCHIVE_TIME in stale_dir stale_file negative_lock negative_file malformed_file; do
    make_archived_fixture
    BASE_ARCHIVE_PROTECTED="$(archive_protected_hash)"
    if archive_time_apply "$ARCHIVE_TIME" > "$TMP/archive-time-$ARCHIVE_TIME.out" 2>&1; then
        fail "unsafe archived ctime accepted: $ARCHIVE_TIME"
    fi
    archive_assert_stopped "$ARCHIVE_TIME"
    if grep -q '^terse$' "$STAT_TRACE"; then fail "archived file metadata failure changed stat mode: $ARCHIVE_TIME"; fi
done

# Exercise the full production claim, including the second and final evidence
# checks. Shims only modify this test's fixture at an existing stage boundary.
ARCHIVE_RACE_BIN="$TMP/archive-race-bin"
ARCHIVE_REAL_SLEEP="$(command -v sleep)"
ARCHIVE_REAL_DATE="$(command -v date)"
ARCHIVE_REAL_MV="$(command -v mv)"
mkdir "$ARCHIVE_RACE_BIN"
cat > "$ARCHIVE_RACE_BIN/stage-command" <<'EOF'
#!/bin/sh
set -eu
name="${0##*/}"
trigger=no
if [ ! -e "$FREENET_TEST_ARCHIVE_FIRED" ]; then
    case "$name:$FREENET_TEST_ARCHIVE_RACE" in
        sleep:replace_archive|sleep:change_original|sleep:change_live_and_archive|sleep:newer_wait|sleep:nested_appears)
            [ "${1:-}" != 1 ] || trigger=yes
            ;;
        date:final_newer)
            for receipt in "$FREENET_ROOT/var/lib/freenet/vpn-lock-recovery-history"/*.state; do
                [ -f "$receipt" ] || continue
                if grep -Fxq STATE=OBSERVED "$receipt"; then trigger=yes; fi
            done
            ;;
        mv:after_install)
            last=''
            for arg in "$@"; do last="$arg"; done
            [ "$last" != "$FREENET_ROOT/sbin/freenet-ui" ] || trigger=yes
            ;;
    esac
fi
if [ "$trigger" = yes ]; then
    : > "$FREENET_TEST_ARCHIVE_FIRED"
    archive="$FREENET_TEST_ARCHIVE_DIR"
    pending="$FREENET_ROOT/var/lib/freenet/vpn-transaction.pending"
    case "$FREENET_TEST_ARCHIVE_RACE" in
        replace_archive|after_install)
            cp "$archive/out.before" "$archive/out.replacement"
            "$FREENET_TEST_REAL_MV" "$archive/out.replacement" "$archive/out.before"
            ;;
        change_original) echo EXTERNAL-WRITER >> "$pending/out.before" ;;
        change_live_and_archive)
            echo EXTERNAL-WRITER >> "$archive/out.before"
            echo EXTERNAL-WRITER >> "$FREENET_ROOT/etc/xray/configs/04_outbounds.json"
            ;;
        newer_wait|final_newer) cp -R "$archive" "$(dirname "$archive")/20261021T155154Z.9001" ;;
        nested_appears) cp -R "$archive" "$pending/recovery.pending" ;;
    esac
    find "$FREENET_ROOT/etc" "$FREENET_ROOT/sbin" "$FREENET_ROOT/bin" "$FREENET_ROOT/lib" "$FREENET_ROOT/var/lib/freenet" \
        -type f ! -path '*/vpn-lock-recovery-history/*' -exec sha256sum '{}' ';' | sort | sha256sum > "$FREENET_TEST_ARCHIVE_EXPECTED"
fi
case "$name" in
    sleep) exec "$FREENET_TEST_REAL_SLEEP" "$@" ;;
    date) exec "$FREENET_TEST_REAL_DATE" "$@" ;;
    mv) exec "$FREENET_TEST_REAL_MV" "$@" ;;
    *) exit 1 ;;
esac
EOF
chmod 755 "$ARCHIVE_RACE_BIN/stage-command"
for ARCHIVE_COMMAND in sleep date mv; do ln -s stage-command "$ARCHIVE_RACE_BIN/$ARCHIVE_COMMAND"; done
archive_race_apply() {
    rm -f "$TMP/archive-race-fired" "$TMP/archive-race-expected"
    PATH="$ARCHIVE_RACE_BIN:$PATH" FREENET_TEST_ARCHIVE_DIR="$A" FREENET_TEST_ARCHIVE_RACE="$1" \
        FREENET_TEST_ARCHIVE_FIRED="$TMP/archive-race-fired" FREENET_TEST_ARCHIVE_EXPECTED="$TMP/archive-race-expected" \
        FREENET_TEST_REAL_SLEEP="$ARCHIVE_REAL_SLEEP" FREENET_TEST_REAL_DATE="$ARCHIVE_REAL_DATE" FREENET_TEST_REAL_MV="$ARCHIVE_REAL_MV" \
        run_apply "$R" "$D" "$P0_ENV" v0.8.0-beta.11 v0.8.0-beta.14 v0.8.0-beta.14
}
for ARCHIVE_RACE in replace_archive change_original change_live_and_archive newer_wait nested_appears final_newer; do
    make_archived_fixture
    if archive_race_apply "$ARCHIVE_RACE" > "$TMP/archive-race-$ARCHIVE_RACE.out" 2>&1; then
        fail "changed archived proof accepted: $ARCHIVE_RACE"
    fi
    [ -f "$TMP/archive-race-fired" ] && [ -s "$TMP/archive-race-expected" ] || fail "archive race fixture did not reach its boundary: $ARCHIVE_RACE"
    BASE_ARCHIVE_PROTECTED="$(cat "$TMP/archive-race-expected")"
    ARCHIVE_HISTORY=no
    [ "$ARCHIVE_RACE" != final_newer ] || ARCHIVE_HISTORY=observed
    archive_assert_stopped "$ARCHIVE_RACE" "$ARCHIVE_HISTORY"
done

# Replacing an archive file with identical bytes after app installation still
# invalidates its pinned identity. Acceptance and rollback remain fail-closed;
# the updater must retain its fence instead of reporting preserved evidence.
make_archived_fixture
BASE_ARCHIVE_PROTECTED="$(archive_protected_hash)"
if archive_race_apply after_install > "$TMP/archive-after-install.out" 2>&1; then
    fail 'app acceptance ignored changed archive identity after claim'
fi
[ -f "$TMP/archive-race-fired" ] || fail 'archive preservation fixture did not reach installation'
grep -Fxq STATE=ROLLBACK_FAILED "$R/var/run/update.state" || fail 'changed archive preservation was reported as an accepted rollback'
[ -d "$R/var/run/vpn-mutation.lock" ] && [ -d "$R/var/run/update.lock" ] || fail 'unconfirmed archive preservation released fences'
[ "$BASE_ARCHIVE_PROTECTED" = "$(archive_protected_hash)" ] || fail 'app rollback changed protected bytes after external archive replacement'
echo 'archived rollback bridge: global bounded inventory, owner/freshness/content identity, stage rechecks and preservation gates PASS'

make_orphan_fixture
echo BAD_ASSET >> "$D/vpn"
if run_apply "$R" "$D" "$P0_ENV" v0.8.0-beta.11 v0.8.0-beta.14 v0.8.0-beta.14 > "$TMP/orphan-bad-sha.out" 2>&1; then
    fail 'checksum failure still claimed orphan lock'
fi
[ -d "$R/var/run/vpn-mutation.lock" ] && [ "$(cat "$R/sbin/freenet-ui")" = OLD_UI ] || fail 'checksum mismatch mutated files or lock'
[ ! -d "$R/var/lib/freenet/vpn-lock-recovery-history" ] || fail 'checksum mismatch wrote recovery history'
echo 'verified orphan VPN lock bridge: successful exact Beta app update and protected STOP gates PASS'

echo 'web self update transactional + explicit Beta channel contract: PASS'