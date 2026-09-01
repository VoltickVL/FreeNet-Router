#!/bin/sh

SETUP_URL="https://raw.githubusercontent.com/VoltickVL/FreeNet-Router/main/setup.sh"
SETUP_HOST="raw.githubusercontent.com"
TMP_DIR=""
CRON_BEFORE=""
CRON_CLEAN=""
SETUP_FILE=""
SUCCESS=0
CRON_TOUCHED=0

say() { printf '%s\n' "$*"; }
fail() { printf '\n[FreeNet] ОШИБКА: %s\n' "$*" >&2; exit 1; }

cleanup() {
    if [ "$SUCCESS" != "1" ] && [ "$CRON_TOUCHED" = "1" ] && [ -f "$CRON_BEFORE" ]; then
        crontab "$CRON_BEFORE" >/dev/null 2>&1 || true
    fi
    [ -n "$TMP_DIR" ] && rm -rf "$TMP_DIR" 2>/dev/null
}
trap cleanup 0 1 2 15

for tool in curl awk crontab mktemp nslookup; do
    command -v "$tool" >/dev/null 2>&1 || fail "не найдена обязательная команда: $tool"
done

TMP_DIR="$(mktemp -d /tmp/freenet-bootstrap.XXXXXX 2>/dev/null)"
[ -n "$TMP_DIR" ] && [ -d "$TMP_DIR" ] || fail "не удалось создать временный каталог"

CRON_BEFORE="$TMP_DIR/crontab.before"
CRON_CLEAN="$TMP_DIR/crontab.clean"
SETUP_FILE="$TMP_DIR/setup.sh"

crontab -l > "$CRON_BEFORE" 2>/dev/null || : > "$CRON_BEFORE"

# Миграция старой ручной схемы FreeNet: удаляем только две команды,
# которыми теперь управляет блок # BEGIN/END FREENET. Прочие cron-задачи сохраняются.
awk '
    /^# BEGIN FREENET$/ {managed=1; print; next}
    /^# END FREENET$/ {managed=0; print; next}
    managed {print; next}
    /[[:space:]]\/opt\/sbin\/xkeen[[:space:]]+-ug([[:space:]]|$)/ {next}
    /[[:space:]]\/opt\/bin\/blanc_xkeen_update_outbounds\.sh([[:space:]]|$)/ {next}
    {print}
' "$CRON_BEFORE" > "$CRON_CLEAN" || fail "не удалось подготовить миграцию cron"

if ! cmp "$CRON_BEFORE" "$CRON_CLEAN" >/dev/null 2>&1; then
    crontab "$CRON_CLEAN" || fail "не удалось временно нормализовать legacy cron"
    CRON_TOUCHED=1
    say "[FreeNet] Legacy cron нормализован; backup будет восстановлен при ошибке."
fi

if ! curl -fLsS --connect-timeout 20 --max-time 120 "$SETUP_URL" -o "$SETUP_FILE"; then
    IP="$(nslookup "$SETUP_HOST" 77.88.8.8 2>/dev/null | awk '/^Name:/{seen=1;next} seen && /^Address [0-9]+:/ {if ($3 ~ /^[0-9]+\./){print $3;exit}}')"
    [ -n "$IP" ] || fail "не удалось разрешить $SETUP_HOST через bootstrap DNS"
    curl -fLsS --resolve "$SETUP_HOST:443:$IP" --connect-timeout 20 --max-time 120 "$SETUP_URL" -o "$SETUP_FILE" \
        || fail "не удалось скачать setup.sh"
fi

sh -n "$SETUP_FILE" || fail "скачанный setup.sh имеет ошибку синтаксиса"

/opt/bin/sh "$SETUP_FILE" install
RC=$?
[ "$RC" -eq 0 ] || fail "setup.sh завершился с кодом $RC"

SUCCESS=1
say "[FreeNet] Bootstrap завершён успешно."
exit 0
