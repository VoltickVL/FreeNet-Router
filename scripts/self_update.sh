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
LOCK_DIR="${FREENET_UPDATE_LOCK_DIR:-/tmp/freenet-self-update.lock}"
TEST_MODE="${FREENET_SELF_UPDATE_TEST_MODE:-no}"
TEST_RELEASE_DIR="${FREENET_TEST_RELEASE_DIR:-}"
LATEST_OVERRIDE="${FREENET_LATEST_TAG:-}"
FAIL_STAGE="${FREENET_TEST_FAIL_STAGE:-}"
ROLLBACK_FAIL="${FREENET_TEST_ROLLBACK_FAIL:-no}"
MODE="${1:-plan}"
TARGET_TAG="${2:-}"
TMP_DIR=""
BACKUP_DIR=""
LOCK_HELD=0
KEEP_LOCK=0
MUTATED=0
LAST_DOWNLOAD_ERROR=""

say() { printf '%s\n' "$*"; }
err() { printf '[FreeNet Web Update] ERROR: %s\n' "$*" >&2; }

cleanup() {
    [ -n "$TMP_DIR" ] && [ -d "$TMP_DIR" ] && rm -rf "$TMP_DIR" 2>/dev/null || true
    if [ "$LOCK_HELD" = 1 ] && [ "$KEEP_LOCK" = 0 ]; then
        rm -rf "$LOCK_DIR" 2>/dev/null || true
        LOCK_HELD=0
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
    printf '%s\n' "$1" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+$'
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

        IP="$(bootstrap_ip "$H")"
        if [ -n "$IP" ]; then
            curl -fsS --connect-timeout 20 --max-time 180 --resolve "$H:443:$IP" -D "$HDR" "$CUR" -o "$BODY" 2>"$ERRFILE"
            RC=$?
        else
            curl -fsS --connect-timeout 20 --max-time 180 -D "$HDR" "$CUR" -o "$BODY" 2>"$ERRFILE"
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
    download_url "$BASE/SHA256SUMS" "$TMP_DIR/SHA256SUMS" || return 1
    manifest_complete
}

verify_asset() {
    NAME="$1"
    FILE="$2"
    EXPECTED="$(manifest_expected "$NAME")"
    [ -n "$EXPECTED" ] || return 1
    ACTUAL="$(sha256sum "$FILE" | awk '{print $1}')"
    [ "$EXPECTED" = "$ACTUAL" ]
}

download_assets() {
    TAG="$1"
    fetch_manifest "$TAG" || return 1
    BASE="https://github.com/$REPO/releases/download/$TAG"
    for NAME in $(asset_list); do
        download_url "$BASE/$NAME" "$TMP_DIR/$NAME" || return 1
        verify_asset "$NAME" "$TMP_DIR/$NAME" || return 1
        MODE_NOW="$(asset_mode "$NAME")"
        chmod "$MODE_NOW" "$TMP_DIR/$NAME" 2>/dev/null || return 1
    done
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
    I=0
    for NAME in $(asset_list); do
        I=$((I + 1))
        DEST="$(asset_dest "$NAME")" || return 1
        backup_one "$DEST" "asset-$I" || return 1
    done
    snapshot_xray "$BACKUP_DIR/xray-hashes.before" || return 1
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

ui_port() {
    P="$(sed -n 's/^UI_PORT=//p' "$ROOT/etc/freenet/freenet.conf" 2>/dev/null | tail -n 1 | tr -d "'\"\r")"
    [ -n "$P" ] && printf '%s\n' "$P" || printf '%s\n' 1001
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
        pidof xray >/dev/null 2>&1 || return 1
        jq -e '([.outbounds[]? | select(.tag == "dns-out")] | length) == 1' "$ROOT/etc/xray/configs/04_outbounds.json" >/dev/null 2>&1 || return 1
    fi

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
    normalize_current || { plan_error 'current FreeNet version is invalid'; return 1; }
    get_arch || { plan_error 'unsupported Entware architecture'; return 1; }
    for T in curl sha256sum sed awk grep mktemp jq; do
        command -v "$T" >/dev/null 2>&1 || { plan_error "required command missing: $T"; return 1; }
    done
    LATEST="$(latest_tag)" || { plan_error 'cannot determine latest FreeNet release'; return 1; }
    valid_tag "$LATEST" || { plan_error 'latest release tag is invalid'; return 1; }
    fetch_manifest "$LATEST" || { plan_error 'release manifest is unavailable or incomplete'; return 1; }

    AVAILABLE=no
    version_gt "$LATEST" "$CURRENT_VERSION" && AVAILABLE=yes

    say 'SUCCESS=yes'
    say 'READY=yes'
    say "CURRENT_VERSION=$CURRENT_VERSION"
    say "LATEST_VERSION=$LATEST"
    say "TARGET_TAG=$LATEST"
    say "UPDATE_AVAILABLE=$AVAILABLE"
    say "ARCH=$ARCH"
    say 'MANIFEST_VERIFIED=yes'
    say 'COMPONENTS=FreeNet UI; manager; VPN/updater helpers; network/provider/finalize helpers; bootstrap helper; self-update helper; upstream pins'
    if [ "$AVAILABLE" = yes ]; then
        say "EXPECTED_DELTA=replace verified FreeNet-owned application assets with exact release $LATEST; restart FreeNet UI; validate target version and unchanged Xray state"
    else
        say 'EXPECTED_DELTA=NONE; installed FreeNet version is current or newer'
    fi
    say 'EXPECTED_NO_DELTA=subscription secret; Xray credentials/config; ISP/DNS/routing state; XKeen/Xray/XKeen UI core; cron'
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
    normalize_current || fail_before_mutation 'current FreeNet version is invalid'
    valid_tag "$TARGET_TAG" || fail_before_mutation 'target release tag is invalid'
    version_gt "$TARGET_TAG" "$CURRENT_VERSION" || fail_before_mutation 'target release is not newer than current FreeNet'
    get_arch || fail_before_mutation 'unsupported Entware architecture'
    for T in curl sha256sum sed awk grep cmp mktemp jq; do
        command -v "$T" >/dev/null 2>&1 || fail_before_mutation "required command missing: $T"
    done

    if ! mkdir "$LOCK_DIR" 2>/dev/null; then
        write_state BUSY "$TARGET_TAG" 'Другая операция обновления уже выполняется' 'update lock is already held' NOT_NEEDED '' || true
        exit 3
    fi
    LOCK_HELD=1
    write_state CHECKING "$TARGET_TAG" 'Проверяем exact release и SHA-256' '' NOT_NEEDED '' || true

    make_tmp || fail_before_mutation 'cannot create staging directory'
    download_assets "$TARGET_TAG" || fail_before_mutation 'release asset download or SHA-256 verification failed'
    validate_stage || fail_before_mutation 'staging validation failed'
    write_state SNAPSHOT "$TARGET_TAG" 'Создаём snapshot FreeNet-owned файлов' '' NOT_NEEDED '' || true
    prepare_backup || fail_before_mutation 'cannot create pre-update snapshot'

    MUTATED=1
    write_state UPDATING "$TARGET_TAG" 'Применяем проверенные FreeNet assets' '' PENDING "$BACKUP_DIR" || true
    install_assets || fail_after_mutation 'live asset replacement failed'

    write_state RECONNECTING "$TARGET_TAG" 'FreeNet перезапускается; подтверждаем фактическое состояние' '' PENDING "$BACKUP_DIR" || true
    restart_ui "$TARGET_TAG" || fail_after_mutation 'FreeNet UI restart failed'
    accept_runtime "$TARGET_TAG" || fail_after_mutation 'post-update runtime acceptance failed'

    MUTATED=0
    write_state SUCCESS "$TARGET_TAG" 'FreeNet успешно обновлён и проверен' '' NOT_NEEDED "$BACKUP_DIR" || true
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
    *) err 'usage: self_update.sh [plan|apply <vX.Y.Z>]'; exit 2 ;;
esac
