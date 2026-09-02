#!/bin/sh

# FreeNet P1 Entware bootstrap staging.
# This slice is intentionally non-mutating: it classifies the router and can
# fetch+verify exact pinned upstream assets into a temporary directory.
# Permanent /opt installation is a separate transactional apply phase.

say() { printf '%s\n' "$*"; }
err() { printf '[FreeNet Bootstrap] ERROR: %s\n' "$*" >&2; }

ROOT="${FREENET_ROOT:-/opt}"
PIN_FILE="${FREENET_PIN_FILE:-}"
ARCH_RAW="${FREENET_ARCH_RAW:-}"
STAGE_DIR="${FREENET_STAGE_DIR:-}"
MODE=""
ARCH=""
XRAY_ASSET=""
XRAY_SHA256=""
XKEEN_UI_ASSET=""
XKEEN_UI_SHA256=""
LAST_DOWNLOAD_ERROR=""

cleanup() {
    if [ "${FREENET_KEEP_STAGE:-no}" != "yes" ] && [ -n "$STAGE_DIR" ] && [ -d "$STAGE_DIR" ]; then
        rm -rf "$STAGE_DIR" 2>/dev/null || true
    fi
}
trap cleanup 0 1 2 15

find_pin_file() {
    [ -n "$PIN_FILE" ] && [ -f "$PIN_FILE" ] && return 0

    SELF_DIR="$(CDPATH= cd -- "$(dirname -- "$0")" 2>/dev/null && pwd)"
    for CANDIDATE in \
        "$SELF_DIR/../config/upstream-pins.env" \
        "$SELF_DIR/upstream-pins.env" \
        "$ROOT/etc/freenet/upstream-pins.env"
    do
        if [ -f "$CANDIDATE" ]; then
            PIN_FILE="$CANDIDATE"
            return 0
        fi
    done
    return 1
}

load_pins() {
    find_pin_file || { err 'upstream-pins.env not found'; exit 2; }
    # shellcheck disable=SC1090
    . "$PIN_FILE"

    for V in XKEEN_REPO XKEEN_VERSION XKEEN_ASSET XKEEN_SHA256 XRAY_REPO XRAY_VERSION XKEEN_UI_REPO XKEEN_UI_VERSION; do
        eval "VALUE=\${$V:-}"
        [ -n "$VALUE" ] || { err "missing pin: $V"; exit 2; }
    done
}

get_arch() {
    if [ -z "$ARCH_RAW" ]; then
        if [ -x "$ROOT/bin/opkg" ]; then
            ARCH_RAW="$($ROOT/bin/opkg print-architecture 2>/dev/null)"
        elif command -v opkg >/dev/null 2>&1; then
            ARCH_RAW="$(opkg print-architecture 2>/dev/null)"
        fi
    fi

    case "$ARCH_RAW" in
        *aarch64*)
            ARCH='arm64-v8a'
            XRAY_ASSET="$XRAY_ARM64_ASSET"
            XRAY_SHA256="$XRAY_ARM64_SHA256"
            XKEEN_UI_ASSET="$XKEEN_UI_ARM64_ASSET"
            XKEEN_UI_SHA256="$XKEEN_UI_ARM64_SHA256"
            ;;
        *mipsel*)
            ARCH='mips32le'
            XRAY_ASSET="$XRAY_MIPS32LE_ASSET"
            XRAY_SHA256="$XRAY_MIPS32LE_SHA256"
            XKEEN_UI_ASSET="$XKEEN_UI_MIPS32LE_ASSET"
            XKEEN_UI_SHA256="$XKEEN_UI_MIPS32LE_SHA256"
            ;;
        *mips*)
            ARCH='mips32'
            XRAY_ASSET="$XRAY_MIPS32_ASSET"
            XRAY_SHA256="$XRAY_MIPS32_SHA256"
            XKEEN_UI_ASSET="$XKEEN_UI_MIPS32_ASSET"
            XKEEN_UI_SHA256="$XKEEN_UI_MIPS32_SHA256"
            ;;
        *)
            ARCH='unknown'
            ;;
    esac
}

classify() {
    [ -d "$ROOT" ] || { MODE='NO_ENTWARE'; return; }
    if [ ! -x "$ROOT/bin/opkg" ] && ! command -v opkg >/dev/null 2>&1; then
        MODE='NO_ENTWARE'
        return
    fi

    get_arch
    [ "$ARCH" != 'unknown' ] || { MODE='UNSUPPORTED_ARCH'; return; }

    HAS_XKEEN=no; [ -x "$ROOT/sbin/xkeen" ] && HAS_XKEEN=yes
    HAS_XRAY=no; [ -x "$ROOT/sbin/xray" ] && HAS_XRAY=yes

    if [ "$HAS_XKEEN" = yes ] && [ "$HAS_XRAY" = yes ]; then
        MODE='READY_EXISTING_STACK'
    elif [ "$HAS_XKEEN" = no ] && [ "$HAS_XRAY" = no ]; then
        MODE='ENTWARE_ONLY'
    else
        MODE='NEEDS_REVIEW'
    fi
}

print_plan() {
    say '========== FreeNet Bootstrap Plan =========='
    say "MODE=$MODE"
    say "ARCH=$ARCH"
    case "$MODE" in
        ENTWARE_ONLY)
            say "XKEEN=$XKEEN_VERSION/$XKEEN_ASSET"
            say "XRAY=$XRAY_VERSION/$XRAY_ASSET"
            say "XKEEN_UI=$XKEEN_UI_VERSION/$XKEEN_UI_ASSET"
            say 'NEXT=fetch+verify pinned assets; permanent apply remains separate'
            ;;
        READY_EXISTING_STACK)
            say 'NEXT=preserve existing stack and use FreeNet migration/update path'
            ;;
        NEEDS_REVIEW)
            say 'NEXT=STOP: partial stack requires read-only review before mutation'
            ;;
        NO_ENTWARE)
            say 'NEXT=install Entware/OPKG first'
            ;;
        UNSUPPORTED_ARCH)
            say 'NEXT=STOP: unsupported architecture'
            ;;
    esac
    say 'MUTATION=NONE'
    say '========== END =========='
}

bootstrap_ip() {
    HOST="$1"
    command -v nslookup >/dev/null 2>&1 || return 1
    for DNS in 77.88.8.8 8.8.8.8; do
        IP="$(nslookup "$HOST" "$DNS" 2>/dev/null | awk '
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
    HOST="$(url_host "$URL")"
    [ -n "$HOST" ] || { LAST_DOWNLOAD_ERROR='invalid HTTPS URL'; return 1; }

    IP="$(bootstrap_ip "$HOST")"
    if [ -n "$IP" ]; then
        curl -fLsS --connect-timeout 20 --max-time 180 --resolve "$HOST:443:$IP" "$URL" -o "$OUT" || {
            LAST_DOWNLOAD_ERROR="curl failed for $HOST"
            return 1
        }
    else
        curl -fLsS --connect-timeout 20 --max-time 180 "$URL" -o "$OUT" || {
            LAST_DOWNLOAD_ERROR="curl failed for $HOST"
            return 1
        }
    fi
}

verify_sha() {
    FILE="$1"
    EXPECTED="$2"
    ACTUAL="$(sha256sum "$FILE" | awk '{print $1}')"
    [ "$ACTUAL" = "$EXPECTED" ]
}

fetch_one() {
    LABEL="$1"
    URL="$2"
    SHA="$3"
    OUT="$4"
    printf '[FreeNet Bootstrap] %-10s ' "$LABEL"
    if ! download_url "$URL" "$OUT"; then
        printf 'DOWNLOAD FAIL\n'
        err "$LAST_DOWNLOAD_ERROR"
        return 1
    fi
    if ! verify_sha "$OUT" "$SHA"; then
        printf 'SHA FAIL\n'
        err "$LABEL SHA-256 mismatch"
        return 1
    fi
    printf 'OK\n'
}

fetch_assets() {
    [ "$MODE" = 'ENTWARE_ONLY' ] || {
        err "fetch is allowed only for MODE=ENTWARE_ONLY (got $MODE)"
        return 1
    }
    for T in curl sha256sum sed awk; do
        command -v "$T" >/dev/null 2>&1 || { err "missing tool: $T"; return 1; }
    done

    if [ -z "$STAGE_DIR" ]; then
        STAGE_DIR="$(mktemp -d /tmp/freenet-bootstrap.XXXXXX 2>/dev/null)"
        [ -n "$STAGE_DIR" ] && [ -d "$STAGE_DIR" ] || return 1
    else
        mkdir -p "$STAGE_DIR" || return 1
    fi

    XKEEN_URL="https://github.com/$XKEEN_REPO/releases/download/$XKEEN_VERSION/$XKEEN_ASSET"
    XRAY_URL="https://github.com/$XRAY_REPO/releases/download/$XRAY_VERSION/$XRAY_ASSET"
    XKEEN_UI_URL="https://github.com/$XKEEN_UI_REPO/releases/download/$XKEEN_UI_VERSION/$XKEEN_UI_ASSET"

    fetch_one XKeen "$XKEEN_URL" "$XKEEN_SHA256" "$STAGE_DIR/$XKEEN_ASSET" || return 1
    fetch_one Xray "$XRAY_URL" "$XRAY_SHA256" "$STAGE_DIR/$XRAY_ASSET" || return 1
    fetch_one XKeen-UI "$XKEEN_UI_URL" "$XKEEN_UI_SHA256" "$STAGE_DIR/$XKEEN_UI_ASSET" || return 1

    say "[FreeNet Bootstrap] VERIFIED=YES"
    say "[FreeNet Bootstrap] STAGE=$STAGE_DIR"
    say '[FreeNet Bootstrap] MUTATION=NONE'
    # Caller that explicitly asks to keep the stage owns cleanup.
    FREENET_KEEP_STAGE=yes
    export FREENET_KEEP_STAGE
}

load_pins
classify

case "${1:-plan}" in
    plan|doctor)
        print_plan
        ;;
    fetch)
        print_plan
        fetch_assets
        ;;
    *)
        err 'usage: bootstrap_entware.sh [plan|fetch]'
        exit 2
        ;;
esac
