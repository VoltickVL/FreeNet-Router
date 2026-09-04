#!/bin/sh
set -eu

ROOT_DIR="$(CDPATH= cd "$(dirname "$0")/.." && pwd)"
SCRIPT="$ROOT_DIR/scripts/apply_network_profile.sh"

fail() { echo "network profile contract FAIL: $*" >&2; exit 1; }

sh -n "$SCRIPT"
grep -Fq "ndmc -c 'opkg dns-override'" "$SCRIPT" || fail 'OPKG DNS override enable missing'
grep -Fq "ndmc -c 'no opkg dns-override'" "$SCRIPT" || fail 'native DNS override disable missing'
grep -Fq "ndmc -c 'system configuration save'" "$SCRIPT" || fail 'NDM persistence missing'
grep -Fq 'NDM_DNS_OVERRIDE=' "$SCRIPT" || fail 'NDM override runtime fact missing'
grep -Fq '02_dns may be native JSONC/comment-only' "$SCRIPT" || fail 'opaque native 02_dns compatibility missing'
grep -Fq 'ROLLBACK ERROR/STATE: FAILED/UNKNOWN' "$SCRIPT" || fail 'rollback failure state missing'
if grep -Ei 'subscription.*url=|uuid=|publicKey|shortId|vless://' "$SCRIPT" >/dev/null; then fail 'network controller contains secret material'; fi

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT INT TERM
TROOT="$TMP/opt"
STATE="$TMP/runtime.state"
mkdir -p "$TROOT/etc/freenet" "$TROOT/etc/xray/configs" "$TROOT/etc/xray/dat" "$TROOT/etc/init.d" "$TROOT/sbin" "$TROOT/backups"

cat > "$TROOT/etc/freenet/freenet.conf" <<'EOF_CONF'
ISP_ID=rostelecom
DNS_MODE=firmware
EOF_CONF
cat > "$TROOT/etc/init.d/S05xkeen" <<'EOF_INIT'
#!/bin/sh
proxy_dns="off"
EOF_INIT
chmod 755 "$TROOT/etc/init.d/S05xkeen"

# Real WORK compatibility: native 02_dns may be comment-only JSONC and must be opaque.
cat > "$TROOT/etc/xray/configs/02_dns.json" <<'EOF_DNS'
// Native Keenetic DNS: no active Xray DNS object.
// This exact content must survive native -> split -> native.
EOF_DNS
cat > "$TROOT/etc/xray/configs/03_inbounds.json" <<'EOF_IN'
{"inbounds":[{"tag":"redirect","port":5000,"protocol":"dokodemo-door"},{"tag":"tproxy","port":5000,"protocol":"dokodemo-door"}]}
EOF_IN
cat > "$TROOT/etc/xray/configs/04_outbounds.json" <<'EOF_OUT'
{"outbounds":[{"tag":"vless-reality","protocol":"freedom","settings":{"marker":"preserve-vpn"}},{"tag":"direct","protocol":"freedom"},{"tag":"block","protocol":"blackhole"}]}
EOF_OUT
cat > "$TROOT/etc/xray/configs/05_routing.json" <<'EOF_ROUTE'
{"routing":{"domainStrategy":"AsIs","rules":[{"type":"field","domain":["ext:geosite.dat:youtube"],"outboundTag":"vless-reality"},{"type":"field","domain":["domain:example.ru"],"outboundTag":"direct"},{"type":"field","network":"tcp,udp","outboundTag":"vless-reality"}]}}
EOF_ROUTE

cat > "$STATE" <<'EOF_STATE'
PORT53_OWNER=ndnproxy
NDM_DNS_OVERRIDE=off
NDM_CONFIG_MARKER=preserved
XRAY_RUNNING=yes
XRAY_GID=11111
DNS_QUERY_OK=yes
XKEEN_ACTION_RESULT=success
NDM_ACTION_RESULT=success
NDM_SAVE_RESULT=success
EOF_STATE

cat > "$TROOT/sbin/xkeen" <<'EOF_XKEEN'
#!/bin/sh
exit 0
EOF_XKEEN
chmod 755 "$TROOT/sbin/xkeen"
cat > "$TROOT/sbin/xray" <<'EOF_XRAY'
#!/bin/sh
exit 0
EOF_XRAY
chmod 755 "$TROOT/sbin/xray"

run_network() {
    FREENET_ROOT="$TROOT" \
    FREENET_BACKUP_ROOT="$TROOT/backups" \
    FREENET_CONFIG_FILE="$TROOT/etc/freenet/freenet.conf" \
    FREENET_CONFIG_DIR="$TROOT/etc/xray/configs" \
    FREENET_XRAY_ASSET_DIR="$TROOT/etc/xray/dat" \
    FREENET_XKEEN_BIN="$TROOT/sbin/xkeen" \
    FREENET_XRAY_BIN="$TROOT/sbin/xray" \
    FREENET_XKEEN_RUNTIME_TIMEOUT=2 \
    FREENET_NETWORK_TEST_MODE=yes \
    FREENET_NETWORK_TEST_STATE="$STATE" \
    sh "$SCRIPT" "$@"
}

state_set() {
    key="$1"; value="$2"; t="$STATE.tmp.$$"
    grep -v "^${key}=" "$STATE" > "$t" 2>/dev/null || true
    echo "${key}=${value}" >> "$t"
    mv "$t" "$STATE"
}

NATIVE_02_HASH="$(sha256sum "$TROOT/etc/xray/configs/02_dns.json" | awk '{print $1}')"
NATIVE_IN_HASH="$(jq -cS . "$TROOT/etc/xray/configs/03_inbounds.json" | sha256sum | awk '{print $1}')"
NATIVE_OUT_NON_DNS_HASH="$(jq -cS '[.outbounds[] | select(.tag != "dns-out")]' "$TROOT/etc/xray/configs/04_outbounds.json" | sha256sum | awk '{print $1}')"
NATIVE_ROUTE_NON_DNS_HASH="$(jq -cS '.routing.rules' "$TROOT/etc/xray/configs/05_routing.json" | sha256sum | awk '{print $1}')"

# Native WORK state is a first-class active topology: no Xray DNS objects.
run_network plan > "$TMP/native.plan"
grep -Fq 'EFFECTIVE_DNS_MODE=firmware' "$TMP/native.plan" || fail 'firmware effective mode missing'
grep -Fq 'NDM_DNS_OVERRIDE=off' "$TMP/native.plan" || fail 'native NDM override fact missing'
grep -Fq 'PORT53_OWNER=ndnproxy' "$TMP/native.plan" || fail 'native :53 owner missing'
grep -Fq 'DNS_OUT=no' "$TMP/native.plan" || fail 'native must have no dns-out'
grep -Fq 'DNS_ROUTING_MODE=native' "$TMP/native.plan" || fail 'native routing mode missing'
run_network apply > "$TMP/native.apply" 2>&1 || fail 'native no-op should succeed'
grep -Fq 'RESULT=SUCCESS' "$TMP/native.apply" || fail 'native no-op success marker missing'

# Native -> OPKG/Xray Split DNS.
sed -i 's/^DNS_MODE=.*/DNS_MODE=xkeen/' "$TROOT/etc/freenet/freenet.conf"
run_network plan > "$TMP/split.plan"
grep -Fq 'EXPECTED_DELTA=enable opkg dns-override' "$TMP/split.plan" || fail 'split NDM delta missing'
run_network apply > "$TMP/split.apply" 2>&1 || { cat "$TMP/split.apply" >&2; fail 'native -> split failed'; }
grep -Fq 'RESULT=SUCCESS' "$TMP/split.apply" || fail 'split success missing'
grep -Fq 'NDM_DNS_OVERRIDE=on' "$TMP/split.apply" || fail 'split override result missing'
grep -Fq '^NDM_DNS_OVERRIDE=on$' "$STATE" || fail 'test NDM override not enabled'
grep -Fq '^PORT53_OWNER=xray$' "$STATE" || fail 'Xray did not own :53'
[ -f "$TROOT/etc/freenet/native-dns/02_dns.native" ] || fail 'native 02 snapshot missing'
[ "$(sha256sum "$TROOT/etc/freenet/native-dns/02_dns.native" | awk '{print $1}')" = "$NATIVE_02_HASH" ] || fail 'native 02 snapshot changed'
jq -e '([.inbounds[] | select(((.port // "")|tostring)=="53" and .protocol=="dokodemo-door")] | length)==1' "$TROOT/etc/xray/configs/03_inbounds.json" >/dev/null || fail 'split :53 inbound missing'
jq -e '([.outbounds[] | select(.tag=="dns-out" and .protocol=="dns")] | length)==1' "$TROOT/etc/xray/configs/04_outbounds.json" >/dev/null || fail 'split dns-out missing'
jq -e '([.routing.rules[] | select(((.inboundTag // [])|index("dns-vless"))!=null and .outboundTag=="vless-reality")] | length)==1' "$TROOT/etc/xray/configs/05_routing.json" >/dev/null || fail 'split dns-vless route missing'
jq -e '([.routing.rules[] | select(((.inboundTag // [])|index("dns-direct"))!=null and .outboundTag=="direct")] | length)==1' "$TROOT/etc/xray/configs/05_routing.json" >/dev/null || fail 'split dns-direct route missing'
[ "$(jq -cS '[.inbounds[] | select((((.port // "") | tostring) != "53"))]' "$TROOT/etc/xray/configs/03_inbounds.json" | sha256sum | awk '{print $1}')" = "$NATIVE_IN_HASH" ] || fail 'split changed non-DNS inbounds'
[ "$(jq -cS '[.outbounds[] | select(.tag != "dns-out")]' "$TROOT/etc/xray/configs/04_outbounds.json" | sha256sum | awk '{print $1}')" = "$NATIVE_OUT_NON_DNS_HASH" ] || fail 'split changed non-DNS outbounds/VPN'
[ "$(jq -cS '[.routing.rules[] | select((.outboundTag // "") != "dns-out") | select(((.inboundTag // []) | index("dns-vless")) == null) | select(((.inboundTag // []) | index("dns-direct")) == null) | select((((.port // "") | tostring) != "53"))]' "$TROOT/etc/xray/configs/05_routing.json" | sha256sum | awk '{print $1}')" = "$NATIVE_ROUTE_NON_DNS_HASH" ] || fail 'split changed non-DNS routing'

# OPKG/Xray -> exact native Keenetic DNS.
sed -i 's/^DNS_MODE=.*/DNS_MODE=firmware/' "$TROOT/etc/freenet/freenet.conf"
run_network apply > "$TMP/restore.apply" 2>&1 || { cat "$TMP/restore.apply" >&2; fail 'split -> native failed'; }
grep -Fq 'NDM_DNS_OVERRIDE=off' "$TMP/restore.apply" || fail 'native override result missing'
grep -Fq '^NDM_DNS_OVERRIDE=off$' "$STATE" || fail 'NDM override not disabled'
grep -Fq '^PORT53_OWNER=ndnproxy$' "$STATE" || fail 'ndnproxy did not regain :53'
[ "$(sha256sum "$TROOT/etc/xray/configs/02_dns.json" | awk '{print $1}')" = "$NATIVE_02_HASH" ] || fail 'native 02 not restored byte-for-byte'
jq -e '([.inbounds[] | select(((.port // "")|tostring)=="53")] | length)==0' "$TROOT/etc/xray/configs/03_inbounds.json" >/dev/null || fail 'native still has Xray :53'
jq -e '([.outbounds[] | select(.tag=="dns-out")] | length)==0' "$TROOT/etc/xray/configs/04_outbounds.json" >/dev/null || fail 'native still has dns-out'
jq -e '([.routing.rules[] | select(((.inboundTag // [])|index("dns-vless"))!=null or ((.inboundTag // [])|index("dns-direct"))!=null or (((.port // "")|tostring)=="53"))] | length)==0' "$TROOT/etc/xray/configs/05_routing.json" >/dev/null || fail 'native still has DNS routing delta'
[ "$(jq -cS . "$TROOT/etc/xray/configs/03_inbounds.json" | sha256sum | awk '{print $1}')" = "$NATIVE_IN_HASH" ] || fail 'native non-DNS inbounds not restored'
[ "$(jq -cS '[.outbounds[] | select(.tag != "dns-out")]' "$TROOT/etc/xray/configs/04_outbounds.json" | sha256sum | awk '{print $1}')" = "$NATIVE_OUT_NON_DNS_HASH" ] || fail 'native VPN/non-DNS outbounds changed'
[ "$(jq -cS '.routing.rules' "$TROOT/etc/xray/configs/05_routing.json" | sha256sum | awk '{print $1}')" = "$NATIVE_ROUTE_NON_DNS_HASH" ] || fail 'native non-DNS routing not restored'

# A failed Split post-acceptance must restore native NDM + all Xray files.
sed -i 's/^DNS_MODE=.*/DNS_MODE=xkeen/' "$TROOT/etc/freenet/freenet.conf"
cp "$TROOT/etc/xray/configs/02_dns.json" "$TMP/fail.02.before"
cp "$TROOT/etc/xray/configs/03_inbounds.json" "$TMP/fail.03.before"
cp "$TROOT/etc/xray/configs/04_outbounds.json" "$TMP/fail.04.before"
cp "$TROOT/etc/xray/configs/05_routing.json" "$TMP/fail.05.before"
state_set DNS_QUERY_OK no
if run_network apply > "$TMP/split.fail" 2>&1; then fail 'failed DNS acceptance unexpectedly succeeded'; fi
grep -Fq 'ROLLBACK ERROR/STATE: rollback success' "$TMP/split.fail" || fail 'rollback success not reported'
grep -Fq '^NDM_DNS_OVERRIDE=off$' "$STATE" || fail 'rollback did not restore native override'
grep -Fq '^PORT53_OWNER=ndnproxy$' "$STATE" || fail 'rollback did not restore native :53 owner'
cmp -s "$TMP/fail.02.before" "$TROOT/etc/xray/configs/02_dns.json" || fail 'rollback did not restore 02'
cmp -s "$TMP/fail.03.before" "$TROOT/etc/xray/configs/03_inbounds.json" || fail 'rollback did not restore 03'
cmp -s "$TMP/fail.04.before" "$TROOT/etc/xray/configs/04_outbounds.json" || fail 'rollback did not restore 04'
cmp -s "$TMP/fail.05.before" "$TROOT/etc/xray/configs/05_routing.json" || fail 'rollback did not restore 05'
state_set DNS_QUERY_OK yes

# Partial/unknown topology must STOP before mutation.
state_set PORT53_OWNER none
if run_network apply > "$TMP/partial.fail" 2>&1; then fail 'partial DNS topology unexpectedly succeeded'; fi
grep -Eq 'native mode должен иметь ndnproxy|partial/unknown' "$TMP/partial.fail" || fail 'partial topology STOP reason missing'
grep -Fq '^NDM_DNS_OVERRIDE=off$' "$STATE" || fail 'partial preflight mutated NDM'

echo 'network profile contract PASS'
