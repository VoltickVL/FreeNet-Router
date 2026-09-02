#!/bin/sh

# FreeNet P1 Entware bootstrap.
# plan/fetch are read-only with respect to persistent /opt state.
# apply provisions targeted dependencies, installs exact pinned XKeen/Xray/
# XKeen UI on a CLEAN Entware-only router, validates the result and rolls the
# core stack back on any post-apply failure. Existing/partial stacks are never
# rebuilt by this path.

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
MUTATED=0
ROLLBACK_ACTIVE=0
BACKUP_DIR=""
TEST_MODE="${FREENET_BOOTSTRAP_TEST_MODE:-no}"

cleanup_stage() {
    if [ "${FREENET_KEEP_STAGE:-no}" != "yes" ] && [ -n "$STAGE_DIR" ] && [ -d "$STAGE_DIR" ]; then
        rm -rf "$STAGE_DIR" 2>/dev/null || true
    fi
}

opkg_cmd() {
    if [ -x "$ROOT/bin/opkg" ]; then
        "$ROOT/bin/opkg" "$@"
    else
        opkg "$@"
    fi
}

find_pin_file() {
    [ -n "$PIN_FILE" ] && [ -f "$PIN_FILE" ] && return 0

    SELF_DIR="$(CDPATH= cd "$(dirname "$0")" 2>/dev/null && pwd)"
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

    for V in \
        XKEEN_REPO XKEEN_VERSION XKEEN_ASSET XKEEN_SHA256 \
        XRAY_REPO XRAY_VERSION XRAY_ARM64_ASSET XRAY_ARM64_SHA256 \
        XRAY_MIPS32LE_ASSET XRAY_MIPS32LE_SHA256 XRAY_MIPS32_ASSET XRAY_MIPS32_SHA256 \
        XKEEN_UI_REPO XKEEN_UI_VERSION XKEEN_UI_ARM64_ASSET XKEEN_UI_ARM64_SHA256 \
        XKEEN_UI_MIPS32LE_ASSET XKEEN_UI_MIPS32LE_SHA256 XKEEN_UI_MIPS32_ASSET XKEEN_UI_MIPS32_SHA256
    do
        eval "VALUE=\${$V:-}"
        [ -n "$VALUE" ] || { err "missing pin: $V"; exit 2; }
    done
}

get_arch() {
    if [ -z "$ARCH_RAW" ]; then
        ARCH_RAW="$(opkg_cmd print-architecture 2>/dev/null)"
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
        *) ARCH='unknown' ;;
    esac
}

has_xray_configs() {
    [ -d "$ROOT/etc/xray/configs" ] || return 1
    find "$ROOT/etc/xray/configs" -maxdepth 1 -type f -name '*.json' 2>/dev/null | grep -q .
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
    HAS_XKEEN_UI=no; [ -x "$ROOT/sbin/xkeen-ui" ] && HAS_XKEEN_UI=yes
    HAS_CONFIGS=no; has_xray_configs && HAS_CONFIGS=yes

    if [ "$HAS_XKEEN" = yes ] && [ "$HAS_XRAY" = yes ] && [ "$HAS_CONFIGS" = yes ]; then
        MODE='READY_EXISTING_STACK'
    elif [ "$HAS_XKEEN" = no ] && [ "$HAS_XRAY" = no ] && [ "$HAS_XKEEN_UI" = no ] && [ "$HAS_CONFIGS" = no ]; then
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
            say 'DEPENDENCIES=targeted opkg install only; no global upgrade'
            say 'APPLY=core stack only; ISP/DNS/VPN subscription remain setup-layer decisions'
            ;;
        READY_EXISTING_STACK)
            say 'NEXT=preserve existing stack and use FreeNet migration/update path'
            ;;
        NEEDS_REVIEW)
            say 'NEXT=STOP: partial stack/config requires read-only review before mutation'
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

make_stage() {
    if [ -z "$STAGE_DIR" ]; then
        STAGE_DIR="$(mktemp -d /tmp/freenet-bootstrap.XXXXXX 2>/dev/null)"
        [ -n "$STAGE_DIR" ] && [ -d "$STAGE_DIR" ] || return 1
    else
        mkdir -p "$STAGE_DIR" || return 1
    fi
}

fetch_assets() {
    [ "$MODE" = 'ENTWARE_ONLY' ] || {
        err "fetch is allowed only for MODE=ENTWARE_ONLY (got $MODE)"
        return 1
    }
    for T in curl sha256sum sed awk; do
        command -v "$T" >/dev/null 2>&1 || { err "missing tool: $T"; return 1; }
    done
    make_stage || return 1

    XKEEN_URL="https://github.com/$XKEEN_REPO/releases/download/$XKEEN_VERSION/$XKEEN_ASSET"
    XRAY_URL="https://github.com/$XRAY_REPO/releases/download/$XRAY_VERSION/$XRAY_ASSET"
    XKEEN_UI_URL="https://github.com/$XKEEN_UI_REPO/releases/download/$XKEEN_UI_VERSION/$XKEEN_UI_ASSET"

    fetch_one XKeen "$XKEEN_URL" "$XKEEN_SHA256" "$STAGE_DIR/$XKEEN_ASSET" || return 1
    fetch_one Xray "$XRAY_URL" "$XRAY_SHA256" "$STAGE_DIR/$XRAY_ASSET" || return 1
    fetch_one XKeen-UI "$XKEEN_UI_URL" "$XKEEN_UI_SHA256" "$STAGE_DIR/$XKEEN_UI_ASSET" || return 1

    say '[FreeNet Bootstrap] VERIFIED=YES'
}

ensure_dependencies() {
    [ "$MODE" = 'ENTWARE_ONLY' ] || return 1
    say '[FreeNet Bootstrap] Targeted Entware dependencies...'

    opkg_cmd update >/tmp/freenet-opkg-update.$$.log 2>&1 || {
        tail -n 20 /tmp/freenet-opkg-update.$$.log 2>/dev/null || true
        rm -f /tmp/freenet-opkg-update.$$.log 2>/dev/null
        err 'opkg update failed; no core stack mutation started'
        return 1
    }
    rm -f /tmp/freenet-opkg-update.$$.log 2>/dev/null

    # Mirrors the userland dependency set required by the pinned XKeen/Xray
    # bootstrap. Kernel-specific packages are not guessed here; clean-router
    # acceptance will add only confirmed hardware requirements.
    PACKAGES='ca-bundle curl jq libc libssp librt libpthread ip-full iptables ipset coreutils-uname coreutils-nohup unzip'
    opkg_cmd install $PACKAGES >/tmp/freenet-opkg-install.$$.log 2>&1 || {
        tail -n 30 /tmp/freenet-opkg-install.$$.log 2>/dev/null || true
        rm -f /tmp/freenet-opkg-install.$$.log 2>/dev/null
        err 'targeted opkg install failed; core stack was not changed'
        return 1
    }
    rm -f /tmp/freenet-opkg-install.$$.log 2>/dev/null
    say '[FreeNet Bootstrap] Targeted dependencies: OK'
}

backup_path() {
    SRC="$1"
    KEY="$2"
    if [ -e "$SRC" ]; then
        echo yes > "$BACKUP_DIR/$KEY.exists"
        cp -a "$SRC" "$BACKUP_DIR/$KEY.before" || return 1
    else
        echo no > "$BACKUP_DIR/$KEY.exists"
    fi
}

prepare_backup() {
    STAMP="$(date +%Y%m%d-%H%M%S 2>/dev/null)"
    [ -n "$STAMP" ] || STAMP="$$"
    BACKUP_DIR="$ROOT/backups/freenet-bootstrap-$STAMP"
    mkdir -p "$BACKUP_DIR" || return 1

    backup_path "$ROOT/sbin/xkeen" xkeen || return 1
    backup_path "$ROOT/sbin/.xkeen" xkeen-tree || return 1
    backup_path "$ROOT/sbin/xray" xray || return 1
    backup_path "$ROOT/sbin/xkeen-ui" xkeen-ui || return 1
    backup_path "$ROOT/etc/xray" etc-xray || return 1
    backup_path "$ROOT/etc/xkeen" etc-xkeen || return 1
    backup_path "$ROOT/etc/init.d/S05xkeen" S05xkeen || return 1
    backup_path "$ROOT/etc/init.d/S99xkeen-ui" S99xkeen-ui || return 1
}

restore_path() {
    DEST="$1"
    KEY="$2"
    rm -rf "$DEST" 2>/dev/null || return 1
    if [ -f "$BACKUP_DIR/$KEY.exists" ] && [ "$(cat "$BACKUP_DIR/$KEY.exists")" = yes ]; then
        cp -a "$BACKUP_DIR/$KEY.before" "$DEST" 2>/dev/null || return 1
    fi
}

rollback_core() {
    [ "$MUTATED" = 1 ] || return 0
    [ -n "$BACKUP_DIR" ] && [ -d "$BACKUP_DIR" ] || return 1
    ROLLBACK_ACTIVE=1
    say '[FreeNet Bootstrap] ROLLBACK: restoring pre-bootstrap core stack...'

    [ -x "$ROOT/etc/init.d/S99xkeen-ui" ] && "$ROOT/etc/init.d/S99xkeen-ui" stop >/dev/null 2>&1 || true
    [ -x "$ROOT/etc/init.d/S05xkeen" ] && "$ROOT/etc/init.d/S05xkeen" stop >/dev/null 2>&1 || true

    RB=0
    restore_path "$ROOT/sbin/xkeen" xkeen || RB=1
    restore_path "$ROOT/sbin/.xkeen" xkeen-tree || RB=1
    restore_path "$ROOT/sbin/xray" xray || RB=1
    restore_path "$ROOT/sbin/xkeen-ui" xkeen-ui || RB=1
    restore_path "$ROOT/etc/xray" etc-xray || RB=1
    restore_path "$ROOT/etc/xkeen" etc-xkeen || RB=1
    restore_path "$ROOT/etc/init.d/S05xkeen" S05xkeen || RB=1
    restore_path "$ROOT/etc/init.d/S99xkeen-ui" S99xkeen-ui || RB=1

    ROLLBACK_ACTIVE=0
    [ "$RB" -eq 0 ]
}

fail_apply() {
    MESSAGE="$1"
    err "$MESSAGE"
    if [ "$MUTATED" = 1 ] && [ "$ROLLBACK_ACTIVE" = 0 ]; then
        if rollback_core; then
            say '[FreeNet Bootstrap] ROLLBACK: SUCCESS'
            cleanup_stage
            exit 1
        fi
        say '[FreeNet Bootstrap] ROLLBACK ERROR: FAILED/UNKNOWN' >&2
        cleanup_stage
        exit 2
    fi
    cleanup_stage
    exit 1
}

on_signal() {
    if [ "$MUTATED" = 1 ] && [ "$ROLLBACK_ACTIVE" = 0 ]; then
        if rollback_core; then
            say '[FreeNet Bootstrap] ROLLBACK: SUCCESS after signal'
        else
            say '[FreeNet Bootstrap] ROLLBACK ERROR: FAILED/UNKNOWN after signal' >&2
        fi
    fi
    cleanup_stage
    exit 130
}

extract_assets() {
    make_stage || return 1
    XKEEN_UNPACK="$STAGE_DIR/xkeen-unpack"
    XRAY_UNPACK="$STAGE_DIR/xray-unpack"
    rm -rf "$XKEEN_UNPACK" "$XRAY_UNPACK" 2>/dev/null
    mkdir -p "$XKEEN_UNPACK" "$XRAY_UNPACK" || return 1

    tar -xzf "$STAGE_DIR/$XKEEN_ASSET" -C "$XKEEN_UNPACK" || return 1
    unzip -oq "$STAGE_DIR/$XRAY_ASSET" -d "$XRAY_UNPACK" || return 1

    [ -f "$XKEEN_UNPACK/xkeen" ] || return 1
    [ -d "$XKEEN_UNPACK/_xkeen" ] || return 1
    [ -f "$XRAY_UNPACK/xray" ] || return 1
    [ -f "$XRAY_UNPACK/geoip.dat" ] || return 1
    [ -f "$XRAY_UNPACK/geosite.dat" ] || return 1
    [ -f "$STAGE_DIR/$XKEEN_UI_ASSET" ] || return 1
}

stage_core_files() {
    mkdir -p "$ROOT/sbin" "$ROOT/etc/xray/dat" "$ROOT/etc/init.d" || return 1

    cp "$XKEEN_UNPACK/xkeen" "$ROOT/sbin/xkeen" || return 1
    rm -rf "$ROOT/sbin/.xkeen" 2>/dev/null
    cp -a "$XKEEN_UNPACK/_xkeen" "$ROOT/sbin/.xkeen" || return 1
    chmod 755 "$ROOT/sbin/xkeen" || return 1

    cp "$XRAY_UNPACK/xray" "$ROOT/sbin/xray" || return 1
    chmod 755 "$ROOT/sbin/xray" || return 1
    cp "$XRAY_UNPACK/geoip.dat" "$ROOT/etc/xray/dat/geoip.dat" || return 1
    cp "$XRAY_UNPACK/geosite.dat" "$ROOT/etc/xray/dat/geosite.dat" || return 1

    cp "$STAGE_DIR/$XKEEN_UI_ASSET" "$ROOT/sbin/xkeen-ui" || return 1
    chmod 755 "$ROOT/sbin/xkeen-ui" || return 1
}

register_xkeen() {
    if [ "$TEST_MODE" = yes ]; then
        mkdir -p "$ROOT/etc/xray/configs" "$ROOT/etc/init.d" || return 1
        cat > "$ROOT/etc/xray/configs/00-bootstrap-test.json" <<'EOF'
{}
EOF
        cat > "$ROOT/etc/init.d/S05xkeen" <<'EOF'
#!/bin/sh
start_auto="off"
proxy_dns="off"
exit 0
EOF
        chmod 755 "$ROOT/etc/init.d/S05xkeen"
        return 0
    fi

    [ "$ROOT" = /opt ] || { err 'real XKeen registration is allowed only for ROOT=/opt'; return 1; }
    export PATH="/opt/sbin:/opt/bin:/opt/usr/sbin:/opt/usr/bin:/usr/sbin:/usr/bin:/sbin:/bin"

    # Pinned XKeen 2.0 -io asks exactly one autostart choice after local
    # registration. Answer 0 = No, then force safe pre-setup defaults.
    printf '0\n' | /opt/sbin/xkeen -io >"$STAGE_DIR/xkeen-register.log" 2>&1 || {
        tail -n 40 "$STAGE_DIR/xkeen-register.log" 2>/dev/null || true
        return 1
    }
    /opt/sbin/xkeen -auto off >"$STAGE_DIR/xkeen-auto.log" 2>&1 || return 1
    /opt/sbin/xkeen -dns off >"$STAGE_DIR/xkeen-dns.log" 2>&1 || return 1
    return 0
}

validate_xray() {
    if [ "$TEST_MODE" = yes ]; then
        [ -x "$ROOT/sbin/xray" ] && [ -d "$ROOT/etc/xray/configs" ]
        return
    fi

    XRAY_LOCATION_ASSET="$ROOT/etc/xray/dat" \
        "$ROOT/sbin/xray" run -test -confdir "$ROOT/etc/xray/configs" \
        >"$STAGE_DIR/xray-test.log" 2>&1 || {
            tail -n 40 "$STAGE_DIR/xray-test.log" 2>/dev/null || true
            return 1
        }
}

write_xkeen_ui_init() {
    cat > "$ROOT/etc/init.d/S99xkeen-ui" <<'EOF'
#!/bin/sh
ENABLED=yes
PROCS=xkeen-ui
ARGS="-p 1000"
PREARGS=""
DESC="$PROCS"
PATH=/opt/sbin:/opt/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
. /opt/etc/init.d/rc.func
EOF
    chmod 755 "$ROOT/etc/init.d/S99xkeen-ui"
}

start_xkeen_ui() {
    [ "$TEST_MODE" = yes ] && return 0
    [ -f "$ROOT/etc/init.d/rc.func" ] || return 1
    "$ROOT/etc/init.d/S99xkeen-ui" stop >/dev/null 2>&1 || true
    "$ROOT/etc/init.d/S99xkeen-ui" start >/dev/null 2>&1 || return 1
    sleep 2
    pidof xkeen-ui >/dev/null 2>&1 || return 1
    netstat -lntp 2>/dev/null | grep ':1000[[:space:]]' >/dev/null 2>&1 || return 1
}

write_bootstrap_state() {
    mkdir -p "$ROOT/etc/freenet" || return 1
    cat > "$ROOT/etc/freenet/bootstrap-state" <<EOF
BOOTSTRAP_SCHEMA=1
XKEEN_VERSION='$XKEEN_VERSION'
XRAY_VERSION='$XRAY_VERSION'
XKEEN_UI_VERSION='$XKEEN_UI_VERSION'
CORE_READY=yes
XKEEN_AUTOSTART=off
PROXY_DNS=off
EOF
    chmod 600 "$ROOT/etc/freenet/bootstrap-state" 2>/dev/null || true
}

apply_core() {
    [ "$MODE" = ENTWARE_ONLY ] || fail_apply "apply requires MODE=ENTWARE_ONLY; got $MODE"

    ensure_dependencies || fail_apply 'targeted Entware dependency provisioning failed'

    # Dependencies may have supplied curl/unzip. Re-check before fetching.
    for T in curl sha256sum sed awk tar unzip cp find; do
        command -v "$T" >/dev/null 2>&1 || fail_apply "required bootstrap tool missing after dependencies: $T"
    done

    fetch_assets || fail_apply 'pinned upstream asset fetch/verification failed'
    extract_assets || fail_apply 'cannot extract or validate pinned upstream asset layout'
    prepare_backup || fail_apply 'cannot create pre-bootstrap backup'

    MUTATED=1
    stage_core_files || fail_apply 'cannot install pinned core files'
    register_xkeen || fail_apply 'pinned XKeen local registration failed'
    validate_xray || fail_apply 'Xray baseline validation failed'
    write_xkeen_ui_init || fail_apply 'cannot create XKeen UI init script'
    start_xkeen_ui || fail_apply 'XKeen UI runtime acceptance failed'
    write_bootstrap_state || fail_apply 'cannot write bootstrap state'

    MUTATED=0
    say '[FreeNet Bootstrap] CORE_APPLY=SUCCESS'
    say "[FreeNet Bootstrap] BACKUP=$BACKUP_DIR"
    say '[FreeNet Bootstrap] XKeen autostart=off (setup wizard must choose network policy first)'
    say '[FreeNet Bootstrap] proxy_dns=off (setup wizard must choose ISP/DNS first)'
    say '[FreeNet Bootstrap] XKeen UI=:1000 ready'
    say '[FreeNet Bootstrap] ROLLBACK=AVAILABLE'
}

load_pins
classify

case "${1:-plan}" in
    plan|doctor)
        print_plan
        cleanup_stage
        ;;
    fetch)
        print_plan
        fetch_assets || { cleanup_stage; exit 1; }
        say "[FreeNet Bootstrap] STAGE=$STAGE_DIR"
        say '[FreeNet Bootstrap] MUTATION=NONE'
        FREENET_KEEP_STAGE=yes
        export FREENET_KEEP_STAGE
        ;;
    apply)
        print_plan
        trap on_signal 1 2 15
        apply_core
        cleanup_stage
        ;;
    *)
        err 'usage: bootstrap_entware.sh [plan|fetch|apply]'
        cleanup_stage
        exit 2
        ;;
esac
