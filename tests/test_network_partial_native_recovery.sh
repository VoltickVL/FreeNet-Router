#!/bin/sh
set -eu

ROOT_DIR="$(CDPATH= cd "$(dirname "$0")/.." && pwd)"
SCRIPT="$ROOT_DIR/scripts/apply_network_profile.sh"
fail() { echo "partial native recovery FAIL: $*" >&2; exit 1; }

TMP="$(mktemp -d)"; trap 'rm -rf "$TMP"' EXIT INT TERM
TROOT="$TMP/opt"; STATE="$TMP/runtime.state"
mkdir -p "$TROOT/etc/freenet/native-dns" "$TROOT/etc/xray/configs" "$TROOT/etc/xray/dat" "$TROOT/etc/init.d" "$TROOT/sbin" "$TROOT/backups"
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
{"dns":{"tag":"dns-vless","servers":[{"address":"https://8.8.8.8/dns-query","tag":"dns-vless","finalQuery":true}],"queryStrategy":"UseIPv4"}}
EOF
cat > "$TROOT/etc/xray/configs/03_inbounds.json" <<'EOF'
{"inbounds":[{"tag":"redirect","port":5000,"protocol":"dokodemo-door"},{"tag":"dns","port":53,"protocol":"dokodemo-door","settings":{"network":"tcp,udp"}}]}
EOF
cat > "$TROOT/etc/xray/configs/04_outbounds.json" <<'EOF'
{"outbounds":[{"tag":"vless-reality","protocol":"freedom"},{"tag":"direct","protocol":"freedom"},{"tag":"dns-out","protocol":"dns"}]}
EOF
cat > "$TROOT/etc/xray/configs/05_routing.json" <<'EOF'
{"routing":{"rules":[{"type":"field","inboundTag":["dns-vless"],"outboundTag":"vless-reality"},{"type":"field","inboundTag":["dns-direct"],"outboundTag":"direct"},{"type":"field","port":53,"outboundTag":"dns-out"},{"type":"field","domain":["ext:geosite.dat:youtube"],"outboundTag":"direct"},{"type":"field","network":"tcp,udp","outboundTag":"vless-reality"}]}}
EOF
printf '{}\n' > "$TROOT/etc/freenet/native-dns/02_dns.native"
sha256sum "$TROOT/etc/freenet/native-dns/02_dns.native" | awk '{print $1}' > "$TROOT/etc/freenet/native-dns/02_dns.native.sha256"
printf 'public\n' > "$TROOT/etc/freenet/native-dns/filter-engine.native"
printf 'on\n' > "$TROOT/etc/freenet/native-dns/intercept.native"
cat > "$STATE" <<'EOF'
PORT53_OWNER=xray
NDM_DNS_OVERRIDE=on
NDM_FILTER_ENGINE=public
NDM_DNS_INTERCEPT=off
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

run_network apply > "$TMP/apply.log" 2>&1 || { cat "$TMP/apply.log" >&2; fail 'confirmed native-engine partial state did not recover'; }
grep -Fq 'PARTIAL_NATIVE_CONTROL_PLANE=confirmed-native-engine' "$TMP/apply.log" || fail 'partial recovery marker missing'
grep -Fq 'RESULT=SUCCESS' "$TMP/apply.log" || fail 'success marker missing'
grep -q '^NDM_DNS_OVERRIDE=off$' "$STATE" || fail 'dns override not disabled'
grep -q '^NDM_FILTER_ENGINE=public$' "$STATE" || fail 'native engine not preserved'
grep -q '^NDM_DNS_INTERCEPT=on$' "$STATE" || fail 'native intercept not restored'
grep -q '^PORT53_OWNER=ndnproxy$' "$STATE" || fail 'ndnproxy did not regain :53'
grep -q '^NDM_DNS_PROFILE_MARKER=preserved$' "$STATE" || fail 'protected native DNS state changed'
jq -e '([.inbounds[]? | select(((.port // "")|tostring)=="53")] | length)==0' "$TROOT/etc/xray/configs/03_inbounds.json" >/dev/null || fail 'Xray :53 remained after native recovery'
jq -e '([.outbounds[]? | select(.tag=="dns-out")] | length)==0' "$TROOT/etc/xray/configs/04_outbounds.json" >/dev/null || fail 'dns-out remained after native recovery'

echo "partial native recovery: PASS"
