#!/bin/sh

REPO="VoltickVL/FreeNet-Router"
RELEASE_BASE="https://github.com/$REPO/releases/latest/download"

FREENET_BIN="/opt/sbin/freenet-ui"
FREENET_INIT="/opt/etc/init.d/S99freenet-ui"
FREENET_MANAGER="/opt/bin/freenet"
VPN_BIN="/opt/bin/vpn"
UPDATER_BIN="/opt/bin/blanc_xkeen_update_outbounds.sh"
CONFIG_DIR="/opt/etc/freenet"
CONFIG_FILE="$CONFIG_DIR/freenet.conf"
SUB_FILE="/opt/etc/xray/blanc_subscription.url"
FILTER_FILE="/opt/etc/xray/blanc_profile_filter.regex"
LOG_FILE="/opt/var/log/blanc_xkeen_update.log"

UI_PORT=1001
AUTO_ENDPOINT_UPDATE=yes
AUTO_ENDPOINT_CRON="50 6 * * *"
AUTO_XKEEN_GEODATA=yes
AUTO_XKEEN_GEODATA_CRON="30 6 * * *"

TMP_DIR=""
ARCH=""
LAN_IP=""
BACKUP_DIR=""
MUTATED=0
ROLLBACK_ACTIVE=0
UI_WAS_RUNNING=0

say() { printf '%s\n' "$*"; }
info() { printf '\n[FreeNet] %s\n' "$*"; }

cleanup() {
    [ -n "$TMP_DIR" ] && rm -rf "$TMP_DIR" 2>/dev/null
}

restore_one() {
    R_PATH="$1"
    R_KEY="$2"
    if [ -f "$BACKUP_DIR/$R_KEY.exists" ] && [ "$(cat "$BACKUP_DIR/$R_KEY.exists")" = "yes" ]; then
        mkdir -p "$(dirname "$R_PATH")" 2>/dev/null || true
        cp -p "$BACKUP_DIR/$R_KEY.before" "$R_PATH" 2>/dev/null || return 1
    else
        rm -f "$R_PATH" 2>/dev/null || return 1
    fi
    return 0
}

rollback_install() {
    [ -n "$BACKUP_DIR" ] && [ -d "$BACKUP_DIR" ] || return 1
    ROLLBACK_ACTIVE=1
    say ""
    say "[FreeNet] ROLLBACK: возвращаю состояние до установки..."

    [ -x "$FREENET_INIT" ] && "$FREENET_INIT" stop >/dev/null 2>&1 || :
    killall freenet-ui >/dev/null 2>&1 || :

    RB_OK=1
    restore_one "$FREENET_BIN" freenet-ui || RB_OK=0
    restore_one "$FREENET_INIT" S99freenet-ui || RB_OK=0
    restore_one "$FREENET_MANAGER" freenet-manager || RB_OK=0
    restore_one "$VPN_BIN" vpn || RB_OK=0
    restore_one "$UPDATER_BIN" updater || RB_OK=0
    restore_one "$CONFIG_FILE" freenet-conf || RB_OK=0
    restore_one "$SUB_FILE" subscription-url || RB_OK=0

    if [ -f "$BACKUP_DIR/crontab.before" ]; then
        crontab "$BACKUP_DIR/crontab.before" >/dev/null 2>&1 || RB_OK=0
    fi

    if [ "$UI_WAS_RUNNING" = "1" ] && [ -x "$FREENET_INIT" ]; then
        "$FREENET_INIT" start >/dev/null 2>&1 || RB_OK=0
    fi

    MUTATED=0
    ROLLBACK_ACTIVE=0
    [ "$RB_OK" = "1" ]
}

fail() {
    printf '\n[FreeNet] ОШИБКА: %s\n' "$*" >&2
    if [ "$MUTATED" = "1" ] && [ "$ROLLBACK_ACTIVE" = "0" ]; then
        if rollback_install; then
            say "[FreeNet] ROLLBACK: успешно."
        else
            say "[FreeNet] ROLLBACK ERROR: состояние требует read-only проверки." >&2
        fi
    fi
    cleanup
    exit 1
}

trap cleanup 0 1 2 15

read_tty() {
    IFS= read -r REPLY < /dev/tty
}

confirm() {
    printf '%s [y/N]: ' "$1" > /dev/tty
    read_tty
    case "$REPLY" in
        y|Y|yes|YES|да|ДА|д|Д) return 0 ;;
        *) return 1 ;;
    esac
}

load_config() {
    UI_PORT=1001
    AUTO_ENDPOINT_UPDATE=yes
    AUTO_ENDPOINT_CRON="50 6 * * *"
    AUTO_XKEEN_GEODATA=yes
    AUTO_XKEEN_GEODATA_CRON="30 6 * * *"

    if [ -f "$CONFIG_FILE" ]; then
        . "$CONFIG_FILE"
    fi
}

save_config() {
    mkdir -p "$CONFIG_DIR" || return 1
    cat > "$CONFIG_FILE.tmp.$$" <<EOF
UI_PORT=$UI_PORT
AUTO_ENDPOINT_UPDATE=$AUTO_ENDPOINT_UPDATE
AUTO_ENDPOINT_CRON='$AUTO_ENDPOINT_CRON'
AUTO_XKEEN_GEODATA=$AUTO_XKEEN_GEODATA
AUTO_XKEEN_GEODATA_CRON='$AUTO_XKEEN_GEODATA_CRON'
EOF
    chmod 600 "$CONFIG_FILE.tmp.$$" 2>/dev/null || true
    mv -f "$CONFIG_FILE.tmp.$$" "$CONFIG_FILE"
}

get_arch() {
    A="$(opkg print-architecture 2>/dev/null)"
    case "$A" in
        *aarch64*) ARCH="arm64-v8a" ;;
        *mipsel*) ARCH="mips32le" ;;
        *mips*) ARCH="mips32" ;;
        *) fail "не удалось определить поддерживаемую архитектуру Entware" ;;
    esac
}

get_lan_ip() {
    LAN_IP="$(ip -4 addr show br0 2>/dev/null | sed -n 's/.*inet \([0-9.]*\)\/.*/\1/p' | head -n 1)"
    [ -n "$LAN_IP" ] || fail "не удалось определить IPv4 интерфейса br0"
}

bootstrap_ip() {
    H="$1"
    command -v nslookup >/dev/null 2>&1 || return 1
    nslookup "$H" 77.88.8.8 2>/dev/null | awk '
        /^Name:/ { seen=1; next }
        seen && /^Address [0-9]+:/ {
            if ($3 ~ /^[0-9]+\./) { print $3; exit }
        }
    '
}

url_host() {
    printf '%s\n' "$1" | sed -n 's#^https://\([^/]*\)/.*#\1#p'
}

download_url() {
    URL="$1"
    OUT="$2"

    if curl -fLsS --connect-timeout 20 --max-time 180 "$URL" -o "$OUT"; then
        return 0
    fi

    CUR="$URL"
    I=0
    while [ "$I" -lt 6 ]; do
        I=$((I + 1))
        H="$(url_host "$CUR")"
        [ -n "$H" ] || return 1
        IP="$(bootstrap_ip "$H")"
        [ -n "$IP" ] || return 1

        HDR="$TMP_DIR/headers.$I"
        BODY="$TMP_DIR/body.$I"
        rm -f "$HDR" "$BODY"

        curl -fsS --connect-timeout 20 --max-time 180 \
            --resolve "$H:443:$IP" \
            -D "$HDR" \
            "$CUR" \
            -o "$BODY" || return 1

        CODE="$(awk '/^HTTP\// {code=$2} END {print code}' "$HDR")"
        case "$CODE" in
            200|206)
                mv -f "$BODY" "$OUT" || return 1
                return 0
                ;;
            301|302|303|307|308)
                LOC="$(sed -n 's/^[Ll]ocation:[[:space:]]*//p' "$HDR" | tr -d '\r' | tail -n 1)"
                [ -n "$LOC" ] || return 1
                case "$LOC" in
                    https://*) CUR="$LOC" ;;
                    /*) CUR="https://$H$LOC" ;;
                    *) return 1 ;;
                esac
                ;;
            *) return 1 ;;
        esac
    done

    return 1
}

ensure_tools() {
    for T in curl sha256sum sed awk grep mktemp crontab ip netstat nslookup; do
        command -v "$T" >/dev/null 2>&1 || fail "не найдена обязательная команда: $T"
    done

    if ! command -v jq >/dev/null 2>&1; then
        info "jq не найден. Устанавливаю через Entware..."
        opkg update >/dev/null 2>&1 || fail "opkg update завершился ошибкой"
        opkg install jq >/dev/null 2>&1 || fail "не удалось установить jq"
    fi
}

ensure_runtime() {
    [ -d /opt ] || fail "Entware /opt не найден"
    command -v opkg >/dev/null 2>&1 || fail "Entware opkg не найден"
    [ -x /opt/sbin/xkeen ] || fail "XKeen не установлен: /opt/sbin/xkeen отсутствует"
    [ -x /opt/sbin/xray ] || fail "Xray не установлен: /opt/sbin/xray отсутствует"
    [ -d /opt/etc/xray/configs ] || fail "каталог Xray configs не найден"
    ensure_tools
    get_arch
    get_lan_ip
}

make_tmp() {
    TMP_DIR="$(mktemp -d /tmp/freenet-setup.XXXXXX 2>/dev/null)"
    if [ -z "$TMP_DIR" ] || [ ! -d "$TMP_DIR" ]; then
        TMP_DIR="/tmp/freenet-setup.$$"
        mkdir -p "$TMP_DIR" || fail "не удалось создать временный каталог"
    fi
}

verify_asset() {
    NAME="$1"
    FILE="$2"
    SUMS="$3"
    EXPECTED="$(awk -v n="$NAME" '$2==n {print $1; exit}' "$SUMS")"
    [ -n "$EXPECTED" ] || fail "для $NAME нет SHA-256 в SHA256SUMS"
    ACTUAL="$(sha256sum "$FILE" | awk '{print $1}')"
    [ "$EXPECTED" = "$ACTUAL" ] || fail "SHA-256 не совпал для $NAME"
}

download_release() {
    make_tmp
    info "Скачиваю FreeNet release для $ARCH..."

    download_url "$RELEASE_BASE/SHA256SUMS" "$TMP_DIR/SHA256SUMS" || fail "не удалось скачать SHA256SUMS"

    UI_ASSET="freenet-ui-$ARCH"
    for NAME in "$UI_ASSET" vpn blanc_xkeen_update_outbounds.sh freenet freenet.conf.example; do
        download_url "$RELEASE_BASE/$NAME" "$TMP_DIR/$NAME" || fail "не удалось скачать $NAME"
        verify_asset "$NAME" "$TMP_DIR/$NAME" "$TMP_DIR/SHA256SUMS"
    done

    chmod 755 "$TMP_DIR/$UI_ASSET" "$TMP_DIR/vpn" "$TMP_DIR/blanc_xkeen_update_outbounds.sh" "$TMP_DIR/freenet"
    say "Проверка SHA-256: OK"
}

backup_one() {
    B_PATH="$1"
    B_KEY="$2"
    if [ -f "$B_PATH" ]; then
        cp -p "$B_PATH" "$BACKUP_DIR/$B_KEY.before" || fail "не удалось сохранить backup $B_PATH"
        echo yes > "$BACKUP_DIR/$B_KEY.exists"
    else
        echo no > "$BACKUP_DIR/$B_KEY.exists"
    fi
}

backup_current() {
    STAMP="$(date +%Y%m%d-%H%M%S)"
    BACKUP_DIR="/opt/backups/freenet-setup-$STAMP"
    mkdir -p "$BACKUP_DIR" || fail "не удалось создать backup $BACKUP_DIR"

    UI_WAS_RUNNING=0
    pidof freenet-ui >/dev/null 2>&1 && UI_WAS_RUNNING=1

    backup_one "$FREENET_BIN" freenet-ui
    backup_one "$FREENET_INIT" S99freenet-ui
    backup_one "$FREENET_MANAGER" freenet-manager
    backup_one "$VPN_BIN" vpn
    backup_one "$UPDATER_BIN" updater
    backup_one "$CONFIG_FILE" freenet-conf
    backup_one "$SUB_FILE" subscription-url
    crontab -l > "$BACKUP_DIR/crontab.before" 2>/dev/null || :
}

write_init() {
    load_config
    get_lan_ip
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

remove_managed_cron() {
    C1="$TMP_DIR/cron.current"
    C2="$TMP_DIR/cron.clean"
    crontab -l > "$C1" 2>/dev/null || :
    awk '
        /^# BEGIN FREENET$/ {skip=1; next}
        /^# END FREENET$/ {skip=0; next}
        !skip {print}
    ' "$C1" > "$C2"
    crontab "$C2"
}

apply_cron() {
    load_config
    MAKE_TMP_IF_NEEDED=0
    [ -n "$TMP_DIR" ] && [ -d "$TMP_DIR" ] || { make_tmp; MAKE_TMP_IF_NEEDED=1; }

    C1="$TMP_DIR/cron.current"
    C2="$TMP_DIR/cron.new"
    crontab -l > "$C1" 2>/dev/null || :

    awk '
        /^# BEGIN FREENET$/ {skip=1; next}
        /^# END FREENET$/ {skip=0; next}
        !skip {print}
    ' "$C1" > "$C2"

    {
        echo '# BEGIN FREENET'
        if [ "$AUTO_XKEEN_GEODATA" = "yes" ]; then
            echo "$AUTO_XKEEN_GEODATA_CRON /opt/sbin/xkeen -ug"
        fi
        if [ "$AUTO_ENDPOINT_UPDATE" = "yes" ]; then
            echo "$AUTO_ENDPOINT_CRON /opt/bin/blanc_xkeen_update_outbounds.sh >> /opt/var/log/blanc_xkeen_update.log 2>&1"
        fi
        echo '# END FREENET'
    } >> "$C2"

    crontab "$C2" || fail "не удалось применить FreeNet cron"
    [ "$MAKE_TMP_IF_NEEDED" = "1" ] && cleanup
}

install_files() {
    UI_ASSET="freenet-ui-$ARCH"

    cp "$TMP_DIR/$UI_ASSET" "$FREENET_BIN.tmp.$$" || fail "не удалось stage freenet-ui"
    chmod 755 "$FREENET_BIN.tmp.$$" || fail "chmod freenet-ui"
    mv -f "$FREENET_BIN.tmp.$$" "$FREENET_BIN" || fail "не удалось установить freenet-ui"

    cp "$TMP_DIR/vpn" "$VPN_BIN.tmp.$$" || fail "не удалось stage vpn"
    chmod 755 "$VPN_BIN.tmp.$$"
    mv -f "$VPN_BIN.tmp.$$" "$VPN_BIN" || fail "не удалось установить vpn"

    cp "$TMP_DIR/blanc_xkeen_update_outbounds.sh" "$UPDATER_BIN.tmp.$$" || fail "не удалось stage updater"
    chmod 755 "$UPDATER_BIN.tmp.$$"
    mv -f "$UPDATER_BIN.tmp.$$" "$UPDATER_BIN" || fail "не удалось установить updater"

    cp "$TMP_DIR/freenet" "$FREENET_MANAGER.tmp.$$" || fail "не удалось stage manager"
    chmod 755 "$FREENET_MANAGER.tmp.$$"
    mv -f "$FREENET_MANAGER.tmp.$$" "$FREENET_MANAGER" || fail "не удалось установить manager"

    mkdir -p "$CONFIG_DIR" || fail "не удалось создать $CONFIG_DIR"
    if [ ! -f "$CONFIG_FILE" ]; then
        cp "$TMP_DIR/freenet.conf.example" "$CONFIG_FILE" || fail "не удалось создать конфигурацию"
        chmod 600 "$CONFIG_FILE" 2>/dev/null || true
    fi

    write_init || fail "не удалось создать init script"
}

configure_subscription() {
    mkdir -p "$(dirname "$SUB_FILE")" || return 1
    printf 'Введите URL подписки BlancVPN (не будет сохранён в GitHub): ' > /dev/tty
    read_tty
    [ -n "$REPLY" ] || { say "URL не изменён."; return 0; }
    printf '%s\n' "$REPLY" > "$SUB_FILE.tmp.$$" || return 1
    chmod 600 "$SUB_FILE.tmp.$$" 2>/dev/null || true
    mv -f "$SUB_FILE.tmp.$$" "$SUB_FILE"
    say "URL подписки сохранён локально."
}

start_ui() {
    [ -x "$FREENET_INIT" ] || fail "init script FreeNet UI отсутствует"
    "$FREENET_INIT" stop >/dev/null 2>&1 || :
    killall freenet-ui >/dev/null 2>&1 || :
    "$FREENET_INIT" start >/dev/null 2>&1 || fail "FreeNet UI не запустился"
    sleep 2
    pidof freenet-ui >/dev/null 2>&1 || fail "процесс freenet-ui не найден после start"
}

validate_runtime() {
    load_config
    get_lan_ip

    XRAY_LOCATION_ASSET=/opt/etc/xray/dat /opt/sbin/xray run -test -confdir /opt/etc/xray/configs >/tmp/freenet-xray-test.$$ 2>&1 || {
        tail -n 30 /tmp/freenet-xray-test.$$ 2>/dev/null
        rm -f /tmp/freenet-xray-test.$$
        fail "текущий Xray config невалиден"
    }
    rm -f /tmp/freenet-xray-test.$$

    pidof xray >/dev/null 2>&1 || fail "Xray не работает"

    curl -fsS --connect-timeout 3 "http://$LAN_IP:$UI_PORT/healthz" 2>/dev/null | grep -q '^ok' || fail "healthz FreeNet UI не отвечает"

    LISTEN="$(netstat -lntp 2>/dev/null | grep "$LAN_IP:$UI_PORT[[:space:]]" | head -n 1)"
    [ -n "$LISTEN" ] || fail "FreeNet UI не слушает $LAN_IP:$UI_PORT"
}

install_or_update() {
    ensure_runtime
    load_config
    download_release
    backup_current

    info "Устанавливаю FreeNet..."
    MUTATED=1
    install_files
    apply_cron

    if [ ! -s "$SUB_FILE" ]; then
        info "URL подписки ещё не задан."
        configure_subscription || fail "не удалось сохранить URL подписки"
    fi

    start_ui
    validate_runtime
    MUTATED=0

    info "FreeNet установлен/обновлён успешно."
    say "Панель: http://$LAN_IP:$UI_PORT/"
    say "Меню: freenet"
    say "Backup: $BACKUP_DIR"
    say "Текущий VPN/Xray routing установщик не переписывал."
}

show_status() {
    ensure_runtime
    load_config
    get_lan_ip

    say ""
    say "========== FreeNet status =========="
    say "Архитектура: $ARCH"
    say "LAN: $LAN_IP"
    say "UI: http://$LAN_IP:$UI_PORT/"
    if pidof freenet-ui >/dev/null 2>&1; then say "FreeNet UI: работает"; else say "FreeNet UI: не запущен"; fi
    if pidof xray >/dev/null 2>&1; then say "Xray: работает"; else say "Xray: НЕ работает"; fi
    if [ -x "$VPN_BIN" ]; then "$VPN_BIN" current; else say "vpn helper: не установлен"; fi
    say ""
    say "Автообновление endpoint: $AUTO_ENDPOINT_UPDATE ($AUTO_ENDPOINT_CRON)"
    say "XKeen geodata cron: $AUTO_XKEEN_GEODATA ($AUTO_XKEEN_GEODATA_CRON)"
    say ""
    say "Управляемый cron-блок:"
    crontab -l 2>/dev/null | awk '/^# BEGIN FREENET$/{p=1} p{print} /^# END FREENET$/{p=0}'
}

restart_after_config() {
    make_tmp
    backup_current
    MUTATED=1
    write_init || fail "не удалось обновить init script"
    apply_cron
    if [ -x "$FREENET_BIN" ]; then
        start_ui
        validate_runtime
    fi
    MUTATED=0
}

configure_menu() {
    ensure_runtime
    load_config

    while :; do
        say ""
        say "========== Настройки FreeNet =========="
        say "1. Порт FreeNet UI: $UI_PORT"
        say "2. Автообновление endpoint/IP: $AUTO_ENDPOINT_UPDATE"
        say "3. Cron endpoint update: $AUTO_ENDPOINT_CRON"
        say "4. Автообновление XKeen geodata: $AUTO_XKEEN_GEODATA"
        say "5. Cron XKeen geodata: $AUTO_XKEEN_GEODATA_CRON"
        say "6. Задать/изменить URL подписки"
        say "7. Применить настройки"
        say "0. Назад"
        printf '> ' > /dev/tty
        read_tty
        case "$REPLY" in
            1)
                printf 'Новый порт: ' > /dev/tty
                read_tty
                case "$REPLY" in
                    ''|*[!0-9]*) say "Некорректный порт." ;;
                    *) UI_PORT="$REPLY"; save_config || fail "не удалось сохранить config" ;;
                esac
                ;;
            2)
                if [ "$AUTO_ENDPOINT_UPDATE" = "yes" ]; then AUTO_ENDPOINT_UPDATE=no; else AUTO_ENDPOINT_UPDATE=yes; fi
                save_config || fail "не удалось сохранить config"
                ;;
            3)
                printf 'Cron (5 полей, например 50 6 * * *): ' > /dev/tty
                read_tty
                [ -n "$REPLY" ] && AUTO_ENDPOINT_CRON="$REPLY" && save_config
                ;;
            4)
                if [ "$AUTO_XKEEN_GEODATA" = "yes" ]; then AUTO_XKEEN_GEODATA=no; else AUTO_XKEEN_GEODATA=yes; fi
                save_config || fail "не удалось сохранить config"
                ;;
            5)
                printf 'Cron (5 полей, например 30 6 * * *): ' > /dev/tty
                read_tty
                [ -n "$REPLY" ] && AUTO_XKEEN_GEODATA_CRON="$REPLY" && save_config
                ;;
            6) configure_subscription || fail "не удалось сохранить URL подписки" ;;
            7) restart_after_config; say "Настройки применены." ;;
            0) return ;;
            *) say "Неверный выбор." ;;
        esac
        load_config
    done
}

uninstall_freenet() {
    ensure_runtime
    make_tmp
    if ! confirm "Удалить FreeNet UI, manager, vpn helper и updater? Xray configs и URL подписки будут сохранены."; then
        say "Отмена."
        return
    fi

    backup_current
    [ -x "$FREENET_INIT" ] && "$FREENET_INIT" stop >/dev/null 2>&1 || :
    killall freenet-ui >/dev/null 2>&1 || :
    remove_managed_cron || fail "не удалось убрать FreeNet cron"
    rm -f "$FREENET_BIN" "$FREENET_INIT" "$FREENET_MANAGER" "$VPN_BIN" "$UPDATER_BIN"
    say "FreeNet удалён. Xray configs, subscription URL и filter сохранены."
    say "Backup: $BACKUP_DIR"
}

print_menu() {
    ensure_runtime
    load_config
    clear 2>/dev/null || :
    say "========================================"
    say "              FreeNet"
    say "========================================"
    if [ -x "$FREENET_BIN" ]; then say "Статус: установлен"; else say "Статус: не установлен"; fi
    say "Архитектура: $ARCH"
    say ""
    say "1. Установить / переустановить"
    say "2. Обновить"
    say "3. Настройки"
    say "4. Статус"
    say "5. Удалить"
    say "0. Выйти"
    printf '> ' > /dev/tty
    read_tty
    case "$REPLY" in
        1|2) install_or_update ;;
        3) configure_menu ;;
        4) show_status ;;
        5) uninstall_freenet ;;
        0) exit 0 ;;
        *) fail "неверный выбор" ;;
    esac
}

case "${1:-}" in
    install|reinstall|update) install_or_update ;;
    status) show_status ;;
    configure|config) configure_menu ;;
    uninstall|remove) uninstall_freenet ;;
    -h|--help|help)
        say "FreeNet: install|update|status|configure|uninstall"
        ;;
    *) print_menu ;;
esac
