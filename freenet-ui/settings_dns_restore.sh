#!/bin/sh
set -u

ROOT="${FREENET_ROOT:-/opt}"
CONFIG_DIR="${FREENET_XRAY_CONFIG_DIR:-${FREENET_CONFIG_DIR:-$ROOT/etc/xray/configs}}"
DNS_FILE="$CONFIG_DIR/02_dns.json"
XRAY_BIN="${FREENET_XRAY_BIN:-$ROOT/sbin/xray}"
XRAY_ASSET_DIR="${FREENET_XRAY_ASSET_DIR:-$ROOT/etc/xray/dat}"
RUNTIME_TIMEOUT="${FREENET_XKEEN_RUNTIME_TIMEOUT:-75}"
TEST_MODE="${FREENET_SETTINGS_DNS_TEST_MODE:-no}"
BACKUP="${1:-}"

err() { printf '[FreeNet Settings DNS] ERROR: %s\n' "$*" >&2; }

xkeen_init() {
    for f in "$ROOT/etc/init.d/S99xkeen" "$ROOT/etc/init.d/S05xkeen"; do
        [ -f "$f" ] && { printf '%s\n' "$f"; return 0; }
    done
    return 1
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
    run_bounded "$RUNTIME_TIMEOUT" "/tmp/freenet-settings-dns-restore.$$.log" "$init" restart on
}

dns_query_ok() {
    if [ "$TEST_MODE" = yes ]; then
        [ "${FREENET_SETTINGS_DNS_TEST_QUERY:-success}" = success ]
        return
    fi
    n=0
    while [ "$n" -lt 3 ]; do
        nslookup example.com 127.0.0.1 >/tmp/freenet-settings-dns-restore-query.$$.log 2>&1 && return 0
        sleep 2
        n=$((n + 1))
    done
    return 1
}

runtime_ok() {
    if [ "$TEST_MODE" = yes ]; then
        [ "${FREENET_SETTINGS_DNS_TEST_RUNTIME:-success}" = success ]
        return
    fi
    pidof xray >/dev/null 2>&1 || return 1
    netstat -lnptu 2>/dev/null | grep ':53[[:space:]]' | grep -q '/xray'
}

for cmd in jq cp mv sha256sum; do
    command -v "$cmd" >/dev/null 2>&1 || { err "PRIMARY ERROR: missing command $cmd"; err 'ROLLBACK ERROR/STATE: FAILED/UNKNOWN'; exit 1; }
done
[ -n "$BACKUP" ] && [ -f "$BACKUP" ] || { err 'PRIMARY ERROR: resolver backup unavailable'; err 'ROLLBACK ERROR/STATE: FAILED/UNKNOWN'; exit 1; }
jq -e . "$BACKUP" >/dev/null 2>&1 || { err 'PRIMARY ERROR: resolver backup is invalid'; err 'ROLLBACK ERROR/STATE: FAILED/UNKNOWN'; exit 1; }
expected="$(sha256sum "$BACKUP" | awk '{print $1}')"
cp -p "$BACKUP" "$DNS_FILE.restore.$$" || { err 'PRIMARY ERROR: cannot stage resolver restore'; err 'ROLLBACK ERROR/STATE: FAILED/UNKNOWN'; exit 1; }
mv -f "$DNS_FILE.restore.$$" "$DNS_FILE" || { err 'PRIMARY ERROR: cannot install resolver restore'; err 'ROLLBACK ERROR/STATE: FAILED/UNKNOWN'; exit 1; }
restart_xkeen || { err 'PRIMARY ERROR: XKeen/Xray restart failed during resolver restore'; err 'ROLLBACK ERROR/STATE: FAILED/UNKNOWN'; exit 1; }
actual="$(sha256sum "$DNS_FILE" | awk '{print $1}')"
[ "$actual" = "$expected" ] && runtime_ok && dns_query_ok || { err 'PRIMARY ERROR: resolver restore acceptance failed'; err 'ROLLBACK ERROR/STATE: FAILED/UNKNOWN'; exit 1; }
printf '%s\n' 'RESULT=RESTORED'
printf '%s\n' 'ROLLBACK=SUCCESS'
