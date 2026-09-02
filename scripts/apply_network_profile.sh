#!/bin/sh

# FreeNet ISP/DNS network profile controller.
# plan = read-only runtime facts + expected delta.
# apply = only a verified preset may call the transactional Split-DNS engine.

CONFIG_FILE="${FREENET_CONFIG_FILE:-/opt/etc/freenet/freenet.conf}"
MIGRATE_SCRIPT="${FREENET_MIGRATE_SCRIPT:-/opt/lib/freenet/migrate_split_dns.sh}"
CONFIG_DIR="/opt/etc/xray/configs"
OUT_FILE="$CONFIG_DIR/04_outbounds.json"
XKEEN_BIN="/opt/sbin/xkeen"
XRAY_BIN="/opt/sbin/xray"
XRAY_ASSET_DIR="/opt/etc/xray/dat"
MODE="${1:-plan}"

say() { printf '%s\n' "$*"; }
err() { printf '[FreeNet Network] ERROR: %s\n' "$*" >&2; }

config_value() {
    KEY="$1"
    DEFAULT="$2"
    VALUE="$(sed -n "s/^${KEY}=//p" "$CONFIG_FILE" 2>/dev/null | tail -n 1 | tr -d "'\"\r")"
    [ -n "$VALUE" ] && printf '%s\n' "$VALUE" || printf '%s\n' "$DEFAULT"
}

xkeen_init() {
    for F in /opt/etc/init.d/S99xkeen /opt/etc/init.d/S05xkeen; do
        [ -f "$F" ] && { printf '%s\n' "$F"; return 0; }
    done
    return 1
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
    PROXY_DNS=unknown
    if [ -n "$XINIT" ]; then
        if grep -Eq '^[[:space:]]*proxy_dns="?on"?[[:space:]]*$' "$XINIT"; then
            PROXY_DNS=on
        elif grep -Eq '^[[:space:]]*proxy_dns="?off"?[[:space:]]*$' "$XINIT"; then
            PROXY_DNS=off
        fi
    fi

    PORT53_OWNER=none
    PORT53="$(netstat -lnp 2>/dev/null | grep ':53[[:space:]]' || true)"
    if printf '%s\n' "$PORT53" | grep -q '/ndnproxy'; then
        PORT53_OWNER=ndnproxy
    elif printf '%s\n' "$PORT53" | grep -q '/xray'; then
        PORT53_OWNER=xray
    elif [ -n "$PORT53" ]; then
        PORT53_OWNER=other
    fi

    XRAY_GID=unknown
    XPID="$(pidof xray 2>/dev/null | awk '{print $1}')"
    if [ -n "$XPID" ]; then
        XRAY_GID="$(awk '/^Gid:/ {print $2; exit}' "/proc/$XPID/status" 2>/dev/null)"
        [ -n "$XRAY_GID" ] || XRAY_GID=unknown
    fi

    DNS_OUT=no; has_dns_out && DNS_OUT=yes
    VLESS=no; has_vless && VLESS=yes

    say "XKEEN_INIT=${XINIT:-missing}"
    say "PROXY_DNS=$PROXY_DNS"
    say "PORT53_OWNER=$PORT53_OWNER"
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
                REASON='verified WORK preset'
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
        say 'EXPECTED_DELTA=preserve firmware ndnproxy :53; preserve existing VLESS/non-DNS routing; add/repair Xray built-in split DNS + dns-out + DNS routing rules; restart XKeen; validate; rollback on failure'
        say 'EXPECTED_NO_DELTA=no new Xray listener :53; no VLESS credential rewrite; no subscription secret change'
    else
        say 'EXPECTED_DELTA=NONE until preset runtime acceptance'
    fi
    say 'MUTATION=NONE'
    say '========== END =========='
}

preflight_rostelecom() {
    for C in jq netstat pidof awk grep sed; do
        command -v "$C" >/dev/null 2>&1 || { err "missing command: $C"; return 1; }
    done
    [ -x "$XKEEN_BIN" ] || { err 'XKeen missing'; return 1; }
    [ -x "$XRAY_BIN" ] || { err 'Xray missing'; return 1; }
    [ -x "$MIGRATE_SCRIPT" ] || { err 'transactional DNS migration engine missing'; return 1; }
    [ -d "$CONFIG_DIR" ] || { err 'Xray config directory missing'; return 1; }
    [ -d "$XRAY_ASSET_DIR" ] || { err 'Xray asset directory missing'; return 1; }
    has_vless || { err 'exactly one existing vless-reality profile is required before Rostelecom DNS apply'; return 1; }

    XINIT="$(xkeen_init 2>/dev/null || true)"
    [ -n "$XINIT" ] || { err 'XKeen init script missing'; return 1; }
    grep -Eq '^[[:space:]]*proxy_dns="?on"?[[:space:]]*$' "$XINIT" || {
        err 'Rostelecom preset requires XKeen proxy_dns=on; changing this switch is a separate controlled action'
        return 1
    }

    PORT53="$(netstat -lnp 2>/dev/null | grep ':53[[:space:]]' || true)"
    printf '%s\n' "$PORT53" | grep -q '/ndnproxy' || {
        err 'Rostelecom preset requires confirmed firmware ndnproxy owner on :53'
        return 1
    }
    if printf '%s\n' "$PORT53" | grep -q '/xray'; then
        err 'unexpected Xray listener already owns :53'
        return 1
    fi

    XPID="$(pidof xray 2>/dev/null | awk '{print $1}')"
    [ -n "$XPID" ] || { err 'Xray process not running'; return 1; }
    XGID="$(awk '/^Gid:/ {print $2; exit}' "/proc/$XPID/status" 2>/dev/null)"
    [ "$XGID" = 11111 ] || { err "Xray GID is $XGID, expected 11111"; return 1; }

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
            "$MIGRATE_SCRIPT"
            RC=$?
            case "$RC" in
                0)
                    has_dns_out || { err 'dns-out missing after migration success'; return 2; }
                    XRAY_LOCATION_ASSET="$XRAY_ASSET_DIR" "$XRAY_BIN" run -test -confdir "$CONFIG_DIR" >/tmp/freenet-network-xray.$$.log 2>&1 || {
                        tail -n 30 /tmp/freenet-network-xray.$$.log 2>/dev/null || true
                        rm -f /tmp/freenet-network-xray.$$.log 2>/dev/null
                        err 'post-apply Xray validation failed; migration rollback state must be inspected'
                        return 2
                    }
                    rm -f /tmp/freenet-network-xray.$$.log 2>/dev/null
                    say '[FreeNet Network] RESULT=SUCCESS'
                    say '[FreeNet Network] DNS_OUT=YES'
                    say '[FreeNet Network] PORT53_OWNER=ndnproxy-preserved'
                    return 0
                    ;;
                2)
                    err 'ROLLBACK ERROR/STATE: FAILED/UNKNOWN from DNS migration engine'
                    return 2
                    ;;
                *)
                    err 'PRIMARY ERROR: DNS migration failed and reported rollback success/no live apply'
                    return 1
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
