#!/opt/usr/bin/sh

SUB_FILE="${FREENET_SUB_FILE:-/opt/etc/xray/blanc_subscription.url}"
FILTER_FILE="${FREENET_FILTER_FILE:-/opt/etc/xray/blanc_profile_filter.regex}"
CONFIG_DIR="${FREENET_CONFIG_DIR:-/opt/etc/xray/configs}"
ASSET_DIR="${FREENET_ASSET_DIR:-/opt/etc/xray/dat}"
OUT_FILE="$CONFIG_DIR/04_outbounds.json"
BACKUP_FILE="$CONFIG_DIR/04_outbounds.json.bak"
LOCK_DIR="${FREENET_LOCK_DIR:-/tmp/blanc_xkeen_update.lock}"
LOG_PREFIX="[blanc-xkeen]"
BOOTSTRAP_DNS_PRIMARY="77.88.8.8"
BOOTSTRAP_DNS_SECONDARY="8.8.8.8"
ACTION_REASON="${FREENET_ACTION_REASON:-refresh}"
FILTER_OVERRIDE="${FREENET_FILTER_OVERRIDE:-}"
PROFILE_LABEL="${FREENET_PROFILE_LABEL:-}"

LOCK_HELD=0
TMP_DIR=""
FILTER_BEFORE_EXISTS=no

log() {
    echo "$LOG_PREFIX $*"
}

fail() {
    log "ERROR: $*"
    exit 1
}

cleanup() {
    [ -n "$TMP_DIR" ] && rm -rf "$TMP_DIR" 2>/dev/null
    if [ "$LOCK_HELD" = "1" ]; then
        rm -rf "$LOCK_DIR" 2>/dev/null
    fi
}

trap cleanup 0
trap 'exit 130' 1 2 15

acquire_lock() {
    if mkdir "$LOCK_DIR" 2>/dev/null; then
        LOCK_HELD=1
        echo "$$" > "$LOCK_DIR/pid"
        return 0
    fi

    sleep 1
    OLD_PID="$(cat "$LOCK_DIR/pid" 2>/dev/null)"

    if [ -n "$OLD_PID" ] && kill -0 "$OLD_PID" 2>/dev/null; then
        fail "another updater instance is already running (pid $OLD_PID)"
    fi

    rm -rf "$LOCK_DIR" 2>/dev/null || fail "cannot remove stale updater lock"
    mkdir "$LOCK_DIR" 2>/dev/null || fail "cannot acquire updater lock"

    LOCK_HELD=1
    echo "$$" > "$LOCK_DIR/pid"
}

url_decode() {
    printf '%b' "$(printf '%s' "$1" | sed 's/+/ /g; s/%/\\x/g')"
}

get_param() {
    key="$1"
    printf '%s\n' "$QUERY" \
        | tr '&' '\n' \
        | sed -n "s/^${key}=//p" \
        | head -n 1
}

show_extra_profiles() {
    log "available Extra profile names:"

    grep '^vless://' "$DECODED_FILE" 2>/dev/null \
        | grep -i 'Extra' \
        | grep -vi 'Expired' \
        | sed -n 's/^.*#/#/p' \
        | head -n 30
}

restart_xkeen() {
    if [ -n "${FREENET_XKEEN_BIN:-}" ]; then
        "$FREENET_XKEEN_BIN" -restart
        return $?
    fi

    if [ -x /opt/sbin/xkeen ]; then
        /opt/sbin/xkeen -restart
        return $?
    fi

    if command -v xkeen >/dev/null 2>&1; then
        xkeen -restart
        return $?
    fi

    return 1
}

snapshot_filter() {
    FILTER_BEFORE_EXISTS=no
    if [ -f "$FILTER_FILE" ]; then
        cp -p "$FILTER_FILE" "$FILTER_BEFORE" \
            || fail "cannot snapshot current profile filter"
        FILTER_BEFORE_EXISTS=yes
    fi
}

restore_filter() {
    if [ "$FILTER_BEFORE_EXISTS" = yes ]; then
        cp -p "$FILTER_BEFORE" "$FILTER_FILE.restore.$$" 2>/dev/null \
            || return 1
        mv -f "$FILTER_FILE.restore.$$" "$FILTER_FILE" 2>/dev/null \
            || return 1
    else
        rm -f "$FILTER_FILE" 2>/dev/null || return 1
    fi
    return 0
}

persist_filter() {
    [ -n "$FILTER_OVERRIDE" ] || return 0

    FILTER_DIR="$(dirname "$FILTER_FILE")"
    mkdir -p "$FILTER_DIR" 2>/dev/null || return 1

    printf '%s\n' "$FILTER" > "$FILTER_FILE.new.$$" \
        || return 1
    chmod 644 "$FILTER_FILE.new.$$" 2>/dev/null || true
    mv -f "$FILTER_FILE.new.$$" "$FILTER_FILE" \
        || return 1
    return 0
}

rollback_state() {
    log "ROLLBACK: restoring previous 04_outbounds.json and profile filter"

    ROLLBACK_STAGE="$CONFIG_DIR/.04_outbounds.json.rollback.$$"

    cp -p "$BACKUP_FILE" "$ROLLBACK_STAGE" 2>/dev/null || {
        log "ROLLBACK ERROR: cannot stage outbound backup"
        return 1
    }

    mv -f "$ROLLBACK_STAGE" "$OUT_FILE" 2>/dev/null || {
        rm -f "$ROLLBACK_STAGE" 2>/dev/null
        log "ROLLBACK ERROR: cannot restore live outbound"
        return 1
    }

    if ! restore_filter; then
        log "ROLLBACK ERROR: cannot restore profile filter"
        return 1
    fi

    restart_xkeen
    sleep 5

    if pidof xray >/dev/null 2>&1; then
        log "ROLLBACK: Xray/filter restored"
        return 0
    fi

    log "ROLLBACK ERROR: Xray is not running after restore"
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
        -A "Mozilla/5.0" \
        "$SUB_URL" > "$RAW_FILE" 2> "$CURL_ERR"; then

        log "subscription fetch: explicit DNS OK"
        return 0
    fi

    for DNS_SERVER in "$BOOTSTRAP_DNS_PRIMARY" "$BOOTSTRAP_DNS_SECONDARY"; do

        BOOTSTRAP_IP="$(
            nslookup "$SUB_HOST" "$DNS_SERVER" 2>/dev/null \
                | awk '/^Name:/{seen=1; next} seen && /^Address [0-9]+:/ {if ($3 ~ /^[0-9]+\./) {print $3; exit}}'
        )"

        case "$BOOTSTRAP_IP" in
            ''|*[!0-9.]*) continue ;;
        esac

        : > "$RAW_FILE"

        if curl -4 -fsSL \
            -H 'Cache-Control: no-cache' \
            -H 'Pragma: no-cache' \
            --resolve "$SUB_HOST:$SUB_PORT:$BOOTSTRAP_IP" \
            --connect-timeout 20 \
            --max-time 60 \
            -A "Mozilla/5.0" \
            "$SUB_URL" > "$RAW_FILE" 2> "$CURL_ERR"; then

            log "subscription fetch: bootstrap DNS $DNS_SERVER OK"
            return 0
        fi
    done

    return 1
}

case "$ACTION_REASON" in
    refresh|switch|rotate|failover) ;;
    *) fail "unsupported action reason: $ACTION_REASON" ;;
esac

acquire_lock

[ -d "$CONFIG_DIR" ] || fail "Xray configs dir not found: $CONFIG_DIR"
[ -d "$ASSET_DIR" ] || fail "Xray asset dir not found: $ASSET_DIR"
[ -f "$SUB_FILE" ] || fail "subscription file not found: $SUB_FILE"
[ -f "$OUT_FILE" ] || fail "live outbound file not found: $OUT_FILE"

for tool in jq curl base64 mktemp cmp sha256sum nslookup; do
    command -v "$tool" >/dev/null 2>&1 || fail "required tool not found: $tool"
done

TMP_DIR="$(mktemp -d /tmp/blanc_xkeen_update.XXXXXX 2>/dev/null)"

[ -n "$TMP_DIR" ] && [ -d "$TMP_DIR" ] \
    || fail "cannot create temporary directory"

RAW_FILE="$TMP_DIR/sub_raw.txt"
DECODED_FILE="$TMP_DIR/sub_decoded.txt"
MATCHES_FILE="$TMP_DIR/matching_vless.txt"
VLESS_FILE="$TMP_DIR/selected_vless.txt"
VLESS_OBJECT="$TMP_DIR/vless_object.json"
NEW_FILE="$TMP_DIR/04_outbounds.json"
OLD_NON_VLESS="$TMP_DIR/old_non_vless.json"
NEW_NON_VLESS="$TMP_DIR/new_non_vless.json"
OLD_NORMALIZED="$TMP_DIR/old_normalized.json"
NEW_NORMALIZED="$TMP_DIR/new_normalized.json"
CURL_ERR="$TMP_DIR/curl.err"
TEST_CONF="$TMP_DIR/conf"
XRAY_TEST_LOG="$TMP_DIR/xray-test.log"
FILTER_BEFORE="$TMP_DIR/filter.before"

snapshot_filter

SUB_URL="$(tr -d '\r\n' < "$SUB_FILE")"

[ -n "$SUB_URL" ] || fail "empty subscription URL"

case "$SUB_URL" in
    https://*)
        DEFAULT_PORT="443"
        ;;
    http://*)
        DEFAULT_PORT="80"
        ;;
    *)
        fail "unsupported subscription URL scheme"
        ;;
esac

SUB_REST="${SUB_URL#*://}"
SUB_AUTH="${SUB_REST%%/*}"

case "$SUB_AUTH" in
    *:*)
        SUB_HOST="${SUB_AUTH%%:*}"
        SUB_PORT="${SUB_AUTH##*:}"
        ;;
    *)
        SUB_HOST="$SUB_AUTH"
        SUB_PORT="$DEFAULT_PORT"
        ;;
esac

[ -n "$SUB_HOST" ] || fail "subscription hostname is empty"

case "$SUB_PORT" in
    ''|*[!0-9]*)
        fail "subscription port is invalid"
        ;;
esac

if ! jq -e '
    ((.outbounds | type) == "array") and
    (([.outbounds[] | select(.tag == "vless-reality")] | length) == 1)
' "$OUT_FILE" >/dev/null 2>&1; then
    fail "live 04_outbounds.json must contain exactly one vless-reality"
fi

log "action reason: $ACTION_REASON"
log "fetching subscription..."

fetch_subscription \
    || fail "failed to fetch subscription through explicit/bootstrap DNS"

[ -s "$RAW_FILE" ] || fail "downloaded subscription is empty"

if grep -q '^vless://' "$RAW_FILE"; then
    cp "$RAW_FILE" "$DECODED_FILE" \
        || fail "cannot prepare decoded subscription"
else
    base64 -d "$RAW_FILE" > "$DECODED_FILE" 2>/dev/null \
        || cp "$RAW_FILE" "$DECODED_FILE"
fi

tr -d '\r' < "$DECODED_FILE" > "$DECODED_FILE.clean" \
    || fail "cannot normalize subscription"

mv "$DECODED_FILE.clean" "$DECODED_FILE" \
    || fail "cannot finalize normalized subscription"

grep -q '^vless://' "$DECODED_FILE" \
    || fail "no vless profiles found in subscription"

if [ -n "$FILTER_OVERRIDE" ]; then
    FILTER="$FILTER_OVERRIDE"
elif [ -f "$FILTER_FILE" ] && [ -s "$FILTER_FILE" ]; then
    FILTER="$(tr -d '\r\n' < "$FILTER_FILE")"
else
    FILTER="Warsaw|Warszawa|Poland|Polska|Варшава|Польша"
fi

[ -n "$FILTER" ] || fail "empty profile filter"

log "using requested filter"
[ -n "$PROFILE_LABEL" ] && log "requested profile: $PROFILE_LABEL"

grep '^vless://' "$DECODED_FILE" \
    | grep -i 'Extra' \
    | grep -vi 'Expired' \
    | grep -Ei "$FILTER" > "$MATCHES_FILE" || true

MATCH_COUNT="$(wc -l < "$MATCHES_FILE" | tr -d '[:space:]')"
case "$MATCH_COUNT" in
    ''|*[!0-9]*) MATCH_COUNT=0 ;;
esac
log "matching Extra candidates: $MATCH_COUNT"

head -n 1 "$MATCHES_FILE" > "$VLESS_FILE"

if [ ! -s "$VLESS_FILE" ]; then
    log "ERROR: requested Extra profile not found"
    show_extra_profiles
    exit 1
fi

VLESS="$(head -n 1 "$VLESS_FILE")"

case "$VLESS" in
    *#*)
        PROFILE_NAME="$(url_decode "${VLESS##*#}")"
        ;;
    *)
        PROFILE_NAME="selected Extra profile"
        ;;
esac

log "selected profile: $PROFILE_NAME"

BODY="${VLESS#vless://}"
UUID="${BODY%%@*}"
REST="${BODY#*@}"

HOSTPORT="$(printf '%s\n' "$REST" | sed 's/[?].*$//')"
QUERY_AND_NAME="$(printf '%s\n' "$REST" | sed 's/^[^?]*[?]//')"
QUERY="${QUERY_AND_NAME%%#*}"

case "$HOSTPORT" in
    *:*)
        ADDRESS="${HOSTPORT%:*}"
        PORT="${HOSTPORT##*:}"
        ;;
    *)
        fail "selected VLESS profile has invalid endpoint"
        ;;
esac

FLOW="$(get_param flow)"
SECURITY="$(get_param security)"
TYPE="$(get_param type)"
FP="$(get_param fp)"
SNI="$(get_param sni)"
PBK="$(get_param pbk)"
SID="$(get_param sid)"
SPX_RAW="$(get_param spx)"
SPX="$(url_decode "$SPX_RAW")"

[ -n "$FLOW" ] || FLOW="xtls-rprx-vision"
[ -n "$TYPE" ] || TYPE="tcp"
[ -n "$SECURITY" ] || SECURITY="reality"
[ -n "$FP" ] || FP="firefox"
[ -n "$SPX" ] || SPX="/"

MISSING=""

[ -n "$UUID" ] || MISSING="$MISSING UUID"
[ -n "$ADDRESS" ] || MISSING="$MISSING ADDRESS"
[ -n "$PORT" ] || MISSING="$MISSING PORT"
[ -n "$PBK" ] || MISSING="$MISSING PBK"
[ -n "$SID" ] || MISSING="$MISSING SID"
[ -n "$SNI" ] || MISSING="$MISSING SNI"

if [ -n "$MISSING" ]; then
    fail "selected VLESS profile is missing required fields:$MISSING"
fi

case "$PORT" in
    ''|*[!0-9]*)
        fail "selected VLESS profile has invalid port"
        ;;
esac

if [ "$PORT" -lt 1 ] 2>/dev/null \
    || [ "$PORT" -gt 65535 ] 2>/dev/null; then
    fail "selected VLESS profile has out-of-range port"
fi

OLD_IP="$(
    jq -r '
        .outbounds[]
        | select(.tag == "vless-reality")
        | .settings.vnext[0].address // empty
    ' "$OUT_FILE" | head -n 1
)"

OLD_PORT="$(
    jq -r '
        .outbounds[]
        | select(.tag == "vless-reality")
        | .settings.vnext[0].port // empty
    ' "$OUT_FILE" | head -n 1
)"

NEW_IP="$ADDRESS"
NEW_PORT="$PORT"

log "endpoint candidate: $NEW_IP:$NEW_PORT"

jq -n \
    --arg address "$ADDRESS" \
    --argjson port "$PORT" \
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
      "tag": "vless-reality",
      "protocol": "vless",
      "settings": {
        "vnext": [
          {
            "address": $address,
            "port": $port,
            "users": [
              {
                "id": $uuid,
                "flow": $flow,
                "encryption": "none",
                "level": 0
              }
            ]
          }
        ]
      },
      "streamSettings": {
        "network": $network,
        "security": $security,
        "realitySettings": {
          "fingerprint": $fingerprint,
          "serverName": $serverName,
          "publicKey": $publicKey,
          "shortId": $shortId,
          "spiderX": $spiderX
        }
      }
    }' > "$VLESS_OBJECT" \
    || fail "cannot generate VLESS outbound object"

PROFILE_FP="$(sha256sum "$VLESS_OBJECT" | awk '{print substr($1,1,12)}')"
log "candidate fingerprint: $PROFILE_FP"

jq --slurpfile replacement "$VLESS_OBJECT" '
    .outbounds |= map(
        if .tag == "vless-reality"
        then $replacement[0]
        else .
        end
    )
' "$OUT_FILE" > "$NEW_FILE" \
    || fail "cannot generate candidate 04_outbounds.json"

if ! jq -e '
    ((.outbounds | type) == "array") and
    (([.outbounds[] | select(.tag == "vless-reality")] | length) == 1)
' "$NEW_FILE" >/dev/null 2>&1; then
    fail "candidate outbound structure is invalid"
fi

jq -S '
    [.outbounds[] | select(.tag != "vless-reality")]
' "$OUT_FILE" > "$OLD_NON_VLESS" \
    || fail "cannot normalize live non-VLESS outbounds"

jq -S '
    [.outbounds[] | select(.tag != "vless-reality")]
' "$NEW_FILE" > "$NEW_NON_VLESS" \
    || fail "cannot normalize candidate non-VLESS outbounds"

cmp "$OLD_NON_VLESS" "$NEW_NON_VLESS" >/dev/null 2>&1 \
    || fail "candidate unexpectedly changes non-VLESS outbounds"

mkdir -p "$TEST_CONF" \
    || fail "cannot create temporary Xray confdir"

cp "$CONFIG_DIR"/*.json "$TEST_CONF/" \
    || fail "cannot copy Xray configs for validation"

cp "$NEW_FILE" "$TEST_CONF/04_outbounds.json" \
    || fail "cannot stage candidate for Xray validation"

if ! XRAY_LOCATION_ASSET="$ASSET_DIR" \
    /opt/sbin/xray run -test -confdir "$TEST_CONF" \
    > "$XRAY_TEST_LOG" 2>&1; then

    log "ERROR: candidate Xray validation failed"
    tail -n 40 "$XRAY_TEST_LOG" 2>/dev/null
    exit 1
fi

log "candidate Xray validation: OK"

jq -S . "$OUT_FILE" > "$OLD_NORMALIZED" \
    || fail "cannot normalize live outbound"

jq -S . "$NEW_FILE" > "$NEW_NORMALIZED" \
    || fail "cannot normalize candidate outbound"

FILTER_CHANGED=no
if [ -n "$FILTER_OVERRIDE" ]; then
    CURRENT_FILTER="$(tr -d '\r\n' < "$FILTER_FILE" 2>/dev/null || true)"
    [ "$CURRENT_FILTER" = "$FILTER" ] || FILTER_CHANGED=yes
fi

if cmp "$OLD_NORMALIZED" "$NEW_NORMALIZED" >/dev/null 2>&1; then
    if [ "$FILTER_CHANGED" = yes ]; then
        persist_filter || fail "cannot commit validated profile filter"
        log "profile filter committed after successful validation"
    fi

    log "endpoint/config unchanged: $NEW_IP:$NEW_PORT"
    log "04_outbounds.json unchanged"
    log "no restart"
    exit 0
fi

cp -p "$OUT_FILE" "$BACKUP_FILE" \
    || fail "cannot save rolling outbound backup"

chmod 600 "$BACKUP_FILE" 2>/dev/null

log "backup saved: $BACKUP_FILE"

STAGE_FILE="$CONFIG_DIR/.04_outbounds.json.new.$$"

cp "$NEW_FILE" "$STAGE_FILE" \
    || fail "cannot stage live outbound"

chmod 600 "$STAGE_FILE" 2>/dev/null

mv -f "$STAGE_FILE" "$OUT_FILE" || {
    rm -f "$STAGE_FILE" 2>/dev/null
    fail "cannot atomically replace live outbound"
}

if ! XRAY_LOCATION_ASSET="$ASSET_DIR" \
    /opt/sbin/xray run -test -confdir "$CONFIG_DIR" \
    > "$XRAY_TEST_LOG" 2>&1; then

    log "ERROR: live Xray validation failed after atomic replace"

    if rollback_state; then
        exit 1
    fi

    exit 2
fi

if [ -n "$FILTER_OVERRIDE" ]; then
    if ! persist_filter; then
        log "ERROR: cannot commit validated profile filter"
        if rollback_state; then
            exit 1
        fi
        exit 2
    fi
    log "profile filter committed after successful outbound validation"
fi

if [ "$OLD_IP" = "$NEW_IP" ] && [ "$OLD_PORT" = "$NEW_PORT" ]; then
    log "endpoint unchanged, profile parameters changed"
else
    log "endpoint changed: ${OLD_IP:-none}:${OLD_PORT:-none} -> $NEW_IP:$NEW_PORT"
fi
