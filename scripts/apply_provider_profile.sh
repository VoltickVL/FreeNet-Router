#!/bin/sh

# Transactional initial VLESS-Reality provider profile selection for FreeNet.
# Reads the local subscription secret but never prints it or credential fields.

SUB_FILE="${FREENET_SUB_FILE:-/opt/etc/xray/blanc_subscription.url}"
CONFIG_DIR="${FREENET_CONFIG_DIR:-/opt/etc/xray/configs}"
ASSET_DIR="${FREENET_ASSET_DIR:-/opt/etc/xray/dat}"
OUT_FILE="$CONFIG_DIR/04_outbounds.json"
PROFILE_FILE="${FREENET_PROFILE_FILE:-/opt/etc/freenet/vpn_profile_name}"
FILTER_FILE="${FREENET_FILTER_FILE:-/opt/etc/xray/blanc_profile_filter.regex}"
HISTORY_FILE="${FREENET_AUTOMATION_HISTORY:-/opt/var/log/freenet-automation.history}"
PROVIDER_CACHE="${FREENET_PROVIDER_SUBSCRIPTION_CACHE:-/opt/var/lib/freenet/provider-subscription.lkg}"
PROVIDER_CACHE_SOURCE="${FREENET_PROVIDER_SUBSCRIPTION_SOURCE:-${PROVIDER_CACHE}.source.sha256}"
XRAY_BIN="${FREENET_XRAY_BIN:-/opt/sbin/xray}"
XKEEN_BIN="${FREENET_XKEEN_BIN:-/opt/sbin/xkeen}"
CORE_RESTART_HELPER="${FREENET_XRAY_CORE_RESTART_HELPER:-}"
LOCK_DIR="${FREENET_LOCK_DIR:-/tmp/blanc_xkeen_update.lock}"
CHECKPOINT_DIR="${FREENET_VPN_TRANSACTION_DIR:-/opt/var/lib/freenet/vpn-transaction.pending}"
AUTOMATION_STATE="${FREENET_AUTOMATION_STATE:-/opt/var/run/freenet-automation.state}"
CURL_BIN="${FREENET_CURL_BIN:-curl}"
PROVIDER_ROUTE_PROBE_BIN="${FREENET_PROVIDER_ROUTE_PROBE_BIN:-}"
BOOTSTRAP_DNS_PRIMARY="77.88.8.8"
BOOTSTRAP_DNS_SECONDARY="8.8.8.8"
TMP_DIR=""
WAS_RUNNING=0
OUT_BEFORE_EXISTS=no
PROFILE_BEFORE_EXISTS=no
FILTER_BEFORE_EXISTS=no
APPLIED=0
ROLLBACK_ACTIVE=0
LOCK_HELD=0
CORE_RESTART_STAGE="not-started"
CORE_RESTART_EXIT="not-run"
ROLLBACK_FILES_STATUS="UNKNOWN"
ROLLBACK_CORE_STATUS="UNKNOWN"
ROLLBACK_VPN_STATUS="UNKNOWN"
PROVIDER_FINALIZATION_PHASE=""
OWNED_XRAY_ENABLED=0
OWNED_XRAY_SEQUENCE=0
OWNED_XRAY_CHILD_PID=0

say() { printf '%s\n' "$*"; }
err() { printf '[FreeNet Provider] ERROR: %s\n' "$*" >&2; }

# A production core must inherit neither a probe marker nor this invocation's
# private worker/finalization channel. Its lifecycle stays with XKeen.
production_core_command() (
    unset FREENET_XRAY_PROBE
    unset FREENET_OWNED_XRAY_DIR FREENET_OWNED_XRAY_NONCE FREENET_OWNED_XRAY_WORK_DIR
    unset FREENET_PROVIDER_FINALIZATION_STATUS FREENET_PROVIDER_FINALIZATION_NONCE
    exec "$@"
)

# The Go parent owns this private channel and every requested Xray process.
# An incomplete channel is an error, never a fallback to an unowned child.
owned_xray_init() {
    [ -n "${FREENET_OWNED_XRAY_DIR:-}${FREENET_OWNED_XRAY_NONCE:-}${FREENET_OWNED_XRAY_WORK_DIR:-}" ] || return 0
    [ "${#FREENET_OWNED_XRAY_NONCE}" -eq 32 ] || return 1
    case "$FREENET_OWNED_XRAY_NONCE" in *[!0-9a-f]*) return 1 ;; esac
    [ "$FREENET_OWNED_XRAY_DIR" = "$1/owned-xray" ] &&
        [ "$FREENET_OWNED_XRAY_WORK_DIR" = "$1/helper" ] || return 1
    for OWNED_XRAY_DIRECTORY in "$1" "$FREENET_OWNED_XRAY_DIR" "$FREENET_OWNED_XRAY_WORK_DIR"; do
        [ -d "$OWNED_XRAY_DIRECTORY" ] && [ ! -L "$OWNED_XRAY_DIRECTORY" ] || return 1
    done
    OWNED_XRAY_ENABLED=1
}

owned_xray_request() {
    [ "$OWNED_XRAY_ENABLED" -eq 1 ] && [ "$OWNED_XRAY_SEQUENCE" -lt 16 ] || return 1
    OWNED_XRAY_SEQUENCE=$((OWNED_XRAY_SEQUENCE + 1))
    OWNED_XRAY_REQUEST="$FREENET_OWNED_XRAY_DIR/request.tmp.$$"
    [ ! -e "$OWNED_XRAY_REQUEST" ] && [ ! -L "$OWNED_XRAY_REQUEST" ] || return 1
    (umask 077; set -C; printf 'FREENET_OWNED_XRAY_V1\t%s\t%s\t%s\t%s\t%s\n' \
        "$FREENET_OWNED_XRAY_NONCE" "$$" "$OWNED_XRAY_SEQUENCE" "$1" "$2" > "$OWNED_XRAY_REQUEST") || return 1
    chmod 600 "$OWNED_XRAY_REQUEST" || return 1
    mv -f "$OWNED_XRAY_REQUEST" "$FREENET_OWNED_XRAY_DIR/request" || return 1
    OWNED_XRAY_RESPONSE="$FREENET_OWNED_XRAY_DIR/response"
    OWNED_XRAY_POLL=0
    while [ "$OWNED_XRAY_POLL" -lt 180 ]; do
        if [ -e "$OWNED_XRAY_RESPONSE" ] || [ -L "$OWNED_XRAY_RESPONSE" ]; then
            [ -f "$OWNED_XRAY_RESPONSE" ] && [ ! -L "$OWNED_XRAY_RESPONSE" ] || return 1
            [ "$(wc -c < "$OWNED_XRAY_RESPONSE")" -le 512 ] || return 1
            [ "$(wc -l < "$OWNED_XRAY_RESPONSE")" -eq 1 ] || return 1
            OWNED_XRAY_REPLY="$(cat "$OWNED_XRAY_RESPONSE")" || return 1
            IFS="$(printf '\t')" read -r OX_PROTOCOL OX_NONCE OX_PID OX_SEQ OX_STAGE OX_RESULT OX_CHILD OX_EXTRA <<EOF
$OWNED_XRAY_REPLY
EOF
            [ "$OWNED_XRAY_REPLY" = "$(printf '%s\t%s\t%s\t%s\t%s\t%s\t%s' \
                "$OX_PROTOCOL" "$OX_NONCE" "$OX_PID" "$OX_SEQ" "$OX_STAGE" "$OX_RESULT" "$OX_CHILD")" ] || return 1
            [ "$OX_PROTOCOL" = FREENET_OWNED_XRAY_V1 ] && [ "$OX_NONCE" = "$FREENET_OWNED_XRAY_NONCE" ] && [ "$OX_PID" = "$$" ] || return 1
            case "$OX_CHILD" in ''|*[!0-9]*) return 1 ;; esac
            if [ "$OX_SEQ" = "$OWNED_XRAY_SEQUENCE" ]; then
                [ "$OX_STAGE" = "$1" ] || return 1
                case "$OX_RESULT" in OK) OWNED_XRAY_CHILD_PID="$OX_CHILD"; return 0 ;; FAIL) return 1 ;; *) return 1 ;; esac
            fi
            [ "$OX_SEQ" = "$((OWNED_XRAY_SEQUENCE - 1))" ] || return 1
        fi
        sleep 1
        OWNED_XRAY_POLL=$((OWNED_XRAY_POLL + 1))
    done
    return 1
}

cleanup() {
    [ -n "$TMP_DIR" ] && rm -rf "$TMP_DIR" 2>/dev/null || true
    if [ "$LOCK_HELD" -eq 1 ]; then
        rm -rf "$LOCK_DIR" 2>/dev/null || true
        LOCK_HELD=0
    fi
}

# Only the Go-owned selection directory carries this per-invocation channel.
# Profile/output text never grants more execution time. No apply or retry may
# follow either phase: only archive/checks, journal, receipt and cleanup remain.
provider_finalization_status() {
    case "$MODE" in apply-core|recover-core) ;; *) return 0 ;; esac
    [ -n "${FREENET_PROVIDER_FINALIZATION_STATUS:-}${FREENET_PROVIDER_FINALIZATION_NONCE:-}" ] || return 0
    case "$1:$2" in
        rollback-finalizing:pending|rollback-finalizing:rolled-back|accepted-finalizing:pending|accepted-finalizing:accepted) ;;
        *) return 1 ;;
    esac
    [ "${#FREENET_PROVIDER_FINALIZATION_NONCE}" -eq 32 ] || return 1
    case "$FREENET_PROVIDER_FINALIZATION_NONCE" in *[!0-9a-f]*) return 1 ;; esac
    FINALIZATION_PARENT="$(dirname "$PROVIDER_CACHE")"
    [ "$FREENET_PROVIDER_FINALIZATION_STATUS" = "$FINALIZATION_PARENT/finalization.status" ] || return 1
    [ -d "$FINALIZATION_PARENT" ] && [ ! -L "$FINALIZATION_PARENT" ] || return 1
    [ ! -L "$FREENET_PROVIDER_FINALIZATION_STATUS" ] || return 1
    [ ! -e "$FREENET_PROVIDER_FINALIZATION_STATUS" ] || [ -f "$FREENET_PROVIDER_FINALIZATION_STATUS" ] || return 1
    FINALIZATION_TEMP="$FREENET_PROVIDER_FINALIZATION_STATUS.tmp.$$"
    [ ! -e "$FINALIZATION_TEMP" ] && [ ! -L "$FINALIZATION_TEMP" ] || return 1
    (umask 077; set -C; printf 'FREENET_PROVIDER_FINALIZATION_V1\t%s\t%s\t%s\t%s\n' \
        "$FREENET_PROVIDER_FINALIZATION_NONCE" "$$" "$1" "$2" > "$FINALIZATION_TEMP") || return 1
    chmod 600 "$FINALIZATION_TEMP" || return 1
    mv -f "$FINALIZATION_TEMP" "$FREENET_PROVIDER_FINALIZATION_STATUS" || return 1
    PROVIDER_FINALIZATION_PHASE="$1"
}

# A zero/one exit alone does not prove cleanup: the historical EXIT trap
# suppresses rm failures. Managed receipts require an explicit absence check.
# The caller's writer/probe fences stay held until Go has also waited for exit.
complete_provider_finalization() {
    case "$MODE" in apply-core|recover-core) ;; *) return 0 ;; esac
    [ -n "${FREENET_PROVIDER_FINALIZATION_STATUS:-}${FREENET_PROVIDER_FINALIZATION_NONCE:-}" ] || return 0
    [ "$PROVIDER_FINALIZATION_PHASE" = "$1" ] && [ "$LOCK_HELD" -eq 1 ] || return 1
    cleanup
    [ "$LOCK_HELD" -eq 0 ] && [ ! -e "$LOCK_DIR" ] && [ ! -L "$LOCK_DIR" ] || return 1
    if [ -n "$TMP_DIR" ]; then
        [ ! -e "$TMP_DIR" ] && [ ! -L "$TMP_DIR" ] || return 1
    fi
    provider_finalization_status "$1" "$2"
}

transaction_pending() {
    [ -e "$CHECKPOINT_DIR" ] || [ -L "$CHECKPOINT_DIR" ]
}

mutation_forbidden() {
    transaction_pending || grep -qs '^MUTATION_BLOCKED=yes$' "$AUTOMATION_STATE" 2>/dev/null
}

# Only user-confirmed manual recovery can enter this mode. The original PENDING
# checkpoint must remain verifiable and available until a separate snapshot and
# the selected live VPN pass acceptance.
valid_recovery_checkpoint() {
    [ -d "$CHECKPOINT_DIR" ] && [ ! -L "$CHECKPOINT_DIR" ] || return 1
    [ -f "$CHECKPOINT_DIR/state" ] && [ ! -L "$CHECKPOINT_DIR/state" ] || return 1
    [ "$(cat "$CHECKPOINT_DIR/state" 2>/dev/null)" = PENDING ] || return 1
    if [ -e "$CHECKPOINT_DIR/recovery.pending" ] || [ -L "$CHECKPOINT_DIR/recovery.pending" ]; then
        valid_recovery_attempt || return 1
    fi
    for RECOVERY_KIND in out profile filter; do
        RECOVERY_BEFORE="$CHECKPOINT_DIR/$RECOVERY_KIND.before"
        RECOVERY_ABSENT="$CHECKPOINT_DIR/$RECOVERY_KIND.absent"
        [ ! -L "$RECOVERY_BEFORE" ] && [ ! -L "$RECOVERY_ABSENT" ] || return 1
        if [ -f "$RECOVERY_BEFORE" ]; then
            [ ! -e "$RECOVERY_ABSENT" ] || return 1
            [ "$(wc -c < "$RECOVERY_BEFORE" 2>/dev/null)" -le 2097152 ] || return 1
        else
            [ ! -e "$RECOVERY_BEFORE" ] && [ -f "$RECOVERY_ABSENT" ] || return 1
            [ "$(wc -c < "$RECOVERY_ABSENT" 2>/dev/null)" -eq 0 ] || return 1
        fi
    done
    for RECOVERY_LIVE in "$OUT_FILE" "$PROFILE_FILE" "$FILTER_FILE"; do
        [ ! -L "$RECOVERY_LIVE" ] || return 1
        [ ! -e "$RECOVERY_LIVE" ] || [ -f "$RECOVERY_LIVE" ] || return 1
    done
    return 0
}

# A previous fully written recovery snapshot is preserved before the next
# explicit attempt. Incomplete snapshots and unknown writers remain STOP.
valid_recovery_attempt() {
    ATTEMPT="$CHECKPOINT_DIR/recovery.pending"
    [ -d "$ATTEMPT" ] && [ ! -L "$ATTEMPT" ] || return 1
    [ -f "$ATTEMPT/state" ] && [ ! -L "$ATTEMPT/state" ] || return 1
    [ "$(cat "$ATTEMPT/state")" = PENDING ] || return 1
    if [ -e "$ATTEMPT/core.before" ] || [ -L "$ATTEMPT/core.before" ]; then
        [ -f "$ATTEMPT/core.before" ] && [ ! -L "$ATTEMPT/core.before" ] || return 1
        case "$(cat "$ATTEMPT/core.before")" in running|stopped) ;; *) return 1 ;; esac
    fi
    for KIND in out profile filter; do
        BEFORE="$ATTEMPT/$KIND.before"; ABSENT="$ATTEMPT/$KIND.absent"
        [ ! -L "$BEFORE" ] && [ ! -L "$ABSENT" ] || return 1
        if [ -f "$BEFORE" ]; then
            [ ! -e "$ABSENT" ] && [ -r "$BEFORE" ] || return 1
            [ "$(wc -c < "$BEFORE")" -le 2097152 ] || return 1
        else
            [ ! -e "$BEFORE" ] && [ -f "$ABSENT" ] || return 1
            [ "$(wc -c < "$ABSENT")" -eq 0 ] || return 1
        fi
    done
}

archive_recovery_attempt() {
    ATTEMPT_RESULT="$1"
    ATTEMPT_ROOT="$(dirname "$CHECKPOINT_DIR")/vpn-transaction.attempts"
    if [ -e "$ATTEMPT_ROOT" ] || [ -L "$ATTEMPT_ROOT" ]; then
        [ -d "$ATTEMPT_ROOT" ] && [ ! -L "$ATTEMPT_ROOT" ] || return 1
    else
        (umask 077; mkdir "$ATTEMPT_ROOT") || return 1
    fi
    chmod 700 "$ATTEMPT_ROOT" || return 1
    ATTEMPT_DEST="$ATTEMPT_ROOT/$(date -u '+%Y%m%dT%H%M%SZ').$$"
    [ ! -e "$ATTEMPT_DEST" ] && [ ! -L "$ATTEMPT_DEST" ] || return 1
    # Do not rewrite either generation of saved configuration bytes.
    printf '%s\n' "$ATTEMPT_RESULT" > "$CHECKPOINT_DIR/recovery.pending/result" || return 1
    mv "$CHECKPOINT_DIR/recovery.pending" "$ATTEMPT_DEST" || return 1
    if command -v sync >/dev/null 2>&1; then sync || return 1; fi
    [ ! -e "$CHECKPOINT_DIR/recovery.pending" ] && [ -d "$ATTEMPT_DEST" ]
}

# A second durable checkpoint captures ACTUAL live bytes. The original
# PENDING marker continues to fence AUTO and survives process death.
begin_recovery_checkpoint() {
    RECOVERY_DIR="$CHECKPOINT_DIR/recovery.pending"
    umask 077
    mkdir "$RECOVERY_DIR" || return 1
    chmod 700 "$RECOVERY_DIR" || return 1
    for RECOVERY_KIND in out profile filter; do
        case "$RECOVERY_KIND" in
            out) RECOVERY_SRC="$OUT_BEFORE"; RECOVERY_PRESENT="$OUT_BEFORE_EXISTS" ;;
            profile) RECOVERY_SRC="$PROFILE_BEFORE"; RECOVERY_PRESENT="$PROFILE_BEFORE_EXISTS" ;;
            filter) RECOVERY_SRC="$FILTER_BEFORE"; RECOVERY_PRESENT="$FILTER_BEFORE_EXISTS" ;;
        esac
        if [ "$RECOVERY_PRESENT" = yes ]; then
            cp "$RECOVERY_SRC" "$RECOVERY_DIR/$RECOVERY_KIND.before" || return 1
            chmod 600 "$RECOVERY_DIR/$RECOVERY_KIND.before" || return 1
        else
            : > "$RECOVERY_DIR/$RECOVERY_KIND.absent" || return 1
        fi
    done
    if [ "$WAS_RUNNING" -eq 1 ]; then BEFORE_CORE=running; else BEFORE_CORE=stopped; fi
    printf '%s\n' "$BEFORE_CORE" > "$RECOVERY_DIR/core.before" || return 1
    chmod 600 "$RECOVERY_DIR/core.before" || return 1
    printf 'PENDING\n' > "$RECOVERY_DIR/state" || return 1
    chmod 600 "$RECOVERY_DIR/state" || return 1
    if command -v sync >/dev/null 2>&1; then sync || return 1; fi
    [ -s "$RECOVERY_DIR/state" ]
}

# Archive BOTH generations of protected snapshots after confirmed live HTTPS.
# A rename within the same filesystem removes the pending marker only after
# candidate acceptance; the AUTO latch remains until Go adopts manual server.
archive_recovery_checkpoint() {
    RECOVERY_ARCHIVE_ROOT="$(dirname "$CHECKPOINT_DIR")/vpn-transaction.recovered"
    if [ -e "$RECOVERY_ARCHIVE_ROOT" ]; then
        [ -d "$RECOVERY_ARCHIVE_ROOT" ] && [ ! -L "$RECOVERY_ARCHIVE_ROOT" ] || return 1
    else
        umask 077
        mkdir "$RECOVERY_ARCHIVE_ROOT" || return 1
        chmod 700 "$RECOVERY_ARCHIVE_ROOT" || return 1
    fi
    RECOVERY_ARCHIVE="$RECOVERY_ARCHIVE_ROOT/$(date -u '+%Y%m%dT%H%M%SZ').$$"
    [ ! -e "$RECOVERY_ARCHIVE" ] && [ ! -L "$RECOVERY_ARCHIVE" ] || return 1
    [ -d "$RECOVERY_DIR" ] && [ -f "$RECOVERY_DIR/state" ] || return 1
    mv "$CHECKPOINT_DIR" "$RECOVERY_ARCHIVE" || return 1
    [ ! -e "$CHECKPOINT_DIR" ] && [ -d "$RECOVERY_ARCHIVE" ] || return 1
    if command -v sync >/dev/null 2>&1; then sync || return 1; fi
    return 0
}

acquire_mutation_lock() {
    [ "${1:-normal}" = recover-core ] || { mutation_forbidden && return 1; }
    if mkdir "$LOCK_DIR" 2>/dev/null; then
        LOCK_HELD=1
        printf '%s\n' "$$" > "$LOCK_DIR/pid" 2>/dev/null || {
            rm -rf "$LOCK_DIR" 2>/dev/null || true
            LOCK_HELD=0
            return 1
        }
        return 0
    fi

    # Never steal a stale lock; previous writer may have died after commit.
    return 1
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

subscription_source_fingerprint() {
    printf '%s' "$SUB_URL" | sha256sum | awk '{print $1}'
}

load_provider_cache() {
    [ -s "$PROVIDER_CACHE" ] || return 1
    [ -s "$PROVIDER_CACHE_SOURCE" ] || return 1
    EXPECTED_SOURCE="$(subscription_source_fingerprint)"
    CACHED_SOURCE="$(tr -d '\r\n ' < "$PROVIDER_CACHE_SOURCE" 2>/dev/null)"
    [ -n "$EXPECTED_SOURCE" ] && [ "$CACHED_SOURCE" = "$EXPECTED_SOURCE" ] || return 1
    cp "$PROVIDER_CACHE" "$DECODED_FILE" || return 1
    tr -d '\r' < "$DECODED_FILE" > "$DECODED_FILE.clean" || return 1
    mv "$DECODED_FILE.clean" "$DECODED_FILE" || return 1
    grep -q '^vless://' "$DECODED_FILE" || return 1
    SUBSCRIPTION_SOURCE='secure-lkg'
    return 0
}

save_provider_cache() {
    CACHE_DIR="$(dirname "$PROVIDER_CACHE")"
    SOURCE_DIR="$(dirname "$PROVIDER_CACHE_SOURCE")"
    mkdir -p "$CACHE_DIR" "$SOURCE_DIR" 2>/dev/null || return 1
    CACHE_STAGE="$PROVIDER_CACHE.new.$$"
    SOURCE_STAGE="$PROVIDER_CACHE_SOURCE.new.$$"
    cp "$DECODED_FILE" "$CACHE_STAGE" || return 1
    chmod 600 "$CACHE_STAGE" 2>/dev/null || true
    printf '%s\n' "$(subscription_source_fingerprint)" > "$SOURCE_STAGE" || { rm -f "$CACHE_STAGE"; return 1; }
    chmod 600 "$SOURCE_STAGE" 2>/dev/null || true
    mv -f "$CACHE_STAGE" "$PROVIDER_CACHE" || { rm -f "$CACHE_STAGE" "$SOURCE_STAGE"; return 1; }
    mv -f "$SOURCE_STAGE" "$PROVIDER_CACHE_SOURCE" || return 1
    return 0
}

prepare_subscription() {
    if load_provider_cache; then
        return 0
    fi
    fetch_subscription || return 1
    [ -s "$RAW_FILE" ] || return 1
    if grep -q '^vless://' "$RAW_FILE"; then
        cp "$RAW_FILE" "$DECODED_FILE" || return 1
    else
        base64 -d "$RAW_FILE" > "$DECODED_FILE" 2>/dev/null || return 1
    fi
    tr -d '\r' < "$DECODED_FILE" > "$DECODED_FILE.clean" || return 1
    mv "$DECODED_FILE.clean" "$DECODED_FILE" || return 1
    grep -q '^vless://' "$DECODED_FILE" || return 1
    SUBSCRIPTION_SOURCE='fresh'
    save_provider_cache || return 1
    return 0
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

sanitize_history() {
    TEXT="$(printf '%s' "$1" | tr '\r\n\t' '   ')"
    LOWER="$(printf '%s' "$TEXT" | tr '[:upper:]' '[:lower:]')"
    case "$LOWER" in
        *vless://*|*https://*|*http://*|*uuid=*|*pbk=*|*sid=*|*publickey=*|*private-token*)
            printf '%s\n' 'redacted provider switch event'
            ;;
        *)
            printf '%s\n' "$TEXT"
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
    SECURITY_ID="$2"
    TYPE_ID="$3"
    SNI_ID="$4"
    HOST_ID="$5"
    PATH_ID="$6"
    LOWER_SNI="$(printf '%s' "$SNI_ID" | tr '[:upper:]' '[:lower:]')"
    LOWER_HOST="$(printf '%s' "$HOST_ID" | tr '[:upper:]' '[:lower:]')"
    printf '%s|%s|%s|%s|%s|%s' "$NAME" "$SECURITY_ID" "$TYPE_ID" "$LOWER_SNI" "$LOWER_HOST" "$PATH_ID" | sha256sum | awk '{print substr($1,1,16)}'
}

parse_line_identity() {
    LINE="$1"
    BODY="${LINE#vless://}"
    case "$BODY" in *@*) ;; *) return 1 ;; esac
    REST="${BODY#*@}"
    HOSTPORT="$(printf '%s\n' "$REST" | sed 's/[?].*$//')"
    QUERY_AND_NAME="$(printf '%s\n' "$REST" | sed 's/^[^?]*[?]//')"
    QUERY="${QUERY_AND_NAME%%#*}"

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

    ID_SECURITY="$(printf '%s' "$(get_param security)" | tr '[:upper:]' '[:lower:]')"
    ID_TYPE="$(printf '%s' "$(get_param type)" | tr '[:upper:]' '[:lower:]')"
    [ -n "$ID_SECURITY" ] || ID_SECURITY='reality'
    [ -n "$ID_TYPE" ] || ID_TYPE='tcp'
    ID_SNI="$(url_decode "$(get_param sni)")"
    ID_HOST="$(url_decode "$(get_param host)")"
    ID_PATH="$(url_decode "$(get_param path)")"
    case "$ID_SECURITY:$ID_TYPE" in
        reality:tcp)
            ID_HOST=''
            ID_PATH=''
            ;;
        tls:tcp)
            ID_HOST=''
            ID_PATH=''
            ;;
        tls:ws)
            [ -n "$ID_PATH" ] || ID_PATH='/'
            [ -n "$ID_HOST" ] || ID_HOST="$ID_SNI"
            ;;
        *)
            return 1
            ;;
    esac
    ID="$(profile_id "$NAME" "$ID_SECURITY" "$ID_TYPE" "$ID_SNI" "$ID_HOST" "$ID_PATH")"
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
    SECURITY="$(printf '%s' "$(get_param security)" | tr '[:upper:]' '[:lower:]')"
    TYPE="$(printf '%s' "$(get_param type)" | tr '[:upper:]' '[:lower:]')"
    FP="$(get_param fp)"
    SNI="$(url_decode "$(get_param sni)")"
    PBK="$(get_param pbk)"
    SID="$(get_param sid)"
    SPX="$(url_decode "$(get_param spx)")"
    WS_HOST="$(url_decode "$(get_param host)")"
    WS_PATH="$(url_decode "$(get_param path)")"

    [ -n "$SECURITY" ] || SECURITY='reality'
    [ -n "$TYPE" ] || TYPE='tcp'
    [ -n "$FP" ] || FP='firefox'
    [ -n "$SPX" ] || SPX='/'
    if [ "$SECURITY" = reality ] && [ -z "$FLOW" ]; then
        FLOW='xtls-rprx-vision'
    fi

    case "$SECURITY:$TYPE" in
        reality:tcp)
            ;;
        tls:tcp)
            ;;
        tls:ws)
            [ -z "$FLOW" ] || { err 'VLESS WS profile must not use XTLS flow'; return 1; }
            [ -n "$WS_PATH" ] || WS_PATH='/'
            [ -n "$WS_HOST" ] || WS_HOST="$SNI"
            ;;
        *)
            err "unsupported VLESS transport: $SECURITY/$TYPE"
            return 1
            ;;
    esac

    MISSING=''
    [ -n "$UUID" ] || MISSING="$MISSING UUID"
    [ -n "$SELECTED_ADDRESS" ] || MISSING="$MISSING ADDRESS"
    [ -n "$SELECTED_PORT" ] || MISSING="$MISSING PORT"
    [ -n "$SNI" ] || MISSING="$MISSING SNI"
    if [ "$SECURITY" = reality ]; then
        [ -n "$PBK" ] || MISSING="$MISSING PBK"
        [ -n "$SID" ] || MISSING="$MISSING SID"
    fi
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
        --arg wsHost "$WS_HOST" \
        --arg wsPath "$WS_PATH" \
        '
        def user:
          {id:$uuid,encryption:"none",level:0}
          + (if $flow == "" then {} else {flow:$flow} end);
        {
          tag:"vless-reality",
          protocol:"vless",
          settings:{vnext:[{address:$address,port:$port,users:[user]}]},
          streamSettings: (
            if $security == "reality" then
              {network:$network,security:"reality",realitySettings:{fingerprint:$fingerprint,serverName:$serverName,publicKey:$publicKey,shortId:$shortId,spiderX:$spiderX}}
            elif $security == "tls" and $network == "ws" then
              {network:"ws",security:"tls",tlsSettings:{fingerprint:$fingerprint,serverName:$serverName},wsSettings:{path:$wsPath,headers:{Host:$wsHost}}}
            elif $security == "tls" and $network == "tcp" then
              {network:"tcp",security:"tls",tlsSettings:{fingerprint:$fingerprint,serverName:$serverName}}
            else
              error("unsupported VLESS transport")
            end
          )
        }' > "$VLESS_OBJECT"
}
jsonc_normalize() {
    awk '
        BEGIN { in_string=0; escaped=0; block=0; pending=0; ws="" }
        function flush_pending() { if (pending) { printf ",%s", ws; pending=0; ws="" } }
        {
            line=$0; i=1
            while (i <= length(line)) {
                c=substr(line,i,1); n=(i < length(line) ? substr(line,i+1,1) : "")
                if (block) {
                    if (c == "*" && n == "/") { block=0; i+=2 } else i++
                    continue
                }
                if (in_string) {
                    printf "%s", c
                    if (escaped) escaped=0
                    else if (c == "\\") escaped=1
                    else if (c == "\"") in_string=0
                    i++; continue
                }
                if (c == "/" && n == "/") break
                if (c == "/" && n == "*") { block=1; i+=2; continue }
                if (pending) {
                    if (c == " " || c == "\t" || c == "\r") { ws=ws c; i++; continue }
                    if (c == "}" || c == "]") { printf "%s%s", ws, c; pending=0; ws=""; i++; continue }
                    flush_pending()
                }
                if (c == "\"") { in_string=1; printf "%s", c; i++; continue }
                if (c == ",") { pending=1; ws=""; i++; continue }
                printf "%s", c; i++
            }
            if (pending) ws=ws "\n"; else printf "\n"
        }
        END {
            if (pending) printf ",%s", ws
            if (block || in_string) exit 2
        }
    ' "$1"
}

build_candidate() {
    if [ -e "$OUT_FILE" ] || [ -L "$OUT_FILE" ]; then
        [ -f "$OUT_FILE" ] && [ ! -L "$OUT_FILE" ] && [ -r "$OUT_FILE" ] || return 1
        NORMALIZED_OUT="$TMP_DIR/current-outbounds.json"
        jsonc_normalize "$OUT_FILE" > "$NORMALIZED_OUT" || return 1
        jq -e '(.outbounds | type) == "array"' "$NORMALIZED_OUT" >/dev/null 2>&1 || return 1
        jq --slurpfile replacement "$VLESS_OBJECT" '
          .outbounds as $old
          | ($old | map(select(.tag != "vless-reality"))) as $rest0
          | ($rest0 | if any(.[]; .tag == "direct") then . else . + [{tag:"direct",protocol:"freedom",streamSettings:{sockopt:{mark:255}}}] end) as $rest1
          | ($rest1 | if any(.[]; .tag == "block") then . else . + [{tag:"block",protocol:"blackhole"}] end) as $rest2
          | .outbounds = ([$replacement[0]] + $rest2)
        ' "$NORMALIZED_OUT" > "$CANDIDATE_OUT" || return 1
    else
        [ ! -e "$OUT_FILE" ] && [ ! -L "$OUT_FILE" ] || { err 'existing outbound is unreadable or invalid; candidate did not replace it'; return 1; }
        jq -n --slurpfile replacement "$VLESS_OBJECT" '
          {outbounds:[$replacement[0],{tag:"direct",protocol:"freedom",streamSettings:{sockopt:{mark:255}}},{tag:"block",protocol:"blackhole"}]}
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
    if [ "$OWNED_XRAY_ENABLED" -eq 1 ]; then
        owned_xray_request candidate-test "$TEST_CONF"
        return $?
    fi
    unmanaged_xray_test "$TEST_CONF"
}

# Direct CLI calls retain their synchronous lifecycle, but their read-only
# validation is still visibly isolated from the production core inventory.
unmanaged_xray_test() {
    TEST_XRAY="$TMP_DIR/fn-xray-test"
    TEST_XRAY_TARGET="$XRAY_BIN"
    case "$TEST_XRAY_TARGET" in
        /*) ;;
        *)
            TEST_XRAY_PARENT="$(CDPATH= cd "$(dirname "$XRAY_BIN")" && pwd)" || return 1
            TEST_XRAY_TARGET="$TEST_XRAY_PARENT/$(basename "$XRAY_BIN")"
            ;;
    esac
    if [ -e "$TEST_XRAY" ] || [ -L "$TEST_XRAY" ]; then
        [ -L "$TEST_XRAY" ] && [ "$(readlink "$TEST_XRAY")" = "$TEST_XRAY_TARGET" ] || return 1
    else
        ln -s "$TEST_XRAY_TARGET" "$TEST_XRAY" || return 1
    fi
    FREENET_XRAY_PROBE=1 XRAY_LOCATION_ASSET="$ASSET_DIR" "$TEST_XRAY" run -test -confdir "$1" > "$XRAY_TEST_LOG" 2>&1
}

validate_live() {
    if [ "$OWNED_XRAY_ENABLED" -eq 1 ]; then
        owned_xray_request live-test "$CONFIG_DIR"
        return $?
    fi
    unmanaged_xray_test "$CONFIG_DIR"
}

stop_provider_route_probe() {
    if [ "$OWNED_XRAY_ENABLED" -eq 1 ]; then
        owned_xray_request route-stop "$PROBE_DIR"
        return $?
    fi
    kill "$PROBE_PID" 2>/dev/null || true
    wait "$PROBE_PID" 2>/dev/null || true
}

provider_route_probe() {
    PROBE_OUT="$1"
    [ -s "$PROBE_OUT" ] || return 1

    if [ -n "$PROVIDER_ROUTE_PROBE_BIN" ]; then
        [ -x "$PROVIDER_ROUTE_PROBE_BIN" ] || return 1
        "$PROVIDER_ROUTE_PROBE_BIN" "$PROBE_OUT"
        return $?
    fi

    for C in jq mktemp netstat awk grep ln; do
        command -v "$C" >/dev/null 2>&1 || return 1
    done
    command -v "$CURL_BIN" >/dev/null 2>&1 || [ -x "$CURL_BIN" ] || return 1

    PROBE_DIR="$TMP_DIR/provider-route-probe"
    rm -rf "$PROBE_DIR" 2>/dev/null || true
    mkdir -p "$PROBE_DIR" || return 1
    chmod 700 "$PROBE_DIR" 2>/dev/null || true

    PROBE_PORT=$((12080 + ($$ % 200)))
    PROBE_TRY=0
    while [ "$PROBE_TRY" -lt 5 ]; do
        if ! netstat -lnt 2>/dev/null | awk '{print $4}' | grep -Eq "(^|[.:])${PROBE_PORT}$"; then
            break
        fi
        PROBE_PORT=$((PROBE_PORT + 1))
        PROBE_TRY=$((PROBE_TRY + 1))
    done
    [ "$PROBE_TRY" -lt 5 ] || return 1

    PROBE_CFG="$PROBE_DIR/00_probe.json"
    jq -n --slurpfile live "$PROBE_OUT" --argjson port "$PROBE_PORT" '
      {
        log:{loglevel:"warning"},
        inbounds:[{listen:"127.0.0.1",port:$port,protocol:"socks",settings:{udp:false},tag:"freenet-provider-probe"}],
        outbounds:[($live[0].outbounds[] | select(.tag=="vless-reality") | .streamSettings.sockopt.mark = 255)],
        routing:{domainStrategy:"AsIs",rules:[{type:"field",inboundTag:["freenet-provider-probe"],outboundTag:"vless-reality"}]}
      }
    ' > "$PROBE_CFG" 2>/dev/null || return 1
    chmod 600 "$PROBE_CFG" 2>/dev/null || true

    if [ "$OWNED_XRAY_ENABLED" -eq 1 ]; then
        owned_xray_request route-test "$PROBE_DIR" || return 1
        owned_xray_request route-start "$PROBE_DIR" || return 1
        PROBE_PID="$OWNED_XRAY_CHILD_PID"
        [ "$PROBE_PID" -gt 0 ] || return 1
    else
        PROBE_XRAY="$PROBE_DIR/fn-xray-provider-probe"
        ln -s "$XRAY_BIN" "$PROBE_XRAY" 2>/dev/null || return 1
        FREENET_XRAY_PROBE=1 XRAY_LOCATION_ASSET="$ASSET_DIR" "$PROBE_XRAY" run -test -confdir "$PROBE_DIR" >/dev/null 2>&1 || return 1
        FREENET_XRAY_PROBE=1 XRAY_LOCATION_ASSET="$ASSET_DIR" "$PROBE_XRAY" run -confdir "$PROBE_DIR" >/dev/null 2>&1 &
        PROBE_PID=$!
    fi
    PROBE_READY=0
    PROBE_TRY=0
    while [ "$PROBE_TRY" -lt 5 ]; do
        if netstat -lnt 2>/dev/null | awk '{print $4}' | grep -Eq "(^|[.:])${PROBE_PORT}$"; then
            PROBE_READY=1
            break
        fi
        kill -0 "$PROBE_PID" 2>/dev/null || break
        sleep 1
        PROBE_TRY=$((PROBE_TRY + 1))
    done
    if [ "$PROBE_READY" -ne 1 ]; then
        stop_provider_route_probe || return 1
        return 1
    fi

    PROBE_OK=0
    PROBE_TOTAL=0
    for PROBE_URL in \
        'https://www.gstatic.com/generate_204' \
        'https://cp.cloudflare.com/generate_204' \
        'https://1.1.1.1/cdn-cgi/trace'
    do
        PROBE_RESULT="$("$CURL_BIN" --socks5-hostname "127.0.0.1:$PROBE_PORT" -sS \
            --connect-timeout 4 --max-time 8 -o /dev/null \
            -w '%{http_code}' "$PROBE_URL" 2>/dev/null || true)"
        PROBE_TOTAL=$((PROBE_TOTAL + 1))
        case "$PROBE_RESULT" in
            2??|3??|4??) PROBE_OK=$((PROBE_OK + 1)) ;;
        esac
        [ "$PROBE_OK" -ge 2 ] && break
        PROBE_REMAIN=$((3 - PROBE_TOTAL))
        [ $((PROBE_OK + PROBE_REMAIN)) -ge 2 ] || break
    done
    stop_provider_route_probe || return 1
    [ "$PROBE_OK" -ge 2 ]
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

begin_checkpoint() {
    transaction_pending && return 1
    umask 077
    mkdir -p "$(dirname "$CHECKPOINT_DIR")" || return 1
    mkdir "$CHECKPOINT_DIR" || return 1
    chmod 700 "$CHECKPOINT_DIR" || return 1
    for ITEM in out profile filter; do
        case "$ITEM" in
            out) SRC="$OUT_BEFORE"; PRESENT="$OUT_BEFORE_EXISTS" ;;
            profile) SRC="$PROFILE_BEFORE"; PRESENT="$PROFILE_BEFORE_EXISTS" ;;
            filter) SRC="$FILTER_BEFORE"; PRESENT="$FILTER_BEFORE_EXISTS" ;;
        esac
        if [ "$PRESENT" = yes ]; then
            cp "$SRC" "$CHECKPOINT_DIR/$ITEM.before" || return 1
            chmod 600 "$CHECKPOINT_DIR/$ITEM.before" || return 1
        else
            : > "$CHECKPOINT_DIR/$ITEM.absent" || return 1
        fi
    done
    printf 'PENDING\n' > "$CHECKPOINT_DIR/state" || return 1
    chmod 600 "$CHECKPOINT_DIR/state" || return 1
    if command -v sync >/dev/null 2>&1; then sync || return 1; fi
    [ -s "$CHECKPOINT_DIR/state" ]
}

clear_checkpoint() {
    case "$CHECKPOINT_DIR" in
        /|/opt|/opt/var|/opt/var/lib|/opt/var/lib/freenet|'') return 1 ;;
    esac
    [ -d "$CHECKPOINT_DIR" ] && [ ! -L "$CHECKPOINT_DIR" ] || return 1
    rm -rf "$CHECKPOINT_DIR" || return 1
    [ ! -e "$CHECKPOINT_DIR" ] && [ ! -L "$CHECKPOINT_DIR" ]
}

short_pause() {
    if command -v usleep >/dev/null 2>&1; then
        usleep 50000
    else
        sleep 1
    fi
}

single_xray_pid() {
    PIDS="$(pidof xray 2>/dev/null || true)"
    set -- $PIDS
    [ "$#" -eq 1 ] || return 1
    printf '%s\n' "$1"
}

# Return a finite, secret-free preflight reason. Do not relax any admission
# conditions: an unknown inventory still forbids mutation of the live core.
core_restart_preflight() {
    CORE_PREFLIGHT_REASON="inventory-unknown"
    PIDS="$(pidof xray 2>/dev/null || true)"
    set -- $PIDS
    [ "$#" -le 1 ] || { CORE_PREFLIGHT_REASON="multiple-xray-processes"; return 1; }
    if [ "$#" -eq 0 ]; then
        [ "${FREENET_PROVIDER_RTT_MANUAL:-0}" = 1 ] ||
            { CORE_PREFLIGHT_REASON="production-xray-not-found"; return 1; }
    else
        case "$1" in
            ''|0|*[!0-9]*) CORE_PREFLIGHT_REASON="invalid-xray-process-id"; return 1 ;;
        esac
    fi
    if [ -n "$CORE_RESTART_HELPER" ]; then
        [ -x "$CORE_RESTART_HELPER" ] ||
            { CORE_PREFLIGHT_REASON="core-restart-helper-unavailable"; return 1; }
        CORE_PREFLIGHT_REASON="ready"
        return 0
    fi
    command -v kill >/dev/null 2>&1 ||
        { CORE_PREFLIGHT_REASON="core-signal-unavailable"; return 1; }
    [ -x "$XKEEN_BIN" ] ||
        { CORE_PREFLIGHT_REASON="xkeen-start-unavailable"; return 1; }
    if single_xray_pid >/dev/null 2>&1; then
        CORE_PREFLIGHT_REASON="ready"
        return 0
    fi
    if [ "${FREENET_PROVIDER_RTT_MANUAL:-0}" = 1 ] &&
        [ -z "$(pidof xray 2>/dev/null || true)" ]; then
        CORE_PREFLIGHT_REASON="ready"
        return 0
    fi
    CORE_PREFLIGHT_REASON="xray-process-inventory-changed"
    return 1
}

# Only stage names and numeric exit codes are reported; XKeen output may
# contain local paths/credentials and is never copied to the public journal.
# A zombie still answers kill -0; a reused PID must never be signalled.
# Only the original /proc identity may be treated as the previous core.
# The optional proc root exists only for isolated fixture tests.
# A failed proc read is not proof that the task exited. Require agreement
# between an accessible proc root, an absent PID entry and kill -0. Recheck
# absence after kill -0 so a newly visible/reused PID remains STOP.
xray_pid_confirmed_gone() {
    XRAY_ABSENT_PID="$1"
    XRAY_ABSENT_ROOT="$2"
    [ -d "$XRAY_ABSENT_ROOT" ] && [ -r "$XRAY_ABSENT_ROOT" ] &&
        [ -x "$XRAY_ABSENT_ROOT" ] || return 1
    [ ! -e "$XRAY_ABSENT_ROOT/$XRAY_ABSENT_PID" ] &&
        [ ! -L "$XRAY_ABSENT_ROOT/$XRAY_ABSENT_PID" ] || return 1
    if kill -0 "$XRAY_ABSENT_PID" 2>/dev/null; then return 1; fi
    [ ! -e "$XRAY_ABSENT_ROOT/$XRAY_ABSENT_PID" ] &&
        [ ! -L "$XRAY_ABSENT_ROOT/$XRAY_ABSENT_PID" ]
}

xray_pid_lifecycle() {
    XRAY_PID="$1"
    XRAY_EXPECTED_START="$2"
    XRAY_PROC_ROOT="${3:-/proc}"
    case "$XRAY_PID" in ''|0|*[!0-9]*) printf unknown; return ;; esac
    case "$XRAY_EXPECTED_START" in ''|*[!0-9]*) printf unknown; return ;; esac
    XRAY_STAT="$XRAY_PROC_ROOT/$XRAY_PID/stat"
    if [ ! -r "$XRAY_STAT" ]; then
        if xray_pid_confirmed_gone "$XRAY_PID" "$XRAY_PROC_ROOT"; then printf gone; else printf unknown; fi
        return
    fi
    XRAY_CURRENT_START="$(awk '{print $22}' "$XRAY_STAT" 2>/dev/null)" || XRAY_CURRENT_START=""
    if [ -z "$XRAY_CURRENT_START" ]; then
        if xray_pid_confirmed_gone "$XRAY_PID" "$XRAY_PROC_ROOT"; then printf gone; else printf unknown; fi
        return
    fi
    if [ "$XRAY_CURRENT_START" != "$XRAY_EXPECTED_START" ]; then
        printf reused
        return
    fi
    XRAY_PROC_STATE="$(awk '$1 == "State:" {print $2; exit}' "$XRAY_PROC_ROOT/$XRAY_PID/status" 2>/dev/null)" || XRAY_PROC_STATE=""
    if [ -z "$XRAY_PROC_STATE" ]; then
        if xray_pid_confirmed_gone "$XRAY_PID" "$XRAY_PROC_ROOT"; then printf gone; else printf unknown; fi
        return
    fi
    case "$XRAY_PROC_STATE" in
        Z|X|x) printf zombie ;;
        R|S|D|T|t|I|W|P) printf 'live:%s' "$XRAY_PROC_STATE" ;;
        *) printf unknown ;;
    esac
}

# The production core's PID must be fully absent from pidof before XKeen
# starts. Only the original PID with its captured /proc starttime can be
# tolerated in zombie state for a bounded reaping interval. No signals are
# sent here, and an unrelated or unknown Xray always blocks the cutover.
# An alternate proc root is for isolated fixture tests only.
wait_xray_post_stop_inventory() {
    POST_STOP_PROC_ROOT="${1:-/proc}"
    POST_STOP_WAIT=0
    while :; do
        POST_STOP_PIDS="$(pidof xray 2>/dev/null || true)"
        set -- $POST_STOP_PIDS
        [ "$#" -gt 0 ] || return 0
        for POST_STOP_PID in "$@"; do
            case "$POST_STOP_PID" in
                ''|0|*[!0-9]*)
                    CORE_RESTART_EXIT="unexpected-xray-process"
                    return 1
                    ;;
            esac
            if [ -z "$OLD_PID" ] || [ "$POST_STOP_PID" != "$OLD_PID" ]; then
                CORE_RESTART_EXIT="unexpected-xray-process"
                return 1
            fi
            POST_STOP_STATE="$(xray_pid_lifecycle "$OLD_PID" "$OLD_START" "$POST_STOP_PROC_ROOT")"
            case "$POST_STOP_STATE" in
                gone|zombie) ;;
                reused) CORE_RESTART_EXIT="old-core-pid-reused"; return 1 ;;
                live:D) CORE_RESTART_EXIT="old-core-uninterruptible"; return 1 ;;
                live:*) CORE_RESTART_EXIT="old-core-still-running"; return 1 ;;
                *) CORE_RESTART_EXIT="old-core-identity-unknown"; return 1 ;;
            esac
        done
        if [ "$POST_STOP_WAIT" -ge 20 ]; then
            CORE_RESTART_EXIT="old-core-zombie-unreaped"
            return 1
        fi
        short_pause
        POST_STOP_WAIT=$((POST_STOP_WAIT + 1))
    done
}

restart_xray_core() {
    ALLOW_STOPPED="${1:-no}"
    CORE_RESTART_STAGE="process-inventory"
    CORE_RESTART_EXIT="not-run"
    if [ -n "$CORE_RESTART_HELPER" ]; then
        CORE_RESTART_STAGE="restart-helper"
        if production_core_command "$CORE_RESTART_HELPER" "$ALLOW_STOPPED"; then
            CORE_RESTART_STAGE="ready"
            CORE_RESTART_EXIT="0"
            return 0
        else
            CORE_RESTART_EXIT="$?"
            return 1
        fi
    fi

    OLD_PID=""
    PIDS="$(pidof xray 2>/dev/null || true)"
    set -- $PIDS
    case "$#" in
        0)
            [ "$ALLOW_STOPPED" = yes ] || { CORE_RESTART_EXIT="no-process"; return 1; }
            ;;
        1)
            OLD_PID="$1"
            ;;
        *)
            CORE_RESTART_EXIT="multiple-processes"
            return 1
            ;;
    esac

    if [ -n "$OLD_PID" ]; then
        CORE_RESTART_STAGE="inspect-old-core"
        OLD_START="$(awk '{print $22}' "/proc/$OLD_PID/stat" 2>/dev/null)"
        case "$OLD_START" in
            ''|*[!0-9]*) CORE_RESTART_EXIT="old-core-identity-unknown"; return 1 ;;
        esac
        OLD_STATE="$(xray_pid_lifecycle "$OLD_PID" "$OLD_START")"
        case "$OLD_STATE" in
            gone|zombie) ;;
            live:*)
                CORE_RESTART_STAGE="terminate-old-core"
                if ! kill "$OLD_PID" 2>/dev/null; then
                    OLD_STATE="$(xray_pid_lifecycle "$OLD_PID" "$OLD_START")"
                    case "$OLD_STATE" in
                        gone|zombie) ;;
                        *) CORE_RESTART_EXIT="signal-failed"; return 1 ;;
                    esac
                fi
                I=0
                while [ "$I" -lt 40 ]; do
                    OLD_STATE="$(xray_pid_lifecycle "$OLD_PID" "$OLD_START")"
                    case "$OLD_STATE" in
                        live:*) short_pause; I=$((I + 1)) ;;
                        *) break ;;
                    esac
                done
                case "$OLD_STATE" in
                    live:*)
                        # Recheck identity immediately before a forced signal.
                        OLD_STATE="$(xray_pid_lifecycle "$OLD_PID" "$OLD_START")"
                        case "$OLD_STATE" in
                            live:*)
                                if ! kill -9 "$OLD_PID" 2>/dev/null; then
                                    OLD_STATE="$(xray_pid_lifecycle "$OLD_PID" "$OLD_START")"
                                    case "$OLD_STATE" in
                                        gone|zombie) ;;
                                        *) CORE_RESTART_EXIT="forced-signal-failed"; return 1 ;;
                                    esac
                                fi
                                I=0
                                while [ "$I" -lt 10 ]; do
                                    OLD_STATE="$(xray_pid_lifecycle "$OLD_PID" "$OLD_START")"
                                    case "$OLD_STATE" in
                                        live:*) short_pause; I=$((I + 1)) ;;
                                        *) break ;;
                                    esac
                                done
                                ;;
                        esac
                        ;;
                esac
                ;;
        esac
        OLD_STATE="$(xray_pid_lifecycle "$OLD_PID" "$OLD_START")"
        case "$OLD_STATE" in
            gone|zombie) ;;
            reused) CORE_RESTART_EXIT="old-core-pid-reused"; return 1 ;;
            live:D) CORE_RESTART_EXIT="old-core-uninterruptible"; return 1 ;;
            live:*) CORE_RESTART_EXIT="old-core-still-running"; return 1 ;;
            *) CORE_RESTART_EXIT="old-core-identity-unknown"; return 1 ;;
        esac
    fi

    # XKeen's legacy pidof may still report our original unreaped zombie.
    # Wait only for that proven retired process to leave the inventory.
    # Every other process (including an unknown/reused PID) remains STOP.
    CORE_RESTART_STAGE="process-inventory-after-stop"
    if ! wait_xray_post_stop_inventory; then
        return 1
    fi

    # Preserve XKeen/netfilter ownership, never call the full XKeen restart.
    CORE_RESTART_STAGE="xkeen-start"
    if XKEEN_FOREGROUND=1 production_core_command "$XKEEN_BIN" -start >/dev/null 2>&1; then
        :
    else
        CORE_RESTART_EXIT="$?"
        return 1
    fi

    CORE_RESTART_STAGE="single-core-readiness"
    I=0
    STABLE=0
    while [ "$I" -lt 60 ]; do
        PIDS="$(pidof xray 2>/dev/null || true)"
        set -- $PIDS
        if [ "$#" -eq 1 ] && kill -0 "$1" 2>/dev/null; then
            STABLE=$((STABLE + 1))
            if [ "$STABLE" -ge 3 ]; then
                CORE_RESTART_STAGE="ready"
                CORE_RESTART_EXIT="0"
                return 0
            fi
        else
            STABLE=0
        fi
        short_pause
        I=$((I + 1))
    done
    CORE_RESTART_EXIT="readiness-timeout"
    return 1
}

restart_if_needed() {
    if [ "$MODE" = apply-core ] || [ "$MODE" = recover-core ]; then
        if [ "${FREENET_PROVIDER_RTT_MANUAL:-0}" = 1 ]; then
            restart_xray_core yes
        else
            restart_xray_core no
        fi
        return $?
    fi
    [ "$WAS_RUNNING" -eq 1 ] || return 0
    CORE_RESTART_STAGE="xkeen-full-restart"
    if production_core_command "$XKEEN_BIN" -restart >/dev/null 2>&1; then
        sleep 4
    else
        CORE_RESTART_EXIT="$?"
        return 1
    fi
    CORE_RESTART_STAGE="readiness"
    if pidof xray >/dev/null 2>&1; then
        CORE_RESTART_STAGE="ready"
        CORE_RESTART_EXIT="0"
        return 0
    fi
    CORE_RESTART_EXIT="no-process"
    return 1
}

verify_restored_provider_files() {
    if [ "$OUT_BEFORE_EXISTS" = yes ]; then
        cmp -s "$OUT_BEFORE" "$OUT_FILE" || return 1
    else
        [ ! -e "$OUT_FILE" ] && [ ! -L "$OUT_FILE" ] || return 1
    fi
    if [ "$PROFILE_BEFORE_EXISTS" = yes ]; then
        cmp -s "$PROFILE_BEFORE" "$PROFILE_FILE" || return 1
    else
        [ ! -e "$PROFILE_FILE" ] && [ ! -L "$PROFILE_FILE" ] || return 1
    fi
    if [ "$FILTER_BEFORE_EXISTS" = yes ]; then
        cmp -s "$FILTER_BEFORE" "$FILTER_FILE" || return 1
    else
        [ ! -e "$FILTER_FILE" ] && [ ! -L "$FILTER_FILE" ] || return 1
    fi
    return 0
}

restore_previously_stopped_core() {
    if [ -n "$CORE_RESTART_HELPER" ]; then
        production_core_command "$CORE_RESTART_HELPER" stop || return 1
        [ -z "$(pidof xray 2>/dev/null || true)" ]
        return $?
    fi
    PIDS="$(pidof xray 2>/dev/null || true)"
    set -- $PIDS
    [ "$#" -eq 0 ] && return 0
    [ "$#" -eq 1 ] || return 1
    STOP_PID="$1"
    kill "$STOP_PID" 2>/dev/null || return 1
    I=0
    while kill -0 "$STOP_PID" 2>/dev/null && [ "$I" -lt 40 ]; do short_pause; I=$((I + 1)); done
    if kill -0 "$STOP_PID" 2>/dev/null; then
        kill -9 "$STOP_PID" 2>/dev/null || return 1
        I=0
        while kill -0 "$STOP_PID" 2>/dev/null && [ "$I" -lt 10 ]; do short_pause; I=$((I + 1)); done
    fi
    [ -z "$(pidof xray 2>/dev/null || true)" ]
}

rollback_state() {
    ROLLBACK_ACTIVE=1
    ROLLBACK_FILES_STATUS="UNKNOWN"
    ROLLBACK_CORE_STATUS="UNKNOWN"
    ROLLBACK_VPN_STATUS="UNKNOWN"
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

    # Restore bytes and restore VPN availability are separate facts. The old
    # endpoint can already be dead (the reason emergency failover began).
    if verify_restored_provider_files; then
        ROLLBACK_FILES_STATUS="RESTORED"
    else
        ROLLBACK_FILES_STATUS="FAILED"
        RB=1
    fi
    if [ "$WAS_RUNNING" -eq 1 ]; then
        if [ "$MODE" = apply-core ] || [ "$MODE" = recover-core ]; then
            if restart_xray_core yes; then
                ROLLBACK_CORE_STATUS="RESTORED"
            else
                ROLLBACK_CORE_STATUS="FAILED"
                RB=1
            fi
        else
            if production_core_command "$XKEEN_BIN" -restart >/dev/null 2>&1 && pidof xray >/dev/null 2>&1; then
                ROLLBACK_CORE_STATUS="RESTORED"
            else
                ROLLBACK_CORE_STATUS="FAILED"
                RB=1
            fi
        fi
    else
        if { [ "$MODE" = apply-core ] || [ "$MODE" = recover-core ]; } && [ "${FREENET_PROVIDER_RTT_MANUAL:-0}" = 1 ]; then
            if restore_previously_stopped_core; then
                ROLLBACK_CORE_STATUS="PREVIOUSLY_STOPPED"
            else
                ROLLBACK_CORE_STATUS="FAILED"
                RB=1
            fi
        else
            ROLLBACK_CORE_STATUS="PREVIOUSLY_STOPPED"
        fi
    fi
    if [ "$RB" -eq 0 ]; then
        if provider_route_probe "$OUT_FILE"; then
            ROLLBACK_VPN_STATUS="REACHABLE"
        else
            ROLLBACK_VPN_STATUS="UNREACHABLE"
            # An already-dead endpoint does not invalidate restored files/core.
        fi
    else
        ROLLBACK_VPN_STATUS="NOT_TESTED"
    fi
    # These statuses contain no profile identity, endpoint, path or secrets.
    err "ROLLBACK DETAIL: files=$ROLLBACK_FILES_STATUS; core=$ROLLBACK_CORE_STATUS; vpn=$ROLLBACK_VPN_STATUS"
    append_provider_history 'rollback' "files=$ROLLBACK_FILES_STATUS; core=$ROLLBACK_CORE_STATUS; vpn=$ROLLBACK_VPN_STATUS"
    ROLLBACK_ACTIVE=0
    [ "$RB" -eq 0 ]
}

append_provider_history() {
    RESULT="$(sanitize_history "$1")"
    MESSAGE="$(sanitize_history "$2")"
    [ -n "$RESULT" ] || RESULT='unknown'
    [ -n "$MESSAGE" ] || MESSAGE='provider VPN switch'
    mkdir -p "$(dirname "$HISTORY_FILE")" 2>/dev/null || return 0
    printf '%s\t%s\t%s\t%s\n' "$(date -u '+%Y-%m-%dT%H:%M:%SZ')" 'VPN switch' "$RESULT" "$MESSAGE" >> "$HISTORY_FILE" 2>/dev/null || return 0
    if [ -f "$HISTORY_FILE" ]; then
        awk 'NF { lines[++n]=$0 } END { start=n-49; if (start<1) start=1; for (i=start;i<=n;i++) print lines[i] }' "$HISTORY_FILE" > "$HISTORY_FILE.trim.$$" 2>/dev/null && mv -f "$HISTORY_FILE.trim.$$" "$HISTORY_FILE" 2>/dev/null || rm -f "$HISTORY_FILE.trim.$$" 2>/dev/null || true
    fi
}

fail_apply() {
    MESSAGE="$1"
    err "PRIMARY ERROR: $MESSAGE"
    append_provider_history 'failed' "VPN server apply failed: $MESSAGE"
    if [ "$APPLIED" -eq 1 ] && [ "$ROLLBACK_ACTIVE" -eq 0 ]; then
        if rollback_state; then
            provider_finalization_status rollback-finalizing pending || {
                err 'ROLLBACK ERROR/STATE: FAILED/UNKNOWN (finalization phase not confirmed)'
                exit 2
            }
            if [ "$MODE" = recover-core ]; then
                if archive_recovery_attempt ROLLED_BACK; then
                    complete_provider_finalization rollback-finalizing rolled-back || {
                        err 'ROLLBACK ERROR/STATE: FAILED/UNKNOWN (finalization cleanup not confirmed)'
                        exit 2
                    }
                    err 'ROLLBACK ERROR/STATE: rollback success (original checkpoint retained; another explicit candidate is allowed)'
                    exit 1
                fi
                err 'ROLLBACK ERROR/STATE: FAILED/UNKNOWN (attempt archive not confirmed)'
                exit 2
            fi
            if [ "$ROLLBACK_VPN_STATUS" != REACHABLE ] || { [ "$ROLLBACK_CORE_STATUS" = PREVIOUSLY_STOPPED ] && [ "${FREENET_PROVIDER_RTT_MANUAL:-0}" = 1 ]; }; then
                complete_provider_finalization rollback-finalizing rolled-back || {
                    err 'ROLLBACK ERROR/STATE: FAILED/UNKNOWN (finalization cleanup not confirmed)'
                    exit 2
                }
                err 'ROLLBACK ERROR/STATE: rollback success (files/core restored, old VPN unavailable; protected manual recovery is available)'
                exit 1
            fi
            if clear_checkpoint; then
                complete_provider_finalization rollback-finalizing rolled-back || {
                    err 'ROLLBACK ERROR/STATE: FAILED/UNKNOWN (finalization cleanup not confirmed)'
                    exit 2
                }
                err 'ROLLBACK ERROR/STATE: rollback success'
                exit 1
            fi
            err 'ROLLBACK ERROR/STATE: FAILED/UNKNOWN (checkpoint retained)'
            exit 2
        fi
        err 'ROLLBACK ERROR/STATE: FAILED/UNKNOWN'
        exit 2
    fi
    err 'ROLLBACK ERROR/STATE: no live apply'
    exit 1
}

MODE="${1:-plan}"
REQUESTED_ID="${2:-}"

if [ "$MODE" = core-restart ]; then
    mutation_forbidden && { err 'PRIMARY ERROR: pending VPN transaction requires read-only reconciliation'; exit 2; }
    acquire_mutation_lock || {
        err 'PRIMARY ERROR: another VPN mutation is already running'
        err 'ROLLBACK ERROR/STATE: no live apply'
        exit 1
    }
    TMP_DIR="$(mktemp -d /tmp/freenet-core-restart.XXXXXX 2>/dev/null)"
    [ -n "$TMP_DIR" ] && [ -d "$TMP_DIR" ] || { err 'cannot create core restart temporary directory'; exit 1; }
    restart_xray_core yes || { err 'core-only Xray restart failed'; exit 1; }
    say '[FreeNet Provider] CORE_RESTART=SUCCESS'
    exit 0
fi

case "$MODE" in plan|apply|apply-core|recover-core) ;; *) err 'usage: apply_provider_profile.sh [plan|apply|apply-core|recover-core|core-restart] PROFILE_ID'; exit 2 ;; esac
case "$REQUESTED_ID" in
    ''|*[!0-9a-f]*) err 'PROFILE_ID must be 16 lowercase hex characters'; exit 2 ;;
esac
[ "${#REQUESTED_ID}" -eq 16 ] || { err 'PROFILE_ID must be 16 lowercase hex characters'; exit 2; }

if [ -n "${FREENET_OWNED_XRAY_DIR:-}${FREENET_OWNED_XRAY_NONCE:-}${FREENET_OWNED_XRAY_WORK_DIR:-}" ]; then
    case "$MODE" in
        apply-core|recover-core)
            [ "${FREENET_PROVIDER_FINALIZATION_NONCE:-}" = "$FREENET_OWNED_XRAY_NONCE" ] &&
                owned_xray_init "$(dirname "$PROVIDER_CACHE")" || { err 'owned Xray channel is invalid'; exit 2; }
            ;;
        plan|apply)
            owned_xray_init "$(dirname "$FREENET_OWNED_XRAY_DIR")" || { err 'owned Xray channel is invalid'; exit 2; }
            ;;
        *) err 'owned Xray channel is not valid for this provider mode'; exit 2 ;;
    esac
fi

for C in jq sed awk grep tr head base64 sha256sum mktemp cp mv mkdir dirname pidof date; do
    command -v "$C" >/dev/null 2>&1 || { err "required command missing: $C"; exit 1; }
done
[ -x "$XRAY_BIN" ] || { err 'Xray binary is missing'; exit 1; }
[ -d "$CONFIG_DIR" ] || { err 'Xray config directory is missing'; exit 1; }
[ -d "$ASSET_DIR" ] || { err 'Xray asset directory is missing'; exit 1; }
[ -s "$SUB_FILE" ] || { err 'subscription is not configured'; exit 1; }

if [ "$MODE" != plan ]; then
    if [ "$MODE" = recover-core ]; then
        [ "${FREENET_PROVIDER_RECOVERY_MANUAL:-0}" = 1 ] &&
            [ "${FREENET_PROVIDER_RTT_MANUAL:-0}" = 1 ] ||
            fail_apply 'manual recovery requires explicit authenticated selection'
        valid_recovery_checkpoint ||
            fail_apply 'protected VPN checkpoint is incomplete or recovery was interrupted; STOP'
        acquire_mutation_lock recover-core || fail_apply 'another VPN mutation is already running'
        valid_recovery_checkpoint ||
            fail_apply 'protected VPN checkpoint changed before manual recovery; STOP'
    else
        mutation_forbidden && fail_apply 'pending VPN transaction requires read-only reconciliation'
        acquire_mutation_lock || fail_apply 'another VPN mutation is already running'
        mutation_forbidden && fail_apply 'pending VPN transaction requires read-only reconciliation'
    fi
fi

SUB_URL="$(tr -d '\r\n' < "$SUB_FILE")"
case "$SUB_URL" in https://*) SUB_PORT=443 ;; *) err 'subscription URL must use HTTPS'; exit 1 ;; esac
SUB_REST="${SUB_URL#https://}"
SUB_AUTH="${SUB_REST%%/*}"
case "$SUB_AUTH" in *:*) SUB_HOST="${SUB_AUTH%%:*}"; SUB_PORT="${SUB_AUTH##*:}" ;; *) SUB_HOST="$SUB_AUTH" ;; esac
[ -n "$SUB_HOST" ] || { err 'subscription host is empty'; exit 1; }
case "$SUB_PORT" in ''|*[!0-9]*) err 'subscription port is invalid'; exit 1 ;; esac

if [ "$OWNED_XRAY_ENABLED" -eq 1 ]; then
    TMP_DIR="$FREENET_OWNED_XRAY_WORK_DIR"
else
    TMP_DIR="$(mktemp -d /tmp/freenet-provider.XXXXXX 2>/dev/null)"
fi
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

prepare_subscription || { err 'fresh subscription unavailable and secure provider cache is missing or does not match'; exit 1; }

select_profile || { err 'requested Extra profile is not present in the prepared subscription'; exit 1; }
build_vless_object || { err 'cannot build selected VLESS profile'; exit 1; }
build_candidate || { err 'cannot build candidate 04_outbounds.json'; exit 1; }
validate_candidate || { err 'candidate Xray configuration validation failed'; exit 1; }
# A successful explicit RTT sweep already measured this exact selected VPN.
# For manual emergency apply only, avoid a second isolated route preflight.
# The Xray config test above, state snapshot, live post-check and rollback
# below remain mandatory and are never bypassed.
CANDIDATE_ROUTE_STATUS=yes
if { [ "$MODE" = apply-core ] || [ "$MODE" = recover-core ]; } && [ "${FREENET_PROVIDER_RTT_MANUAL:-0}" = 1 ]; then
    CANDIDATE_ROUTE_STATUS=skipped
else
    provider_route_probe "$CANDIDATE_OUT" || { err 'candidate VPN application route validation failed'; exit 1; }
fi

say '========== FreeNet Provider Plan =========='
say "PROFILE_ID=$REQUESTED_ID"
say "PROFILE_NAME=$SELECTED_NAME"
say "ENDPOINT=$SELECTED_ADDRESS:$SELECTED_PORT"
if [ -f "$OUT_FILE" ]; then say 'CURRENT_OUTBOUND=present'; else say 'CURRENT_OUTBOUND=missing'; fi
if pidof xray >/dev/null 2>&1; then say 'XRAY_RUNNING=yes'; else say 'XRAY_RUNNING=no'; fi
say 'CANDIDATE_XRAY_VALID=yes'
say "CANDIDATE_ROUTE_OK=$CANDIDATE_ROUTE_STATUS"
say 'EXPECTED_DELTA=install or replace exactly one vless-reality outbound; preserve existing non-VLESS outbounds; persist safe preferred profile name and exact active profile filter'
say 'EXPECTED_NO_DELTA=subscription URL and VLESS/Reality credentials are never printed; ISP/DNS/routing are not changed by this helper'
case "$MODE" in
    apply|apply-core|recover-core) say 'MUTATION=PENDING' ;;
    *) say 'MUTATION=NONE' ;;
esac
say '========== END =========='

[ "$MODE" = plan ] && exit 0

snapshot_state || fail_apply 'cannot snapshot current provider state'
if [ "$MODE" = apply-core ] || [ "$MODE" = recover-core ]; then
    [ "$WAS_RUNNING" -eq 1 ] || [ "${FREENET_PROVIDER_RTT_MANUAL:-0}" = 1 ] || fail_apply 'safe core-only Xray restart requires exactly one running Xray process'
    core_restart_preflight || fail_apply "safe core-only Xray restart preflight failed; reason=$CORE_PREFLIGHT_REASON"
fi
if [ "$MODE" = recover-core ]; then
    if [ -e "$CHECKPOINT_DIR/recovery.pending" ]; then
        # Candidate/config validation and the writer fence precede reconciliation.
        archive_recovery_attempt RECONCILED || fail_apply 'cannot archive complete prior recovery evidence; STOP before mutation'
    fi
    begin_recovery_checkpoint || fail_apply 'cannot persist separate protected manual recovery snapshot; STOP before mutation'
else
    begin_checkpoint || fail_apply 'cannot persist protected VPN transaction checkpoint; STOP before mutation'
fi
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

restart_if_needed || fail_apply "Xray/XKeen runtime acceptance failed after provider apply; stage=$CORE_RESTART_STAGE; code=$CORE_RESTART_EXIT"
validate_live || fail_apply 'live Xray configuration validation failed after provider apply'
provider_route_probe "$OUT_FILE" || fail_apply 'live VPN application route validation failed after provider apply'

provider_finalization_status accepted-finalizing pending || {
    err 'PRIMARY ERROR: VPN accepted but finalization phase is UNKNOWN; STOP'
    err 'ROLLBACK ERROR/STATE: FAILED/UNKNOWN'
    exit 2
}
if [ "$MODE" = recover-core ]; then
    archive_recovery_checkpoint || {
        err 'PRIMARY ERROR: manual VPN connected but original checkpoint archive is UNKNOWN; STOP'
        err 'ROLLBACK ERROR/STATE: FAILED/UNKNOWN'
        exit 2
    }
    say '[FreeNet Provider] RECOVERY=COMPLETED'
else
    clear_checkpoint || { err 'PRIMARY ERROR: VPN applied but checkpoint cleanup is UNKNOWN; STOP'; exit 2; }
fi
append_provider_history 'success' "VPN server applied: $SELECTED_NAME · $SELECTED_ADDRESS:$SELECTED_PORT"
complete_provider_finalization accepted-finalizing accepted || {
    err 'PRIMARY ERROR: VPN accepted but finalization cleanup is UNKNOWN; STOP'
    err 'ROLLBACK ERROR/STATE: FAILED/UNKNOWN'
    exit 2
}
say '[FreeNet Provider] RESULT=SUCCESS'
say '[FreeNet Provider] ROLLBACK=NOT_NEEDED'
