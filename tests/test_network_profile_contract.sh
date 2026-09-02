#!/bin/sh

set -eu

ROOT_DIR="$(CDPATH= cd "$(dirname "$0")/.." && pwd)"
SCRIPT="$ROOT_DIR/scripts/apply_network_profile.sh"
MIGRATE="$ROOT_DIR/scripts/migrate_split_dns.sh"

fail() { echo "network profile contract FAIL: $*" >&2; exit 1; }

sh -n "$SCRIPT"
sh -n "$MIGRATE"

grep -Fq 'EFFECTIVE_DNS=firmware' "$SCRIPT" || fail 'safe firmware DNS mode missing'
grep -Fq 'EFFECTIVE_DNS=xkeen' "$SCRIPT" || fail 'explicit split DNS mode missing'
grep -Fq "REASON='штатный DNS Keenetic — безопасный режим по умолчанию'" "$SCRIPT" || fail 'safe default reason missing'
grep -Fq 'set_proxy_dns_state()' "$SCRIPT" || fail 'non-interactive proxy_dns controller missing'
grep -Fq 'run_bounded()' "$SCRIPT" || fail 'bounded runtime helper missing'
grep -Fq 'xkeen_runtime()' "$SCRIPT" || fail 'init-based XKeen runtime helper missing'
grep -Fq 'xkeen-init.before' "$SCRIPT" || fail 'XKeen init backup missing'
grep -Fq 'firmware_dns_path()' "$SCRIPT" || fail 'Keenetic firmware DNS path detector missing'
grep -Fq 'ndm_dns_redirect_port()' "$SCRIPT" || fail 'NDM DNS redirect parser missing'
grep -Fq 'iptables-save' "$SCRIPT" || fail 'NDM redirect inspection missing'
grep -Fq 'FIRMWARE_DNS_PATH=' "$SCRIPT" || fail 'firmware DNS path reporting missing'
grep -Fq 'DNS_QUERY_RESULT=CLIENT_REQUIRED' "$SCRIPT" || fail 'redirect client acceptance marker missing'
grep -Fq 'expected 11111' "$SCRIPT" || fail 'Xray GID acceptance missing'
grep -Fq 'MUTATION=NONE' "$SCRIPT" || fail 'read-only plan marker missing'
grep -Fq 'Xray уже владеет :53; сначала примените штатный DNS' "$SCRIPT" || fail 'unsafe split-DNS Xray:53 guard missing'
grep -Fq 'post-apply acceptance штатного DNS не пройден' "$SCRIPT" || fail 'standard DNS acceptance missing'
grep -Fq 'post-apply Split DNS acceptance failed' "$SCRIPT" || fail 'split DNS acceptance missing'
grep -Fq 'ROLLBACK ERROR/STATE: FAILED/UNKNOWN' "$SCRIPT" || fail 'rollback unknown state missing'
grep -Fq 'RESULT=SUCCESS' "$SCRIPT" || fail 'success marker missing'
grep -Fq 'restart_xkeen()' "$MIGRATE" || fail 'Split DNS migration bounded init restart missing'
grep -Fq 'run_bounded()' "$MIGRATE" || fail 'Split DNS migration runtime timeout missing'

# Redirect must be discovered from NDM rules, not hardcoded to HOME port 41100.
if grep -Eq '(^|[^0-9])41100([^0-9]|$)' "$SCRIPT"; then
    fail 'Keenetic DNS backend port must not be hardcoded'
fi

# DNS helpers must never call XKeen interactive CLI for DNS/runtime mutation.
if grep -Eq '\$XKEEN_BIN"[[:space:]]+-(dns|start|stop|restart)' "$SCRIPT" "$MIGRATE"; then
    fail 'interactive XKeen CLI runtime call must not be used'
fi

# Existing migration engine must keep supporting both known XKeen init paths.
grep -Fq '/opt/etc/init.d/S99xkeen /opt/etc/init.d/S05xkeen' "$MIGRATE" || fail 'migration must support S99/S05 XKeen init paths'

if grep -Ei 'subscription.*url=|uuid=|publicKey|shortId|vless://' "$SCRIPT" >/dev/null; then
    fail 'network profile controller contains secret material'
fi
if grep -Eq 'eval[[:space:]]+.*(ISP|DNS)|sh[[:space:]]+-c[[:space:]]+.*(ISP|DNS)' "$SCRIPT"; then
    fail 'network profile values must not become shell commands'
fi

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT INT TERM
TROOT="$TMP/opt"
STATE="$TMP/runtime.state"
mkdir -p "$TROOT/etc/freenet" "$TROOT/etc/xray/configs" "$TROOT/etc/xray/dat" "$TROOT/etc/init.d" "$TROOT/sbin" "$TROOT/lib/freenet" "$TROOT/backups"

cat > "$TROOT/etc/freenet/freenet.conf" <<'EOF'
ISP_ID=vladlink
DNS_MODE=auto
EOF
cat > "$TROOT/etc/init.d/S05xkeen" <<'EOF'
#!/bin/sh
proxy_dns="on"
EOF
cat > "$TROOT/etc/xray/configs/02_dns.json" <<'EOF'
{"dns":{"servers":["8.8.8.8"]}}
EOF
cat > "$TROOT/etc/xray/configs/03_inbounds.json" <<'EOF'
{"inbounds":[{"tag":"dns-in","listen":"0.0.0.0","port":53,"protocol":"dokodemo-door"},{"tag":"transparent","port":5000,"protocol":"dokodemo-door"}]}
EOF
cat > "$TROOT/etc/xray/configs/04_outbounds.json" <<'EOF'
{"outbounds":[{"tag":"vless-reality","protocol":"freedom"},{"tag":"direct","protocol":"freedom"},{"tag":"block","protocol":"blackhole"}]}
EOF
cat > "$TROOT/etc/xray/configs/05_routing.json" <<'EOF'
{"routing":{"rules":[{"type":"field","inboundTag":["dns-in"],"outboundTag":"dns-out"},{"type":"field","port":53,"outboundTag":"dns-out"},{"type":"field","domain":["example.org"],"outboundTag":"direct"}]}}
EOF
cat > "$STATE" <<'EOF'
PORT53_OWNER=xray
FIRMWARE_DNS_PATH=redirect:41100
XRAY_RUNNING=yes
XRAY_GID=11111
DNS_QUERY_OK=no
XKEEN_ACTION_RESULT=success
EOF

state_set() {
    key="$1"
    value="$2"
    tmp="$STATE.tmp.$$"
    grep -v "^${key}=" "$STATE" > "$tmp" 2>/dev/null || true
    echo "${key}=${value}" >> "$tmp"
    mv "$tmp" "$STATE"
}

# Deliberately interactive-only stub: any invocation is a regression.
cat > "$TROOT/sbin/xkeen" <<'EOF'
#!/bin/sh
echo "unexpected XKeen CLI invocation: $*" >> "$FREENET_ROOT/xkeen-cli.called"
exit 99
EOF
chmod 755 "$TROOT/sbin/xkeen"

cat > "$TROOT/sbin/xray" <<'EOF'
#!/bin/sh
exit 0
EOF
chmod 755 "$TROOT/sbin/xray"

cat > "$TROOT/lib/freenet/migrate_split_dns.sh" <<'EOF'
#!/bin/sh
case "${FREENET_TEST_MIGRATE_RESULT:-success}" in
    success)
        tmp="$FREENET_CONFIG_DIR/04_outbounds.json.tmp.$$"
        jq 'if any(.outbounds[]?; .tag == "dns-out") then . else .outbounds += [{"tag":"dns-out","protocol":"dns"}] end' \
            "$FREENET_CONFIG_DIR/04_outbounds.json" > "$tmp" || exit 1
        mv "$tmp" "$FREENET_CONFIG_DIR/04_outbounds.json"
        exit 0
        ;;
    fail) exit 1 ;;
    unknown) exit 2 ;;
    *) exit 1 ;;
esac
EOF
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

# HOME exact class: Xray owns :53 while healthy Keenetic firmware DNS exists
# behind an NDM redirect to an ndnproxy backend. Plan must expose both facts.
run_network plan > "$TMP/standard.plan"
grep -Fq 'EFFECTIVE_DNS_MODE=firmware' "$TMP/standard.plan" || fail 'auto must resolve to firmware DNS'
grep -Fq 'SUPPORTED=yes' "$TMP/standard.plan" || fail 'standard DNS must be supported'
grep -Fq 'PORT53_OWNER=xray' "$TMP/standard.plan" || fail 'plan must report broken Xray :53 ownership'
grep -Fq 'FIRMWARE_DNS_PATH=redirect:41100' "$TMP/standard.plan" || fail 'plan must report Keenetic redirected DNS backend'
grep -Fq 'repair legacy Xray :53 ownership' "$TMP/standard.plan" || fail 'plan must disclose Xray :53 repair'

# Regression for v0.2.9 incident: runtime restart times out once after the
# candidate is staged. Rollback must restore JSON + proxy_dns + legacy owner,
# and the interactive xkeen CLI must never be invoked.
state_set XKEEN_ACTION_RESULT timeout-once
if run_network apply > "$TMP/timeout.fail" 2>&1; then
    fail 'simulated XKeen restart timeout unexpectedly succeeded'
fi
grep -Fq 'PRIMARY ERROR: XKeen init restart' "$TMP/timeout.fail" || fail 'restart timeout primary error missing'
grep -Fq 'ROLLBACK ERROR/STATE: rollback success' "$TMP/timeout.fail" || fail 'restart timeout must roll back successfully'
grep -Fq 'proxy_dns="on"' "$TROOT/etc/init.d/S05xkeen" || fail 'timeout rollback must restore proxy_dns on'
grep -Fq 'PORT53_OWNER=xray' "$STATE" || fail 'timeout rollback must restore legacy Xray :53 owner'
jq -e 'any(.inbounds[]?; (((.port // "") | tostring)) == "53")' "$TROOT/etc/xray/configs/03_inbounds.json" >/dev/null || fail 'timeout rollback must restore Xray :53 listener'
jq -e '([.outbounds[]? | select(.tag == "dns-out")] | length) == 0' "$TROOT/etc/xray/configs/04_outbounds.json" >/dev/null || fail 'timeout rollback must restore no dns-out'
[ ! -e "$TROOT/xkeen-cli.called" ] || fail 'interactive XKeen CLI was invoked during timeout/rollback'

# Regression for v0.2.10 incident: after removing Xray :53, there is no direct
# :53 listener. The logical firmware owner is ndnproxy through redirect:41100;
# router-local nslookup is intentionally not required for that LAN-only path.
run_network apply > "$TMP/standard.redirect.apply" 2>&1 || fail 'redirected firmware DNS repair should succeed'
grep -Fq '[FreeNet Network] RESULT=SUCCESS' "$TMP/standard.redirect.apply" || fail 'redirect standard success marker missing'
grep -Fq 'FIRMWARE_DNS_PATH=redirect:41100' "$TMP/standard.redirect.apply" || fail 'redirect path result missing'
grep -Fq 'DNS_QUERY=CLIENT_REQUIRED' "$TMP/standard.redirect.apply" || fail 'redirect path must defer query to client acceptance'
grep -Fq 'proxy_dns="off"' "$TROOT/etc/init.d/S05xkeen" || fail 'standard mode must disable proxy_dns'
grep -Fq 'PORT53_OWNER=none' "$STATE" || fail 'redirect path must not invent a direct :53 listener'
jq -e 'all(.inbounds[]?; (((.port // "") | tostring)) != "53")' "$TROOT/etc/xray/configs/03_inbounds.json" >/dev/null || fail 'standard mode must remove Xray :53 listener'
jq -e '([.outbounds[]? | select(.tag == "dns-out" and .protocol == "dns")] | length) == 1' "$TROOT/etc/xray/configs/04_outbounds.json" >/dev/null || fail 'common schema must retain one inert dns-out'
jq -e 'all(.routing.rules[]?; (.outboundTag // "") != "dns-out" and (((.port // "") | tostring)) != "53")' "$TROOT/etc/xray/configs/05_routing.json" >/dev/null || fail 'standard mode must remove split-DNS routing rules'
jq -e 'any(.routing.rules[]?; .outboundTag == "direct" and (.domain | index("example.org") != null))' "$TROOT/etc/xray/configs/05_routing.json" >/dev/null || fail 'standard mode must preserve non-DNS routing'
[ ! -e "$TROOT/xkeen-cli.called" ] || fail 'interactive XKeen CLI was invoked during standard apply'
run_network plan > "$TMP/standard.redirect.after.plan"
grep -Fq 'PORT53_OWNER=ndnproxy' "$TMP/standard.redirect.after.plan" || fail 'redirect path must expose logical ndnproxy owner'
grep -Fq 'FIRMWARE_DNS_PATH=redirect:41100' "$TMP/standard.redirect.after.plan" || fail 'redirect path must survive runtime facts'

# Direct ndnproxy:53 remains supported and must retain the in-router query gate.
state_set FIRMWARE_DNS_PATH direct53
state_set PORT53_OWNER ndnproxy
state_set DNS_QUERY_OK yes
run_network apply > "$TMP/standard.direct.apply" 2>&1 || fail 'direct firmware DNS should succeed'
grep -Fq 'FIRMWARE_DNS_PATH=direct53' "$TMP/standard.direct.apply" || fail 'direct path result missing'
grep -Fq 'DNS_QUERY=PASS' "$TMP/standard.direct.apply" || fail 'direct path must require DNS query pass'

# Invalid/partial redirected topology is not accepted and must stop before live mutation.
state_set FIRMWARE_DNS_PATH redirect:not-a-port
state_set PORT53_OWNER none
if run_network apply > "$TMP/invalid.redirect.fail" 2>&1; then
    fail 'invalid redirect topology unexpectedly succeeded'
fi
grep -Fq 'неизвестный владелец DNS path: none' "$TMP/invalid.redirect.fail" || fail 'invalid redirect topology must stop in preflight'
grep -Fq 'proxy_dns="off"' "$TROOT/etc/init.d/S05xkeen" || fail 'invalid redirect preflight must not mutate proxy_dns'

# Explicit Split DNS starts from the real redirected firmware topology and must
# preserve that firmware path while enabling XKeen interception.
state_set FIRMWARE_DNS_PATH redirect:41100
state_set PORT53_OWNER none
state_set DNS_QUERY_OK no
sed -i 's/^DNS_MODE=.*/DNS_MODE=xkeen/' "$TROOT/etc/freenet/freenet.conf"
run_network plan > "$TMP/split.plan"
grep -Fq 'EFFECTIVE_DNS_MODE=xkeen' "$TMP/split.plan" || fail 'explicit xkeen mode must resolve to split DNS'
grep -Fq 'FIRMWARE_DNS_PATH=redirect:41100' "$TMP/split.plan" || fail 'split plan must see redirected firmware path'
grep -Fq 'preserve Keenetic firmware DNS path' "$TMP/split.plan" || fail 'split plan must preserve firmware DNS path'

FREENET_TEST_MIGRATE_RESULT=success run_network apply > "$TMP/split.apply" 2>&1 || fail 'explicit Split DNS apply should succeed on redirect topology'
grep -Fq 'proxy_dns="on"' "$TROOT/etc/init.d/S05xkeen" || fail 'split mode must enable proxy_dns non-interactively'
grep -Fq 'PORT53_OWNER=none' "$STATE" || fail 'split redirect mode must keep no direct :53 listener'
grep -Fq 'FIRMWARE_DNS_PATH=redirect:41100' "$TMP/split.apply" || fail 'split result must report redirected firmware path'
grep -Fq 'DNS_QUERY=CLIENT_REQUIRED' "$TMP/split.apply" || fail 'split redirect query must be client acceptance'
grep -Fq 'EFFECTIVE_DNS_MODE=xkeen' "$TMP/split.apply" || fail 'split result mode missing'
[ ! -e "$TROOT/xkeen-cli.called" ] || fail 'interactive XKeen CLI was invoked during split apply'

# Split DNS is forbidden if Xray already owns :53. No blind mutation is allowed.
sed -i 's/^proxy_dns=.*/proxy_dns="off"/' "$TROOT/etc/init.d/S05xkeen"
state_set PORT53_OWNER xray
if run_network apply > "$TMP/xray53.fail" 2>&1; then
    fail 'Split DNS must stop when Xray already owns :53'
fi
grep -Fq 'сначала примените штатный DNS для безопасного repair' "$TMP/xray53.fail" || fail 'Xray :53 recovery instruction missing'
grep -Fq 'proxy_dns="off"' "$TROOT/etc/init.d/S05xkeen" || fail 'unsafe split preflight must not mutate proxy_dns'

# Migration failure after runtime preparation must restore proxy_dns/config/runtime.
state_set PORT53_OWNER none
state_set XRAY_RUNNING yes
cp "$TROOT/etc/xray/configs/04_outbounds.json" "$TMP/out.before"
if FREENET_TEST_MIGRATE_RESULT=fail run_network apply > "$TMP/migrate.fail" 2>&1; then
    fail 'simulated Split DNS migration failure unexpectedly succeeded'
fi
grep -Fq 'ROLLBACK ERROR/STATE: rollback success/no live apply' "$TMP/migrate.fail" || fail 'Split DNS failure must report successful rollback'
grep -Fq 'proxy_dns="off"' "$TROOT/etc/init.d/S05xkeen" || fail 'rollback must restore proxy_dns off'
cmp -s "$TMP/out.before" "$TROOT/etc/xray/configs/04_outbounds.json" || fail 'rollback must restore outbound config snapshot'
[ ! -e "$TROOT/xkeen-cli.called" ] || fail 'interactive XKeen CLI was invoked by DNS controller'

echo 'network profile contract PASS'
