#!/bin/sh

set -eu

ROOT_DIR="$(CDPATH= cd "$(dirname "$0")/.." && pwd)"
BOOT="$ROOT_DIR/bootstrap.sh"
INSTALL="$ROOT_DIR/install.sh"
CONF="$ROOT_DIR/config/freenet.conf.example"
RELEASE="$ROOT_DIR/.github/workflows/release.yml"

fail() { echo "контракт product bootstrap FAIL: $*" >&2; exit 1; }

sh -n "$BOOT"
sh -n "$CONF"

# Продуктовая точка входа должна начинаться с Entware, а не требовать готовый core.
grep -Fq 'CORE_MODE=ENTWARE_ONLY' "$BOOT" && fail 'запрещена жёстко заданная классификация core'
grep -Fq 'bootstrap_entware.sh" plan' "$BOOT" || fail 'нет read-only core plan'
grep -Fq 'bootstrap_entware.sh" apply' "$BOOT" || fail 'нет apply для чистого Entware'
grep -Fq 'READY_EXISTING_STACK' "$BOOT" || fail 'нет пути сохранения существующего stack'
grep -Fq 'NEEDS_REVIEW' "$BOOT" || fail 'нет остановки на частичном stack'

# После готового Entware пользователь не должен вручную собирать toolchain.
# Bootstrap сам ставит только недостающие userland packages и не делает upgrade.
grep -Fq 'ensure_bootstrap_dependencies' "$BOOT" || fail 'нет автоматического provisioning bootstrap tools'
grep -Fq 'opkg status ca-bundle' "$BOOT" || fail 'нет проверки HTTPS CA bundle'
for CONTRACT in \
    'need_tool sha256sum coreutils-sha256sum' \
    'need_tool sed sed' \
    'need_tool awk gawk' \
    'need_tool grep grep' \
    'need_tool mktemp coreutils-mktemp' \
    'need_tool ip ip-full' \
    'need_tool nslookup bind-nslookup' \
    'need_tool jq jq' \
    'need_tool netstat net-tools-netstat' \
    'need_tool cmp diffutils' \
    'need_tool crontab cron'
do
    grep -Fq "$CONTRACT" "$BOOT" || fail "нет dependency mapping: $CONTRACT"
done
grep -Fq 'opkg install $BOOTSTRAP_PACKAGES' "$BOOT" || fail 'нет targeted opkg install для bootstrap tools'
grep -Fq 'freenet-bootstrap-opkg-update.$$.log' "$BOOT" || fail 'opkg update log должен быть process-unique'
grep -Fq 'freenet-bootstrap-opkg-install.$$.log' "$BOOT" || fail 'opkg install log должен быть process-unique'
grep -Fq 'no FreeNet/core/network mutation started' "$BOOT" || fail 'dependency failure не отделён от product mutation'
if grep -E 'opkg[[:space:]]+upgrade' "$BOOT" >/dev/null; then
    fail 'bootstrap dependency provisioning не должен делать global upgrade'
fi

# Сценарий установки определяется bootstrap-ом автоматически и сохраняется только
# внутри transactional app-фазы; пользователь не может вручную включить rebuild core.
grep -Fq 'READY_EXISTING_STACK) INSTALL_SCENARIO=existing_stack' "$BOOT" || fail 'нет сценария действующего роутера'
grep -Fq 'ENTWARE_ONLY) INSTALL_SCENARIO=fresh_entware' "$BOOT" || fail 'нет сценария новой установки'
grep -Fq 'set_config_value INSTALL_SCENARIO "$INSTALL_SCENARIO"' "$BOOT" || fail 'сценарий установки не сохраняется в локальный config'
grep -Fq 'backup_one "$CONFIG_FILE" freenet-conf' "$BOOT" || fail 'config со сценарием не покрыт backup'
grep -Fq 'restore_one "$CONFIG_FILE" freenet-conf' "$BOOT" || fail 'config со сценарием не покрыт rollback'

# После auth/session foundation install acceptance может использовать только публичные
# health/auth endpoints. Protected /api/status без session должен оставаться закрытым.
VALIDATE_BLOCK="$(sed -n '/^validate_app() {/,/^}/p' "$BOOT")"
printf '%s\n' "$VALIDATE_BLOCK" | grep -Fq '/api/auth/status' || fail 'bootstrap acceptance не проверяет публичный auth status'
if printf '%s\n' "$VALIDATE_BLOCK" | grep -Fq '/api/status'; then
    fail 'bootstrap acceptance не должен обращаться к protected /api/status без session'
fi
printf '%s\n' "$VALIDATE_BLOCK" | grep -Fq '(.configured | type) == "boolean"' || fail 'нет проверки формы auth status'
printf '%s\n' "$VALIDATE_BLOCK" | grep -Fq '(.authenticated | type) == "boolean"' || fail 'нет проверки authenticated в auth status'

# VPN/DNS/routing остаются решениями браузерного мастера; app-фаза сама Xray не переписывает.
grep -Fq 'XRAY_CONFIG_DELTA=NONE during app phase' "$BOOT" || fail 'нет Xray no-delta acceptance'
grep -Fq 'snapshot_xray' "$BOOT" || fail 'нет snapshot Xray hash'
grep -Fq 'cmp "$BACKUP_DIR/xray-hashes.before" "$TMP_DIR/xray-hashes.after"' "$BOOT" || fail 'нет проверки неизменности Xray'
if grep -Eq '04_outbounds\.json.*(cp|mv)|02_dns\.json.*(cp|mv)|05_routing\.json.*(cp|mv)' "$BOOT"; then
    fail 'setup-first app-фаза не должна напрямую менять сетевые Xray configs'
fi

# Transactional helpers ставятся отдельно и вызываются браузером только после своих plan-gates.
grep -Fq 'MIGRATE_LIB="$ROOT/lib/freenet/migrate_split_dns.sh"' "$BOOT" || fail 'нет пути split-DNS helper'
grep -Fq 'NETWORK_LIB="$ROOT/lib/freenet/apply_network_profile.sh"' "$BOOT" || fail 'нет пути network profile helper'
grep -Fq 'PROVIDER_LIB="$ROOT/lib/freenet/apply_provider_profile.sh"' "$BOOT" || fail 'нет пути provider profile helper'
grep -Fq 'FINALIZE_LIB="$ROOT/lib/freenet/finalize_setup.sh"' "$BOOT" || fail 'нет пути completion helper'
grep -Fq 'download_asset "$NAME"' "$BOOT" || fail 'нет проверяемого пути загрузки helper'
grep -Fq 'cp "$TMP_DIR/migrate_split_dns.sh" "$MIGRATE_LIB.tmp.$$"' "$BOOT" || fail 'нет установки split-DNS helper'
grep -Fq 'cp "$TMP_DIR/apply_network_profile.sh" "$NETWORK_LIB.tmp.$$"' "$BOOT" || fail 'нет установки network helper'
grep -Fq 'cp "$TMP_DIR/apply_provider_profile.sh" "$PROVIDER_LIB.tmp.$$"' "$BOOT" || fail 'нет установки provider helper'
grep -Fq 'cp "$TMP_DIR/finalize_setup.sh" "$FINALIZE_LIB.tmp.$$"' "$BOOT" || fail 'нет установки completion helper'
grep -Fq 'backup_one "$NETWORK_LIB" network-lib' "$BOOT" || fail 'нет backup network helper'
grep -Fq 'restore_one "$NETWORK_LIB" network-lib' "$BOOT" || fail 'нет rollback network helper'
grep -Fq 'backup_one "$PROVIDER_LIB" provider-lib' "$BOOT" || fail 'нет backup provider helper'
grep -Fq 'restore_one "$PROVIDER_LIB" provider-lib' "$BOOT" || fail 'нет rollback provider helper'
grep -Fq 'backup_one "$FINALIZE_LIB" finalize-lib' "$BOOT" || fail 'нет backup completion helper'
grep -Fq 'restore_one "$FINALIZE_LIB" finalize-lib' "$BOOT" || fail 'нет rollback completion helper'

# Свежая установка остаётся безопасной, пока браузерный мастер не завершён.
grep -Fq 'SETUP_COMPLETE=no' "$CONF" || fail 'fresh config должен начинаться setup-incomplete'
grep -Fq 'AUTO_ENDPOINT_UPDATE=no' "$CONF" || fail 'legacy endpoint mirror должен начинаться выключенным'

# AUTO VPN scheduler имеет ровно одного owner: FreeNet Settings v3. Bootstrap и
# compatibility installer обязаны вызывать canonical reconcile самого freenet-ui,
# а не собирать managed cron самостоятельно.
BOOT_CRON="$(sed -n '/^apply_safe_cron() {/,/^}/p' "$BOOT")"
INSTALL_CRON="$(sed -n '/^apply_cron() {/,/^}/p' "$INSTALL")"
printf '%s\n' "$BOOT_CRON" | grep -Fq '"$FREENET_BIN" settings-v3-reconcile --config "$CONFIG_FILE"' || fail 'bootstrap не делегирует scheduler canonical Settings v3 owner'
printf '%s\n' "$INSTALL_CRON" | grep -Fq '"$FREENET_BIN" settings-v3-reconcile --config "$CONFIG_FILE"' || fail 'installer не делегирует scheduler canonical Settings v3 owner'
if printf '%s\n%s\n' "$BOOT_CRON" "$INSTALL_CRON" | grep -Eq '(blanc_xkeen_update_outbounds|/opt/bin/vpn[[:space:]]+failover|automation-best-run|/opt/lib/freenet/auto_vpn.sh)'; then
    fail 'bootstrap/install содержит parallel legacy AUTO scheduler'
fi

# Compatibility config defaults сохраняются fail-safe для старых management paths.
grep -Fq 'SETUP_COMPLETE=no' "$INSTALL" || fail 'installer fresh/default setup state должен быть incomplete'
grep -Fq 'AUTO_ENDPOINT_UPDATE=no' "$INSTALL" || fail 'installer fresh/default endpoint update должен быть выключен'
# Compatibility CLI больше не является вторым AUTO settings owner и при сохранении
# своих локальных параметров не имеет права перезаписывать современный config.
SAVE_BLOCK="$(sed -n '/^save_config() {/,/^}/p' "$INSTALL")"
MENU_BLOCK="$(sed -n '/^configure_menu() {/,/^}/p' "$INSTALL")"
printf '%s\n' "$SAVE_BLOCK" | grep -Fq '{ print }' || fail 'CLI save_config не сохраняет неизвестные/современные ключи'
printf '%s\n' "$SAVE_BLOCK" | grep -Fq '"$CONFIG_FILE.tmp.$$"' || fail 'CLI save_config должен использовать process-unique atomic temp path'
if printf '%s\n' "$SAVE_BLOCK" | grep -Fq '"$CONFIG_FILE.tmp.$"'; then
    fail 'CLI save_config содержит static/literal temp path'
fi
if printf '%s\n' "$SAVE_BLOCK" | grep -Eq 'print "?(SETUP_COMPLETE|AUTO_ENDPOINT_UPDATE|AUTO_ENDPOINT_CRON)='; then
    fail 'CLI save_config всё ещё владеет AUTO/setup ключами'
fi
printf '%s\n' "$MENU_BLOCK" | grep -Fq 'AUTO VPN / health-watch / endpoint recovery управляются только в FreeNet Control Center.' || fail 'CLI не объясняет canonical AUTO owner'
if printf '%s\n' "$MENU_BLOCK" | grep -Fq 'Автообновление endpoint/IP'; then
    fail 'CLI всё ещё предлагает parallel AUTO endpoint control'
fi

# Существующая subscription никогда не печатается и не заменяется bootstrap-ом.
if grep -Eq 'cat[[:space:]]+.*blanc_subscription|Введите URL подписки|read.*SUB' "$BOOT"; then
    fail 'bootstrap не должен раскрывать или запрашивать subscription secret'
fi

# Откат app-фазы должен быть явным и независимимым от core bootstrap rollback.
grep -Fq 'ROLLBACK: restoring app files and cron' "$BOOT" || fail 'нет app rollback'
grep -Fq 'ROLLBACK ERROR: FAILED/UNKNOWN' "$BOOT" || fail 'нет unknown rollback state'
grep -Fq 'core bootstrap rollback FAILED/UNKNOWN' "$BOOT" || fail 'нет core rollback blocker'

# Глобальный Entware upgrade и отдельный неконтролируемый upstream setup.sh запрещены.
if grep -E 'opkg[[:space:]]+upgrade' "$BOOT" >/dev/null; then fail 'глобальный opkg upgrade запрещён'; fi
if grep -Eq '(^|[^[:alnum:]_])setup\.sh([^[:alnum:]_.]|$)' "$BOOT"; then
    fail 'неконтролируемый upstream setup.sh запрещён'
fi

# Release обязан публиковать entrypoint/helpers и покрывать их SHA256SUMS.
grep -Fq 'cp bootstrap.sh dist/bootstrap.sh' "$RELEASE" || fail 'нет bootstrap.sh в release assets'
grep -Fq 'cp scripts/migrate_split_dns.sh dist/migrate_split_dns.sh' "$RELEASE" || fail 'нет migration helper в release assets'
grep -Fq 'cp scripts/apply_network_profile.sh dist/apply_network_profile.sh' "$RELEASE" || fail 'нет network helper в release assets'
grep -Fq 'cp scripts/apply_provider_profile.sh dist/apply_provider_profile.sh' "$RELEASE" || fail 'нет provider helper в release assets'
grep -Fq 'cp scripts/finalize_setup.sh dist/finalize_setup.sh' "$RELEASE" || fail 'нет completion helper в release assets'
grep -Eq '^[[:space:]]+bootstrap\.sh[[:space:]]+\\$' "$RELEASE" || fail 'bootstrap.sh не покрыт SHA256SUMS'
grep -Eq '^[[:space:]]+apply_network_profile\.sh[[:space:]]+\\$' "$RELEASE" || fail 'network helper не покрыт SHA256SUMS'
grep -Eq '^[[:space:]]+apply_provider_profile\.sh[[:space:]]+\\$' "$RELEASE" || fail 'provider helper не покрыт SHA256SUMS'
grep -Eq '^[[:space:]]+finalize_setup\.sh[[:space:]]+\\$' "$RELEASE" || fail 'completion helper не покрыт SHA256SUMS'

echo 'контракт product bootstrap PASS'
