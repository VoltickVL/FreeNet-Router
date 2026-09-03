#!/bin/sh

# Transactional initial VLESS-Reality provider profile selection for FreeNet.
# Reads the local subscription secret but never prints it or credential fields.

SUB_FILE="${FREENET_SUB_FILE:-/opt/etc/xray/blanc_subscription.url}"
CONFIG_DIR="${FREENET_CONFIG_DIR:-/opt/etc/xray/configs}"
ASSET_DIR="${FREENET_ASSET_DIR:-/opt/etc/xray/dat}"
OUT_FILE="$CONFIG_DIR/04_outbounds.json"
PROFILE_FILE="${FREENET_PROFILE_FILE:-/opt/etc/freenet/vpn_profile_name}"
FILTER_FILE="${FREENET_FILTER_FILE:-/opt/etc/xray/blanc_profile_filter.regex}"
XRAY_BIN="${FREENET_XRAY_BIN:-/opt/sbin/xray}"
XKEEN_BIN="${FREENET_XKEEN_BIN:-/opt/sbin/xkeen}"
CURL_BIN="${FREENET_CURL_BIN:-curl}"
BOOTSTRAP_DNS_PRIMARY="77.88.8.8"
BOOTSTRAP_DNS_SECONDARY="8.8.8.8"
TMP_DIR=""
WAS_RUNNING=0
OUT_BEFORE_EXISTS=no
PROFILE_BEFORE_EXISTS=no
FILTER_BEFORE_EXISTS=no
APPLIED=0
ROLLBACK_ACTIVE=0

say() { printf '%s\n' "$*"; }
err() { printf '[FreeNet Provider] ERROR: %s\n' "$*" >&2; }

cleanup() {
    [ -n "$TMP_DIR" ] && rm -rf "$TMP_DIR" 2>/dev/null || true
}
trap cleanup 0 1 2 15

# POSIX-safe percent decoder. Do not rely on printf '\xHH': dash does not
# implement that extension, while BusyBox ash does. We need identical profile
# IDs in CI and on router runtime, including UTF-8 percent-encoded labels.
url_decode() {
    printf '%s' "$1" | awk '
        function hv(c) { return index("0123456789ABCDEF", toupper(c)) - 1 }
        {
            for (i = 1; i <= length($0); i++) {
                c = substr($0, i, 1)
                if (c == "+") {
                    printf " "
                    continue
                }
                if (c == "%" && i + 2 <= length($0)) {
                    a = hv(substr($0, i + 1, 1))
                    b = hv(substr($0, i + 2, 1))
                    if (a >= 0 && b >= 0) {
                        printf "%c", a * 16 + b
                        i += 2
                        continue
                    }
                }
                printf "%s", c
            }
        }
    '
}

get_param() {
    KEY="$1"
    printf '%s\n' "$QUERY" | tr '&' '\n' | sed -n "s/^${KEY}=//p" | head -n 1
}

bootstrap_ip() {
    HOST="$1"
    command -v nslookup >/dev/null 2>&1 || return 1
    for DNS in "$BOOTSTRAP_DNS_PRIMARY" "$BOOTSTRAP_DNS_SECONDARY"; do
        IP="$(nslookup "$HOST" "$DNS" 2>/dev/null | awk '
            /^Name:/ {seen=1; next}
            seen && /^Address [0-9]+:/ {if ($3 ~ /^[0-9]+\./) {print $3; exit}}
            seen && /^Address:/ {if ($2 ~ /^[0-9]+\./) {print $2; exit}}
        ')"
        [ -n "$IP" ] && { printf '%s\n' "$IP"; return 0; }
    done
    return 1
}

fetch_subscription() {
    : > "$RAW_FILE"
    if "$CURL_BIN" -4 -fsSL \
        -H 'Cache-Control: no-cache' \
        -H 'Pragma: no-cache' \
        --dns-servers "$BOOTSTRAP_DNS_PRIMARY,$BOOTSTRAP_DNS_SECONDARY" \
        --connect-timeout 20 \
        --max-time 60 \
        -A 'Mozilla/5.0' \
        "$SUB_URL" > "$RAW_FILE" 2> "$CURL_ERR"; then
        return 0
    fi

    BOOTSTRAP_IP="$(bootstrap_ip "$SUB_HOST")"
    [ -n "$BOOTSTRAP_IP" ] || return 1
    : > "$RAW_FILE"
    "$CURL_BIN" -4 -fsSL \
        -H 'Cache-Control: no-cache' \
        -H 'Pragma: no-cache' \
        --resolve "$SUB_HOST:$SUB_PORT:$BOOTSTRAP_IP" \
        --connect-timeout 20 \
        --max-time 60 \
        -A 'Mozilla/5.0' \
        "$SUB_URL" > "$RAW_FILE" 2> "$CURL_ERR"
}

sanitize_name() {
    NAME_IN="$1"
    NAME_OUT="$(printf '%s' "$NAME_IN" | tr '\r\n\t' '   ')"
    LOWER="$(printf '%s' "$NAME_OUT" | tr '[:upper:]' '[:lower:]')"
    case "$LOWER" in
        *vless://*|*https://*|*http://*|*uuid=*|*pbk=*|*sid=*|*publickey=*)
            printf '%s\n' 'Extra profile'
            ;;
        *)
            printf '%s\n' "$NAME_OUT"
            ;;
    esac
}

escape_ere() {
    awk '
        BEGIN { ORS="" }
        {
            for (i = 1; i <= length($0); i++) {
                c = substr($0, i, 1)
                if (c ~ /[][(){}.^$*+?|\\]/) printf "\\%s", c
                else printf "%s", c
            }
        }
    '
}

profile_id() {
    NAME="$1"
    ADDRESS="$2"
    PORT="$3"
    LOWER_ADDRESS="$(printf '%s' "$ADDRESS" | tr '[:upper:]' '[:lower:]')"
    printf '%s|%s|%s' "$NAME" "$LOWER_ADDRESS" "$PORT" | sha256sum | awk '{print substr($1,1,16)}'
}

parse_line_identity() {
    LINE="$1"
    BODY="${LINE#vless://}"
    case "$BODY" in *@*) ;; *) return 1 ;; esac
    REST="${BODY#*@}"
    HOSTPORT="$(printf '%s\n' "$REST" | sed 's/[?].*$//')"

    case "$HOSTPORT" in
        \[*\]:*)
            ADDRESS="${HOSTPORT%%]*}"
            ADDRESS="${ADDRESS#[}"
            PORT="${HOSTPORT##*:}"
            ;;
        *:*)
            ADDRESS="${HOSTPORT%:*}"
            PORT="${HOSTPORT##*:}"
            ;;
        *) return 1 ;;
    esac
    case "$PORT" in ''|*[!0-9]*) return 1 ;; esac
    [ "$PORT" -ge 1 ] 2>/dev/null && [ "$PORT" -le 65535 ] 2>/dev/null || return 1

    case "$LINE" in *#*) NAME_ENC="${LINE##*#}" ;; *) NAME_ENC='Extra profile' ;; esac
    NAME="$(sanitize_name "$(url_decode "$NAME_ENC")")"
    [ -n "$NAME" ] || NAME='Extra profile'
    ID="$(profile_id "$NAME" "$ADDRESS" "$PORT")"
    return 0
}

select_profile() {
    : > "$SELECTED_FILE"
    while IFS= read -r LINE; do
        [ -n "$LINE" ] || continue
        LOWER_LINE="$(printf '%s' "$LINE" | tr '[:upper:]' '[:lower:]')"
        case "$LOWER_LINE" in *vless://*extra*) ;; *) continue ;; esac
        case "$LOWER_LINE" in *expired*) continue ;; esac
        parse_line_identity "$LINE" || continue
        if [ "$ID" = "$REQUESTED_ID" ]; then
            printf '%s\n' "$LINE" > "$SELECTED_FILE"
            SELECTED_NAME="$NAME"
            SELECTED_ADDRESS="$ADDRESS"
            SELECTED_PORT="$PORT"
            return 0
        fi
    done < "$DECODED_FILE"
    return 1
}

build_vless_object() {
    VLESS="$(head -n 1 "$SELECTED_FILE")"
    BODY="${VLESS#vless://}"
    UUID="${BODY%%@*}"
    REST="${BODY#*@}"
    HOSTPORT="$(printf '%s\n' "$REST" | sed 's/[?].*$//')"
    QUERY_AND_NAME="$(printf '%s\n' "$REST" | sed 's/^[^?]*[?]//')"
    QUERY="${QUERY_AND_NAME%%#*}"

    FLOW="$(get_param flow)"
    SECURITY="$(get_param security)"
    TYPE="$(get_param type)"
    FP="$(get_param fp)"
    SNI="$(url_decode "$(get_param sni)")"
    PBK="$(get_param pbk)"
    SID="$(get_param sid)"
    SPX="$(url_decode "$(get_param spx)")"

    [ -n "$FLOW" ] || FLOW='xtls-rprx-vision'
    [ -n "$SECURITY" ] || SECURITY='reality'
    [ -n "$TYPE" ] || TYPE='tcp'
    [ -n "$FP" ] || FP='firefox'
    [ -n "$SPX" ] || SPX='/'

    MISSING=''
    [ -n "$UUID" ] || MISSING="$MISSING UUID"
    [ -n "$SELECTED_ADDRESS" ] || MISSING="$MISSING ADDRESS"
    [ -n "$SELECTED_PORT" ] || MISSING="$MISSING PORT"
    [ -n "$SNI" ] || MISSING="$MISSING SNI"
    [ -n "$PBK" ] || MISSING="$MISSING PBK"
    [ -n "$SID" ] || MISSING="$MISSING SID"
    [ -z "$MISSING" ] || { err "selected profile is missing required fields:$MISSING"; return 1; }

    jq -n \
        --arg address "$SELECTED_ADDRESS" \
        --argjson port "$SELECTED_PORT" \
        --arg uuid "$UUID" \
        --arg flow "$FLOW" \
        --arg network "$TYPE" \
        --arg security "$SECURITY" \
        --arg fingerprint "$FP" \
        --arg serverName "$SNI" \
        --arg publicKey "$PBK" \
        --arg shortId "$SID" \
        --arg spiderX "$SPX" \
        '{
          tag:"vless-reality",
          protocol:"vless",
          settings:{vnext:[{address:$address,port:$port,users:[{id:$uuid,flow:$flow,encryption:"none",level:0}]}]},
          streamSettings:{network:$network,security:$security,realitySettings:{fingerprint:$fingerprint,serverName:$serverName,publicKey:$publicKey,shortId:$shortId,spiderX:$spiderX}}
        }' > "$VLESS_OBJECT"
}

build_candidate() {
    if [ -f "$OUT_FILE" ] && jq -e '(.outbounds | type) == "array"' "$OUT_FILE" >/dev/null 2>&1; then
        jq --slurpfile replacement "$VLESS_OBJECT" '
          .outbounds as $old
          | ($old | map(select(.tag != "vless-reality"))) as $rest0
          | ($rest0 | if any(.[]; .tag == "direct") then . else . + [{tag:"direct",protocol:"freedom"}] end) as $rest1
          | ($rest1 | if any(.[]; .tag == "block") then . else . + [{tag:"block",protocol:"blackhole"}] end) as $rest2
          | .outbounds = ([$replacement[0]] + $rest2)
        ' "$OUT_FILE" > "$CANDIDATE_OUT" || return 1
    else
        jq -n --slurpfile replacement "$VLESS_OBJECT" '
          {outbounds:[$replacement[0],{tag:"direct",protocol:"freedom"},{tag:"block",protocol:"blackhole"}]}
        ' > "$CANDIDATE_OUT" || return 1
    fi

    jq -e '
      (.outbounds | type) == "array" and
      ([.outbounds[] | select(.tag == "vless-reality")] | length) == 1 and
      ([.outbounds[] | select(.tag == "direct")] | length) >= 1 and
      ([.outbounds[] | select(.tag == "block")] | length) >= 1
    ' "$CANDIDATE_OUT" >/dev/null 2>&1
}

validate_candidate() {
    mkdir -p "$TEST_CONF" || return 1
    FOUND=0
    for F in "$CONFIG_DIR"/*.json; do
        [ -f "$F" ] || continue
        FOUND=1
        cp "$F" "$TEST_CONF/" || return 1
    done
    [ "$FOUND" -eq 1 ] || return 1
    cp "$CANDIDATE_OUT" "$TEST_CONF/04_outbounds.json" || return 1
    XRAY_LOCATION_ASSET="$ASSET_DIR" "$XRAY_BIN" run -test -confdir "$TEST_CONF" > "$XRAY_TEST_LOG" 2>&1
}

snapshot_state() {
    if [ -f "$OUT_FILE" ]; then
        cp -p "$OUT_FILE" "$OUT_BEFORE" || return 1
        OUT_BEFORE_EXISTS=yes
    fi
    if [ -f "$PROFILE_FILE" ]; then
        cp -p "$PROFILE_FILE" "$PROFILE_BEFORE" || return 1
        PROFILE_BEFORE_EXISTS=yes
    fi
    if [ -f "$FILTER_FILE" ]; then
        cp -p "$FILTER_FILE" "$FILTER_BEFORE" || return 1
        FILTER_BEFORE_EXISTS=yes
    fi
    pidof xray >/dev/null 2>&1 && WAS_RUNNING=1 || WAS_RUNNING=0
}

restart_if_needed() {
    [ "$WAS_RUNNING" -eq 1 ] || return 0
    "$XKEEN_BIN" -restart >/dev/null 2>&1 || return 1
    sleep 4
    pidof xray >/dev/null 2>&1
}

rollback_state() {
    ROLLBACK_ACTIVE=1
    RB=0
    if [ "$OUT_BEFORE_EXISTS" = yes ]; then
        cp -p "$OUT_BEFORE" "$OUT_FILE.rollback.$$" 2>/dev/null || RB=1
        [ "$RB" -ne 0 ] || mv -f "$OUT_FILE.rollback.$$" "$OUT_FILE" 2>/dev/null || RB=1
    else
        rm -f "$OUT_FILE" 2>/dev/null || RB=1
    fi

    if [ "$PROFILE_BEFORE_EXISTS" = yes ]; then
        mkdir -p "$(dirname "$PROFILE_FILE")" 2>/dev/null || RB=1
        cp -p "$PROFILE_BEFORE" "$PROFILE_FILE.rollback.$$" 2>/dev/null || RB=1
        [ "$RB" -ne 0 ] || mv -f "$PROFILE_FILE.rollback.$$" "$PROFILE_FILE" 2>/dev/null || RB=1
    else
        rm -f "$PROFILE_FILE" 2>/dev/null || RB=1
    fi

    if [ "$FILTER_BEFORE_EXISTS" = yes ]; then
        mkdir -p "$(dirname "$FILTER_FILE")" 2>/dev/null || RB=1
        cp -p "$FILTER_BEFORE" "$FILTER_FILE.rollback.$$" 2>/dev/null || RB=1
        [ "$RB" -ne 0 ] || mv -f "$FILTER_FILE.rollback.$$" "$FILTER_FILE" 2>/dev/null || RB=1
    else
        rm -f "$FILTER_FILE" 2>/dev/null || RB=1
    fi

    if [ "$WAS_RUNNING" -eq 1 ]; then
        "$XKEEN_BIN" -restart >/dev/null 2>&1 || RB=1
        sleep 4
        pidof xray >/dev/null 2>&1 || RB=1
    fi
    ROLLBACK_ACTIVE=0
    [ "$RB" -eq 0 ]
}

fail_apply() {
    MESSAGE="$1"
    err "PRIMARY ERROR: $MESSAGE"
    if [ "$APPLIED" -eq 1 ] && [ "$ROLLBACK_ACTIVE" -eq 0 ]; then
        if rollback_state; then
            err 'ROLLBACK ERROR/STATE: rollback success'
            exit 1
        fi
        err 'ROLLBACK ERROR/STATE: FAILED/UNKNOWN'
        exit 2
    fi
    err 'ROLLBACK ERROR/STATE: no live apply'
    exit 1
}

MODE="${1:-plan}"
REQUESTED_ID="${2:-}"
case "$MODE" in plan|apply) ;; *) err 'usage: apply_provider_profile.sh [plan|apply] PROFILE_ID'; exit 2 ;; esac
case "$REQUESTED_ID" in
    ''|*[!0-9a-f]*) err 'PROFILE_ID must be 16 lowercase hex characters'; exit 2 ;;
esac
[ "${#REQUESTED_ID}" -eq 16 ] || { err 'PROFILE_ID must be 16 lowercase hex characters'; exit 2; }

for C in jq sed awk grep tr head base64 sha256sum mktemp cp mv mkdir dirname pidof; do
    command -v "$C" >/dev/null 2>&1 || { err "required command missing: $C"; exit 1; }
done
[ -x "$XRAY_BIN" ] || { err 'Xray binary is missing'; exit 1; }
[ -d "$CONFIG_DIR" ] || { err 'Xray config directory is missing'; exit 1; }
[ -d "$ASSET_DIR" ] || { err 'Xray asset directory is missing'; exit 1; }
[ -s "$SUB_FILE" ] || { err 'subscription is not configured'; exit 1; }

SUB_URL="$(tr -d '\r\n' < "$SUB_FILE")"
case "$SUB_URL" in https://*) SUB_PORT=443 ;; *) err 'subscription URL must use HTTPS'; exit 1 ;; esac
SUB_REST="${SUB_URL#https://}"
SUB_AUTH="${SUB_REST%%/*}"
case "$SUB_AUTH" in *:*) SUB_HOST="${SUB_AUTH%%:*}"; SUB_PORT="${SUB_AUTH##*:}" ;; *) SUB_HOST="$SUB_AUTH" ;; esac
[ -n "$SUB_HOST" ] || { err 'subscription host is empty'; exit 1; }
case "$SUB_PORT" in ''|*[!0-9]*) err 'subscription port is invalid'; exit 1 ;; esac

TMP_DIR="$(mktemp -d /tmp/freenet-provider.XXXXXX 2>/dev/null)"
[ -n "$TMP_DIR" ] && [ -d "$TMP_DIR" ] || { err 'cannot create temporary directory'; exit 1; }
RAW_FILE="$TMP_DIR/sub.raw"
DECODED_FILE="$TMP_DIR/sub.decoded"
SELECTED_FILE="$TMP_DIR/selected.vless"
VLESS_OBJECT="$TMP_DIR/vless.json"
CANDIDATE_OUT="$TMP_DIR/04_outbounds.json"
TEST_CONF="$TMP_DIR/conf"
XRAY_TEST_LOG="$TMP_DIR/xray-test.log"
CURL_ERR="$TMP_DIR/curl.err"
OUT_BEFORE="$TMP_DIR/out.before"
PROFILE_BEFORE="$TMP_DIR/profile.before"
FILTER_BEFORE="$TMP_DIR/filter.before"

fetch_subscription || { err 'subscription fetch failed'; exit 1; }
[ -s "$RAW_FILE" ] || { err 'subscription response is empty'; exit 1; }
if grep -q '^vless://' "$RAW_FILE"; then
    cp "$RAW_FILE" "$DECODED_FILE" || exit 1
else
    base64 -d "$RAW_FILE" > "$DECODED_FILE" 2>/dev/null || { err 'subscription decode failed'; exit 1; }
fi
tr -d '\r' < "$DECODED_FILE" > "$DECODED_FILE.clean" || exit 1
mv "$DECODED_FILE.clean" "$DECODED_FILE" || exit 1

select_profile || { err 'requested Extra profile is not present in the fresh subscription'; exit 1; }
build_vless_object || { err 'cannot build selected VLESS profile'; exit 1; }
build_candidate || { err 'cannot build candidate 04_outbounds.json'; exit 1; }
validate_candidate || { err 'candidate Xray configuration validation failed'; exit 1; }

say '========== FreeNet Provider Plan =========='
say "PROFILE_ID=$REQUESTED_ID"
say "PROFILE_NAME=$SELECTED_NAME"
say "ENDPOINT=$SELECTED_ADDRESS:$SELECTED_PORT"
if [ -f "$OUT_FILE" ]; then say 'CURRENT_OUTBOUND=present'; else say 'CURRENT_OUTBOUND=missing'; fi
if pidof xray >/dev/null 2>&1; then say 'XRAY_RUNNING=yes'; else say 'XRAY_RUNNING=no'; fi
say 'CANDIDATE_XRAY_VALID=yes'
say 'EXPECTED_DELTA=install or replace exactly one vless-reality outbound; preserve existing non-VLESS outbounds; persist safe preferred profile name and exact active profile filter'
say 'EXPECTED_NO_DELTA=subscription URL and VLESS/Reality credentials are never printed; ISP/DNS/routing are not changed by this helper'
say "MUTATION=$( [ "$MODE" = apply ] && printf 'PENDING' || printf 'NONE' )"
say '========== END =========='

[ "$MODE" = plan ] && exit 0

snapshot_state || fail_apply 'cannot snapshot current provider state'
APPLIED=1
mkdir -p "$(dirname "$PROFILE_FILE")" || fail_apply 'cannot create FreeNet config directory'
mkdir -p "$(dirname "$FILTER_FILE")" || fail_apply 'cannot create profile filter directory'
cp "$CANDIDATE_OUT" "$OUT_FILE.new.$$" || fail_apply 'cannot stage outbound candidate'
chmod 600 "$OUT_FILE.new.$$" 2>/dev/null || true
mv -f "$OUT_FILE.new.$$" "$OUT_FILE" || fail_apply 'cannot commit outbound candidate'
printf '%s\n' "$SELECTED_NAME" > "$PROFILE_FILE.new.$$" || fail_apply 'cannot stage preferred profile'
chmod 600 "$PROFILE_FILE.new.$$" 2>/dev/null || true
mv -f "$PROFILE_FILE.new.$$" "$PROFILE_FILE" || fail_apply 'cannot commit preferred profile'
printf '%s\n' "$SELECTED_NAME" | escape_ere > "$FILTER_FILE.new.$$" || fail_apply 'cannot stage exact active profile filter'
chmod 644 "$FILTER_FILE.new.$$" 2>/dev/null || true
mv -f "$FILTER_FILE.new.$$" "$FILTER_FILE" || fail_apply 'cannot commit exact active profile filter'

restart_if_needed || fail_apply 'Xray/XKeen runtime acceptance failed after provider apply'
XRAY_LOCATION_ASSET="$ASSET_DIR" "$XRAY_BIN" run -test -confdir "$CONFIG_DIR" > "$XRAY_TEST_LOG" 2>&1 \
    || fail_apply 'live Xray configuration validation failed after provider apply'

say '[FreeNet Provider] RESULT=SUCCESS'
say '[FreeNet Provider] ROLLBACK=NOT_NEEDED'