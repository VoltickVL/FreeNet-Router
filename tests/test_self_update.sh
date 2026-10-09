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
        "$R/etc/xray/configs" "$R/var/run" "$R/backups" \
        "$R/etc/init.d" "$R/etc/ndm/netfilter.d"

    # Working-stack fixture: installed userland is deliberately preserved.
    # Legacy S99xkeen may check pidof, but a compatible controller explicitly
    # declares foreground support. These binaries are NEVER executed.
    cat > "$R/sbin/xkeen" <<'EOF'
#!/bin/sh
# XKEEN_FOREGROUND supported by this test fixture
exit 0
EOF
    cat > "$R/sbin/xray" <<'EOF'
#!/bin/sh
exit 0
EOF
    cat > "$R/etc/init.d/S99xkeen" <<'EOF'
#!/bin/sh
name_client=xray
proxy_status() { pidof "$name_client" >/dev/null 2>&1; }
EOF
    printf '%s\n' 'EXISTING_NETFILTER' > "$R/etc/ndm/netfilter.d/proxy.sh"
    printf '%s\n' 'EXISTING_SUBSCRIPTION_SECRET' > "$R/etc/xray/blanc_subscription.url"
    printf '%s\n' 'EXISTING_PROFILE' > "$R/etc/freenet/vpn_profile_name"
    printf '%s\n' 'EXISTING_FILTER' > "$R/etc/xray/blanc_profile_filter.regex"
    chmod 755 "$R/sbin/xkeen" "$R/sbin/xray" "$R/etc/init.d/S99xkeen"

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

# GitHub REST API may be exhausted for a shared IP even when github.com
# and verified GitHub release assets work. Simulate HTTP 403 on ALL REST calls.
# No test here reaches the network and no router state may be modified.
MOCK_BIN="$TMP/mockbin"
mkdir -p "$MOCK_BIN"
cat > "$MOCK_BIN/curl" <<'EOF'
#!/bin/sh
HEAD=no
HDR=''
OUT=''
URL=''
while [ "$#" -gt 0 ]; do
    case "$1" in
        -I) HEAD=yes ;;
        -D) shift; HDR="$1" ;;
        -o) shift; OUT="$1" ;;
        --connect-timeout|--max-time|--max-redirs|--resolve) shift ;;
        https://*) URL="$1" ;;
    esac
    shift
done
printf '%s %s\n' "$HEAD" "$URL" >> "$MOCK_CURL_LOG"
case "$URL" in
    https://github.com/VoltickVL/FreeNet-Router/releases/latest)
        [ "$HEAD" = yes ] || exit 22
        printf 'HTTP/1.1 302 Found\r\nLocation: %s\r\n\r\n' "${MOCK_LATEST_LOCATION:-https://github.com/VoltickVL/FreeNet-Router/releases/tag/v0.2.28}" > "$HDR"
        exit 0
        ;;
    https://api.github.com/*)
        printf 'HTTP/1.1 403 Forbidden\r\n\r\n' > "$HDR"
        exit 22
        ;;
    https://github.com/VoltickVL/FreeNet-Router/releases/download/v0.2.28/*)
        NAME="${URL##*/}"
        cp "$MOCK_RELEASE_DIR/$NAME" "$OUT" || exit 1
        printf 'HTTP/1.1 200 OK\r\n\r\n' > "$HDR"
        exit 0
        ;;
esac
exit 22
EOF
chmod 755 "$MOCK_BIN/curl"
cat > "$MOCK_BIN/nslookup" <<'EOF'
#!/bin/sh
exit 1
EOF
chmod 755 "$MOCK_BIN/nslookup"
# Crontab fake permits a pure read-only inventory and unmanaged cron fixtures;
# no real host cron is read or modified.
cat > "$MOCK_BIN/crontab" <<'EOF'
#!/bin/sh
[ "$1" = '-l' ] || exit 2
[ -f "$FREENET_TEST_CRONTAB" ] || exit 1
cat "$FREENET_TEST_CRONTAB"
EOF
chmod 755 "$MOCK_BIN/crontab"
FREENET_TEST_CRONTAB="$TMP/crontab.fixture"
: > "$FREENET_TEST_CRONTAB"
export FREENET_TEST_CRONTAB
PATH="$MOCK_BIN:$PATH"
export PATH
MOCK_CURL_LOG="$TMP/github-web-redirect.calls"
: > "$MOCK_CURL_LOG"
env \
    PATH="$MOCK_BIN:$PATH" \
    MOCK_RELEASE_DIR="$D" MOCK_CURL_LOG="$MOCK_CURL_LOG" \
    FREENET_ROOT="$R" FREENET_CURRENT_VERSION=v0.2.27 FREENET_ARCH=arm64-v8a \
    FREENET_UPDATE_STATE_FILE="$R/var/run/update.state" \
    FREENET_UPDATE_LOCK_DIR="$R/var/run/update.lock" \
    FREENET_SELF_UPDATE_TEST_MODE=yes \
    sh "$SCRIPT" plan > "$TMP/rate-limit-plan.out" 2>&1 || {
        cat "$TMP/rate-limit-plan.out" >&2
        fail 'official GitHub redirect should work when REST API returns 403'
    }
grep -Fq 'TARGET_TAG=v0.2.28' "$TMP/rate-limit-plan.out" || fail 'REST-limited plan did not select stable tag'
grep -Fq 'MANIFEST_VERIFIED=yes' "$TMP/rate-limit-plan.out" || fail 'REST-limited plan did not verify SHA manifest'
grep -Fq 'MUTATION=NONE' "$TMP/rate-limit-plan.out" || fail 'rate-limited plan must be read-only'
grep -Fq 'yes https://github.com/VoltickVL/FreeNet-Router/releases/latest' "$MOCK_CURL_LOG" || fail 'canonical latest GitHub redirect unused'
if grep -Fq 'no https://api.github.com/repos/VoltickVL/FreeNet-Router/releases/latest' "$MOCK_CURL_LOG"; then
    fail 'stable link should not consume a latest REST request'
fi
MOCK_CURL_LOG="$TMP/github-bad-redirect.calls"
: > "$MOCK_CURL_LOG"
if env \
    PATH="$MOCK_BIN:$PATH" \
    MOCK_RELEASE_DIR="$D" MOCK_CURL_LOG="$MOCK_CURL_LOG" \
    MOCK_LATEST_LOCATION='https://evil.example/VoltickVL/FreeNet-Router/releases/tag/v0.2.28' \
    FREENET_ROOT="$R" FREENET_CURRENT_VERSION=v0.2.27 FREENET_ARCH=arm64-v8a \
    FREENET_UPDATE_STATE_FILE="$R/var/run/update.state" \
    FREENET_UPDATE_LOCK_DIR="$R/var/run/update.lock" \
    FREENET_SELF_UPDATE_TEST_MODE=yes \
    sh "$SCRIPT" plan > "$TMP/untrusted-redirect.out" 2>&1; then
    fail 'untrusted GitHub latest redirect must fail closed'
fi
grep -Fq 'MUTATION=NONE' "$TMP/untrusted-redirect.out" || fail 'failed redirect must remain mutation-free'

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
grep -Fq 'target release is not a published stable FreeNet release' "$R/var/run/update.state" || fail 'prerelease rejection reason missing'


# Existing-stack protection: an outdated XKeen with legacy PID gate MUST NOT
# accept a newly downloaded FreeNet helper. It also must not change Xray,
# cron, config, or even the previous FreeNet UI when apply is refused.
make_root "$R"
make_release "$D"
sed -i '/XKEEN_FOREGROUND/d' "$R/sbin/xkeen"
XRAY_BEFORE="$(sha256sum "$R/sbin/xray" "$R/etc/xray/configs/04_outbounds.json" | sha256sum)"
run_plan "$R" "$D" v0.2.27 v0.2.28 > "$TMP/legacy-stack-plan.out" || fail 'legacy-stack plan should report a safe explicit BLOCKED result'
grep -Fq 'READY=no' "$TMP/legacy-stack-plan.out" || fail 'unsupported legacy XKeen must not be READY'
grep -Fq 'STACK_COMPATIBILITY=LEGACY_PIDOF' "$TMP/legacy-stack-plan.out" || fail 'legacy XKeen PID-only contract not detected'
grep -Fq 'MUTATION=NONE' "$TMP/legacy-stack-plan.out" || fail 'legacy plan was not read-only'
if run_apply "$R" "$D" "" > "$TMP/legacy-stack-apply.out" 2>&1; then
    fail 'unsupported legacy XKeen accepted a FreeNet helper upgrade'
fi
[ "$(cat "$R/sbin/freenet-ui")" = OLD_UI ] || fail 'blocked legacy upgrade replaced UI'
[ "$(cat "$R/lib/freenet/apply_provider_profile.sh")" = OLD_PROVIDER ] || fail 'blocked legacy upgrade replaced provider helper'
[ "$(sha256sum "$R/sbin/xray" "$R/etc/xray/configs/04_outbounds.json" | sha256sum)" = "$XRAY_BEFORE" ] ||
    fail 'blocked legacy upgrade modified installed Xray or config'
[ ! -s "$FREENET_TEST_CRONTAB" ] || fail 'blocked legacy upgrade modified cron'
grep -Fq 'LEGACY_PIDOF' "$R/var/run/update.state" || fail 'blocked legacy reason missing in update state'

# Missing init is another uncertain/partial stack. No auto repair/reinstall.
make_root "$R"
rm -f "$R/etc/init.d/S99xkeen"
run_plan "$R" "$D" v0.2.27 v0.2.28 > "$TMP/partial-stack-plan.out" ||
    fail 'partial-stack plan must return a safe classified result'
grep -Fq 'STACK_COMPATIBILITY=PARTIAL_STACK' "$TMP/partial-stack-plan.out" ||
    fail 'partial XKeen/init was not rejected'
if run_apply "$R" "$D" "" > "$TMP/partial-stack-apply.out" 2>&1; then
    fail 'incomplete existing stack accepted a helper upgrade'
fi
[ "$(cat "$R/sbin/freenet-ui")" = OLD_UI ] || fail 'partial stack modified app before STOP'

# Independent legacy cron writer is preserved, but incompatible with a safe
# upgrade of a second VPN controller. Normal XKeen geodata -ug is harmless.
make_root "$R"
printf '*/5 * * * * /opt/sbin/xkeen -restart\n' > "$FREENET_TEST_CRONTAB"
run_plan "$R" "$D" v0.2.27 v0.2.28 > "$TMP/foreign-cron-plan.out" ||
    fail 'unsafe cron plan must return a classified result'
grep -Fq 'STACK_COMPATIBILITY=UNMANAGED_WRITER' "$TMP/foreign-cron-plan.out" ||
    fail 'unmanaged cron restart not detected'
if run_apply "$R" "$D" "" > "$TMP/foreign-cron-apply.out" 2>&1; then
    fail 'foreign cron writer accepted a FreeNet helper upgrade'
fi
grep -Fq '/opt/sbin/xkeen -restart' "$FREENET_TEST_CRONTAB" ||
    fail 'preflight removed an unrelated cron entry'
[ "$(cat "$R/bin/vpn")" = OLD_VPN ] || fail 'cron blocked upgrade modified FreeNet'
printf '30 6 * * * /opt/sbin/xkeen -ug\n' > "$FREENET_TEST_CRONTAB"
run_plan "$R" "$D" v0.2.27 v0.2.28 > "$TMP/geodata-cron-plan.out" ||
    fail 'geodata-only cron should not block app update'
grep -Fq 'READY=yes' "$TMP/geodata-cron-plan.out" || fail 'harmless XKeen geodata cron was incorrectly blocked'
: > "$FREENET_TEST_CRONTAB"

# Provider and legacy AUTO locks are non-destructive STOPs. Neither is stolen.
make_root "$R"
STACK_LOCK="$TMP/provider.active"
mkdir -p "$STACK_LOCK"
FREENET_STACK_MUTATION_LOCK="$STACK_LOCK"
export FREENET_STACK_MUTATION_LOCK
run_plan "$R" "$D" v0.2.27 v0.2.28 > "$TMP/busy-stack-plan.out" ||
    fail 'active mutation must be a safe preflight status'
grep -Fq 'STACK_COMPATIBILITY=MUTATION_BUSY' "$TMP/busy-stack-plan.out" ||
    fail 'active mutation lock did not block update'
[ -d "$STACK_LOCK" ] || fail 'read-only guard removed a runtime mutation lock'
if run_apply "$R" "$D" "" > "$TMP/busy-stack-apply.out" 2>&1; then
    fail 'active mutation lock permitted FreeNet helper upgrade'
fi
[ "$(cat "$R/sbin/freenet-ui")" = OLD_UI ] || fail 'busy upgrade modified running UI'
unset FREENET_STACK_MUTATION_LOCK
rm -rf "$STACK_LOCK"

# Preserve actual core/init/netfilter/cron before and after a successful update,
# even if Xray is offline and no user-level VPN activity is taking place.
make_root "$R"
printf '30 6 * * * /opt/sbin/xkeen -ug\n' > "$FREENET_TEST_CRONTAB"
BEFORE_PROTECTED="$(sha256sum "$R/sbin/xray" "$R/sbin/xkeen" "$R/etc/init.d/S99xkeen" "$R/etc/ndm/netfilter.d/proxy.sh" | sha256sum)"
BEFORE_CRON="$(sha256sum "$FREENET_TEST_CRONTAB")"
run_apply "$R" "$D" "" > "$TMP/protected-success.out" 2>&1 ||
    { cat "$TMP/protected-success.out" >&2; fail 'compatible existing stack update failed'; }
[ "$(sha256sum "$R/sbin/xray" "$R/sbin/xkeen" "$R/etc/init.d/S99xkeen" "$R/etc/ndm/netfilter.d/proxy.sh" | sha256sum)" = "$BEFORE_PROTECTED" ] ||
    fail 'successful update modified Xray/XKeen/init/netfilter'
[ "$(sha256sum "$FREENET_TEST_CRONTAB")" = "$BEFORE_CRON" ] ||
    fail 'successful update modified existing crontab'
[ -s "$R/backups/$(ls "$R/backups" | head -n 1)/protected-stack.before" ] ||
    fail 'update did not record protected stack fingerprints in private backup'
: > "$FREENET_TEST_CRONTAB"

echo 'web self update transactional contract: PASS'