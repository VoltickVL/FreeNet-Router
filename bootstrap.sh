#!/bin/sh

# Точка входа продуктовой установки FreeNet Router.
# Предпосылка: пользователь уже подготовил USB + Entware/OPKG.
# Дальше этот скрипт устанавливает или сохраняет core stack, ставит FreeNet
# Control Center, нормализует управляемый cron и передаёт выбор VPN/ISP/DNS
# браузерному мастеру без скрытых сетевых изменений.

REPO="VoltickVL/FreeNet-Router"
RELEASE_BASE="${FREENET_RELEASE_BASE:-https://github.com/$REPO/releases/latest/download}"
ROOT="/opt"
CONFIG_DIR="$ROOT/etc/freenet"
CONFIG_FILE="$CONFIG_DIR/freenet.conf"
PIN_FILE="$CONFIG_DIR/upstream-pins.env"
BOOTSTRAP_LIB="$ROOT/lib/freenet/bootstrap_entware.sh"
MIGRATE_LIB="$ROOT/lib/freenet/migrate_split_dns.sh"
NETWORK_LIB="$ROOT/lib/freenet/apply_network_profile.sh"
PROVIDER_LIB="$ROOT/lib/freenet/apply_provider_profile.sh"
FINALIZE_LIB="$ROOT/lib/freenet/finalize_setup.sh"
FREENET_BIN="$ROOT/sbin/freenet-ui"
FREENET_INIT="$ROOT/etc/init.d/S99freenet-ui"
FREENET_MANAGER="$ROOT/bin/freenet"
VPN_BIN="$ROOT/bin/vpn"
UPDATER_BIN="$ROOT/bin/blanc_xkeen_update_outbounds.sh"
SUB_FILE="$ROOT/etc/xray/blanc_subscription.url"
OUT_FILE="$ROOT/etc/xray/configs/04_outbounds.json"
UI_PORT=1001

TMP_DIR=""
BACKUP_DIR=""
LAN_IP=""
ARCH=""
MUTATED=0
ROLLBACK_ACTIVE=0
UI_WAS_RUNNING=0
LAST_DOWNLOAD_ERROR=""
CORE_MODE=""

say() { printf '%s\n' "$*"; }
info() { printf '\n[FreeNet Setup] %s\n' "$*"; }
ok() { printf '[FreeNet Setup] %s: OK\n' "$*"; }
err() { printf '[FreeNet Setup] ERROR: %s\n' "$*" >&2; }

cleanup() {
    [ -n "$TMP_DIR" ] && rm -rf "$TMP_DIR" 2>/dev/null || true
}

make_tmp() {
    [ -n "$TMP_DIR" ] && [ -d "$TMP_DIR" ] && return 0
    TMP_DIR="$(mktemp -d /tmp/freenet-setup.XXXXXX 2>/dev/null)"
    if [ -z "$TMP_DIR" ] || [ ! -d "$TMP_DIR" ]; then
        TMP_DIR="/tmp/freenet-setup.$$"
        mkdir -p "$TMP_DIR" || return 1
    fi
}

get_arch() {
    A="$(opkg print-architecture 2>/dev/null)"
    case "$A" in
        *aarch64*) ARCH="arm64-v8a" ;;
        *mipsel*) ARCH="mips32le" ;;
        *mips*) ARCH="mips32" ;;
        *) return 1 ;;
    esac
}

get_lan_ip() {
    LAN_IP="$(ip -4 addr show br0 2>/dev/null | sed -n 's/.*inet \([0-9.]*\)\/.*/\1/p' | head -n 1)"
    [ -n "$LAN_IP" ]
}

bootstrap_ip() {
    H="$1"
    command -v nslookup >/dev/null 2>&1 || return 1
    for DNS in 77.88.8.8 8.8.8.8; do
        IP="$(nslookup "$H" "$DNS" 2>/dev/null | awk '
            /^Name:/ {seen=1; next}
            seen && /^Address [0-9]+:/ {if ($3 ~ /^[0-9]+\./) {print $3; exit}}
            seen && /^Address:/ {if ($2 ~ /^[0-9]+\./) {print $2; exit}}
        ')"
        [ -n "$IP" ] && { printf '%s\n' "$IP"; return 0; }
    done
    return 1
}

url_host() {
    printf '%s\n' "$1" | sed -n 's#^https://\([^/]*\)/.*#\1#p'
}

download_url() {
    URL="$1"
    OUT="$2"
    CUR="$URL"
    I=0
    LAST_DOWNLOAD_ERROR=""

    while [ "$I" -lt 8 ]; do
        I=$((I + 1))
        H="$(url_host "$CUR")"
        [ -n "$H" ] || { LAST_DOWNLOAD_ERROR="invalid HTTPS URL"; return 1; }

        HDR="$TMP_DIR/headers.$I"
        BODY="$TMP_DIR/body.$I"
        ERR="$TMP_DIR/curl.$I.err"
        rm -f "$HDR" "$BODY" "$ERR"

        IP="$(bootstrap_ip "$H")"
        if [ -n "$IP" ]; then
            curl -fsS --connect-timeout 20 --max-time 180 --resolve "$H:443:$IP" -D "$HDR" "$CUR" -o "$BODY" 2>"$ERR"
            RC=$?
        else
            curl -fsS --connect-timeout 20 --max-time 180 -D "$HDR" "$CUR" -o "$BODY" 2>"$ERR"
            RC=$?
        fi
        if [ "$RC" -ne 0 ]; then
            LAST_DOWNLOAD_ERROR="$(tail -n 1 "$ERR" 2>/dev/null)"
            [ -n "$LAST_DOWNLOAD_ERROR" ] || LAST_DOWNLOAD_ERROR="curl rc=$RC for $H"
            return 1
        fi

        CODE="$(awk '/^HTTP\// {code=$2} END {print code}' "$HDR")"
        case "$CODE" in
            200|206)
                mv -f "$BODY" "$OUT" || return 1
                return 0
                ;;
            301|302|303|307|308)
                LOC="$(sed -n 's/^[Ll]ocation:[[:space:]]*//p' "$HDR" | tr -d '\r' | tail -n 1)"
                [ -n "$LOC" ] || { LAST_DOWNLOAD_ERROR="redirect without Location"; return 1; }
                case "$LOC" in
                    https://*) CUR="$LOC" ;;
                    /*) CUR="https://$H$LOC" ;;
                    *) LAST_DOWNLOAD_ERROR="unsupported redirect"; return 1 ;;
                esac
                ;;
            *) LAST_DOWNLOAD_ERROR="HTTP ${CODE:-UNKNOWN} from $H"; return 1 ;;
        esac
    done

    LAST_DOWNLOAD_ERROR="too many redirects"
    return 1
}

verify_asset() {
    NAME="$1"
    FILE="$2"
    EXPECTED="$(awk -v n="$NAME" '$2==n {print $1; exit}' "$TMP_DIR/SHA256SUMS")"
    [ -n "$EXPECTED" ] || { err "SHA256SUMS has no $NAME"; return 1; }
    ACTUAL="$(sha256sum "$FILE" | awk '{print $1}')"
    [ "$ACTUAL" = "$EXPECTED" ] || { err "SHA-256 mismatch for $NAME"; return 1; }
}

download_asset() {
    NAME="$1"
    printf '[FreeNet Setup] Download %-34s ' "$NAME..."
    if ! download_url "$RELEASE_BASE/$NAME" "$TMP_DIR/$NAME"; then
        printf 'FAIL\n'
        err "download failed: ${LAST_DOWNLOAD_ERROR:-unknown}"
        return 1
    fi
    if [ "$NAME" != SHA256SUMS ]; then
        verify_asset "$NAME" "$TMP_DIR/$NAME" || { printf 'FAIL\n'; return 1; }
    fi
    printf 'OK\n'
}

ensure_prerequisites() {
    [ -d "$ROOT" ] || { err 'Entware /opt not found'; exit 1; }
    command -v opkg >/dev/null 2>&1 || { err 'Entware opkg not found'; exit 1; }

    for T in curl sha256sum sed awk grep mktemp ip nslookup; do
        command -v "$T" >/dev/null 2>&1 || {
            err "required Entware tool missing before bootstrap: $T"
            exit 1
        }
    done

    get_arch || { err 'unsupported Entware architecture'; exit 1; }
    get_lan_ip || { err 'cannot determine br0 LAN IPv4'; exit 1; }
    make_tmp || { err 'cannot create temporary directory'; exit 1; }
}

download_release_assets() {
    info 'Downloading signed FreeNet release assets through bootstrap DNS...'
    download_asset SHA256SUMS || exit 1
    UI_ASSET="freenet-ui-$ARCH"
    for NAME in \
        bootstrap_entware.sh upstream-pins.env \
        migrate_split_dns.sh apply_network_profile.sh apply_provider_profile.sh finalize_setup.sh \
        "$UI_ASSET" vpn blanc_xkeen_update_outbounds.sh \
        freenet freenet.conf.example
    do
        download_asset "$NAME" || exit 1
    done
    chmod 755 \
        "$TMP_DIR/bootstrap_entware.sh" \
        "$TMP_DIR/migrate_split_dns.sh" \
        "$TMP_DIR/apply_network_profile.sh" \
        "$TMP_DIR/apply_provider_profile.sh" \
        "$TMP_DIR/finalize_setup.sh" \
        "$TMP_DIR/$UI_ASSET" "$TMP_DIR/vpn" \
        "$TMP_DIR/blanc_xkeen_update_outbounds.sh" "$TMP_DIR/freenet"
    ok 'release SHA-256 verification'
}

classify_core() {
    FREENET_PIN_FILE="$TMP_DIR/upstream-pins.env" sh "$TMP_DIR/bootstrap_entware.sh" plan > "$TMP_DIR/core-plan.txt" 2>&1 || {
        cat "$TMP_DIR/core-plan.txt"
        err 'core bootstrap preflight failed'
        exit 1
    }
    CORE_MODE="$(sed -n 's/^MODE=//p' "$TMP_DIR/core-plan.txt" | head -n 1)"
    case "$CORE_MODE" in
        ENTWARE_ONLY|READY_EXISTING_STACK) ;;
        NEEDS_REVIEW|NO_ENTWARE|UNSUPPORTED_ARCH|'')
            cat "$TMP_DIR/core-plan.txt"
            err "core state $CORE_MODE requires review; no app mutation started"
            exit 1
            ;;
        *) err "unknown core state: $CORE_MODE"; exit 1 ;;
    esac
    say "[FreeNet Setup] CORE_MODE=$CORE_MODE"
}

ensure_core() {
    if [ "$CORE_MODE" = READY_EXISTING_STACK ]; then
        ok 'existing XKeen/Xray stack preserved'
        return 0
    fi

    info 'Clean Entware detected. Installing pinned XKeen/Xray/XKeen UI core...'
    FREENET_PIN_FILE="$TMP_DIR/upstream-pins.env" sh "$TMP_DIR/bootstrap_entware.sh" apply
    RC=$?
    if [ "$RC" -eq 2 ]; then
        err 'core bootstrap rollback FAILED/UNKNOWN; stop all mutation and inspect runtime'
        exit 2
    fi
    [ "$RC" -eq 0 ] || { err 'core bootstrap failed/rolled back; app install not started'; exit 1; }
    ok 'pinned core bootstrap'
}

backup_one() {
    PATH_NOW="$1"
    KEY="$2"
    if [ -f "$PATH_NOW" ]; then
        cp -p "$PATH_NOW" "$BACKUP_DIR/$KEY.before" || return 1
        echo yes > "$BACKUP_DIR/$KEY.exists"
    else
        echo no > "$BACKUP_DIR/$KEY.exists"
    fi
}

restore_one() {
    PATH_NOW="$1"
    KEY="$2"
    if [ -f "$BACKUP_DIR/$KEY.exists" ] && [ "$(cat "$BACKUP_DIR/$KEY.exists")" = yes ]; then
        mkdir -p "$(dirname "$PATH_NOW")" 2>/dev/null || true
        cp -p "$BACKUP_DIR/$KEY.before" "$PATH_NOW" 2>/dev/null || return 1
    else
        rm -f "$PATH_NOW" 2>/dev/null || return 1
    fi
}

snapshot_xray() {
    OUT="$1"
    : > "$OUT"
    if [ -d "$ROOT/etc/xray/configs" ]; then
        find "$ROOT/etc/xray/configs" -maxdepth 1 -type f -name '*.json' -print 2>/dev/null | sort | while IFS= read -r F; do
            sha256sum "$F" || exit 1
        done > "$OUT" || return 1
    fi
}

backup_app() {
    STAMP="$(date +%Y%m%d-%H%M%S 2>/dev/null)"
    [ -n "$STAMP" ] || STAMP="$$"
    BACKUP_DIR="$ROOT/backups/freenet-setup-$STAMP"
    mkdir -p "$BACKUP_DIR" || return 1
    pidof freenet-ui >/dev/null 2>&1 && UI_WAS_RUNNING=1 || UI_WAS_RUNNING=0

    backup_one "$FREENET_BIN" freenet-ui || return 1
    backup_one "$FREENET_INIT" S99freenet-ui || return 1
    backup_one "$FREENET_MANAGER" freenet-manager || return 1
    backup_one "$VPN_BIN" vpn || return 1
    backup_one "$UPDATER_BIN" updater || return 1
    backup_one "$CONFIG_FILE" freenet-conf || return 1
    backup_one "$PIN_FILE" pins || return 1
    backup_one "$BOOTSTRAP_LIB" bootstrap-lib || return 1
    backup_one "$MIGRATE_LIB" migrate-lib || return 1
    backup_one "$NETWORK_LIB" network-lib || return 1
    backup_one "$PROVIDER_LIB" provider-lib || return 1
    backup_one "$FINALIZE_LIB" finalize-lib || return 1
    crontab -l > "$BACKUP_DIR/crontab.before" 2>/dev/null || : > "$BACKUP_DIR/crontab.before"
    snapshot_xray "$BACKUP_DIR/xray-hashes.before" || return 1
}

rollback_app() {
    [ "$MUTATED" = 1 ] || return 0
    [ -n "$BACKUP_DIR" ] && [ -d "$BACKUP_DIR" ] || return 1
    ROLLBACK_ACTIVE=1
    say '[FreeNet Setup] ROLLBACK: restoring app files and cron...'

    [ -x "$FREENET_INIT" ] && "$FREENET_INIT" stop >/dev/null 2>&1 || true
    killall freenet-ui >/dev/null 2>&1 || true

    RB=0
    restore_one "$FREENET_BIN" freenet-ui || RB=1
    restore_one "$FREENET_INIT" S99freenet-ui || RB=1
    restore_one "$FREENET_MANAGER" freenet-manager || RB=1
    restore_one "$VPN_BIN" vpn || RB=1
    restore_one "$UPDATER_BIN" updater || RB=1
    restore_one "$CONFIG_FILE" freenet-conf || RB=1
    restore_one "$PIN_FILE" pins || RB=1
    restore_one "$BOOTSTRAP_LIB" bootstrap-lib || RB=1
    restore_one "$MIGRATE_LIB" migrate-lib || RB=1
    restore_one "$NETWORK_LIB" network-lib || RB=1
    restore_one "$PROVIDER_LIB" provider-lib || RB=1
    restore_one "$FINALIZE_LIB" finalize-lib || RB=1
    crontab "$BACKUP_DIR/crontab.before" >/dev/null 2>&1 || RB=1

    if [ "$UI_WAS_RUNNING" = 1 ] && [ -x "$FREENET_INIT" ]; then
        "$FREENET_INIT" start >/dev/null 2>&1 || RB=1
    fi
    ROLLBACK_ACTIVE=0
    [ "$RB" -eq 0 ]
}

fail_app() {
    err "$1"
    if [ "$MUTATED" = 1 ] && [ "$ROLLBACK_ACTIVE" = 0 ]; then
        if rollback_app; then
            say '[FreeNet Setup] ROLLBACK: SUCCESS'
            cleanup
            exit 1
        fi
        say '[FreeNet Setup] ROLLBACK ERROR: FAILED/UNKNOWN' >&2
        cleanup
        exit 2
    fi
    cleanup
    exit 1
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

write_config_if_missing() {
    mkdir -p "$CONFIG_DIR" || return 1
    if [ ! -f "$CONFIG_FILE" ]; then
        cp "$TMP_DIR/freenet.conf.example" "$CONFIG_FILE" || return 1
        chmod 600 "$CONFIG_FILE" 2>/dev/null || true
    fi

    # Свежая установка начинается в незавершённом состоянии. Обновление endpoint
    # запрещено, пока браузерный мастер не создаст и не примет совместимые
    # provider/dns-out состояния. Существующие явные настройки сохраняются.
    if ! grep -q '^SETUP_COMPLETE=' "$CONFIG_FILE" 2>/dev/null; then
        printf '%s\n' 'SETUP_COMPLETE=no' >> "$CONFIG_FILE" || return 1
    fi

    # Пользователь не выбирает тип установки вручную. Он определяется до mutation:
    # готовый XKeen/Xray сохраняется, а ENTWARE_ONLY получает pinned core FreeNet.
    case "$CORE_MODE" in
        READY_EXISTING_STACK) INSTALL_SCENARIO=existing_stack ;;
        ENTWARE_ONLY) INSTALL_SCENARIO=fresh_entware ;;
        *) return 1 ;;
    esac
    set_config_value INSTALL_SCENARIO "$INSTALL_SCENARIO" || return 1
}

config_value() {
    KEY="$1"
    DEFAULT="$2"
    VALUE="$(sed -n "s/^${KEY}=//p" "$CONFIG_FILE" 2>/dev/null | tail -n 1 | tr -d "'\"\r")"
    [ -n "$VALUE" ] && printf '%s\n' "$VALUE" || printf '%s\n' "$DEFAULT"
}

has_dns_out() {
    [ -f "$OUT_FILE" ] || return 1
    command -v jq >/dev/null 2>&1 || return 1
    jq -e '([.outbounds[]? | select(.tag == "dns-out")] | length) == 1' "$OUT_FILE" >/dev/null 2>&1
}

apply_safe_cron() {
    C1="$TMP_DIR/cron.current"
    C2="$TMP_DIR/cron.new"
    crontab -l > "$C1" 2>/dev/null || : > "$C1"

    awk '
        /^# BEGIN FREENET$/ {skip=1; next}
        /^# END FREENET$/ {skip=0; next}
        skip {next}
        /[[:space:]]\/opt\/bin\/blanc_xkeen_update_outbounds\.sh([[:space:]]|$)/ {next}
        /[[:space:]]\/opt\/sbin\/xkeen[[:space:]]+-ug([[:space:]]|$)/ {next}
        {print}
    ' "$C1" > "$C2" || return 1

    AUTO_ENDPOINT_UPDATE="$(config_value AUTO_ENDPOINT_UPDATE no)"
    AUTO_ENDPOINT_CRON="$(config_value AUTO_ENDPOINT_CRON '*/15 * * * *')"
    AUTO_XKEEN_GEODATA="$(config_value AUTO_XKEEN_GEODATA yes)"
    AUTO_XKEEN_GEODATA_CRON="$(config_value AUTO_XKEEN_GEODATA_CRON '30 6 * * *')"
    SETUP_COMPLETE="$(config_value SETUP_COMPLETE no)"

    {
        echo '# BEGIN FREENET'
        if [ "$AUTO_XKEEN_GEODATA" = yes ]; then
            echo "$AUTO_XKEEN_GEODATA_CRON /opt/sbin/xkeen -ug"
        fi
        if [ "$SETUP_COMPLETE" = yes ] && [ "$AUTO_ENDPOINT_UPDATE" = yes ] && [ -s "$SUB_FILE" ] && has_dns_out; then
            echo "$AUTO_ENDPOINT_CRON /opt/bin/blanc_xkeen_update_outbounds.sh >> /opt/var/log/blanc_xkeen_update.log 2>&1"
        else
            echo '# endpoint refresh disabled until setup/subscription/dns-out acceptance'
        fi
        echo '# END FREENET'
    } >> "$C2"

    crontab "$C2"
}

write_ui_init() {
    cat > "$FREENET_INIT.tmp.$$" <<EOF
#!/bin/sh
ENABLED=yes
PROCS=freenet-ui
ARGS="-listen $LAN_IP:$UI_PORT"
PREARGS=""
DESC="\$PROCS"
PATH=/opt/sbin:/opt/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
. /opt/etc/init.d/rc.func
EOF
    chmod 755 "$FREENET_INIT.tmp.$$" || return 1
    sh -n "$FREENET_INIT.tmp.$$" || return 1
    mv -f "$FREENET_INIT.tmp.$$" "$FREENET_INIT"
}

install_app() {
    UI_ASSET="freenet-ui-$ARCH"
    mkdir -p "$ROOT/sbin" "$ROOT/bin" "$ROOT/lib/freenet" "$CONFIG_DIR" || return 1

    cp "$TMP_DIR/$UI_ASSET" "$FREENET_BIN.tmp.$$" || return 1
    chmod 755 "$FREENET_BIN.tmp.$$" || return 1
    mv -f "$FREENET_BIN.tmp.$$" "$FREENET_BIN" || return 1

    cp "$TMP_DIR/vpn" "$VPN_BIN.tmp.$$" || return 1
    chmod 755 "$VPN_BIN.tmp.$$" || return 1
    mv -f "$VPN_BIN.tmp.$$" "$VPN_BIN" || return 1

    cp "$TMP_DIR/blanc_xkeen_update_outbounds.sh" "$UPDATER_BIN.tmp.$$" || return 1
    chmod 755 "$UPDATER_BIN.tmp.$$" || return 1
    mv -f "$UPDATER_BIN.tmp.$$" "$UPDATER_BIN" || return 1

    cp "$TMP_DIR/freenet" "$FREENET_MANAGER.tmp.$$" || return 1
    chmod 755 "$FREENET_MANAGER.tmp.$$" || return 1
    mv -f "$FREENET_MANAGER.tmp.$$" "$FREENET_MANAGER" || return 1

    cp "$TMP_DIR/upstream-pins.env" "$PIN_FILE.tmp.$$" || return 1
    chmod 600 "$PIN_FILE.tmp.$$" 2>/dev/null || true
    mv -f "$PIN_FILE.tmp.$$" "$PIN_FILE" || return 1

    cp "$TMP_DIR/bootstrap_entware.sh" "$BOOTSTRAP_LIB.tmp.$$" || return 1
    chmod 755 "$BOOTSTRAP_LIB.tmp.$$" || return 1
    mv -f "$BOOTSTRAP_LIB.tmp.$$" "$BOOTSTRAP_LIB" || return 1

    cp "$TMP_DIR/migrate_split_dns.sh" "$MIGRATE_LIB.tmp.$$" || return 1
    chmod 755 "$MIGRATE_LIB.tmp.$$" || return 1
    mv -f "$MIGRATE_LIB.tmp.$$" "$MIGRATE_LIB" || return 1

    cp "$TMP_DIR/apply_network_profile.sh" "$NETWORK_LIB.tmp.$$" || return 1
    chmod 755 "$NETWORK_LIB.tmp.$$" || return 1
    mv -f "$NETWORK_LIB.tmp.$$" "$NETWORK_LIB" || return 1

    cp "$TMP_DIR/apply_provider_profile.sh" "$PROVIDER_LIB.tmp.$$" || return 1
    chmod 755 "$PROVIDER_LIB.tmp.$$" || return 1
    mv -f "$PROVIDER_LIB.tmp.$$" "$PROVIDER_LIB" || return 1

    cp "$TMP_DIR/finalize_setup.sh" "$FINALIZE_LIB.tmp.$$" || return 1
    chmod 755 "$FINALIZE_LIB.tmp.$$" || return 1
    mv -f "$FINALIZE_LIB.tmp.$$" "$FINALIZE_LIB" || return 1

    write_config_if_missing || return 1
    write_ui_init || return 1
    apply_safe_cron || return 1
}

start_ui() {
    [ -f "$ROOT/etc/init.d/rc.func" ] || return 1
    "$FREENET_INIT" stop >/dev/null 2>&1 || true
    killall freenet-ui >/dev/null 2>&1 || true
    "$FREENET_INIT" start >/dev/null 2>&1 || return 1
    sleep 2
    pidof freenet-ui >/dev/null 2>&1 || return 1
}

validate_app() {
    HEALTH="$(curl -fsS --connect-timeout 3 "http://$LAN_IP:$UI_PORT/healthz" 2>/dev/null)" || return 1
    [ "$HEALTH" = ok ] || return 1

    curl -fsS --connect-timeout 3 "http://$LAN_IP:$UI_PORT/api/auth/status" -o "$TMP_DIR/auth-status.json" 2>/dev/null || return 1
    jq -e '(.configured | type) == "boolean" and (.authenticated | type) == "boolean"' "$TMP_DIR/auth-status.json" >/dev/null 2>&1 || return 1

    netstat -lntp 2>/dev/null | grep "$LAN_IP:$UI_PORT[[:space:]]" >/dev/null 2>&1 || return 1
    if netstat -lntp 2>/dev/null | grep -E "0\.0\.0\.0:$UI_PORT[[:space:]]|:::$UI_PORT[[:space:]]" >/dev/null 2>&1; then
        return 1
    fi

    snapshot_xray "$TMP_DIR/xray-hashes.after" || return 1
    cmp "$BACKUP_DIR/xray-hashes.before" "$TMP_DIR/xray-hashes.after" >/dev/null 2>&1 || return 1
}

on_signal() {
    if [ "$MUTATED" = 1 ] && [ "$ROLLBACK_ACTIVE" = 0 ]; then
        if rollback_app; then
            say '[FreeNet Setup] ROLLBACK: SUCCESS after signal'
        else
            say '[FreeNet Setup] ROLLBACK ERROR: FAILED/UNKNOWN after signal' >&2
        fi
    fi
    cleanup
    exit 130
}

trap cleanup 0
trap on_signal 1 2 15

ensure_prerequisites
download_release_assets
classify_core
ensure_core

# Core bootstrap/migration завершается до начала изменений FreeNet app-фазы.
backup_app || { err 'cannot create app/cron backup'; exit 1; }
MUTATED=1
install_app || fail_app 'cannot install FreeNet Control Center files/cron safely'
start_ui || fail_app 'FreeNet Control Center failed to start'
validate_app || fail_app 'FreeNet Control Center app acceptance failed'
MUTATED=0

SETUP_COMPLETE="$(config_value SETUP_COMPLETE no)"
INSTALL_SCENARIO="$(config_value INSTALL_SCENARIO unknown)"
SUB_STATE=no; [ -s "$SUB_FILE" ] && SUB_STATE=yes
DNS_STATE=no; has_dns_out && DNS_STATE=yes

info 'FreeNet Control Center is ready.'
say "PANEL=http://$LAN_IP:$UI_PORT/"
say "CORE_MODE=$CORE_MODE"
say "INSTALL_SCENARIO=$INSTALL_SCENARIO"
say "SETUP_COMPLETE=$SETUP_COMPLETE"
say "SUBSCRIPTION_CONFIGURED=$SUB_STATE"
say "DNS_OUT_PRESENT=$DNS_STATE"
say 'XKEEN_CORE_REBUILD=NO for existing stack'
say "BACKUP_APP=$BACKUP_DIR"
say 'XRAY_CONFIG_DELTA=NONE during app phase'
say 'NEXT=Open the panel and finish Provider / ISP / DNS setup in browser.'