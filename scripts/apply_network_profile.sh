#!/bin/sh

# FreeNet ISP/DNS network profile controller.
# plan = read-only runtime facts + expected delta.
# apply = only a verified preset may call the transactional Split-DNS engine.

ROOT="${FREENET_ROOT:-/opt}"
CONFIG_FILE="${FREENET_CONFIG_FILE:-$ROOT/etc/freenet/freenet.conf}"
MIGRATE_SCRIPT="${FREENET_MIGRATE_SCRIPT:-$ROOT/lib/freenet/migrate_split_dns.sh}"
CONFIG_DIR="${FREENET_CONFIG_DIR:-$ROOT/etc/xray/configs}"
OUT_FILE="$CONFIG_DIR/04_outbounds.json"
XKEEN_BIN="${FREENET_XKEEN_BIN:-$ROOT/sbin/xkeen}"
XRAY_BIN="${FREENET_XRAY_BIN:-$ROOT/sbin/xray}"
XRAY_ASSET_DIR="${FREENET_XRAY_ASSET_DIR:-$ROOT/etc/xray/dat}"
MODE="${1:-plan}"
TEST_MODE="${FREENET_NETWORK_TEST_MODE:-no}"
TEST_STATE="${FREENET_NETWORK_TEST_STATE:-}"

say() { printf '%s\n' "$*"; }
err() { printf '[FreeNet Network] ERROR: %s\n' "$*" >&2; }

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
    netstat -lnp 2>/dev/null | grep ':53[[:space:]]' || true
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

has_dns_out() {
    [ -f "$OUT_FILE" ] || return 1
    jq -e 'any(.outbounds[]?; .tag == "dns-out" and .protocol == "dns")' "$OUT_FILE" >/dev/null 2>&1
}

has_vless() {
    [ -f "$OUT_FILE" ] || return 1
    jq -e '([.outbounds[]? | select(.tag == "vless-reality")] | length) == 1' "$OUT_FILE" >/dev/null 2>&1
}

runtime_facts() {
    XINIT="$(xkeen_init 2>/dev/null || true)"
    PROXY_DNS="$(proxy_dns_state)"

    PORT53_OWNER=none
    PORT53="$(port53_lines)"
    if printf '%s\n' "$PORT53" | grep -q '/ndnproxy'; then
        PORT53_OWNER=ndnproxy
    elif printf '%s\n' "$PORT53" | grep -q '/xray'; then
        PORT53_OWNER=xray
    elif [ -n "$PORT53" ]; then
        PORT53_OWNER=other
    fi

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
    DNS_MODE="$(config_value DNS_MODE auto)"
    EFFECTIVE_DNS="$DNS_MODE"

    case "$ISP_ID" in
        rostelecom)
            [ "$DNS_MODE" = auto ] && EFFECTIVE_DNS=firmware
            if [ "$EFFECTIVE_DNS" != firmware ]; then
                SUPPORTED=no
                REASON='Ростелеком preset сейчас верифицирован только для firmware ndnproxy + XKeen proxy_dns topology'
            else
                SUPPORTED=yes
                REASON='verified WORK preset + controlled clean-router activation'
            fi
            ;;
        podryad)
            [ "$DNS_MODE" = auto ] && EFFECTIVE_DNS=firmware
            SUPPORTED=no
            REASON='Подряд имеет отдельный preset ID; отдельный runtime acceptance ещё не выполнен'
            ;;
        vladlink)
            [ "$DNS_MODE" = auto ] && EFFECTIVE_DNS=xkeen
            SUPPORTED=no
            REASON='Владлинк имеет отдельный preset ID; clean-room HOME DNS acceptance ещё не выполнен'
            ;;
        alliancetelecom)
            [ "$DNS_MODE" = auto ] && EFFECTIVE_DNS=xkeen
            SUPPORTED=no
            REASON='АльянсТелеком имеет отдельный preset ID; отдельный clean-room acceptance ещё не выполнен'
            ;;
        auto)
            SUPPORTED=no
            REASON='Auto не выполняет сетевую mutation без выбранного/подтверждённого ISP preset'
            ;;
        custom)
            SUPPORTED=no
            REASON='Custom DNS apply доступен только в будущем Expert mode'
            ;;
        *)
            SUPPORTED=no
            REASON='unknown ISP ID'
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

    if [ "$ISP_ID" = rostelecom ] && [ "$SUPPORTED" = yes ]; then
        DELTA='preserve firmware ndnproxy :53; preserve existing VLESS/non-DNS routing; add/repair Xray built-in split DNS + dns-out + DNS routing rules'
        [ "$PROXY_DNS" = off ] && DELTA="$DELTA; enable XKeen DNS interception via xkeen -dns on"
        [ "$XRAY_RUNNING" = no ] && DELTA="$DELTA; start XKeen/Xray before transactional DNS migration"
        DELTA="$DELTA; restart/validate; restore original proxy_dns/run-state if migration fails"
        say "EXPECTED_DELTA=$DELTA"
        say 'EXPECTED_NO_DELTA=no new Xray listener :53; no VLESS credential rewrite; no subscription secret change'
    else
        say 'EXPECTED_DELTA=NONE until preset runtime acceptance'
    fi
    say 'MUTATION=NONE'
    say '========== END =========='
}

preflight_rostelecom() {
    for C in jq awk grep sed; do
        command -v "$C" >/dev/null 2>&1 || { err "missing command: $C"; return 1; }
    done
    if [ "$TEST_MODE" != yes ]; then
        for C in netstat pidof; do
            command -v "$C" >/dev/null 2>&1 || { err "missing command: $C"; return 1; }
        done
    fi

    [ -x "$XKEEN_BIN" ] || { err 'XKeen missing'; return 1; }
    [ -x "$XRAY_BIN" ] || { err 'Xray missing'; return 1; }
    [ -x "$MIGRATE_SCRIPT" ] || { err 'transactional DNS migration engine missing'; return 1; }
    [ -d "$CONFIG_DIR" ] || { err 'Xray config directory missing'; return 1; }
    [ -d "$XRAY_ASSET_DIR" ] || { err 'Xray asset directory missing'; return 1; }
    has_vless || { err 'exactly one existing vless-reality profile is required before Rostelecom DNS apply'; return 1; }

    XINIT="$(xkeen_init 2>/dev/null || true)"
    [ -n "$XINIT" ] || { err 'XKeen init script missing'; return 1; }
    PROXY_DNS_INITIAL="$(proxy_dns_state)"
    case "$PROXY_DNS_INITIAL" in
        on|off) : ;;
        *) err 'cannot determine XKeen proxy_dns state'; return 1 ;;
    esac

    PORT53="$(port53_lines)"
    printf '%s\n' "$PORT53" | grep -q '/ndnproxy' || {
        err 'Rostelecom preset requires confirmed firmware ndnproxy owner on :53'
        return 1
    }
    if printf '%s\n' "$PORT53" | grep -q '/xray'; then
        err 'unexpected Xray listener already owns :53'
        return 1
    fi

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
        if [ "$WANT" = yes ] && [ -n "$PID" ]; then
            return 0
        fi
        if [ "$WANT" = no ] && [ -z "$PID" ]; then
            return 0
        fi
        [ "$TEST_MODE" = yes ] || sleep 1
        N=$((N + 1))
    done
    return 1
}

restore_rostelecom_runtime() {
    RB_OK=1

    if [ "$PROXY_DNS_INITIAL" = off ]; then
        "$XKEEN_BIN" -dns off >/tmp/freenet-network-dns-rollback.$$.log 2>&1 || RB_OK=0
        [ "$(proxy_dns_state)" = off ] || RB_OK=0
    fi

    if [ "$XRAY_WAS_RUNNING" = yes ]; then
        "$XKEEN_BIN" -restart >/tmp/freenet-network-runtime-rollback.$$.log 2>&1 || RB_OK=0
        wait_for_xray yes || RB_OK=0
        PID="$(xray_pid)"
        [ -n "$PID" ] || RB_OK=0
        [ "$(xray_gid "$PID")" = 11111 ] || RB_OK=0
    else
        "$XKEEN_BIN" -stop >/tmp/freenet-network-runtime-rollback.$$.log 2>&1 || RB_OK=0
        wait_for_xray no || RB_OK=0
    fi

    [ "$RB_OK" = 1 ]
}

fail_before_migration() {
    PRIMARY="$1"
    err "PRIMARY ERROR: $PRIMARY"
    if restore_rostelecom_runtime; then
        err 'ROLLBACK ERROR/STATE: rollback success/no live apply'
        return 1
    fi
    err 'ROLLBACK ERROR/STATE: FAILED/UNKNOWN'
    return 2
}

prepare_rostelecom_runtime() {
    if [ "$XRAY_WAS_RUNNING" = no ]; then
        "$XKEEN_BIN" -start >/tmp/freenet-network-xkeen-start.$$.log 2>&1 || return 1
        wait_for_xray yes || return 1
    fi

    PID="$(xray_pid)"
    [ -n "$PID" ] || return 1
    [ "$(xray_gid "$PID")" = 11111 ] || return 1

    if [ "$PROXY_DNS_INITIAL" = off ]; then
        "$XKEEN_BIN" -dns on >/tmp/freenet-network-dns-enable.$$.log 2>&1 || return 1
        [ "$(proxy_dns_state)" = on ] || return 1
    fi
    return 0
}

accept_rostelecom_success() {
    has_dns_out || return 1
    XRAY_LOCATION_ASSET="$XRAY_ASSET_DIR" "$XRAY_BIN" run -test -confdir "$CONFIG_DIR" >/tmp/freenet-network-xray.$$.log 2>&1 || return 1
    [ "$(proxy_dns_state)" = on ] || return 1
    PID="$(xray_pid)"
    [ -n "$PID" ] || return 1
    [ "$(xray_gid "$PID")" = 11111 ] || return 1
    PORT53="$(port53_lines)"
    printf '%s\n' "$PORT53" | grep -q '/ndnproxy' || return 1
    printf '%s\n' "$PORT53" | grep -q '/xray' && return 1
    return 0
}

apply_profile() {
    resolve_profile
    [ "$SUPPORTED" = yes ] || {
        plan
        err "$REASON"
        return 1
    }

    case "$ISP_ID:$EFFECTIVE_DNS" in
        rostelecom:firmware)
            preflight_rostelecom || return 1
            say '[FreeNet Network] APPLY=rostelecom/firmware'
            say "[FreeNet Network] INITIAL_PROXY_DNS=$PROXY_DNS_INITIAL"
            say "[FreeNet Network] INITIAL_XRAY_RUNNING=$XRAY_WAS_RUNNING"

            if ! prepare_rostelecom_runtime; then
                fail_before_migration 'cannot prepare XKeen/Xray for transactional DNS migration'
                return $?
            fi

            "$MIGRATE_SCRIPT"
            RC=$?
            case "$RC" in
                0)
                    if ! accept_rostelecom_success; then
                        err 'PRIMARY ERROR: post-apply Rostelecom runtime acceptance failed'
                        err 'ROLLBACK ERROR/STATE: FAILED/UNKNOWN; migration reported success, stop before any retry'
                        return 2
                    fi
                    rm -f /tmp/freenet-network-xray.$$.log 2>/dev/null || true
                    say '[FreeNet Network] RESULT=SUCCESS'
                    say '[FreeNet Network] DNS_OUT=YES'
                    say '[FreeNet Network] PROXY_DNS=on'
                    say '[FreeNet Network] XRAY_RUNNING=yes'
                    say '[FreeNet Network] PORT53_OWNER=ndnproxy-preserved'
                    return 0
                    ;;
                2)
                    err 'ROLLBACK ERROR/STATE: FAILED/UNKNOWN from DNS migration engine; no further mutation performed'
                    return 2
                    ;;
                *)
                    err 'PRIMARY ERROR: DNS migration failed'
                    if restore_rostelecom_runtime; then
                        err 'ROLLBACK ERROR/STATE: rollback success/no live apply'
                        return 1
                    fi
                    err 'ROLLBACK ERROR/STATE: FAILED/UNKNOWN'
                    return 2
                    ;;
            esac
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
