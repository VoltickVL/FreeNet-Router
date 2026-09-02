#!/bin/sh

set -eu

ROOT_DIR="$(CDPATH= cd "$(dirname "$0")/.." && pwd)"
BOOT="$ROOT_DIR/bootstrap.sh"
CONF="$ROOT_DIR/config/freenet.conf.example"
RELEASE="$ROOT_DIR/.github/workflows/release.yml"

fail() { echo "product bootstrap contract FAIL: $*" >&2; exit 1; }

sh -n "$BOOT"
sh -n "$CONF"

# Product entrypoint must start from Entware, not demand a pre-existing core.
grep -Fq 'CORE_MODE=ENTWARE_ONLY' "$BOOT" && fail 'hardcoded core classification is forbidden'
grep -Fq 'bootstrap_entware.sh" plan' "$BOOT" || fail 'core read-only plan missing'
grep -Fq 'bootstrap_entware.sh" apply' "$BOOT" || fail 'clean Entware core apply missing'
grep -Fq 'READY_EXISTING_STACK' "$BOOT" || fail 'existing stack preservation path missing'
grep -Fq 'NEEDS_REVIEW' "$BOOT" || fail 'partial stack stop missing'

# Provider/ISP/DNS must be browser decisions; app phase itself must not rewrite Xray.
grep -Fq 'XRAY_CONFIG_DELTA=NONE during app phase' "$BOOT" || fail 'Xray no-delta acceptance missing'
grep -Fq 'snapshot_xray' "$BOOT" || fail 'Xray hash snapshot missing'
grep -Fq 'cmp "$BACKUP_DIR/xray-hashes.before" "$TMP_DIR/xray-hashes.after"' "$BOOT" || fail 'Xray unchanged verification missing'
if grep -Eq '04_outbounds\.json.*(cp|mv)|02_dns\.json.*(cp|mv)|05_routing\.json.*(cp|mv)' "$BOOT"; then
    fail 'setup-first app phase must not directly mutate Xray network configs'
fi

# Transactional network/provider helpers are installed as separate later browser actions.
grep -Fq 'MIGRATE_LIB="$ROOT/lib/freenet/migrate_split_dns.sh"' "$BOOT" || fail 'split-DNS helper path missing'
grep -Fq 'NETWORK_LIB="$ROOT/lib/freenet/apply_network_profile.sh"' "$BOOT" || fail 'network profile helper path missing'
grep -Fq 'PROVIDER_LIB="$ROOT/lib/freenet/apply_provider_profile.sh"' "$BOOT" || fail 'provider profile helper path missing'
grep -Fq 'download_asset "$NAME"' "$BOOT" || fail 'verified helper download path missing'
grep -Fq 'cp "$TMP_DIR/migrate_split_dns.sh" "$MIGRATE_LIB.tmp.$$"' "$BOOT" || fail 'split-DNS helper install missing'
grep -Fq 'cp "$TMP_DIR/apply_network_profile.sh" "$NETWORK_LIB.tmp.$$"' "$BOOT" || fail 'network profile helper install missing'
grep -Fq 'cp "$TMP_DIR/apply_provider_profile.sh" "$PROVIDER_LIB.tmp.$$"' "$BOOT" || fail 'provider profile helper install missing'
grep -Fq 'backup_one "$NETWORK_LIB" network-lib' "$BOOT" || fail 'network helper backup missing'
grep -Fq 'restore_one "$NETWORK_LIB" network-lib' "$BOOT" || fail 'network helper rollback missing'
grep -Fq 'backup_one "$PROVIDER_LIB" provider-lib' "$BOOT" || fail 'provider helper backup missing'
grep -Fq 'restore_one "$PROVIDER_LIB" provider-lib' "$BOOT" || fail 'provider helper rollback missing'

# Fresh installs remain safe until browser setup is completed.
grep -Fq 'SETUP_COMPLETE=no' "$CONF" || fail 'fresh config must be setup-incomplete'
grep -Fq 'AUTO_ENDPOINT_UPDATE=no' "$CONF" || fail 'fresh endpoint cron must start disabled'
grep -Fq 'endpoint refresh disabled until setup/subscription/dns-out acceptance' "$BOOT" || fail 'endpoint cron gate missing'
grep -Fq '[ "$SETUP_COMPLETE" = yes ]' "$BOOT" || fail 'setup-complete cron gate missing'
grep -Fq '[ -s "$SUB_FILE" ]' "$BOOT" || fail 'subscription cron gate missing'
grep -Fq 'has_dns_out' "$BOOT" || fail 'dns-out cron gate missing'

# Existing subscription is never printed/replaced by bootstrap.
if grep -Eq 'cat[[:space:]]+.*blanc_subscription|Введите URL подписки|read.*SUB' "$BOOT"; then
    fail 'bootstrap must not expose or prompt for subscription secret'
fi

# App rollback must be explicit and separate from core bootstrap rollback.
grep -Fq 'ROLLBACK: restoring app files and cron' "$BOOT" || fail 'app rollback missing'
grep -Fq 'ROLLBACK ERROR: FAILED/UNKNOWN' "$BOOT" || fail 'rollback unknown state missing'
grep -Fq 'core bootstrap rollback FAILED/UNKNOWN' "$BOOT" || fail 'core rollback blocker missing'

# No global Entware upgrade and no raw setup.sh dependency.
if grep -E 'opkg[[:space:]]+upgrade' "$BOOT" >/dev/null; then fail 'global opkg upgrade forbidden'; fi
if grep -Fq 'setup.sh' "$BOOT"; then fail 'uncontrolled upstream setup.sh forbidden'; fi

# Release must carry the entrypoint/helpers and cover them with SHA256SUMS.
grep -Fq 'cp bootstrap.sh dist/bootstrap.sh' "$RELEASE" || fail 'bootstrap.sh release asset missing'
grep -Fq 'cp scripts/migrate_split_dns.sh dist/migrate_split_dns.sh' "$RELEASE" || fail 'migration helper release asset missing'
grep -Fq 'cp scripts/apply_network_profile.sh dist/apply_network_profile.sh' "$RELEASE" || fail 'network helper release asset missing'
grep -Fq 'cp scripts/apply_provider_profile.sh dist/apply_provider_profile.sh' "$RELEASE" || fail 'provider helper release asset missing'
grep -Eq '^[[:space:]]+bootstrap\.sh[[:space:]]+\\$' "$RELEASE" || fail 'bootstrap.sh SHA256SUMS coverage missing'
grep -Eq '^[[:space:]]+apply_network_profile\.sh[[:space:]]+\\$' "$RELEASE" || fail 'network helper SHA256SUMS coverage missing'
grep -Eq '^[[:space:]]+apply_provider_profile\.sh[[:space:]]+\\$' "$RELEASE" || fail 'provider helper SHA256SUMS coverage missing'

echo 'product bootstrap contract PASS'