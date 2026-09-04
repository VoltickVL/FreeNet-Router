#!/bin/sh
set -eu

ROOT_DIR="$(CDPATH= cd "$(dirname "$0")/.." && pwd)"
SCRIPT="$ROOT_DIR/scripts/apply_network_profile.sh"
fail() { echo "network profile contract FAIL: $*" >&2; exit 1; }

sh -n "$SCRIPT"
for marker in \
    "ndmc -c 'opkg dns-override'" \
    "ndmc -c 'no opkg dns-override'" \
    "ndmc -c 'system configuration save'" \
    'NDM_DNS_OVERRIDE=' \
    '02_dns may be native JSONC/comment-only' \
    'ROLLBACK ERROR/STATE: no live apply' \
    'ROLLBACK ERROR/STATE: FAILED/UNKNOWN' \
    'TARGETS="127.0.0.1"'; do
    grep -Fq "$marker" "$SCRIPT" || fail "missing contract marker: $marker"
done
if grep -Fq "ip -4 addr show br0" "$SCRIPT"; then fail 'DNS acceptance hardcodes br0'; fi
if grep -Ei 'subscription.*url=|uuid=|publicKey|shortId|vless://' "$SCRIPT" >/dev/null; then fail 'network controller contains secret material'; fi

TMP="$(mktemp -d)"; trap 'rm -rf "$TMP"' EXIT INT TERM
TROOT="$TMP/opt"; STATE="$TMP/runtime.state"
mkdir -p "$TROOT/etc/freenet" "$TROOT/etc/xray/configs" "$TROOT/etc/xray/dat" "$TROOT/etc/init.d" "$TROOT/sbin" "$TROOT/backups"
cat > "$TROOT/etc/freenet/freenet.conf" <<'EOF'
ISP_ID=rostelecom
DNS_MODE=firmware
EOF
cat > "$TROOT/etc/init.d/S05xkeen" <<'EOF'
#!/bin/sh
proxy_dns="off"
EOF
chmod 755 "$TROOT/etc/init.d/S05xkeen"
cat > "$TROOT/etc/xray/configs/02_dns.json" <<'EOF'
// Native Keenetic DNS: no active Xray DNS object.
// Preserve this exact comment-only file.
EOF
cat > "$TROOT/etc/xray/configs/03_inbounds.json" <<'EOF'
{"inbounds":[{"tag":"redirect","port":5000,"protocol":"dokodemo-door"},{"tag":"tproxy","port":5000,"protocol":"dokodemo-door"}]}
EOF
cat > "$TROOT/etc/xray/configs/04_outbounds.json" <<'EOF'
{"outbounds":[{"tag":"vless-reality","protocol":"freedom","settings":{"marker":"preserve-vpn"}},{"tag":"direct","protocol":"freedom"},{"tag":"block","protocol":"blackhole"}]}
EOF
cat > "$TROOT/etc/xray/configs/05_routing.json" <<'EOF'
{"routing":{"domainStrategy":"AsIs","rules":[{"type":"field","domain":["ext:geosite.dat:youtube"],"outboundTag":"vless-reality"},{"type":"field","domain":["domain:example.ru"],"outboundTag":"direct"},{"type":"field","network":"tcp,udp","outboundTag":"vless-reality"}]}}
EOF
cat > "$STATE" <<'EOF'
PORT53_OWNER=ndnproxy
NDM_DNS_OVERRIDE=off
NDM_CONFIG_MARKER=preserved
XRAY_RUNNING=yes
XRAY_GID=11111
DNS_QUERY_OK=yes
XKEEN_ACTION_RESULT=success
NDM_ACTION_RESULT=success
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
state_set() { key="$1"; value="$2"; t="$STATE.tmp.$$"; grep -v "^${key}=" "$STATE" > "$t" || true; echo "${key}=${value}" >> "$t"; mv "$t" "$STATE"; }
hash_in() { jq -cS '[.inbounds[]? | select((((.port // "") | tostring) != "53"))]' "$TROOT/etc/xray/configs/03_inbounds.json" | sha256sum | awk '{print $1}'; }
hash_out() { jq -cS '[.outbounds[]? | select((.tag // "") != "dns-out")]' "$TROOT/etc/xray/configs/04_outbounds.json" | sha256sum | awk '{print $1}'; }
hash_route() { jq -cS '[.routing.rules[]? | select((.outboundTag // "") != "dns-out") | select(((.inboundTag // []) | index("dns-vless")) == null) | select(((.inboundTag // []) | index("dns-direct")) == null) | select((((.port // "") | tostring) != "53"))]' "$TROOT/etc/xray/configs/05_routing.json" | sha256sum | awk '{print $1}'; }

H02="$(sha256sum "$TROOT/etc/xray/configs/02_dns.json" | awk '{print $1}')"; HIN="$(hash_in)"; HOUT="$(hash_out)"; HROUTE="$(hash_route)"

run_network plan > "$TMP/native.plan"
for x in 'EFFECTIVE_DNS_MODE=firmware' 'NDM_DNS_OVERRIDE=off' 'PORT53_OWNER=ndnproxy' 'DNS_OUT=no' 'DNS_ROUTING_MODE=native'; do grep -Fq "$x" "$TMP/native.plan" || fail "native fact missing: $x"; done
run_network apply > "$TMP/native.apply" 2>&1 || fail 'native no-op failed'
grep -Fq 'RESULT=SUCCESS' "$TMP/native.apply" || fail 'native no-op result missing'

# A read-only native acceptance failure must be classified as NOT_APPLIED, not UNKNOWN.
state_set DNS_QUERY_OK no
if run_network apply > "$TMP/native-query-fail.apply" 2>&1; then fail 'native DNS query failure unexpectedly succeeded'; fi
grep -Fq 'PRIMARY ERROR: native Keenetic DNS query failed' "$TMP/native-query-fail.apply" || fail 'native no-op primary error missing'
grep -Fq 'ROLLBACK ERROR/STATE: no live apply' "$TMP/native-query-fail.apply" || fail 'native no-op must report no live apply'
grep -q '^NDM_DNS_OVERRIDE=off$' "$STATE" || fail 'native no-op failure mutated NDM'
grep -q '^PORT53_OWNER=ndnproxy$' "$STATE" || fail 'native no-op failure changed :53 owner'
state_set DNS_QUERY_OK yes

sed -i 's/^DNS_MODE=.*/DNS_MODE=xkeen/' "$TROOT/etc/freenet/freenet.conf"
run_network apply > "$TMP/split.apply" 2>&1 || { cat "$TMP/split.apply" >&2; fail 'native -> split failed'; }
grep -q '^NDM_DNS_OVERRIDE=on$' "$STATE" || fail 'NDM override not enabled'
grep -q '^PORT53_OWNER=xray$' "$STATE" || fail 'Xray did not own :53'
[ "$(sha256sum "$TROOT/etc/freenet/native-dns/02_dns.native" | awk '{print $1}')" = "$H02" ] || fail 'native 02 snapshot changed'
jq -e '([.inbounds[]? | select(((.port // "")|tostring)=="53")] | length)==1' "$TROOT/etc/xray/configs/03_inbounds.json" >/dev/null || fail 'split :53 missing'
jq -e '([.outbounds[]? | select(.tag=="dns-out" and .protocol=="dns")] | length)==1' "$TROOT/etc/xray/configs/04_outbounds.json" >/dev/null || fail 'dns-out missing'
jq -e '([.routing.rules[]? | select(((.inboundTag // [])|index("dns-vless"))!=null and .outboundTag=="vless-reality")] | length)==1' "$TROOT/etc/xray/configs/05_routing.json" >/dev/null || fail 'dns-vless route missing'
[ "$(hash_in)" = "$HIN" ] || fail 'split changed non-DNS inbounds'
[ "$(hash_out)" = "$HOUT" ] || fail 'split changed VPN/non-DNS outbounds'
[ "$(hash_route)" = "$HROUTE" ] || fail 'split changed non-DNS routing'

sed -i 's/^DNS_MODE=.*/DNS_MODE=firmware/' "$TROOT/etc/freenet/freenet.conf"
run_network apply > "$TMP/restore.apply" 2>&1 || { cat "$TMP/restore.apply" >&2; fail 'split -> native failed'; }
grep -q '^NDM_DNS_OVERRIDE=off$' "$STATE" || fail 'NDM override not disabled'
grep -q '^PORT53_OWNER=ndnproxy$' "$STATE" || fail 'ndnproxy did not regain :53'
[ "$(sha256sum "$TROOT/etc/xray/configs/02_dns.json" | awk '{print $1}')" = "$H02" ] || fail 'native 02 not restored byte-for-byte'
jq -e '([.inbounds[]? | select(((.port // "")|tostring)=="53")] | length)==0' "$TROOT/etc/xray/configs/03_inbounds.json" >/dev/null || fail 'native still has Xray :53'
jq -e '([.outbounds[]? | select(.tag=="dns-out")] | length)==0' "$TROOT/etc/xray/configs/04_outbounds.json" >/dev/null || fail 'native still has dns-out'
[ "$(hash_in)" = "$HIN" ] || fail 'native changed non-DNS inbounds'
[ "$(hash_out)" = "$HOUT" ] || fail 'native changed VPN/non-DNS outbounds'
[ "$(hash_route)" = "$HROUTE" ] || fail 'native changed non-DNS routing'

# Failure after live mutation must restore exact native state.
sed -i 's/^DNS_MODE=.*/DNS_MODE=xkeen/' "$TROOT/etc/freenet/freenet.conf"
for n in 02_dns.json 03_inbounds.json 04_outbounds.json 05_routing.json; do cp "$TROOT/etc/xray/configs/$n" "$TMP/$n.before"; done
state_set DNS_QUERY_OK no
if run_network apply > "$TMP/fail.apply" 2>&1; then fail 'failed DNS acceptance unexpectedly succeeded'; fi
grep -Fq 'ROLLBACK ERROR/STATE: rollback success' "$TMP/fail.apply" || fail 'rollback success missing'
grep -q '^NDM_DNS_OVERRIDE=off$' "$STATE" || fail 'rollback did not restore NDM'
grep -q '^PORT53_OWNER=ndnproxy$' "$STATE" || fail 'rollback did not restore :53 owner'
for n in 02_dns.json 03_inbounds.json 04_outbounds.json 05_routing.json; do cmp -s "$TMP/$n.before" "$TROOT/etc/xray/configs/$n" || fail "rollback changed $n"; done
state_set DNS_QUERY_OK yes

state_set PORT53_OWNER none
if run_network apply > "$TMP/partial.apply" 2>&1; then fail 'partial topology unexpectedly succeeded'; fi
grep -Fq 'native mode должен иметь ndnproxy' "$TMP/partial.apply" || fail 'partial topology STOP missing'
grep -Fq 'ROLLBACK ERROR/STATE: no live apply' "$TMP/partial.apply" || fail 'partial preflight must report no live apply'
grep -q '^NDM_DNS_OVERRIDE=off$' "$STATE" || fail 'partial preflight mutated NDM'

echo 'network profile contract PASS'
