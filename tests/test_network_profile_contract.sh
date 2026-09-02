#!/bin/sh

set -eu

ROOT_DIR="$(CDPATH= cd "$(dirname "$0")/.." && pwd)"
SCRIPT="$ROOT_DIR/scripts/apply_network_profile.sh"
MIGRATE="$ROOT_DIR/scripts/migrate_split_dns.sh"

fail() { echo "network profile contract FAIL: $*" >&2; exit 1; }

sh -n "$SCRIPT"
sh -n "$MIGRATE"

grep -Fq 'EFFECTIVE_DNS=firmware' "$SCRIPT" || fail 'standard DNS mode missing'
grep -Fq 'EFFECTIVE_DNS=xkeen' "$SCRIPT" || fail 'explicit split DNS mode missing'
grep -Fq "REASON='обычный DNS без VPN-проксирования — безопасный режим по умолчанию'" "$SCRIPT" || fail 'safe default reason missing'
grep -Fq 'set_proxy_dns_state()' "$SCRIPT" || fail 'non-interactive proxy_dns controller missing'
grep -Fq 'run_bounded()' "$SCRIPT" || fail 'bounded runtime helper missing'
grep -Fq 'xkeen_runtime()' "$SCRIPT" || fail 'init-based XKeen runtime helper missing'
grep -Fq 'xkeen-init.before' "$SCRIPT" || fail 'XKeen init backup missing'
grep -Fq 'ndm_dns_redirect_port()' "$SCRIPT" || fail 'NDM DNS redirect parser missing'
grep -Fq 'iptables-save' "$SCRIPT" || fail 'NDM redirect inspection missing'
grep -Fq 'XRAY_DNS_INBOUND_COUNT=' "$SCRIPT" || fail 'Xray DNS backend fact missing'
grep -Fq 'DNS_ROUTING_MODE=' "$SCRIPT" || fail 'DNS routing mode fact missing'
grep -Fq 'BACKEND_PASS_CLIENT_REQUIRED' "$SCRIPT" || fail 'backend/client acceptance split missing'
grep -Fq 'CLIENT_ACCEPTANCE=REQUIRED' "$SCRIPT" || fail 'client acceptance marker missing'
grep -Fq 'RESULT=ROUTER_SIDE_PASS' "$SCRIPT" || fail 'router-side result marker missing'
grep -Fq '"inboundTag":["dns-vless"],"outboundTag":"direct"' "$SCRIPT" || fail 'standard dns-vless direct route missing'
grep -Fq '"inboundTag":["dns-direct"],"outboundTag":"direct"' "$SCRIPT" || fail 'standard dns-direct route missing'
grep -Fq '"port":53,"outboundTag":"dns-out"' "$SCRIPT" || fail 'dns-out route missing'
grep -Fq 'expected 11111' "$SCRIPT" || fail 'Xray GID acceptance missing'
grep -Fq 'DNS_READINESS=PASS' "$SCRIPT" || fail 'DNS readiness result marker missing'
grep -Fq 'DNS_ATTEMPTS_USED=' "$SCRIPT" || fail 'DNS readiness attempt diagnostics missing'
grep -Fq 'fail-once)' "$SCRIPT" || fail 'transient DNS test state missing'
grep -Fq 'MUTATION=NONE' "$SCRIPT" || fail 'read-only plan marker missing'
grep -Fq 'keep proxy_dns=off' "$SCRIPT" || fail 'Split plan must keep XKeen DNS interception off'
grep -Fq 'ROLLBACK ERROR/STATE: FAILED/UNKNOWN' "$SCRIPT" || fail 'rollback unknown state missing'
grep -Fq 'restart_xkeen()' "$MIGRATE" || fail 'Split DNS migration bounded init restart missing'
grep -Fq 'run_bounded()' "$MIGRATE" || fail 'Split DNS migration runtime timeout missing'
grep -Fq 'standard-backend' "$MIGRATE" || fail 'Split migration must support Xray local DNS backend'
grep -Fq 'proxy_dns должен оставаться off' "$MIGRATE" || fail 'Split migration must require proxy_dns off'

if grep -Eq '\$XKEEN_BIN"[[:space:]]+-(dns|start|stop|restart)' "$SCRIPT" "$MIGRATE"; then
    fail 'interactive XKeen CLI runtime call must not be used'
fi
grep -Fq '/opt/etc/init.d/S99xkeen /opt/etc/init.d/S05xkeen' "$MIGRATE" || fail 'migration must support S99/S05 init paths'
if grep -Ei 'subscription.*url=|uuid=|publicKey|shortId|vless://' "$SCRIPT" >/dev/null; then fail 'network controller contains secret material'; fi

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT INT TERM
TROOT="$TMP/opt"
STATE="$TMP/runtime.state"
mkdir -p "$TROOT/etc/freenet" "$TROOT/etc/xray/configs" "$TROOT/etc/xray/dat" "$TROOT/etc/init.d" "$TROOT/sbin" "$TROOT/lib/freenet" "$TROOT/backups"

cat > "$TROOT/etc/freenet/freenet.conf" <<'EOF_CONF'
ISP_ID=vladlink
DNS_MODE=auto
EOF_CONF
cat > "$TROOT/etc/init.d/S05xkeen" <<'EOF_INIT'
#!/bin/sh
proxy_dns="off"
EOF_INIT

write_broken_v0211_state() {
    cat > "$TROOT/etc/xray/configs/02_dns.json" <<'EOF_DNS'
{"dns":{"tag":"dns-vless","servers":[{"address":"77.88.8.8","tag":"dns-direct","domains":["domain:example.ru"],"skipFallback":true},{"address":"8.8.8.8","tag":"dns-vless"}],"queryStrategy":"UseIPv4"}}
EOF_DNS
    cat > "$TROOT/etc/xray/configs/03_inbounds.json" <<'EOF_IN'
{"inbounds":[{"tag":"redirect","port":5000,"protocol":"dokodemo-door"},{"tag":"tproxy","port":5000,"protocol":"dokodemo-door"}]}
EOF_IN
    cat > "$TROOT/etc/xray/configs/04_outbounds.json" <<'EOF_OUT'
{"outbounds":[{"tag":"vless-reality","protocol":"freedom","settings":{"marker":"keep"}},{"tag":"direct","protocol":"freedom"},{"tag":"block","protocol":"blackhole"},{"tag":"dns-out","protocol":"dns"}]}
EOF_OUT
    cat > "$TROOT/etc/xray/configs/05_routing.json" <<'EOF_ROUTE'
{"routing":{"rules":[{"type":"field","domain":["domain:example.ru"],"outboundTag":"direct"},{"type":"field","domain":["ext:geosite.dat:ru-blocked"],"outboundTag":"vless-reality"},{"type":"field","network":"tcp,udp","outboundTag":"vless-reality"}]}}
EOF_ROUTE
    sed -i 's/^proxy_dns=.*/proxy_dns="off"/' "$TROOT/etc/init.d/S05xkeen"
    cat > "$STATE" <<'EOF_STATE'
PORT53_OWNER=none
FIRMWARE_DNS_PATH=redirect:41100
XRAY_RUNNING=yes
XRAY_GID=11111
DNS_QUERY_OK=yes
XKEEN_ACTION_RESULT=success
EOF_STATE
}

write_legacy_split_state() {
    cat > "$TROOT/etc/xray/configs/02_dns.json" <<'EOF_DNS'
{"dns":{"tag":"dns-vless","servers":[{"address":"77.88.8.8","tag":"dns-direct","domains":["domain:example.ru"],"skipFallback":true},{"address":"8.8.8.8","tag":"dns-vless"}],"queryStrategy":"UseIPv4"}}
EOF_DNS
    cat > "$TROOT/etc/xray/configs/03_inbounds.json" <<'EOF_IN'
{"inbounds":[{"tag":"dns","port":53,"protocol":"dokodemo-door","settings":{"network":"tcp,udp"}},{"tag":"redirect","port":5000,"protocol":"dokodemo-door"},{"tag":"tproxy","port":5000,"protocol":"dokodemo-door"}]}
EOF_IN
    cat > "$TROOT/etc/xray/configs/04_outbounds.json" <<'EOF_OUT'
{"outbounds":[{"tag":"vless-reality","protocol":"freedom","settings":{"marker":"keep"}},{"tag":"direct","protocol":"freedom"},{"tag":"block","protocol":"blackhole"}]}
EOF_OUT
    cat > "$TROOT/etc/xray/configs/05_routing.json" <<'EOF_ROUTE'
{"routing":{"rules":[{"type":"field","inboundTag":["dns-vless"],"outboundTag":"vless-reality"},{"type":"field","inboundTag":["dns-direct"],"outboundTag":"direct"},{"type":"field","port":53,"outboundTag":"dns-out"},{"type":"field","domain":["domain:example.ru"],"outboundTag":"direct"},{"type":"field","network":"tcp,udp","outboundTag":"vless-reality"}]}}
EOF_ROUTE
    sed -i 's/^proxy_dns=.*/proxy_dns="on"/' "$TROOT/etc/init.d/S05xkeen"
    cat > "$STATE" <<'EOF_STATE'
PORT53_OWNER=xray
FIRMWARE_DNS_PATH=redirect:41100
XRAY_RUNNING=yes
XRAY_GID=11111
DNS_QUERY_OK=yes
XKEEN_ACTION_RESULT=success
EOF_STATE
}

state_set() {
    key="$1"; value="$2"; tmp="$STATE.tmp.$$"
    grep -v "^${key}=" "$STATE" > "$tmp" 2>/dev/null || true
    echo "${key}=${value}" >> "$tmp"
    mv "$tmp" "$STATE"
}

cat > "$TROOT/sbin/xkeen" <<'EOF_XKEEN'
#!/bin/sh
echo "unexpected XKeen CLI invocation: $*" >> "$FREENET_ROOT/xkeen-cli.called"
exit 99
EOF_XKEEN
chmod 755 "$TROOT/sbin/xkeen"
cat > "$TROOT/sbin/xray" <<'EOF_XRAY'
#!/bin/sh
exit 0
EOF_XRAY
chmod 755 "$TROOT/sbin/xray"

cat > "$TROOT/lib/freenet/migrate_split_dns.sh" <<'EOF_MIG'
#!/bin/sh
case "${FREENET_TEST_MIGRATE_RESULT:-success}" in
    success)
        tmp="$FREENET_CONFIG_DIR/05_routing.json.tmp.$$"
        jq '.routing.rules = ([
          {"type":"field","inboundTag":["dns-vless"],"outboundTag":"vless-reality"},
          {"type":"field","inboundTag":["dns-direct"],"outboundTag":"direct"},
          {"type":"field","port":53,"outboundTag":"dns-out"}
        ] + [.routing.rules[]?
          | select((.outboundTag // "") != "dns-out")
          | select(((.inboundTag // []) | index("dns-vless")) == null)
          | select(((.inboundTag // []) | index("dns-direct")) == null)
          | select((((.port // "") | tostring)) != "53")])' "$FREENET_CONFIG_DIR/05_routing.json" > "$tmp" || exit 1
        mv "$tmp" "$FREENET_CONFIG_DIR/05_routing.json"
        tmp="$FREENET_CONFIG_DIR/04_outbounds.json.tmp.$$"
        jq 'if any(.outbounds[]?; .tag == "dns-out") then . else .outbounds += [{"tag":"dns-out","protocol":"dns"}] end' "$FREENET_CONFIG_DIR/04_outbounds.json" > "$tmp" || exit 1
        mv "$tmp" "$FREENET_CONFIG_DIR/04_outbounds.json"
        exit 0
        ;;
    fail) exit 1 ;;
    unknown) exit 2 ;;
    *) exit 1 ;;
esac
EOF_MIG
chmod 755 "$TROOT/lib/freenet/migrate_split_dns.sh"

run_network() {
    FREENET_ROOT="$TROOT" \
    FREENET_BACKUP_ROOT="$TROOT/backups" \
    FREENET_CONFIG_FILE="$TROOT/etc/freenet/freenet.conf" \
    FREENET_CONFIG_DIR="$TROOT/etc/xray/configs" \
    FREENET_XRAY_ASSET_DIR="$TROOT/etc/xray/dat" \
    FREENET_XKEEN_BIN="$TROOT/sbin/xkeen" \
    FREENET_XRAY_BIN="$TROOT/sbin/xray" \
    FREENET_MIGRATE_SCRIPT="$TROOT/lib/freenet/migrate_split_dns.sh" \
    FREENET_XKEEN_RUNTIME_TIMEOUT=2 \
    FREENET_NETWORK_TEST_MODE=yes \
    FREENET_NETWORK_TEST_STATE="$STATE" \
    sh "$SCRIPT" "$@"
}

# v0.2.11 broken HOME: NDM redirect exists, Xray DNS backend was removed.
write_broken_v0211_state
run_network plan > "$TMP/broken.plan"
grep -Fq 'EFFECTIVE_DNS_MODE=firmware' "$TMP/broken.plan" || fail 'auto must resolve to standard DNS'
grep -Fq 'PORT53_OWNER=none' "$TMP/broken.plan" || fail 'broken state must expose no direct :53 owner'
grep -Fq 'FIRMWARE_DNS_PATH=redirect:41100' "$TMP/broken.plan" || fail 'redirect path missing'
grep -Fq 'XRAY_DNS_INBOUND_COUNT=0' "$TMP/broken.plan" || fail 'missing Xray DNS backend not reported'

VLESS_BEFORE="$(jq -cS '[.outbounds[]? | select(.tag=="vless-reality")]' "$TROOT/etc/xray/configs/04_outbounds.json" | sha256sum | awk '{print $1}')"
NON_DNS_BEFORE="$(jq -cS '[.routing.rules[]? | select((.outboundTag // "") != "dns-out") | select(((.inboundTag // []) | index("dns-vless")) == null) | select(((.inboundTag // []) | index("dns-direct")) == null) | select((((.port // "") | tostring)) != "53")]' "$TROOT/etc/xray/configs/05_routing.json" | sha256sum | awk '{print $1}')"
run_network apply > "$TMP/standard.apply" 2>&1 || fail 'v0.2.11 HOME repair should reach router-side pass'
grep -Fq '[FreeNet Network] RESULT=ROUTER_SIDE_PASS' "$TMP/standard.apply" || fail 'redirect path must not claim full success'
grep -Fq 'DNS_QUERY=BACKEND_PASS_CLIENT_REQUIRED' "$TMP/standard.apply" || fail 'backend query marker missing'
grep -Fq 'CLIENT_ACCEPTANCE=REQUIRED' "$TMP/standard.apply" || fail 'client acceptance must be required'
grep -Fq 'proxy_dns="off"' "$TROOT/etc/init.d/S05xkeen" || fail 'standard must keep proxy_dns off'
grep -Fq 'PORT53_OWNER=xray' "$STATE" || fail 'standard redirect must restore Xray local :53 backend'
jq -e '([.inbounds[]? | select(((.port // "")|tostring)=="53" and .tag=="dns" and .protocol=="dokodemo-door" and .settings.network=="tcp,udp")] | length)==1' "$TROOT/etc/xray/configs/03_inbounds.json" >/dev/null || fail 'canonical Xray DNS backend missing'
jq -e '([.outbounds[]? | select(.tag=="dns-out" and .protocol=="dns")] | length)==1' "$TROOT/etc/xray/configs/04_outbounds.json" >/dev/null || fail 'dns-out missing'
jq -e '([.routing.rules[]? | select(((.inboundTag // [])|index("dns-vless"))!=null and .outboundTag=="direct")] | length)==1' "$TROOT/etc/xray/configs/05_routing.json" >/dev/null || fail 'dns-vless must be direct in standard mode'
jq -e '([.routing.rules[]? | select(((.inboundTag // [])|index("dns-direct"))!=null and .outboundTag=="direct")] | length)==1' "$TROOT/etc/xray/configs/05_routing.json" >/dev/null || fail 'dns-direct must be direct'
VLESS_AFTER="$(jq -cS '[.outbounds[]? | select(.tag=="vless-reality")]' "$TROOT/etc/xray/configs/04_outbounds.json" | sha256sum | awk '{print $1}')"
NON_DNS_AFTER="$(jq -cS '[.routing.rules[]? | select((.outboundTag // "") != "dns-out") | select(((.inboundTag // []) | index("dns-vless")) == null) | select(((.inboundTag // []) | index("dns-direct")) == null) | select((((.port // "") | tostring)) != "53")]' "$TROOT/etc/xray/configs/05_routing.json" | sha256sum | awk '{print $1}')"
[ "$VLESS_BEFORE" = "$VLESS_AFTER" ] || fail 'VLESS changed'
[ "$NON_DNS_BEFORE" = "$NON_DNS_AFTER" ] || fail 'non-DNS routing changed'
[ ! -e "$TROOT/xkeen-cli.called" ] || fail 'interactive XKeen CLI invoked'

# v0.2.9 timeout regression: candidate mutation must roll back all JSON + proxy_dns.
write_legacy_split_state
cp "$TROOT/etc/xray/configs/03_inbounds.json" "$TMP/in.before"
cp "$TROOT/etc/xray/configs/04_outbounds.json" "$TMP/out.before"
cp "$TROOT/etc/xray/configs/05_routing.json" "$TMP/route.before"
state_set XKEEN_ACTION_RESULT timeout-once
if run_network apply > "$TMP/timeout.fail" 2>&1; then fail 'restart timeout unexpectedly succeeded'; fi
grep -Fq 'PRIMARY ERROR: XKeen init restart' "$TMP/timeout.fail" || fail 'restart timeout primary error missing'
grep -Fq 'ROLLBACK ERROR/STATE: rollback success' "$TMP/timeout.fail" || fail 'timeout rollback result missing'
grep -Fq 'proxy_dns="on"' "$TROOT/etc/init.d/S05xkeen" || fail 'rollback must restore proxy_dns on'
cmp -s "$TMP/in.before" "$TROOT/etc/xray/configs/03_inbounds.json" || fail 'rollback must restore inbound'
cmp -s "$TMP/out.before" "$TROOT/etc/xray/configs/04_outbounds.json" || fail 'rollback must restore outbounds'
cmp -s "$TMP/route.before" "$TROOT/etc/xray/configs/05_routing.json" || fail 'rollback must restore routing'
grep -Fq 'PORT53_OWNER=xray' "$STATE" || fail 'rollback must restore xray owner'
[ ! -e "$TROOT/xkeen-cli.called" ] || fail 'interactive CLI invoked during rollback'

# Successful standard conversion of legacy split DNS keeps :53 but sends both DNS tags direct.
state_set XKEEN_ACTION_RESULT success
run_network apply > "$TMP/legacy.standard.apply" 2>&1 || fail 'legacy split to standard should succeed router-side'
grep -Fq 'RESULT=ROUTER_SIDE_PASS' "$TMP/legacy.standard.apply" || fail 'legacy redirect must require client acceptance'
grep -Fq 'proxy_dns="off"' "$TROOT/etc/init.d/S05xkeen" || fail 'standard must set proxy_dns off'
grep -Fq 'PORT53_OWNER=xray' "$STATE" || fail 'standard must retain Xray backend owner'
jq -e '([.routing.rules[]? | select(((.inboundTag // [])|index("dns-vless"))!=null and .outboundTag=="vless-reality")] | length)==0' "$TROOT/etc/xray/configs/05_routing.json" >/dev/null || fail 'standard must remove DNS via VLESS'

# Split conversion from standard: first DNS readiness attempt may fail while Xray/VLESS warms up.
sed -i 's/^DNS_MODE=.*/DNS_MODE=xkeen/' "$TROOT/etc/freenet/freenet.conf"
run_network plan > "$TMP/split.plan"
grep -Fq 'PORT53_OWNER=xray' "$TMP/split.plan" || fail 'split plan must keep Xray backend'
grep -Fq 'keep proxy_dns=off' "$TMP/split.plan" || fail 'split plan must preserve Keenetic DNS path'
state_set DNS_QUERY_OK fail-once
FREENET_TEST_MIGRATE_RESULT=success run_network apply > "$TMP/split.apply" 2>&1 || fail 'split apply should survive transient DNS readiness failure'
grep -Fq '[FreeNet Network] RESULT=ROUTER_SIDE_PASS' "$TMP/split.apply" || fail 'split redirect must require client acceptance'
grep -Fq 'DNS_ROUTING_MODE=split' "$TMP/split.apply" || fail 'split routing result missing'
grep -Fq 'PROXY_DNS=off' "$TMP/split.apply" || fail 'split result must report proxy_dns off'
grep -Fq 'ROLLBACK=NOT_NEEDED' "$TMP/split.apply" || fail 'transient DNS failure must not trigger rollback'
grep -Fq 'proxy_dns="off"' "$TROOT/etc/init.d/S05xkeen" || fail 'split must keep proxy_dns off'
grep -Fq 'PORT53_OWNER=xray' "$STATE" || fail 'split must keep Xray backend owner'
grep -Fq 'DNS_QUERY_OK=yes' "$STATE" || fail 'transient DNS state must advance to success'
jq -e '([.routing.rules[]? | select(((.inboundTag // [])|index("dns-vless"))!=null and .outboundTag=="vless-reality")] | length)==1' "$TROOT/etc/xray/configs/05_routing.json" >/dev/null || fail 'split dns-vless route missing'
jq -e '([.inbounds[]? | select(((.port // "")|tostring)=="53")] | length)==1' "$TROOT/etc/xray/configs/03_inbounds.json" >/dev/null || fail 'split changed local DNS backend'

# Legacy Split state with proxy_dns=on must be normalized to off without changing the local DNS backend.
write_legacy_split_state
sed -i 's/^DNS_MODE=.*/DNS_MODE=xkeen/' "$TROOT/etc/freenet/freenet.conf"
state_set DNS_QUERY_OK yes
FREENET_TEST_MIGRATE_RESULT=success run_network apply > "$TMP/legacy-split.apply" 2>&1 || fail 'legacy split normalization should succeed'
grep -Fq 'proxy_dns="off"' "$TROOT/etc/init.d/S05xkeen" || fail 'legacy split normalization must disable XKeen DNS interception'
grep -Fq 'DNS_ROUTING_MODE=split' "$TMP/legacy-split.apply" || fail 'legacy split normalization must remain split'
grep -Fq 'PORT53_OWNER=xray' "$STATE" || fail 'legacy split normalization must keep Xray :53 backend'

# Persistent post-apply DNS readiness failure must still fail and restore the standard state.
sed -i 's/^DNS_MODE=.*/DNS_MODE=firmware/' "$TROOT/etc/freenet/freenet.conf"
state_set DNS_QUERY_OK yes
run_network apply >/dev/null 2>&1 || fail 'reset to standard before persistent DNS failure test failed'
sed -i 's/^DNS_MODE=.*/DNS_MODE=xkeen/' "$TROOT/etc/freenet/freenet.conf"
cp "$TROOT/etc/xray/configs/02_dns.json" "$TMP/dns-readiness.02.before"
cp "$TROOT/etc/xray/configs/03_inbounds.json" "$TMP/dns-readiness.03.before"
cp "$TROOT/etc/xray/configs/04_outbounds.json" "$TMP/dns-readiness.04.before"
cp "$TROOT/etc/xray/configs/05_routing.json" "$TMP/dns-readiness.05.before"
cp "$TROOT/etc/init.d/S05xkeen" "$TMP/dns-readiness.init.before"
state_set DNS_QUERY_OK no
if FREENET_TEST_MIGRATE_RESULT=success run_network apply > "$TMP/dns-readiness.fail" 2>&1; then fail 'persistent DNS readiness failure unexpectedly succeeded'; fi
grep -Fq 'PRIMARY ERROR: post-apply Split DNS acceptance failed' "$TMP/dns-readiness.fail" || fail 'persistent DNS readiness primary error missing'
grep -Fq 'ROLLBACK ERROR/STATE: rollback success' "$TMP/dns-readiness.fail" || fail 'persistent DNS readiness rollback result missing'
grep -Fq 'proxy_dns="off"' "$TROOT/etc/init.d/S05xkeen" || fail 'persistent DNS readiness rollback must restore proxy_dns off'
cmp -s "$TMP/dns-readiness.02.before" "$TROOT/etc/xray/configs/02_dns.json" || fail 'persistent DNS readiness rollback must restore DNS config'
cmp -s "$TMP/dns-readiness.03.before" "$TROOT/etc/xray/configs/03_inbounds.json" || fail 'persistent DNS readiness rollback must restore inbounds'
cmp -s "$TMP/dns-readiness.04.before" "$TROOT/etc/xray/configs/04_outbounds.json" || fail 'persistent DNS readiness rollback must restore outbounds'
cmp -s "$TMP/dns-readiness.05.before" "$TROOT/etc/xray/configs/05_routing.json" || fail 'persistent DNS readiness rollback must restore routing'
cmp -s "$TMP/dns-readiness.init.before" "$TROOT/etc/init.d/S05xkeen" || fail 'persistent DNS readiness rollback must restore XKeen init'
state_set DNS_QUERY_OK yes

# Migration failure must restore standard mode and proxy_dns=off.
sed -i 's/^DNS_MODE=.*/DNS_MODE=firmware/' "$TROOT/etc/freenet/freenet.conf"
run_network apply >/dev/null 2>&1 || fail 'reset to standard failed'
sed -i 's/^DNS_MODE=.*/DNS_MODE=xkeen/' "$TROOT/etc/freenet/freenet.conf"
cp "$TROOT/etc/xray/configs/05_routing.json" "$TMP/std.route.before"
if FREENET_TEST_MIGRATE_RESULT=fail run_network apply > "$TMP/migrate.fail" 2>&1; then fail 'simulated migration failure unexpectedly succeeded'; fi
grep -Fq 'ROLLBACK ERROR/STATE: rollback success/no live apply' "$TMP/migrate.fail" || fail 'split failure rollback missing'
grep -Fq 'proxy_dns="off"' "$TROOT/etc/init.d/S05xkeen" || fail 'split failure must restore proxy_dns off'
cmp -s "$TMP/std.route.before" "$TROOT/etc/xray/configs/05_routing.json" || fail 'split failure must restore routing'

# Invalid NDM topology must stop before mutation.
sed -i 's/^DNS_MODE=.*/DNS_MODE=firmware/' "$TROOT/etc/freenet/freenet.conf"
state_set FIRMWARE_DNS_PATH none
state_set PORT53_OWNER none
if run_network apply > "$TMP/invalid.fail" 2>&1; then fail 'invalid DNS topology unexpectedly succeeded'; fi
grep -Fq 'невалидный Keenetic DNS path: none' "$TMP/invalid.fail" || fail 'invalid topology blocker missing'

echo 'network profile contract PASS'