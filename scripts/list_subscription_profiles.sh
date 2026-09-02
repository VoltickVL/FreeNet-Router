#!/bin/sh

# Read-only BlancVPN/VLESS subscription profile discovery.
# Output: one sanitized JSON object per Extra profile.
# Never prints the subscription URL, VLESS URI, UUID or Reality parameters.

SUB_FILE="${FREENET_SUB_FILE:-/opt/etc/xray/blanc_subscription.url}"
BOOTSTRAP_DNS_PRIMARY="77.88.8.8"
BOOTSTRAP_DNS_SECONDARY="8.8.8.8"
TMP_DIR=""
MAX_PROFILES=100

err() { printf '[FreeNet Profiles] ERROR: %s\n' "$*" >&2; }

cleanup() {
    [ -n "$TMP_DIR" ] && rm -rf "$TMP_DIR" 2>/dev/null || true
}
trap cleanup 0 1 2 15

url_decode() {
    printf '%b' "$(printf '%s' "$1" | sed 's/+/ /g; s/%/\\x/g')"
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

    if curl -4 -fsSL \
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
    curl -4 -fsSL \
        -H 'Cache-Control: no-cache' \
        -H 'Pragma: no-cache' \
        --resolve "$SUB_HOST:$SUB_PORT:$BOOTSTRAP_IP" \
        --connect-timeout 20 \
        --max-time 60 \
        -A 'Mozilla/5.0' \
        "$SUB_URL" > "$RAW_FILE" 2> "$CURL_ERR"
}

emit_profile() {
    LINE="$1"

    BODY="${LINE#vless://}"
    case "$BODY" in
        *@*) ;;
        *) return 0 ;;
    esac

    REST="${BODY#*@}"
    HOSTPORT="$(printf '%s\n' "$REST" | sed 's/[?].*$//')"
    case "$HOSTPORT" in
        \[*\]:*)
            ADDRESS="${HOSTPORT%]:*}]"
            ADDRESS="${ADDRESS#[}"
            PORT="${HOSTPORT##*:}"
            ;;
        *:*)
            ADDRESS="${HOSTPORT%:*}"
            PORT="${HOSTPORT##*:}"
            ;;
        *) return 0 ;;
    esac

    case "$PORT" in
        ''|*[!0-9]*) return 0 ;;
    esac
    [ "$PORT" -ge 1 ] 2>/dev/null && [ "$PORT" -le 65535 ] 2>/dev/null || return 0
    [ -n "$ADDRESS" ] || return 0

    case "$LINE" in
        *#*) NAME_ENC="${LINE##*#}" ;;
        *) NAME_ENC='Extra profile' ;;
    esac
    NAME="$(url_decode "$NAME_ENC" | tr '\r\n\t' '   ' | cut -c1-240)"
    [ -n "$NAME" ] || NAME='Extra profile'

    PROFILE_ID="$(printf '%s|%s|%s' "$NAME" "$ADDRESS" "$PORT" | sha256sum | awk '{print substr($1,1,16)}')"

    jq -cn \
        --arg id "$PROFILE_ID" \
        --arg name "$NAME" \
        --arg address "$ADDRESS" \
        --argjson port "$PORT" \
        '{id:$id,name:$name,address:$address,port:$port}'
}

for C in curl sed awk grep base64 mktemp sha256sum jq cut tr; do
    command -v "$C" >/dev/null 2>&1 || { err "required command missing: $C"; exit 1; }
done

[ -s "$SUB_FILE" ] || { err 'subscription is not configured'; exit 1; }
SUB_URL="$(tr -d '\r\n' < "$SUB_FILE")"
case "$SUB_URL" in
    https://*) SUB_PORT=443 ;;
    *) err 'subscription URL must use HTTPS'; exit 1 ;;
esac

SUB_REST="${SUB_URL#https://}"
SUB_AUTH="${SUB_REST%%/*}"
case "$SUB_AUTH" in
    *:*)
        SUB_HOST="${SUB_AUTH%%:*}"
        SUB_PORT="${SUB_AUTH##*:}"
        ;;
    *) SUB_HOST="$SUB_AUTH" ;;
esac
[ -n "$SUB_HOST" ] || { err 'subscription host is empty'; exit 1; }
case "$SUB_PORT" in ''|*[!0-9]*) err 'subscription port is invalid'; exit 1 ;; esac

TMP_DIR="$(mktemp -d /tmp/freenet-profiles.XXXXXX 2>/dev/null)"
if [ -z "$TMP_DIR" ] || [ ! -d "$TMP_DIR" ]; then
    TMP_DIR="/tmp/freenet-profiles.$$"
    mkdir -p "$TMP_DIR" || { err 'cannot create temporary directory'; exit 1; }
fi
RAW_FILE="$TMP_DIR/sub.raw"
DECODED_FILE="$TMP_DIR/sub.decoded"
MATCH_FILE="$TMP_DIR/extra.lines"
CURL_ERR="$TMP_DIR/curl.err"

fetch_subscription || { err 'subscription fetch failed'; exit 1; }
[ -s "$RAW_FILE" ] || { err 'subscription response is empty'; exit 1; }

if grep -q '^vless://' "$RAW_FILE"; then
    cp "$RAW_FILE" "$DECODED_FILE" || exit 1
else
    base64 -d "$RAW_FILE" > "$DECODED_FILE" 2>/dev/null || {
        err 'subscription is neither plain VLESS nor decodable base64'
        exit 1
    }
fi

tr -d '\r' < "$DECODED_FILE" \
    | grep '^vless://' \
    | grep -i 'Extra' \
    | grep -vi 'Expired' \
    | head -n "$MAX_PROFILES" > "$MATCH_FILE" || true

[ -s "$MATCH_FILE" ] || { err 'no active Extra profiles found'; exit 1; }

COUNT=0
while IFS= read -r LINE; do
    [ -n "$LINE" ] || continue
    SAFE_JSON="$(emit_profile "$LINE")"
    [ -n "$SAFE_JSON" ] || continue
    printf '%s\n' "$SAFE_JSON"
    COUNT=$((COUNT + 1))
done < "$MATCH_FILE"

[ "$COUNT" -gt 0 ] || { err 'no valid Extra endpoints found'; exit 1; }
