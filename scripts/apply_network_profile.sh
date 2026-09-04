#!/bin/sh

# FreeNet ISP/DNS controller.
# plan  - read-only facts and expected delta.
# apply - transactional switch between native Keenetic DNS and OPKG/Xray Split DNS.

ROOT="${FREENET_ROOT:-/opt}"
CONFIG_FILE="${FREENET_CONFIG_FILE:-$ROOT/etc/freenet/freenet.conf}"
CONFIG_DIR="${FREENET_CONFIG_DIR:-$ROOT/etc/xray/configs}"
DNS_FILE="$CONFIG_DIR/02_dns.json"
INBOUND_FILE="$CONFIG_DIR/03_inbounds.json"
OUT_FILE="$CONFIG_DIR/04_outbounds.json"
ROUTING_FILE="$CONFIG_DIR/05_routing.json"
XKEEN_BIN="${FREENET_XKEEN_BIN:-$ROOT/sbin/xkeen}"
XRAY_BIN="${FREENET_XRAY_BIN:-$ROOT/sbin/xray}"
XRAY_ASSET_DIR="${FREENET_XRAY_ASSET_DIR:-$ROOT/etc/xray/dat}"
BACKUP_ROOT="${FREENET_BACKUP_ROOT:-$ROOT/backups}"
NATIVE_STATE_DIR="${FREENET_NATIVE_DNS_STATE_DIR:-$ROOT/etc/freenet/native-dns}"
RUNTIME_TIMEOUT="${FREENET_XKEEN_RUNTIME_TIMEOUT:-75}"
MODE="${1:-plan}"
TEST_MODE="${FREENET_NETWORK_TEST_MODE:-no}"
TEST_STATE="${FREENET_NETWORK_TEST_STATE:-}"
TMP_DIR=""
BACKUP_DIR=""
PROXY_DNS_INITIAL="unknown"
NDM_OVERRIDE_INITIAL="unknown"
NDM_FILTER_ENGINE_INITIAL="unknown"
NDM_INTERCEPT_INITIAL="unknown"
NDM_PROTECTED_HASH_INITIAL=""
XRAY_WAS_RUNNING="no"
CONFIG_SNAPSHOT_KIND="none"

say() { printf '%s\n' "$*"; }
err() { printf '[FreeNet Network] ERROR: %s\n' "$*" >&2; }
fail_not_applied() {
    err "PRIMARY ERROR: $*"
    err 'ROLLBACK ERROR/STATE: no live apply'
    return 1
}

cleanup() {
    [ -n "$TMP_DIR" ] && rm -rf "$TMP_DIR" 2>/dev/null || true
}
trap cleanup 0 1 2 15

make_tmp() {
    [ -n "$TMP_DIR" ] && [ -d "$TMP_DIR" ] && return 0
    TMP_DIR="$(mktemp -d /tmp/freenet-network.XXXXXX 2>/dev/null)"
    if [ -z "$TMP_DIR" ] || [ ! -d "$TMP_DIR" ]; then
        TMP_DIR="/tmp/freenet-network.$$"
        mkdir -p "$TMP_DIR" || return 1
    fi
}

config_value() {
    KEY="$1"; DEFAULT="$2"
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
    KEY="$1"; DEFAULT="$2"
    if [ "$TEST_MODE" = yes ] && [ -n "$TEST_STATE" ] && [ -f "$TEST_STATE" ]; then
        VALUE="$(sed -n "s/^${KEY}=//p" "$TEST_STATE" 2>/dev/null | tail -n 1)"
        [ -n "$VALUE" ] && { printf '%s\n' "$VALUE"; return 0; }
    fi
    printf '%s\n' "$DEFAULT"
}

test_state_set() {
    KEY="$1"; VALUE="$2"
    [ "$TEST_MODE" = yes ] && [ -n "$TEST_STATE" ] || return 1
    T="$TEST_STATE.tmp.$$"
    grep -v "^${KEY}=" "$TEST_STATE" > "$T" 2>/dev/null || true
    printf '%s=%s\n' "$KEY" "$VALUE" >> "$T" || return 1
    mv -f "$T" "$TEST_STATE"
}

proxy_dns_state() {
    INIT="$(xkeen_init 2>/dev/null || true)"
    [ -n "$INIT" ] || { printf '%s\n' unknown; return 0; }
    if grep -Eq '^[[:space:]]*proxy_dns="?off"?[[:space:]]*$' "$INIT"; then
        printf '%s\n' off
    elif grep -Eq '^[[:space:]]*proxy_dns="?on"?[[:space:]]*$' "$INIT"; then
        printf '%s\n' on
    else
        printf '%s\n' unknown
    fi
}

set_proxy_dns_off() {
    INIT="$(xkeen_init 2>/dev/null || true)"
    [ -n "$INIT" ] || return 1
    [ "$(proxy_dns_state)" = off ] && return 0
    STAGED="$INIT.freenet.$$"
    cp -p "$INIT" "$STAGED" || return 1
    sed -i \
        -e 's/^[[:space:]]*proxy_dns="on"[[:space:]]*$/proxy_dns="off"/' \
        -e 's/^[[:space:]]*proxy_dns=on[[:space:]]*$/proxy_dns="off"/' \
        "$STAGED" || { rm -f "$STAGED"; return 1; }
    grep -Eq '^[[:space:]]*proxy_dns="?off"?[[:space:]]*$' "$STAGED" || { rm -f "$STAGED"; return 1; }
    mv -f "$STAGED" "$INIT" || { rm -f "$STAGED"; return 1; }
}

listener_lines() {
    if [ "$TEST_MODE" = yes ]; then
        case "$(test_state_value PORT53_OWNER none)" in
            ndnproxy)
                printf '%s\n' 'tcp 0 0 0.0.0.0:53 0.0.0.0:* LISTEN 800/ndnproxy'
                printf '%s\n' 'udp 0 0 0.0.0.0:53 0.0.0.0:* 800/ndnproxy'
                ;;
            xray)
                printf '%s\n' 'tcp 0 0 0.0.0.0:53 0.0.0.0:* LISTEN 900/xray'
                printf '%s\n' 'udp 0 0 0.0.0.0:53 0.0.0.0:* 900/xray'
                ;;
            other)
                printf '%s\n' 'tcp 0 0 0.0.0.0:53 0.0.0.0:* LISTEN 700/other'
                ;;
        esac
        return 0
    fi
    netstat -lnptu 2>/dev/null || true
}

port53_owner() {
    LINES="$(listener_lines | grep ':53[[:space:]]' || true)"
    if printf '%s\n' "$LINES" | grep -q '/xray'; then printf '%s\n' xray
    elif printf '%s\n' "$LINES" | grep -q '/ndnproxy'; then printf '%s\n' ndnproxy
    elif [ -n "$LINES" ]; then printf '%s\n' other
    else printf '%s\n' none
    fi
}

xray_pid() {
    if [ "$TEST_MODE" = yes ]; then
        [ "$(test_state_value XRAY_RUNNING yes)" = yes ] && printf '%s\n' 4242
        return 0
    fi
    pidof xray 2>/dev/null | awk '{print $1}'
}

xray_gid() {
    PID="$1"
    if [ "$TEST_MODE" = yes ]; then test_state_value XRAY_GID 11111; return 0; fi
    [ -n "$PID" ] || { printf '%s\n' unknown; return 0; }
    VALUE="$(awk '/^Gid:/ {print $2; exit}' "/proc/$PID/status" 2>/dev/null)"
    [ -n "$VALUE" ] && printf '%s\n' "$VALUE" || printf '%s\n' unknown
}

xray_dns_inbound_count() {
    jq -r '[.inbounds[]? | select((((.port // "") | tostring) == "53") and .protocol == "dokodemo-door")] | length' "$INBOUND_FILE" 2>/dev/null || printf '%s\n' unknown
}

has_dns_out() {
    jq -e '([.outbounds[]? | select(.tag == "dns-out" and .protocol == "dns")] | length) == 1' "$OUT_FILE" >/dev/null 2>&1
}

has_vless() {
    jq -e '([.outbounds[]? | select(.tag == "vless-reality")] | length) == 1' "$OUT_FILE" >/dev/null 2>&1
}

dns_routing_mode() {
    STD="$(jq -r '[.routing.rules[]? | select(((.inboundTag // []) | index("dns-vless")) != null and .outboundTag == "direct")] | length' "$ROUTING_FILE" 2>/dev/null || printf 0)"
    SPLIT="$(jq -r '[.routing.rules[]? | select(((.inboundTag // []) | index("dns-vless")) != null and .outboundTag == "vless-reality")] | length' "$ROUTING_FILE" 2>/dev/null || printf 0)"
    DIRECT="$(jq -r '[.routing.rules[]? | select(((.inboundTag // []) | index("dns-direct")) != null and .outboundTag == "direct")] | length' "$ROUTING_FILE" 2>/dev/null || printf 0)"
    OUT="$(jq -r '[.routing.rules[]? | select((((.port // "") | tostring) == "53") and .outboundTag == "dns-out")] | length' "$ROUTING_FILE" 2>/dev/null || printf 0)"
    if [ "$STD:$SPLIT:$DIRECT:$OUT" = "0:0:0:0" ]; then printf '%s\n' native
    elif [ "$STD:$SPLIT:$DIRECT:$OUT" = "0:1:1:1" ]; then printf '%s\n' split
    elif [ "$STD:$SPLIT:$DIRECT:$OUT" = "1:0:1:1" ]; then printf '%s\n' standard
    else printf '%s\n' unknown
    fi
}

run_bounded() {
    LIMIT="$1"; LOG_FILE="$2"; shift 2
    "$@" > "$LOG_FILE" 2>&1 &
    CMD_PID=$!; ELAPSED=0
    while kill -0 "$CMD_PID" 2>/dev/null; do
        if [ "$ELAPSED" -ge "$LIMIT" ]; then
            kill -TERM "$CMD_PID" 2>/dev/null || true
            sleep 2
            kill -0 "$CMD_PID" 2>/dev/null && kill -KILL "$CMD_PID" 2>/dev/null || true
            wait "$CMD_PID" 2>/dev/null || true
            return 124
        fi
        sleep 1; ELAPSED=$((ELAPSED + 1))
    done
    wait "$CMD_PID"
}

test_runtime_action() {
    ACTION="$1"
    RESULT="$(test_state_value XKEEN_ACTION_RESULT success)"
    case "$RESULT" in timeout) return 124 ;; fail) return 1 ;; success|'') : ;; *) return 1 ;; esac
    case "$ACTION" in
        start|restart)
            test_state_set XRAY_RUNNING yes || return 1
            test_state_set XRAY_GID 11111 || return 1
            if jq -e 'any(.inbounds[]?; (((.port // "") | tostring) == "53"))' "$INBOUND_FILE" >/dev/null 2>&1; then
                if [ "$(test_state_value NDM_DNS_OVERRIDE off)" = on ]; then test_state_set PORT53_OWNER xray || return 1
                else test_state_set PORT53_OWNER other || return 1
                fi
            elif [ "$(test_state_value NDM_DNS_OVERRIDE off)" = off ]; then
                test_state_set PORT53_OWNER ndnproxy || return 1
            else
                test_state_set PORT53_OWNER none || return 1
            fi
            ;;
        stop)
            test_state_set XRAY_RUNNING no || return 1
            if [ "$(test_state_value NDM_DNS_OVERRIDE off)" = off ]; then test_state_set PORT53_OWNER ndnproxy || return 1
            else test_state_set PORT53_OWNER none || return 1
            fi
            ;;
        *) return 1 ;;
    esac
}

xkeen_runtime() {
    ACTION="$1"; LOG_FILE="$2"
    if [ "$TEST_MODE" = yes ]; then test_runtime_action "$ACTION"; return; fi
    INIT="$(xkeen_init 2>/dev/null || true)"
    [ -n "$INIT" ] && [ -x "$INIT" ] || return 1
    case "$ACTION" in
        start) run_bounded "$RUNTIME_TIMEOUT" "$LOG_FILE" "$INIT" start on ;;
        restart) run_bounded "$RUNTIME_TIMEOUT" "$LOG_FILE" "$INIT" restart on ;;
        stop) run_bounded "$RUNTIME_TIMEOUT" "$LOG_FILE" "$INIT" stop ;;
        *) return 1 ;;
    esac
}

wait_for_xray() {
    WANT="$1"; N=0
    while [ "$N" -lt 12 ]; do
        PID="$(xray_pid)"
        [ "$WANT" = yes ] && [ -n "$PID" ] && return 0
        [ "$WANT" = no ] && [ -z "$PID" ] && return 0
        [ "$TEST_MODE" = yes ] || sleep 1
        N=$((N + 1))
    done
    return 1
}

wait_port53_owner() {
    WANT="$1"; N=0
    while [ "$N" -lt 15 ]; do
        [ "$(port53_owner)" = "$WANT" ] && return 0
        [ "$TEST_MODE" = yes ] || sleep 1
        N=$((N + 1))
    done
    return 1
}

ndm_running_config() {
    if [ "$TEST_MODE" = yes ]; then
        OVERRIDE="$(test_state_value NDM_DNS_OVERRIDE off)"
        ENGINE="$(test_state_value NDM_FILTER_ENGINE public)"
        INTERCEPT="$(test_state_value NDM_DNS_INTERCEPT on)"
        PROFILE_MARKER="$(test_state_value NDM_DNS_PROFILE_MARKER preserved)"
        printf '%s\n' 'dns-proxy'
        printf '%s\n' '    rebind-protect auto'
        [ "$INTERCEPT" = on ] && printf '%s\n' '    intercept enable'
        printf '%s\n' '    tls upstream common.dot.dns.yandex.net'
        printf '%s\n' '    https upstream https://common.dot.dns.yandex.net/dns-query'
        printf '%s\n' '    filter profile xbox-dns.ru'
        printf '    filter profile xbox-dns.ru description %s\n' "$PROFILE_MARKER"
        printf '%s\n' '    filter profile xbox-dns.ru tls upstream xbox-dns.ru'
        printf '%s\n' '    filter assign host profile aa:bb:cc:dd:ee:ff xbox-dns.ru'
        printf '    filter engine %s\n' "$ENGINE"
        printf '%s\n' '!'
        printf '%s\n' 'interface GigabitEthernet0/0'
        printf '%s\n' '    ip dhcp client dns-routes'
        printf '%s\n' '    ip dhcp client no name-servers'
        printf '%s\n' '!'
        [ "$OVERRIDE" = on ] && printf '%s\n' 'opkg dns-override'
        printf '%s\n' 'system'
        printf '    description runtime-%s-%s\n' "$OVERRIDE" "$(test_state_value NDM_CONFIG_MARKER preserved)"
        printf '%s\n' '!'
        return 0
    fi
    ndmc -c 'show running-config'
}

ndm_override_state() {
    CFG="$(ndm_running_config 2>/dev/null)" || { printf '%s\n' unknown; return 0; }
    if printf '%s\n' "$CFG" | grep -Eq '^[[:space:]]*opkg dns-override[[:space:]]*$'; then printf '%s\n' on
    else printf '%s\n' off
    fi
}

ndm_filter_engine_state() {
    ENGINE="$(ndm_running_config 2>/dev/null | tr -d '\r' | awk '
        function trim(s) { sub(/^[ \t]+/, "", s); sub(/[ \t]+$/, "", s); return s }
        {
            raw=$0
            if (raw ~ /^[^ \t]/) {
                top=trim(raw)
                if (top == "!") { dns=0; next }
                if (top == "dns-proxy") { dns=1; next }
                if (top ~ /^dns-proxy[ \t]+filter[ \t]+engine[ \t]+/) {
                    sub(/^dns-proxy[ \t]+filter[ \t]+engine[ \t]+/, "", top)
                    split(top, a, /[ \t]+/); print a[1]; exit
                }
                dns=0
                next
            }
            line=trim(raw)
            if (dns && line ~ /^filter[ \t]+engine[ \t]+/) {
                sub(/^filter[ \t]+engine[ \t]+/, "", line)
                split(line, a, /[ \t]+/); print a[1]; exit
            }
        }
    ')"
    [ -n "$ENGINE" ] && printf '%s\n' "$ENGINE" || printf '%s\n' unknown
}

ndm_filter_engine_token_ok() {
    printf '%s\n' "$1" | grep -Eq '^[A-Za-z0-9_-]+$'
}

ndm_intercept_state() {
    STATE="$(ndm_running_config 2>/dev/null | tr -d '\r' | awk '
        function trim(s) { sub(/^[ \t]+/, "", s); sub(/[ \t]+$/, "", s); return s }
        {
            raw=$0
            if (raw ~ /^[^ \t]/) {
                top=trim(raw)
                if (top == "!") { dns=0; next }
                if (top == "dns-proxy") { dns=1; next }
                if (top == "dns-proxy intercept enable") { print "on"; exit }
                dns=0
                next
            }
            line=trim(raw)
            if (dns && line == "intercept enable") { print "on"; exit }
        }
        END { }
    ')"
    [ "$STATE" = on ] && printf '%s\n' on || printf '%s\n' off
}

# Protect only user DNS state that must survive native <-> OPKG transitions.
# Engine/intercept representation is intentionally excluded because OPKG mode changes
# that control plane; profiles/upstreams/assignments and WAN DNS flags must not drift.
ndm_protected_state() {
    ndm_running_config 2>/dev/null | tr -d '\r' | awk '
        function trim(s) { sub(/^[ \t]+/, "", s); sub(/[ \t]+$/, "", s); return s }
        function emit(scope, line) { line=trim(line); if (line != "") print scope "|" line }
        function protected_dns(line) {
            return line ~ /^(tls|https|dns53)[ \t]+upstream([ \t]|$)/ ||
                   line ~ /^filter[ \t]+profile([ \t]|$)/ ||
                   line ~ /^filter[ \t]+assign([ \t]|$)/
        }
        {
            raw=$0
            if (raw ~ /^[^ \t]/) {
                top=trim(raw)
                if (top == "!") { scope=""; next }
                if (top == "dns-proxy") { scope="dns-proxy"; next }
                if (top ~ /^dns-proxy[ \t]+/) {
                    line=top
                    sub(/^dns-proxy[ \t]+/, "", line)
                    if (protected_dns(line)) emit("dns-proxy", line)
                    scope=""
                    next
                }
                if (top ~ /^interface[ \t]+/) { scope=top; next }
                if (top ~ /^ip[ \t]+host[ \t]+/ || top ~ /^ip[ \t]+name-server[ \t]+/ || top ~ /^ipv6[ \t]+name-server[ \t]+/) {
                    emit("global", top)
                    scope=""
                    next
                }
                scope=""
                next
            }
            line=trim(raw)
            if (scope == "dns-proxy") {
                if (protected_dns(line)) emit("dns-proxy", line)
            } else if (scope ~ /^interface[ \t]+/) {
                if (line ~ /(^|[ \t])name-servers([ \t]|$)/ || line ~ /^ip[ \t]+dhcp[ \t]+client[ \t]+dns-routes([ \t]|$)/) emit(scope, line)
            }
        }
    ' | LC_ALL=C sort
}

ndm_protected_hash() {
    ndm_protected_state | sha256sum | awk '{print $1}'
}

ndm_set_override() {
    WANT="$1"
    if [ "$TEST_MODE" = yes ]; then
        [ "$(test_state_value NDM_ACTION_RESULT success)" = success ] || return 1
        test_state_set NDM_DNS_OVERRIDE "$WANT" || return 1
        if [ "$WANT" = on ]; then test_state_set PORT53_OWNER none || return 1
        elif jq -e 'any(.inbounds[]?; (((.port // "") | tostring) == "53"))' "$INBOUND_FILE" >/dev/null 2>&1; then test_state_set PORT53_OWNER other || return 1
        else test_state_set PORT53_OWNER ndnproxy || return 1
        fi
        if [ "$(test_state_value NDM_MUTATE_PROTECTED_ON_OVERRIDE no)" = yes ]; then
            test_state_set NDM_DNS_PROFILE_MARKER changed || return 1
        fi
        return 0
    fi
    case "$WANT" in
        on) ndmc -c 'opkg dns-override' >/dev/null 2>&1 ;;
        off) ndmc -c 'no opkg dns-override' >/dev/null 2>&1 ;;
        *) return 1 ;;
    esac
}

ndm_set_filter_engine() {
    WANT="$1"
    ndm_filter_engine_token_ok "$WANT" || return 1
    if [ "$TEST_MODE" = yes ]; then
        [ "$(test_state_value NDM_FILTER_ENGINE_ACTION_RESULT success)" = success ] || return 1
        test_state_set NDM_FILTER_ENGINE "$WANT"
        return
    fi
    ndmc -c "dns-proxy filter engine $WANT" >/dev/null 2>&1
}

ndm_set_intercept() {
    WANT="$1"
    case "$WANT" in on|off) : ;; *) return 1 ;; esac
    if [ "$TEST_MODE" = yes ]; then
        [ "$(test_state_value NDM_INTERCEPT_ACTION_RESULT success)" = success ] || return 1
        test_state_set NDM_DNS_INTERCEPT "$WANT"
        return
    fi
    case "$WANT" in
        on) ndmc -c 'dns-proxy intercept enable' >/dev/null 2>&1 ;;
        off) ndmc -c 'dns-proxy no intercept enable' >/dev/null 2>&1 ;;
    esac
}

ndm_save() {
    if [ "$TEST_MODE" = yes ]; then
        [ "$(test_state_value NDM_SAVE_RESULT success)" = success ]
        return
    fi
    ndmc -c 'system configuration save' >/dev/null 2>&1
}

dns_query_ok() {
    if [ "$TEST_MODE" = yes ]; then [ "$(test_state_value DNS_QUERY_OK yes)" = yes ]; return; fi
    TARGETS="127.0.0.1"
    if command -v ip >/dev/null 2>&1; then
        ADDRS="$(ip -o -4 addr show 2>/dev/null | awk '$2 != "lo" {split($4,a,"/"); if (a[1] != "") print a[1]}' | awk '!seen[$0]++')"
        [ -n "$ADDRS" ] && TARGETS="$TARGETS $ADDRS"
    fi
    for DNS_TARGET in $TARGETS; do
        N=0
        while [ "$N" -lt 3 ]; do
            nslookup example.com "$DNS_TARGET" >/tmp/freenet-network-dns-query.$$.log 2>&1 && return 0
            sleep 2; N=$((N + 1))
        done
    done
    return 1
}

non_dns_hashes() {
    jq -cS '[.inbounds[]? | select((((.port // "") | tostring) != "53"))]' "$INBOUND_FILE" | sha256sum | awk '{print $1}'
    jq -cS '[.outbounds[]? | select((.tag // "") != "dns-out")]' "$OUT_FILE" | sha256sum | awk '{print $1}'
    jq -cS '[.routing.rules[]? | select((.outboundTag // "") != "dns-out") | select(((.inboundTag // []) | index("dns-vless")) == null) | select(((.inboundTag // []) | index("dns-direct")) == null) | select(((.inboundTag // []) | index("dns-in")) == null) | select((((.port // "") | tostring) != "53"))]' "$ROUTING_FILE" | sha256sum | awk '{print $1}'
}

resolve_profile() {
    ISP_ID="$(config_value ISP_ID auto)"
    DNS_MODE="$(config_value DNS_MODE firmware)"
    case "$ISP_ID" in auto|vladlink|alliancetelecom|rostelecom|podryad|custom) : ;; *) SUPPORTED=no; EFFECTIVE_DNS=unknown; REASON='неизвестный профиль интернет-провайдера'; return ;; esac
    case "$DNS_MODE" in
        auto|firmware) EFFECTIVE_DNS=firmware; SUPPORTED=yes; REASON='нативный DNS Keenetic без VPN-проксирования' ;;
        xkeen) EFFECTIVE_DNS=xkeen; SUPPORTED=yes; REASON='Split DNS через OPKG/Xray выбран явно' ;;
        custom) EFFECTIVE_DNS=custom; SUPPORTED=no; REASON='свой DNS доступен только в будущем Expert mode' ;;
        *) EFFECTIVE_DNS=unknown; SUPPORTED=no; REASON='неизвестный DNS mode' ;;
    esac
}

runtime_facts() {
    PROXY_DNS="$(proxy_dns_state)"
    PORT53_OWNER="$(port53_owner)"
    NDM_OVERRIDE="$(ndm_override_state)"
    NDM_ENGINE="$(ndm_filter_engine_state)"
    NDM_INTERCEPT="$(ndm_intercept_state)"
    DNS_INBOUND="$(xray_dns_inbound_count)"
    DNS_OUT=no; has_dns_out && DNS_OUT=yes
    VLESS=no; has_vless && VLESS=yes
    DNS_ROUTING="$(dns_routing_mode)"
    if [ "$DNS_ROUTING" = split ] && [ "$NDM_INTERCEPT" != off ]; then
        DNS_ROUTING=split-intercept
    fi
    XPID="$(xray_pid)"; XRAY_RUNNING=no; XRAY_GID=unknown
    if [ -n "$XPID" ]; then XRAY_RUNNING=yes; XRAY_GID="$(xray_gid "$XPID")"; fi
    say "PROXY_DNS=$PROXY_DNS"
    say "PORT53_OWNER=$PORT53_OWNER"
    say "NDM_DNS_OVERRIDE=$NDM_OVERRIDE"
    say "NDM_FILTER_ENGINE=$NDM_ENGINE"
    say "NDM_DNS_INTERCEPT=$NDM_INTERCEPT"
    say "XRAY_DNS_INBOUND_COUNT=$DNS_INBOUND"
    say "DNS_ROUTING_MODE=$DNS_ROUTING"
    say "XRAY_RUNNING=$XRAY_RUNNING"
    say "XRAY_GID=$XRAY_GID"
    say "DNS_OUT=$DNS_OUT"
    say "VLESS_PROFILE=$VLESS"
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
        say 'EXPECTED_DELTA=Keenetic native DNS owns :53; no opkg dns-override; restore native filter engine and native system DNS intercept state; remove only FreeNet DNS inbound/dns-out/DNS routing; restore exact native 02_dns; normalize proxy_dns=off if required without unnecessary runtime restart; save NDM only after acceptance'
        say 'EXPECTED_NO_DELTA=no VPN credential rewrite; no subscription change; no non-DNS routing/inbound/outbound change; preserve Keenetic DNS profiles/DoT/DoH/client assignments/WAN DNS flags'
    elif [ "$SUPPORTED" = yes ] && [ "$EFFECTIVE_DNS" = xkeen ]; then
        say 'EXPECTED_DELTA=enable opkg dns-override; suppress native System DNS intercept while Split is active; set Keenetic filter engine opkg; Xray owns :53; ordered first-match DNS policy mirrors domain routing; one dns-out; dns-direct -> direct; dns-vless -> vless-reality; normalize proxy_dns=off after snapshot; save NDM only after acceptance'
        say 'EXPECTED_NO_DELTA=no VPN credential rewrite; no subscription change; no non-DNS routing/inbound/outbound change; preserve Keenetic DNS profiles/DoT/DoH/client assignments/WAN DNS flags; exact native intercept state is restored on Direct/rollback'
    else
        say 'EXPECTED_DELTA=NONE until a supported DNS mode is selected'
        say 'EXPECTED_NO_DELTA=all runtime state preserved'
    fi
    say 'MUTATION=NONE'
    say '========== END =========='
}

preflight_common() {
    for C in jq awk grep sed sort mktemp cp mv sha256sum; do command -v "$C" >/dev/null 2>&1 || { err "не найдена обязательная команда: $C"; return 1; }; done
    if [ "$TEST_MODE" != yes ]; then
        for C in netstat pidof nslookup ndmc; do command -v "$C" >/dev/null 2>&1 || { err "не найдена обязательная команда: $C"; return 1; }; done
    fi
    [ -x "$XKEEN_BIN" ] || { err 'XKeen не найден'; return 1; }
    [ -x "$XRAY_BIN" ] || { err 'Xray не найден'; return 1; }
    [ -d "$CONFIG_DIR" ] || { err 'каталог Xray config не найден'; return 1; }
    [ -d "$XRAY_ASSET_DIR" ] || { err 'каталог Xray assets не найден'; return 1; }
    # 02_dns may be native JSONC/comment-only; preserve it opaquely. 03-05 must be JSON.
    [ -f "$DNS_FILE" ] || { err 'не найден 02_dns.json'; return 1; }
    for F in "$INBOUND_FILE" "$OUT_FILE" "$ROUTING_FILE"; do [ -f "$F" ] && jq -e . "$F" >/dev/null 2>&1 || { err "невалидный обязательный Xray JSON: $F"; return 1; }; done
    INIT="$(xkeen_init 2>/dev/null || true)"; [ -n "$INIT" ] || { err 'init XKeen не найден'; return 1; }
    PROXY_DNS_INITIAL="$(proxy_dns_state)"; case "$PROXY_DNS_INITIAL" in off|on) : ;; *) err 'не удалось определить proxy_dns'; return 1 ;; esac
    NDM_OVERRIDE_INITIAL="$(ndm_override_state)"; case "$NDM_OVERRIDE_INITIAL" in off|on) : ;; *) err 'не удалось определить opkg dns-override'; return 1 ;; esac
    NDM_FILTER_ENGINE_INITIAL="$(ndm_filter_engine_state)"; ndm_filter_engine_token_ok "$NDM_FILTER_ENGINE_INITIAL" || { err 'не удалось определить Keenetic filter engine'; return 1; }
    NDM_INTERCEPT_INITIAL="$(ndm_intercept_state)"; case "$NDM_INTERCEPT_INITIAL" in off|on) : ;; *) err 'не удалось определить Keenetic DNS intercept'; return 1 ;; esac
    NDM_PROTECTED_HASH_INITIAL="$(ndm_protected_hash)"; [ -n "$NDM_PROTECTED_HASH_INITIAL" ] || { err 'не удалось снять protected NDM hash'; return 1; }
    XPID="$(xray_pid)"; XRAY_WAS_RUNNING=no
    if [ -n "$XPID" ]; then XRAY_WAS_RUNNING=yes; [ "$(xray_gid "$XPID")" = 11111 ] || { err 'Xray GID не равен 11111'; return 1; }; fi
}

snapshot_configs() {
    KIND="$1"; make_tmp || return 1
    STAMP="$(date +%Y%m%d-%H%M%S 2>/dev/null)"; [ -n "$STAMP" ] || STAMP="$$"
    BACKUP_DIR="$BACKUP_ROOT/freenet-network-$KIND-$STAMP"
    mkdir -p "$BACKUP_DIR" || return 1
    for NAME in 02_dns.json 03_inbounds.json 04_outbounds.json 05_routing.json; do cp -p "$CONFIG_DIR/$NAME" "$BACKUP_DIR/$NAME" || return 1; done
    INIT="$(xkeen_init 2>/dev/null || true)"; cp -p "$INIT" "$BACKUP_DIR/xkeen-init.before" || return 1
    ndm_running_config > "$BACKUP_DIR/ndm-running.before" 2>/dev/null || return 1
    ndm_protected_state > "$BACKUP_DIR/ndm-protected.before" 2>/dev/null || return 1
    printf '%s\n' "$NDM_OVERRIDE_INITIAL" > "$BACKUP_DIR/ndm-override.before"
    printf '%s\n' "$NDM_FILTER_ENGINE_INITIAL" > "$BACKUP_DIR/ndm-filter-engine.before"
    printf '%s\n' "$NDM_INTERCEPT_INITIAL" > "$BACKUP_DIR/ndm-intercept.before"
    CONFIG_SNAPSHOT_KIND="$KIND"
}

restore_files() {
    [ -n "$BACKUP_DIR" ] && [ -d "$BACKUP_DIR" ] || return 1
    for NAME in 02_dns.json 03_inbounds.json 04_outbounds.json 05_routing.json; do
        cp -p "$BACKUP_DIR/$NAME" "$CONFIG_DIR/$NAME.rollback.$$" || return 1
        mv -f "$CONFIG_DIR/$NAME.rollback.$$" "$CONFIG_DIR/$NAME" || return 1
    done
    INIT="$(xkeen_init 2>/dev/null || true)"; cp -p "$BACKUP_DIR/xkeen-init.before" "$INIT" || return 1
}

restore_runtime() {
    if [ "$NDM_OVERRIDE_INITIAL" = off ]; then
        ndm_set_filter_engine "$NDM_FILTER_ENGINE_INITIAL" || return 1
        ndm_set_override off || return 1
    else
        ndm_set_override on || return 1
        ndm_set_filter_engine "$NDM_FILTER_ENGINE_INITIAL" || return 1
    fi
    ndm_set_intercept "$NDM_INTERCEPT_INITIAL" || return 1
    if [ "$XRAY_WAS_RUNNING" = yes ]; then xkeen_runtime restart "/tmp/freenet-network-rollback.$$.log" || return 1; wait_for_xray yes || return 1
    else xkeen_runtime stop "/tmp/freenet-network-rollback.$$.log" || true; wait_for_xray no || return 1
    fi
    case "$NDM_OVERRIDE_INITIAL" in on) wait_port53_owner xray || return 1 ;; off) wait_port53_owner ndnproxy || return 1 ;; esac
    [ "$(proxy_dns_state)" = "$PROXY_DNS_INITIAL" ] || return 1
    [ "$(ndm_filter_engine_state)" = "$NDM_FILTER_ENGINE_INITIAL" ] || return 1
    [ "$(ndm_intercept_state)" = "$NDM_INTERCEPT_INITIAL" ] || return 1
    dns_query_ok || return 1
    [ "$(ndm_protected_hash)" = "$NDM_PROTECTED_HASH_INITIAL" ] || return 1
}

rollback_all() {
    restore_files || return 1
    restore_runtime || return 1
    return 0
}

rollback_files_only() {
    restore_files || return 1
    [ "$(proxy_dns_state)" = "$PROXY_DNS_INITIAL" ] || return 1
    [ "$(ndm_filter_engine_state)" = "$NDM_FILTER_ENGINE_INITIAL" ] || return 1
    [ "$(ndm_intercept_state)" = "$NDM_INTERCEPT_INITIAL" ] || return 1
    dns_query_ok || return 1
    [ "$(ndm_protected_hash)" = "$NDM_PROTECTED_HASH_INITIAL" ] || return 1
    return 0
}

preserve_native_dns_file() {
    mkdir -p "$NATIVE_STATE_DIR" || return 1
    cp -p "$DNS_FILE" "$NATIVE_STATE_DIR/02_dns.native.tmp.$$" || return 1
    mv -f "$NATIVE_STATE_DIR/02_dns.native.tmp.$$" "$NATIVE_STATE_DIR/02_dns.native" || return 1
    sha256sum "$NATIVE_STATE_DIR/02_dns.native" | awk '{print $1}' > "$NATIVE_STATE_DIR/02_dns.native.sha256" || return 1
}

preserve_native_filter_engine() {
    mkdir -p "$NATIVE_STATE_DIR" || return 1
    ndm_filter_engine_token_ok "$NDM_FILTER_ENGINE_INITIAL" || return 1
    printf '%s\n' "$NDM_FILTER_ENGINE_INITIAL" > "$NATIVE_STATE_DIR/filter-engine.native.tmp.$$" || return 1
    mv -f "$NATIVE_STATE_DIR/filter-engine.native.tmp.$$" "$NATIVE_STATE_DIR/filter-engine.native" || return 1
}

preserve_native_intercept() {
    mkdir -p "$NATIVE_STATE_DIR" || return 1
    case "$NDM_INTERCEPT_INITIAL" in on|off) : ;; *) return 1 ;; esac
    printf '%s\n' "$NDM_INTERCEPT_INITIAL" > "$NATIVE_STATE_DIR/intercept.native.tmp.$$" || return 1
    mv -f "$NATIVE_STATE_DIR/intercept.native.tmp.$$" "$NATIVE_STATE_DIR/intercept.native" || return 1
}

native_dns_file_valid() {
    [ -f "$NATIVE_STATE_DIR/02_dns.native" ] || return 1
    [ -f "$NATIVE_STATE_DIR/02_dns.native.sha256" ] || return 1
    EXPECTED="$(cat "$NATIVE_STATE_DIR/02_dns.native.sha256" 2>/dev/null)"
    ACTUAL="$(sha256sum "$NATIVE_STATE_DIR/02_dns.native" | awk '{print $1}')"
    [ -n "$EXPECTED" ] && [ "$EXPECTED" = "$ACTUAL" ]
}

native_filter_engine_value() {
    [ -f "$NATIVE_STATE_DIR/filter-engine.native" ] || return 1
    VALUE="$(sed -n '1p' "$NATIVE_STATE_DIR/filter-engine.native" 2>/dev/null | tr -d '\r\n')"
    ndm_filter_engine_token_ok "$VALUE" || return 1
    [ "$VALUE" != opkg ] || return 1
    printf '%s\n' "$VALUE"
}

native_intercept_value() {
    [ -f "$NATIVE_STATE_DIR/intercept.native" ] || return 1
    VALUE="$(sed -n '1p' "$NATIVE_STATE_DIR/intercept.native" 2>/dev/null | tr -d '\r\n')"
    case "$VALUE" in on|off) printf '%s\n' "$VALUE" ;; *) return 1 ;; esac
}

validate_preserve_hashes() {
    BEFORE="$1"; AFTER="$(non_dns_hashes)" || return 1
    [ "$BEFORE" = "$AFTER" ]
}

build_split_candidate() {
    C="$TMP_DIR/split"; mkdir -p "$C" || return 1
    DNS_SERVERS="$(jq -c '[
        .routing.rules[]?
        | select((.domain? | type) == "array" and (.domain | length) > 0)
        | if .outboundTag == "direct" then
            {address:"77.88.8.8",port:53,domains:.domain,skipFallback:true,finalQuery:true,tag:"dns-direct"}
          else
            {address:"https://8.8.8.8/dns-query",domains:.domain,skipFallback:true,finalQuery:true,tag:"dns-vless"}
          end
      ] + [{address:"https://8.8.8.8/dns-query",tag:"dns-vless",finalQuery:true}]' "$ROUTING_FILE")" || return 1
    jq -n --argjson servers "$DNS_SERVERS" '{dns:{tag:"dns-vless",servers:$servers,queryStrategy:"UseIPv4"}}' > "$C/02_dns.json" || return 1
    jq '.inbounds = ([.inbounds[]? | select((((.port // "") | tostring) != "53"))] + [{"tag":"dns","port":53,"protocol":"dokodemo-door","settings":{"network":"tcp,udp"}}])' "$INBOUND_FILE" > "$C/03_inbounds.json" || return 1
    jq '.outbounds = ([.outbounds[]? | select((.tag // "") != "dns-out")] + [{"protocol":"dns","tag":"dns-out"}])' "$OUT_FILE" > "$C/04_outbounds.json" || return 1
    jq '.routing.rules = ([{"type":"field","inboundTag":["dns-vless"],"outboundTag":"vless-reality"},{"type":"field","inboundTag":["dns-direct"],"outboundTag":"direct"},{"type":"field","port":53,"outboundTag":"dns-out"}] + [.routing.rules[]? | select((.outboundTag // "") != "dns-out") | select(((.inboundTag // []) | index("dns-vless")) == null) | select(((.inboundTag // []) | index("dns-direct")) == null) | select(((.inboundTag // []) | index("dns-in")) == null) | select((((.port // "") | tostring) != "53"))])' "$ROUTING_FILE" > "$C/05_routing.json" || return 1
    XRAY_LOCATION_ASSET="$XRAY_ASSET_DIR" "$XRAY_BIN" run -test -confdir "$C" > "$TMP_DIR/xray-split-candidate.log" 2>&1 || return 1
}

build_native_candidate() {
    native_dns_file_valid || { err 'нет проверенного native 02_dns snapshot; отказ от догадки'; return 1; }
    C="$TMP_DIR/native"; mkdir -p "$C" || return 1
    cp -p "$NATIVE_STATE_DIR/02_dns.native" "$C/02_dns.json" || return 1
    jq '.inbounds = [.inbounds[]? | select((((.port // "") | tostring) != "53"))]' "$INBOUND_FILE" > "$C/03_inbounds.json" || return 1
    jq '.outbounds = [.outbounds[]? | select((.tag // "") != "dns-out")]' "$OUT_FILE" > "$C/04_outbounds.json" || return 1
    jq '.routing.rules = [.routing.rules[]? | select((.outboundTag // "") != "dns-out") | select(((.inboundTag // []) | index("dns-vless")) == null) | select(((.inboundTag // []) | index("dns-direct")) == null) | select(((.inboundTag // []) | index("dns-in")) == null) | select((((.port // "") | tostring) != "53"))]' "$ROUTING_FILE" > "$C/05_routing.json" || return 1
    XRAY_LOCATION_ASSET="$XRAY_ASSET_DIR" "$XRAY_BIN" run -test -confdir "$C" > "$TMP_DIR/xray-native-candidate.log" 2>&1 || return 1
}

apply_candidate_dir() {
    C="$1"
    for NAME in 02_dns.json 03_inbounds.json 04_outbounds.json 05_routing.json; do cp -p "$C/$NAME" "$CONFIG_DIR/$NAME.freenet.$$" || return 1; done
    for NAME in 02_dns.json 03_inbounds.json 04_outbounds.json 05_routing.json; do mv -f "$CONFIG_DIR/$NAME.freenet.$$" "$CONFIG_DIR/$NAME" || return 1; done
}

split_success() {
    say '[FreeNet Network] RESULT=SUCCESS'
    say '[FreeNet Network] EFFECTIVE_DNS_MODE=xkeen'
    say '[FreeNet Network] NDM_DNS_OVERRIDE=on'
    say '[FreeNet Network] NDM_FILTER_ENGINE=opkg'
    say '[FreeNet Network] NDM_DNS_INTERCEPT=off'
    say '[FreeNet Network] PORT53_OWNER=xray'
    say '[FreeNet Network] DNS_ROUTING_MODE=split'
    say '[FreeNet Network] ROLLBACK=NOT_NEEDED'
}

apply_split() {
    preflight_common || { fail_not_applied 'Split DNS preflight failed before mutation'; return 1; }
    has_vless || { fail_not_applied 'Split DNS требует рабочий vless-reality'; return 1; }
    PRESERVE_BEFORE="$(non_dns_hashes)" || { fail_not_applied 'не удалось снять non-DNS preserve hashes'; return 1; }

    # Repair v0.2.43-style Split in place: OPKG/Xray already owns :53, but native
    # System DNS interception can still bypass Xray and leak the native resolver.
    if [ "$NDM_OVERRIDE_INITIAL" = on ]; then
        [ "$PROXY_DNS_INITIAL" = off ] || { fail_not_applied 'Split repair требует proxy_dns=off'; return 1; }
        [ "$NDM_FILTER_ENGINE_INITIAL" = opkg ] || { fail_not_applied 'Split repair требует Keenetic filter engine opkg'; return 1; }
        [ "$(port53_owner)" = xray ] || { fail_not_applied 'Split repair требует Xray на :53'; return 1; }
        [ "$(xray_dns_inbound_count)" = 1 ] && has_dns_out && [ "$(dns_routing_mode)" = split ] || { fail_not_applied 'Split repair: DNS topology неполна; STOP'; return 1; }
        if ! native_intercept_value >/dev/null 2>&1; then
            # v0.2.43 did not persist this control bit. Only the observed legacy
            # state "on" is safe to adopt as the native baseline; otherwise STOP.
            [ "$NDM_INTERCEPT_INITIAL" = on ] || { fail_not_applied 'нет native intercept snapshot, а legacy Split уже имеет intercept=off; отказ от догадки'; return 1; }
            preserve_native_intercept || { fail_not_applied 'не удалось сохранить legacy native intercept state'; return 1; }
        fi
        snapshot_configs split-repair || { fail_not_applied 'не удалось создать backup Split repair'; return 1; }
        make_tmp || { fail_not_applied 'не удалось создать временный каталог Split repair'; return 1; }
        build_split_candidate || { fail_not_applied 'candidate Split repair не прошёл validation'; return 1; }
        ndm_set_intercept off || { err 'PRIMARY ERROR: не удалось отключить native Keenetic DNS intercept'; rollback_all && err 'ROLLBACK ERROR/STATE: rollback success' || err 'ROLLBACK ERROR/STATE: FAILED/UNKNOWN'; return 1; }
        apply_candidate_dir "$TMP_DIR/split" || { err 'PRIMARY ERROR: не удалось применить Split repair candidate'; rollback_all && err 'ROLLBACK ERROR/STATE: rollback success' || err 'ROLLBACK ERROR/STATE: FAILED/UNKNOWN'; return 1; }
        xkeen_runtime restart "/tmp/freenet-network-split-repair.$$.log" || { err 'PRIMARY ERROR: XKeen/Xray restart failed during Split repair'; rollback_all && err 'ROLLBACK ERROR/STATE: rollback success' || err 'ROLLBACK ERROR/STATE: FAILED/UNKNOWN'; return 1; }
        wait_for_xray yes && wait_port53_owner xray || { err 'PRIMARY ERROR: Xray не подтвердил :53 после Split repair'; rollback_all && err 'ROLLBACK ERROR/STATE: rollback success' || err 'ROLLBACK ERROR/STATE: FAILED/UNKNOWN'; return 1; }
        [ "$(proxy_dns_state)" = off ] && [ "$(ndm_override_state)" = on ] && [ "$(ndm_filter_engine_state)" = opkg ] && [ "$(ndm_intercept_state)" = off ] && [ "$(xray_dns_inbound_count)" = 1 ] && has_dns_out && [ "$(dns_routing_mode)" = split ] && dns_query_ok || { err 'PRIMARY ERROR: post-repair Split DNS acceptance failed'; rollback_all && err 'ROLLBACK ERROR/STATE: rollback success' || err 'ROLLBACK ERROR/STATE: FAILED/UNKNOWN'; return 1; }
        validate_preserve_hashes "$PRESERVE_BEFORE" || { err 'PRIMARY ERROR: non-DNS Xray state changed during Split repair'; rollback_all && err 'ROLLBACK ERROR/STATE: rollback success' || err 'ROLLBACK ERROR/STATE: FAILED/UNKNOWN'; return 1; }
        [ "$(ndm_protected_hash)" = "$NDM_PROTECTED_HASH_INITIAL" ] || { err 'PRIMARY ERROR: Keenetic protected DNS/WAN state changed during Split repair'; rollback_all && err 'ROLLBACK ERROR/STATE: rollback success' || err 'ROLLBACK ERROR/STATE: FAILED/UNKNOWN'; return 1; }
        ndm_save || { err 'PRIMARY ERROR: NDM save failed after Split repair acceptance'; rollback_all && err 'ROLLBACK ERROR/STATE: rollback success' || err 'ROLLBACK ERROR/STATE: FAILED/UNKNOWN'; return 1; }
        split_success
        return 0
    fi

    [ "$NDM_FILTER_ENGINE_INITIAL" != opkg ] || { fail_not_applied 'native mode неожиданно использует Keenetic filter engine opkg'; return 1; }
    [ "$(port53_owner)" = ndnproxy ] || { fail_not_applied 'native mode должен иметь ndnproxy на :53'; return 1; }
    [ "$(xray_dns_inbound_count)" = 0 ] || { fail_not_applied 'native mode неожиданно содержит Xray :53 inbound'; return 1; }
    has_dns_out && { fail_not_applied 'native mode неожиданно содержит dns-out'; return 1; }
    [ "$(dns_routing_mode)" = native ] || { fail_not_applied 'native mode содержит DNS routing delta'; return 1; }
    snapshot_configs split || { fail_not_applied 'не удалось создать backup Split DNS'; return 1; }
    preserve_native_dns_file || { fail_not_applied 'не удалось сохранить native 02_dns'; return 1; }
    preserve_native_filter_engine || { fail_not_applied 'не удалось сохранить native Keenetic filter engine'; return 1; }
    preserve_native_intercept || { fail_not_applied 'не удалось сохранить native Keenetic DNS intercept state'; return 1; }
    make_tmp || { fail_not_applied 'не удалось создать временный каталог Split DNS'; return 1; }
    build_split_candidate || { fail_not_applied 'candidate Split DNS не прошёл validation'; return 1; }
    set_proxy_dns_off || { err 'PRIMARY ERROR: proxy_dns не удалось нормализовать в off'; rollback_all && err 'ROLLBACK ERROR/STATE: rollback success' || err 'ROLLBACK ERROR/STATE: FAILED/UNKNOWN'; return 1; }
    ndm_set_override on || { err 'PRIMARY ERROR: не удалось включить opkg dns-override'; rollback_all && err 'ROLLBACK ERROR/STATE: rollback success' || err 'ROLLBACK ERROR/STATE: FAILED/UNKNOWN'; return 1; }
    ndm_set_intercept off || { err 'PRIMARY ERROR: не удалось отключить native Keenetic DNS intercept'; rollback_all && err 'ROLLBACK ERROR/STATE: rollback success' || err 'ROLLBACK ERROR/STATE: FAILED/UNKNOWN'; return 1; }
    ndm_set_filter_engine opkg || { err 'PRIMARY ERROR: не удалось включить Keenetic filter engine opkg'; rollback_all && err 'ROLLBACK ERROR/STATE: rollback success' || err 'ROLLBACK ERROR/STATE: FAILED/UNKNOWN'; return 1; }
    apply_candidate_dir "$TMP_DIR/split" || { err 'PRIMARY ERROR: не удалось применить Split candidate'; rollback_all && err 'ROLLBACK ERROR/STATE: rollback success' || err 'ROLLBACK ERROR/STATE: FAILED/UNKNOWN'; return 1; }
    xkeen_runtime restart "/tmp/freenet-network-split-restart.$$.log" || { err 'PRIMARY ERROR: XKeen/Xray restart failed'; rollback_all && err 'ROLLBACK ERROR/STATE: rollback success' || err 'ROLLBACK ERROR/STATE: FAILED/UNKNOWN'; return 1; }
    wait_for_xray yes && wait_port53_owner xray || { err 'PRIMARY ERROR: Xray не стал владельцем :53'; rollback_all && err 'ROLLBACK ERROR/STATE: rollback success' || err 'ROLLBACK ERROR/STATE: FAILED/UNKNOWN'; return 1; }
    [ "$(proxy_dns_state)" = off ] && [ "$(ndm_override_state)" = on ] && [ "$(ndm_filter_engine_state)" = opkg ] && [ "$(ndm_intercept_state)" = off ] && [ "$(xray_dns_inbound_count)" = 1 ] && has_dns_out && [ "$(dns_routing_mode)" = split ] && dns_query_ok || { err 'PRIMARY ERROR: post-apply Split DNS acceptance failed'; rollback_all && err 'ROLLBACK ERROR/STATE: rollback success' || err 'ROLLBACK ERROR/STATE: FAILED/UNKNOWN'; return 1; }
    validate_preserve_hashes "$PRESERVE_BEFORE" || { err 'PRIMARY ERROR: non-DNS Xray state changed'; rollback_all && err 'ROLLBACK ERROR/STATE: rollback success' || err 'ROLLBACK ERROR/STATE: FAILED/UNKNOWN'; return 1; }
    [ "$(ndm_protected_hash)" = "$NDM_PROTECTED_HASH_INITIAL" ] || { err 'PRIMARY ERROR: Keenetic protected DNS/WAN state changed unexpectedly'; rollback_all && err 'ROLLBACK ERROR/STATE: rollback success' || err 'ROLLBACK ERROR/STATE: FAILED/UNKNOWN'; return 1; }
    ndm_save || { err 'PRIMARY ERROR: NDM save failed after acceptance'; rollback_all && err 'ROLLBACK ERROR/STATE: rollback success' || err 'ROLLBACK ERROR/STATE: FAILED/UNKNOWN'; return 1; }
    split_success
}

apply_native() {
    preflight_common || { fail_not_applied 'native DNS preflight failed before mutation'; return 1; }
    PRESERVE_BEFORE="$(non_dns_hashes)" || { fail_not_applied 'не удалось снять non-DNS preserve hashes'; return 1; }

    # A healthy existing native topology may still carry legacy proxy_dns=on in the init file.
    # Normalize only the persisted init value here; do not restart a working runtime merely to save ISP metadata.
    if [ "$NDM_OVERRIDE_INITIAL" = off ] && [ "$(port53_owner)" = ndnproxy ] && [ "$(xray_dns_inbound_count)" = 0 ] && ! has_dns_out && [ "$(dns_routing_mode)" = native ]; then
        [ "$NDM_FILTER_ENGINE_INITIAL" != opkg ] || { fail_not_applied 'native topology с Keenetic filter engine opkg неоднозначна; STOP'; return 1; }
        dns_query_ok || { fail_not_applied 'native Keenetic DNS query failed'; return 1; }
        if [ "$PROXY_DNS_INITIAL" = on ]; then
            snapshot_configs native-normalize || { fail_not_applied 'не удалось создать backup proxy_dns normalization'; return 1; }
            set_proxy_dns_off || { err 'PRIMARY ERROR: proxy_dns не удалось нормализовать в off'; rollback_files_only && err 'ROLLBACK ERROR/STATE: rollback success' || err 'ROLLBACK ERROR/STATE: FAILED/UNKNOWN'; return 1; }
            [ "$(proxy_dns_state)" = off ] && [ "$(ndm_filter_engine_state)" = "$NDM_FILTER_ENGINE_INITIAL" ] && [ "$(ndm_intercept_state)" = "$NDM_INTERCEPT_INITIAL" ] && dns_query_ok && validate_preserve_hashes "$PRESERVE_BEFORE" && [ "$(ndm_protected_hash)" = "$NDM_PROTECTED_HASH_INITIAL" ] || { err 'PRIMARY ERROR: native proxy_dns normalization acceptance failed'; rollback_files_only && err 'ROLLBACK ERROR/STATE: rollback success' || err 'ROLLBACK ERROR/STATE: FAILED/UNKNOWN'; return 1; }
        fi
        say '[FreeNet Network] RESULT=SUCCESS'
        say '[FreeNet Network] EFFECTIVE_DNS_MODE=firmware'
        say '[FreeNet Network] NDM_DNS_OVERRIDE=off'
        say "[FreeNet Network] NDM_FILTER_ENGINE=$NDM_FILTER_ENGINE_INITIAL"
        say "[FreeNet Network] NDM_DNS_INTERCEPT=$NDM_INTERCEPT_INITIAL"
        say '[FreeNet Network] PORT53_OWNER=ndnproxy'
        say '[FreeNet Network] DNS_ROUTING_MODE=native'
        say '[FreeNet Network] ROLLBACK=NOT_NEEDED'
        return 0
    fi

    [ "$PROXY_DNS_INITIAL" = off ] || { fail_not_applied 'partial/unknown DNS topology with proxy_dns=on; STOP'; return 1; }
    [ "$NDM_OVERRIDE_INITIAL" = on ] || { fail_not_applied 'partial/unknown DNS topology: native restore ожидает opkg dns-override=on'; return 1; }
    [ "$NDM_FILTER_ENGINE_INITIAL" = opkg ] || { fail_not_applied 'Split mode должен иметь Keenetic filter engine opkg'; return 1; }
    [ "$NDM_INTERCEPT_INITIAL" = off ] || { fail_not_applied 'Split mode имеет native DNS intercept; сначала требуется repair текущего XKeen/Xray DNS'; return 1; }
    [ "$(port53_owner)" = xray ] || { fail_not_applied 'Split mode должен иметь Xray на :53'; return 1; }
    [ "$(xray_dns_inbound_count)" = 1 ] && has_dns_out && [ "$(dns_routing_mode)" = split ] || { fail_not_applied 'Split topology неполна; STOP'; return 1; }
    NATIVE_ENGINE="$(native_filter_engine_value)" || { fail_not_applied 'нет проверенного native filter engine snapshot; отказ от догадки'; return 1; }
    NATIVE_INTERCEPT="$(native_intercept_value)" || { fail_not_applied 'нет проверенного native intercept snapshot; отказ от догадки'; return 1; }
    snapshot_configs native || { fail_not_applied 'не удалось создать backup native restore'; return 1; }
    make_tmp || { fail_not_applied 'не удалось создать временный каталог native restore'; return 1; }
    build_native_candidate || { fail_not_applied 'native candidate не прошёл validation'; return 1; }
    apply_candidate_dir "$TMP_DIR/native" || { err 'PRIMARY ERROR: не удалось применить native candidate'; rollback_all && err 'ROLLBACK ERROR/STATE: rollback success' || err 'ROLLBACK ERROR/STATE: FAILED/UNKNOWN'; return 1; }
    xkeen_runtime restart "/tmp/freenet-network-native-restart.$$.log" || { err 'PRIMARY ERROR: Xray restart после удаления DNS inbound failed'; rollback_all && err 'ROLLBACK ERROR/STATE: rollback success' || err 'ROLLBACK ERROR/STATE: FAILED/UNKNOWN'; return 1; }
    wait_for_xray yes || { err 'PRIMARY ERROR: Xray не поднялся в native candidate'; rollback_all && err 'ROLLBACK ERROR/STATE: rollback success' || err 'ROLLBACK ERROR/STATE: FAILED/UNKNOWN'; return 1; }
    ndm_set_filter_engine "$NATIVE_ENGINE" || { err 'PRIMARY ERROR: не удалось восстановить native Keenetic filter engine'; rollback_all && err 'ROLLBACK ERROR/STATE: rollback success' || err 'ROLLBACK ERROR/STATE: FAILED/UNKNOWN'; return 1; }
    ndm_set_override off || { err 'PRIMARY ERROR: не удалось отключить opkg dns-override'; rollback_all && err 'ROLLBACK ERROR/STATE: rollback success' || err 'ROLLBACK ERROR/STATE: FAILED/UNKNOWN'; return 1; }
    ndm_set_intercept "$NATIVE_INTERCEPT" || { err 'PRIMARY ERROR: не удалось восстановить native Keenetic DNS intercept state'; rollback_all && err 'ROLLBACK ERROR/STATE: rollback success' || err 'ROLLBACK ERROR/STATE: FAILED/UNKNOWN'; return 1; }
    wait_port53_owner ndnproxy || { err 'PRIMARY ERROR: ndnproxy не стал владельцем :53'; rollback_all && err 'ROLLBACK ERROR/STATE: rollback success' || err 'ROLLBACK ERROR/STATE: FAILED/UNKNOWN'; return 1; }
    [ "$(ndm_filter_engine_state)" = "$NATIVE_ENGINE" ] && [ "$(ndm_intercept_state)" = "$NATIVE_INTERCEPT" ] && [ "$(xray_dns_inbound_count)" = 0 ] && ! has_dns_out && [ "$(dns_routing_mode)" = native ] && dns_query_ok || { err 'PRIMARY ERROR: post-apply native DNS acceptance failed'; rollback_all && err 'ROLLBACK ERROR/STATE: rollback success' || err 'ROLLBACK ERROR/STATE: FAILED/UNKNOWN'; return 1; }
    validate_preserve_hashes "$PRESERVE_BEFORE" || { err 'PRIMARY ERROR: non-DNS Xray state changed'; rollback_all && err 'ROLLBACK ERROR/STATE: rollback success' || err 'ROLLBACK ERROR/STATE: FAILED/UNKNOWN'; return 1; }
    [ "$(ndm_protected_hash)" = "$NDM_PROTECTED_HASH_INITIAL" ] || { err 'PRIMARY ERROR: Keenetic protected DNS/WAN state changed unexpectedly'; rollback_all && err 'ROLLBACK ERROR/STATE: rollback success' || err 'ROLLBACK ERROR/STATE: FAILED/UNKNOWN'; return 1; }
    ndm_save || { err 'PRIMARY ERROR: NDM save failed after native acceptance'; rollback_all && err 'ROLLBACK ERROR/STATE: rollback success' || err 'ROLLBACK ERROR/STATE: FAILED/UNKNOWN'; return 1; }
    say '[FreeNet Network] RESULT=SUCCESS'
    say '[FreeNet Network] EFFECTIVE_DNS_MODE=firmware'
    say '[FreeNet Network] NDM_DNS_OVERRIDE=off'
    say "[FreeNet Network] NDM_FILTER_ENGINE=$NATIVE_ENGINE"
    say "[FreeNet Network] NDM_DNS_INTERCEPT=$NATIVE_INTERCEPT"
    say '[FreeNet Network] PORT53_OWNER=ndnproxy'
    say '[FreeNet Network] DNS_ROUTING_MODE=native'
    say '[FreeNet Network] ROLLBACK=NOT_NEEDED'
}

apply_profile() {
    resolve_profile
    [ "$SUPPORTED" = yes ] || { plan; fail_not_applied "$REASON"; return 1; }
    case "$EFFECTIVE_DNS" in
        firmware) say '[FreeNet Network] APPLY=firmware/native'; apply_native ;;
        xkeen) say '[FreeNet Network] APPLY=xkeen/opkg-split'; apply_split ;;
        *) fail_not_applied 'unsupported apply combination'; return 1 ;;
    esac
}

case "$MODE" in
    plan) plan ;;
    apply) apply_profile ;;
    *) err 'usage: apply_network_profile.sh [plan|apply]'; exit 2 ;;
esac
