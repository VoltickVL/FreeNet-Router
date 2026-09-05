#!/bin/sh
set -eu

ROOT_DIR="$(CDPATH= cd "$(dirname "$0")/.." && pwd)"
SCRIPT="$ROOT_DIR/scripts/apply_network_profile.sh"
fail() { echo "network profile contract FAIL: $*" >&2; exit 1; }

sh -n "$SCRIPT"
for marker in \
    "ndmc -c 'opkg dns-override'" \
    "ndmc -c 'no opkg dns-override'" \
    'ndmc -c "dns-proxy filter engine $WANT"' \
    "ndmc -c 'dns-proxy intercept enable'" \
    "ndmc -c 'dns-proxy no intercept enable'" \
    "ndmc -c 'system configuration save'" \
    'NDM_DNS_OVERRIDE=' \
    'NDM_FILTER_ENGINE=' \
    'NDM_DNS_INTERCEPT=' \
    'filter-engine.native' \
    'intercept.native' \
    'assignments.native' \
    'split-assignments' \
    'dns-proxy no filter assign' \
    'ndm_protected_state' \
    'Keenetic protected DNS/WAN state changed unexpectedly' \
    '02_dns may be native JSONC/comment-only' \
    'ROLLBACK ERROR/STATE: no live apply' \
    'ROLLBACK ERROR/STATE: FAILED/UNKNOWN' \
    'TARGETS="127.0.0.1"' \
    'ordered first-match DNS policy mirrors domain routing' \
    'suppress native System DNS intercept' \
    'normalize proxy_dns=off if required without unnecessary runtime restart'; do
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
proxy_dns="on"
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
{"routing":{"domainStrategy":"AsIs","rules":[{"type":"field","domain":["ext:geosite.dat:youtube"],"outboundTag":"vless-reality"},{"type":"field","domain":["ext:geosite.dat:google","domain:example.ru"],"outboundTag":"direct"},{"type":"field","network":"tcp,udp","outboundTag":"vless-reality"}]}}
EOF
cat > "$STATE" <<'EOF'
PORT53_OWNER=ndnproxy
NDM_DNS_OVERRIDE=off
NDM_FILTER_ENGINE=public
NDM_DNS_INTERCEPT=on
NDM_CONFIG_MARKER=preserved
NDM_DNS_PROFILE_MARKER=preserved
NDM_DNS_ASSIGNMENTS=on
NDM_MUTATE_PROTECTED_ON_OVERRIDE=no
XRAY_RUNNING=yes
XRAY_GID=11111
DNS_QUERY_OK=yes
XKEEN_ACTION_RESULT=fail
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
state_set() { key="$1"; value="$2"; t="$STATE.tmp.$$"; grep -v "^${key}=" "$STATE" > "$t" || true; echo "${key}=${value}" >> "$t"; mv "$t" "$STATE"; }
hash_in() { jq -cS '[.inbounds[]? | select((((.port // "") | tostring) != "53"))]' "$TROOT/etc/xray/configs/03_inbounds.json" | sha256sum | awk '{print $1}'; }
hash_out() { jq -cS '[.outbounds[]? | select((.tag // "") != "dns-out")]' "$TROOT/etc/xray/configs/04_outbounds.json" | sha256sum | awk '{print $1}'; }
hash_route() { jq -cS '[.routing.rules[]? | select((.outboundTag // "") != "dns-out") | select(((.inboundTag // []) | index("dns-vless")) == null) | select(((.inboundTag // []) | index("dns-direct")) == null) | select((((.port // "") | tostring) != "53"))]' "$TROOT/etc/xray/configs/05_routing.json" | sha256sum | awk '{print $1}'; }

H02="$(sha256sum "$TROOT/etc/xray/configs/02_dns.json" | awk '{print $1}')"; HIN="$(hash_in)"; HOUT="$(hash_out)"; HROUTE="$(hash_route)"

run_network plan > "$TMP/native.plan"
for x in 'EFFECTIVE_DNS_MODE=firmware' 'PROXY_DNS=on' 'NDM_DNS_OVERRIDE=off' 'NDM_FILTER_ENGINE=public' 'NDM_DNS_INTERCEPT=on' 'NDM_DNS_ASSIGNMENTS=present' 'PORT53_OWNER=ndnproxy' 'DNS_OUT=no' 'DNS_ROUTING_MODE=native'; do grep -Fq "$x" "$TMP/native.plan" || fail "native fact missing: $x"; done

# WORK-like existing stack: healthy native DNS may carry legacy proxy_dns=on in init.
# FreeNet must normalize the persisted init value without restarting a working native runtime.
run_network apply > "$TMP/native.apply" 2>&1 || { cat "$TMP/native.apply" >&2; fail 'native legacy proxy normalization failed'; }
grep -Fq 'RESULT=SUCCESS' "$TMP/native.apply" || fail 'native normalization result missing'
grep -Eq '^proxy_dns="?off"?$' "$TROOT/etc/init.d/S05xkeen" || fail 'native apply did not normalize proxy_dns=off'
grep -q '^NDM_DNS_OVERRIDE=off$' "$STATE" || fail 'native normalization changed NDM override'
grep -q '^NDM_FILTER_ENGINE=public$' "$STATE" || fail 'native normalization changed filter engine'
grep -q '^NDM_DNS_INTERCEPT=on$' "$STATE" || fail 'native normalization changed system intercept'
grep -q '^NDM_DNS_ASSIGNMENTS=on$' "$STATE" || fail 'native normalization changed filter assignments'
grep -q '^PORT53_OWNER=ndnproxy$' "$STATE" || fail 'native normalization changed :53 owner'
grep -q '^XRAY_RUNNING=yes$' "$STATE" || fail 'native normalization changed Xray runtime'
# XKEEN_ACTION_RESULT=fail proves the successful native normalization did not invoke restart/start/stop.
state_set XKEEN_ACTION_RESULT success

# A read-only native acceptance failure must be classified as NOT_APPLIED, not UNKNOWN.
state_set DNS_QUERY_OK no
if run_network apply > "$TMP/native-query-fail.apply" 2>&1; then fail 'native DNS query failure unexpectedly succeeded'; fi
grep -Fq 'PRIMARY ERROR: native Keenetic DNS query failed' "$TMP/native-query-fail.apply" || fail 'native no-op primary error missing'
grep -Fq 'ROLLBACK ERROR/STATE: no live apply' "$TMP/native-query-fail.apply" || fail 'native no-op must report no live apply'
grep -q '^NDM_DNS_OVERRIDE=off$' "$STATE" || fail 'native no-op failure mutated NDM'
grep -q '^NDM_FILTER_ENGINE=public$' "$STATE" || fail 'native no-op failure changed filter engine'
grep -q '^NDM_DNS_INTERCEPT=on$' "$STATE" || fail 'native no-op failure changed intercept'
grep -q '^NDM_DNS_ASSIGNMENTS=on$' "$STATE" || fail 'native no-op failure changed filter assignments'
grep -q '^PORT53_OWNER=ndnproxy$' "$STATE" || fail 'native no-op failure changed :53 owner'
state_set DNS_QUERY_OK yes

# Direct native -> Split manages all required Keenetic control-plane state itself:
# dns-override frees :53, filter engine becomes opkg, and native System-profile DNS
# interception is disabled so ordinary clients cannot bypass Xray to native Yandex/DoT/DoH.
sed -i 's/^proxy_dns=.*/proxy_dns="on"/' "$TROOT/etc/init.d/S05xkeen"
sed -i 's/^DNS_MODE=.*/DNS_MODE=xkeen/' "$TROOT/etc/freenet/freenet.conf"
run_network apply > "$TMP/split.apply" 2>&1 || { cat "$TMP/split.apply" >&2; fail 'native -> split failed'; }
grep -q '^NDM_DNS_OVERRIDE=on$' "$STATE" || fail 'NDM override not enabled'
grep -q '^NDM_FILTER_ENGINE=opkg$' "$STATE" || fail 'Keenetic filter engine not switched to opkg'
grep -q '^NDM_DNS_INTERCEPT=off$' "$STATE" || fail 'native System DNS intercept not disabled'
grep -q '^NDM_DNS_ASSIGNMENTS=off$' "$STATE" || fail 'native DNS filter assignments not detached in Split'
grep -q '^PORT53_OWNER=xray$' "$STATE" || fail 'Xray did not own :53'
grep -Eq '^proxy_dns="?off"?$' "$TROOT/etc/init.d/S05xkeen" || fail 'split did not normalize proxy_dns=off'
[ "$(cat "$TROOT/etc/freenet/native-dns/filter-engine.native")" = public ] || fail 'native filter engine snapshot missing or wrong'
[ "$(cat "$TROOT/etc/freenet/native-dns/intercept.native")" = on ] || fail 'native intercept snapshot missing or wrong'
grep -Fq 'filter assign host profile aa:bb:cc:dd:ee:ff xbox-dns.ru' "$TROOT/etc/freenet/native-dns/assignments.native" || fail 'native host assignment snapshot missing'
grep -Fq 'filter assign interface preset Home cloudflare-unfiltered' "$TROOT/etc/freenet/native-dns/assignments.native" || fail 'native interface assignment snapshot missing'
[ "$(sha256sum "$TROOT/etc/freenet/native-dns/02_dns.native" | awk '{print $1}')" = "$H02" ] || fail 'native 02 snapshot changed'
jq -e '([.inbounds[]? | select(((.port // "")|tostring)=="53")] | length)==1' "$TROOT/etc/xray/configs/03_inbounds.json" >/dev/null || fail 'split :53 missing'
jq -e '([.outbounds[]? | select(.tag=="dns-out" and .protocol=="dns")] | length)==1' "$TROOT/etc/xray/configs/04_outbounds.json" >/dev/null || fail 'dns-out missing'
jq -e '([.routing.rules[]? | select(((.inboundTag // [])|index("dns-vless"))!=null and .outboundTag=="vless-reality")] | length)==1' "$TROOT/etc/xray/configs/05_routing.json" >/dev/null || fail 'dns-vless route missing'
# First-match parity: YouTube VPN rule precedes the broader Google DIRECT rule and
# therefore its DNS selector must also precede the Google selector and terminate fallback.
jq -e '
  ([.dns.servers | to_entries[] | select((.value.domains // []) | index("ext:geosite.dat:youtube")) | .key][0]) as $youtube |
  ([.dns.servers | to_entries[] | select((.value.domains // []) | index("ext:geosite.dat:google")) | .key][0]) as $google |
  $youtube < $google and .dns.servers[$youtube].tag=="dns-vless" and .dns.servers[$youtube].finalQuery==true and .dns.servers[$google].tag=="dns-direct" and .dns.servers[$google].finalQuery==true
' "$TROOT/etc/xray/configs/02_dns.json" >/dev/null || fail 'Split DNS does not mirror first-match domain routing order'
[ "$(hash_in)" = "$HIN" ] || fail 'split changed non-DNS inbounds'
[ "$(hash_out)" = "$HOUT" ] || fail 'split changed VPN/non-DNS outbounds'
[ "$(hash_route)" = "$HROUTE" ] || fail 'split changed non-DNS routing'

# Reproduce v0.2.52 defect: native assignments may remain active in an otherwise
# valid Split topology. Plan must classify it repair-ready, and apply must detach
# the exact saved assignments without touching their profile definitions.
state_set NDM_DNS_ASSIGNMENTS on
run_network plan > "$TMP/split-assignments.plan"
grep -Fq 'DNS_ROUTING_MODE=split-assignments' "$TMP/split-assignments.plan" || fail 'Split assignments bypass not classified'
run_network apply > "$TMP/split-assignments.repair" 2>&1 || { cat "$TMP/split-assignments.repair" >&2; fail 'Split assignments repair failed'; }
grep -q '^NDM_DNS_ASSIGNMENTS=off$' "$STATE" || fail 'Split repair did not detach native assignments'
grep -Fq 'RESULT=SUCCESS' "$TMP/split-assignments.repair" || fail 'Split assignment repair result missing'

# Split -> native restores the exact native engine, intercept and filter assignments.
sed -i 's/^DNS_MODE=.*/DNS_MODE=firmware/' "$TROOT/etc/freenet/freenet.conf"
run_network apply > "$TMP/restore.apply" 2>&1 || { cat "$TMP/restore.apply" >&2; fail 'split -> native failed'; }
grep -q '^NDM_DNS_OVERRIDE=off$' "$STATE" || fail 'NDM override not disabled'
grep -q '^NDM_FILTER_ENGINE=public$' "$STATE" || fail 'native filter engine not restored'
grep -q '^NDM_DNS_INTERCEPT=on$' "$STATE" || fail 'native intercept state not restored'
grep -q '^NDM_DNS_ASSIGNMENTS=on$' "$STATE" || fail 'native filter assignments not restored'
grep -q '^PORT53_OWNER=ndnproxy$' "$STATE" || fail 'ndnproxy did not regain :53'
[ "$(sha256sum "$TROOT/etc/xray/configs/02_dns.json" | awk '{print $1}')" = "$H02" ] || fail 'native 02 not restored byte-for-byte'
jq -e '([.inbounds[]? | select(((.port // "")|tostring)=="53")] | length)==0' "$TROOT/etc/xray/configs/03_inbounds.json" >/dev/null || fail 'native still has Xray :53'
jq -e '([.outbounds[]? | select(.tag=="dns-out")] | length)==0' "$TROOT/etc/xray/configs/04_outbounds.json" >/dev/null || fail 'native still has dns-out'
[ "$(hash_in)" = "$HIN" ] || fail 'native changed non-DNS inbounds'
[ "$(hash_out)" = "$HOUT" ] || fail 'native changed VPN/non-DNS outbounds'
[ "$(hash_route)" = "$HROUTE" ] || fail 'native changed non-DNS routing'

# A genuine protected NDM drift must still STOP. The controller cannot safely guess/replay
# user DNS profile definitions, so rollback state remains UNKNOWN until facts are established.
sed -i 's/^DNS_MODE=.*/DNS_MODE=xkeen/' "$TROOT/etc/freenet/freenet.conf"
state_set NDM_MUTATE_PROTECTED_ON_OVERRIDE yes
if run_network apply > "$TMP/protected-drift.apply" 2>&1; then fail 'protected NDM drift unexpectedly succeeded'; fi
grep -Fq 'PRIMARY ERROR: Keenetic protected DNS/WAN state changed unexpectedly' "$TMP/protected-drift.apply" || fail 'protected NDM drift was not detected'
grep -Fq 'ROLLBACK ERROR/STATE: FAILED/UNKNOWN' "$TMP/protected-drift.apply" || fail 'protected NDM drift must stop as unknown when protected state cannot be restored'
# Runtime rollback reaches the original topology before protected verification fails.
grep -q '^NDM_DNS_OVERRIDE=off$' "$STATE" || fail 'protected drift rollback did not restore NDM override'
grep -q '^NDM_FILTER_ENGINE=public$' "$STATE" || fail 'protected drift rollback did not restore filter engine'
grep -q '^NDM_DNS_INTERCEPT=on$' "$STATE" || fail 'protected drift rollback did not restore intercept'
grep -q '^PORT53_OWNER=ndnproxy$' "$STATE" || fail 'protected drift rollback did not restore :53 owner'
state_set NDM_MUTATE_PROTECTED_ON_OVERRIDE no
state_set NDM_DNS_PROFILE_MARKER preserved

# Failure after live mutation must restore exact native state, engine, intercept and DNS.
for n in 02_dns.json 03_inbounds.json 04_outbounds.json 05_routing.json; do cp "$TROOT/etc/xray/configs/$n" "$TMP/$n.before"; done
state_set NDM_SAVE_RESULT fail
if run_network apply > "$TMP/fail.apply" 2>&1; then fail 'failed NDM save unexpectedly succeeded'; fi
grep -Fq 'PRIMARY ERROR: NDM save failed after acceptance' "$TMP/fail.apply" || fail 'NDM save primary error missing'
grep -Fq 'ROLLBACK ERROR/STATE: rollback success' "$TMP/fail.apply" || fail 'rollback success missing'
grep -q '^NDM_DNS_OVERRIDE=off$' "$STATE" || fail 'rollback did not restore NDM'
grep -q '^NDM_FILTER_ENGINE=public$' "$STATE" || fail 'rollback did not restore filter engine'
grep -q '^NDM_DNS_INTERCEPT=on$' "$STATE" || fail 'rollback did not restore intercept'
grep -q '^PORT53_OWNER=ndnproxy$' "$STATE" || fail 'rollback did not restore :53 owner'
for n in 02_dns.json 03_inbounds.json 04_outbounds.json 05_routing.json; do cmp -s "$TMP/$n.before" "$TROOT/etc/xray/configs/$n" || fail "rollback changed $n"; done
state_set NDM_SAVE_RESULT success

# v0.2.43 repair path: Split topology can already own :53 but still have native System
# interception enabled. A read-only plan must expose it as split-intercept so UI does not
# call it active, and Apply must repair in place without touching protected profiles/assignments.
run_network apply > "$TMP/legacy-seed.apply" 2>&1 || { cat "$TMP/legacy-seed.apply" >&2; fail 'cannot seed Split before legacy repair'; }
state_set NDM_DNS_INTERCEPT on
rm -f "$TROOT/etc/freenet/native-dns/intercept.native"
# Emulate old flat Split DNS state whose DIRECT union ignored first-match overlaps.
cat > "$TROOT/etc/xray/configs/02_dns.json" <<'EOF'
{"dns":{"tag":"dns-vless","servers":[{"address":"77.88.8.8","port":53,"domains":["ext:geosite.dat:google"],"skipFallback":true,"tag":"dns-direct"},{"address":"https://8.8.8.8/dns-query","tag":"dns-vless"}],"queryStrategy":"UseIPv4"}}
EOF
run_network plan > "$TMP/legacy-repair.plan"
grep -Fq 'DNS_ROUTING_MODE=split-intercept' "$TMP/legacy-repair.plan" || fail 'legacy Split with native intercept was incorrectly marked healthy'
run_network apply > "$TMP/legacy-repair.apply" 2>&1 || { cat "$TMP/legacy-repair.apply" >&2; fail 'legacy Split repair failed'; }
grep -q '^NDM_DNS_OVERRIDE=on$' "$STATE" || fail 'legacy repair changed override'
grep -q '^NDM_FILTER_ENGINE=opkg$' "$STATE" || fail 'legacy repair changed filter engine'
grep -q '^NDM_DNS_INTERCEPT=off$' "$STATE" || fail 'legacy repair did not suppress System intercept'
grep -q '^PORT53_OWNER=xray$' "$STATE" || fail 'legacy repair lost Xray :53'
[ "$(cat "$TROOT/etc/freenet/native-dns/intercept.native")" = on ] || fail 'legacy repair did not adopt observed native intercept baseline'
grep -q '^NDM_DNS_PROFILE_MARKER=preserved$' "$STATE" || fail 'legacy repair changed protected DNS profile state'
jq -e '
  ([.dns.servers | to_entries[] | select((.value.domains // []) | index("ext:geosite.dat:youtube")) | .key][0]) as $youtube |
  ([.dns.servers | to_entries[] | select((.value.domains // []) | index("ext:geosite.dat:google")) | .key][0]) as $google |
  $youtube < $google and .dns.servers[$youtube].tag=="dns-vless" and .dns.servers[$youtube].finalQuery==true and .dns.servers[$google].tag=="dns-direct"
' "$TROOT/etc/xray/configs/02_dns.json" >/dev/null || fail 'legacy repair did not rebuild ordered first-match DNS policy'

# Return the repaired Split to exact native to prove the adopted intercept snapshot is reversible.
sed -i 's/^DNS_MODE=.*/DNS_MODE=firmware/' "$TROOT/etc/freenet/freenet.conf"
run_network apply > "$TMP/legacy-restore.apply" 2>&1 || { cat "$TMP/legacy-restore.apply" >&2; fail 'repaired Split -> native failed'; }
grep -q '^NDM_DNS_INTERCEPT=on$' "$STATE" || fail 'repaired Split did not restore native intercept'
grep -q '^NDM_DNS_OVERRIDE=off$' "$STATE" || fail 'repaired Split did not restore native override'
grep -q '^PORT53_OWNER=ndnproxy$' "$STATE" || fail 'repaired Split did not restore native :53 owner'

state_set PORT53_OWNER none
if run_network apply > "$TMP/partial.apply" 2>&1; then fail 'partial topology unexpectedly succeeded'; fi
if ! grep -Eq 'PRIMARY ERROR: (native mode должен иметь ndnproxy|native DNS preflight failed before mutation|partial/unknown DNS topology: native restore ожидает opkg dns-override=on)' "$TMP/partial.apply"; then
    cat "$TMP/partial.apply" >&2
    fail 'partial topology STOP missing'
fi
grep -Fq 'ROLLBACK ERROR/STATE: no live apply' "$TMP/partial.apply" || fail 'partial preflight must report no live apply'
grep -q '^NDM_DNS_OVERRIDE=off$' "$STATE" || fail 'partial preflight mutated NDM'
grep -q '^NDM_FILTER_ENGINE=public$' "$STATE" || fail 'partial preflight changed filter engine'
grep -q '^NDM_DNS_INTERCEPT=on$' "$STATE" || fail 'partial preflight changed intercept'

echo 'network profile contract PASS'
