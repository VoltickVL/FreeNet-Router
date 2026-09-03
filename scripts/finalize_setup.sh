#!/bin/sh

# FreeNet Browser Setup completion gate.
# plan = read-only acceptance facts + exact expected delta.
# apply = transactional SETUP_COMPLETE/autostart/managed-cron commit.

ROOT="${FREENET_ROOT:-/opt}"
CONFIG_FILE="${FREENET_CONFIG_FILE:-$ROOT/etc/freenet/freenet.conf}"
SUB_FILE="${FREENET_SUB_FILE:-$ROOT/etc/xray/blanc_subscription.url}"
PROFILE_FILE="${FREENET_PROFILE_FILE:-$ROOT/etc/freenet/vpn_profile_name}"
CONFIG_DIR="${FREENET_XRAY_CONFIG_DIR:-$ROOT/etc/xray/configs}"
OUT_FILE="${FREENET_OUT_FILE:-$CONFIG_DIR/04_outbounds.json}"
ASSET_DIR="${FREENET_XRAY_ASSET_DIR:-$ROOT/etc/xray/dat}"
XKEEN_BIN="${FREENET_XKEEN_BIN:-$ROOT/sbin/xkeen}"
XRAY_BIN="${FREENET_XRAY_BIN:-$ROOT/sbin/xray}"
VPN_BIN="${FREENET_VPN_BIN:-$ROOT/bin/vpn}"
NETWORK_HELPER="${FREENET_NETWORK_HELPER:-$ROOT/lib/freenet/apply_network_profile.sh}"
CRONTAB_BIN="${FREENET_CRONTAB_BIN:-crontab}"
TEST_MODE="${FREENET_FINALIZE_TEST_MODE:-no}"
TEST_STATE="${FREENET_FINALIZE_TEST_STATE:-}"
MODE="${1:-plan}"
TMP_DIR=""
CONFIG_BEFORE=""
CRON_BEFORE=""
AUTOSTART_BEFORE="unknown"
APPLIED=0
ROLLBACK_ACTIVE=0

say() { printf '%s\n' "$*"; }
err() { printf '[FreeNet Setup Finalize] ERROR: %s\n' "$*" >&2; }

cleanup() {
    [ -n "$TMP_DIR" ] && rm -rf "$TMP_DIR" 2>/dev/null || true
}
trap cleanup 0 1 2 15

make_tmp() {
    [ -n "$TMP_DIR" ] && [ -d "$TMP_DIR" ] && return 0
    TMP_DIR="$(mktemp -d /tmp/freenet-finalize.XXXXXX 2>/dev/null)"
    [ -n "$TMP_DIR" ] && [ -d "$TMP_DIR" ] || {
        TMP_DIR="/tmp/freenet-finalize.$$"
        mkdir -p "$TMP_DIR" || return 1
    }
}

config_value() {
    KEY="$1"
    DEFAULT="$2"
    VALUE="$(sed -n "s/^${KEY}=//p" "$CONFIG_FILE" 2>/dev/null | tail -n 1 | tr -d "'\"\r")"
    [ -n "$VALUE" ] && printf '%s\n' "$VALUE" || printf '%s\n' "$DEFAULT"
}

set_config_value() {
    KEY="$1"
    VALUE="$2"
    TMP="$CONFIG_FILE.new.$$"
    awk -v key="$KEY" -v value="$VALUE" '
        BEGIN { found=0 }
        $0 ~ "^" key "=" {
            print key "=" value
            found=1
            next
        }
        { print }
        END { if (!found) print key "=" value }
    ' "$CONFIG_FILE" > "$TMP" || return 1
    chmod 600 "$TMP" 2>/dev/null || true
    mv -f "$TMP" "$CONFIG_FILE"
}

xkeen_init() {
    for F in "$ROOT/etc/init.d/S99xkeen" "$ROOT/etc/init.d/S05xkeen"; do
        [ -f "$F" ] && { printf '%s\n' "$F"; return 0; }
    done
    return 1
}

autostart_state() {
    INIT="$(xkeen_init 2>/dev/null || true)"
    [ -n "$INIT" ] || { printf '%s\n' unknown; return 0; }
    if grep -Eq '^[[:space:]]*start_auto="?on"?[[:space:]]*$' "$INIT"; then
        printf '%s\n' on
    elif grep -Eq '^[[:space:]]*start_auto="?off"?[[:space:]]*$' "$INIT"; then
        printf '%s\n' off
    else
        printf '%s\n' unknown
    fi
}

state_value() {
    KEY="$1"
    DEFAULT="$2"
    if [ "$TEST_MODE" = yes ] && [ -n "$TEST_STATE" ] && [ -f "$TEST_STATE" ]; then
        VALUE="$(sed -n "s/^${KEY}=//p" "$TEST_STATE" 2>/dev/null | tail -n 1)"
        [ -n "$VALUE" ] && { printf '%s\n' "$VALUE"; return 0; }
    fi
    printf '%s\n' "$DEFAULT"
}

xray_running() {
    if [ "$TEST_MODE" = yes ]; then
        [ "$(state_value XRAY_RUNNING no)" = yes ]
        return
    fi
    pidof xray >/dev/null 2>&1
}

validate_xray() {
    [ -x "$XRAY_BIN" ] || return 1
    [ -d "$CONFIG_DIR" ] || return 1
    [ -d "$ASSET_DIR" ] || return 1
    XRAY_LOCATION_ASSET="$ASSET_DIR" "$XRAY_BIN" run -test -confdir "$CONFIG_DIR" > "$TMP_DIR/xray-test.log" 2>&1
}

outbound_state() {
    DNS_OUT=no
    VLESS_PROFILE=no
    if [ -f "$OUT_FILE" ]; then
        jq -e '([.outbounds[]? | select(.tag == "dns-out" and .protocol == "dns")] | length) == 1' "$OUT_FILE" >/dev/null 2>&1 && DNS_OUT=yes
        jq -e '([.outbounds[]? | select(.tag == "vless-reality")] | length) == 1' "$OUT_FILE" >/dev/null 2>&1 && VLESS_PROFILE=yes
    fi
}

read_network_plan() {
    NETWORK_SUPPORTED=no
    NETWORK_MUTATION=unknown
    NETWORK_REASON='network plan unavailable'
    NETWORK_PROXY_DNS=unknown
    [ -f "$NETWORK_HELPER" ] || return 1
    sh "$NETWORK_HELPER" plan > "$TMP_DIR/network-plan.out" 2> "$TMP_DIR/network-plan.err" || return 1
    NETWORK_SUPPORTED="$(sed -n 's/^SUPPORTED=//p' "$TMP_DIR/network-plan.out" | tail -n 1)"
    NETWORK_MUTATION="$(sed -n 's/^MUTATION=//p' "$TMP_DIR/network-plan.out" | tail -n 1)"
    NETWORK_REASON="$(sed -n 's/^REASON=//p' "$TMP_DIR/network-plan.out" | tail -n 1)"
    NETWORK_PROXY_DNS="$(sed -n 's/^PROXY_DNS=//p' "$TMP_DIR/network-plan.out" | tail -n 1)"
    [ "$NETWORK_SUPPORTED" = yes ] && [ "$NETWORK_MUTATION" = NONE ]
}

cron_read() {
    "$CRONTAB_BIN" -l 2>/dev/null || true
}

build_managed_cron() {
    CURRENT="$TMP_DIR/cron.current"
    NEW="$TMP_DIR/cron.new"
    cron_read > "$CURRENT" || return 1

    awk '
        /^# BEGIN FREENET$/ {skip=1; next}
        /^# END FREENET$/ {skip=0; next}
        skip {next}
        /[[:space:]]\/opt\/bin\/blanc_xkeen_update_outbounds\.sh([[:space:]]|$)/ {next}
        /[[:space:]]\/opt\/bin\/vpn[[:space:]]+failover([[:space:]]|$)/ {next}
        /[[:space:]]\/opt\/sbin\/xkeen[[:space:]]+-ug([[:space:]]|$)/ {next}
        {print}
    ' "$CURRENT" > "$NEW" || return 1

    AUTO_ENDPOINT_UPDATE="$(config_value AUTO_ENDPOINT_UPDATE no)"
    AUTO_ENDPOINT_CRON="$(config_value AUTO_ENDPOINT_CRON '*/15 * * * *')"
    AUTO_VPN_FAILOVER="$(config_value AUTO_VPN_FAILOVER no)"
    AUTO_VPN_FAILOVER_CRON="$(config_value AUTO_VPN_FAILOVER_CRON '*/5 * * * *')"
    AUTO_XKEEN_GEODATA="$(config_value AUTO_XKEEN_GEODATA yes)"
    AUTO_XKEEN_GEODATA_CRON="$(config_value AUTO_XKEEN_GEODATA_CRON '30 6 * * *')"

    {
        echo '# BEGIN FREENET'
        if [ "$AUTO_XKEEN_GEODATA" = yes ]; then
            echo "$AUTO_XKEEN_GEODATA_CRON /opt/sbin/xkeen -ug"
        fi
        if [ "$AUTO_ENDPOINT_UPDATE" = yes ]; then
            echo "$AUTO_ENDPOINT_CRON /opt/bin/blanc_xkeen_update_outbounds.sh >> /opt/var/log/blanc_xkeen_update.log 2>&1"
        else
            echo '# endpoint refresh disabled by FreeNet settings'
        fi
        if [ "$AUTO_VPN_FAILOVER" = yes ]; then
            echo "$AUTO_VPN_FAILOVER_CRON /opt/bin/vpn failover >> /opt/var/log/freenet-vpn-failover.log 2>&1"
        else
            echo '# vpn failover disabled by FreeNet settings'
        fi
        echo '# END FREENET'
    } >> "$NEW"

    "$CRONTAB_BIN" "$NEW"
}

managed_cron_ok() {
    CRON="$TMP_DIR/cron.accept"
    cron_read > "$CRON" || return 1
    grep -q '^# BEGIN FREENET$' "$CRON" || return 1
    grep -q '^# END FREENET$' "$CRON" || return 1
    AUTO_ENDPOINT_UPDATE="$(config_value AUTO_ENDPOINT_UPDATE no)"
    if [ "$AUTO_ENDPOINT_UPDATE" = yes ]; then
        grep -q '/opt/bin/blanc_xkeen_update_outbounds.sh' "$CRON" || return 1
    else
        ! grep -q '^[^#].*/opt/bin/blanc_xkeen_update_outbounds.sh' "$CRON" || return 1
    fi
    AUTO_VPN_FAILOVER="$(config_value AUTO_VPN_FAILOVER no)"
    if [ "$AUTO_VPN_FAILOVER" = yes ]; then
        [ -x "$VPN_BIN" ] || return 1
        grep -q '/opt/bin/vpn failover' "$CRON" || return 1
    else
        ! grep -q '^[^#].*/opt/bin/vpn[[:space:]]\+failover' "$CRON" || return 1
    fi
    return 0
}

evaluate() {
    READY=yes
    REASON='ready to finalize'
    INSTALL_SCENARIO="$(config_value INSTALL_SCENARIO unknown)"
    SETUP_COMPLETE="$(config_value SETUP_COMPLETE no)"
    AUTOSTART="$(autostart_state)"
    AUTO_ENDPOINT_UPDATE="$(config_value AUTO_ENDPOINT_UPDATE no)"
    AUTO_ENDPOINT_CRON="$(config_value AUTO_ENDPOINT_CRON '*/15 * * * *')"
    AUTO_VPN_FAILOVER="$(config_value AUTO_VPN_FAILOVER no)"
    AUTO_VPN_FAILOVER_CRON="$(config_value AUTO_VPN_FAILOVER_CRON '*/5 * * * *')"
    SUBSCRIPTION_CONFIGURED=no; [ -s "$SUB_FILE" ] && SUBSCRIPTION_CONFIGURED=yes
    PREFERRED_PROFILE_SET=no; [ -s "$PROFILE_FILE" ] && PREFERRED_PROFILE_SET=yes
    XRAY_RUNNING=no; xray_running && XRAY_RUNNING=yes
    XRAY_VALID=no; validate_xray && XRAY_VALID=yes
    outbound_state
    read_network_plan || true

    if [ "$SUBSCRIPTION_CONFIGURED" != yes ]; then READY=no; REASON='subscription is not configured'
    elif [ "$PREFERRED_PROFILE_SET" != yes ]; then READY=no; REASON='preferred VPN profile is not applied'
    elif [ "$VLESS_PROFILE" != yes ]; then READY=no; REASON='exactly one vless-reality outbound is required'
    elif [ "$DNS_OUT" != yes ]; then READY=no; REASON='dns-out is not accepted yet'
    elif [ "$XRAY_RUNNING" != yes ]; then READY=no; REASON='Xray is not running'
    elif [ "$XRAY_VALID" != yes ]; then READY=no; REASON='live Xray configuration validation failed'
    elif [ "$NETWORK_SUPPORTED" != yes ] || [ "$NETWORK_MUTATION" != NONE ]; then READY=no; REASON='saved ISP/DNS profile is not runtime-accepted'
    elif [ "$AUTOSTART" = unknown ]; then READY=no; REASON='cannot determine XKeen autostart state'
    elif [ "$AUTO_VPN_FAILOVER" = yes ] && [ ! -x "$VPN_BIN" ]; then READY=no; REASON='vpn helper is required for managed failover'
    fi
}

print_plan() {
    make_tmp || { err 'cannot create temporary directory'; return 1; }
    evaluate
    say '========== FreeNet Setup Completion Plan =========='
    say "READY=$READY"
    say "REASON=$REASON"
    say "INSTALL_SCENARIO=$INSTALL_SCENARIO"
    say "SETUP_COMPLETE=$SETUP_COMPLETE"
    say "SUBSCRIPTION_CONFIGURED=$SUBSCRIPTION_CONFIGURED"
    say "PREFERRED_PROFILE_SET=$PREFERRED_PROFILE_SET"
    say "NETWORK_SUPPORTED=$NETWORK_SUPPORTED"
    say "XRAY_RUNNING=$XRAY_RUNNING"
    say "XRAY_VALID=$XRAY_VALID"
    say "DNS_OUT=$DNS_OUT"
    say "VLESS_PROFILE=$VLESS_PROFILE"
    say "XKEEN_AUTOSTART=$AUTOSTART"
    say "AUTO_ENDPOINT_UPDATE=$AUTO_ENDPOINT_UPDATE"
    say "AUTO_ENDPOINT_CRON=$AUTO_ENDPOINT_CRON"
    say "AUTO_VPN_FAILOVER=$AUTO_VPN_FAILOVER"
    say "AUTO_VPN_FAILOVER_CRON=$AUTO_VPN_FAILOVER_CRON"
    if [ "$READY" = yes ]; then
        DELTA='set SETUP_COMPLETE=yes; rebuild the FreeNet-managed cron block'
        [ "$AUTOSTART" = off ] && DELTA="$DELTA; enable XKeen autostart through xkeen -auto on"
        [ "$AUTO_ENDPOINT_UPDATE" = yes ] && DELTA="$DELTA; activate configured endpoint refresh schedule" || DELTA="$DELTA; keep endpoint refresh disabled until Automation settings enable it"
        [ "$AUTO_VPN_FAILOVER" = yes ] && DELTA="$DELTA; activate configured VPN failover schedule" || DELTA="$DELTA; keep automatic VPN failover disabled"
        say "EXPECTED_DELTA=$DELTA"
    else
        say 'EXPECTED_DELTA=NONE until all provider/network/runtime acceptance gates pass'
    fi
    say 'EXPECTED_NO_DELTA=current XKeen/Xray/XKeen UI core is not reinstalled; no subscription secret/VLESS credential rewrite; no ISP/DNS/routing mutation; no raw shell command surface'
    say 'MUTATION=NONE'
    say '========== END =========='
    [ "$READY" = yes ]
}

snapshot() {
    make_tmp || return 1
    [ -f "$CONFIG_FILE" ] || return 1
    CONFIG_BEFORE="$TMP_DIR/freenet.conf.before"
    CRON_BEFORE="$TMP_DIR/crontab.before"
    cp -p "$CONFIG_FILE" "$CONFIG_BEFORE" || return 1
    cron_read > "$CRON_BEFORE" || return 1
    AUTOSTART_BEFORE="$(autostart_state)"
    case "$AUTOSTART_BEFORE" in on|off) ;; *) return 1 ;; esac
}

restore() {
    ROLLBACK_ACTIVE=1
    RB=0
    [ -f "$CONFIG_BEFORE" ] && cp -p "$CONFIG_BEFORE" "$CONFIG_FILE.rollback.$$" 2>/dev/null || RB=1
    [ "$RB" -ne 0 ] || mv -f "$CONFIG_FILE.rollback.$$" "$CONFIG_FILE" 2>/dev/null || RB=1
    "$CRONTAB_BIN" "$CRON_BEFORE" >/dev/null 2>&1 || RB=1
    case "$AUTOSTART_BEFORE" in
        on) "$XKEEN_BIN" -auto on >/dev/null 2>&1 || RB=1 ;;
        off) "$XKEEN_BIN" -auto off >/dev/null 2>&1 || RB=1 ;;
        *) RB=1 ;;
    esac
    [ "$(autostart_state)" = "$AUTOSTART_BEFORE" ] || RB=1
    ROLLBACK_ACTIVE=0
    [ "$RB" -eq 0 ]
}

fail_apply() {
    PRIMARY="$1"
    err "PRIMARY ERROR: $PRIMARY"
    if [ "$APPLIED" -eq 1 ] && [ "$ROLLBACK_ACTIVE" -eq 0 ]; then
        if restore; then
            err 'ROLLBACK ERROR/STATE: rollback success'
            exit 1
        fi
        err 'ROLLBACK ERROR/STATE: FAILED/UNKNOWN'
        exit 2
    fi
    err 'ROLLBACK ERROR/STATE: no live apply'
    exit 1
}

apply() {
    make_tmp || fail_apply 'cannot create temporary directory'
    evaluate
    [ "$READY" = yes ] || fail_apply "$REASON"
    snapshot || fail_apply 'cannot snapshot FreeNet config/cron/autostart'
    APPLIED=1

    if [ "$AUTOSTART_BEFORE" = off ]; then
        "$XKEEN_BIN" -auto on > "$TMP_DIR/xkeen-auto.log" 2>&1 || fail_apply 'xkeen -auto on failed'
        [ "$(autostart_state)" = on ] || fail_apply 'XKeen autostart did not become on'
    fi

    set_config_value SETUP_COMPLETE yes || fail_apply 'cannot commit SETUP_COMPLETE=yes'
    build_managed_cron || fail_apply 'cannot rebuild FreeNet-managed cron block'

    evaluate
    [ "$SETUP_COMPLETE" = yes ] || fail_apply 'SETUP_COMPLETE acceptance failed'
    [ "$AUTOSTART" = on ] || fail_apply 'XKeen autostart acceptance failed'
    [ "$READY" = yes ] || fail_apply "post-finalize acceptance failed: $REASON"
    managed_cron_ok || fail_apply 'managed cron acceptance failed'

    APPLIED=0
    say '[FreeNet Setup Finalize] RESULT=SUCCESS'
    say '[FreeNet Setup Finalize] SETUP_COMPLETE=yes'
    say '[FreeNet Setup Finalize] XKEEN_AUTOSTART=on'
    say "[FreeNet Setup Finalize] AUTO_ENDPOINT_UPDATE=$(config_value AUTO_ENDPOINT_UPDATE no)"
    say "[FreeNet Setup Finalize] AUTO_VPN_FAILOVER=$(config_value AUTO_VPN_FAILOVER no)"
    say '[FreeNet Setup Finalize] ROLLBACK=NOT_NEEDED'
}

for C in jq sed awk grep tr tail mktemp cp mv mkdir chmod; do
    command -v "$C" >/dev/null 2>&1 || { err "required command missing: $C"; exit 1; }
done
command -v "$CRONTAB_BIN" >/dev/null 2>&1 || { err 'crontab command is missing'; exit 1; }
[ -x "$XKEEN_BIN" ] || { err 'XKeen binary is missing'; exit 1; }
[ -x "$XRAY_BIN" ] || { err 'Xray binary is missing'; exit 1; }
[ -f "$CONFIG_FILE" ] || { err 'FreeNet config is missing'; exit 1; }
[ -f "$NETWORK_HELPER" ] || { err 'network profile helper is missing'; exit 1; }

case "$MODE" in
    plan) print_plan ;;
    apply) apply ;;
    *) err 'usage: finalize_setup.sh [plan|apply]'; exit 2 ;;
esac
