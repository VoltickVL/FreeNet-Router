#!/bin/sh
set -eu

ROOT_DIR="$(CDPATH= cd "$(dirname "$0")/.." && pwd)"
SCRIPT="$ROOT_DIR/scripts/finalize_setup.sh"

fail() { echo "setup finalize contract FAIL: $*" >&2; exit 1; }

sh -n "$SCRIPT"
for NEEDLE in \
    'SETUP_COMPLETE=yes' \
    'xkeen -auto on' \
    '# BEGIN FREENET' \
    'AUTO_ENDPOINT_UPDATE' \
    'AUTO_VPN_FAILOVER' \
    '/opt/bin/vpn failover' \
    'NETWORK_PROXY_DNS' \
    'ACTIVE_VPN_FILTER_SET' \
    'ROLLBACK ERROR/STATE: rollback success' \
    'EXPECTED_NO_DELTA=current XKeen/Xray/XKeen UI core is not reinstalled'
do
    grep -Fq "$NEEDLE" "$SCRIPT" || fail "missing contract: $NEEDLE"
done

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT INT TERM
TROOT="$TMP/opt"
CONF="$TROOT/etc/freenet/freenet.conf"
SUB="$TROOT/etc/xray/sub.url"
PROFILE="$TROOT/etc/freenet/vpn_profile_name"
FILTER="$TROOT/etc/xray/blanc_profile_filter.regex"
OUT="$TROOT/etc/xray/configs/04_outbounds.json"
INIT="$TROOT/etc/init.d/S05xkeen"
CRON_BIN="$TMP/fake-crontab"
CRON_STORE="$TMP/crontab.store"
CRON_FAIL_MARKER="$TMP/crontab.fail.once"
STATE="$TMP/state"
mkdir -p "$TROOT/etc/freenet" "$TROOT/etc/xray/configs" "$TROOT/etc/xray/dat" "$TROOT/etc/init.d" "$TROOT/sbin" "$TROOT/lib/freenet" "$TROOT/bin"

# Split-DNS baseline with legacy config intentionally missing AUTO_VPN_FAILOVER keys.
cat > "$CONF" <<'EOF'
UI_PORT=1001
INSTALL_SCENARIO=existing_stack
SETUP_COMPLETE=no
ISP_ID=rostelecom
DNS_MODE=xkeen
AUTO_ENDPOINT_UPDATE=no
AUTO_ENDPOINT_CRON='*/15 * * * *'
AUTO_XKEEN_GEODATA=yes
AUTO_XKEEN_GEODATA_CRON='30 6 * * *'
EOF
printf '%s\n' 'https://example.invalid/key' > "$SUB"
printf '%s\n' 'Poland Warsaw Extra' > "$PROFILE"
printf '%s\n' 'Warsaw|Warszawa|Poland|Polska|Польша|Варшава' > "$FILTER"
cat > "$OUT" <<'EOF'
{"outbounds":[{"tag":"vless-reality","protocol":"freedom"},{"tag":"dns-out","protocol":"dns"},{"tag":"direct","protocol":"freedom"}]}
EOF
cat > "$TROOT/etc/xray/configs/02_dns.json" <<'EOF'
{"dns":{"tag":"dns-vless","servers":[]}}
EOF
cat > "$TROOT/etc/xray/configs/03_inbounds.json" <<'EOF'
{"inbounds":[]}
EOF
cat > "$TROOT/etc/xray/configs/05_routing.json" <<'EOF'
{"routing":{"rules":[]}}
EOF
cat > "$INIT" <<'EOF'
#!/bin/sh
start_auto="off"
proxy_dns="on"
EOF
cat > "$STATE" <<'EOF'
XRAY_RUNNING=yes
EOF
cat > "$CRON_STORE" <<'EOF'
5 4 * * * /opt/bin/unrelated-task
EOF

cat > "$TROOT/sbin/xkeen" <<'EOF'
#!/bin/sh
case "${1:-}" in
    -auto)
        case "${2:-}" in on|off) sed -i "s/^start_auto=.*/start_auto=\"${2}\"/" "$FREENET_ROOT/etc/init.d/S05xkeen" ;; *) exit 1 ;; esac
        ;;
    *) exit 1 ;;
esac
EOF
chmod 755 "$TROOT/sbin/xkeen"
cat > "$TROOT/sbin/xray" <<'EOF'
#!/bin/sh
exit 0
EOF
chmod 755 "$TROOT/sbin/xray"
cat > "$TROOT/bin/vpn" <<'EOF'
#!/bin/sh
[ "${1:-}" = failover ] || exit 2
exit 0
EOF
chmod 755 "$TROOT/bin/vpn"
cat > "$TROOT/lib/freenet/apply_network_profile.sh" <<'EOF'
#!/bin/sh
[ "${1:-}" = plan ] || exit 2
cat <<'PLAN'
SUPPORTED=yes
REASON=verified preset
PROXY_DNS=on
DNS_OUT=yes
VLESS_PROFILE=yes
MUTATION=NONE
PLAN
EOF
chmod 755 "$TROOT/lib/freenet/apply_network_profile.sh"
cat > "$CRON_BIN" <<'EOF'
#!/bin/sh
if [ "${1:-}" = -l ]; then
    cat "$FREENET_TEST_CRON_STORE" 2>/dev/null || true
    exit 0
fi
if [ "${FREENET_TEST_CRON_FAIL:-no}" = yes ] && [ ! -e "$FREENET_TEST_CRON_FAIL_MARKER" ]; then
    : > "$FREENET_TEST_CRON_FAIL_MARKER"
    exit 1
fi
cp "$1" "$FREENET_TEST_CRON_STORE"
EOF
chmod 755 "$CRON_BIN"

run_finalize() {
    FREENET_ROOT="$TROOT" \
    FREENET_CONFIG_FILE="$CONF" \
    FREENET_SUB_FILE="$SUB" \
    FREENET_PROFILE_FILE="$PROFILE" \
    FREENET_FILTER_FILE="$FILTER" \
    FREENET_XRAY_CONFIG_DIR="$TROOT/etc/xray/configs" \
    FREENET_XRAY_ASSET_DIR="$TROOT/etc/xray/dat" \
    FREENET_XKEEN_BIN="$TROOT/sbin/xkeen" \
    FREENET_XRAY_BIN="$TROOT/sbin/xray" \
    FREENET_VPN_BIN="$TROOT/bin/vpn" \
    FREENET_NETWORK_HELPER="$TROOT/lib/freenet/apply_network_profile.sh" \
    FREENET_CRONTAB_BIN="$CRON_BIN" \
    FREENET_FINALIZE_TEST_MODE=yes \
    FREENET_FINALIZE_TEST_STATE="$STATE" \
    FREENET_TEST_CRON_STORE="$CRON_STORE" \
    FREENET_TEST_CRON_FAIL="${FREENET_TEST_CRON_FAIL:-no}" \
    FREENET_TEST_CRON_FAIL_MARKER="$CRON_FAIL_MARKER" \
    sh "$SCRIPT" "$@"
}

# Legacy config: missing failover key must be treated as disabled.
run_finalize plan > "$TMP/plan.out"
grep -Fq 'READY=yes' "$TMP/plan.out" || fail 'accepted setup should be ready'
grep -Fq 'INSTALL_SCENARIO=existing_stack' "$TMP/plan.out" || fail 'plan must expose existing-stack scenario'
grep -Fq 'XKEEN_AUTOSTART=off' "$TMP/plan.out" || fail 'plan must expose autostart off'
grep -Fq 'NETWORK_PROXY_DNS=on' "$TMP/plan.out" || fail 'plan must expose split-DNS topology'
grep -Fq 'ACTIVE_VPN_FILTER_SET=yes' "$TMP/plan.out" || fail 'managed VPN filter must be exposed'
grep -Fq 'AUTO_VPN_FAILOVER=no' "$TMP/plan.out" || fail 'legacy config must default failover to disabled'
grep -Fq 'keep automatic VPN failover disabled' "$TMP/plan.out" || fail 'plan must disclose disabled failover'
grep -Fq 'enable XKeen autostart through xkeen -auto on' "$TMP/plan.out" || fail 'plan must disclose autostart delta'
grep -Fq 'MUTATION=NONE' "$TMP/plan.out" || fail 'plan must be read-only'

FREENET_TEST_CRON_FAIL=no
export FREENET_TEST_CRON_FAIL
if ! run_finalize apply > "$TMP/apply.out" 2>&1; then
    cat "$TMP/apply.out" >&2
    fail 'finalize apply should succeed'
fi
grep -Fq '[FreeNet Setup Finalize] RESULT=SUCCESS' "$TMP/apply.out" || fail 'success marker missing'
grep -qx 'INSTALL_SCENARIO=existing_stack' "$CONF" || fail 'install scenario changed unexpectedly'
grep -qx 'SETUP_COMPLETE=yes' "$CONF" || fail 'setup complete not committed'
grep -Fq 'start_auto="on"' "$INIT" || fail 'XKeen autostart not enabled'
grep -qx '# BEGIN FREENET' "$CRON_STORE" || fail 'managed cron block missing'
grep -Fq '/opt/sbin/xkeen -ug' "$CRON_STORE" || fail 'geodata schedule missing'
grep -Fq '/opt/bin/unrelated-task' "$CRON_STORE" || fail 'foreign cron entry was not preserved'
if grep -q '^[^#].*/opt/bin/blanc_xkeen_update_outbounds.sh' "$CRON_STORE"; then
    fail 'endpoint refresh must remain disabled while AUTO_ENDPOINT_UPDATE=no'
fi
if grep -q '^[^#].*/opt/bin/vpn[[:space:]]\+failover' "$CRON_STORE"; then
    fail 'legacy config must not silently enable failover'
fi

# Explicit failover enablement is independent from endpoint refresh.
sed -i 's/^SETUP_COMPLETE=.*/SETUP_COMPLETE=no/' "$CONF"
sed -i 's/^start_auto=.*/start_auto="off"/' "$INIT"
printf '%s\n' 'AUTO_VPN_FAILOVER=yes' >> "$CONF"
printf '%s\n' "AUTO_VPN_FAILOVER_CRON='7,22,37,52 * * * *'" >> "$CONF"
run_finalize plan > "$TMP/failover-plan.out"
grep -Fq 'AUTO_VPN_FAILOVER=yes' "$TMP/failover-plan.out" || fail 'explicit failover enablement not exposed'
grep -Fq 'activate configured VPN failover schedule' "$TMP/failover-plan.out" || fail 'failover delta missing'
if ! run_finalize apply > "$TMP/failover-apply.out" 2>&1; then
    cat "$TMP/failover-apply.out" >&2
    fail 'failover-enabled finalize should succeed'
fi
grep -Fq '7,22,37,52 * * * * /opt/bin/vpn failover' "$CRON_STORE" || fail 'managed failover cron missing'
if grep -q '^[^#].*/opt/bin/blanc_xkeen_update_outbounds.sh' "$CRON_STORE"; then
    fail 'enabling failover must not enable endpoint refresh'
fi
grep -Fq '/opt/bin/unrelated-task' "$CRON_STORE" || fail 'foreign cron entry lost after failover enablement'

# Disabling failover removes an existing active failover job while preserving foreign jobs.
sed -i 's/^SETUP_COMPLETE=.*/SETUP_COMPLETE=no/' "$CONF"
sed -i 's/^start_auto=.*/start_auto="off"/' "$INIT"
sed -i 's/^AUTO_VPN_FAILOVER=.*/AUTO_VPN_FAILOVER=no/' "$CONF"
if ! run_finalize apply > "$TMP/failover-disable.out" 2>&1; then
    cat "$TMP/failover-disable.out" >&2
    fail 'failover-disabled finalize should succeed'
fi
if grep -q '^[^#].*/opt/bin/vpn[[:space:]]\+failover' "$CRON_STORE"; then
    fail 'disabled failover cron remains active'
fi
grep -Fq '/opt/bin/unrelated-task' "$CRON_STORE" || fail 'foreign cron entry lost after failover disablement'

# Direct-DNS quick-country state is managed by the active filter even when preferred-profile metadata is absent.
sed -i 's/^SETUP_COMPLETE=.*/SETUP_COMPLETE=no/' "$CONF"
sed -i 's/^DNS_MODE=.*/DNS_MODE=firmware/' "$CONF"
sed -i 's/^start_auto=.*/start_auto="off"/' "$INIT"
sed -i 's/^proxy_dns=.*/proxy_dns="off"/' "$INIT"
rm -f "$PROFILE"
cat > "$OUT" <<'EOF'
{"outbounds":[{"tag":"vless-reality","protocol":"freedom"},{"tag":"direct","protocol":"freedom"}]}
EOF
cat > "$TROOT/lib/freenet/apply_network_profile.sh" <<'EOF'
#!/bin/sh
[ "${1:-}" = plan ] || exit 2
cat <<'PLAN'
SUPPORTED=yes
REASON=direct DNS accepted
PROXY_DNS=off
DNS_OUT=no
VLESS_PROFILE=yes
MUTATION=NONE
PLAN
EOF
chmod 755 "$TROOT/lib/freenet/apply_network_profile.sh"
run_finalize plan > "$TMP/direct-plan.out"
grep -Fq 'READY=yes' "$TMP/direct-plan.out" || { cat "$TMP/direct-plan.out" >&2; fail 'direct DNS managed quick-country state must be ready without preferred profile metadata'; }
grep -Fq 'PREFERRED_PROFILE_SET=no' "$TMP/direct-plan.out" || fail 'missing optional preferred profile metadata not exposed'
grep -Fq 'ACTIVE_VPN_FILTER_SET=yes' "$TMP/direct-plan.out" || fail 'managed quick-country filter not accepted'
grep -Fq 'NETWORK_PROXY_DNS=off' "$TMP/direct-plan.out" || fail 'direct DNS topology not exposed'
grep -Fq 'DNS_OUT=no' "$TMP/direct-plan.out" || fail 'direct DNS must allow absent dns-out'
grep -Fq 'keep direct DNS topology unchanged' "$TMP/direct-plan.out" || fail 'direct DNS expected delta missing'
if ! run_finalize apply > "$TMP/direct-apply.out" 2>&1; then
    cat "$TMP/direct-apply.out" >&2
    fail 'direct DNS finalize should succeed without preferred profile metadata'
fi
grep -Fq '[FreeNet Setup Finalize] RESULT=SUCCESS' "$TMP/direct-apply.out" || fail 'direct DNS finalize success marker missing'
grep -qx 'SETUP_COMPLETE=yes' "$CONF" || fail 'direct DNS setup complete not committed'
[ ! -e "$PROFILE" ] || fail 'finalize must not invent preferred profile metadata'
if grep -q 'dns-out' "$OUT"; then
    fail 'finalize must not add dns-out in direct DNS mode'
fi
grep -Fq 'proxy_dns="off"' "$INIT" || fail 'finalize must not enable proxy_dns in direct DNS mode'

# Missing managed VPN filter remains a real blocker even when stale preferred-profile metadata exists.
sed -i 's/^SETUP_COMPLETE=.*/SETUP_COMPLETE=no/' "$CONF"
printf '%s\n' 'stale optional label' > "$PROFILE"
rm -f "$FILTER"
if run_finalize plan > "$TMP/no-filter-plan.out" 2>&1; then
    cat "$TMP/no-filter-plan.out" >&2
    fail 'setup without managed VPN filter must stay blocked'
fi
grep -Fq 'ACTIVE_VPN_FILTER_SET=no' "$TMP/no-filter-plan.out" || fail 'missing filter state not exposed'
grep -Fq 'REASON=managed VPN profile filter is not established' "$TMP/no-filter-plan.out" || fail 'missing filter reason not reported'
printf '%s\n' 'Warsaw|Warszawa|Poland|Polska|Польша|Варшава' > "$FILTER"

# Simulate one cron-write failure after autostart mutation. Config, cron and autostart must roll back.
sed -i 's/^SETUP_COMPLETE=.*/SETUP_COMPLETE=no/' "$CONF"
sed -i 's/^start_auto=.*/start_auto="off"/' "$INIT"
sed -i 's/^AUTO_VPN_FAILOVER=.*/AUTO_VPN_FAILOVER=yes/' "$CONF"
cat > "$CRON_STORE" <<'EOF'
17 3 * * * /opt/bin/original-task
EOF
rm -f "$CRON_FAIL_MARKER"
cp "$CONF" "$TMP/conf.before"
cp "$CRON_STORE" "$TMP/cron.before"

FREENET_TEST_CRON_FAIL=yes
export FREENET_TEST_CRON_FAIL
if run_finalize apply > "$TMP/fail.out" 2>&1; then
    fail 'simulated cron failure unexpectedly succeeded'
fi
grep -Fq 'ROLLBACK ERROR/STATE: rollback success' "$TMP/fail.out" || {
    cat "$TMP/fail.out" >&2
    fail 'rollback success not reported'
}
cmp "$TMP/conf.before" "$CONF" || fail 'config was not restored'
cmp "$TMP/cron.before" "$CRON_STORE" || fail 'cron was not restored'
grep -Fq 'start_auto="off"' "$INIT" || fail 'autostart was not restored'

echo 'setup finalize contract PASS'
