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
TEST_MODE="${FREENET_SELF_UPDATE_TEST_MODE:-no}"
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
if [ "$MODE" = plan ]; then
    # Read-only planning must finish inside the Control Center request budget.
    # Apply keeps the longer retry policy for real asset downloads.
    DOWNLOAD_RETRIES=1
fi
TMP_DIR=""
BACKUP_DIR=""
LOCK_HELD=0
STACK_LOCK_HELD=0
STACK_MUTATION_LOCK=""
KEEP_LOCK=0
MUTATED=0
LAST_DOWNLOAD_ERROR=""
RELEASE_META_TAG=""
RELEASE_META_FILE=""

say() { printf '%s\n' "$*"; }
err() { printf '[FreeNet Web Update] ERROR: %s\n' "$*" >&2; }

cleanup() {
    [ -n "$TMP_DIR" ] && [ -d "$TMP_DIR" ] && rm -rf "$TMP_DIR" 2>/dev/null || true
    if [ "$LOCK_HELD" = 1 ] && [ "$KEEP_LOCK" = 0 ]; then
        rm -rf "$LOCK_DIR" 2>/dev/null || true
        LOCK_HELD=0
    fi
    if [ "$STACK_LOCK_HELD" = 1 ] && [ "$KEEP_LOCK" = 0 ]; then
        # Never remove a lock that is no longer owned by this updater.
        if [ "$(cat "$STACK_MUTATION_LOCK/pid" 2>/dev/null)" = "$" ]; then
            rm -rf "$STACK_MUTATION_LOCK" 2>/dev/null || true
            STACK_LOCK_HELD=0
        fi
    fi
}
trap cleanup 0 1 2 15

normalize_current() {
    case "$CURRENT_VERSION" in
        v*) ;;
        '') return 1 ;;
        *) CURRENT_VERSION="v$CURRENT_VERSION" ;;
    esac
    valid_tag "$CURRENT_VERSION"
}

valid_tag() {
    TAG="$1"
    printf '%s\n' "$TAG" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+$' || return 1
    PATCH="${TAG##*.}"
    [ "$PATCH" -le 99 ]
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
        exit 1;
    }'
}

make_tmp() {
    [ -n "$TMP_DIR" ] && [ -d "$TMP_DIR" ] && return 0
    TMP_DIR="$(mktemp -d /tmp/freenet-self-update.XXXXXX 2>/dev/null)"
    if [ -z "$TMP_DIR" ] || [ ! -d "$TMP_DIR" ]; then
        TMP_DIR="/tmp/freenet-self-update.$$"
        mkdir -p "$TMP_DIR" || return 1
    fi
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
        if [ "$MODE" = plan ]; then
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

# Trust only GitHub's canonical stable-release redirect. HEAD does not
# consume the per-IP unauthenticated REST API quota. No HTML scraping, remote
# executable redirects, downgrade guesses, or insecure TLS are allowed.
web_latest_tag() {
    [ "$REPO" = 'VoltickVL/FreeNet-Router' ] || return 1
    make_tmp || return 1
    if [ -s "$TMP_DIR/web-latest-stable" ]; then
        cat "$TMP_DIR/web-latest-stable"
        return 0
    fi
    HDR="$TMP_DIR/web-latest.headers"
    curl -sS -I --connect-timeout 5 --max-time 12 --max-redirs 0 \
        -D "$HDR" -o /dev/null "https://github.com/$REPO/releases/latest" 2>/dev/null || return 1
    CODE="$(awk '/^HTTP\// {code=$2} END {print code}' "$HDR")"
    case "$CODE" in 301|302|303|307|308) ;; *) return 1 ;; esac
    LOC="$(sed -n 's/^[Ll]ocation:[[:space:]]*//p' "$HDR" | tr -d '\r' | tail -n 1)"
    case "$LOC" in
        "https://github.com/$REPO/releases/tag/"*) TAG="${LOC##*/}" ;;
        "/$REPO/releases/tag/"*) TAG="${LOC##*/}" ;;
        *) return 1 ;;
    esac
    valid_tag "$TAG" || return 1
    printf '%s\n' "$TAG" > "$TMP_DIR/web-latest-stable" || return 1
    printf '%s\n' "$TAG"
}

latest_tag() {
    if [ -n "$LATEST_OVERRIDE" ]; then
        printf '%s\n' "$LATEST_OVERRIDE"
        return 0
    fi
    # When the REST API is rate limited, the stable-releases HTML link still
    # gives a trusted exact tag without consuming the API's hourly budget.
    if WEB_LATEST="$(web_latest_tag)"; then
        printf '%s\n' "$WEB_LATEST"
        return 0
    fi
    make_tmp || return 1
    META="$TMP_DIR/latest.json"
    download_url "https://api.github.com/repos/$REPO/releases/latest" "$META" || return 1
    TAG="$(jq -r '.tag_name // empty' "$META" 2>/dev/null)"
    valid_tag "$TAG" || return 1
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
            # API metadata is rate-limited. Only the *current latest stable*
            # tag confirmed by GitHub's trusted official redirect may bypass
            # this read; older exact targets and prereleases fail closed.
            WEB_LATEST="$(web_latest_tag)" || return 1
            [ "$WEB_LATEST" = "$TAG" ] || return 1
            printf '{"tag_name":"%s","draft":false,"prerelease":false,"body":""}\n' "$TAG" > "$META" || return 1
        fi
    fi
    META_TAG="$(jq -r '.tag_name // empty' "$META" 2>/dev/null)"
    META_DRAFT="$(jq -r '.draft // false' "$META" 2>/dev/null)"
    META_PRE="$(jq -r '.prerelease // false' "$META" 2>/dev/null)"
    [ "$META_TAG" = "$TAG" ] || return 1
    [ "$META_DRAFT" != true ] || return 1
    [ "$META_PRE" != true ] || return 1
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

asset_list() {
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


# Existing-stack upgrade preflight does not start/stop Xray, execute XKeen,
# read credentials, change cron, or probe the VPN.
STACK_KIND=UNKNOWN
STACK_ERROR=''
stack_refuse() {
    STACK_KIND="$1"
    STACK_ERROR="$2"
    return 1
}

check_existing_stack_compatibility() {
    STACK_KIND=UNKNOWN
    STACK_ERROR=''
    XK="$ROOT/sbin/xkeen"
    XR="$ROOT/sbin/xray"
    INIT="$ROOT/etc/init.d/S99xkeen"

    [ -x "$XK" ] && [ -x "$XR" ] && [ -f "$INIT" ] && [ -d "$ROOT/etc/xray/configs" ] ||
        { stack_refuse PARTIAL_STACK 'Existing XKeen/Xray/configs/init are incomplete; safe FreeNet helper upgrade is blocked'; return 1; }

    # Old Giga XKeen uses PID-only readiness without supported foreground start.
    # Neither a PID nor "xray run -test" proves startup compatibility.
    if ! grep -Fq 'XKEEN_FOREGROUND' "$XK" 2>/dev/null; then
        if grep -Eq 'pidof[[:space:]]+("?[$]name_client"?|xray)' "$INIT" 2>/dev/null; then
            stack_refuse LEGACY_PIDOF 'Legacy XKeen pidof startup without foreground support; working VPN preserved, update requires compatibility repair'
        else
            stack_refuse UNKNOWN_XKEEN 'XKeen foreground startup compatibility cannot be verified read-only; update is blocked'
        fi
        return 1
    fi

    # Do not upgrade the helpers while a VPN writer is running or unresolved.
    STACK_LOCK='/tmp/blanc_xkeen_update.lock'
    [ -z "$FREENET_STACK_MUTATION_LOCK" ] || STACK_LOCK="$FREENET_STACK_MUTATION_LOCK"
    [ ! -e "$STACK_LOCK" ] ||
        { stack_refuse MUTATION_BUSY 'An Xray/provider mutation lock exists; await read-only reconciliation'; return 1; }
    STACK_LOCK='/tmp/freenet-auto-vpn.lock'
    [ -z "$FREENET_STACK_AUTO_LOCK" ] || STACK_LOCK="$FREENET_STACK_AUTO_LOCK"
    [ ! -e "$STACK_LOCK" ] ||
        { stack_refuse MUTATION_BUSY 'A legacy AUTO VPN mutation lock exists; await read-only reconciliation'; return 1; }

    command -v crontab >/dev/null 2>&1 ||
        { stack_refuse CRON_UNKNOWN 'Entware crontab is unavailable; other runtime writers cannot be verified'; return 1; }
    # "no crontab" with empty stdout is a legitimate empty schedule.
    STACK_CRON="$(crontab -l 2>/dev/null)" || {
        [ -z "$STACK_CRON" ] ||
            { stack_refuse CRON_UNKNOWN 'Unable to inspect cron safely'; return 1; }
    }
    if ! printf '%s\n' "$STACK_CRON" | awk '
        /^# BEGIN FREENET$/ {managed=1; next}
        /^# END FREENET$/ {managed=0; next}
        managed || /^[[:space:]]*#/ || NF < 6 {next}
        /xkeen[[:space:]]+-(restart|start|stop)([[:space:]]|$)/ ||
        /blanc_xkeen_update_outbounds[.]sh/ ||
        /auto_vpn[.]sh/ ||
        /freenet-ui[[:space:]]+(automation-health-watch|settings-v3-endpoint-refresh|settings-v3-subscription)/ ||
        /(^|\/)xray[[:space:]]+run([[:space:]]|$)/ {unsafe++}
        END {exit unsafe > 0 ? 1 : 0}
    '; then
        stack_refuse UNMANAGED_WRITER 'Unmanaged cron controls Xray/XKeen/VPN; update blocked without changing the job'
        return 1
    fi

    STACK_KIND=DECLARED_COMPATIBLE
    return 0
}

# Persist only hashes of protected external router assets, never credentials.
# Offline Xray is permitted. Update must not modify core binaries, init hooks,
# netfilter, subscription, profile, router config or cron.
# Recheck before calling this helper: the mkdir is the atomic cross-process
# fence shared with the legacy provider updater. Keep the fence throughout
# FreeNet asset replacement, UI restart, acceptance and rollback. The Xray
# binary and its running process are never touched by this update.
acquire_update_stack_lock() {
    STACK_MUTATION_LOCK='/tmp/blanc_xkeen_update.lock'
    [ -z "$FREENET_STACK_MUTATION_LOCK" ] ||
        STACK_MUTATION_LOCK="$FREENET_STACK_MUTATION_LOCK"
    mkdir "$STACK_MUTATION_LOCK" 2>/dev/null || return 1
    STACK_LOCK_HELD=1
    printf '%s\n' "$" > "$STACK_MUTATION_LOCK/pid" 2>/dev/null || return 1
    return 0
}

snapshot_protected_stack() {
    OUT="$1"
    : > "$OUT" || return 1
    for F in \
        "$ROOT/sbin/xray" "$ROOT/sbin/xkeen" "$ROOT/sbin/xkeen-ui" \
        "$ROOT/etc/init.d/S99xkeen" "$ROOT/etc/init.d/S99xkeen-ui" \
        "$ROOT/etc/ndm/netfilter.d/proxy.sh" \
        "$ROOT/etc/freenet/freenet.conf" "$ROOT/etc/freenet/vpn_profile_name" \
        "$ROOT/etc/xray/blanc_subscription.url" "$ROOT/etc/xray/blanc_profile_filter.regex"
    do
        if [ -e "$F" ]; then
            [ -f "$F" ] || return 1
            sha256sum "$F" >> "$OUT" || return 1
        else
            printf '%s MISSING\n' "$F" >> "$OUT" || return 1
        fi
    done
    command -v crontab >/dev/null 2>&1 || return 1
    CRON_SNAPSHOT="$(crontab -l 2>/dev/null)" || [ -z "$CRON_SNAPSHOT" ] || return 1
    CRON_SHA="$(printf '%s' "$CRON_SNAPSHOT" | sha256sum | awk '{print $1}')"
    [ -n "$CRON_SHA" ] || return 1
    printf '%s CRONTAB\n' "$CRON_SHA" >> "$OUT"
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

prepare_backup() {
    STAMP="$(date +%Y%m%d-%H%M%S 2>/dev/null)"
    [ -n "$STAMP" ] || STAMP="$$"
    BACKUP_DIR="$ROOT/backups/freenet-web-update-$STAMP"
    mkdir -p "$BACKUP_DIR" || return 1
    chmod 700 "$BACKUP_DIR" || return 1
    I=0
    for NAME in $(asset_list); do
        I=$((I + 1))
        DEST="$(asset_dest "$NAME")" || return 1
        backup_one "$DEST" "asset-$I" || return 1
    done
    snapshot_xray "$BACKUP_DIR/xray-hashes.before" || return 1
    snapshot_protected_stack "$BACKUP_DIR/protected-stack.before" || return 1
}

restore_one() {
    DEST="$1"
    KEY="$2"
    if [ -f "$BACKUP_DIR/$KEY.exists" ] && [ "$(cat "$BACKUP_DIR/$KEY.exists")" = yes ]; then
        mkdir -p "$(dirname "$DEST")" 2>/dev/null || return 1
        cp -p "$BACKUP_DIR/$KEY.before" "$DEST" 2>/dev/null || return 1
    else
        rm -f "$DEST" 2>/dev/null || return 1
    fi
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
    TARGET="$1"
    [ "$FAIL_STAGE" = restart ] && return 1
    if [ "$TEST_MODE" = yes ]; then
        mkdir -p "$ROOT/var/run" || return 1
        printf '%s\n' "$TARGET" > "$ROOT/var/run/freenet-test-version" || return 1
        return 0
    fi
    INIT="$ROOT/etc/init.d/S99freenet-ui"
    [ -x "$INIT" ] || return 1
    "$INIT" stop >/dev/null 2>&1 || true
    killall freenet-ui >/dev/null 2>&1 || true
    "$INIT" start >/dev/null 2>&1 || return 1
    return 0
}

accept_runtime() {
    TARGET="$1"
    [ "$FAIL_STAGE" = accept ] && return 1

    if [ "$TEST_MODE" = yes ]; then
        [ "$(cat "$ROOT/var/run/freenet-test-version" 2>/dev/null)" = "$TARGET" ] || return 1
    else
        PORT="$(ui_port)"
        OK=no
        I=0
        while [ "$I" -lt 25 ]; do
            I=$((I + 1))
            HEALTH="$(curl -fsS --connect-timeout 2 "http://127.0.0.1:$PORT/healthz" 2>/dev/null || true)"
            V="$(curl -fsS --connect-timeout 2 "http://127.0.0.1:$PORT/versionz" 2>/dev/null || true)"
            if [ "$HEALTH" = ok ] && [ "$V" = "$TARGET" ]; then
                OK=yes
                break
            fi
            sleep 1
        done
        [ "$OK" = yes ] || return 1
    fi

    snapshot_xray "$TMP_DIR/xray-hashes.after" || return 1
    cmp "$BACKUP_DIR/xray-hashes.before" "$TMP_DIR/xray-hashes.after" >/dev/null 2>&1 || return 1
    snapshot_protected_stack "$TMP_DIR/protected-stack.after" || return 1
    cmp "$BACKUP_DIR/protected-stack.before" "$TMP_DIR/protected-stack.after" >/dev/null 2>&1 || return 1
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
    restart_ui "$CURRENT_VERSION" || return 1
    if [ "$TEST_MODE" = no ]; then
        PORT="$(ui_port)"
        I=0
        while [ "$I" -lt 20 ]; do
            I=$((I + 1))
            V="$(curl -fsS --connect-timeout 2 "http://127.0.0.1:$PORT/versionz" 2>/dev/null || true)"
            [ "$V" = "$CURRENT_VERSION" ] && break
            sleep 1
        done
        [ "$V" = "$CURRENT_VERSION" ] || return 1
    fi
    snapshot_xray "$TMP_DIR/xray-hashes.rollback" || return 1
    cmp "$BACKUP_DIR/xray-hashes.before" "$TMP_DIR/xray-hashes.rollback" >/dev/null 2>&1 || return 1
    snapshot_protected_stack "$TMP_DIR/protected-stack.rollback" || return 1
    cmp "$BACKUP_DIR/protected-stack.before" "$TMP_DIR/protected-stack.rollback" >/dev/null 2>&1 || return 1
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
    # Malformed release tags must be rejected even when local compatibility
    # is blocked. No malformed target can become a misleading READY=no plan.
    if [ -n "$TARGET_TAG" ]; then
        valid_tag "$TARGET_TAG" || { plan_error 'target release tag is invalid'; return 1; }
    fi
    if [ -n "$LATEST_OVERRIDE" ]; then
        valid_tag "$LATEST_OVERRIDE" || { plan_error 'latest release tag is invalid'; return 1; }
    fi
    for T in curl sha256sum sed awk grep mktemp jq; do
        command -v "$T" >/dev/null 2>&1 || { plan_error "required command missing: $T"; return 1; }
    done
    if ! check_existing_stack_compatibility; then
        say 'SUCCESS=yes'
        say 'READY=no'
        say "CURRENT_VERSION=$CURRENT_VERSION"
        say "TARGET_TAG=$TARGET_TAG"
        say "STACK_COMPATIBILITY=$STACK_KIND"
        say "ERROR=$STACK_ERROR"
        say 'EXPECTED_DELTA=NONE; existing router stack preserved'
        say 'MUTATION=NONE'
        return 0
    fi
    if [ -n "$TARGET_TAG" ]; then
        # Exact target is already selected by the authenticated release catalog.
        # Do not spend another network round-trip asking GitHub which release is latest.
        PLAN_TARGET="$TARGET_TAG"
        LATEST="$LATEST_OVERRIDE"
        if [ -n "$LATEST" ]; then
            valid_tag "$LATEST" || { plan_error 'latest release tag is invalid'; return 1; }
        fi
    else
        LATEST="$(latest_tag)" || { plan_error 'cannot determine latest FreeNet release: official GitHub latest link and REST API unavailable'; return 1; }
        valid_tag "$LATEST" || { plan_error 'latest release tag is invalid'; return 1; }
        PLAN_TARGET="$LATEST"
    fi
    valid_tag "$PLAN_TARGET" || { plan_error 'target release tag is invalid'; return 1; }
    fetch_release_metadata "$PLAN_TARGET" || { plan_error 'target release is not a published stable FreeNet release'; return 1; }
    fetch_manifest "$PLAN_TARGET" || { plan_error 'release manifest is unavailable or incomplete'; return 1; }
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
    say "STACK_COMPATIBILITY=$STACK_KIND"
    say 'STACK_RUNTIME_ACCEPTANCE=NOT_TESTED'
    say "CURRENT_VERSION=$CURRENT_VERSION"
    say "LATEST_VERSION=$LATEST"
    say "TARGET_TAG=$PLAN_TARGET"
    say "UPDATE_AVAILABLE=$AVAILABLE"
    say "DIRECTION=$DIRECTION"
    say "ARCH=$ARCH"
    say 'MANIFEST_VERIFIED=yes'
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
    write_state FAILED "$TARGET_TAG" 'Обновление не началось: проверка не пройдена' "$MSG" NOT_NEEDED "$BACKUP_DIR" || true
    err "$MSG"
    exit 1
}

fail_after_mutation() {
    MSG="$1"
    err "$MSG"
    if rollback_assets; then
        MUTATED=0
        write_state FAILED "$TARGET_TAG" 'Обновление отменено; предыдущее состояние восстановлено' "$MSG" SUCCESS "$BACKUP_DIR" || true
        exit 1
    fi
    KEEP_LOCK=1
    write_state ROLLBACK_FAILED "$TARGET_TAG" 'Откат не подтверждён; дальнейшие mutation запрещены' "$MSG" FAILED_UNKNOWN "$BACKUP_DIR" || true
    exit 2
}

run_apply() {
    detect_current || fail_before_mutation 'current FreeNet version is invalid'
    valid_tag "$TARGET_TAG" || fail_before_mutation 'target release tag is invalid'
    [ "$TARGET_TAG" != "$CURRENT_VERSION" ] || fail_before_mutation 'target FreeNet version is already installed'
    get_arch || fail_before_mutation 'unsupported Entware architecture'
    for T in curl sha256sum sed awk grep cmp mktemp jq; do
        command -v "$T" >/dev/null 2>&1 || fail_before_mutation "required command missing: $T"
    done
    check_existing_stack_compatibility || fail_before_mutation "$STACK_KIND: $STACK_ERROR"
    fetch_release_metadata "$TARGET_TAG" || fail_before_mutation 'target release is not a published stable FreeNet release'

    if ! mkdir "$LOCK_DIR" 2>/dev/null; then
        write_state BUSY "$TARGET_TAG" 'Другая операция обновления уже выполняется' 'update lock is already held' NOT_NEEDED '' || true
        exit 3
    fi
    LOCK_HELD=1
    printf '%s\n' "$" > "$LOCK_DIR/owner.pid" 2>/dev/null || true
    printf '%s\n' "$TARGET_TAG" > "$LOCK_DIR/target" 2>/dev/null || true
    write_state CHECKING "$TARGET_TAG" 'Проверяем выбранный exact release и SHA-256' '' NOT_NEEDED '' || true

    make_tmp || fail_before_mutation 'cannot create staging directory'
    download_assets "$TARGET_TAG" || fail_before_mutation "${LAST_DOWNLOAD_ERROR:-release asset download or SHA-256 verification failed}"
    validate_stage || fail_before_mutation 'staging validation failed'
    check_existing_stack_compatibility || fail_before_mutation "$STACK_KIND: $STACK_ERROR"
    acquire_update_stack_lock || fail_before_mutation 'Xray/provider mutation lock could not be acquired; another writer may be active'
    write_state SNAPSHOT "$TARGET_TAG" 'Создаём snapshot FreeNet-owned файлов' '' NOT_NEEDED '' || true
    prepare_backup || fail_before_mutation 'cannot create pre-update snapshot'

    MUTATED=1
    write_state UPDATING "$TARGET_TAG" 'Применяем проверенные FreeNet assets' '' PENDING "$BACKUP_DIR" || true
    install_assets || fail_after_mutation 'live asset replacement failed'

    write_state RECONNECTING "$TARGET_TAG" 'FreeNet перезапускается; подтверждаем фактическое состояние' '' PENDING "$BACKUP_DIR" || true
    restart_ui "$TARGET_TAG" || fail_after_mutation 'FreeNet UI restart failed'
    accept_runtime "$TARGET_TAG" || fail_after_mutation 'post-update runtime acceptance failed'
    persist_version "$TARGET_TAG" || fail_after_mutation 'cannot persist installed version marker'

    MUTATED=0
    write_state SUCCESS "$TARGET_TAG" 'Выбранная версия FreeNet установлена и проверена' '' NOT_NEEDED "$BACKUP_DIR" || true
    say "RESULT=SUCCESS"
    say "TARGET_VERSION=$TARGET_TAG"
    say "BACKUP_DIR=$BACKUP_DIR"
    say 'ROLLBACK=NOT_NEEDED'
}

case "$MODE" in
    plan) run_plan ;;
    apply)
        [ -n "$TARGET_TAG" ] || { err 'usage: self_update.sh apply <vX.Y.Z>'; exit 2; }
        run_apply
        ;;
    *) err 'usage: self_update.sh [plan [vX.Y.Z]|apply <vX.Y.Z>]'; exit 2 ;;
esac