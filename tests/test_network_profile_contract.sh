#!/bin/sh

set -eu

ROOT_DIR="$(CDPATH= cd "$(dirname "$0")/.." && pwd)"
SCRIPT="$ROOT_DIR/scripts/apply_network_profile.sh"
MIGRATE="$ROOT_DIR/scripts/migrate_split_dns.sh"

fail() { echo "network profile contract FAIL: $*" >&2; exit 1; }

sh -n "$SCRIPT"
sh -n "$MIGRATE"

grep -Fq 'rostelecom)' "$SCRIPT" || fail 'Rostelecom preset missing'
grep -Fq 'podryad)' "$SCRIPT" || fail 'Podryad must remain a separate preset'
grep -Fq 'vladlink)' "$SCRIPT" || fail 'Vladlink must remain a separate preset'
grep -Fq 'alliancetelecom)' "$SCRIPT" || fail 'AllianceTelecom must remain a separate preset'

grep -Fq "REASON='Подряд имеет отдельный preset ID" "$SCRIPT" || fail 'Podryad runtime gate missing'
grep -Fq "REASON='Владлинк имеет отдельный preset ID" "$SCRIPT" || fail 'Vladlink clean-room gate missing'
grep -Fq "REASON='АльянсТелеком имеет отдельный preset ID" "$SCRIPT" || fail 'AllianceTelecom clean-room gate missing'

grep -Fq 'PORT53_OWNER=ndnproxy' "$SCRIPT" || fail 'ndnproxy fact reporting missing'
grep -Fq 'expected 11111' "$SCRIPT" || fail 'Xray GID acceptance missing'
grep -Fq 'xkeen -dns on' "$SCRIPT" || fail 'controlled XKeen DNS activation missing'
grep -Fq 'XRAY_RUNNING=' "$SCRIPT" || fail 'Xray running-state fact missing'
grep -Fq 'VLESS_PROFILE=' "$SCRIPT" || fail 'VLESS presence fact missing'
grep -Fq 'MUTATION=NONE' "$SCRIPT" || fail 'read-only plan marker missing'
grep -Fq 'EXPECTED_NO_DELTA=no new Xray listener :53; no VLESS credential rewrite; no subscription secret change' "$SCRIPT" || fail 'no-delta boundary missing'

grep -Fq '"$MIGRATE_SCRIPT"' "$SCRIPT" || fail 'transactional migration engine delegation missing'
grep -Fq 'ROLLBACK ERROR/STATE: FAILED/UNKNOWN' "$SCRIPT" || fail 'rollback unknown state missing'
grep -Fq 'rollback success/no live apply' "$SCRIPT" || fail 'runtime rollback success state missing'
grep -Fq 'RESULT=SUCCESS' "$SCRIPT" || fail 'success marker missing'

grep -Fq '/opt/etc/init.d/S99xkeen /opt/etc/init.d/S05xkeen' "$MIGRATE" || fail 'migration must support legacy S99 and current XKeen 2.0 S05 init paths'

if grep -Ei 'subscription.*url=|uuid=|publicKey|shortId|vless://' "$SCRIPT" >/dev/null; then
    fail 'network profile controller contains secret material'
fi

# The controller must not implement arbitrary custom shell commands.
if grep -Eq 'eval[[:space:]]+.*(ISP|DNS)|sh[[:space:]]+-c[[:space:]]+.*(ISP|DNS)' "$SCRIPT"; then
    fail 'network profile values must not become shell commands'
fi

# Functional clean-router orchestration test. This does not touch /opt.
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT INT TERM
TROOT="$TMP/opt"
STATE="$TMP/runtime.state"
mkdir -p "$TROOT/etc/freenet" "$TROOT/etc/xray/configs" "$TROOT/etc/xray/dat" "$TROOT/etc/init.d" "$TROOT/sbin" "$TROOT/lib/freenet"

cat > "$TROOT/etc/freenet/freenet.conf" <<'EOF'
ISP_ID=rostelecom
DNS_MODE=firmware
EOF
cat > "$TROOT/etc/init.d/S05xkeen" <<'EOF'
#!/bin/sh
proxy_dns="off"
EOF
cat > "$TROOT/etc/xray/configs/02_dns.json" <<'EOF'
{}
EOF
cat > "$TROOT/etc/xray/configs/03_inbounds.json" <<'EOF'
{"inbounds":[]}
EOF
cat > "$TROOT/etc/xray/configs/04_outbounds.json" <<'EOF'
{"outbounds":[{"tag":"vless-reality","protocol":"freedom"},{"tag":"direct","protocol":"freedom"},{"tag":"block","protocol":"blackhole"}]}
EOF
cat > "$TROOT/etc/xray/configs/05_routing.json" <<'EOF'
{"routing":{"rules":[]}}
EOF
cat > "$STATE" <<'EOF'
PORT53_OWNER=ndnproxy
XRAY_RUNNING=no
XRAY_GID=11111
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
            on|off)
                sed -i "s/^proxy_dns=.*/proxy_dns=\"${2}\"/" "$FREENET_ROOT/etc/init.d/S05xkeen"
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
# Candidate/live validation is represented by a successful fake core in this contract test.
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

run_network plan > "$TMP/plan.out"
grep -Fq 'SUPPORTED=yes' "$TMP/plan.out" || fail 'clean Rostelecom plan should be supported'
grep -Fq 'PROXY_DNS=off' "$TMP/plan.out" || fail 'plan must report initial proxy_dns off'
grep -Fq 'XRAY_RUNNING=no' "$TMP/plan.out" || fail 'plan must report stopped clean Xray state'
grep -Fq 'enable XKeen DNS interception via xkeen -dns on' "$TMP/plan.out" || fail 'plan must disclose DNS activation delta'
grep -Fq 'start XKeen/Xray before transactional DNS migration' "$TMP/plan.out" || fail 'plan must disclose clean start delta'

FREENET_TEST_MIGRATE_RESULT=success run_network apply > "$TMP/apply.out" 2>&1 || fail 'clean Rostelecom apply should succeed'
grep -Fq '[FreeNet Network] RESULT=SUCCESS' "$TMP/apply.out" || fail 'success marker missing after clean apply'
grep -Fq 'proxy_dns="on"' "$TROOT/etc/init.d/S05xkeen" || fail 'clean apply must enable XKeen proxy_dns through xkeen command'
grep -Fq 'XRAY_RUNNING=yes' "$STATE" || fail 'clean apply must leave Xray running'
jq -e 'any(.outbounds[]?; .tag == "dns-out" and .protocol == "dns")' "$TROOT/etc/xray/configs/04_outbounds.json" >/dev/null || fail 'clean apply must accept dns-out result'

# Reset and force a migration failure. The wrapper must restore the original
# proxy_dns=off + Xray stopped state instead of leaving a half-applied setup.
sed -i 's/^proxy_dns=.*/proxy_dns="off"/' "$TROOT/etc/init.d/S05xkeen"
sed -i 's/^XRAY_RUNNING=.*/XRAY_RUNNING=no/' "$STATE"
jq ' .outbounds = [.outbounds[] | select(.tag != "dns-out")] ' "$TROOT/etc/xray/configs/04_outbounds.json" > "$TMP/out.json"
mv "$TMP/out.json" "$TROOT/etc/xray/configs/04_outbounds.json"

if FREENET_TEST_MIGRATE_RESULT=fail run_network apply > "$TMP/fail.out" 2>&1; then
    fail 'simulated migration failure unexpectedly succeeded'
fi
grep -Fq 'ROLLBACK ERROR/STATE: rollback success/no live apply' "$TMP/fail.out" || fail 'migration failure must report successful orchestration rollback'
grep -Fq 'proxy_dns="off"' "$TROOT/etc/init.d/S05xkeen" || fail 'rollback must restore proxy_dns off'
grep -Fq 'XRAY_RUNNING=no' "$STATE" || fail 'rollback must restore original stopped Xray state'
if jq -e 'any(.outbounds[]?; .tag == "dns-out")' "$TROOT/etc/xray/configs/04_outbounds.json" >/dev/null; then
    fail 'failed migration must not leave dns-out in live config'
fi

echo 'network profile contract PASS'
