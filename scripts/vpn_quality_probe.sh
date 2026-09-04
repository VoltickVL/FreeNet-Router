#!/bin/sh

CURL_BIN="${FREENET_CURL_BIN:-curl}"
PIDOF_BIN="${FREENET_PIDOF_BIN:-pidof}"
XRAY_BIN="${FREENET_XRAY_BIN:-/opt/sbin/xray}"
ASSET_DIR="${FREENET_ASSET_DIR:-/opt/etc/xray/dat}"
STATE_FILE="${FREENET_VPN_QUALITY_STATE_FILE:-/tmp/freenet-vpn-quality.state}"
DOWN_STREAK="${FREENET_VPN_QUALITY_DOWN_STREAK:-3}"
RTT_DEGRADED_MS="${FREENET_VPN_QUALITY_RTT_DEGRADED_MS:-1200}"
CONNECT_TIMEOUT="${FREENET_VPN_QUALITY_CONNECT_TIMEOUT:-4}"
MAX_TIME="${FREENET_VPN_QUALITY_MAX_TIME:-8}"
PROBE_URL="${FREENET_VPN_QUALITY_URL:-https://www.gstatic.com/generate_204}"
PROBE_BIN="${FREENET_VPN_QUALITY_PROBE_BIN:-}"
PORT_BASE="${FREENET_VPN_QUALITY_PORT_BASE:-11081}"

if [ -n "${FREENET_CONFIG_DIR:-}" ]; then
    CONFIG_DIR="$FREENET_CONFIG_DIR"
elif [ -d /opt/etc/xray/configs ]; then
    CONFIG_DIR=/opt/etc/xray/configs
elif [ -d /opkg/etc/xray/configs ]; then
    CONFIG_DIR=/opkg/etc/xray/configs
else
    CONFIG_DIR=/opt/etc/xray/configs
fi
OUT_FILE="$CONFIG_DIR/04_outbounds.json"

valid_positive_int() {
    case "$1" in ''|*[!0-9]*) return 1 ;; esac
    [ "$1" -gt 0 ] 2>/dev/null
}

state_value() {
    key="$1"
    [ -s "$STATE_FILE" ] || return 0
    awk -F= -v k="$key" '$1 == k {sub(/^[^=]*=/, ""); print; exit}' "$STATE_FILE" 2>/dev/null
}

emit() {
    printf 'VPN_PROCESS=%s\n' "$VPN_PROCESS"
    printf 'ENDPOINT_REACHABLE=%s\n' "$ENDPOINT_REACHABLE"
    printf 'VPN_ROUTE_OK=%s\n' "$VPN_ROUTE_OK"
    printf 'DNS_OK=%s\n' "$DNS_OK"
    printf 'VPN_QUALITY=%s\n' "$VPN_QUALITY"
    printf 'RTT_MS=%s\n' "$RTT_MS"
    printf 'FAILURE_STREAK=%s\n' "$FAILURE_STREAK"
    printf 'RECENT_FAILURE_RATIO=%s\n' "$RECENT_FAILURE_RATIO"
    printf 'LAST_SUCCESS=%s\n' "$LAST_SUCCESS"
    printf 'REASON=%s\n' "$REASON"
    printf 'MUTATION=NONE\n'
}

persist() {
    tmp="$STATE_FILE.tmp.$$"
    {
        printf 'VPN_PROCESS=%s\n' "$VPN_PROCESS"
        printf 'ENDPOINT_REACHABLE=%s\n' "$ENDPOINT_REACHABLE"
        printf 'VPN_ROUTE_OK=%s\n' "$VPN_ROUTE_OK"
        printf 'DNS_OK=%s\n' "$DNS_OK"
        printf 'VPN_QUALITY=%s\n' "$VPN_QUALITY"
        printf 'RTT_MS=%s\n' "$RTT_MS"
        printf 'FAILURE_STREAK=%s\n' "$FAILURE_STREAK"
        printf 'RECENT_FAILURE_RATIO=%s\n' "$RECENT_FAILURE_RATIO"
        printf 'LAST_SUCCESS=%s\n' "$LAST_SUCCESS"
        printf 'RECENT_RESULTS=%s\n' "$RECENT_RESULTS"
        printf 'REASON=%s\n' "$REASON"
    } > "$tmp" 2>/dev/null || return 1
    chmod 600 "$tmp" 2>/dev/null || true
    mv -f "$tmp" "$STATE_FILE" 2>/dev/null
}

update_history() {
    bit="$1"
    now="$(date +%s 2>/dev/null)"
    case "$now" in ''|*[!0-9]*) now=0 ;; esac
    old_streak="$(state_value FAILURE_STREAK)"
    case "$old_streak" in ''|*[!0-9]*) old_streak=0 ;; esac
    old_last="$(state_value LAST_SUCCESS)"
    case "$old_last" in ''|*[!0-9]*) old_last=0 ;; esac
    old_results="$(state_value RECENT_RESULTS)"
    case "$old_results" in *[!01]*) old_results="" ;; esac

    if [ "$bit" = 1 ]; then
        FAILURE_STREAK=0
        LAST_SUCCESS="$now"
    else
        FAILURE_STREAK=$((old_streak + 1))
        LAST_SUCCESS="$old_last"
    fi
    RECENT_RESULTS="$(printf '%s%s\n' "$old_results" "$bit" | awk '{s=$0; if (length(s)>5) s=substr(s,length(s)-4); print s}')"
    total=${#RECENT_RESULTS}
    failures="$(printf '%s' "$RECENT_RESULTS" | tr -cd '0' | wc -c | tr -d '[:space:]')"
    case "$failures" in ''|*[!0-9]*) failures=0 ;; esac
    if [ "$total" -gt 0 ] 2>/dev/null; then
        RECENT_FAILURE_RATIO=$((failures * 100 / total))
    else
        RECENT_FAILURE_RATIO=0
    fi
}

read_endpoint() {
    CURRENT_ADDRESS="$(jq -r '.outbounds[] | select(.tag == "vless-reality") | .settings.vnext[0].address // empty' "$OUT_FILE" 2>/dev/null | head -n 1)"
    CURRENT_PORT="$(jq -r '.outbounds[] | select(.tag == "vless-reality") | .settings.vnext[0].port // empty' "$OUT_FILE" 2>/dev/null | head -n 1)"
    [ -n "$CURRENT_ADDRESS" ] && [ -n "$CURRENT_PORT" ]
}

tcp_probe() {
    address="$1"
    port="$2"
    log="/tmp/freenet-vpn-quality-tcp.$$.log"
    case "$address" in *:*) host="[$address]" ;; *) host="$address" ;; esac
    rm -f "$log" 2>/dev/null || true
    "$CURL_BIN" --noproxy '*' -k -v -sS \
        --connect-timeout "$CONNECT_TIMEOUT" --max-time "$MAX_TIME" \
        -o /dev/null "https://$host:$port/" >/dev/null 2> "$log" || true
    if grep -F 'Connected to ' "$log" >/dev/null 2>&1; then
        rm -f "$log" 2>/dev/null || true
        return 0
    fi
    rm -f "$log" 2>/dev/null || true
    return 1
}

pick_port() {
    valid_positive_int "$PORT_BASE" || return 1
    port="$PORT_BASE"
    i=0
    while [ "$i" -lt 5 ]; do
        if ! netstat -lnt 2>/dev/null | awk '{print $4}' | grep -Eq "(^|[.:])${port}$"; then
            printf '%s\n' "$port"
            return 0
        fi
        port=$((port + 1))
        i=$((i + 1))
    done
    return 1
}

route_probe() {
    ROUTE_RTT_MS=unknown
    if [ -n "$PROBE_BIN" ]; then
        [ -x "$PROBE_BIN" ] || return 2
        output="$($PROBE_BIN 2>/dev/null)"
        rc=$?
        [ "$rc" -eq 0 ] || return "$rc"
        ROUTE_RTT_MS="$(printf '%s\n' "$output" | sed -n 's/^RTT_MS=\([0-9][0-9]*\)$/\1/p' | head -n 1)"
        [ -n "$ROUTE_RTT_MS" ] || ROUTE_RTT_MS=unknown
        return 0
    fi

    for tool in jq mktemp netstat grep awk sed; do
        command -v "$tool" >/dev/null 2>&1 || return 2
    done
    command -v "$CURL_BIN" >/dev/null 2>&1 || [ -x "$CURL_BIN" ] || return 2
    port="$(pick_port)" || return 2
    tmp="$(mktemp -d /tmp/freenet-vpn-quality.XXXXXX 2>/dev/null)" || return 2
    [ -n "$tmp" ] && [ -d "$tmp" ] || return 2
    chmod 700 "$tmp" 2>/dev/null || true
    cfg="$tmp/00_probe.json"
    log="$tmp/xray.log"

    if ! jq -n --slurpfile live "$OUT_FILE" --argjson port "$port" '
      {
        log:{loglevel:"warning"},
        inbounds:[{listen:"127.0.0.1",port:$port,protocol:"socks",settings:{udp:false},tag:"freenet-probe-socks"}],
        outbounds:[$live[0].outbounds[] | select(.tag == "vless-reality")],
        routing:{domainStrategy:"AsIs",rules:[{type:"field",inboundTag:["freenet-probe-socks"],outboundTag:"vless-reality"}]}
      }
    ' > "$cfg" 2>/dev/null; then
        rm -rf "$tmp" 2>/dev/null
        return 2
    fi
    chmod 600 "$cfg" 2>/dev/null || true
    if ! XRAY_LOCATION_ASSET="$ASSET_DIR" "$XRAY_BIN" run -test -confdir "$tmp" >/dev/null 2>&1; then
        rm -rf "$tmp" 2>/dev/null
        return 2
    fi

    XRAY_LOCATION_ASSET="$ASSET_DIR" "$XRAY_BIN" run -confdir "$tmp" > "$log" 2>&1 &
    probe_pid=$!
    ready=0
    i=0
    while [ "$i" -lt 4 ]; do
        if netstat -lnt 2>/dev/null | awk '{print $4}' | grep -Eq "(^|[.:])${port}$"; then ready=1; break; fi
        kill -0 "$probe_pid" 2>/dev/null || break
        sleep 1
        i=$((i + 1))
    done
    if [ "$ready" -ne 1 ]; then
        kill "$probe_pid" 2>/dev/null || true
        wait "$probe_pid" 2>/dev/null || true
        rm -rf "$tmp" 2>/dev/null
        return 2
    fi

    output="$($CURL_BIN --socks5-hostname "127.0.0.1:$port" -sS \
        --connect-timeout "$CONNECT_TIMEOUT" --max-time "$MAX_TIME" \
        -o /dev/null -w '%{http_code}\t%{time_total}\n' "$PROBE_URL" 2>/dev/null)"
    rc=$?
    kill "$probe_pid" 2>/dev/null || true
    wait "$probe_pid" 2>/dev/null || true
    rm -rf "$tmp" 2>/dev/null
    [ "$rc" -eq 0 ] || return 1

    http_code="$(printf '%s\n' "$output" | awk 'NR==1 {print $1}')"
    time_total="$(printf '%s\n' "$output" | awk 'NR==1 {print $2}')"
    case "$http_code" in 2??|3??|4??) ;; *) return 1 ;; esac
    ROUTE_RTT_MS="$(awk -v t="$time_total" 'BEGIN {if (t+0 >= 0) printf "%.0f", (t+0)*1000; else print "unknown"}')"
    case "$ROUTE_RTT_MS" in ''|*[!0-9]*) ROUTE_RTT_MS=unknown ;; esac
    return 0
}

VPN_PROCESS=UNKNOWN
ENDPOINT_REACHABLE=unknown
VPN_ROUTE_OK=unknown
DNS_OK=unknown
VPN_QUALITY=UNKNOWN
RTT_MS=unknown
FAILURE_STREAK="$(state_value FAILURE_STREAK)"
case "$FAILURE_STREAK" in ''|*[!0-9]*) FAILURE_STREAK=0 ;; esac
RECENT_FAILURE_RATIO="$(state_value RECENT_FAILURE_RATIO)"
case "$RECENT_FAILURE_RATIO" in ''|*[!0-9]*) RECENT_FAILURE_RATIO=0 ;; esac
LAST_SUCCESS="$(state_value LAST_SUCCESS)"
case "$LAST_SUCCESS" in ''|*[!0-9]*) LAST_SUCCESS=0 ;; esac
RECENT_RESULTS="$(state_value RECENT_RESULTS)"
case "$RECENT_RESULTS" in *[!01]*) RECENT_RESULTS="" ;; esac
REASON="baseline unavailable"

for value in "$DOWN_STREAK" "$RTT_DEGRADED_MS" "$CONNECT_TIMEOUT" "$MAX_TIME"; do
    valid_positive_int "$value" || { REASON="invalid quality probe policy"; emit; exit 2; }
done
[ -s "$OUT_FILE" ] || { REASON="current outbound is missing"; emit; exit 2; }
[ -x "$XRAY_BIN" ] || { REASON="Xray binary is missing"; emit; exit 2; }
[ -d "$ASSET_DIR" ] || { REASON="Xray asset directory is missing"; emit; exit 2; }
command -v "$PIDOF_BIN" >/dev/null 2>&1 || [ -x "$PIDOF_BIN" ] || { REASON="pidof tool is missing"; emit; exit 2; }

if ! "$PIDOF_BIN" xray >/dev/null 2>&1; then
    VPN_PROCESS=DOWN
    VPN_QUALITY=DOWN
    REASON="local Xray is offline"
    emit
    exit 1
fi
VPN_PROCESS=UP

if jq -e '([.outbounds[] | select(.tag == "dns-out")] | length) == 1' "$OUT_FILE" >/dev/null 2>&1; then DNS_OK=yes; else DNS_OK=no; fi
jq -e '((.outbounds|type)=="array") and (([.outbounds[]|select(.tag=="vless-reality")]|length)==1) and (([.outbounds[]|select(.tag=="dns-out")]|length)==1)' "$OUT_FILE" >/dev/null 2>&1 || {
    REASON="local Xray outbound baseline is invalid"; emit; exit 2;
}
XRAY_LOCATION_ASSET="$ASSET_DIR" "$XRAY_BIN" run -test -confdir "$CONFIG_DIR" >/dev/null 2>&1 || {
    REASON="live Xray configuration is invalid"; emit; exit 2;
}
read_endpoint || { REASON="current vless-reality endpoint is unavailable"; emit; exit 2; }

if tcp_probe "$CURRENT_ADDRESS" "$CURRENT_PORT"; then ENDPOINT_REACHABLE=yes; else ENDPOINT_REACHABLE=no; fi

if route_probe; then
    VPN_ROUTE_OK=yes
    RTT_MS="$ROUTE_RTT_MS"
    update_history 1
    if [ "$RTT_MS" != unknown ] && [ "$RTT_MS" -ge "$RTT_DEGRADED_MS" ] 2>/dev/null; then
        VPN_QUALITY=DEGRADED
        REASON="VPN route is working but RTT is high"
    else
        VPN_QUALITY=HEALTHY
        REASON="VPN route application probe succeeded"
    fi
else
    rc=$?
    if [ "$rc" -eq 2 ]; then
        VPN_ROUTE_OK=unknown
        VPN_QUALITY=UNKNOWN
        REASON="VPN route probe could not be constructed safely"
    else
        VPN_ROUTE_OK=no
        update_history 0
        if [ "$FAILURE_STREAK" -ge "$DOWN_STREAK" ] 2>/dev/null; then
            VPN_QUALITY=DOWN
            REASON="VPN route application probe repeatedly failed"
        elif [ "$ENDPOINT_REACHABLE" = yes ]; then
            VPN_QUALITY=DEGRADED
            REASON="endpoint TCP is reachable but VPN route application probe failed"
        else
            VPN_QUALITY=DEGRADED
            REASON="endpoint and VPN route probes failed; waiting for repeated confirmation"
        fi
    fi
fi
persist || true
emit
exit 0