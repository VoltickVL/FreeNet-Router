#!/bin/sh
set -eu

ROOT_DIR="$(CDPATH= cd "$(dirname "$0")/.." && pwd)"
SCRIPT="$ROOT_DIR/scripts/finalize_setup.sh"

fail() { echo "finalize DNS mode contract FAIL: $*" >&2; exit 1; }

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT INT TERM
TROOT="$TMP/opt"
CONF="$TROOT/etc/freenet/freenet.conf"
SUB="$TROOT/etc/xray/blanc_subscription.url"
FILTER="$TROOT/etc/xray/blanc_profile_filter.regex"
OUT="$TROOT/etc/xray/configs/04_outbounds.json"
INIT="$TROOT/etc/init.d/S05xkeen"
STATE="$TMP/state"
CRON_BIN="$TMP/fake-crontab"
HELPER="$TROOT/lib/freenet/apply_network_profile.sh"

mkdir -p "$TROOT/etc/freenet" "$TROOT/etc/xray/configs" "$TROOT/etc/xray/dat" "$TROOT/etc/init.d" "$TROOT/sbin" "$TROOT/lib/freenet"

cat > "$CONF" <<'EOF'
INSTALL_SCENARIO=existing_stack
SETUP_COMPLETE=no
ISP_ID=rostelecom
DNS_MODE=firmware
AUTO_ENDPOINT_UPDATE=no
AUTO_VPN_FAILOVER=no
EOF
printf '%s\n' 'https://example.invalid/key' > "$SUB"
printf '%s\n' 'Warsaw|Poland|Польша' > "$FILTER"
cat > "$OUT" <<'EOF'
{"outbounds":[{"tag":"vless-reality","protocol":"freedom"},{"tag":"direct","protocol":"freedom"}]}
EOF
cat > "$INIT" <<'EOF'
#!/bin/sh
start_auto="on"
proxy_dns="on"
EOF
cat > "$STATE" <<'EOF'
XRAY_RUNNING=yes
EOF
cat > "$TROOT/sbin/xkeen" <<'EOF'
#!/bin/sh
exit 0
EOF
chmod 755 "$TROOT/sbin/xkeen"
cat > "$TROOT/sbin/xray" <<'EOF'
#!/bin/sh
exit 0
EOF
chmod 755 "$TROOT/sbin/xray"
cat > "$CRON_BIN" <<'EOF'
#!/bin/sh
[ "${1:-}" = -l ] && exit 0
exit 0
EOF
chmod 755 "$CRON_BIN"

write_plan() {
    MODE="$1"
    PROXY="$2"
    cat > "$HELPER" <<EOF
#!/bin/sh
[ "\${1:-}" = plan ] || exit 2
cat <<'PLAN'
SUPPORTED=yes
REASON=runtime accepted
EFFECTIVE_DNS_MODE=$MODE
PROXY_DNS=$PROXY
DNS_OUT=no
VLESS_PROFILE=yes
MUTATION=NONE
PLAN
EOF
    chmod 755 "$HELPER"
}

run_plan() {
    FREENET_ROOT="$TROOT" \
    FREENET_CONFIG_FILE="$CONF" \
    FREENET_SUB_FILE="$SUB" \
    FREENET_FILTER_FILE="$FILTER" \
    FREENET_XRAY_CONFIG_DIR="$TROOT/etc/xray/configs" \
    FREENET_XRAY_ASSET_DIR="$TROOT/etc/xray/dat" \
    FREENET_XKEEN_BIN="$TROOT/sbin/xkeen" \
    FREENET_XRAY_BIN="$TROOT/sbin/xray" \
    FREENET_NETWORK_HELPER="$HELPER" \
    FREENET_CRONTAB_BIN="$CRON_BIN" \
    FREENET_FINALIZE_TEST_MODE=yes \
    FREENET_FINALIZE_TEST_STATE="$STATE" \
    sh "$SCRIPT" plan
}

# Regression from real v0.2.37 existing-stack runtime:
# saved/effective DNS is direct firmware, while legacy runtime proxy_dns still reports on.
# dns-out must not become a false readiness gate.
write_plan firmware on
run_plan > "$TMP/direct.out"
grep -Fq 'READY=yes' "$TMP/direct.out" || { cat "$TMP/direct.out" >&2; fail 'direct DNS was blocked by legacy PROXY_DNS=on'; }
grep -Fq 'NETWORK_EFFECTIVE_DNS_MODE=firmware' "$TMP/direct.out" || fail 'effective direct DNS mode not exposed'
grep -Fq 'NETWORK_PROXY_DNS=on' "$TMP/direct.out" || fail 'legacy proxy flag not exposed'
grep -Fq 'DNS_OUT=no' "$TMP/direct.out" || fail 'direct DNS no-dns-out state not exposed'
grep -Fq 'keep direct DNS topology unchanged' "$TMP/direct.out" || fail 'direct DNS expected delta missing'

# Split DNS remains strict: effective xkeen mode still requires dns-out.
write_plan xkeen off
if run_plan > "$TMP/xkeen.out" 2>&1; then
    cat "$TMP/xkeen.out" >&2
    fail 'xkeen DNS without dns-out unexpectedly passed'
fi
grep -Fq 'READY=no' "$TMP/xkeen.out" || fail 'split DNS failure did not report READY=no'
grep -Fq 'REASON=dns-out is required for the selected XKeen/Xray DNS mode' "$TMP/xkeen.out" || fail 'split DNS missing-dns-out reason changed'

echo 'finalize DNS mode contract PASS'
