#!/bin/sh

# AUTO VPN v1: refresh only the endpoint of the current logical Extra profile.
# Country/profile switching is forbidden. Ambiguity is fail-closed.

ROOT="${FREENET_ROOT:-/opt}"
CONFIG_FILE="${FREENET_CONFIG_FILE:-$ROOT/etc/freenet/freenet.conf}"
SUB_FILE="${FREENET_SUB_FILE:-$ROOT/etc/xray/blanc_subscription.url}"
PROFILE_FILE="${FREENET_PROFILE_FILE:-$ROOT/etc/freenet/vpn_profile_name}"
FILTER_FILE="${FREENET_FILTER_FILE:-$ROOT/etc/xray/blanc_profile_filter.regex}"
CONFIG_DIR="${FREENET_CONFIG_DIR:-$ROOT/etc/xray/configs}"
OUT_FILE="${FREENET_OUT_FILE:-$CONFIG_DIR/04_outbounds.json}"
ASSET_DIR="${FREENET_ASSET_DIR:-$ROOT/etc/xray/dat}"
UPDATER_BIN="${FREENET_UPDATER_BIN:-$ROOT/bin/blanc_xkeen_update_outbounds.sh}"
XKEEN_BIN="${FREENET_XKEEN_BIN:-$ROOT/sbin/xkeen}"
XRAY_BIN="${FREENET_XRAY_BIN:-$ROOT/sbin/xray}"
CRONTAB_BIN="${FREENET_CRONTAB_BIN:-crontab}"
STATE_FILE="${FREENET_AUTOMATION_STATE:-$ROOT/var/run/freenet-automation.state}"
HISTORY_FILE="${FREENET_AUTOMATION_HISTORY:-$ROOT/var/log/freenet-automation.history}"
RUNTIME_LOG="${FREENET_AUTO_VPN_LOG:-$ROOT/var/log/freenet-auto-vpn.log}"
BACKUP_DIR="${FREENET_AUTO_VPN_BACKUP:-$ROOT/backups/freenet-auto-vpn-last}"
LOCK_DIR="${FREENET_AUTO_VPN_LOCK:-/tmp/freenet-auto-vpn.lock}"
CURL_BIN="${FREENET_CURL_BIN:-curl}"
BOOTSTRAP_DNS_PRIMARY="77.88.8.8"
BOOTSTRAP_DNS_SECONDARY="8.8.8.8"
MODE="${1:-run}"
TMP_DIR=""
LOCK_HELD=0

say() { printf '%s\n' "$*"; }
err() { printf '[FreeNet AUTO VPN] ERROR: %s\n' "$*" >&2; }

cleanup() {
    [ -n "$TMP_DIR" ] && rm -rf "$TMP_DIR" 2>/dev/null || true
    if [ "$LOCK_HELD" = 1 ]; then
        rm -rf "$LOCK_DIR" 2>/dev/null || true
        LOCK_HELD=0
    fi
}
trap cleanup 0 1 2 15

now() { date -u +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || echo unknown; }

safe_text() {
    printf '%s' "$1" | tr '\r\n\t' '   ' | sed 's/vless:\/\/[^ ]*/[redacted]/g; s#https\?://[^ ]*#[redacted]#g'
}

write_state() {
    RESULT="$1"
    REASON="$(safe_text "$2")"
    ROLLBACK_READY="${3:-no}"
    DIR="$(dirname "$STATE_FILE")"
    mkdir -p "$DIR" 2>/dev/null || return 1
    T="$STATE_FILE.tmp.$$"
    {
        echo "LAST_RUN=$(now)"
        echo "LAST_RESULT=$RESULT"
        echo "LAST_REASON=$REASON"
        echo "ROLLBACK_READY=$ROLLBACK_READY"
    } > "$T" || return 1
    chmod 600 "$T" 2>/dev/null || true
    mv -f "$T" "$STATE_FILE"
}

append_history() {
    RESULT="$1"
    REASON="$(safe_text "$2")"
    DIR="$(dirname "$HISTORY_FILE")"
    mkdir -p "$DIR" 2>/dev/null || return 0
    printf '%s\tAUTO VPN\t%s\t%s\n' "$(now)" "$RESULT" "$REASON" >> "$HISTORY_FILE" 2>/dev/null || return 0
    if [ -f "$HISTORY_FILE" ]; then
        tail -n 50 "$HISTORY_FILE" > "$HISTORY_FILE.tmp.$$" 2>/dev/null && mv -f "$HISTORY_FILE.tmp.$$" "$HISTORY_FILE" 2>/dev/null || true
        chmod 600 "$HISTORY_FILE" 2>/dev/null || true
    fi
}

finish_no_mutation() {
    RESULT="$1"
    REASON="$2"
    write_state "$RESULT" "$REASON" no || true
    append_history "$RESULT" "$REASON"
    say "RESULT=$RESULT"
    say "REASON=$(safe_text "$REASON")"
    say 'MUTATION=NONE'
    exit 0
}

fail_no_mutation() {
    REASON="$1"
    write_state failed "$REASON" no || true
    append_history failed "$REASON"
    err "$REASON"
    say "ERROR=$(safe_text "$REASON")"
    say 'MUTATION=NONE'
    exit 1
}

config_value() {
    KEY="$1"; DEFAULT="$2"
    VALUE="$(sed -n "s/^${KEY}=//p" "$CONFIG_FILE" 2>/dev/null | tail -n 1 | tr -d "'\"\r")"
    [ -n "$VALUE" ] && printf '%s\n' "$VALUE" || printf '%s\n' "$DEFAULT"
}

set_config_value() {
    KEY="$1"; VALUE="$2"
    T="$CONFIG_FILE.new.$$"
    awk -v key="$KEY" -v value="$VALUE" '
        BEGIN { found=0 }
        $0 ~ "^" key "=" { print key "=" value; found=1; next }
        { print }
        END { if (!found) print key "=" value }
    ' "$CONFIG_FILE" > "$T" || return 1
    chmod 600 "$T" 2>/dev/null || true
    mv -f "$T" "$CONFIG_FILE"
}

cron_for_interval() {
    case "$1" in
        30m) printf '%s\n' '*/30 * * * *' ;;
        1h) printf '%s\n' '0 * * * *' ;;
        3h) printf '%s\n' '0 */3 * * *' ;;
        6h) printf '%s\n' '0 */6 * * *' ;;
        manual) printf '%s\n' '' ;;
        *) return 1 ;;
    esac
}

rebuild_managed_cron() {
    CURRENT="$TMP_DIR/cron.current"
    NEW="$TMP_DIR/cron.new"
    "$CRONTAB_BIN" -l > "$CURRENT" 2>/dev/null || : > "$CURRENT"
    awk '
        /^# BEGIN FREENET$/ {skip=1; next}
        /^# END FREENET$/ {skip=0; next}
        skip {next}
        /[[:space:]]\/opt\/bin\/blanc_xkeen_update_outbounds\.sh([[:space:]]|$)/ {next}
        /[[:space:]]\/opt\/lib\/freenet\/auto_vpn\.sh[[:space:]]+run([[:space:]]|$)/ {next}
        /[[:space:]]\/opt\/bin\/vpn[[:space:]]+failover([[:space:]]|$)/ {next}
        /[[:space:]]\/opt\/sbin\/xkeen[[:space:]]+-ug([[:space:]]|$)/ {next}
        {print}
    ' "$CURRENT" > "$NEW" || return 1

    AUTO_VPN_V1="$(config_value AUTO_VPN_V1 no)"
    AUTO_VPN_V1_INTERVAL="$(config_value AUTO_VPN_V1_INTERVAL manual)"
    AUTO_ENDPOINT_CRON="$(config_value AUTO_ENDPOINT_CRON '')"
    AUTO_VPN_FAILOVER="$(config_value AUTO_VPN_FAILOVER no)"
    AUTO_VPN_FAILOVER_CRON="$(config_value AUTO_VPN_FAILOVER_CRON '*/5 * * * *')"
    AUTO_XKEEN_GEODATA="$(config_value AUTO_XKEEN_GEODATA yes)"
    AUTO_XKEEN_GEODATA_CRON="$(config_value AUTO_XKEEN_GEODATA_CRON '30 6 * * *')"

    {
        echo '# BEGIN FREENET'
        if [ "$AUTO_XKEEN_GEODATA" = yes ]; then
            echo "$AUTO_XKEEN_GEODATA_CRON /opt/sbin/xkeen -ug"
        fi
        if [ "$AUTO_VPN_V1" = yes ] && [ "$AUTO_VPN_V1_INTERVAL" != manual ] && [ -n "$AUTO_ENDPOINT_CRON" ]; then
            echo "$AUTO_ENDPOINT_CRON /opt/lib/freenet/auto_vpn.sh run >> /opt/var/log/freenet-auto-vpn.log 2>&1"
        else
            echo '# AUTO VPN v1 scheduler disabled by FreeNet settings'
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

configure() {
    shift
    ENABLED=""; INTERVAL=""; PROVIDED_CRON=""
    while [ "$#" -gt 0 ]; do
        case "$1" in
            --enabled) [ "$#" -ge 2 ] || exit 2; ENABLED="$2"; shift 2 ;;
            --interval) [ "$#" -ge 2 ] || exit 2; INTERVAL="$2"; shift 2 ;;
            --cron) [ "$#" -ge 2 ] || exit 2; PROVIDED_CRON="$2"; shift 2 ;;
            *) err 'unsupported configure argument'; exit 2 ;;
        esac
    done
    case "$ENABLED" in true|false) ;; *) err 'enabled must be true or false'; exit 2 ;; esac
    CRON="$(cron_for_interval "$INTERVAL")" || { err 'unsupported interval'; exit 2; }
    [ -z "$PROVIDED_CRON" ] || [ "$PROVIDED_CRON" = "$CRON" ] || { err 'cron does not match interval'; exit 2; }
    [ -f "$CONFIG_FILE" ] || { err 'FreeNet config is missing'; exit 1; }
    TMP_DIR="$(mktemp -d /tmp/freenet-auto-vpn-config.XXXXXX 2>/dev/null)" || exit 1
    CONFIG_BEFORE="$TMP_DIR/freenet.conf.before"; CRON_BEFORE="$TMP_DIR/cron.before"
    cp -p "$CONFIG_FILE" "$CONFIG_BEFORE" || exit 1
    "$CRONTAB_BIN" -l > "$CRON_BEFORE" 2>/dev/null || : > "$CRON_BEFORE"

    if [ "$ENABLED" = true ]; then AUTO_VPN_VALUE=yes; else AUTO_VPN_VALUE=no; fi
    if [ "$ENABLED" = true ] && [ "$INTERVAL" != manual ]; then LEGACY_ENDPOINT=yes; else LEGACY_ENDPOINT=no; fi
    set_config_value AUTO_VPN_V1 "$AUTO_VPN_VALUE" || CONFIG_FAIL=1
    set_config_value AUTO_VPN_V1_INTERVAL "$INTERVAL" || CONFIG_FAIL=1
    set_config_value AUTO_ENDPOINT_UPDATE "$LEGACY_ENDPOINT" || CONFIG_FAIL=1
    if [ -n "$CRON" ]; then set_config_value AUTO_ENDPOINT_CRON "'$CRON'" || CONFIG_FAIL=1; fi
    if [ "${CONFIG_FAIL:-0}" = 1 ] || ! rebuild_managed_cron; then
        cp -p "$CONFIG_BEFORE" "$CONFIG_FILE" 2>/dev/null || true
        "$CRONTAB_BIN" "$CRON_BEFORE" 2>/dev/null || true
        err 'cannot commit AUTO VPN settings; previous config/cron restored'
        exit 1
    fi
    say 'RESULT=SUCCESS'
    say "AUTO_VPN_V1=$AUTO_VPN_VALUE"
    say "AUTO_VPN_V1_INTERVAL=$INTERVAL"
    say 'ROLLBACK=NOT_NEEDED'
}

acquire_lock() {
    if mkdir "$LOCK_DIR" 2>/dev/null; then
        LOCK_HELD=1
        echo "$$" > "$LOCK_DIR/pid" 2>/dev/null || true
        return 0
    fi
    OLD_PID="$(cat "$LOCK_DIR/pid" 2>/dev/null)"
    if [ -n "$OLD_PID" ] && kill -0 "$OLD_PID" 2>/dev/null; then
        finish_no_mutation busy 'AUTO VPN check skipped: another AUTO VPN operation is active'
    fi
    rm -rf "$LOCK_DIR" 2>/dev/null || return 1
    mkdir "$LOCK_DIR" 2>/dev/null || return 1
    LOCK_HELD=1
    echo "$$" > "$LOCK_DIR/pid" 2>/dev/null || true
}

fetch_subscription() {
    URL="$1"; OUT="$2"; : > "$OUT"
    if "$CURL_BIN" -4 -fsSL -H 'Cache-Control: no-cache' -H 'Pragma: no-cache' --connect-timeout 20 --max-time 60 -A 'Mozilla/5.0' "$URL" > "$OUT" 2>/dev/null; then return 0; fi
    : > "$OUT"
    if "$CURL_BIN" -4 -fsSL -H 'Cache-Control: no-cache' -H 'Pragma: no-cache' --dns-servers "$BOOTSTRAP_DNS_PRIMARY,$BOOTSTRAP_DNS_SECONDARY" --connect-timeout 20 --max-time 60 -A 'Mozilla/5.0' "$URL" > "$OUT" 2>/dev/null; then return 0; fi
    command -v nslookup >/dev/null 2>&1 || return 1
    case "$URL" in https://*) PORT=443 ;; *) return 1 ;; esac
    REST="${URL#https://}"; AUTH="${REST%%/*}"; HOST="${AUTH%%:*}"; [ "$HOST" = "$AUTH" ] || PORT="${AUTH##*:}"
    for DNS in "$BOOTSTRAP_DNS_PRIMARY" "$BOOTSTRAP_DNS_SECONDARY"; do
        IP="$(nslookup "$HOST" "$DNS" 2>/dev/null | awk '/^Name:/{seen=1;next} seen && /^Address [0-9]+:/ {if($3~/^[0-9]+\./){print $3;exit}}')"
        [ -n "$IP" ] || continue
        : > "$OUT"
        "$CURL_BIN" -4 -fsSL -H 'Cache-Control: no-cache' -H 'Pragma: no-cache' --resolve "$HOST:$PORT:$IP" --connect-timeout 20 --max-time 60 -A 'Mozilla/5.0' "$URL" > "$OUT" 2>/dev/null && return 0
    done
    return 1
}

read_endpoint() {
    FILE="$1"
    ADDRESS="$(jq -r '.outbounds[]? | select(.tag=="vless-reality") | .settings.vnext[0].address // empty' "$FILE" 2>/dev/null | head -n 1)"
    PORT="$(jq -r '.outbounds[]? | select(.tag=="vless-reality") | .settings.vnext[0].port // empty' "$FILE" 2>/dev/null | head -n 1)"
    [ -n "$ADDRESS" ] && [ -n "$PORT" ] || return 1
    ENDPOINT="$ADDRESS:$PORT"
}

parse_candidate_endpoint() {
    LINE="$1"; BODY="${LINE#vless://}"; case "$BODY" in *@*) ;; *) return 1 ;; esac
    REST="${BODY#*@}"; HOSTPORT="$(printf '%s\n' "$REST" | sed 's/[?].*$//')"
    case "$HOSTPORT" in \[*\]:*) CANDIDATE_ADDRESS="${HOSTPORT%%]*}"; CANDIDATE_ADDRESS="${CANDIDATE_ADDRESS#[}"; CANDIDATE_PORT="${HOSTPORT##*:}" ;; *:*) CANDIDATE_ADDRESS="${HOSTPORT%:*}"; CANDIDATE_PORT="${HOSTPORT##*:}" ;; *) return 1 ;; esac
    case "$CANDIDATE_PORT" in ''|*[!0-9]*) return 1 ;; esac
    [ "$CANDIDATE_PORT" -ge 1 ] 2>/dev/null && [ "$CANDIDATE_PORT" -le 65535 ] 2>/dev/null || return 1
    CANDIDATE_ENDPOINT="$CANDIDATE_ADDRESS:$CANDIDATE_PORT"
}

tcp_connect_ms() {
    H="$1"; P="$2"; case "$H" in *:*) URL_HOST="[$H]" ;; *) URL_HOST="$H" ;; esac
    T="$("$CURL_BIN" --noproxy '*' -k -sS --connect-timeout 3 --max-time 4 -o /dev/null -w '%{time_connect}' "https://$URL_HOST:$P/" 2>/dev/null || true)"
    awk -v t="$T" 'BEGIN { if (t+0 > 0) printf "%d\n", (t*1000)+0.5; else exit 1 }'
}

restore_snapshot() {
    cp -p "$TMP_DIR/out.before" "$OUT_FILE.restore.$$" 2>/dev/null || return 1
    mv -f "$OUT_FILE.restore.$$" "$OUT_FILE" 2>/dev/null || return 1
    cp -p "$TMP_DIR/profile.before" "$PROFILE_FILE.restore.$$" 2>/dev/null || return 1
    mv -f "$PROFILE_FILE.restore.$$" "$PROFILE_FILE" 2>/dev/null || return 1
    cp -p "$TMP_DIR/filter.before" "$FILTER_FILE.restore.$$" 2>/dev/null || return 1
    mv -f "$FILTER_FILE.restore.$$" "$FILTER_FILE" 2>/dev/null || return 1
    "$XKEEN_BIN" -restart >/dev/null 2>&1 || return 1
    sleep 4
    pidof xray >/dev/null 2>&1 || return 1
    XRAY_LOCATION_ASSET="$ASSET_DIR" "$XRAY_BIN" run -test -confdir "$CONFIG_DIR" >/dev/null 2>&1
}

retain_backup() {
    mkdir -p "$BACKUP_DIR" 2>/dev/null || return 1
    cp -p "$TMP_DIR/out.before" "$BACKUP_DIR/04_outbounds.json" || return 1
    cp -p "$TMP_DIR/profile.before" "$BACKUP_DIR/vpn_profile_name" || return 1
    cp -p "$TMP_DIR/filter.before" "$BACKUP_DIR/blanc_profile_filter.regex" || return 1
    chmod 600 "$BACKUP_DIR/04_outbounds.json" "$BACKUP_DIR/vpn_profile_name" 2>/dev/null || true
    return 0
}

run_auto_vpn() {
    acquire_lock || fail_no_mutation 'cannot acquire AUTO VPN lock'
    [ -s "$CONFIG_FILE" ] || fail_no_mutation 'FreeNet config is unavailable'
    [ -s "$SUB_FILE" ] || fail_no_mutation 'subscription is not configured'
    [ -s "$PROFILE_FILE" ] || finish_no_mutation ambiguous 'current logical profile is unavailable'
    [ -s "$FILTER_FILE" ] || finish_no_mutation ambiguous 'current logical profile filter is unavailable'
    [ -s "$OUT_FILE" ] || fail_no_mutation 'current VPN outbound is unavailable'
    [ -x "$UPDATER_BIN" ] || fail_no_mutation 'canonical endpoint updater is unavailable'
    [ -x "$XRAY_BIN" ] || fail_no_mutation 'Xray binary is unavailable'
    [ -x "$XKEEN_BIN" ] || fail_no_mutation 'XKeen binary is unavailable'
    for T in jq grep sed awk base64 mktemp cp mv pidof tail wc; do command -v "$T" >/dev/null 2>&1 || fail_no_mutation "required command missing: $T"; done
    command -v "$CURL_BIN" >/dev/null 2>&1 || fail_no_mutation 'curl is unavailable'

    TMP_DIR="$(mktemp -d /tmp/freenet-auto-vpn.XXXXXX 2>/dev/null)" || fail_no_mutation 'cannot create AUTO VPN staging directory'
    read_endpoint "$OUT_FILE" || fail_no_mutation 'current VPN endpoint cannot be read'
    CURRENT_ADDRESS="$ADDRESS"; CURRENT_PORT="$PORT"; CURRENT_ENDPOINT="$ENDPOINT"
    PROFILE_NAME="$(tr -d '\r\n' < "$PROFILE_FILE")"
    FILTER="$(tr -d '\r\n' < "$FILTER_FILE")"
    [ -n "$PROFILE_NAME" ] && [ -n "$FILTER" ] || finish_no_mutation ambiguous 'current logical profile identity is empty'

    SUB_URL="$(tr -d '\r\n' < "$SUB_FILE")"
    case "$SUB_URL" in https://*) ;; *) fail_no_mutation 'subscription URL is invalid' ;; esac
    RAW="$TMP_DIR/sub.raw"; DECODED="$TMP_DIR/sub.decoded"; MATCHES="$TMP_DIR/matches"
    fetch_subscription "$SUB_URL" "$RAW" || fail_no_mutation 'fresh subscription fetch failed before mutation'
    [ -s "$RAW" ] || fail_no_mutation 'fresh subscription is empty'
    if grep -q '^vless://' "$RAW"; then cp "$RAW" "$DECODED" || fail_no_mutation 'cannot stage subscription'; else base64 -d "$RAW" > "$DECODED" 2>/dev/null || fail_no_mutation 'cannot decode subscription'; fi
    tr -d '\r' < "$DECODED" > "$DECODED.clean" || fail_no_mutation 'cannot normalize subscription'; mv -f "$DECODED.clean" "$DECODED"

    grep '^vless://' "$DECODED" | grep -i 'Extra' | grep -vi 'Expired' | grep -vi 'Whitelist' | grep -Ei "$FILTER" > "$MATCHES" || true
    COUNT="$(wc -l < "$MATCHES" | tr -d '[:space:]')"; case "$COUNT" in ''|*[!0-9]*) COUNT=0 ;; esac
    [ "$COUNT" -gt 0 ] || finish_no_mutation no_new 'fresh subscription has no endpoint for the current logical profile'
    [ "$COUNT" -eq 1 ] || finish_no_mutation ambiguous 'fresh subscription contains multiple matches for the current logical profile; no endpoint was chosen'

    CANDIDATE_LINE="$(head -n 1 "$MATCHES")"
    parse_candidate_endpoint "$CANDIDATE_LINE" || finish_no_mutation uncertain 'fresh endpoint metadata is invalid'
    [ "$CANDIDATE_ENDPOINT" != "$CURRENT_ENDPOINT" ] || finish_no_mutation same 'current endpoint is already actual; update is not required'

    CANDIDATE_MS="$(tcp_connect_ms "$CANDIDATE_ADDRESS" "$CANDIDATE_PORT" 2>/dev/null || true)"
    [ -n "$CANDIDATE_MS" ] || finish_no_mutation uncertain 'fresh endpoint did not pass bounded TCP preflight'
    CURRENT_MS="$(tcp_connect_ms "$CURRENT_ADDRESS" "$CURRENT_PORT" 2>/dev/null || true)"
    if [ -n "$CURRENT_MS" ]; then
        WORSE="$(awk -v old="$CURRENT_MS" -v new="$CANDIDATE_MS" 'BEGIN { if (new > old + 100 && new > old * 1.6) print "yes"; else print "no" }')"
        [ "$WORSE" != yes ] || finish_no_mutation worse 'fresh endpoint is materially slower on bounded preflight; current endpoint preserved'
    fi

    cp -p "$OUT_FILE" "$TMP_DIR/out.before" || fail_no_mutation 'cannot snapshot current outbound'
    cp -p "$PROFILE_FILE" "$TMP_DIR/profile.before" || fail_no_mutation 'cannot snapshot current profile identity'
    cp -p "$FILTER_FILE" "$TMP_DIR/filter.before" || fail_no_mutation 'cannot snapshot current profile filter'
    retain_backup || fail_no_mutation 'cannot retain AUTO VPN rollback snapshot'
    write_state checking 'fresh endpoint found; candidate validation is running' yes || true

    FREENET_ACTION_REASON=refresh FREENET_PROFILE_LABEL="$PROFILE_NAME" "$UPDATER_BIN" > "$TMP_DIR/updater.log" 2>&1
    RC=$?
    if [ "$RC" -ne 0 ]; then
        if restore_snapshot; then
            write_state failed 'candidate apply failed; previous VPN state restored' yes || true
            append_history failed 'candidate apply failed; previous VPN state restored'
            say 'ERROR=candidate apply failed; rollback success'
            say 'ROLLBACK=SUCCESS'
            exit 1
        fi
        write_state rollback_failed 'candidate apply failed and rollback could not be confirmed' no || true
        append_history rollback_failed 'candidate apply failed and rollback could not be confirmed; STOP'
        say 'ERROR=candidate apply failed; rollback failed or unknown'
        say 'ROLLBACK=FAILED_UNKNOWN'
        exit 2
    fi

    read_endpoint "$OUT_FILE" || POST_FAIL=1
    POST_ENDPOINT="${ENDPOINT:-}"
    POST_PROFILE="$(tr -d '\r\n' < "$PROFILE_FILE" 2>/dev/null)"
    POST_FILTER="$(tr -d '\r\n' < "$FILTER_FILE" 2>/dev/null)"
    if [ "${POST_FAIL:-0}" = 1 ] || [ "$POST_ENDPOINT" != "$CANDIDATE_ENDPOINT" ] || [ "$POST_PROFILE" != "$PROFILE_NAME" ] || [ "$POST_FILTER" != "$FILTER" ]; then
        if restore_snapshot; then
            write_state failed 'post-apply acceptance failed; previous VPN state restored' yes || true
            append_history failed 'post-apply acceptance failed; previous VPN state restored'
            say 'ERROR=post-apply acceptance failed; rollback success'
            say 'ROLLBACK=SUCCESS'
            exit 1
        fi
        write_state rollback_failed 'post-apply acceptance failed and rollback could not be confirmed' no || true
        append_history rollback_failed 'post-apply acceptance failed and rollback could not be confirmed; STOP'
        say 'ERROR=post-apply acceptance failed; rollback failed or unknown'
        say 'ROLLBACK=FAILED_UNKNOWN'
        exit 2
    fi

    write_state updated 'new endpoint validated, applied and confirmed; logical profile/country unchanged' yes || true
    append_history updated 'new endpoint validated, applied and confirmed; logical profile/country unchanged'
    say 'RESULT=updated'
    say 'REASON=new endpoint validated, applied and confirmed; logical profile/country unchanged'
    say 'ROLLBACK=NOT_NEEDED'
}

case "$MODE" in
    configure) configure "$@" ;;
    run) run_auto_vpn ;;
    *) err 'usage: auto_vpn.sh [run|configure --enabled true|false --interval 30m|1h|3h|6h|manual]'; exit 2 ;;
esac
