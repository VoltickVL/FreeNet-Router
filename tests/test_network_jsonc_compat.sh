#!/bin/sh
set -eu

ROOT_DIR="$(CDPATH= cd "$(dirname "$0")/.." && pwd)"
SCRIPT="$ROOT_DIR/scripts/apply_network_profile.sh"
fail() { echo "network JSONC compat FAIL: $*" >&2; exit 1; }

TMP="$(mktemp -d)"; trap 'rm -rf "$TMP"' EXIT INT TERM
TROOT="$TMP/opt"; STATE="$TMP/runtime.state"
mkdir -p "$TROOT/etc/freenet" "$TROOT/etc/xray/configs" "$TROOT/etc/xray/dat" "$TROOT/etc/init.d" "$TROOT/sbin" "$TROOT/backups"

cat > "$TROOT/etc/freenet/freenet.conf" <<'EOF'
ISP_ID=vladlink
DNS_MODE=firmware
EOF
cat > "$TROOT/etc/init.d/S05xkeen" <<'EOF'
#!/bin/sh
proxy_dns="off"
EOF
chmod 755 "$TROOT/etc/init.d/S05xkeen"

cat > "$TROOT/etc/xray/configs/02_dns.json" <<'EOF'
// Native Keenetic DNS may be comment-only.
EOF
cat > "$TROOT/etc/xray/configs/03_inbounds.json" <<'EOF'
{"inbounds":[{"tag":"redirect","port":5000,"protocol":"dokodemo-door"},{"tag":"tproxy","port":5000,"protocol":"dokodemo-door"}]}
EOF
cat > "$TROOT/etc/xray/configs/04_outbounds.json" <<'EOF'
{"outbounds":[{"tag":"vless-reality","protocol":"freedom","settings":{"marker":"preserve-vpn"}},{"tag":"direct","protocol":"freedom"},{"tag":"block","protocol":"blackhole"}]}
EOF
cat > "$TROOT/etc/xray/configs/05_routing.json" <<'EOF'
{
  // Existing XKeen/Xray configs may contain comments.
  "routing": {
    "domainStrategy": "AsIs",
    "rules": [
      {"type":"field","domain":["ext:geosite.dat:youtube"],"outboundTag":"vless-reality"},
      {"type":"field","domain":["ext:geosite.dat:google","domain:example.ru"],"outboundTag":"direct"},
      {"type":"field","network":"tcp,udp","outboundTag":"vless-reality"},
    ],
  },
}
EOF

cat > "$STATE" <<'EOF'
PORT53_OWNER=ndnproxy
NDM_DNS_OVERRIDE=off
NDM_FILTER_ENGINE=public
NDM_DNS_INTERCEPT=on
NDM_CONFIG_MARKER=preserved
NDM_DNS_PROFILE_MARKER=preserved
NDM_MUTATE_PROTECTED_ON_OVERRIDE=no
XRAY_RUNNING=yes
XRAY_GID=11111
DNS_QUERY_OK=yes
XKEEN_ACTION_RESULT=success
NDM_ACTION_RESULT=success
NDM_FILTER_ENGINE_ACTION_RESULT=success
NDM_INTERCEPT_ACTION_RESULT=success
NDM_SAVE_RESULT=success
EOF
printf '#!/bin/sh\nexit 0\n' > "$TROOT/sbin/xkeen"; chmod 755 "$TROOT/sbin/xkeen"
printf '#!/bin/sh\nexit 0\n' > "$TROOT/sbin/xray"; chmod 755 "$TROOT/sbin/xray"

run_network() {
    FREENET_ROOT="$TROOT" FREENET_BACKUP_ROOT="$TROOT/backups" \
    FREENET_CONFIG_FILE="$TROOT/etc/freenet/freenet.conf" FREENET_CONFIG_DIR="$TROOT/etc/xray/configs" \
    FREENET_XRAY_ASSET_DIR="$TROOT/etc/xray/dat" FREENET_XKEEN_BIN="$TROOT/sbin/xkeen" \
    FREENET_XRAY_BIN="$TROOT/sbin/xray" FREENET_XKEEN_RUNTIME_TIMEOUT=2 \
    FREENET_NETWORK_TEST_MODE=yes FREENET_NETWORK_TEST_STATE="$STATE" sh "$SCRIPT" "$@"
}

run_network plan > "$TMP/native.plan"
grep -Fq 'DNS_ROUTING_MODE=native' "$TMP/native.plan" || fail 'JSONC routing was not recognized as native'
grep -Fq 'VLESS_PROFILE=yes' "$TMP/native.plan" || fail 'JSONC-compatible plan lost VLESS facts'

run_network apply > "$TMP/native.apply" 2>&1 || { cat "$TMP/native.apply" >&2; fail 'native apply rejected valid JSONC'; }
grep -Fq 'RESULT=SUCCESS' "$TMP/native.apply" || fail 'native JSONC apply result missing'
grep -q '^NDM_DNS_OVERRIDE=off$' "$STATE" || fail 'native JSONC apply mutated override'
grep -q '^PORT53_OWNER=ndnproxy$' "$STATE" || fail 'native JSONC apply changed :53 owner'

sed -i 's/^DNS_MODE=.*/DNS_MODE=xkeen/' "$TROOT/etc/freenet/freenet.conf"
run_network apply > "$TMP/split.apply" 2>&1 || { cat "$TMP/split.apply" >&2; fail 'native JSONC -> Split failed'; }
grep -Fq 'RESULT=SUCCESS' "$TMP/split.apply" || fail 'Split JSONC result missing'
grep -q '^NDM_DNS_OVERRIDE=on$' "$STATE" || fail 'Split did not enable override'
grep -q '^NDM_FILTER_ENGINE=opkg$' "$STATE" || fail 'Split did not enable OPKG engine'
grep -q '^NDM_DNS_INTERCEPT=off$' "$STATE" || fail 'Split did not suppress native intercept'
grep -q '^PORT53_OWNER=xray$' "$STATE" || fail 'Split did not move :53 to Xray'
jq -e '.routing.rules | length > 3' "$TROOT/etc/xray/configs/05_routing.json" >/dev/null || fail 'Split candidate was not strict JSON after JSONC input'

sed -i 's/^DNS_MODE=.*/DNS_MODE=firmware/' "$TROOT/etc/freenet/freenet.conf"
run_network apply > "$TMP/restore.apply" 2>&1 || { cat "$TMP/restore.apply" >&2; fail 'Split -> native after JSONC input failed'; }
grep -Fq 'RESULT=SUCCESS' "$TMP/restore.apply" || fail 'native restore result missing'
grep -q '^NDM_DNS_OVERRIDE=off$' "$STATE" || fail 'native restore did not disable override'
grep -q '^NDM_FILTER_ENGINE=public$' "$STATE" || fail 'native restore did not restore engine'
grep -q '^NDM_DNS_INTERCEPT=on$' "$STATE" || fail 'native restore did not restore intercept'
grep -q '^PORT53_OWNER=ndnproxy$' "$STATE" || fail 'native restore did not restore :53 owner'

# Xray semantic validation is authoritative, but FreeNet must still STOP if the
# config cannot be converted into a safe jq read stream for transactional editing.
cat > "$TROOT/etc/xray/configs/05_routing.json" <<'EOF'
{"routing":{"rules":[{"type":"field","network":"tcp,udp","outboundTag":"vless-reality"}]
EOF
if run_network apply > "$TMP/broken.apply" 2>&1; then fail 'broken JSONC unexpectedly accepted'; fi
grep -Fq 'Xray JSON/JSONC нельзя безопасно разобрать' "$TMP/broken.apply" || fail 'broken JSONC reason missing'
grep -Fq 'ROLLBACK ERROR/STATE: no live apply' "$TMP/broken.apply" || fail 'broken JSONC must be NOT_APPLIED'
grep -q '^NDM_DNS_OVERRIDE=off$' "$STATE" || fail 'broken JSONC preflight mutated NDM'
grep -q '^PORT53_OWNER=ndnproxy$' "$STATE" || fail 'broken JSONC preflight changed :53 owner'

echo 'network JSONC compat PASS'
