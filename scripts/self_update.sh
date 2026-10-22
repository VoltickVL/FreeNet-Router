#!/bin/sh

# FreeNet Web Self-Update helper.
# plan is read-only with respect to persistent FreeNet state.
# apply downloads an exact release tag, verifies SHA-256, stages all FreeNet-owned
# application assets, snapshots current files, performs controlled replacement,
# restarts only FreeNet UI, validates runtime, and rolls back on failure.

REPO="${FREENET_REPO:-VoltickVL/FreeNet-Router}"
ROOT="${FREENET_ROOT:-/opt}"
CURRENT_VERSION="${FREENET_CURRENT_VERSION:-}"
ARCH="${FREENET_ARCH:-}"
STATE_FILE="${FREENET_UPDATE_STATE_FILE:-$ROOT/var/run/freenet-self-update.state}"
VERSION_FILE="${FREENET_VERSION_FILE:-$ROOT/etc/freenet/version}"
LOCK_DIR="${FREENET_UPDATE_LOCK_DIR:-/tmp/freenet-self-update.lock}"
MUTATION_LOCK_DIR="${FREENET_UPDATE_MUTATION_LOCK:-/tmp/blanc_xkeen_update.lock}"
CHECKPOINT_DIR="${FREENET_VPN_TRANSACTION_DIR:-$ROOT/var/lib/freenet/vpn-transaction.pending}"
AUTOMATION_STATE="${FREENET_AUTOMATION_STATE:-$ROOT/var/run/freenet-automation.state}"
TEST_MODE="${FREENET_SELF_UPDATE_TEST_MODE:-no}"
if [ "$TEST_MODE" = yes ] && [ -z "${FREENET_UPDATE_MUTATION_LOCK:-}" ]; then
    MUTATION_LOCK_DIR="$ROOT/var/run/vpn-mutation.lock"
fi
TEST_RELEASE_DIR="${FREENET_TEST_RELEASE_DIR:-}"
LATEST_OVERRIDE="${FREENET_LATEST_TAG:-}"
FAIL_STAGE="${FREENET_TEST_FAIL_STAGE:-}"
ROLLBACK_FAIL="${FREENET_TEST_ROLLBACK_FAIL:-no}"
TEST_DOWNLOAD_FAIL_ONCE="${FREENET_TEST_DOWNLOAD_FAIL_ONCE:-}"
TEST_VERIFY_FAIL_ONCE="${FREENET_TEST_VERIFY_FAIL_ONCE:-}"
DOWNLOAD_RETRIES="${FREENET_UPDATE_DOWNLOAD_RETRIES:-2}"
case "$DOWNLOAD_RETRIES" in
    ''|*[!0-9]*|0) DOWNLOAD_RETRIES=2 ;;
esac
MODE="${1:-plan}"
TARGET_TAG="${2:-}"
if [ "$MODE" = plan ] || [ "$MODE" = reconcile-rollback-plan ] || [ "$MODE" = reconcile-rollback ]; then
    # Read-only planning must finish inside the Control Center request budget.
    # Apply keeps the longer retry policy for real asset downloads.
    DOWNLOAD_RETRIES=1
fi
TMP_DIR=""
BACKUP_DIR=""
LOCK_HELD=0
MUTATION_LOCK_HELD=0
RECOVERY_GUARD_HELD=0
RECOVERY_GUARD_DIR="$MUTATION_LOCK_DIR.recovery-guard"
ORPHAN_LOCK_ARCHIVE=""
ORPHAN_EVIDENCE_MODE=""
ORPHAN_ARCHIVE_PROOF=""
NETWORK_FENCE_DEFERRED=0
KEEP_LOCK=0
MUTATED=0
LAST_DOWNLOAD_ERROR=""
RELEASE_META_TAG=""
RELEASE_META_FILE=""
UPDATE_UID="$(id -u)"
UPDATE_PROC_ROOT="${FREENET_PROC_ROOT:-/proc}"
UI_SERVICE_READY=0
UI_OLD_SHA=""
UI_OLD_PIN=""
UI_STARTED_PID=""
UI_STARTED_ID=""
UI_STARTED_SHA=""
UI_STARTED_PIN=""
LAST_UPDATE_STAGE=""
RECONCILE_GUARD_HELD=0
RECONCILE_GUARD_DIR="$LOCK_DIR.reconcile-guard"

say() { printf '%s\n' "$*"; }
err() { printf '[FreeNet Web Update] ERROR: %s\n' "$*" >&2; }

cleanup() {
    if [ -n "${RQ_TERMINAL_TMP:-}" ] && [ -f "$RQ_TERMINAL_TMP" ] && [ ! -L "$RQ_TERMINAL_TMP" ]; then
        rm -f "$RQ_TERMINAL_TMP" 2>/dev/null || true
    fi
    if [ "$RECONCILE_GUARD_HELD" = 1 ]; then
        # This is this command's empty, temporary serialization directory,
        # never either of the existing application/network fences.
        rmdir "$RECONCILE_GUARD_DIR" 2>/dev/null || true
        RECONCILE_GUARD_HELD=0
    fi
    if [ "$RECOVERY_GUARD_HELD" = 1 ]; then
        rmdir "$RECOVERY_GUARD_DIR" 2>/dev/null || true
        RECOVERY_GUARD_HELD=0
    fi
    [ -n "$TMP_DIR" ] && [ -d "$TMP_DIR" ] && rm -rf "$TMP_DIR" 2>/dev/null || true
    if [ "$MUTATION_LOCK_HELD" = 1 ] && [ "$KEEP_LOCK" = 0 ]; then
        rm -rf "$MUTATION_LOCK_DIR" 2>/dev/null || true
        MUTATION_LOCK_HELD=0
    fi
    if [ "$LOCK_HELD" = 1 ] && [ "$KEEP_LOCK" = 0 ]; then
        rm -rf "$LOCK_DIR" 2>/dev/null || true
        LOCK_HELD=0
    fi
}
trap cleanup 0
on_update_signal() {
    if [ "$MUTATED" = 1 ]; then
        trap '' 1 2 15
        fail_after_mutation 'update interrupted; restoring application snapshot'
    fi
    [ "$LOCK_HELD" = 1 ] && fail_before_mutation 'update interrupted before mutation'
    exit 1
}
trap on_update_signal 1 2 15

normalize_current() {
    case "$CURRENT_VERSION" in
        v*) ;;
        '') return 1 ;;
        *) CURRENT_VERSION="v$CURRENT_VERSION" ;;
    esac
    valid_tag "$CURRENT_VERSION"
}

beta_tag() {
    printf '%s\n' "$1" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+-beta\.([1-9]|[1-9][0-9])$'
}

valid_tag() {
    TAG="$1"
    printf '%s\n' "$TAG" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+(-beta\.([1-9]|[1-9][0-9]))?$' || return 1
    CORE="${TAG%%-beta.*}"
    PATCH="${CORE##*.}"
    [ "$PATCH" -le 99 ]
}

beta_opted_in() {
    ! beta_tag "$1" || [ "${FREENET_UPDATE_BETA_OPT_IN:-no}" = yes ]
}

version_gt() {
    A="${1#v}"
    B="${2#v}"
    awk -v a="$A" -v b="$B" 'BEGIN {
        split(a,A,"."); split(b,B,".");
        for (i=1; i<=3; i++) {
            av=A[i]+0; bv=B[i]+0;
            if (av > bv) exit 0;
            if (av < bv) exit 1;
        }
        ab=index(a,"-beta."); bb=index(b,"-beta.");
        if (ab && !bb) exit 1;
        if (!ab && bb) exit 0;
        if (ab && bb) {
            an=substr(a,ab+6)+0; bn=substr(b,bb+6)+0;
            if (an>bn) exit 0;
        }
        exit 1;
    }'
}

make_tmp() {
    [ -n "$TMP_DIR" ] && [ -d "$TMP_DIR" ] && return 0
    TMP_DIR="$(umask 077; mktemp -d /tmp/freenet-self-update.XXXXXX)" || return 1
    [ -n "$TMP_DIR" ] && [ -d "$TMP_DIR" ] && [ ! -L "$TMP_DIR" ] || return 1
    chmod 700 "$TMP_DIR"
}

get_arch() {
    [ -n "$ARCH" ] && return 0
    A="$(opkg print-architecture 2>/dev/null)"
    case "$A" in
        *aarch64*) ARCH="arm64-v8a" ;;
        *mipsel*) ARCH="mips32le" ;;
        *mips*) ARCH="mips32" ;;
        *) return 1 ;;
    esac
}

state_value() {
    printf '%s' "$1" | tr '\r\n' '  '
}

write_state() {
    S="$1"
    TARGET="$2"
    MESSAGE="$3"
    PRIMARY="$4"
    ROLLBACK="$5"
    BACKUP="$6"
    DIR="$(dirname "$STATE_FILE")"
    mkdir -p "$DIR" 2>/dev/null || return 1
    TMP_STATE="$STATE_FILE.tmp.$$"
    {
        echo "STATE=$(state_value "$S")"
        echo "FROM_VERSION=$(state_value "$CURRENT_VERSION")"
        echo "TARGET_VERSION=$(state_value "$TARGET")"
        echo "MESSAGE=$(state_value "$MESSAGE")"
        echo "PRIMARY_ERROR=$(state_value "$PRIMARY")"
        echo "ROLLBACK_STATE=$(state_value "$ROLLBACK")"
        echo "BACKUP_DIR=$(state_value "$BACKUP")"
        echo "UPDATED_AT=$(date -u +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || echo unknown)"
    } > "$TMP_STATE" || return 1
    chmod 600 "$TMP_STATE" 2>/dev/null || true
    mv -f "$TMP_STATE" "$STATE_FILE"
}

ui_port() {
    P="$(sed -n 's/^UI_PORT=//p' "$ROOT/etc/freenet/freenet.conf" 2>/dev/null | tail -n 1 | tr -d "'\"\r")"
    [ -n "$P" ] && printf '%s\n' "$P" || printf '%s\n' 1001
}

detect_current() {
    if [ -n "$CURRENT_VERSION" ]; then
        normalize_current
        return $?
    fi

    if [ -s "$VERSION_FILE" ]; then
        CURRENT_VERSION="$(head -n 1 "$VERSION_FILE" 2>/dev/null | tr -d '\r\n')"
        normalize_current && return 0
        CURRENT_VERSION=""
    fi

    PORT="$(ui_port)"
    LIVE_VERSION="$(curl -fsS --connect-timeout 2 --max-time 5 "http://127.0.0.1:$PORT/versionz" 2>/dev/null | tr -d '\r\n' || true)"
    if valid_tag "$LIVE_VERSION"; then
        CURRENT_VERSION="$LIVE_VERSION"
        return 0
    fi

    if [ -f "$STATE_FILE" ] && grep -Eq '^STATE=SUCCESS$' "$STATE_FILE" 2>/dev/null; then
        STATE_VERSION="$(sed -n 's/^TARGET_VERSION=//p' "$STATE_FILE" 2>/dev/null | tail -n 1 | tr -d '\r\n')"
        if valid_tag "$STATE_VERSION"; then
            CURRENT_VERSION="$STATE_VERSION"
            return 0
        fi
    fi

    CURRENT_VERSION=""
    return 1
}

persist_version() {
    TARGET="$1"
    valid_tag "$TARGET" || return 1
    DIR="$(dirname "$VERSION_FILE")"
    mkdir -p "$DIR" 2>/dev/null || return 1
    TMP_VERSION="$VERSION_FILE.tmp.$$"
    printf '%s\n' "$TARGET" > "$TMP_VERSION" || return 1
    chmod 600 "$TMP_VERSION" 2>/dev/null || true
    mv -f "$TMP_VERSION" "$VERSION_FILE"
}

bootstrap_ip() {
    H="$1"
    command -v nslookup >/dev/null 2>&1 || return 1
    for DNS in 77.88.8.8 8.8.8.8; do
        IP="$(nslookup "$H" "$DNS" 2>/dev/null | awk '
            /^Name:/ {seen=1; next}
            seen && /^Address [0-9]+:/ {if ($3 ~ /^[0-9]+\./) {print $3; exit}}
            seen && /^Address:/ {if ($2 ~ /^[0-9]+\./) {print $2; exit}}
        ')"
        [ -n "$IP" ] && { printf '%s\n' "$IP"; return 0; }
    done
    return 1
}

url_host() {
    printf '%s\n' "$1" | sed -n 's#^https://\([^/]*\)/.*#\1#p'
}

download_url() {
    URL="$1"
    OUT="$2"
    LAST_DOWNLOAD_ERROR=""

    if [ -n "$TEST_RELEASE_DIR" ]; then
        NAME="${URL##*/}"
        if [ -n "$TEST_DOWNLOAD_FAIL_ONCE" ] && [ "$NAME" = "$TEST_DOWNLOAD_FAIL_ONCE" ] && [ ! -f "$TMP_DIR/.download-failed-once-$NAME" ]; then
            : > "$TMP_DIR/.download-failed-once-$NAME"
            LAST_DOWNLOAD_ERROR="temporary download failure for $NAME"
            return 1
        fi
        [ -f "$TEST_RELEASE_DIR/$NAME" ] || { LAST_DOWNLOAD_ERROR="test asset missing: $NAME"; return 1; }
        cp "$TEST_RELEASE_DIR/$NAME" "$OUT" || { LAST_DOWNLOAD_ERROR="cannot copy test asset: $NAME"; return 1; }
        return 0
    fi

    CUR="$URL"
    I=0
    while [ "$I" -lt 8 ]; do
        I=$((I + 1))
        H="$(url_host "$CUR")"
        [ -n "$H" ] || { LAST_DOWNLOAD_ERROR="invalid HTTPS URL"; return 1; }
        HDR="$TMP_DIR/headers.$I"
        BODY="$TMP_DIR/body.$I"
        ERRFILE="$TMP_DIR/curl.$I.err"
        rm -f "$HDR" "$BODY" "$ERRFILE"

        CONNECT_TIMEOUT=10
        MAX_TIME=60
        if [ "$MODE" = plan ] || [ "$MODE" = reconcile-rollback-plan ] || [ "$MODE" = reconcile-rollback ]; then
            CONNECT_TIMEOUT=5
            MAX_TIME=12
        fi
        IP="$(bootstrap_ip "$H")"
        if [ -n "$IP" ]; then
            curl -fsS --connect-timeout "$CONNECT_TIMEOUT" --max-time "$MAX_TIME" --resolve "$H:443:$IP" -D "$HDR" "$CUR" -o "$BODY" 2>"$ERRFILE"
            RC=$?
        else
            curl -fsS --connect-timeout "$CONNECT_TIMEOUT" --max-time "$MAX_TIME" -D "$HDR" "$CUR" -o "$BODY" 2>"$ERRFILE"
            RC=$?
        fi
        if [ "$RC" -ne 0 ]; then
            LAST_DOWNLOAD_ERROR="curl failed for $H"
            return 1
        fi

        CODE="$(awk '/^HTTP\// {code=$2} END {print code}' "$HDR")"
        case "$CODE" in
            200|206)
                mv -f "$BODY" "$OUT" || { LAST_DOWNLOAD_ERROR="cannot save downloaded asset"; return 1; }
                return 0
                ;;
            301|302|303|307|308)
                LOC="$(sed -n 's/^[Ll]ocation:[[:space:]]*//p' "$HDR" | tr -d '\r' | tail -n 1)"
                [ -n "$LOC" ] || { LAST_DOWNLOAD_ERROR="redirect without Location"; return 1; }
                case "$LOC" in
                    https://*) CUR="$LOC" ;;
                    /*) CUR="https://$H$LOC" ;;
                    *) LAST_DOWNLOAD_ERROR="unsupported redirect"; return 1 ;;
                esac
                ;;
            *) LAST_DOWNLOAD_ERROR="HTTP ${CODE:-UNKNOWN} from $H"; return 1 ;;
        esac
    done
    LAST_DOWNLOAD_ERROR="too many redirects"
    return 1
}

latest_tag() {
    if [ -n "$LATEST_OVERRIDE" ]; then
        printf '%s\n' "$LATEST_OVERRIDE"
        return 0
    fi
    make_tmp || return 1
    META="$TMP_DIR/latest.json"
    download_url "https://api.github.com/repos/$REPO/releases/latest" "$META" || return 1
    TAG="$(jq -r '.tag_name // empty' "$META" 2>/dev/null)"
    [ -n "$TAG" ] || return 1
    printf '%s\n' "$TAG"
}

fetch_release_metadata() {
    TAG="$1"
    if [ "$RELEASE_META_TAG" = "$TAG" ] && [ -n "$RELEASE_META_FILE" ] && [ -s "$RELEASE_META_FILE" ]; then
        return 0
    fi
    make_tmp || return 1
    META="$TMP_DIR/release-meta.json"
    if [ -n "$TEST_RELEASE_DIR" ]; then
        if [ -f "$TEST_RELEASE_DIR/release.json" ]; then
            cp "$TEST_RELEASE_DIR/release.json" "$META" || return 1
        elif [ "$TEST_MODE" = yes ]; then
            printf '{"tag_name":"%s","draft":false,"prerelease":false}\n' "$TAG" > "$META" || return 1
        else
            return 1
        fi
    else
        if ! download_url "https://api.github.com/repos/$REPO/releases/tags/$TAG" "$META"; then
            fetch_manifest "$TAG" || return 1
            download_verified_asset release-metadata.json "https://github.com/$REPO/releases/download/$TAG/release-metadata.json" "$META" || return 1
        fi
    fi
    META_TAG="$(jq -r '.tag_name // empty' "$META" 2>/dev/null)"
    META_DRAFT="$(jq -r '.draft // false' "$META" 2>/dev/null)"
    META_PRE="$(jq -r '.prerelease // false' "$META" 2>/dev/null)"
    [ "$META_TAG" = "$TAG" ] || return 1
    [ "$META_DRAFT" != true ] || return 1
    if beta_tag "$TAG"; then
        beta_opted_in "$TAG" || return 1
        [ "$META_PRE" = true ] || return 1
    else
        [ "$META_PRE" != true ] || return 1
    fi
    RELEASE_META_TAG="$TAG"
    RELEASE_META_FILE="$META"
    return 0
}

release_notes() {
    TAG="$1"
    fetch_release_metadata "$TAG" || return 1
    META="$RELEASE_META_FILE"
    jq -r '.body // ""' "$META" 2>/dev/null | awk '
        BEGIN { out=""; count=0 }
        /^[*-][[:space:]]+/ {
            line=$0
            sub(/^[*-][[:space:]]+/, "", line)
            if (line ~ /^release:[[:space:]]*v[0-9]/) next
            sub(/[[:space:]]+by[[:space:]]+@[^[:space:]]+[[:space:]]+in[[:space:]]+https?:\/\/[^[:space:]]+.*/, "", line)
            sub(/[[:space:]]+in[[:space:]]+https?:\/\/[^[:space:]]+.*/, "", line)
            gsub(/[*_`]/, "", line)
            gsub(/[[:space:]]+/, " ", line)
            if (line == "") next
            if (count > 0) out = out " | "
            out = out line
            count++
            if (count >= 4) { print out; exit }
        }
        END { if (count > 0 && count < 4) print out }
    '
}

recovery_bridge_target() {
    beta_tag "$CURRENT_VERSION" && [ "${1:-$TARGET_TAG}" = v0.7.18 ]
}

compatible_update_target() {
    COMPAT_TARGET="$1"
    if [ ! -e "$CHECKPOINT_DIR" ] && [ ! -L "$CHECKPOINT_DIR" ] &&
       ! grep -qs '^MUTATION_BLOCKED=yes$' "$AUTOMATION_STATE" 2>/dev/null; then
        return 0
    fi
    # The reviewed bridge runs only Recovery and preserves Beta VPN helpers.
    recovery_bridge_target "$COMPAT_TARGET" && return 0
    # Exact .6 introduced the protocol; later releases must declare it in
    # SHA-verified metadata (this is not inferred merely from a larger number).
    fetch_manifest "$COMPAT_TARGET" || return 1
    PROTOCOL_FILE="$TMP_DIR/recovery-protocol.json"
    download_verified_asset recovery-protocol.json "https://github.com/$REPO/releases/download/$COMPAT_TARGET/recovery-protocol.json" "$PROTOCOL_FILE" || return 1
    jq -e --arg tag "$COMPAT_TARGET" '.protocol == 2 and .tag == $tag' "$PROTOCOL_FILE" >/dev/null 2>&1
}

asset_list() {
    if recovery_bridge_target; then
        printf '%s\n' "freenet-ui-$ARCH" self_update.sh
        return
    fi
    echo "freenet-ui-$ARCH"
    echo "freenet"
    echo "vpn"
    echo "blanc_xkeen_update_outbounds.sh"
    echo "migrate_split_dns.sh"
    echo "apply_network_profile.sh"
    echo "apply_provider_profile.sh"
    echo "finalize_setup.sh"
    echo "bootstrap_entware.sh"
    echo "upstream-pins.env"
    echo "self_update.sh"
}

asset_dest() {
    case "$1" in
        freenet-ui-*) echo "$ROOT/sbin/freenet-ui" ;;
        freenet) echo "$ROOT/bin/freenet" ;;
        vpn) echo "$ROOT/bin/vpn" ;;
        blanc_xkeen_update_outbounds.sh) echo "$ROOT/bin/blanc_xkeen_update_outbounds.sh" ;;
        migrate_split_dns.sh) echo "$ROOT/lib/freenet/migrate_split_dns.sh" ;;
        apply_network_profile.sh) echo "$ROOT/lib/freenet/apply_network_profile.sh" ;;
        apply_provider_profile.sh) echo "$ROOT/lib/freenet/apply_provider_profile.sh" ;;
        finalize_setup.sh) echo "$ROOT/lib/freenet/finalize_setup.sh" ;;
        bootstrap_entware.sh) echo "$ROOT/lib/freenet/bootstrap_entware.sh" ;;
        upstream-pins.env) echo "$ROOT/etc/freenet/upstream-pins.env" ;;
        self_update.sh) echo "$ROOT/lib/freenet/self_update.sh" ;;
        *) return 1 ;;
    esac
}

asset_mode() {
    [ "$1" = upstream-pins.env ] && echo 600 || echo 755
}

manifest_expected() {
    NAME="$1"
    awk -v n="$NAME" '$2==n {print $1; exit}' "$TMP_DIR/SHA256SUMS"
}

manifest_complete() {
    for NAME in $(asset_list); do
        SUM="$(manifest_expected "$NAME")"
        printf '%s\n' "$SUM" | grep -Eq '^[0-9a-f]{64}$' || return 1
    done
    return 0
}

fetch_manifest() {
    TAG="$1"
    make_tmp || return 1
    BASE="https://github.com/$REPO/releases/download/$TAG"
    download_file_with_retry "SHA256SUMS" "$BASE/SHA256SUMS" "$TMP_DIR/SHA256SUMS" || return 1
    if ! manifest_complete; then
        LAST_DOWNLOAD_ERROR="release SHA256SUMS is incomplete"
        return 1
    fi
    return 0
}

verify_asset() {
    NAME="$1"
    FILE="$2"
    EXPECTED="$(manifest_expected "$NAME")"
    if [ -z "$EXPECTED" ]; then
        LAST_DOWNLOAD_ERROR="SHA-256 manifest entry missing for $NAME"
        return 1
    fi
    if [ -n "$TEST_VERIFY_FAIL_ONCE" ] && [ "$NAME" = "$TEST_VERIFY_FAIL_ONCE" ] && [ ! -f "$TMP_DIR/.verify-failed-once-$NAME" ]; then
        : > "$TMP_DIR/.verify-failed-once-$NAME"
        LAST_DOWNLOAD_ERROR="SHA-256 mismatch for $NAME"
        return 1
    fi
    ACTUAL="$(sha256sum "$FILE" | awk '{print $1}')"
    if [ "$EXPECTED" != "$ACTUAL" ]; then
        LAST_DOWNLOAD_ERROR="SHA-256 mismatch for $NAME"
        return 1
    fi
    return 0
}

retry_pause() {
    [ "$TEST_MODE" = yes ] && return 0
    sleep "$1"
}

write_checking_progress() {
    NAME="$1"
    ATTEMPT="$2"
    TOTAL="$3"
    [ "$MODE" = apply ] || return 0
    [ "$LOCK_HELD" = 1 ] || return 0
    write_state CHECKING "$TARGET_TAG" "Загружаем и проверяем $NAME · попытка $ATTEMPT/$TOTAL" '' NOT_NEEDED '' || true
}

download_file_with_retry() {
    NAME="$1"
    URL="$2"
    OUT="$3"
    ATTEMPT=1
    LAST=""
    while [ "$ATTEMPT" -le "$DOWNLOAD_RETRIES" ]; do
        write_checking_progress "$NAME" "$ATTEMPT" "$DOWNLOAD_RETRIES"
        rm -f "$OUT" 2>/dev/null || true
        if download_url "$URL" "$OUT"; then
            return 0
        fi
        LAST="$LAST_DOWNLOAD_ERROR"
        [ "$ATTEMPT" -lt "$DOWNLOAD_RETRIES" ] && retry_pause "$ATTEMPT"
        ATTEMPT=$((ATTEMPT + 1))
    done
    LAST_DOWNLOAD_ERROR="${LAST:-download failed for $NAME} after $DOWNLOAD_RETRIES attempts"
    return 1
}

download_verified_asset() {
    NAME="$1"
    URL="$2"
    OUT="$3"
    ATTEMPT=1
    LAST=""
    while [ "$ATTEMPT" -le "$DOWNLOAD_RETRIES" ]; do
        write_checking_progress "$NAME" "$ATTEMPT" "$DOWNLOAD_RETRIES"
        rm -f "$OUT" 2>/dev/null || true
        if download_url "$URL" "$OUT" && verify_asset "$NAME" "$OUT"; then
            return 0
        fi
        LAST="$LAST_DOWNLOAD_ERROR"
        [ "$ATTEMPT" -lt "$DOWNLOAD_RETRIES" ] && retry_pause "$ATTEMPT"
        ATTEMPT=$((ATTEMPT + 1))
    done
    LAST_DOWNLOAD_ERROR="${LAST:-download or SHA-256 verification failed for $NAME} after $DOWNLOAD_RETRIES attempts"
    return 1
}

download_assets() {
    TAG="$1"
    fetch_manifest "$TAG" || return 1
    BASE="https://github.com/$REPO/releases/download/$TAG"
    for NAME in $(asset_list); do
        download_verified_asset "$NAME" "$BASE/$NAME" "$TMP_DIR/$NAME" || return 1
        MODE_NOW="$(asset_mode "$NAME")"
        if ! chmod "$MODE_NOW" "$TMP_DIR/$NAME" 2>/dev/null; then
            LAST_DOWNLOAD_ERROR="cannot set staged mode for $NAME"
            return 1
        fi
    done
    return 0
}

validate_stage() {
    [ "$FAIL_STAGE" = staging ] && return 1
    [ -s "$TMP_DIR/freenet-ui-$ARCH" ] || return 1
    if recovery_bridge_target; then
        sh -n "$TMP_DIR/self_update.sh" >/dev/null 2>&1
        return $?
    fi
    for NAME in freenet vpn blanc_xkeen_update_outbounds.sh migrate_split_dns.sh apply_network_profile.sh apply_provider_profile.sh finalize_setup.sh bootstrap_entware.sh self_update.sh; do
        sh -n "$TMP_DIR/$NAME" >/dev/null 2>&1 || return 1
    done
    return 0
}

snapshot_xray() {
    OUT="$1"
    : > "$OUT"
    if [ -d "$ROOT/etc/xray/configs" ]; then
        find "$ROOT/etc/xray/configs" -maxdepth 1 -type f -name '*.json' -print 2>/dev/null | sort | while IFS= read -r F; do
            sha256sum "$F" || exit 1
        done > "$OUT" || return 1
    fi
}

backup_one() {
    SRC="$1"
    KEY="$2"
    if [ -f "$SRC" ]; then
        cp -p "$SRC" "$BACKUP_DIR/$KEY.before" || return 1
        echo yes > "$BACKUP_DIR/$KEY.exists"
    else
        echo no > "$BACKUP_DIR/$KEY.exists"
    fi
}

snapshot_preserved_state() {
    PRESERVED_OUT="$1"
    : > "$PRESERVED_OUT" || return 1
    for PRESERVED_PATH in "$ROOT/etc/freenet/freenet.conf" \
        "$ROOT/etc/freenet/vpn_profile_name" "$ROOT/etc/xray/blanc_profile_filter.regex" \
        "$ROOT/etc/xray/blanc_subscription.url"; do
        [ ! -L "$PRESERVED_PATH" ] || return 1
        if [ -f "$PRESERVED_PATH" ]; then
            sha256sum "$PRESERVED_PATH" >> "$PRESERVED_OUT" || return 1
        else
            [ ! -e "$PRESERVED_PATH" ] || return 1
            printf 'ABSENT %s\n' "$PRESERVED_PATH" >> "$PRESERVED_OUT" || return 1
        fi
    done
    if [ -e "$CHECKPOINT_DIR" ] || [ -L "$CHECKPOINT_DIR" ]; then
        [ -d "$CHECKPOINT_DIR" ] && [ ! -L "$CHECKPOINT_DIR" ] || return 1
        [ -z "$(find "$CHECKPOINT_DIR" -type l -print)" ] || return 1
        printf 'CHECKPOINT_PRESENT\n' >> "$PRESERVED_OUT" || return 1
        find "$CHECKPOINT_DIR" -type f -print | sort | while IFS= read -r PRESERVED_FILE; do
            sha256sum "$PRESERVED_FILE" || exit 1
        done >> "$PRESERVED_OUT" || return 1
    else
        printf 'CHECKPOINT_ABSENT\n' >> "$PRESERVED_OUT" || return 1
    fi
    # The archived-rollback claim also pins evidence outside CHECKPOINT_DIR.
    # Keep the ordinary snapshot format unchanged, including the pre-claim one.
    [ "$ORPHAN_EVIDENCE_MODE" != archived ] || vpn_lock_recovery_evidence_safe
}

prepare_backup() {
    STAMP="$(date +%Y%m%d-%H%M%S 2>/dev/null)"
    [ -n "$STAMP" ] || STAMP="$$"
    BACKUP_DIR="$ROOT/backups/freenet-web-update-$STAMP.$$"
    umask 077
    mkdir -p "$ROOT/backups" || return 1
    mkdir "$BACKUP_DIR" || return 1
    chmod 700 "$BACKUP_DIR" || return 1
    I=0
    for NAME in $(asset_list); do
        I=$((I + 1))
        DEST="$(asset_dest "$NAME")" || return 1
        backup_one "$DEST" "asset-$I" || return 1
    done
    backup_one "$VERSION_FILE" version-marker || return 1
    snapshot_preserved_state "$BACKUP_DIR/preserved.before" || return 1
    snapshot_xray "$BACKUP_DIR/xray-hashes.before" || return 1
}

# Numeric metadata without GNU-only stat assumptions. Device/inode/timestamps
# remain strings; Entware's shell arithmetic can be only 32 bits wide.
update_stat() (
    us_path="$1"
    if us_value="$(LC_ALL=C stat -L -c '%f %u %g %h %s %D %i %Y %Z' "$us_path" 2>/dev/null)"; then
        us_fields="$us_value"
    else
        us_value="$(LC_ALL=C stat -L -t "$us_path" 2>/dev/null)" || return 1
        case "$us_value" in "$us_path "*) us_value="${us_value#"$us_path "}" ;; *) return 1 ;; esac
        us_fields="$(printf '%s\n' "$us_value" | LC_ALL=C awk '
            NR == 1 && NF == 14 { print $3, $4, $5, $8, $1, $6, $7, $12, $13; ok=1 }
            END { if (NR != 1 || !ok) exit 1 }
        ')" || return 1
    fi
    printf '%s\n' "$us_fields" | LC_ALL=C awk '
        function hex(s, n,i,c) {
            n=0; s=tolower(s)
            for (i=1; i<=length(s); i++) { c=index("0123456789abcdef",substr(s,i,1))-1; if(c<0)return -1; n=n*16+c }
            return n
        }
        NR != 1 || NF != 9 { bad=1; next }
        {
            if ($1 !~ /^[0-9a-fA-F]+$/ || length($1)>8 || $6 !~ /^[0-9a-fA-F]+$/) bad=1
            for(i=2;i<=9;i++) if(i!=6 && $i !~ /^[0-9]+$/) bad=1
            mode=hex($1)
            if(mode<0) bad=1
            out=tolower($6) ":" $7 ":" $2 ":" $3 ":" sprintf("%o",mode%4096) ":" $4 ":" $5 ":" $8 ":" $9 ":" int(mode/4096)
        }
        END { if(NR!=1 || bad) exit 1; print out }
    '
)

update_sha() (
    uh_value="$(sha256sum < "$1")" || return 1
    uh_value="${uh_value%% *}"
    [ "${#uh_value}" -eq 64 ] || return 1
    case "$uh_value" in *[!0-9a-f]*) return 1 ;; esac
    printf '%s\n' "$uh_value"
)

update_file_proof() (
    uf_path="$1"; uf_limit="$2"; uf_mode="${3:-}"
    [ -f "$uf_path" ] && [ ! -L "$uf_path" ] && [ -r "$uf_path" ] || return 1
    uf_before="$(update_stat "$uf_path")" || return 1
    printf '%s\n' "$uf_before" | LC_ALL=C awk -F: -v uid="$UPDATE_UID" -v limit="$uf_limit" -v mode="$uf_mode" '
        NF!=10 || $3!=uid || $6!="1" || $7>limit || $10!="8" { exit 1 }
        $5 !~ /^[0-7][0-7][0-7]$/ { exit 1 }
        substr($5,2,1) ~ /[2367]/ || substr($5,3,1) ~ /[2367]/ { exit 1 }
        mode!="" && $5!=mode { exit 1 }
    ' || return 1
    uf_sha="$(update_sha "$uf_path")" || return 1
    [ -f "$uf_path" ] && [ ! -L "$uf_path" ] || return 1
    uf_after="$(update_stat "$uf_path")" || return 1
    [ "$uf_before" = "$uf_after" ] || return 1
    printf '%s:%s\n' "$uf_before" "$uf_sha"
)

update_directory_proof() (
    ud_path="$1"; ud_mode="${2:-}"
    [ -d "$ud_path" ] && [ ! -L "$ud_path" ] && [ -r "$ud_path" ] && [ -x "$ud_path" ] || return 1
    ud_value="$(update_stat "$ud_path")" || return 1
    printf '%s\n' "$ud_value" | LC_ALL=C awk -F: -v uid="$UPDATE_UID" -v mode="$ud_mode" '
        NF!=10 || $3!=uid || $10!="4" { exit 1 }
        $5 !~ /^[0-7][0-7][0-7]$/ { exit 1 }
        substr($5,2,1) ~ /[2367]/ || substr($5,3,1) ~ /[2367]/ { exit 1 }
        mode!="" && $5!=mode { exit 1 }
    ' || return 1
    printf '%s\n' "$ud_value"
)

update_stage_error() {
    LAST_UPDATE_STAGE="$1"
    err "stage=$1: $2"
    case "$MODE" in
        reconcile-rollback*) return 1 ;;
    esac
    if [ -n "$BACKUP_DIR" ] && [ -d "$BACKUP_DIR" ] && [ ! -L "$BACKUP_DIR" ] &&
       [ ! -L "$BACKUP_DIR/service.log" ]; then
        (umask 077; printf 'STAGE=%s ERROR=%s\n' "$1" "$2" >> "$BACKUP_DIR/service.log") || return 1
    fi
    return 1
}

ui_prepare_service() {
    UI_BIN="$ROOT/sbin/freenet-ui"
    UI_IMAGE_PATH="$(readlink -f "$UI_BIN")" || return 1
    [ -n "$UI_IMAGE_PATH" ] && [ -f "$UI_BIN" ] && [ ! -L "$UI_BIN" ] || return 1
    UI_INIT="$ROOT/etc/init.d/S99freenet-ui"
    UI_INIT_PROOF="$(update_file_proof "$UI_INIT" 16384 755)" || return 1
    # Parse the stock launch specification as data. Never source/eval init or
    # inherit its process-name based service control; unknown custom init is STOP.
    UI_LISTEN="$(LC_ALL=C awk '
        /^[[:space:]]*$/ || /^#/ { next }
        /^ENABLED=yes$/ { enabled++; next }
        /^PROCS=freenet-ui$/ { procs++; next }
        /^ARGS="-listen [0-9.]+:[0-9]+"$/ { args++; value=$0; sub(/^ARGS="-listen /,"",value); sub(/"$/,"",value); next }
        /^PREARGS=""$/ { pre++; next }
        /^DESC="\$PROCS"$/ { desc++; next }
        /^PATH=\/opt\/sbin:\/opt\/bin:\/usr\/local\/sbin:\/usr\/local\/bin:\/usr\/sbin:\/usr\/bin:\/sbin:\/bin$/ { path++; next }
        /^\. \/opt\/etc\/init.d\/rc.func$/ { source++; next }
        { bad=1 }
        END { if(bad || enabled!=1 || procs!=1 || args!=1 || pre!=1 || desc!=1 || path!=1 || source!=1) exit 1; print value }
    ' "$UI_INIT")" || return 1
    UI_HOST="${UI_LISTEN%:*}"
    UI_PORT="${UI_LISTEN##*:}"
    printf '%s\n' "$UI_PORT" | LC_ALL=C awk 'NR!=1 || $0 !~ /^[0-9]+$/ || $0<1 || $0>65535 {exit 1}' || return 1
    [ "$(ui_port)" = "$UI_PORT" ] || return 1
    [ "$UI_HOST" != 0.0.0.0 ] || return 1
    UI_HOST_HEX="$(ui_ipv4_hex "$UI_HOST")" || return 1
    UI_LOOP_HEX="$(ui_ipv4_hex 127.0.0.1)" || return 1
    UI_PORT_HEX="$(printf '%04X' "$UI_PORT")" || return 1
    [ -d "$UPDATE_PROC_ROOT" ] && [ ! -L "$UPDATE_PROC_ROOT" ] && [ -r "$UPDATE_PROC_ROOT/net/tcp" ] || return 1
    printf '%s\000-listen\000%s\000' "$UI_BIN" "$UI_LISTEN" > "$TMP_DIR/ui-argv.full" || return 1
    printf 'freenet-ui\000-listen\000%s\000' "$UI_LISTEN" > "$TMP_DIR/ui-argv.short" || return 1
    UI_SERVICE_READY=1
}

ui_ipv4_hex() (
    printf '%s\n' "$1" | LC_ALL=C awk -F. -v arch="$ARCH" '
        NR!=1 || NF!=4 { bad=1; next }
        { for(i=1;i<=4;i++) if($i !~ /^[0-9]+$/ || $i<0 || $i>255) bad=1 }
        END {
            if(NR!=1 || bad) exit 1
            if(arch=="mips32") printf "%02X%02X%02X%02X\n",$1,$2,$3,$4
            else if(arch=="mips32le" || arch=="arm64-v8a") printf "%02X%02X%02X%02X\n",$4,$3,$2,$1
            else exit 1
        }
    '
)

ui_proc_start() (
    up_pid="$1"
    case "$up_pid" in ''|*[!0-9]*) return 1 ;; esac
    LC_ALL=C awk -v pid="$up_pid" '
        NR!=1 || $1!=pid { bad=1; next }
        { sub(/^[0-9]+ \(.*\) /,""); if(NF<20 || $1=="Z" || $20 !~ /^[0-9]+$/ || $20=="0") bad=1; birth=$20 }
        END { if(NR!=1 || bad) exit 1; print birth }
    ' "$UPDATE_PROC_ROOT/$up_pid/stat"
)

ui_process_fingerprint() (
    up_pid="$1"; up_expected="$2"
    up_birth="$(ui_proc_start "$up_pid")" || return 1
    up_dir="$UPDATE_PROC_ROOT/$up_pid"
    LC_ALL=C awk -v uid="$UPDATE_UID" '/^Uid:/ { if(NF!=5 || $2!=uid || $3!=uid || $4!=uid || $5!=uid) exit 1; found=1 } END { if(!found) exit 1 }' "$up_dir/status" || return 1
    # Some cmp -s implementations trust proc's reported zero file size.
    # Compare a checked regular snapshot and rehash live proc below.
    up_cmdline="$TMP_DIR/ui-fingerprint-$up_pid.cmdline"
    cat "$up_dir/cmdline" > "$up_cmdline" || return 1
    if ! cmp -s "$up_cmdline" "$TMP_DIR/ui-argv.full" && ! cmp -s "$up_cmdline" "$TMP_DIR/ui-argv.short"; then return 1; fi
    up_argv="$(update_sha "$up_cmdline")" || return 1
    up_exe="$(readlink "$up_dir/exe")" || return 1
    case "$up_exe" in "$UI_IMAGE_PATH"|"$UI_IMAGE_PATH (deleted)") ;; *) return 1 ;; esac
    up_stat="$(update_stat "$up_dir/exe")" || return 1
    up_sha="$(update_sha "$up_dir/exe")" || return 1
    case " $up_expected " in *" $up_sha "*) ;; *) return 1 ;; esac
    [ "$(ui_proc_start "$up_pid")" = "$up_birth" ] &&
        [ "$(update_stat "$up_dir/exe")" = "$up_stat" ] &&
        [ "$(update_sha "$up_dir/cmdline")" = "$up_argv" ] || return 1
    printf '%s:%s:%s:%s:%s\n' "$up_pid" "$up_birth" "$up_stat" "$up_sha" "$up_argv"
)

ui_stable_pin() {
    # Atomic rename legitimately changes the old executable's link count and
    # ctime. Its PID lifetime, device/inode, bytes and argv must remain pinned.
    printf '%s\n' "$1" | LC_ALL=C awk -F: 'NF!=14 {exit 1} {print $1 ":" $2 ":" $3 ":" $4 ":" $13 ":" $14}'
}

ui_find_daemon() (
    uf_expected="$1"; uf_found=""
    for uf_dir in "$UPDATE_PROC_ROOT"/[0-9]*; do
        [ -d "$uf_dir" ] || continue
        uf_exe="$(readlink "$uf_dir/exe" 2>/dev/null)" || continue
        case "$uf_exe" in "$UI_IMAGE_PATH"|"$UI_IMAGE_PATH (deleted)") ;; *) continue ;; esac
        uf_pid="${uf_dir##*/}"
        uf_cmdline="$TMP_DIR/ui-find-$uf_pid.cmdline"
        cat "$uf_dir/cmdline" > "$uf_cmdline" || { [ ! -e "$uf_dir" ] && continue; return 1; }
        if ! cmp -s "$uf_cmdline" "$TMP_DIR/ui-argv.full" && ! cmp -s "$uf_cmdline" "$TMP_DIR/ui-argv.short"; then
            # Same ELF is also the official cron/CLI worker. Its mode is not a
            # daemon identity, and it must neither block start nor receive kill.
            uf_mode="$(tr '\000' '\n' < "$uf_cmdline" | sed -n '2p')" || return 1
            case "$uf_mode" in
                automation-health-watch|automation-best-run|settings-v3-reconcile|settings-v3-endpoint-refresh|settings-v3-subscription|settings-v3-geodata|settings-v3-freenet-check|settings-v3-backup|plan|apply) continue ;;
                *) [ ! -e "$uf_dir" ] && continue; return 1 ;;
            esac
        fi
        uf_id="$(ui_process_fingerprint "$uf_pid" "$uf_expected")" || { [ ! -e "$uf_dir" ] && continue; return 1; }
        [ -z "$uf_found" ] || return 1
        uf_found="$uf_id"
    done
    printf '%s\n' "$uf_found"
)

ui_listener_table() (
    [ -r "$UPDATE_PROC_ROOT/net/tcp" ] || return 1
    ul_files="$UPDATE_PROC_ROOT/net/tcp"
    if [ -e "$UPDATE_PROC_ROOT/net/tcp6" ]; then
        [ -r "$UPDATE_PROC_ROOT/net/tcp6" ] || return 1
        # Paths are supplied as separate, quoted arguments below.
        LC_ALL=C awk -v port="$UI_PORT_HEX" '$4=="0A" && toupper(substr($2,length($2)-3))==port { if(NF<10 || $10 !~ /^[0-9]+$/) exit 1; print toupper($2),$10 }' "$UPDATE_PROC_ROOT/net/tcp" "$UPDATE_PROC_ROOT/net/tcp6"
    else
        LC_ALL=C awk -v port="$UI_PORT_HEX" '$4=="0A" && toupper(substr($2,length($2)-3))==port { if(NF<10 || $10 !~ /^[0-9]+$/) exit 1; print toupper($2),$10 }' "$UPDATE_PROC_ROOT/net/tcp"
    fi
)

ui_owned_listeners() (
    ul_pid="$1"
    ul_table="$(ui_listener_table)" || return 1
    printf '%s\n' "$ul_table" | LC_ALL=C awk -v lan="$UI_HOST_HEX:$UI_PORT_HEX" -v loop="$UI_LOOP_HEX:$UI_PORT_HEX" '
        NF!=2 || $2 !~ /^[0-9]+$/ || $2=="0" { bad=1; next }
        $1==lan { lanCount++; next }
        $1==loop { loopCount++; next }
        { bad=1 }
        END { if(bad || lanCount!=1 || (lan!=loop && loopCount!=1) || (lan==loop && loopCount!=0)) exit 1 }
    ' || return 1
    [ -d "$UPDATE_PROC_ROOT/$ul_pid/fd" ] && [ -r "$UPDATE_PROC_ROOT/$ul_pid/fd" ] || return 1
    for ul_inode in $(printf '%s\n' "$ul_table" | awk '{print $2}'); do
        ul_owned=no
        for ul_fd in "$UPDATE_PROC_ROOT/$ul_pid/fd"/*; do
            ul_link="$(readlink "$ul_fd" 2>/dev/null)" || continue
            [ "$ul_link" != "socket:[$ul_inode]" ] || ul_owned=yes
        done
        [ "$ul_owned" = yes ] || return 1
    done
    printf '%s\n' "$ul_table" | LC_ALL=C sort
)

ui_runtime_proof() (
    ur_target="$1"; ur_sha="$2"
    ur_id="$(ui_find_daemon "$ur_sha")" || return 1
    [ -n "$ur_id" ] || return 1
    ur_pid="${ur_id%%:*}"
    ur_sockets="$(ui_owned_listeners "$ur_pid")" || return 1
    curl -q -fsS --noproxy '*' --connect-timeout 1 --max-time 2 "http://127.0.0.1:$UI_PORT/healthz" > "$TMP_DIR/ui-health.body" 2> "$TMP_DIR/ui-health.err" || return 1
    curl -q -fsS --noproxy '*' --connect-timeout 1 --max-time 2 "http://127.0.0.1:$UI_PORT/versionz" > "$TMP_DIR/ui-version.body" 2> "$TMP_DIR/ui-version.err" || return 1
    [ "$(cat "$TMP_DIR/ui-health.body")" = ok ] && [ "$(cat "$TMP_DIR/ui-version.body")" = "$ur_target" ] || return 1
    [ "$(ui_process_fingerprint "$ur_pid" "$ur_sha")" = "$ur_id" ] &&
        [ "$(ui_owned_listeners "$ur_pid")" = "$ur_sockets" ] || return 1
    printf 'DAEMON=%s\nLISTEN=%s\nSOCKETS=%s\nVERSION=%s\n' "$ur_id" "$UI_LISTEN" "$(printf '%s\n' "$ur_sockets" | sha256sum | awk '{print $1}')" "$ur_target"
)

ui_wait_ready() {
    uw_target="$1"; uw_sha="$2"; uw_attempt=0
    # Ten attempts have two finite 2-second HTTP budgets and nine 1-second
    # pauses; proc identity checks do not create unbounded network waits.
    while [ "$uw_attempt" -lt 10 ]; do
        uw_attempt=$((uw_attempt + 1))
        if ui_runtime_proof "$uw_target" "$uw_sha" > "$TMP_DIR/ui-runtime.proof"; then return 0; fi
        [ "$uw_attempt" -ge 10 ] || sleep 1
    done
    if [ -n "$BACKUP_DIR" ] && [ ! -L "$BACKUP_DIR/service.log" ]; then
        for uw_err in "$TMP_DIR/ui-health.err" "$TMP_DIR/ui-version.err"; do
            [ ! -f "$uw_err" ] || cat "$uw_err" >> "$BACKUP_DIR/service.log"
        done
    fi
    update_stage_error runtime-acceptance 'HTTP daemon identity, health or exact version was not confirmed'
}

ui_stop_daemon() {
    us_expected="$1"
    us_id="$(ui_find_daemon "$us_expected")" || { update_stage_error daemon-identity 'HTTP daemon is ambiguous'; return 1; }
    if [ -z "$us_id" ]; then
        us_table="$(ui_listener_table)" || return 1
        [ -z "$us_table" ] || { update_stage_error daemon-listener 'port has an unowned listener'; return 1; }
        return 0
    fi
    us_pid="${us_id%%:*}"
    us_birth="${us_id#*:}"; us_birth="${us_birth%%:*}"
    us_pin="$(ui_stable_pin "$us_id")" || return 1
    [ "$us_pin" = "$UI_OLD_PIN" ] || [ "$us_pin" = "$UI_STARTED_PIN" ] ||
        { update_stage_error daemon-identity 'HTTP daemon lifetime differs from the pre-mutation or owned-child pin'; return 1; }
    if [ "$us_pid:$us_birth" != "$UI_STARTED_ID" ]; then
        ui_owned_listeners "$us_pid" > "$TMP_DIR/ui-stop.sockets" || { update_stage_error daemon-listener 'HTTP sockets are not owned by the selected daemon'; return 1; }
    fi
    [ "$(ui_process_fingerprint "$us_pid" "$us_expected")" = "$us_id" ] || return 1
    if ! kill -TERM "$us_pid" 2>> "$BACKUP_DIR/service.log"; then
        [ ! -e "$UPDATE_PROC_ROOT/$us_pid" ] || { update_stage_error daemon-stop 'TERM failed for the identified HTTP daemon'; return 1; }
    fi
    us_wait=0
    while [ -e "$UPDATE_PROC_ROOT/$us_pid" ] && [ "$us_wait" -lt 10 ]; do
        # A reused PID is never signalled. Only the original lifetime is waited.
        us_now="$(ui_proc_start "$us_pid" 2>/dev/null)" || break
        us_birth="${us_id#*:}"; us_birth="${us_birth%%:*}"
        [ "$us_now" = "$us_birth" ] || break
        us_wait=$((us_wait + 1))
        sleep 1
    done
    us_remaining="$(ui_find_daemon "$us_expected")" || return 1
    us_table="$(ui_listener_table)" || return 1
    [ -z "$us_remaining" ] && [ -z "$us_table" ] || { update_stage_error daemon-stop 'HTTP daemon or listener did not stop'; return 1; }
}

restore_one() {
    DEST="$1"
    KEY="$2"
    [ -f "$BACKUP_DIR/$KEY.exists" ] && [ ! -L "$BACKUP_DIR/$KEY.exists" ] && [ ! -L "$DEST" ] || return 1
    case "$(cat "$BACKUP_DIR/$KEY.exists")" in
        yes)
            RESTORE_PROOF="$(update_file_proof "$BACKUP_DIR/$KEY.before" 67108864)" || return 1
            mkdir -p "$(dirname "$DEST")" 2>> "$BACKUP_DIR/service.log" || return 1
            RESTORE_TMP="$(umask 077; mktemp "$DEST.rollback.XXXXXX")" || return 1
            RESTORE_MODE="$(printf '%s\n' "$RESTORE_PROOF" | awk -F: '{print $5}')"
            if ! cp -p "$BACKUP_DIR/$KEY.before" "$RESTORE_TMP" 2>> "$BACKUP_DIR/service.log" ||
               ! cmp -s "$BACKUP_DIR/$KEY.before" "$RESTORE_TMP" ||
               ! RESTORE_TMP_PROOF="$(update_file_proof "$RESTORE_TMP" 67108864 "$RESTORE_MODE")" ||
               [ "${RESTORE_TMP_PROOF##*:}" != "${RESTORE_PROOF##*:}" ] ||
               [ "$(update_file_proof "$BACKUP_DIR/$KEY.before" 67108864)" != "$RESTORE_PROOF" ]; then
                rm -f "$RESTORE_TMP"
                update_stage_error rollback-copy 'cannot stage the exact backup asset'
                return 1
            fi
            # Rename a closed file on the destination filesystem. Never write
            # into a possibly executing ELF inode (ETXTBSY / partial rollback).
            if ! mv -f "$RESTORE_TMP" "$DEST" 2>> "$BACKUP_DIR/service.log"; then
                rm -f "$RESTORE_TMP"
                update_stage_error rollback-rename 'cannot atomically restore the backup asset'
                return 1
            fi
            RESTORE_LIVE_PROOF="$(update_file_proof "$DEST" 67108864 "$RESTORE_MODE")" || return 1
            [ "${RESTORE_LIVE_PROOF##*:}" = "${RESTORE_PROOF##*:}" ] || return 1
            ;;
        no)
            [ ! -e "$BACKUP_DIR/$KEY.before" ] && [ ! -L "$BACKUP_DIR/$KEY.before" ] || return 1
            rm -f "$DEST" 2>> "$BACKUP_DIR/service.log" || return 1
            [ ! -e "$DEST" ] && [ ! -L "$DEST" ] || return 1
            ;;
        *) return 1 ;;
    esac
}

install_assets() {
    [ "$FAIL_STAGE" = replace ] && return 1
    I=0
    for NAME in $(asset_list); do
        I=$((I + 1))
        DEST="$(asset_dest "$NAME")" || return 1
        MODE_NOW="$(asset_mode "$NAME")"
        mkdir -p "$(dirname "$DEST")" || return 1
        cp "$TMP_DIR/$NAME" "$DEST.new.$$" || return 1
        chmod "$MODE_NOW" "$DEST.new.$$" 2>/dev/null || return 1
        mv -f "$DEST.new.$$" "$DEST" || return 1
    done
    return 0
}

restart_ui() {
    UI_TARGET="$1"
    [ "$FAIL_STAGE" = restart ] && return 1
    if [ "$TEST_MODE" = yes ]; then
        mkdir -p "$ROOT/var/run" || return 1
        printf '%s\n' "$UI_TARGET" > "$ROOT/var/run/freenet-test-version" || return 1
        return 0
    fi
    [ "$UI_SERVICE_READY" = 1 ] && [ "$(update_file_proof "$UI_INIT" 16384 755)" = "$UI_INIT_PROOF" ] ||
        { update_stage_error daemon-spec 'verified UI launch specification changed'; return 1; }
    UI_CURRENT_SHA="$(update_sha "$UI_BIN")" || return 1
    ui_stop_daemon "$UI_OLD_SHA $UI_CURRENT_SHA $UI_STARTED_SHA" || return 1
    UI_START_LOG="$BACKUP_DIR/ui-$UI_TARGET.log"
    [ ! -L "$UI_START_LOG" ] || return 1
    (umask 077; : >> "$UI_START_LOG") || return 1
    (
        # Only this child ignores HUP. It owns no updater EXIT/signal cleanup,
        # and its descriptors cannot keep the caller's HTTP/SSH pipe open.
        trap - 0 1 2 15
        trap '' 1
        PATH=/opt/sbin:/opt/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
        FREENET_DISABLE_SCHEDULER_RECONCILE=yes
        export PATH FREENET_DISABLE_SCHEDULER_RECONCILE
        exec "$UI_BIN" -listen "$UI_LISTEN"
    ) < /dev/null >> "$UI_START_LOG" 2>&1 &
    UI_STARTED_PID=$!
    UI_STARTED_ID=""
    UI_STARTED_SHA="$UI_CURRENT_SHA"
    UI_START_TRIES=0
    while [ "$UI_START_TRIES" -lt 5 ]; do
        UI_START_TRIES=$((UI_START_TRIES + 1))
        if UI_START_FINGERPRINT="$(ui_process_fingerprint "$UI_STARTED_PID" "$UI_CURRENT_SHA")"; then
            UI_START_BIRTH="${UI_START_FINGERPRINT#*:}"; UI_START_BIRTH="${UI_START_BIRTH%%:*}"
            UI_STARTED_ID="$UI_STARTED_PID:$UI_START_BIRTH"
            UI_STARTED_PIN="$(ui_stable_pin "$UI_START_FINGERPRINT")" || return 1
            return 0
        fi
        [ -e "$UPDATE_PROC_ROOT/$UI_STARTED_PID" ] || break
        [ "$UI_START_TRIES" -ge 5 ] || sleep 1
    done
    update_stage_error daemon-start 'new HTTP daemon identity was not established; private startup log retained'
}

accept_runtime() {
    TARGET="$1"
    [ "$FAIL_STAGE" = accept ] && return 1

    if [ "$TEST_MODE" = yes ]; then
        [ "$(cat "$ROOT/var/run/freenet-test-version" 2>/dev/null)" = "$TARGET" ] || return 1
    else
        UI_ACCEPT_SHA="$(update_sha "$UI_BIN")" || return 1
        ui_wait_ready "$TARGET" "$UI_ACCEPT_SHA" || return 1
    fi

    snapshot_preserved_state "$TMP_DIR/preserved.after" || return 1
    cmp -s "$BACKUP_DIR/preserved.before" "$TMP_DIR/preserved.after" || return 1
    snapshot_xray "$TMP_DIR/xray-hashes.after" || return 1
    cmp "$BACKUP_DIR/xray-hashes.before" "$TMP_DIR/xray-hashes.after" >/dev/null 2>&1 || return 1
    return 0
}

rollback_assets() {
    [ "$ROLLBACK_FAIL" = yes ] && return 1
    [ -n "$BACKUP_DIR" ] && [ -d "$BACKUP_DIR" ] || return 1
    I=0
    for NAME in $(asset_list); do
        I=$((I + 1))
        DEST="$(asset_dest "$NAME")" || return 1
        restore_one "$DEST" "asset-$I" || return 1
    done
    restore_one "$VERSION_FILE" version-marker || return 1
    restart_ui "$CURRENT_VERSION" || return 1
    if [ "$TEST_MODE" = no ]; then
        UI_ROLLBACK_SHA="$(update_sha "$UI_BIN")" || return 1
        ui_wait_ready "$CURRENT_VERSION" "$UI_ROLLBACK_SHA" || return 1
    fi
    snapshot_preserved_state "$TMP_DIR/preserved.rollback" || return 1
    cmp -s "$BACKUP_DIR/preserved.before" "$TMP_DIR/preserved.rollback" || return 1
    snapshot_xray "$TMP_DIR/xray-hashes.rollback" || return 1
    cmp "$BACKUP_DIR/xray-hashes.before" "$TMP_DIR/xray-hashes.rollback" >/dev/null 2>&1 || return 1
    return 0
}

plan_error() {
    MSG="$1"
    say 'SUCCESS=no'
    say 'READY=no'
    say "CURRENT_VERSION=$CURRENT_VERSION"
    say "ERROR=$MSG"
    say 'MUTATION=NONE'
    return 1
}

run_plan() {
    detect_current || { plan_error 'current FreeNet version is invalid'; return 1; }
    get_arch || { plan_error 'unsupported Entware architecture'; return 1; }
    for T in curl sha256sum sed awk grep mktemp jq; do
        command -v "$T" >/dev/null 2>&1 || { plan_error "required command missing: $T"; return 1; }
    done
    if [ -n "$TARGET_TAG" ]; then
        # Exact target is already selected by the authenticated release catalog.
        # Do not spend another network round-trip asking GitHub which release is latest.
        PLAN_TARGET="$TARGET_TAG"
        LATEST="$LATEST_OVERRIDE"
        if [ -n "$LATEST" ]; then
            valid_tag "$LATEST" || { plan_error 'latest release tag is invalid'; return 1; }
        fi
    else
        LATEST="$(latest_tag)" || { plan_error 'cannot determine latest FreeNet release'; return 1; }
        valid_tag "$LATEST" || { plan_error 'latest release tag is invalid'; return 1; }
        PLAN_TARGET="$LATEST"
    fi
    valid_tag "$PLAN_TARGET" || { plan_error 'target release tag is invalid'; return 1; }
    beta_opted_in "$PLAN_TARGET" || { plan_error 'explicit Beta opt-in required'; return 1; }
    fetch_release_metadata "$PLAN_TARGET" || { plan_error 'target release metadata or Beta channel is not verified'; return 1; }
    TARGET_TAG="$PLAN_TARGET"
    fetch_manifest "$PLAN_TARGET" || { plan_error 'release manifest is unavailable or incomplete'; return 1; }
    compatible_update_target "$PLAN_TARGET" || { plan_error 'target cannot preserve pending VPN recovery state; choose a recovery-protocol-2 release or the v0.7.18 recovery bridge'; return 1; }
    RELEASE_NOTES="$(release_notes "$PLAN_TARGET" 2>/dev/null || true)"

    AVAILABLE=yes
    DIRECTION=upgrade
    if [ "$PLAN_TARGET" = "$CURRENT_VERSION" ]; then
        AVAILABLE=no
        DIRECTION=same
    elif version_gt "$PLAN_TARGET" "$CURRENT_VERSION"; then
        DIRECTION=upgrade
    else
        DIRECTION=downgrade
    fi

    say 'SUCCESS=yes'
    say 'READY=yes'
    say "CURRENT_VERSION=$CURRENT_VERSION"
    say "LATEST_VERSION=$LATEST"
    say "TARGET_TAG=$PLAN_TARGET"
    say "UPDATE_AVAILABLE=$AVAILABLE"
    say "DIRECTION=$DIRECTION"
    say "ARCH=$ARCH"
    say 'MANIFEST_VERIFIED=yes'
    say 'COMPATIBILITY_VERIFIED=yes'
    say "RELEASE_NOTES=$RELEASE_NOTES"
    say 'COMPONENTS=FreeNet UI; manager; VPN/updater helpers; network/provider/finalize helpers; bootstrap helper; self-update helper; upstream pins'
    if [ "$AVAILABLE" = yes ]; then
        say "EXPECTED_DELTA=replace verified FreeNet-owned application assets with exact release $PLAN_TARGET; restart FreeNet UI; validate target version and unchanged Xray state"
    else
        say 'EXPECTED_DELTA=NONE; selected FreeNet version is already installed'
    fi
    say 'EXPECTED_NO_DELTA=subscription secret; Xray credentials/config; DNS/routing state; legacy config keys; XKeen/Xray/XKeen UI core; cron'
    say 'MUTATION=NONE'
}

fail_before_mutation() {
    MSG="$1"
    [ "$LOCK_HELD" = 1 ] && write_state FAILED "$TARGET_TAG" 'Обновление не началось: проверка не пройдена' "$MSG" NOT_NEEDED "$BACKUP_DIR" || true
    err "$MSG"
    exit 1
}

fail_after_mutation() {
    MSG="$1"
    [ -z "$LAST_UPDATE_STAGE" ] || MSG="$MSG (stage=$LAST_UPDATE_STAGE)"
    err "$MSG"
    if rollback_assets; then
        MUTATED=0
        [ "$LOCK_HELD" = 1 ] && write_state FAILED "$TARGET_TAG" 'Обновление отменено; предыдущее состояние восстановлено' "$MSG" SUCCESS "$BACKUP_DIR" || true
        exit 1
    fi
    KEEP_LOCK=1
    write_state ROLLBACK_FAILED "$TARGET_TAG" 'Откат не подтверждён; дальнейшие mutation запрещены' "$MSG" FAILED_UNKNOWN "$BACKUP_DIR" || true
    exit 2
}

previous_update_receipt_safe() {
    [ -e "$STATE_FILE" ] || [ -L "$STATE_FILE" ] || return 0
    [ -f "$STATE_FILE" ] && [ ! -L "$STATE_FILE" ] && [ -r "$STATE_FILE" ] || return 1
    [ "$(wc -c < "$STATE_FILE")" -le 131072 ] || return 1
    OLD_UPDATE_STATE="$(sed -n 's/^STATE=//p' "$STATE_FILE")"
    OLD_UPDATE_ROLLBACK="$(sed -n 's/^ROLLBACK_STATE=//p' "$STATE_FILE")"
    case "$OLD_UPDATE_STATE" in ''|IDLE|BUSY|CHECKING|SNAPSHOT|FAILED|SUCCESS) ;; *) return 1 ;; esac
    case "$OLD_UPDATE_ROLLBACK" in ''|NOT_NEEDED|SUCCESS) return 0 ;; *) return 1 ;; esac
}

# This is an application-only recovery bridge, never a VPN rollback.
# It runs ONLY from a staged, SHA-verified, exact Beta update with opt-in.
# A missing PID alone never authorizes removing or overwriting a VPN lock.
vpn_lock_dual_recovery_evidence_safe() {
    [ -d "$CHECKPOINT_DIR" ] && [ ! -L "$CHECKPOINT_DIR" ] || return 1
    [ -z "$(find "$CHECKPOINT_DIR" -type l -print 2>/dev/null)" ] || return 1
    for RECOVERY_LAYER in "$CHECKPOINT_DIR" "$CHECKPOINT_DIR/recovery.pending"; do
        [ -d "$RECOVERY_LAYER" ] && [ ! -L "$RECOVERY_LAYER" ] || return 1
        [ -f "$RECOVERY_LAYER/state" ] && [ ! -L "$RECOVERY_LAYER/state" ] || return 1
        [ "$(cat "$RECOVERY_LAYER/state" 2>/dev/null)" = PENDING ] || return 1
        for RECOVERY_KIND in out profile filter; do
            RECOVERY_SAVED="$RECOVERY_LAYER/$RECOVERY_KIND.before"
            [ -f "$RECOVERY_SAVED" ] && [ ! -L "$RECOVERY_SAVED" ] && [ -r "$RECOVERY_SAVED" ] || return 1
            [ "$(wc -c < "$RECOVERY_SAVED" 2>/dev/null)" -le 2097152 ] || return 1
            [ ! -e "$RECOVERY_LAYER/$RECOVERY_KIND.absent" ] &&
                [ ! -L "$RECOVERY_LAYER/$RECOVERY_KIND.absent" ] || return 1
        done
    done
    [ -f "$CHECKPOINT_DIR/recovery.pending/core.before" ] &&
        [ ! -L "$CHECKPOINT_DIR/recovery.pending/core.before" ] || return 1
    case "$(cat "$CHECKPOINT_DIR/recovery.pending/core.before" 2>/dev/null)" in
        running|stopped) ;;
        *) return 1 ;;
    esac
    for LIVE in "$ROOT/etc/xray/configs/04_outbounds.json" \
        "$ROOT/etc/freenet/vpn_profile_name" "$ROOT/etc/xray/blanc_profile_filter.regex"; do
        [ -f "$LIVE" ] && [ ! -L "$LIVE" ] && [ -r "$LIVE" ] || return 1
        [ "$(wc -c < "$LIVE" 2>/dev/null)" -le 2097152 ] || return 1
    done
    return 0
}

# Compare ctimes as decimal strings. Entware shell arithmetic may be 32-bit,
# and awk numbers cannot represent every inode/timestamp string exactly.
vpn_recovery_ctime_at_least() (
    LC_ALL=C awk -v newer="$1" -v older="$2" 'BEGIN {
        if (newer !~ /^[0-9]+$/ || older !~ /^[0-9]+$/) exit 1
        sub(/^0+/, "", newer); sub(/^0+/, "", older)
        if (length(newer) != length(older)) exit (length(newer) > length(older) ? 0 : 1)
        exit ("x" newer >= "x" older ? 0 : 1)
    }'
)

# A content proof includes the exact filesystem identity on both sides of the
# read. Never print file contents; callers retain this metadata in memory only.
vpn_recovery_file_proof() (
    RECOVERY_FILE="$1"
    [ -f "$RECOVERY_FILE" ] && [ ! -L "$RECOVERY_FILE" ] && [ -r "$RECOVERY_FILE" ] || return 1
    RECOVERY_FILE_SIGNATURE="$(vpn_recovery_stat_signature "$RECOVERY_FILE")" || return 1
    [ "$(wc -c < "$RECOVERY_FILE" 2>/dev/null)" -le "$2" ] || return 1
    if [ -n "${3:-}" ]; then
        vpn_recovery_ctime_at_least "${RECOVERY_FILE_SIGNATURE##*:}" "$3" || return 1
    fi
    RECOVERY_FILE_SHA="$(sha256sum < "$RECOVERY_FILE")" || return 1
    RECOVERY_FILE_SHA="${RECOVERY_FILE_SHA%% *}"
    [ "${#RECOVERY_FILE_SHA}" -eq 64 ] || return 1
    case "$RECOVERY_FILE_SHA" in *[!0-9a-fA-F]*) return 1 ;; esac
    [ -f "$RECOVERY_FILE" ] && [ ! -L "$RECOVERY_FILE" ] && [ -r "$RECOVERY_FILE" ] || return 1
    RECOVERY_FILE_AFTER="$(vpn_recovery_stat_signature "$RECOVERY_FILE")" || return 1
    [ "$RECOVERY_FILE_AFTER" = "$RECOVERY_FILE_SIGNATURE" ] || return 1
    printf '%s:%s\n' "$RECOVERY_FILE_SIGNATURE" "$RECOVERY_FILE_SHA"
)

# Transaction helpers write these markers with printf '%s\n'. Match the hash
# already sampled by file_proof, rather than reading a different value later.
vpn_recovery_proof_matches_marker() (
    RECOVERY_MARKER_SHA="$(printf '%s\n' "$2" | sha256sum)" || return 1
    [ "${1##*:}" = "${RECOVERY_MARKER_SHA%% *}" ]
)

# Inspect every immediate entry, including hidden entries. Overflow is STOP,
# never a truncated search that could overlook a newer failed/foreign attempt.
vpn_recovery_archive_names() (
    RECOVERY_ARCHIVE_COUNT=0
    for RECOVERY_ARCHIVE_ENTRY in "$1"/* "$1"/.[!.]* "$1"/..?*; do
        if [ ! -e "$RECOVERY_ARCHIVE_ENTRY" ] && [ ! -L "$RECOVERY_ARCHIVE_ENTRY" ]; then
            case "$RECOVERY_ARCHIVE_ENTRY" in "$1/*"|"$1/.[!.]*"|"$1/..?*") continue ;; *) return 1 ;; esac
        fi
        RECOVERY_ARCHIVE_COUNT=$((RECOVERY_ARCHIVE_COUNT + 1))
        [ "$RECOVERY_ARCHIVE_COUNT" -le 64 ] || return 1
        [ -d "$RECOVERY_ARCHIVE_ENTRY" ] && [ ! -L "$RECOVERY_ARCHIVE_ENTRY" ] || return 1
        RECOVERY_ARCHIVE_NAME="${RECOVERY_ARCHIVE_ENTRY##*/}"
        # Reject separators/control characters before creating a line manifest.
        case "$RECOVERY_ARCHIVE_NAME" in ''|*[!0-9TZ.]*) return 1 ;; esac
        printf '%s\n' "$RECOVERY_ARCHIVE_NAME"
    done
    [ "$RECOVERY_ARCHIVE_COUNT" -gt 0 ]
)

vpn_recovery_archive_files_exact() (
    RECOVERY_ARCHIVE_COUNT=0
    for RECOVERY_ARCHIVE_ENTRY in "$1"/* "$1"/.[!.]* "$1"/..?*; do
        if [ ! -e "$RECOVERY_ARCHIVE_ENTRY" ] && [ ! -L "$RECOVERY_ARCHIVE_ENTRY" ]; then
            case "$RECOVERY_ARCHIVE_ENTRY" in "$1/*"|"$1/.[!.]*"|"$1/..?*") continue ;; *) return 1 ;; esac
        fi
        RECOVERY_ARCHIVE_COUNT=$((RECOVERY_ARCHIVE_COUNT + 1))
        [ "$RECOVERY_ARCHIVE_COUNT" -le 6 ] || return 1
        case "${RECOVERY_ARCHIVE_ENTRY##*/}" in state|result|core.before|out.before|profile.before|filter.before) ;; *) return 1 ;; esac
        [ -f "$RECOVERY_ARCHIVE_ENTRY" ] && [ ! -L "$RECOVERY_ARCHIVE_ENTRY" ] && [ -r "$RECOVERY_ARCHIVE_ENTRY" ] || return 1
    done
    [ "$RECOVERY_ARCHIVE_COUNT" -eq 6 ]
)

# This branch is available only after the nested checkpoint was fully archived.
# A ROLLED_BACK label alone is insufficient: bind the newest global attempt to
# the orphan owner, require its before-files to equal live VPN bytes, and pin
# the original checkpoint, archive inventory, selected archive and live files.
vpn_lock_archived_recovery_proof() (
    LC_ALL=C
    export LC_ALL
    [ -d "$CHECKPOINT_DIR" ] && [ ! -L "$CHECKPOINT_DIR" ] || return 1
    [ ! -e "$CHECKPOINT_DIR/recovery.pending" ] && [ ! -L "$CHECKPOINT_DIR/recovery.pending" ] || return 1
    ARCHIVED_LINKS="$(find "$CHECKPOINT_DIR" -type l -print 2>/dev/null)" || return 1
    [ -z "$ARCHIVED_LINKS" ] || return 1
    ARCHIVED_ORIGINAL_SIGNATURE="$(vpn_lock_stat_signature "$CHECKPOINT_DIR")" || return 1
    ARCHIVED_ROOT="$(dirname "$CHECKPOINT_DIR")/vpn-transaction.attempts"
    [ -d "$ARCHIVED_ROOT" ] && [ ! -L "$ARCHIVED_ROOT" ] && [ -r "$ARCHIVED_ROOT" ] && [ -x "$ARCHIVED_ROOT" ] || return 1
    ARCHIVED_ROOT_SIGNATURE="$(vpn_lock_stat_signature "$ARCHIVED_ROOT")" || return 1
    ARCHIVED_NAMES="$(vpn_recovery_archive_names "$ARCHIVED_ROOT")" || return 1
    ARCHIVED_NAMES="$(printf '%s\n' "$ARCHIVED_NAMES" | sort)" || return 1
    ARCHIVED_NAME="$(printf '%s\n' "$ARCHIVED_NAMES" | awk '
        {
            if (split($0, part, /\./) != 2 || length(part[2]) > 32 || part[2] !~ /^[1-9][0-9]*$/) { invalid=1; next }
            stamp=part[1]
            if (length(stamp) != 16 || substr(stamp,9,1) != "T" || substr(stamp,16,1) != "Z" ||
                substr(stamp,1,8) !~ /^[0-9]+$/ || substr(stamp,10,6) !~ /^[0-9]+$/) { invalid=1; next }
            year=substr(stamp,1,4)+0; month=substr(stamp,5,2)+0; day=substr(stamp,7,2)+0
            hour=substr(stamp,10,2)+0; minute=substr(stamp,12,2)+0; second=substr(stamp,14,2)+0
            if (year < 1 || month < 1 || month > 12 || day < 1 || hour > 23 || minute > 59 || second > 59) { invalid=1; next }
            days=31
            if (month==4 || month==6 || month==9 || month==11) days=30
            if (month==2) days=(year%4==0 && (year%100!=0 || year%400==0) ? 29 : 28)
            if (day > days) { invalid=1; next }
            if (latest=="" || "x" stamp > "x" latest) { latest=stamp; selected=$0; ties=1 }
            else if (stamp==latest) ties++
        }
        END { if (NR==0 || invalid || ties!=1) exit 1; print selected }
    ')" || return 1
    [ "${ARCHIVED_NAME##*.}" = "$ORPHAN_PID" ] || return 1
    ARCHIVED_DIR="$ARCHIVED_ROOT/$ARCHIVED_NAME"
    [ -d "$ARCHIVED_DIR" ] && [ ! -L "$ARCHIVED_DIR" ] && [ -r "$ARCHIVED_DIR" ] && [ -x "$ARCHIVED_DIR" ] || return 1
    ARCHIVED_DIR_SIGNATURE="$(vpn_lock_stat_signature "$ARCHIVED_DIR")" || return 1
    ARCHIVED_LOCK_CTIME="${ORPHAN_SIGNATURE##*:}"
    vpn_recovery_ctime_at_least "${ARCHIVED_DIR_SIGNATURE##*:}" "$ARCHIVED_LOCK_CTIME" || return 1
    vpn_recovery_archive_files_exact "$ARCHIVED_DIR" || return 1
    printf 'ORIGINAL_DIR %s\nARCHIVE_ROOT %s\nARCHIVE_NAMES\n%s\nARCHIVE_DIR %s %s\n' \
        "$ARCHIVED_ORIGINAL_SIGNATURE" "$ARCHIVED_ROOT_SIGNATURE" "$ARCHIVED_NAMES" "$ARCHIVED_NAME" "$ARCHIVED_DIR_SIGNATURE"
    ARCHIVED_PROOF="$(vpn_recovery_file_proof "$CHECKPOINT_DIR/state" 128)" || return 1
    vpn_recovery_proof_matches_marker "$ARCHIVED_PROOF" PENDING || return 1
    printf 'ORIGINAL_STATE %s\n' "$ARCHIVED_PROOF"
    for ARCHIVED_META in state result core.before; do
        ARCHIVED_PROOF="$(vpn_recovery_file_proof "$ARCHIVED_DIR/$ARCHIVED_META" 128 "$ARCHIVED_LOCK_CTIME")" || return 1
        case "$ARCHIVED_META" in
            state) vpn_recovery_proof_matches_marker "$ARCHIVED_PROOF" PENDING ;;
            result) vpn_recovery_proof_matches_marker "$ARCHIVED_PROOF" ROLLED_BACK ;;
            core.before) vpn_recovery_proof_matches_marker "$ARCHIVED_PROOF" running || vpn_recovery_proof_matches_marker "$ARCHIVED_PROOF" stopped ;;
        esac || return 1
        printf 'ARCHIVE_%s %s\n' "$ARCHIVED_META" "$ARCHIVED_PROOF"
    done
    for ARCHIVED_KIND in out profile filter; do
        case "$ARCHIVED_KIND" in
            out) ARCHIVED_LIVE="$ROOT/etc/xray/configs/04_outbounds.json" ;;
            profile) ARCHIVED_LIVE="$ROOT/etc/freenet/vpn_profile_name" ;;
            filter) ARCHIVED_LIVE="$ROOT/etc/xray/blanc_profile_filter.regex" ;;
        esac
        [ ! -e "$CHECKPOINT_DIR/$ARCHIVED_KIND.absent" ] && [ ! -L "$CHECKPOINT_DIR/$ARCHIVED_KIND.absent" ] || return 1
        ARCHIVED_ORIGINAL="$(vpn_recovery_file_proof "$CHECKPOINT_DIR/$ARCHIVED_KIND.before" 2097152)" || return 1
        ARCHIVED_BEFORE="$(vpn_recovery_file_proof "$ARCHIVED_DIR/$ARCHIVED_KIND.before" 2097152 "$ARCHIVED_LOCK_CTIME")" || return 1
        ARCHIVED_CURRENT="$(vpn_recovery_file_proof "$ARCHIVED_LIVE" 2097152)" || return 1
        [ "${ARCHIVED_BEFORE##*:}" = "${ARCHIVED_CURRENT##*:}" ] || return 1
        cmp -s "$ARCHIVED_DIR/$ARCHIVED_KIND.before" "$ARCHIVED_LIVE" || return 1
        printf 'ORIGINAL_%s %s\nARCHIVE_%s %s\nLIVE_%s %s\n' \
            "$ARCHIVED_KIND" "$ARCHIVED_ORIGINAL" "$ARCHIVED_KIND" "$ARCHIVED_BEFORE" "$ARCHIVED_KIND" "$ARCHIVED_CURRENT"
    done
    # Re-list as well as checking ctime: filesystem timestamps can have only
    # one-second resolution, including during a directory-entry replacement.
    ARCHIVED_NAMES_AFTER="$(vpn_recovery_archive_names "$ARCHIVED_ROOT")" || return 1
    ARCHIVED_NAMES_AFTER="$(printf '%s\n' "$ARCHIVED_NAMES_AFTER" | sort)" || return 1
    [ "$ARCHIVED_NAMES_AFTER" = "$ARCHIVED_NAMES" ] || return 1
    vpn_recovery_archive_files_exact "$ARCHIVED_DIR" || return 1
    [ "$(vpn_lock_stat_signature "$CHECKPOINT_DIR")" = "$ARCHIVED_ORIGINAL_SIGNATURE" ] || return 1
    [ "$(vpn_lock_stat_signature "$ARCHIVED_ROOT")" = "$ARCHIVED_ROOT_SIGNATURE" ] || return 1
    [ "$(vpn_lock_stat_signature "$ARCHIVED_DIR")" = "$ARCHIVED_DIR_SIGNATURE" ] || return 1
    [ ! -e "$CHECKPOINT_DIR/recovery.pending" ] && [ ! -L "$CHECKPOINT_DIR/recovery.pending" ]
)

vpn_lock_recovery_evidence_safe() {
    if [ -e "$CHECKPOINT_DIR/recovery.pending" ] || [ -L "$CHECKPOINT_DIR/recovery.pending" ]; then
        RECOVERY_EVIDENCE_MODE=dual
    else
        RECOVERY_EVIDENCE_MODE=archived
    fi
    [ -z "$ORPHAN_EVIDENCE_MODE" ] || [ "$ORPHAN_EVIDENCE_MODE" = "$RECOVERY_EVIDENCE_MODE" ] || return 1
    if [ "$RECOVERY_EVIDENCE_MODE" = dual ]; then
        vpn_lock_dual_recovery_evidence_safe || return 1
    else
        RECOVERY_ARCHIVE_PROOF="$(vpn_lock_archived_recovery_proof)" || return 1
        if [ -n "$ORPHAN_ARCHIVE_PROOF" ]; then
            [ "$RECOVERY_ARCHIVE_PROOF" = "$ORPHAN_ARCHIVE_PROOF" ] || return 1
        else
            ORPHAN_ARCHIVE_PROOF="$RECOVERY_ARCHIVE_PROOF"
        fi
    fi
    ORPHAN_EVIDENCE_MODE="$RECOVERY_EVIDENCE_MODE"
}

# Fail closed if another known FreeNet writer is running, even if the former
# lock owner's PID has vanished. Never print process argv or credentials.
vpn_lock_recovery_writers_absent() {
    if [ "$TEST_MODE" = yes ]; then
        PROC_ROOT="${FREENET_UPDATE_PROC_ROOT:-/proc}"
    else
        PROC_ROOT=/proc
    fi
    [ -d "$PROC_ROOT" ] && [ ! -L "$PROC_ROOT" ] && [ -r "$PROC_ROOT" ] || return 1
    [ ! -e "$PROC_ROOT/$ORPHAN_PID" ] && [ ! -L "$PROC_ROOT/$ORPHAN_PID" ] || return 1
    for PROC_PATH in "$PROC_ROOT"/[0-9]*; do
        [ -d "$PROC_PATH" ] || continue
        case "$PROC_PATH" in "$PROC_ROOT/$$") continue ;; esac
        if [ ! -r "$PROC_PATH/cmdline" ]; then
            [ ! -e "$PROC_PATH" ] || return 1
            continue
        fi
        if tr '\000' ' ' < "$PROC_PATH/cmdline" 2>/dev/null |
            grep -Eq '(^|/)(apply_provider_profile|apply_network_profile|blanc_xkeen_update_outbounds|self_update)\.sh[[:space:]]+(apply|apply-core|recover-core|core-restart)([[:space:]]|$)'; then
            return 1
        fi
    done
    [ ! -e "$PROC_ROOT/$ORPHAN_PID" ] && [ ! -L "$PROC_ROOT/$ORPHAN_PID" ]
}

# Entware BusyBox may omit FEATURE_STAT_FORMAT (-c) while retaining -t.
# Pin one format for the entire claim. Both yield the same dev/inode/mtime/
# ctime identity; keep the hex device and decimal inode as strings (no 32-bit
# shell arithmetic or floating-point awk conversion). Exit 2 means the stat
# command failed; malformed successful output must not select another format.
vpn_recovery_stat_signature() (
    LOCK_STAT_PATH="$1"
    { [ -d "$LOCK_STAT_PATH" ] || [ -f "$LOCK_STAT_PATH" ]; } && [ ! -L "$LOCK_STAT_PATH" ] || return 1
    if printf '%s' "$LOCK_STAT_PATH" | LC_ALL=C grep -q '[[:cntrl:]]'; then
        return 1
    fi
    case "$ORPHAN_STAT_MODE" in
        format)
            LOCK_STAT_VALUE="$(LC_ALL=C stat -c '%D:%i:%Y:%Z' "$LOCK_STAT_PATH" 2>/dev/null)" || return 2
            [ "${#LOCK_STAT_VALUE}" -le 256 ] || return 1
            printf '%s\n' "$LOCK_STAT_VALUE" | LC_ALL=C awk -F: '
                NR != 1 || NF != 4 { invalid=1; next }
                {
                    if ($1 !~ /^[0-9a-fA-F]+$/ || $2 !~ /^[0-9]+$/ ||
                        $3 !~ /^-?[0-9]+$/ || $4 !~ /^-?[0-9]+$/) invalid=1
                    signature=tolower($1) ":" $2 ":" $3 ":" $4
                }
                END { if (NR != 1 || invalid) exit 1; print signature }
            '
            ;;
        terse)
            LOCK_STAT_LINE="$(LC_ALL=C stat -t "$LOCK_STAT_PATH" 2>/dev/null)" || return 2
            # BusyBox prints the literal filename, then exactly 14 fields.
            # Remove that exact prefix before splitting, including spaces or
            # glob characters in the path. GNU's extra birth-time field and
            # any unexpected/multiline schema remain a safe STOP.
            case "$LOCK_STAT_LINE" in
                "$LOCK_STAT_PATH "*) LOCK_STAT_VALUE="${LOCK_STAT_LINE#"$LOCK_STAT_PATH "}" ;;
                *) return 1 ;;
            esac
            [ "${#LOCK_STAT_VALUE}" -le 512 ] || return 1
            printf '%s\n' "$LOCK_STAT_VALUE" | LC_ALL=C awk '
                NR != 1 || NF != 14 { invalid=1; next }
                {
                    for (i=1; i<=14; i++) {
                        if (i==3 || i==6 || i==9 || i==10) {
                            if ($i !~ /^[0-9a-fA-F]+$/) invalid=1
                        } else if ($i !~ /^[0-9]+$/) invalid=1
                    }
                    signature=tolower($6) ":" $7 ":" $12 ":" $13
                }
                END { if (NR != 1 || invalid) exit 1; print signature }
            '
            ;;
        *) return 1 ;;
    esac
)

vpn_lock_stat_signature() {
    # The existing owner/quarantine reader remains directory-only.
    [ -d "$1" ] && [ ! -L "$1" ] || return 1
    vpn_recovery_stat_signature "$1"
}

vpn_lock_recovery_same_owner() {
    [ -d "$MUTATION_LOCK_DIR" ] && [ ! -L "$MUTATION_LOCK_DIR" ] || return 1
    [ ! -e "$MUTATION_LOCK_DIR/freenet-update-owner" ] &&
        [ ! -L "$MUTATION_LOCK_DIR/freenet-update-owner" ] || return 1
    [ -f "$MUTATION_LOCK_DIR/pid" ] && [ ! -L "$MUTATION_LOCK_DIR/pid" ] || return 1
    [ "$(cat "$MUTATION_LOCK_DIR/pid" 2>/dev/null)" = "$ORPHAN_PID" ] || return 1
    SAME_OWNER_SIGNATURE="$(vpn_lock_stat_signature "$MUTATION_LOCK_DIR")" || return 1
    [ "$SAME_OWNER_SIGNATURE" = "$ORPHAN_SIGNATURE" ] || return 1
    for LOCK_ENTRY in "$MUTATION_LOCK_DIR"/* "$MUTATION_LOCK_DIR"/.[!.]* "$MUTATION_LOCK_DIR"/..?*; do
        [ -e "$LOCK_ENTRY" ] || [ -L "$LOCK_ENTRY" ] || continue
        [ "$LOCK_ENTRY" = "$MUTATION_LOCK_DIR/pid" ] || return 1
    done
}

# Exclusive guard + bounded twice-verified orphan owner, followed by same-FS
# directory rename to QUARANTINE. A concurrent writer must still win mkdir
# of the fresh lock to proceed; we fail without touching its files.
# No original checkpoint is ever erased, modified or considered recovered.
claim_verified_orphan_vpn_lock() {
    [ "$NETWORK_FENCE_DEFERRED" = 1 ] && [ "$LOCK_HELD" = 1 ] || return 1
    [ "${FREENET_UPDATE_BETA_OPT_IN:-no}" = yes ] && beta_tag "$TARGET_TAG" || return 1
    [ -d "$MUTATION_LOCK_DIR" ] && [ ! -L "$MUTATION_LOCK_DIR" ] || return 1
    [ -f "$MUTATION_LOCK_DIR/pid" ] && [ ! -L "$MUTATION_LOCK_DIR/pid" ] || return 1
    [ "$(wc -c < "$MUTATION_LOCK_DIR/pid" 2>/dev/null)" -le 32 ] || return 1
    ORPHAN_PID="$(cat "$MUTATION_LOCK_DIR/pid" 2>/dev/null)" || return 1
    case "$ORPHAN_PID" in ''|0*|1|*[!0-9]*) return 1 ;; esac
    ORPHAN_STAT_MODE=format
    ORPHAN_SIGNATURE="$(vpn_lock_stat_signature "$MUTATION_LOCK_DIR")"
    case "$?" in
        0) ;;
        2)
            ORPHAN_STAT_MODE=terse
            ORPHAN_SIGNATURE="$(vpn_lock_stat_signature "$MUTATION_LOCK_DIR")" || return 1
            ;;
        *) return 1 ;;
    esac
    ORPHAN_ID="${ORPHAN_SIGNATURE%:*:*}"
    vpn_lock_recovery_evidence_safe || return 1
    vpn_lock_recovery_same_owner || return 1
    mkdir "$RECOVERY_GUARD_DIR" 2>/dev/null || return 1
    RECOVERY_GUARD_HELD=1
    vpn_lock_recovery_writers_absent || return 1
    sleep 1
    vpn_lock_recovery_evidence_safe || return 1
    vpn_lock_recovery_same_owner || return 1
    vpn_lock_recovery_writers_absent || return 1

    # Persistent evidence is metadata only, not secret-bearing checkpoint
    # material. Original before-files remain untouched in their own directory.
    EVIDENCE_ROOT="$ROOT/var/lib/freenet/vpn-lock-recovery-history"
    [ ! -L "$EVIDENCE_ROOT" ] || return 1
    (umask 077; mkdir -p "$EVIDENCE_ROOT") || return 1
    [ -d "$EVIDENCE_ROOT" ] && [ ! -L "$EVIDENCE_ROOT" ] || return 1
    EVIDENCE_FILE="$EVIDENCE_ROOT/$(date -u '+%Y%m%dT%H%M%SZ').$$.state"
    [ ! -e "$EVIDENCE_FILE" ] && [ ! -L "$EVIDENCE_FILE" ] || return 1
    (umask 077; printf 'STATE=OBSERVED\nOWNER_PID=%s\nTARGET=%s\n' "$ORPHAN_PID" "$TARGET_TAG" > "$EVIDENCE_FILE") || return 1

    ORPHAN_LOCK_ARCHIVE="$MUTATION_LOCK_DIR.orphaned.$(date -u '+%Y%m%dT%H%M%SZ').$$"
    [ ! -e "$ORPHAN_LOCK_ARCHIVE" ] && [ ! -L "$ORPHAN_LOCK_ARCHIVE" ] || return 1
    if [ "$ORPHAN_EVIDENCE_MODE" = archived ]; then
        vpn_lock_recovery_evidence_safe || return 1
    fi
    vpn_lock_recovery_same_owner && vpn_lock_recovery_writers_absent || return 1
    mv "$MUTATION_LOCK_DIR" "$ORPHAN_LOCK_ARCHIVE" 2>/dev/null || return 1
    ARCHIVE_SIGNATURE="$(vpn_lock_stat_signature "$ORPHAN_LOCK_ARCHIVE")" || return 1
    [ "${ARCHIVE_SIGNATURE%:*:*}" = "$ORPHAN_ID" ] || return 1
    (umask 077; printf 'STATE=QUARANTINED\nOWNER_PID=%s\nTARGET=%s\n' "$ORPHAN_PID" "$TARGET_TAG" > "$EVIDENCE_FILE") || return 1

    mkdir "$MUTATION_LOCK_DIR" 2>/dev/null || return 1
    MUTATION_LOCK_HELD=1
    printf '%s\n' "$$" > "$MUTATION_LOCK_DIR/pid" || return 1
    printf '%s\n' "$LOCK_DIR" > "$MUTATION_LOCK_DIR/freenet-update-owner" || return 1
    (umask 077; printf 'STATE=CLAIMED\nOWNER_PID=%s\nTARGET=%s\n' "$ORPHAN_PID" "$TARGET_TAG" > "$EVIDENCE_FILE") || return 1
    say 'VPN_LOCK_RECOVERY=ORPHAN_QUARANTINED; CHECKPOINT=PRESERVED; VPN_MUTATION=NONE'
    return 0
}

# Reconciliation confirms an application rollback that is already complete.
# It never restores assets, restarts services, changes VPN evidence, or releases
# either existing fence. Only the original UNKNOWN receipt can be terminalized.
reconcile_root_path_safe() (
    rp_path="$1"
    case "$ROOT" in /*) ;; *) return 1 ;; esac
    case "$rp_path" in "$ROOT"/*) ;; *) return 1 ;; esac
    case "$rp_path" in *//*|*/../*|*/./*|*/..|*/.) return 1 ;; esac
    printf '%s' "$rp_path" | LC_ALL=C grep -q '[[:cntrl:]]' && return 1
    while [ "$rp_path" != "$ROOT" ]; do
        [ ! -L "$rp_path" ] || return 1
        if [ -d "$rp_path" ]; then
            update_directory_proof "$rp_path" > /dev/null || return 1
        fi
        rp_path="$(dirname "$rp_path")" || return 1
        case "$rp_path" in "$ROOT"|"$ROOT"/*) ;; *) return 1 ;; esac
    done
)

reconcile_marker() (
    rm_proof="$(update_file_proof "$1" 4096 600)" || return 1
    rm_sha="$(printf '%s\n' "$2" | sha256sum)" || return 1
    [ "${rm_proof##*:}" = "${rm_sha%% *}" ] || return 1
    printf '%s\n' "$rm_proof"
)

reconcile_state_values() {
    reconcile_root_path_safe "$STATE_FILE" || return 1
    RQ_STATE_PROOF="$(update_file_proof "$STATE_FILE" 131072 600)" || return 1
    RQ_SHA="${RQ_STATE_PROOF##*:}"
    LC_ALL=C awk -F= '
        $1 !~ /^(STATE|FROM_VERSION|TARGET_VERSION|MESSAGE|PRIMARY_ERROR|ROLLBACK_STATE|BACKUP_DIR|UPDATED_AT)$/ || seen[$1]++ { bad=1 }
        END {
            if(bad || !seen["STATE"] || !seen["FROM_VERSION"] || !seen["TARGET_VERSION"] ||
               !seen["ROLLBACK_STATE"] || !seen["BACKUP_DIR"] || !seen["PRIMARY_ERROR"]) exit 1
        }
    ' "$STATE_FILE" || return 1
    [ "$(sed -n 's/^STATE=//p' "$STATE_FILE")" = ROLLBACK_FAILED ] &&
        [ "$(sed -n 's/^ROLLBACK_STATE=//p' "$STATE_FILE")" = FAILED_UNKNOWN ] || return 1
    RQ_FROM="$(sed -n 's/^FROM_VERSION=//p' "$STATE_FILE")"
    RQ_TARGET="$(sed -n 's/^TARGET_VERSION=//p' "$STATE_FILE")"
    valid_tag "$RQ_FROM" && valid_tag "$RQ_TARGET" && [ "$RQ_FROM" != "$RQ_TARGET" ] || return 1
    RQ_BACKUP="$(sed -n 's/^BACKUP_DIR=//p' "$STATE_FILE")"
    reconcile_root_path_safe "$RQ_BACKUP" || return 1
    [ "$(dirname "$RQ_BACKUP")" = "$ROOT/backups" ] || return 1
    RQ_BACKUP_NAME="${RQ_BACKUP##*/}"
    printf '%s\n' "$RQ_BACKUP_NAME" | LC_ALL=C grep -Eq '^freenet-web-update-[0-9]{8}-[0-9]{6}\.[1-9][0-9]*$' || return 1
    RQ_OWNER="${RQ_BACKUP_NAME##*.}"
    [ "$(update_file_proof "$STATE_FILE" 131072 600)" = "$RQ_STATE_PROOF" ] || return 1
    CURRENT_VERSION="$RQ_FROM"
    TARGET_TAG="$RQ_TARGET"
    BACKUP_DIR="$RQ_BACKUP"
    # The two-asset historical recovery bridge does not have this full-asset
    # proof contract. It remains STOP, rather than inventing missing evidence.
    recovery_bridge_target && return 1
    return 0
}

reconcile_lock_proof() (
    rl_app="$(update_directory_proof "$LOCK_DIR" 700)" || return 1
    rl_net="$(update_directory_proof "$MUTATION_LOCK_DIR" 700)" || return 1
    # Default /tmp fences are immediate children of the trusted sticky /tmp.
    # Custom test/deployment paths must remain inside the selected ROOT.
    for rl_dir in "$LOCK_DIR" "$MUTATION_LOCK_DIR"; do
        case "$rl_dir" in
            "$ROOT"/*) reconcile_root_path_safe "$rl_dir" || return 1 ;;
            /tmp/*) [ "$(dirname "$rl_dir")" = /tmp ] || return 1 ;;
            *) return 1 ;;
        esac
    done
    rl_count=0
    for rl_entry in "$LOCK_DIR"/* "$LOCK_DIR"/.[!.]* "$LOCK_DIR"/..?*; do
        if [ ! -e "$rl_entry" ] && [ ! -L "$rl_entry" ]; then continue; fi
        case "${rl_entry##*/}" in owner.pid|target) ;; *) return 1 ;; esac
        rl_count=$((rl_count + 1))
    done
    [ "$rl_count" -eq 2 ] || return 1
    rl_count=0
    for rl_entry in "$MUTATION_LOCK_DIR"/* "$MUTATION_LOCK_DIR"/.[!.]* "$MUTATION_LOCK_DIR"/..?*; do
        if [ ! -e "$rl_entry" ] && [ ! -L "$rl_entry" ]; then continue; fi
        case "${rl_entry##*/}" in pid|freenet-update-owner) ;; *) return 1 ;; esac
        rl_count=$((rl_count + 1))
    done
    [ "$rl_count" -eq 2 ] || return 1
    rl_owner="$(reconcile_marker "$LOCK_DIR/owner.pid" "$RQ_OWNER")" || return 1
    rl_target="$(reconcile_marker "$LOCK_DIR/target" "$RQ_TARGET")" || return 1
    rl_pid="$(reconcile_marker "$MUTATION_LOCK_DIR/pid" "$RQ_OWNER")" || return 1
    rl_link="$(reconcile_marker "$MUTATION_LOCK_DIR/freenet-update-owner" "$LOCK_DIR")" || return 1
    [ "$(update_directory_proof "$LOCK_DIR" 700)" = "$rl_app" ] &&
        [ "$(update_directory_proof "$MUTATION_LOCK_DIR" 700)" = "$rl_net" ] || return 1
    printf 'APP_LOCK=%s\nNETWORK_LOCK=%s\nOWNER=%s\nTARGET=%s\nNETWORK_PID=%s\nNETWORK_OWNER=%s\n' "$rl_app" "$rl_net" "$rl_owner" "$rl_target" "$rl_pid" "$rl_link"
)

reconcile_own_descendant() (
    rd_pid="$1"; rd_steps=0
    while [ "$rd_steps" -lt 32 ]; do
        [ "$rd_pid" != "$$" ] || return 0
        case "$rd_pid" in ''|0|1|*[!0-9]*) return 1 ;; esac
        rd_pid="$(LC_ALL=C awk '{sub(/^[0-9]+ \(.*\) /,""); if(NR!=1 || NF<2 || $2 !~ /^[0-9]+$/) exit 1; print $2}' "$UPDATE_PROC_ROOT/$rd_pid/stat")" || return 1
        rd_steps=$((rd_steps + 1))
    done
    return 1
)

reconcile_writers_absent() (
    [ -d "$UPDATE_PROC_ROOT" ] && [ ! -L "$UPDATE_PROC_ROOT" ] && [ -r "$UPDATE_PROC_ROOT" ] || return 1
    [ ! -e "$UPDATE_PROC_ROOT/$RQ_OWNER" ] && [ ! -L "$UPDATE_PROC_ROOT/$RQ_OWNER" ] || return 1
    for rw_dir in "$UPDATE_PROC_ROOT"/[0-9]*; do
        [ -d "$rw_dir" ] || continue
        rw_pid="${rw_dir##*/}"
        [ "$rw_pid" != "$$" ] || continue
        if [ ! -r "$rw_dir/cmdline" ]; then
            [ ! -e "$rw_dir" ] && continue
            return 1
        fi
        # NUL arguments are kept as separate records. No argv or environment
        # is ever printed into the public receipt or diagnostic output.
        # Check the proc read itself: without pipefail an unreadable live PID
        # must not become awk's empty, apparently writer-free input.
        if ! rw_args="$(tr '\000' '\n' < "$rw_dir/cmdline" 2>/dev/null)"; then
            [ ! -e "$rw_dir" ] && continue
            return 1
        fi
        if printf '%s\n' "$rw_args" | LC_ALL=C awk '
            prev ~ /(^|\/)self_update\.sh$/ && $0 ~ /^(apply|reconcile-rollback)$/ {found=1}
            prev ~ /(^|\/)(apply_provider_profile|apply_network_profile|blanc_xkeen_update_outbounds)\.sh$/ && $0 ~ /^(apply|apply-core|recover-core|core-restart)$/ {found=1}
            prev ~ /(^|\/)freenet-ui$/ && $0=="apply" {found=1}
            {prev=$0}
            END {exit !found}
        '; then
            reconcile_own_descendant "$rw_pid" || return 1
        fi
    done
    [ ! -e "$UPDATE_PROC_ROOT/$RQ_OWNER" ] && [ ! -L "$UPDATE_PROC_ROOT/$RQ_OWNER" ]
)

reconcile_checkpoint_absent() (
    reconcile_root_path_safe "$CHECKPOINT_DIR" || return 1
    [ ! -e "$CHECKPOINT_DIR" ] && [ ! -L "$CHECKPOINT_DIR" ]
)

reconcile_xray_role() (
    rx_pid="$1"; rx_comm="$2"
    # Read status is checked before parsing. Original newlines become CR so
    # an unrelated environment value cannot create another key record.
    rx_env="$(tr '\000\012' '\012\015' < "$UPDATE_PROC_ROOT/$rx_pid/environ" 2>/dev/null)" || return 1
    printf '%s\n' "$rx_env" | LC_ALL=C awk -v comm="$rx_comm" -v config="XRAY_LOCATION_CONFDIR=$ROOT/etc/xray/configs" '
        /^FREENET_XRAY_PROBE=/ { probes++; probe=$0 }
        /^XRAY_LOCATION_CONFDIR=/ { configs++; if($0==config) configured=1 }
        END {
            if(probes>1 || configs>1) exit 1
            if(comm=="fn-xray-probe" && probe=="FREENET_XRAY_PROBE=1") { print "PROBE"; exit }
            if(comm=="xray" && (probes==0 || probe=="FREENET_XRAY_PROBE=") && configs==1 && configured) { print "CORE"; exit }
            exit 1
        }
    '
)

reconcile_xray_runtime_proof() (
    rx_bin="$ROOT/sbin/xray"
    reconcile_root_path_safe "$rx_bin" || return 1
    rx_image="$(update_file_proof "$rx_bin" 134217728 755)" || return 1
    rx_path="$(readlink -f "$rx_bin")" || return 1
    [ -n "$rx_path" ] || return 1
    printf '%s\000run\000' "$rx_bin" > "$TMP_DIR/xray-argv.full" || return 1
    printf 'xray\000run\000' > "$TMP_DIR/xray-argv.short" || return 1
    rx_core=""
    for rx_dir in "$UPDATE_PROC_ROOT"/[0-9]*; do
        [ -d "$rx_dir" ] || continue
        rx_pid="${rx_dir##*/}"
        if ! rx_comm="$(cat "$rx_dir/comm" 2>/dev/null)"; then
            [ ! -e "$rx_dir" ] && continue
            return 1
        fi
        if ! rx_exe="$(readlink "$rx_dir/exe" 2>/dev/null)"; then
            [ ! -e "$rx_dir" ] && continue
            case "$rx_comm" in xray|fn-xray-probe) return 1 ;; esac
            continue
        fi
        case "$rx_exe" in
            "$rx_path"|"$rx_path (deleted)") ;;
            *) case "$rx_comm" in xray|fn-xray-probe) return 1 ;; esac; continue ;;
        esac
        if ! rx_role="$(reconcile_xray_role "$rx_pid" "$rx_comm")"; then
            [ ! -e "$rx_dir" ] && continue
            return 1
        fi
        # Marked, aliased read-only probes are expected under retained update
        # fences. They are not production cores and are never signalled here.
        [ "$rx_role" != PROBE ] || continue
        [ "$rx_role" = CORE ] && [ -z "$rx_core" ] || return 1
        rx_birth="$(ui_proc_start "$rx_pid")" || return 1
        LC_ALL=C awk -v uid="$UPDATE_UID" '/^Uid:/ { if(NF!=5 || $2!=uid || $3!=uid || $4!=uid || $5!=uid) exit 1; found=1 } END { if(!found) exit 1 }' "$rx_dir/status" || return 1
        rx_cmdline="$TMP_DIR/xray-$rx_pid.cmdline"
        cat "$rx_dir/cmdline" > "$rx_cmdline" || return 1
        if ! cmp -s "$rx_cmdline" "$TMP_DIR/xray-argv.full" && ! cmp -s "$rx_cmdline" "$TMP_DIR/xray-argv.short"; then return 1; fi
        rx_argv="$(update_sha "$rx_cmdline")" || return 1
        rx_stat="$(update_stat "$rx_dir/exe")" || return 1
        rx_sha="$(update_sha "$rx_dir/exe")" || return 1
        [ "$rx_exe" = "$rx_path" ] && [ "$rx_stat" = "${rx_image%:*}" ] && [ "$rx_sha" = "${rx_image##*:}" ] || return 1
        [ "$(ui_proc_start "$rx_pid")" = "$rx_birth" ] &&
            [ "$(update_stat "$rx_dir/exe")" = "$rx_stat" ] &&
            [ "$(readlink "$rx_dir/exe")" = "$rx_path" ] &&
            [ "$(update_sha "$rx_dir/cmdline")" = "$rx_argv" ] &&
            [ "$(cat "$rx_dir/comm")" = xray ] &&
            [ "$(reconcile_xray_role "$rx_pid" xray)" = CORE ] || return 1
        rx_core="$rx_pid:$rx_birth:$rx_stat:$rx_sha:$rx_argv"
    done
    [ -n "$rx_core" ] && [ "$(update_stat "$rx_bin")" = "${rx_image%:*}" ] || return 1
    printf 'XRAY_IMAGE=%s\nXRAY_CORE=%s\n' "$rx_image" "$rx_core"
)

reconcile_manifest_sha() (
    printf '%s\n' "$1" | LC_ALL=C grep -Eq '^[A-Za-z0-9_.-]+$' || return 1
    LC_ALL=C awk -v name="$1" '
        $2==name { if(NF!=2 || length($1)!=64 || $1 ~ /[^0-9a-f]/) bad=1; found++; sha=$1 }
        END { if(bad || found!=1) exit 1; print sha }
    ' "$TMP_DIR/SHA256SUMS"
)

reconcile_backup_inventory() (
    for ri_entry in "$RQ_BACKUP"/* "$RQ_BACKUP"/.[!.]* "$RQ_BACKUP"/..?*; do
        if [ ! -e "$ri_entry" ] && [ ! -L "$ri_entry" ]; then continue; fi
        [ -f "$ri_entry" ] && [ ! -L "$ri_entry" ] || return 1
        ri_name="${ri_entry##*/}"
        case "$ri_name" in
            version-marker.exists|version-marker.before|preserved.before|xray-hashes.before|service.log) ;;
            asset-*.before|asset-*.exists)
                printf '%s\n' "$ri_name" | LC_ALL=C grep -Eq '^asset-([1-9]|10|11)\.(before|exists)$' || return 1
                ;;
            ui-v*.log)
                ri_tag="${ri_name#ui-}"; ri_tag="${ri_tag%.log}"
                valid_tag "$ri_tag" || return 1
                ;;
            *) return 1 ;;
        esac
    done
)

reconcile_version_proof() (
    rv_marker="$(update_file_proof "$RQ_BACKUP/version-marker.exists" 8 600)" || return 1
    case "$(cat "$RQ_BACKUP/version-marker.exists")" in
        yes)
            reconcile_marker "$RQ_BACKUP/version-marker.exists" yes > /dev/null || return 1
            rv_before="$(reconcile_marker "$RQ_BACKUP/version-marker.before" "$RQ_FROM")" || return 1
            reconcile_root_path_safe "$VERSION_FILE" || return 1
            rv_live="$(reconcile_marker "$VERSION_FILE" "$RQ_FROM")" || return 1
            printf 'VERSION_EXISTS=%s\nVERSION_BACKUP=%s\nVERSION_LIVE=%s\n' "$rv_marker" "$rv_before" "$rv_live"
            ;;
        no)
            reconcile_marker "$RQ_BACKUP/version-marker.exists" no > /dev/null || return 1
            reconcile_root_path_safe "$VERSION_FILE" || return 1
            [ ! -e "$RQ_BACKUP/version-marker.before" ] && [ ! -L "$RQ_BACKUP/version-marker.before" ] &&
                [ ! -e "$VERSION_FILE" ] && [ ! -L "$VERSION_FILE" ] || return 1
            printf 'VERSION_EXISTS=%s\nVERSION_LIVE=ABSENT\n' "$rv_marker"
            ;;
        *) return 1 ;;
    esac
)

reconcile_build_proof() (
    reconcile_checkpoint_absent || return 1
    rb_xray_runtime="$(reconcile_xray_runtime_proof)" || return 1
    [ "$(update_file_proof "$STATE_FILE" 131072 600)" = "$RQ_STATE_PROOF" ] || return 1
    rb_backup="$(update_directory_proof "$RQ_BACKUP" 700)" || return 1
    reconcile_backup_inventory || return 1
    rb_manifest="$(update_file_proof "$TMP_DIR/SHA256SUMS" 2097152)" || return 1
    [ "${rb_manifest##*:}" = "$RQ_MANIFEST_SHA" ] || return 1
    printf 'PROTOCOL=1\nRECEIPT=%s\nFROM_VERSION=%s\nTARGET_VERSION=%s\nARCH=%s\nBACKUP=%s\nFROM_MANIFEST_SHA256=%s\n' "$RQ_STATE_PROOF" "$RQ_FROM" "$RQ_TARGET" "$ARCH" "$rb_backup" "$RQ_MANIFEST_SHA"
    printf 'CHECKPOINT=ABSENT\n%s\n' "$rb_xray_runtime"
    reconcile_lock_proof || return 1
    reconcile_writers_absent || return 1
    rb_i=0
    for rb_name in $(asset_list); do
        rb_i=$((rb_i + 1))
        rb_dest="$(asset_dest "$rb_name")" || return 1
        reconcile_root_path_safe "$rb_dest" || return 1
        rb_mode="$(asset_mode "$rb_name")"
        rb_exists="$(reconcile_marker "$RQ_BACKUP/asset-$rb_i.exists" yes)" || return 1
        rb_saved="$(update_file_proof "$RQ_BACKUP/asset-$rb_i.before" 67108864 "$rb_mode")" || return 1
        rb_live="$(update_file_proof "$rb_dest" 67108864 "$rb_mode")" || return 1
        rb_expected="$(reconcile_manifest_sha "$rb_name")" || return 1
        [ "${rb_saved##*:}" = "$rb_expected" ] && [ "${rb_live##*:}" = "$rb_expected" ] || return 1
        printf 'ASSET_%s_EXISTS=%s\nASSET_%s_BACKUP=%s\nASSET_%s_LIVE=%s\n' "$rb_i" "$rb_exists" "$rb_i" "$rb_saved" "$rb_i" "$rb_live"
    done
    [ "$rb_i" -eq 11 ] || return 1
    reconcile_version_proof || return 1
    rb_preserved="$(update_file_proof "$RQ_BACKUP/preserved.before" 16777216 600)" || return 1
    rb_xray="$(update_file_proof "$RQ_BACKUP/xray-hashes.before" 2097152 600)" || return 1
    for rb_protected in "$ROOT/etc/freenet/freenet.conf" "$ROOT/etc/freenet/vpn_profile_name" \
        "$ROOT/etc/xray/blanc_profile_filter.regex" "$ROOT/etc/xray/blanc_subscription.url"; do
        reconcile_root_path_safe "$rb_protected" || return 1
        if [ -e "$rb_protected" ] || [ -L "$rb_protected" ]; then
            rb_protected_proof="$(update_file_proof "$rb_protected" 16777216)" || return 1
            printf 'PROTECTED_FILE=%s:%s\n' "$rb_protected" "$rb_protected_proof"
        fi
    done
    # Symlinked configs must not disappear from find -type f comparisons.
    [ -d "$ROOT/etc/xray/configs" ] && [ ! -L "$ROOT/etc/xray/configs" ] || return 1
    rb_links="$(find "$ROOT/etc/xray/configs" -maxdepth 1 -type l -print)" || return 1
    [ -z "$rb_links" ] || return 1
    for rb_config in "$ROOT/etc/xray/configs"/*.json; do
        if [ ! -e "$rb_config" ] && [ ! -L "$rb_config" ]; then continue; fi
        reconcile_root_path_safe "$rb_config" || return 1
        rb_config_proof="$(update_file_proof "$rb_config" 16777216)" || return 1
        printf 'XRAY_FILE=%s:%s\n' "$rb_config" "$rb_config_proof"
    done
    snapshot_preserved_state "$TMP_DIR/reconcile-preserved.current" || return 1
    snapshot_xray "$TMP_DIR/reconcile-xray.current" || return 1
    cmp -s "$RQ_BACKUP/preserved.before" "$TMP_DIR/reconcile-preserved.current" &&
        cmp -s "$RQ_BACKUP/xray-hashes.before" "$TMP_DIR/reconcile-xray.current" || return 1
    [ "$(update_file_proof "$RQ_BACKUP/preserved.before" 16777216 600)" = "$rb_preserved" ] &&
        [ "$(update_file_proof "$RQ_BACKUP/xray-hashes.before" 2097152 600)" = "$rb_xray" ] || return 1
    printf 'PRESERVED=%s\nXRAY_CONFIGS=%s\n' "$rb_preserved" "$rb_xray"
    [ "$(update_file_proof "$UI_INIT" 16384 755)" = "$UI_INIT_PROOF" ] || return 1
    printf 'INIT=%s\n' "$UI_INIT_PROOF"
    rb_ui_sha="$(reconcile_manifest_sha "freenet-ui-$ARCH")" || return 1
    ui_runtime_proof "$RQ_FROM" "$rb_ui_sha" || return 1
    reconcile_writers_absent || return 1
    reconcile_checkpoint_absent && [ "$(reconcile_xray_runtime_proof)" = "$rb_xray_runtime" ] || return 1
    [ "$(update_directory_proof "$RQ_BACKUP" 700)" = "$rb_backup" ] &&
        [ "$(update_file_proof "$STATE_FILE" 131072 600)" = "$RQ_STATE_PROOF" ] || return 1
)

reconcile_stop() {
    err "reconciliation stopped: $1; original receipt and both fences preserved"
    say 'SUCCESS=no'
    say 'READY=no'
    say "ERROR=$1"
    say 'APPLICATION_RECEIPT=PRESERVED'
    say 'FENCES=PRESERVED'
    say 'VPN_MUTATION=NONE'
    if [ "${RQ_ARCHIVE_CREATED:-0}" = 1 ]; then say 'MUTATION=APPLICATION_EVIDENCE_ONLY'; else say 'MUTATION=NONE'; fi
    return 1
}

run_reconcile_rollback() {
    RQ_REQUESTED_SHA="$1"
    [ "$TEST_MODE" = no ] || { reconcile_stop 'runtime proof cannot use the test-mode acceptance bypass'; return 1; }
    umask 077
    for RQ_TOOL in curl sha256sum sed awk grep cmp mktemp jq stat readlink id sort tr sync; do
        command -v "$RQ_TOOL" > /dev/null 2>&1 || { reconcile_stop 'required proof command is unavailable'; return 1; }
    done
    make_tmp || { reconcile_stop 'private proof staging is unavailable'; return 1; }
    if [ "$MODE" = reconcile-rollback ]; then
        [ "${#RQ_REQUESTED_SHA}" -eq 64 ] || { reconcile_stop 'exact planned receipt SHA-256 is required'; return 1; }
        case "$RQ_REQUESTED_SHA" in *[!0-9a-f]*) reconcile_stop 'receipt SHA-256 is invalid'; return 1 ;; esac
        # A separate empty guard serializes this narrowly scoped command. The
        # existing update/network fences remain byte-for-byte untouched.
        mkdir "$RECONCILE_GUARD_DIR" || { reconcile_stop 'another reconciliation is running or its temporary guard is unresolved'; return 1; }
        RECONCILE_GUARD_HELD=1
        chmod 700 "$RECONCILE_GUARD_DIR" || { reconcile_stop 'cannot protect reconciliation guard'; return 1; }
        RQ_GUARD_PROOF="$(update_directory_proof "$RECONCILE_GUARD_DIR" 700)" || { reconcile_stop 'cannot pin reconciliation guard'; return 1; }
    fi
    reconcile_state_values || { reconcile_stop 'UNKNOWN receipt or backup identity is invalid'; return 1; }
    if [ -n "$RQ_REQUESTED_SHA" ] && [ "$RQ_REQUESTED_SHA" != "$RQ_SHA" ]; then
        reconcile_stop 'receipt changed after the read-only plan'; return 1
    fi
    RQ_LOCKS_PROOF="$(reconcile_lock_proof)" && reconcile_writers_absent ||
        { reconcile_stop 'both owned fences and a stopped updater are required'; return 1; }
    reconcile_checkpoint_absent || { reconcile_stop 'pending VPN checkpoint must be absent'; return 1; }
    get_arch || { reconcile_stop 'Entware architecture is unknown'; return 1; }
    fetch_release_metadata "$RQ_FROM" && fetch_manifest "$RQ_FROM" || { reconcile_stop 'exact FROM release metadata or SHA-256 manifest is unavailable'; return 1; }
    RQ_MANIFEST_SHA="$(update_sha "$TMP_DIR/SHA256SUMS")" || { reconcile_stop 'FROM manifest cannot be pinned'; return 1; }
    ui_prepare_service || { reconcile_stop 'stock HTTP daemon launch specification cannot be verified'; return 1; }
    reconcile_build_proof > "$TMP_DIR/reconcile.proof" || { reconcile_stop 'restored application, protected state, runtime or owned fences are not proven'; return 1; }
    if [ "$MODE" = reconcile-rollback-plan ]; then
        say 'SUCCESS=yes'
        say 'READY=yes'
        say "RECEIPT_SHA256=$RQ_SHA"
        say "FROM_VERSION=$RQ_FROM"
        say "TARGET_VERSION=$RQ_TARGET"
        say 'APPLICATION_ASSETS=VERIFIED'
        say 'RUNTIME=VERIFIED_FROM_RELEASE'
        say 'FENCES=PRESERVED'
        say 'MUTATION=NONE'
        return 0
    fi

    RQ_ARCHIVE_ROOT="$ROOT/var/lib/freenet/update-rollback-reconciliation"
    reconcile_root_path_safe "$RQ_ARCHIVE_ROOT" || { reconcile_stop 'archive path is unsafe'; return 1; }
    mkdir -p "$RQ_ARCHIVE_ROOT" || { reconcile_stop 'cannot create reconciliation archive root'; return 1; }
    update_directory_proof "$RQ_ARCHIVE_ROOT" 700 > /dev/null || { reconcile_stop 'archive directory ownership or mode is unsafe'; return 1; }
    RQ_ARCHIVE="$RQ_ARCHIVE_ROOT/$RQ_SHA.$$"
    mkdir "$RQ_ARCHIVE" || { reconcile_stop 'cannot create unique reconciliation archive'; return 1; }
    RQ_ARCHIVE_CREATED=1
    RQ_STAMP="$(date -u +%Y-%m-%dT%H:%M:%SZ)" || { reconcile_stop 'cannot timestamp recovery evidence'; return 1; }
    cp "$STATE_FILE" "$RQ_ARCHIVE/original.state" &&
        cp "$TMP_DIR/reconcile.proof" "$RQ_ARCHIVE/proof" &&
        cp "$TMP_DIR/SHA256SUMS" "$RQ_ARCHIVE/SHA256SUMS" || { reconcile_stop 'cannot archive original receipt and proof'; return 1; }
    [ "$(update_sha "$RQ_ARCHIVE/original.state")" = "$RQ_SHA" ] || { reconcile_stop 'original receipt changed during archival'; return 1; }
    LC_ALL=C awk -v stamp="$RQ_STAMP" -v receipt="$RQ_SHA" -v archive="$RQ_ARCHIVE" '
        /^STATE=/ { print "STATE=FAILED"; next }
        /^ROLLBACK_STATE=/ { print "ROLLBACK_STATE=SUCCESS"; next }
        /^MESSAGE=/ { print "MESSAGE=Предыдущее состояние приложения восстановлено и независимо подтверждено; блокировки сохранены до штатного снятия"; next }
        /^UPDATED_AT=/ { print "UPDATED_AT=" stamp; next }
        { print }
        END { print "RECONCILIATION_RECEIPT_SHA256=" receipt; print "RECONCILIATION_ARCHIVE=" archive }
    ' "$RQ_ARCHIVE/original.state" > "$RQ_ARCHIVE/terminal.state" || { reconcile_stop 'cannot prepare terminal receipt'; return 1; }
    chmod 600 "$RQ_ARCHIVE/original.state" "$RQ_ARCHIVE/proof" "$RQ_ARCHIVE/SHA256SUMS" "$RQ_ARCHIVE/terminal.state" || { reconcile_stop 'cannot protect durable evidence'; return 1; }
    RQ_TERMINAL_SHA="$(update_sha "$RQ_ARCHIVE/terminal.state")" || { reconcile_stop 'cannot pin terminal receipt'; return 1; }
    RQ_PROOF_SHA="$(update_sha "$RQ_ARCHIVE/proof")" || { reconcile_stop 'cannot pin archived proof'; return 1; }
    RQ_ARCHIVE_PROOF="$(update_directory_proof "$RQ_ARCHIVE" 700)" || { reconcile_stop 'cannot pin private archive'; return 1; }
    RQ_TERMINAL_TMP="$(mktemp "$STATE_FILE.reconcile.XXXXXX")" || { reconcile_stop 'cannot stage atomic terminal receipt'; return 1; }
    cp "$RQ_ARCHIVE/terminal.state" "$RQ_TERMINAL_TMP" && chmod 600 "$RQ_TERMINAL_TMP" ||
        { rm -f "$RQ_TERMINAL_TMP"; reconcile_stop 'cannot prepare exact terminal receipt bytes'; return 1; }
    # Archive and prospective terminal bytes are durable before UNKNOWN can
    # change. Old UI versions recognize only apply as an active updater.
    sync || { rm -f "$RQ_TERMINAL_TMP"; reconcile_stop 'cannot make recovery evidence durable'; return 1; }
    if ! reconcile_build_proof > "$TMP_DIR/reconcile.repeat" ||
       ! cmp -s "$TMP_DIR/reconcile.proof" "$TMP_DIR/reconcile.repeat" ||
       [ "$(update_directory_proof "$RQ_ARCHIVE" 700)" != "$RQ_ARCHIVE_PROOF" ] ||
       ! RQ_ORIGINAL_FINAL="$(update_file_proof "$RQ_ARCHIVE/original.state" 131072 600)" || [ "${RQ_ORIGINAL_FINAL##*:}" != "$RQ_SHA" ] ||
       ! RQ_PROOF_FINAL="$(update_file_proof "$RQ_ARCHIVE/proof" 16777216 600)" || [ "${RQ_PROOF_FINAL##*:}" != "$RQ_PROOF_SHA" ] ||
       ! RQ_MANIFEST_FINAL="$(update_file_proof "$RQ_ARCHIVE/SHA256SUMS" 2097152 600)" || [ "${RQ_MANIFEST_FINAL##*:}" != "$RQ_MANIFEST_SHA" ] ||
       ! RQ_TERMINAL_FINAL="$(update_file_proof "$RQ_ARCHIVE/terminal.state" 131072 600)" || [ "${RQ_TERMINAL_FINAL##*:}" != "$RQ_TERMINAL_SHA" ] ||
       [ "$(update_sha "$RQ_TERMINAL_TMP")" != "$RQ_TERMINAL_SHA" ]; then
        rm -f "$RQ_TERMINAL_TMP"
        reconcile_stop 'evidence changed before terminal commit'
        return 1
    fi
    # Final rechecks are adjacent to the only state mutation. No service,
    # asset, receipt, archive or fence writes are allowed after this rename.
    if [ "$(update_directory_proof "$RECONCILE_GUARD_DIR" 700)" != "$RQ_GUARD_PROOF" ] ||
       [ "$(reconcile_lock_proof)" != "$RQ_LOCKS_PROOF" ] ||
       [ "$(update_file_proof "$STATE_FILE" 131072 600)" != "$RQ_STATE_PROOF" ] ||
       ! reconcile_checkpoint_absent || ! reconcile_writers_absent; then
        rm -f "$RQ_TERMINAL_TMP"
        reconcile_stop 'receipt or updater ownership changed before terminal commit'
        return 1
    fi
    if ! mv -f "$RQ_TERMINAL_TMP" "$STATE_FILE"; then
        rm -f "$RQ_TERMINAL_TMP"
        reconcile_stop 'atomic terminal receipt commit failed'
        return 1
    fi
    say 'SUCCESS=yes'
    say 'RESULT=RECONCILED_ROLLBACK'
    say 'ROLLBACK=SUCCESS'
    say 'FENCES=PRESERVED'
    say 'VPN_MUTATION=NONE'
    say 'MUTATION=APPLICATION_RECEIPT_ONLY'
    say "RECEIPT_SHA256=$RQ_SHA"
    say "ARCHIVE=$RQ_ARCHIVE"
}

run_apply() {
    umask 077
    if ! mkdir "$LOCK_DIR" 2>/dev/null; then
        err 'update lock is already held; existing state preserved'
        exit 3
    fi
    LOCK_HELD=1
    if ! previous_update_receipt_safe; then
        KEEP_LOCK=1
        err 'previous application receipt is unknown; original state preserved'
        exit 2
    fi
    if [ -e "$STATE_FILE" ] && [ ! -r "$STATE_FILE" ]; then
        KEEP_LOCK=1; err 'update state is unreadable'; exit 2
    fi
    # A missing lock never clears a previous unconfirmed application rollback.
    if [ -L "$STATE_FILE" ] || { [ -e "$STATE_FILE" ] && [ ! -f "$STATE_FILE" ]; } ||
       grep -Eq '^(STATE=(ROLLBACK_FAILED|UPDATING|RECONNECTING)|ROLLBACK_STATE=(FAILED_UNKNOWN|PENDING))$' "$STATE_FILE" 2>/dev/null; then
        KEEP_LOCK=1
        err 'previous application update is unresolved; state and backup preserved'
        exit 2
    fi
    printf '%s\n' "$$" > "$LOCK_DIR/owner.pid" || fail_before_mutation 'cannot persist update owner'
    printf '%s\n' "$TARGET_TAG" > "$LOCK_DIR/target" || fail_before_mutation 'cannot persist update target'
    if mkdir "$MUTATION_LOCK_DIR" 2>/dev/null; then
        MUTATION_LOCK_HELD=1
        printf '%s\n' "$$" > "$MUTATION_LOCK_DIR/pid" || fail_before_mutation 'cannot persist network fence owner'
        printf '%s\n' "$LOCK_DIR" > "$MUTATION_LOCK_DIR/freenet-update-owner" || fail_before_mutation 'cannot persist fence ownership'
    else
        # This is not an unlock: any recovery remains behind exact release
        # staging + protected checkpoint verification and a protected claim.
        NETWORK_FENCE_DEFERRED=1
    fi
    detect_current || fail_before_mutation 'current FreeNet version is invalid'
    valid_tag "$TARGET_TAG" || fail_before_mutation 'target release tag is invalid'
    beta_opted_in "$TARGET_TAG" || fail_before_mutation 'explicit Beta opt-in required'
    [ "$TARGET_TAG" != "$CURRENT_VERSION" ] || fail_before_mutation 'target FreeNet version is already installed'
    get_arch || fail_before_mutation 'unsupported Entware architecture'
    for T in curl sha256sum sed awk grep cmp mktemp jq stat readlink id sort tr; do
        command -v "$T" >/dev/null 2>&1 || fail_before_mutation "required command missing: $T"
    done
    fetch_release_metadata "$TARGET_TAG" || fail_before_mutation 'target release metadata or Beta channel is not verified'

    compatible_update_target "$TARGET_TAG" || fail_before_mutation 'target cannot preserve pending VPN recovery state; choose a recovery-protocol-2 release or the v0.7.18 recovery bridge'
    write_state CHECKING "$TARGET_TAG" 'Проверяем выбранный exact release и SHA-256' '' NOT_NEEDED '' || fail_before_mutation 'cannot persist update state'

    make_tmp || fail_before_mutation 'cannot create staging directory'
    download_assets "$TARGET_TAG" || fail_before_mutation "${LAST_DOWNLOAD_ERROR:-release asset download or SHA-256 verification failed}"
    validate_stage || fail_before_mutation 'staging validation failed'
    if [ "$NETWORK_FENCE_DEFERRED" = 1 ]; then
        snapshot_preserved_state "$TMP_DIR/fence-before" || fail_before_mutation 'cannot preserve original VPN evidence'
        snapshot_xray "$TMP_DIR/xray-fence-before" || fail_before_mutation 'cannot verify Xray baseline'
        claim_verified_orphan_vpn_lock || fail_before_mutation 'VPN/network lock ownership could not be proved safe; STOP'
        snapshot_preserved_state "$TMP_DIR/fence-after" || fail_before_mutation 'VPN evidence changed during protected lock claim; STOP'
        snapshot_xray "$TMP_DIR/xray-fence-after" || fail_before_mutation 'Xray changed during protected lock claim; STOP'
        cmp -s "$TMP_DIR/fence-before" "$TMP_DIR/fence-after" || fail_before_mutation 'VPN evidence changed during protected lock claim; STOP'
        cmp -s "$TMP_DIR/xray-fence-before" "$TMP_DIR/xray-fence-after" || fail_before_mutation 'Xray baseline changed during protected lock claim; STOP'
    fi
    if [ "$TEST_MODE" = no ]; then
        ui_prepare_service || fail_before_mutation 'stock HTTP daemon launch specification cannot be verified'
        UI_OLD_IMAGE_PROOF="$(update_file_proof "$UI_BIN" 67108864 755)" || fail_before_mutation 'current HTTP daemon image cannot be verified'
        UI_OLD_SHA="${UI_OLD_IMAGE_PROOF##*:}"
        UI_BEFORE="$(ui_find_daemon "$UI_OLD_SHA")" || fail_before_mutation 'current HTTP daemon identity is ambiguous'
        if [ -n "$UI_BEFORE" ]; then
            ui_runtime_proof "$CURRENT_VERSION" "$UI_OLD_SHA" > "$TMP_DIR/ui-before.proof" ||
                fail_before_mutation 'current HTTP daemon ownership, health or version is unconfirmed'
            UI_OLD_PIN="$(ui_stable_pin "$UI_BEFORE")" || fail_before_mutation 'cannot pin current HTTP daemon lifetime'
        else
            UI_BEFORE_SOCKETS="$(ui_listener_table)" || fail_before_mutation 'current HTTP listeners cannot be inspected'
            [ -z "$UI_BEFORE_SOCKETS" ] || fail_before_mutation 'UI port is occupied by an unowned listener'
        fi
    fi
    write_state SNAPSHOT "$TARGET_TAG" 'Создаём snapshot FreeNet-owned файлов' '' NOT_NEEDED '' || true
    prepare_backup || fail_before_mutation 'cannot create pre-update snapshot'

    write_state UPDATING "$TARGET_TAG" 'Применяем проверенные FreeNet assets' '' PENDING "$BACKUP_DIR" || fail_before_mutation 'cannot persist update checkpoint'
    MUTATED=1
    install_assets || fail_after_mutation 'live asset replacement failed'

    write_state RECONNECTING "$TARGET_TAG" 'FreeNet перезапускается; подтверждаем фактическое состояние' '' PENDING "$BACKUP_DIR" || true
    [ "$FAIL_STAGE" != interrupt-after-replace ] || kill -TERM "$$"
    restart_ui "$TARGET_TAG" || fail_after_mutation 'FreeNet UI restart failed'
    accept_runtime "$TARGET_TAG" || fail_after_mutation 'post-update runtime acceptance failed'
    persist_version "$TARGET_TAG" || fail_after_mutation 'cannot persist installed version marker'

    write_state SUCCESS "$TARGET_TAG" 'Выбранная версия FreeNet установлена и проверена' '' NOT_NEEDED "$BACKUP_DIR" || fail_after_mutation 'cannot persist update acceptance'
    MUTATED=0
    say "RESULT=SUCCESS"
    say "TARGET_VERSION=$TARGET_TAG"
    say "BACKUP_DIR=$BACKUP_DIR"
    say 'ROLLBACK=NOT_NEEDED'
}

case "$MODE" in
    plan) run_plan ;;
    reconcile-rollback-plan)
        [ -z "$TARGET_TAG" ] || { err 'usage: self_update.sh reconcile-rollback-plan'; exit 2; }
        run_reconcile_rollback ''
        ;;
    reconcile-rollback)
        run_reconcile_rollback "$TARGET_TAG"
        ;;
    apply)
        [ -n "$TARGET_TAG" ] || { err 'usage: self_update.sh apply <vX.Y.Z>'; exit 2; }
        run_apply
        ;;
    *) err 'usage: self_update.sh [plan [vX.Y.Z]|apply <vX.Y.Z>|reconcile-rollback-plan|reconcile-rollback <receiptSHA256>]'; exit 2 ;;
esac
