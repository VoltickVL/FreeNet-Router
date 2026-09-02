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
grep -Fq '"$XKEEN_BIN" -dns off' "$SCRIPT" || fail 'controlled standard DNS activation missing'
grep -Fq '"$XKEEN_BIN" -dns on' "$SCRIPT" || fail 'controlled Split DNS activation missing'
grep -Fq 'PORT53_OWNER=ndnproxy' "$SCRIPT" || fail 'ndnproxy fact reporting missing'
grep -Fq 'expected 11111' "$SCRIPT" || fail 'Xray GID acceptance missing'
grep -Fq 'MUTATION=NONE' "$SCRIPT" || fail 'read-only plan marker missing'
grep -Fq 'Xray уже владеет :53; сначала примените штатный DNS' "$SCRIPT" || fail 'unsafe split-DNS Xray:53 guard missing'
grep -Fq 'post-apply acceptance штатного DNS не пройден' "$SCRIPT" || fail 'standard DNS acceptance missing'
grep -Fq 'post-apply Split DNS acceptance failed' "$SCRIPT" || fail 'split DNS acceptance missing'
grep -Fq 'ROLLBACK ERROR/STATE: FAILED/UNKNOWN' "$SCRIPT" || fail 'rollback unknown state missing'
grep -Fq 'RESULT=SUCCESS' "$SCRIPT" || fail 'success marker missing'

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
XRAY_RUNNING=yes
XRAY_GID=11111
DNS_QUERY_OK=yes
EOF

cat > "$TROOT/sbin/xkeen" <<'EOF'
#!/bin/sh
set_state() {
    key="$1"; value="$2"; tmp="${FREENET_NETWORK_TEST_STATE}.tmp.$$"
    grep -v "^${key}=" "$FREENET_NETWORK_TEST_STATE" > "$tmp" 2>/dev/null || true
    echo "${key}=${value}" >> "$tmp"
    mv "$tmp" "$FREENET_NETWORK_TEST_STATE"
}
case "${1:-}" in
    -start|-restart)
        set_state XRAY_RUNNING yes
        set_state XRAY_GID 11111
        ;;
    -stop)
        set_state XRAY_RUNNING no
        ;;
    -dns)
        case "${2:-}" in
            on)
                sed -i 's/^proxy_dns=.*/proxy_dns="on"/' "$FREENET_ROOT/etc/init.d/S05xkeen"
                set_state PORT53_OWNER ndnproxy
                ;;
            off)
                sed -i 's/^proxy_dns=.*/proxy_dns="off"/' "$FREENET_ROOT/etc/init.d/S05xkeen"
                set_state PORT53_OWNER ndnproxy
                ;;
            *) exit 1 ;;
        esac
        ;;
    *) exit 1 ;;
esac
exit 0
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
    FREENET_NETWORK_TEST_MODE=yes \
    FREENET_NETWORK_TEST_STATE="$STATE" \
    sh "$SCRIPT" "$@"
}

# auto is now safe standard DNS even for Vladlink. The plan must disclose repair
# of the exact HOME failure class where Xray owns port 53.
run_network plan > "$TMP/standard.plan"
grep -Fq 'EFFECTIVE_DNS_MODE=firmware' "$TMP/standard.plan" || fail 'auto must resolve to firmware DNS'
grep -Fq 'SUPPORTED=yes' "$TMP/standard.plan" || fail 'standard DNS must be supported'
grep -Fq 'PORT53_OWNER=xray' "$TMP/standard.plan" || fail 'plan must report broken Xray :53 ownership'
grep -Fq 'repair legacy Xray :53 ownership' "$TMP/standard.plan" || fail 'plan must disclose Xray :53 repair'

run_network apply > "$TMP/standard.apply" 2>&1 || fail 'standard DNS repair should succeed'
grep -Fq '[FreeNet Network] RESULT=SUCCESS' "$TMP/standard.apply" || fail 'standard success marker missing'
grep -Fq 'EFFECTIVE_DNS_MODE=firmware' "$TMP/standard.apply" || fail 'standard result mode missing'
grep -Fq 'proxy_dns="off"' "$TROOT/etc/init.d/S05xkeen" || fail 'standard mode must disable proxy_dns'
grep -Fq 'PORT53_OWNER=ndnproxy' "$STATE" || fail 'standard mode must restore ndnproxy :53 ownership'
jq -e 'all(.inbounds[]?; (((.port // "") | tostring)) != "53")' "$TROOT/etc/xray/configs/03_inbounds.json" >/dev/null || fail 'standard mode must remove Xray :53 listener'
jq -e '([.outbounds[]? | select(.tag == "dns-out" and .protocol == "dns")] | length) == 1' "$TROOT/etc/xray/configs/04_outbounds.json" >/dev/null || fail 'common schema must retain one inert dns-out'
jq -e 'all(.routing.rules[]?; (.outboundTag // "") != "dns-out" and (((.port // "") | tostring)) != "53")' "$TROOT/etc/xray/configs/05_routing.json" >/dev/null || fail 'standard mode must remove split-DNS routing rules'
jq -e 'any(.routing.rules[]?; .outboundTag == "direct" and (.domain | index("example.org") != null))' "$TROOT/etc/xray/configs/05_routing.json" >/dev/null || fail 'standard mode must preserve non-DNS routing'

# Explicit Split DNS starts only from a healthy firmware-DNS topology and must
# keep ndnproxy on :53 while enabling XKeen interception.
sed -i 's/^DNS_MODE=.*/DNS_MODE=xkeen/' "$TROOT/etc/freenet/freenet.conf"
run_network plan > "$TMP/split.plan"
grep -Fq 'EFFECTIVE_DNS_MODE=xkeen' "$TMP/split.plan" || fail 'explicit xkeen mode must resolve to split DNS'
grep -Fq 'preserve Keenetic ndnproxy as owner of :53' "$TMP/split.plan" || fail 'split plan must preserve firmware :53 owner'

FREENET_TEST_MIGRATE_RESULT=success run_network apply > "$TMP/split.apply" 2>&1 || fail 'explicit Split DNS apply should succeed'
grep -Fq 'proxy_dns="on"' "$TROOT/etc/init.d/S05xkeen" || fail 'split mode must enable proxy_dns through xkeen command'
grep -Fq 'PORT53_OWNER=ndnproxy' "$STATE" || fail 'split mode must keep ndnproxy :53 ownership'
grep -Fq 'EFFECTIVE_DNS_MODE=xkeen' "$TMP/split.apply" || fail 'split result mode missing'

# Split DNS is forbidden if Xray already owns :53. No blind mutation is allowed.
sed -i 's/^proxy_dns=.*/proxy_dns="off"/' "$TROOT/etc/init.d/S05xkeen"
sed -i 's/^PORT53_OWNER=.*/PORT53_OWNER=xray/' "$STATE"
if run_network apply > "$TMP/xray53.fail" 2>&1; then
    fail 'Split DNS must stop when Xray already owns :53'
fi
grep -Fq 'сначала примените штатный DNS для безопасного repair' "$TMP/xray53.fail" || fail 'Xray :53 recovery instruction missing'
grep -Fq 'proxy_dns="off"' "$TROOT/etc/init.d/S05xkeen" || fail 'unsafe split preflight must not mutate proxy_dns'

# Migration failure after runtime preparation must restore proxy_dns/config/runtime.
sed -i 's/^PORT53_OWNER=.*/PORT53_OWNER=ndnproxy/' "$STATE"
sed -i 's/^XRAY_RUNNING=.*/XRAY_RUNNING=yes/' "$STATE"
cp "$TROOT/etc/xray/configs/04_outbounds.json" "$TMP/out.before"
if FREENET_TEST_MIGRATE_RESULT=fail run_network apply > "$TMP/migrate.fail" 2>&1; then
    fail 'simulated Split DNS migration failure unexpectedly succeeded'
fi
grep -Fq 'ROLLBACK ERROR/STATE: rollback success/no live apply' "$TMP/migrate.fail" || fail 'Split DNS failure must report successful rollback'
grep -Fq 'proxy_dns="off"' "$TROOT/etc/init.d/S05xkeen" || fail 'rollback must restore proxy_dns off'
cmp -s "$TMP/out.before" "$TROOT/etc/xray/configs/04_outbounds.json" || fail 'rollback must restore outbound config snapshot'

echo 'network profile contract PASS'
