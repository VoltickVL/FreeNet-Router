#!/bin/sh
set -u

ROOT="${FREENET_ROOT:-/opt}"
CONFIG_DIR="${FREENET_XRAY_CONFIG_DIR:-${FREENET_CONFIG_DIR:-$ROOT/etc/xray/configs}}"
DNS_FILE="$CONFIG_DIR/02_dns.json"
XRAY_BIN="${FREENET_XRAY_BIN:-$ROOT/sbin/xray}"
XRAY_ASSET_DIR="${FREENET_XRAY_ASSET_DIR:-$ROOT/etc/xray/dat}"
BACKUP_ROOT="${FREENET_SETTINGS_DNS_BACKUP_ROOT:-$ROOT/backups}"
RUNTIME_TIMEOUT="${FREENET_XKEEN_RUNTIME_TIMEOUT:-75}"
DNS_READY_TIMEOUT="${FREENET_SETTINGS_DNS_READY_TIMEOUT:-30}"
DNS_READY_INTERVAL="${FREENET_SETTINGS_DNS_READY_INTERVAL:-2}"
MODE="${1:-plan}"
DIRECT_PROVIDER="${2:-yandex-doh}"
VPN_PROVIDER="${3:-google-doh}"
TEST_MODE="${FREENET_SETTINGS_DNS_TEST_MODE:-no}"
TMP_DIR=""
BACKUP_FILE=""

say() { printf '%s\n' "$*"; }
err() { printf '[FreeNet Settings DNS] ERROR: %s\n' "$*" >&2; }
fail_no_apply() {
    err "PRIMARY ERROR: $*"
    err 'ROLLBACK ERROR/STATE: no live apply'
    return 1
}

cleanup() {
    [ -n "$TMP_DIR" ] && rm -rf "$TMP_DIR" 2>/dev/null || true
}
trap cleanup 0 1 2 15

provider_endpoint() {
    case "$1" in
        yandex-doh) printf '%s\n' 'https://dns.yandex.ru/dns-query' ;;
        google-doh) printf '%s\n' 'https://dns.google/dns-query' ;;
        *) return 1 ;;
    esac
}

xkeen_init() {
    for f in "$ROOT/etc/init.d/S99xkeen" "$ROOT/etc/init.d/S05xkeen"; do
        [ -f "$f" ] && { printf '%s\n' "$f"; return 0; }
    done
    return 1
}

make_tmp() {
    [ -n "$TMP_DIR" ] && [ -d "$TMP_DIR" ] && return 0
    TMP_DIR="$(mktemp -d /tmp/freenet-settings-dns.XXXXXX 2>/dev/null)"
    [ -n "$TMP_DIR" ] && [ -d "$TMP_DIR" ] && return 0
    TMP_DIR="/tmp/freenet-settings-dns.$$"
    mkdir -p "$TMP_DIR"
}

run_bounded() {
    limit="$1"; log="$2"; shift 2
    "$@" >"$log" 2>&1 &
    pid=$!; elapsed=0
    while kill -0 "$pid" 2>/dev/null; do
        if [ "$elapsed" -ge "$limit" ]; then
            kill -TERM "$pid" 2>/dev/null || true
            sleep 2
            kill -0 "$pid" 2>/dev/null && kill -KILL "$pid" 2>/dev/null || true
            wait "$pid" 2>/dev/null || true
            return 124
        fi
        sleep 1
        elapsed=$((elapsed + 1))
    done
    wait "$pid"
}

restart_xkeen() {
    if [ "$TEST_MODE" = yes ]; then
        [ "${FREENET_SETTINGS_DNS_TEST_RESTART:-success}" = success ]
        return
    fi
    init="$(xkeen_init 2>/dev/null || true)"
    [ -n "$init" ] && [ -x "$init" ] || return 1
    run_bounded "$RUNTIME_TIMEOUT" "/tmp/freenet-settings-dns-restart.$$.log" "$init" restart on
}

test_query_once() {
    mode="${FREENET_SETTINGS_DNS_TEST_QUERY:-success}"
    case "$mode" in
        success) return 0 ;;
        fail) return 1 ;;
        delayed)
            state="${FREENET_SETTINGS_DNS_TEST_QUERY_STATE:-}"
            [ -n "$state" ] || return 1
            count=0
            if [ -f "$state" ]; then
                IFS= read -r count <"$state" || count=0
            fi
            case "$count" in ''|*[!0-9]*) count=0 ;; esac
            count=$((count + 1))
            printf '%s\n' "$count" >"$state" || return 1
            [ "$count" -ge "${FREENET_SETTINGS_DNS_TEST_QUERY_SUCCEED_AFTER:-2}" ]
            return
            ;;
        *) return 1 ;;
    esac
}

dns_query_once() {
    if [ "$TEST_MODE" = yes ]; then
        test_query_once
        return
    fi
    nslookup example.com 127.0.0.1 >/tmp/freenet-settings-dns-query.$$.log 2>&1
}

xray_runtime_ok() {
    if [ "$TEST_MODE" = yes ]; then
        [ "${FREENET_SETTINGS_DNS_TEST_RUNTIME:-success}" = success ]
        return
    fi
    pidof xray >/dev/null 2>&1 || return 1
    netstat -lnptu 2>/dev/null | grep ':53[[:space:]]' | grep -q '/xray'
}

dns_runtime_ready() {
    elapsed=0
    while :; do
        if xray_runtime_ok && dns_query_once; then
            return 0
        fi
        [ "$elapsed" -ge "$DNS_READY_TIMEOUT" ] && return 1
        sleep "$DNS_READY_INTERVAL"
        elapsed=$((elapsed + DNS_READY_INTERVAL))
    done
}

pair_value() {
    tag="$1"
    values="$(jq -r --arg tag "$tag" '[.dns.servers[]? | select(.tag==$tag) | .address] | unique | .[]?' "$DNS_FILE" 2>/dev/null)" || return 1
    count="$(printf '%s\n' "$values" | sed '/^$/d' | wc -l | tr -d ' ')"
    [ "$count" = 1 ] || return 1
    printf '%s\n' "$values"
}

runtime_pair_state() {
    direct="$(pair_value dns-direct 2>/dev/null || true)"
    vpn="$(pair_value dns-vless 2>/dev/null || true)"
    case "$direct|$vpn" in
        'https://dns.yandex.ru/dns-query|https://dns.google/dns-query'|'https://dns.yandex.ru/dns-query|https://dns.yandex.ru/dns-query'|'https://dns.google/dns-query|https://dns.google/dns-query'|'https://dns.google/dns-query|https://dns.yandex.ru/dns-query') printf '%s\n' accepted ;;
        '77.88.8.8|https://8.8.8.8/dns-query') printf '%s\n' legacy ;;
        *) printf '%s\n' unknown ;;
    esac
}

preflight() {
    for cmd in jq mktemp cp mv sha256sum grep sed wc; do
        command -v "$cmd" >/dev/null 2>&1 || { err "не найдена обязательная команда: $cmd"; return 1; }
    done
    if [ "$TEST_MODE" != yes ]; then
        for cmd in pidof netstat nslookup; do command -v "$cmd" >/dev/null 2>&1 || { err "не найдена обязательная команда: $cmd"; return 1; }; done
    fi
    provider_endpoint "$DIRECT_PROVIDER" >/dev/null 2>&1 || { err 'неподдерживаемый DIRECT DNS provider'; return 1; }
    provider_endpoint "$VPN_PROVIDER" >/dev/null 2>&1 || { err 'неподдерживаемый VPN DNS provider'; return 1; }
    [ -f "$DNS_FILE" ] || { err 'не найден 02_dns.json'; return 1; }
    jq -e . "$DNS_FILE" >/dev/null 2>&1 || { err '02_dns.json не является валидным managed JSON'; return 1; }
    [ "$(jq -r '[.dns.servers[]? | select(.tag=="dns-direct")] | length' "$DNS_FILE")" -ge 1 ] || { err 'dns-direct resolver отсутствует'; return 1; }
    [ "$(jq -r '[.dns.servers[]? | select(.tag=="dns-vless")] | length' "$DNS_FILE")" -ge 1 ] || { err 'dns-vless resolver отсутствует'; return 1; }
    state="$(runtime_pair_state)"
    [ "$state" != unknown ] || { err 'активная Split DNS resolver-схема неизвестна; STOP'; return 1; }
    [ -x "$XRAY_BIN" ] || { err 'Xray не найден'; return 1; }
    [ -d "$XRAY_ASSET_DIR" ] || { err 'Xray assets не найдены'; return 1; }
    if [ "$TEST_MODE" != yes ]; then
        init="$(xkeen_init 2>/dev/null || true)"
        [ -n "$init" ] && [ -x "$init" ] || { err 'init XKeen не найден'; return 1; }
        xray_runtime_ok || { err 'Xray не подтверждён владельцем DNS runtime'; return 1; }
    fi
}

build_candidate() {
    make_tmp || return 1
    candidate="$TMP_DIR/02_dns.json"
    direct="$(provider_endpoint "$DIRECT_PROVIDER")" || return 1
    vpn="$(provider_endpoint "$VPN_PROVIDER")" || return 1
    jq --arg direct "$direct" --arg vpn "$vpn" '
      .dns.servers |= map(
        if .tag == "dns-direct" then .address=$direct | del(.port)
        elif .tag == "dns-vless" then .address=$vpn | del(.port)
        else . end
      )
    ' "$DNS_FILE" >"$candidate" || return 1
    jq -e --arg direct "$direct" --arg vpn "$vpn" '
      ([.dns.servers[]? | select(.tag=="dns-direct" and .address==$direct)] | length) >= 1 and
      ([.dns.servers[]? | select(.tag=="dns-vless" and .address==$vpn)] | length) >= 1
    ' "$candidate" >/dev/null 2>&1 || return 1

    if [ "$TEST_MODE" != yes ]; then
        candidate_dir="$TMP_DIR/conf"
        mkdir -p "$candidate_dir" || return 1
        for f in "$CONFIG_DIR"/*.json; do [ -f "$f" ] && cp -p "$f" "$candidate_dir/" || return 1; done
        cp -p "$candidate" "$candidate_dir/02_dns.json" || return 1
        XRAY_LOCATION_ASSET="$XRAY_ASSET_DIR" "$XRAY_BIN" run -test -confdir "$candidate_dir" >"$TMP_DIR/xray-test.log" 2>&1 || return 1
    fi
}

verify_target() {
    direct="$(provider_endpoint "$DIRECT_PROVIDER")" || return 1
    vpn="$(provider_endpoint "$VPN_PROVIDER")" || return 1
    [ "$(pair_value dns-direct 2>/dev/null || true)" = "$direct" ] || return 1
    [ "$(pair_value dns-vless 2>/dev/null || true)" = "$vpn" ] || return 1
    dns_runtime_ready || return 1
}

snapshot() {
    stamp="$(date +%Y%m%d-%H%M%S 2>/dev/null || printf '%s' $$)"
    mkdir -p "$BACKUP_ROOT" || return 1
    BACKUP_FILE="$BACKUP_ROOT/freenet-settings-dns-$stamp-$$.json"
    cp -p "$DNS_FILE" "$BACKUP_FILE" || return 1
}

rollback() {
    [ -n "$BACKUP_FILE" ] && [ -f "$BACKUP_FILE" ] || return 1
    before="$(sha256sum "$BACKUP_FILE" | awk '{print $1}')"
    cp -p "$BACKUP_FILE" "$DNS_FILE.rollback.$$" || return 1
    mv -f "$DNS_FILE.rollback.$$" "$DNS_FILE" || return 1
    restart_xkeen || return 1
    after="$(sha256sum "$DNS_FILE" | awk '{print $1}')"
    [ "$before" = "$after" ] || return 1
    dns_runtime_ready || return 1
}

plan() {
    preflight || { fail_no_apply 'resolver preflight failed'; return 1; }
    build_candidate || { fail_no_apply 'resolver candidate validation failed'; return 1; }
    say 'RESULT=PLAN_OK'
    say "CURRENT_PAIR_STATE=$(runtime_pair_state)"
    say "DIRECT_PROVIDER=$DIRECT_PROVIDER"
    say "VPN_PROVIDER=$VPN_PROVIDER"
    say 'MUTATION=NONE'
}

apply() {
    preflight || { fail_no_apply 'resolver preflight failed'; return 1; }
    build_candidate || { fail_no_apply 'resolver candidate validation failed'; return 1; }
    current_hash="$(sha256sum "$DNS_FILE" | awk '{print $1}')"
    candidate_hash="$(sha256sum "$TMP_DIR/02_dns.json" | awk '{print $1}')"
    if [ "$current_hash" = "$candidate_hash" ]; then
        verify_target || { fail_no_apply 'целевые resolver-ы уже записаны, но runtime acceptance не прошёл'; return 1; }
        say 'RESULT=SUCCESS'
        say 'APPLIED=no'
        say 'ROLLBACK=NOT_NEEDED'
        return 0
    fi
    snapshot || { fail_no_apply 'не удалось создать snapshot 02_dns.json'; return 1; }
    cp -p "$TMP_DIR/02_dns.json" "$DNS_FILE.freenet.$$" || { fail_no_apply 'не удалось staged-copy resolver candidate'; return 1; }
    mv -f "$DNS_FILE.freenet.$$" "$DNS_FILE" || { fail_no_apply 'не удалось atomically install resolver candidate'; return 1; }
    if ! restart_xkeen; then
        err 'PRIMARY ERROR: XKeen/Xray restart failed after resolver apply'
        rollback && err 'ROLLBACK ERROR/STATE: rollback success' || err 'ROLLBACK ERROR/STATE: FAILED/UNKNOWN'
        return 1
    fi
    if ! verify_target; then
        err 'PRIMARY ERROR: post-apply resolver acceptance failed'
        rollback && err 'ROLLBACK ERROR/STATE: rollback success' || err 'ROLLBACK ERROR/STATE: FAILED/UNKNOWN'
        return 1
    fi
    say 'RESULT=SUCCESS'
    say 'APPLIED=yes'
    say "DIRECT_PROVIDER=$DIRECT_PROVIDER"
    say "VPN_PROVIDER=$VPN_PROVIDER"
    say 'ROLLBACK=NOT_NEEDED'
}

case "$MODE" in
    plan) plan ;;
    apply) apply ;;
    *) err 'usage: settings_dns_apply.sh [plan|apply] [direct-provider] [vpn-provider]'; exit 2 ;;
esac
