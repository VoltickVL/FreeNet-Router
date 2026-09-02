#!/bin/sh

# FreeNet ISP/DNS network profile controller.
# plan = только read-only факты и ожидаемая дельта.
# apply = транзакционное применение одного из двух DNS-режимов:
#   firmware — штатный DNS Keenetic по умолчанию;
#   xkeen    — явный Split DNS через XKeen/Xray.

ROOT="${FREENET_ROOT:-/opt}"
CONFIG_FILE="${FREENET_CONFIG_FILE:-$ROOT/etc/freenet/freenet.conf}"
MIGRATE_SCRIPT="${FREENET_MIGRATE_SCRIPT:-$ROOT/lib/freenet/migrate_split_dns.sh}"
CONFIG_DIR="${FREENET_CONFIG_DIR:-$ROOT/etc/xray/configs}"
DNS_FILE="$CONFIG_DIR/02_dns.json"
INBOUND_FILE="$CONFIG_DIR/03_inbounds.json"
OUT_FILE="$CONFIG_DIR/04_outbounds.json"
ROUTING_FILE="$CONFIG_DIR/05_routing.json"
XKEEN_BIN="${FREENET_XKEEN_BIN:-$ROOT/sbin/xkeen}"
XRAY_BIN="${FREENET_XRAY_BIN:-$ROOT/sbin/xray}"
XRAY_ASSET_DIR="${FREENET_XRAY_ASSET_DIR:-$ROOT/etc/xray/dat}"
BACKUP_ROOT="${FREENET_BACKUP_ROOT:-$ROOT/backups}"
RUNTIME_TIMEOUT="${FREENET_XKEEN_RUNTIME_TIMEOUT:-75}"
MODE="${1:-plan}"
TEST_MODE="${FREENET_NETWORK_TEST_MODE:-no}"
TEST_STATE="${FREENET_NETWORK_TEST_STATE:-}"
TMP_DIR=""
BACKUP_DIR=""
PROXY_DNS_INITIAL="unknown"
XRAY_WAS_RUNNING="no"
CONFIG_SNAPSHOT_KIND="none"

say() { printf '%s\n' "$*"; }
err() { printf '[FreeNet Network] ERROR: %s\n' "$*" >&2; }

cleanup() {
    [ -n "$TMP_DIR" ] && rm -rf "$TMP_DIR" 2>/dev/null || true
}
trap cleanup 0 1 2 15

make_tmp() {
    [ -n "$TMP_DIR" ] && [ -d "$TMP_DIR" ] && return 0
    TMP_DIR="$(mktemp -d /tmp/freenet-network.XXXXXX 2>/dev/null)"
    [ -n "$TMP_DIR" ] && [ -d "$TMP_DIR" ] || {
        TMP_DIR="/tmp/freenet-network.$$"
        mkdir -p "$TMP_DIR" || return 1
    }
}

config_value() {
    KEY="$1"
    DEFAULT="$2"
    VALUE="$(sed -n "s/^${KEY}=//p" "$CONFIG_FILE" 2>/dev/null | tail -n 1 | tr -d "'\"\r")"
    [ -n "$VALUE" ] && printf '%s\n' "$VALUE" || printf '%s\n' "$DEFAULT"
}

xkeen_init() {
    for F in "$ROOT/etc/init.d/S99xkeen" "$ROOT/etc/init.d/S05xkeen"; do
        [ -f "$F" ] && { printf '%s\n' "$F"; return 0; }
    done
    return 1
}

test_state_value() {
    KEY="$1"
    DEFAULT="$2"
    if [ "$TEST_MODE" = yes ] && [ -n "$TEST_STATE" ] && [ -f "$TEST_STATE" ]; then
        VALUE="$(sed -n "s/^${KEY}=//p" "$TEST_STATE" 2>/dev/null | tail -n 1)"
        [ -n "$VALUE" ] && { printf '%s\n' "$VALUE"; return 0; }
    fi
    printf '%s\n' "$DEFAULT"
}

test_state_set() {
    KEY="$1"
    VALUE="$2"
    [ "$TEST_MODE" = yes ] && [ -n "$TEST_STATE" ] || return 1
    TMP_STATE="$TEST_STATE.tmp.$$"
    grep -v "^${KEY}=" "$TEST_STATE" > "$TMP_STATE" 2>/dev/null || true
    printf '%s=%s\n' "$KEY" "$VALUE" >> "$TMP_STATE" || return 1
    mv -f "$TMP_STATE" "$TEST_STATE"
}

port53_lines() {
    if [ "$TEST_MODE" = yes ]; then
        case "$(test_state_value PORT53_OWNER none)" in
            ndnproxy) printf '%s\n' 'tcp 0 0 0.0.0.0:53 0.0.0.0:* LISTEN 800/ndnproxy' ;;
            xray) printf '%s\n' 'tcp 0 0 0.0.0.0:53 0.0.0.0:* LISTEN 900/xray' ;;
            other) printf '%s\n' 'tcp 0 0 0.0.0.0:53 0.0.0.0:* LISTEN 700/other' ;;
            *) : ;;
        esac
        return 0
    fi
    netstat -lnptu 2>/dev/null | grep ':53[[:space:]]' || true
}

port53_owner() {
    LINES="$(port53_lines)"
    if printf '%s\n' "$LINES" | grep -q '/xray'; then
        printf '%s\n' xray
    elif printf '%s\n' "$LINES" | grep -q '/ndnproxy'; then
        printf '%s\n' ndnproxy
    elif [ -n "$LINES" ]; then
        printf '%s\n' other
    else
        printf '%s\n' none
    fi
}

xray_pid() {
    if [ "$TEST_MODE" = yes ]; then
        [ "$(test_state_value XRAY_RUNNING no)" = yes ] && printf '%s\n' 4242
        return 0
    fi
    pidof xray 2>/dev/null | awk '{print $1}'
}

xray_gid() {
    PID="$1"
    if [ "$TEST_MODE" = yes ]; then
        test_state_value XRAY_GID unknown
        return 0
    fi
    [ -n "$PID" ] || { printf '%s\n' unknown; return 0; }
    VALUE="$(awk '/^Gid:/ {print $2; exit}' "/proc/$PID/status" 2>/dev/null)"
    [ -n "$VALUE" ] && printf '%s\n' "$VALUE" || printf '%s\n' unknown
}

proxy_dns_state() {
    INIT="$(xkeen_init 2>/dev/null || true)"
    [ -n "$INIT" ] || { printf '%s\n' unknown; return 0; }
    if grep -Eq '^[[:space:]]*proxy_dns="?on"?[[:space:]]*$' "$INIT"; then
        printf '%s\n' on
    elif grep -Eq '^[[:space:]]*proxy_dns="?off"?[[:space:]]*$' "$INIT"; then
        printf '%s\n' off
    else
        printf '%s\n' unknown
    fi
}

set_proxy_dns_state() {
    DESIRED="$1"
    case "$DESIRED" in on|off) : ;; *) return 1 ;; esac

    INIT="$(xkeen_init 2>/dev/null || true)"
    [ -n "$INIT" ] || return 1
    [ "$(proxy_dns_state)" = "$DESIRED" ] && return 0

    STAGED="$INIT.freenet.$$"
    cp -p "$INIT" "$STAGED" || return 1
    sed -i \
        -e "s/^[[:space:]]*proxy_dns=\"[a-z]*\"[[:space:]]*$/proxy_dns=\"$DESIRED\"/" \
        -e "s/^[[:space:]]*proxy_dns=[a-z]*[[:space:]]*$/proxy_dns=\"$DESIRED\"/" \
        "$STAGED" || { rm -f "$STAGED"; return 1; }

    COUNT="$(grep -Ec '^[[:space:]]*proxy_dns="?(on|off)"?[[:space:]]*$' "$STAGED" 2>/dev/null || true)"
    [ "$COUNT" = 1 ] || { rm -f "$STAGED"; return 1; }
    grep -Eq "^[[:space:]]*proxy_dns=\"?$DESIRED\"?[[:space:]]*$" "$STAGED" || { rm -f "$STAGED"; return 1; }

    mv -f "$STAGED" "$INIT" || { rm -f "$STAGED"; return 1; }
    [ "$(proxy_dns_state)" = "$DESIRED" ]
}

run_bounded() {
    LIMIT="$1"
    LOG_FILE="$2"
    shift 2

    "$@" > "$LOG_FILE" 2>&1 &
    CMD_PID=$!
    ELAPSED=0
    while kill -0 "$CMD_PID" 2>/dev/null; do
        if [ "$ELAPSED" -ge "$LIMIT" ]; then
            err "runtime command timeout after ${LIMIT}s: $*"
            kill -TERM "$CMD_PID" 2>/dev/null || true
            sleep 2
            kill -0 "$CMD_PID" 2>/dev/null && kill -KILL "$CMD_PID" 2>/dev/null || true
            wait "$CMD_PID" 2>/dev/null || true
            return 124
        fi
        sleep 1
        ELAPSED=$((ELAPSED + 1))
    done
    wait "$CMD_PID"
}

test_runtime_action() {
    ACTION="$1"
    RESULT="$(test_state_value XKEEN_ACTION_RESULT success)"
    case "$RESULT" in
        timeout-once)
            test_state_set XKEEN_ACTION_RESULT success || return 1
            return 124
            ;;
        fail-once)
            test_state_set XKEEN_ACTION_RESULT success || return 1
            return 1
            ;;
        timeout) return 124 ;;
        fail) return 1 ;;
        success|'') : ;;
        *) return 1 ;;
    esac

    case "$ACTION" in
        start|restart)
            test_state_set XRAY_RUNNING yes || return 1
            test_state_set XRAY_GID 11111 || return 1
            if jq -e 'any(.inbounds[]?; (((.port // "") | tostring)) == "53")' "$INBOUND_FILE" >/dev/null 2>&1; then
                test_state_set PORT53_OWNER xray || return 1
            else
                test_state_set PORT53_OWNER ndnproxy || return 1
            fi
            ;;
        stop)
            test_state_set XRAY_RUNNING no || return 1
            test_state_set PORT53_OWNER ndnproxy || return 1
            ;;
        *) return 1 ;;
    esac
}

xkeen_runtime() {
    ACTION="$1"
    LOG_FILE="$2"

    if [ "$TEST_MODE" = yes ]; then
        test_runtime_action "$ACTION"
        return
    fi

    INIT="$(xkeen_init 2>/dev/null || true)"
    [ -n "$INIT" ] && [ -x "$INIT" ] || return 1
    case "$ACTION" in
        start) run_bounded "$RUNTIME_TIMEOUT" "$LOG_FILE" "$INIT" start on ;;
        restart) run_bounded "$RUNTIME_TIMEOUT" "$LOG_FILE" "$INIT" restart on ;;
        stop) run_bounded "$RUNTIME_TIMEOUT" "$LOG_FILE" "$INIT" stop ;;
        *) return 1 ;;
    esac
}

has_dns_out() {
    [ -f "$OUT_FILE" ] || return 1
    jq -e 'any(.outbounds[]?; .tag == "dns-out" and .protocol == "dns")' "$OUT_FILE" >/dev/null 2>&1
}

has_vless() {
    [ -f "$OUT_FILE" ] || return 1
    jq -e '([.outbounds[]? | select(.tag == "vless-reality")] | length) == 1' "$OUT_FILE" >/dev/null 2>&1
}

dns_query_ok() {
    if [ "$TEST_MODE" = yes ]; then
        [ "$(test_state_value DNS_QUERY_OK yes)" = yes ]
        return
    fi
    LAN_IP="$(ip -4 addr show br0 2>/dev/null | sed -n 's/.*inet \([0-9.]*\)\/.*/\1/p' | head -n 1)"
    [ -n "$LAN_IP" ] || return 1
    nslookup example.com "$LAN_IP" >/tmp/freenet-network-dns-query.$$.log 2>&1
}

runtime_facts() {
    XINIT="$(xkeen_init 2>/dev/null || true)"
    PROXY_DNS="$(proxy_dns_state)"
    PORT53_OWNER="$(port53_owner)"

    XRAY_RUNNING=no
    XRAY_GID=unknown
    XPID="$(xray_pid)"
    if [ -n "$XPID" ]; then
        XRAY_RUNNING=yes
        XRAY_GID="$(xray_gid "$XPID")"
    fi

    DNS_OUT=no; has_dns_out && DNS_OUT=yes
    VLESS=no; has_vless && VLESS=yes

    say "XKEEN_INIT=${XINIT:-missing}"
    say "PROXY_DNS=$PROXY_DNS"
    say "PORT53_OWNER=$PORT53_OWNER"
    say "XRAY_RUNNING=$XRAY_RUNNING"
    say "XRAY_GID=$XRAY_GID"
    say "DNS_OUT=$DNS_OUT"
    say "VLESS_PROFILE=$VLESS"
}

resolve_profile() {
    ISP_ID="$(config_value ISP_ID auto)"
    DNS_MODE="$(config_value DNS_MODE firmware)"

    case "$ISP_ID" in
        auto|vladlink|alliancetelecom|rostelecom|podryad|custom) : ;;
        *)
            SUPPORTED=no
            EFFECTIVE_DNS=unknown
            REASON='неизвестный профиль интернет-провайдера'
            return 0
            ;;
    esac

    case "$DNS_MODE" in
        auto|firmware)
            EFFECTIVE_DNS=firmware
            SUPPORTED=yes
            REASON='штатный DNS Keenetic — безопасный режим по умолчанию'
            ;;
        xkeen)
            EFFECTIVE_DNS=xkeen
            SUPPORTED=yes
            REASON='Split DNS через XKeen/Xray выбран явно'
            ;;
        custom)
            EFFECTIVE_DNS=custom
            SUPPORTED=no
            REASON='свой DNS доступен только в будущем Expert mode'
            ;;
        *)
            EFFECTIVE_DNS=unknown
            SUPPORTED=no
            REASON='неизвестный DNS mode'
            ;;
    esac
}

plan() {
    resolve_profile
    say '========== FreeNet Network Plan =========='
    say "ISP_ID=$ISP_ID"
    say "DNS_MODE=$DNS_MODE"
    say "EFFECTIVE_DNS_MODE=$EFFECTIVE_DNS"
    say "SUPPORTED=$SUPPORTED"
    say "REASON=$REASON"
    runtime_facts

    if [ "$SUPPORTED" = yes ] && [ "$EFFECTIVE_DNS" = firmware ]; then
        DELTA='use Keenetic firmware DNS on router port 53; keep DHCP DNS as router IP; disable XKeen DNS interception'
        [ "$PORT53_OWNER" = xray ] && DELTA="$DELTA; repair legacy Xray :53 ownership by applying the standard-DNS candidate"
        [ "$DNS_OUT" = no ] && DELTA="$DELTA; add inert dns-out object required by the common Xray schema/finalize gate without DNS interception"
        DELTA="$DELTA; preserve VLESS credentials, subscription and non-DNS routing"
        say "EXPECTED_DELTA=$DELTA"
        say 'EXPECTED_NO_DELTA=no VPN credential rewrite; no subscription secret change; no client DNS override outside normal router DHCP'
    elif [ "$SUPPORTED" = yes ] && [ "$EFFECTIVE_DNS" = xkeen ]; then
        DELTA='preserve Keenetic ndnproxy as owner of :53; enable XKeen DNS interception; add/repair Xray built-in split DNS + dns-out + DNS routing rules'
        [ "$XRAY_RUNNING" = no ] && DELTA="$DELTA; start XKeen/Xray before transactional Split DNS migration"
        DELTA="$DELTA; validate DNS and restore original runtime/config if apply fails"
        say "EXPECTED_DELTA=$DELTA"
        say 'EXPECTED_NO_DELTA=no new Xray listener :53; no VLESS credential rewrite; no subscription secret change'
    else
        say 'EXPECTED_DELTA=NONE until a supported DNS mode is selected'
    fi
    say 'MUTATION=NONE'
    say '========== END =========='
}

preflight_common() {
    for C in jq awk grep sed mktemp cp mv sha256sum; do
        command -v "$C" >/dev/null 2>&1 || { err "не найдена обязательная команда: $C"; return 1; }
    done
    if [ "$TEST_MODE" != yes ]; then
        for C in netstat pidof ip nslookup; do
            command -v "$C" >/dev/null 2>&1 || { err "не найдена обязательная команда: $C"; return 1; }
        done
    fi

    case "$RUNTIME_TIMEOUT" in ''|*[!0-9]*) err 'некорректный timeout XKeen runtime'; return 1 ;; esac
    [ "$RUNTIME_TIMEOUT" -gt 0 ] || { err 'timeout XKeen runtime должен быть больше нуля'; return 1; }

    [ -x "$XKEEN_BIN" ] || { err 'XKeen не найден'; return 1; }
    [ -x "$XRAY_BIN" ] || { err 'Xray не найден'; return 1; }
    [ -d "$CONFIG_DIR" ] || { err 'каталог Xray config не найден'; return 1; }
    [ -d "$XRAY_ASSET_DIR" ] || { err 'каталог Xray assets не найден'; return 1; }
    for F in "$DNS_FILE" "$INBOUND_FILE" "$OUT_FILE" "$ROUTING_FILE"; do
        [ -f "$F" ] || { err "не найден обязательный Xray config: $F"; return 1; }
        jq -e . "$F" >/dev/null 2>&1 || { err "невалидный JSON: $F"; return 1; }
    done

    XINIT="$(xkeen_init 2>/dev/null || true)"
    [ -n "$XINIT" ] || { err 'init XKeen не найден'; return 1; }
    [ "$TEST_MODE" = yes ] || [ -x "$XINIT" ] || { err 'init XKeen не исполняемый'; return 1; }
    PROXY_LINES="$(grep -Ec '^[[:space:]]*proxy_dns="?(on|off)"?[[:space:]]*$' "$XINIT" 2>/dev/null || true)"
    [ "$PROXY_LINES" = 1 ] || { err "ожидалась ровно одна настройка proxy_dns, найдено: ${PROXY_LINES:-0}"; return 1; }
    PROXY_DNS_INITIAL="$(proxy_dns_state)"
    case "$PROXY_DNS_INITIAL" in on|off) : ;; *) err 'не удалось определить proxy_dns XKeen'; return 1 ;; esac

    XRAY_WAS_RUNNING=no
    XPID="$(xray_pid)"
    if [ -n "$XPID" ]; then
        XRAY_WAS_RUNNING=yes
        XGID="$(xray_gid "$XPID")"
        [ "$XGID" = 11111 ] || { err "Xray GID is $XGID, expected 11111"; return 1; }
    fi
    return 0
}

wait_for_xray() {
    WANT="$1"
    N=0
    while [ "$N" -lt 12 ]; do
        PID="$(xray_pid)"
        if [ "$WANT" = yes ] && [ -n "$PID" ]; then return 0; fi
        if [ "$WANT" = no ] && [ -z "$PID" ]; then return 0; fi
        [ "$TEST_MODE" = yes ] || sleep 1
        N=$((N + 1))
    done
    return 1
}

snapshot_configs() {
    KIND="$1"
    make_tmp || return 1
    STAMP="$(date +%Y%m%d-%H%M%S 2>/dev/null)"
    [ -n "$STAMP" ] || STAMP="$$"
    BACKUP_DIR="$BACKUP_ROOT/freenet-network-$KIND-$STAMP"
    mkdir -p "$BACKUP_DIR" || return 1
    for NAME in 02_dns.json 03_inbounds.json 04_outbounds.json 05_routing.json; do
        cp -p "$CONFIG_DIR/$NAME" "$BACKUP_DIR/$NAME" || return 1
    done
    XINIT="$(xkeen_init 2>/dev/null || true)"
    [ -n "$XINIT" ] || return 1
    cp -p "$XINIT" "$BACKUP_DIR/xkeen-init.before" || return 1
    sha256sum "$BACKUP_DIR"/*.json "$BACKUP_DIR/xkeen-init.before" > "$BACKUP_DIR/SHA256SUMS.before" 2>/dev/null || true
    CONFIG_SNAPSHOT_KIND="$KIND"
}

restore_configs() {
    [ -n "$BACKUP_DIR" ] && [ -d "$BACKUP_DIR" ] || return 1
    RB=0
    for NAME in 02_dns.json 03_inbounds.json 04_outbounds.json 05_routing.json; do
        cp -p "$BACKUP_DIR/$NAME" "$CONFIG_DIR/$NAME.rollback.$$" 2>/dev/null || RB=1
        [ "$RB" -ne 0 ] || mv -f "$CONFIG_DIR/$NAME.rollback.$$" "$CONFIG_DIR/$NAME" 2>/dev/null || RB=1
    done

    XINIT="$(xkeen_init 2>/dev/null || true)"
    if [ -z "$XINIT" ] || [ ! -f "$BACKUP_DIR/xkeen-init.before" ]; then
        RB=1
    else
        cp -p "$BACKUP_DIR/xkeen-init.before" "$XINIT.rollback.$$" 2>/dev/null || RB=1
        [ "$RB" -ne 0 ] || mv -f "$XINIT.rollback.$$" "$XINIT" 2>/dev/null || RB=1
    fi
    [ "$RB" -eq 0 ]
}

restore_runtime() {
    RB=0
    [ "$(proxy_dns_state)" = "$PROXY_DNS_INITIAL" ] || RB=1

    if [ "$XRAY_WAS_RUNNING" = yes ]; then
        xkeen_runtime restart "/tmp/freenet-network-runtime-rollback.$$.log" || RB=1
        wait_for_xray yes || RB=1
    else
        xkeen_runtime stop "/tmp/freenet-network-runtime-rollback.$$.log" || RB=1
        wait_for_xray no || RB=1
    fi
    [ "$RB" -eq 0 ]
}

rollback_all() {
    RB=0
    [ "$CONFIG_SNAPSHOT_KIND" = none ] || restore_configs || RB=1
    restore_runtime || RB=1
    [ "$RB" -eq 0 ]
}

build_standard_candidate() {
    CANDIDATE="$TMP_DIR/standard"
    mkdir -p "$CANDIDATE" || return 1
    cp -p "$DNS_FILE" "$CANDIDATE/02_dns.json" || return 1

    jq '.inbounds = [.inbounds[]? | select((((.port // "") | tostring)) != "53")]' \
        "$INBOUND_FILE" > "$CANDIDATE/03_inbounds.json" || return 1

    jq '.outbounds = ([.outbounds[]? | select((.tag // "") != "dns-out")] + [{"protocol":"dns","tag":"dns-out"}])' \
        "$OUT_FILE" > "$CANDIDATE/04_outbounds.json" || return 1

    jq '.routing.rules = [
          .routing.rules[]?
          | select((.outboundTag // "") != "dns-out")
          | select(((.inboundTag // []) | index("dns-vless")) == null)
          | select(((.inboundTag // []) | index("dns-direct")) == null)
          | select(((.inboundTag // []) | index("dns-in")) == null)
          | select((((.port // "") | tostring)) != "53")
        ]' "$ROUTING_FILE" > "$CANDIDATE/05_routing.json" || return 1

    VLESS_BEFORE="$(jq -cS '[.outbounds[]? | select(.tag == "vless-reality")]' "$OUT_FILE" | sha256sum | awk '{print $1}')" || return 1
    VLESS_AFTER="$(jq -cS '[.outbounds[]? | select(.tag == "vless-reality")]' "$CANDIDATE/04_outbounds.json" | sha256sum | awk '{print $1}')" || return 1
    [ "$VLESS_BEFORE" = "$VLESS_AFTER" ] || { err 'standard DNS candidate меняет VLESS credentials'; return 1; }

    jq -e 'all(.inbounds[]?; (((.port // "") | tostring)) != "53")' "$CANDIDATE/03_inbounds.json" >/dev/null 2>&1 || return 1
    jq -e '([.outbounds[]? | select(.tag == "dns-out" and .protocol == "dns")] | length) == 1' "$CANDIDATE/04_outbounds.json" >/dev/null 2>&1 || return 1
    jq -e 'all(.routing.rules[]?; (.outboundTag // "") != "dns-out" and (((.port // "") | tostring)) != "53")' "$CANDIDATE/05_routing.json" >/dev/null 2>&1 || return 1

    XRAY_LOCATION_ASSET="$XRAY_ASSET_DIR" "$XRAY_BIN" run -test -confdir "$CANDIDATE" > "$TMP_DIR/xray-standard-candidate.log" 2>&1 || {
        tail -n 40 "$TMP_DIR/xray-standard-candidate.log" >&2 2>/dev/null || true
        err 'candidate штатного DNS не прошёл Xray validation'
        return 1
    }
}

apply_standard_candidate() {
    CANDIDATE="$TMP_DIR/standard"
    for NAME in 03_inbounds.json 04_outbounds.json 05_routing.json; do
        cp -p "$CANDIDATE/$NAME" "$CONFIG_DIR/$NAME.freenet.$$" || return 1
    done
    for NAME in 03_inbounds.json 04_outbounds.json 05_routing.json; do
        mv -f "$CONFIG_DIR/$NAME.freenet.$$" "$CONFIG_DIR/$NAME" || return 1
    done
}

accept_standard() {
    [ "$(proxy_dns_state)" = off ] || return 1
    OWNER="$(port53_owner)"
    [ "$OWNER" = ndnproxy ] || return 1
    [ "$OWNER" != xray ] || return 1
    has_dns_out || return 1
    XRAY_LOCATION_ASSET="$XRAY_ASSET_DIR" "$XRAY_BIN" run -test -confdir "$CONFIG_DIR" >/tmp/freenet-network-xray.$$.log 2>&1 || return 1
    if [ "$XRAY_WAS_RUNNING" = yes ]; then
        PID="$(xray_pid)"
        [ -n "$PID" ] || return 1
        [ "$(xray_gid "$PID")" = 11111 ] || return 1
    fi
    dns_query_ok || return 1
    return 0
}

apply_standard() {
    preflight_common || return 1
    OWNER_BEFORE="$(port53_owner)"
    case "$OWNER_BEFORE" in ndnproxy|xray) : ;; *) err "неизвестный владелец порта 53: $OWNER_BEFORE"; return 1 ;; esac

    snapshot_configs standard || { err 'не удалось создать backup штатного DNS'; return 1; }
    build_standard_candidate || return 1
    apply_standard_candidate || {
        err 'PRIMARY ERROR: не удалось применить candidate штатного DNS'
        if rollback_all; then err 'ROLLBACK ERROR/STATE: rollback success'; return 1; fi
        err 'ROLLBACK ERROR/STATE: FAILED/UNKNOWN'; return 2
    }

    set_proxy_dns_state off || {
        err 'PRIMARY ERROR: не удалось non-interactive установить proxy_dns=off'
        if rollback_all; then err 'ROLLBACK ERROR/STATE: rollback success'; return 1; fi
        err 'ROLLBACK ERROR/STATE: FAILED/UNKNOWN'; return 2
    }

    if [ "$XRAY_WAS_RUNNING" = yes ]; then
        xkeen_runtime restart "/tmp/freenet-network-standard-restart.$$.log" || {
            RC=$?
            err "PRIMARY ERROR: XKeen init restart после выключения DNS завершился ошибкой/timeout (rc=$RC)"
            if rollback_all; then err 'ROLLBACK ERROR/STATE: rollback success'; return 1; fi
            err 'ROLLBACK ERROR/STATE: FAILED/UNKNOWN'; return 2
        }
        wait_for_xray yes || {
            err 'PRIMARY ERROR: Xray не поднялся после штатного DNS apply'
            if rollback_all; then err 'ROLLBACK ERROR/STATE: rollback success'; return 1; fi
            err 'ROLLBACK ERROR/STATE: FAILED/UNKNOWN'; return 2
        }
    else
        xkeen_runtime stop "/tmp/freenet-network-standard-stop.$$.log" || true
    fi

    if ! accept_standard; then
        err 'PRIMARY ERROR: post-apply acceptance штатного DNS не пройден'
        if rollback_all; then err 'ROLLBACK ERROR/STATE: rollback success'; return 1; fi
        err 'ROLLBACK ERROR/STATE: FAILED/UNKNOWN'; return 2
    fi

    say '[FreeNet Network] RESULT=SUCCESS'
    say '[FreeNet Network] EFFECTIVE_DNS_MODE=firmware'
    say '[FreeNet Network] PROXY_DNS=off'
    say '[FreeNet Network] PORT53_OWNER=ndnproxy'
    say '[FreeNet Network] DNS_QUERY=PASS'
    say '[FreeNet Network] ROLLBACK=NOT_NEEDED'
    return 0
}

preflight_split() {
    preflight_common || return 1
    [ -x "$MIGRATE_SCRIPT" ] || { err 'transactional Split DNS migration engine не найден'; return 1; }
    has_vless || { err 'для Split DNS требуется ровно один vless-reality профиль'; return 1; }
    OWNER="$(port53_owner)"
    [ "$OWNER" = ndnproxy ] || {
        [ "$OWNER" = xray ] && err 'Xray уже владеет :53; сначала примените штатный DNS для безопасного repair' || err "Split DNS требует штатного владельца :53, сейчас: $OWNER"
        return 1
    }
    return 0
}

prepare_split_runtime() {
    if [ "$XRAY_WAS_RUNNING" = no ]; then
        xkeen_runtime start "/tmp/freenet-network-xkeen-start.$$.log" || return 1
        wait_for_xray yes || return 1
    fi
    PID="$(xray_pid)"
    [ -n "$PID" ] || return 1
    [ "$(xray_gid "$PID")" = 11111 ] || return 1

    if [ "$PROXY_DNS_INITIAL" = off ]; then
        set_proxy_dns_state on || return 1
        [ "$(proxy_dns_state)" = on ] || return 1
    fi
    [ "$(port53_owner)" = ndnproxy ] || return 1
}

accept_split() {
    has_dns_out || return 1
    XRAY_LOCATION_ASSET="$XRAY_ASSET_DIR" "$XRAY_BIN" run -test -confdir "$CONFIG_DIR" >/tmp/freenet-network-xray.$$.log 2>&1 || return 1
    [ "$(proxy_dns_state)" = on ] || return 1
    PID="$(xray_pid)"
    [ -n "$PID" ] || return 1
    [ "$(xray_gid "$PID")" = 11111 ] || return 1
    OWNER="$(port53_owner)"
    [ "$OWNER" = ndnproxy ] || return 1
    [ "$OWNER" != xray ] || return 1
    dns_query_ok || return 1
}

apply_split() {
    preflight_split || return 1
    snapshot_configs split || { err 'не удалось создать backup Split DNS'; return 1; }

    if ! prepare_split_runtime; then
        err 'PRIMARY ERROR: не удалось подготовить XKeen/Xray для Split DNS'
        if rollback_all; then err 'ROLLBACK ERROR/STATE: rollback success/no live apply'; return 1; fi
        err 'ROLLBACK ERROR/STATE: FAILED/UNKNOWN'; return 2
    fi

    FREENET_XKEEN_INIT="$(xkeen_init 2>/dev/null || true)" \
    FREENET_XKEEN_RUNTIME_TIMEOUT="$RUNTIME_TIMEOUT" \
    "$MIGRATE_SCRIPT"
    RC=$?
    if [ "$RC" -ne 0 ]; then
        err 'PRIMARY ERROR: Split DNS migration failed'
        if rollback_all; then err 'ROLLBACK ERROR/STATE: rollback success/no live apply'; return 1; fi
        err 'ROLLBACK ERROR/STATE: FAILED/UNKNOWN'; return 2
    fi

    if ! accept_split; then
        err 'PRIMARY ERROR: post-apply Split DNS acceptance failed'
        if rollback_all; then err 'ROLLBACK ERROR/STATE: rollback success'; return 1; fi
        err 'ROLLBACK ERROR/STATE: FAILED/UNKNOWN'; return 2
    fi

    say '[FreeNet Network] RESULT=SUCCESS'
    say '[FreeNet Network] EFFECTIVE_DNS_MODE=xkeen'
    say '[FreeNet Network] DNS_OUT=YES'
    say '[FreeNet Network] PROXY_DNS=on'
    say '[FreeNet Network] XRAY_RUNNING=yes'
    say '[FreeNet Network] PORT53_OWNER=ndnproxy-preserved'
    say '[FreeNet Network] DNS_QUERY=PASS'
    say '[FreeNet Network] ROLLBACK=NOT_NEEDED'
    return 0
}

apply_profile() {
    resolve_profile
    [ "$SUPPORTED" = yes ] || {
        plan
        err "$REASON"
        return 1
    }

    case "$EFFECTIVE_DNS" in
        firmware)
            say '[FreeNet Network] APPLY=firmware/standard'
            apply_standard
            ;;
        xkeen)
            say '[FreeNet Network] APPLY=xkeen/split'
            apply_split
            ;;
        *)
            err 'unsupported apply combination'
            return 1
            ;;
    esac
}

case "$MODE" in
    plan) plan ;;
    apply) apply_profile ;;
    *) err 'usage: apply_network_profile.sh [plan|apply]'; exit 2 ;;
esac
