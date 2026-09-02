#!/bin/sh

set -eu

ROOT_DIR="$(CDPATH= cd "$(dirname "$0")/.." && pwd)"
BOOT="$ROOT_DIR/bootstrap.sh"
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

# Provider/ISP/DNS остаются решениями браузерного мастера; app-фаза сама Xray не переписывает.
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
grep -Fq 'AUTO_ENDPOINT_UPDATE=no' "$CONF" || fail 'endpoint cron должен начинаться выключенным'
grep -Fq 'endpoint refresh disabled until setup/subscription/dns-out acceptance' "$BOOT" || fail 'нет gate для endpoint cron'
grep -Fq '[ "$SETUP_COMPLETE" = yes ]' "$BOOT" || fail 'нет setup-complete gate для cron'
grep -Fq '[ -s "$SUB_FILE" ]' "$BOOT" || fail 'нет subscription gate для cron'
grep -Fq 'has_dns_out' "$BOOT" || fail 'нет dns-out gate для cron'

# Существующая subscription никогда не печатается и не заменяется bootstrap-ом.
if grep -Eq 'cat[[:space:]]+.*blanc_subscription|Введите URL подписки|read.*SUB' "$BOOT"; then
    fail 'bootstrap не должен раскрывать или запрашивать subscription secret'
fi

# Откат app-фазы должен быть явным и независимимым от core bootstrap rollback.
grep -Fq 'ROLLBACK: restoring app files and cron' "$BOOT" || fail 'нет app rollback'
grep -Fq 'ROLLBACK ERROR: FAILED/UNKNOWN' "$BOOT" || fail 'нет unknown rollback state'
grep -Fq 'core bootstrap rollback FAILED/UNKNOWN' "$BOOT" || fail 'нет core rollback blocker'

# Глобальный Entware upgrade и неконтролируемый upstream setup.sh запрещены.
if grep -E 'opkg[[:space:]]+upgrade' "$BOOT" >/dev/null; then fail 'глобальный opkg upgrade запрещён'; fi
if grep -Fq 'setup.sh' "$BOOT"; then fail 'неконтролируемый upstream setup.sh запрещён'; fi

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
