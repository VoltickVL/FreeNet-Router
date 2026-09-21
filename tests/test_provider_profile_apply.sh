#!/bin/sh

set -eu

ROOT_DIR="$(CDPATH= cd "$(dirname "$0")/.." && pwd)"
SCRIPT="$ROOT_DIR/scripts/apply_provider_profile.sh"
TMP="$(mktemp -d /tmp/freenet-provider-test.XXXXXX)"
trap 'rm -rf "$TMP"' EXIT HUP INT TERM

fail() { echo "provider profile test FAIL: $*" >&2; exit 1; }

mkdir -p "$TMP/bin" "$TMP/configs" "$TMP/dat" "$TMP/etc"
printf '%s\n' 'https://subscription.invalid/private-token' > "$TMP/sub.url"
printf '%s\n' 'Germany|Frankfurt' > "$TMP/profile.filter"
cat > "$TMP/sub.fixture" <<'EOF'
vless://TEST-ID-A@203.0.113.10:443?flow=xtls-rprx-vision&security=reality&type=tcp&fp=firefox&sni=example.test&pbk=TEST-PBK-A&sid=TEST-SID-A&spx=%2F#Frankfurt%2C%20Germany%2C%20Extra
vless://TEST-ID-B@198.51.100.20:443?flow=xtls-rprx-vision&security=reality&type=tcp&fp=firefox&sni=example.test&pbk=TEST-PBK-B&sid=TEST-SID-B&spx=%2F#Warsaw%2C%20Poland%2C%20Extra
vless://TEST-ID-C@192.0.2.30:443?security=reality&sni=example.test&pbk=X&sid=Y#Expired%20Extra
EOF

cat > "$TMP/bin/curl" <<EOF
#!/bin/sh
cat "$TMP/sub.fixture"
EOF
chmod 755 "$TMP/bin/curl"

cat > "$TMP/bin/nslookup" <<'EOF'
#!/bin/sh
cat <<OUT
Name: subscription.invalid
Address 1: 203.0.113.53 subscription.invalid
OUT
EOF
chmod 755 "$TMP/bin/nslookup"

cat > "$TMP/bin/xray" <<'EOF'
#!/bin/sh
[ "$1" = run ] || exit 9
[ "$2" = -test ] || exit 9
[ "$3" = -confdir ] || exit 9
[ -d "$4" ] || exit 9
exit 0
EOF
chmod 755 "$TMP/bin/xray"

cat > "$TMP/bin/xkeen" <<'EOF'
#!/bin/sh
exit 0
EOF
chmod 755 "$TMP/bin/xkeen"

cat > "$TMP/bin/pidof" <<'EOF'
#!/bin/sh
exit 1
EOF
chmod 755 "$TMP/bin/pidof"

cat > "$TMP/configs/01_log.json" <<'EOF'
{}
EOF
cat > "$TMP/configs/03_inbounds.json" <<'EOF'
{"inbounds":[]}
EOF
cat > "$TMP/configs/04_outbounds.json" <<'EOF'
{"outbounds":[{"tag":"direct","protocol":"freedom"},{"tag":"block","protocol":"blackhole"},{"tag":"keep-me","protocol":"freedom"}]}
EOF
cat > "$TMP/configs/05_routing.json" <<'EOF'
{"routing":{"rules":[]}}
EOF

PROFILE_NAME='Frankfurt, Germany, Extra'
PROFILE_ID="$(printf '%s|%s|%s' "$PROFILE_NAME" '203.0.113.10' '443' | sha256sum | awk '{print substr($1,1,16)}')"
HISTORY_FILE="$TMP/history.log"

run_helper() {
    PATH="$TMP/bin:$PATH" \
    FREENET_SUB_FILE="$TMP/sub.url" \
    FREENET_CONFIG_DIR="$TMP/configs" \
    FREENET_ASSET_DIR="$TMP/dat" \
    FREENET_PROFILE_FILE="$TMP/etc/vpn_profile_name" \
    FREENET_FILTER_FILE="$TMP/profile.filter" \
    FREENET_AUTOMATION_HISTORY="$HISTORY_FILE" \
    FREENET_XRAY_BIN="$TMP/bin/xray" \
    FREENET_XKEEN_BIN="$TMP/bin/xkeen" \
    FREENET_XRAY_CORE_RESTART_HELPER="${CORE_HELPER:-}" \
    FREENET_CURL_BIN="$TMP/bin/curl" \
    sh "$SCRIPT" "$@"
}

# plan must be read-only and secret-safe.
OUT_HASH_BEFORE="$(sha256sum "$TMP/configs/04_outbounds.json" | awk '{print $1}')"
FILTER_BEFORE="$(cat "$TMP/profile.filter")"
run_helper plan "$PROFILE_ID" > "$TMP/plan.out" 2> "$TMP/plan.err"
OUT_HASH_AFTER="$(sha256sum "$TMP/configs/04_outbounds.json" | awk '{print $1}')"
[ "$OUT_HASH_BEFORE" = "$OUT_HASH_AFTER" ] || fail 'plan mutated 04_outbounds.json'
[ "$FILTER_BEFORE" = "$(cat "$TMP/profile.filter")" ] || fail 'plan mutated active profile filter'
[ ! -e "$TMP/etc/vpn_profile_name" ] || fail 'plan persisted preferred profile'
[ ! -e "$HISTORY_FILE" ] || fail 'plan wrote provider switch journal event'
grep -Fq "PROFILE_ID=$PROFILE_ID" "$TMP/plan.out" || fail 'plan id missing'
grep -Fq "PROFILE_NAME=$PROFILE_NAME" "$TMP/plan.out" || fail 'plan name missing'
grep -Fq 'ENDPOINT=203.0.113.10:443' "$TMP/plan.out" || fail 'safe endpoint missing'
grep -Fq 'CANDIDATE_XRAY_VALID=yes' "$TMP/plan.out" || fail 'candidate validation missing'
grep -Fq 'MUTATION=NONE' "$TMP/plan.out" || fail 'plan must report MUTATION=NONE'
if grep -Eq 'TEST-ID-A|TEST-PBK|TEST-SID|private-token|vless://' "$TMP/plan.out" "$TMP/plan.err"; then
    fail 'plan leaked provider/subscription credentials'
fi

# apply installs exactly one vless-reality, preserves unrelated outbounds and
# commits the exact profile selector used by status/Refresh/Rotate.
run_helper apply "$PROFILE_ID" > "$TMP/apply.out" 2> "$TMP/apply.err"
grep -Fq '[FreeNet Provider] RESULT=SUCCESS' "$TMP/apply.out" || fail 'apply success missing'
grep -Fq '[FreeNet Provider] ROLLBACK=NOT_NEEDED' "$TMP/apply.out" || fail 'rollback status missing'
jq -e '([.outbounds[] | select(.tag=="vless-reality")] | length)==1' "$TMP/configs/04_outbounds.json" >/dev/null || fail 'vless-reality not installed exactly once'
jq -e 'any(.outbounds[]; .tag=="direct") and any(.outbounds[]; .tag=="block") and any(.outbounds[]; .tag=="keep-me")' "$TMP/configs/04_outbounds.json" >/dev/null || fail 'non-VLESS outbounds not preserved'
[ "$(cat "$TMP/etc/vpn_profile_name")" = "$PROFILE_NAME" ] || fail 'safe preferred profile name not persisted'
[ "$(cat "$TMP/profile.filter")" = "$PROFILE_NAME" ] || fail 'exact active profile filter not committed'
grep -Fq 'VPN switch' "$HISTORY_FILE" || fail 'provider switch journal kind missing'
grep -Fq 'success' "$HISTORY_FILE" || fail 'provider switch journal result missing'
grep -Fq "$PROFILE_NAME" "$HISTORY_FILE" || fail 'provider switch journal profile missing'
grep -Fq '203.0.113.10:443' "$HISTORY_FILE" || fail 'provider switch journal endpoint missing'
if grep -Eq 'TEST-ID-A|TEST-PBK|TEST-SID|private-token|vless://' "$TMP/apply.out" "$TMP/apply.err" "$HISTORY_FILE"; then
    fail 'apply leaked provider/subscription credentials'
fi

# Endpoint-only apply must preserve XKeen/netfilter ownership: it uses the
# core-only restart hook and must never call xkeen -restart.
cat > "$TMP/bin/pidof" <<'EOF'
#!/bin/sh
[ "$1" = xray ] && { echo 4242; exit 0; }
exit 1
EOF
chmod 755 "$TMP/bin/pidof"
cat > "$TMP/bin/xkeen" <<EOF
#!/bin/sh
printf '%s\n' "\$*" >> "$TMP/xkeen.calls"
exit 99
EOF
chmod 755 "$TMP/bin/xkeen"
cat > "$TMP/bin/core-restart" <<EOF
#!/bin/sh
printf '%s\n' "\${1:-no}" >> "$TMP/core.calls"
exit 0
EOF
chmod 755 "$TMP/bin/core-restart"
CORE_HELPER="$TMP/bin/core-restart"
export CORE_HELPER
run_helper apply-core "$PROFILE_ID" > "$TMP/core.out" 2> "$TMP/core.err"
grep -Fq '[FreeNet Provider] RESULT=SUCCESS' "$TMP/core.out" || fail 'core-only apply success missing'
[ -s "$TMP/core.calls" ] || fail 'core-only apply did not invoke core restart helper'
[ ! -s "$TMP/xkeen.calls" ] || fail 'endpoint-only apply called xkeen and may tear down firewall rules'
# The standalone core-restart command used by Go-side rollback must use the
# same firewall-preserving mechanism.
run_helper core-restart > "$TMP/core-command.out" 2> "$TMP/core-command.err"
grep -Fq '[FreeNet Provider] CORE_RESTART=SUCCESS' "$TMP/core-command.out" || fail 'standalone core-restart command failed'
[ ! -s "$TMP/xkeen.calls" ] || fail 'standalone core restart called xkeen'
unset CORE_HELPER
rm -f "$TMP/core.calls" "$TMP/xkeen.calls"

# Restore default non-running stubs for the remaining generic apply tests.
cat > "$TMP/bin/pidof" <<'EOF'
#!/bin/sh
exit 1
EOF
chmod 755 "$TMP/bin/pidof"
cat > "$TMP/bin/xkeen" <<'EOF'
#!/bin/sh
exit 0
EOF
chmod 755 "$TMP/bin/xkeen"

# A stale/nonexistent id must fail before live mutation.
HASH_VALID="$(sha256sum "$TMP/configs/04_outbounds.json" | awk '{print $1}')"
FILTER_VALID="$(cat "$TMP/profile.filter")"
if run_helper apply 0123456789abcdef > "$TMP/miss.out" 2> "$TMP/miss.err"; then
    fail 'missing profile id unexpectedly succeeded'
fi
[ "$HASH_VALID" = "$(sha256sum "$TMP/configs/04_outbounds.json" | awk '{print $1}')" ] || fail 'missing id changed live outbound'
[ "$FILTER_VALID" = "$(cat "$TMP/profile.filter")" ] || fail 'missing id changed active profile filter'

# Simulate running Xray + first restart failure: outbound, preferred profile and
# exact active filter must all roll back to the previous accepted state.
cp "$TMP/configs/04_outbounds.json" "$TMP/configs/04_outbounds.good"
printf '%s\n' 'OLD SAFE PROFILE' > "$TMP/etc/vpn_profile_name"
printf '%s\n' 'OLD|FILTER' > "$TMP/profile.filter"
cat > "$TMP/bin/pidof" <<'EOF'
#!/bin/sh
[ "$1" = xray ] && exit 0
exit 1
EOF
chmod 755 "$TMP/bin/pidof"
cat > "$TMP/bin/xkeen" <<EOF
#!/bin/sh
COUNT_FILE="$TMP/restart.count"
COUNT=0
[ -f "\$COUNT_FILE" ] && COUNT="\$(cat "\$COUNT_FILE")"
COUNT=\$((COUNT+1))
printf '%s\n' "\$COUNT" > "\$COUNT_FILE"
[ "\$COUNT" -eq 1 ] && exit 1
exit 0
EOF
chmod 755 "$TMP/bin/xkeen"

ROLLBACK_HASH="$(sha256sum "$TMP/configs/04_outbounds.json" | awk '{print $1}')"
if run_helper apply "$PROFILE_ID" > "$TMP/rb.out" 2> "$TMP/rb.err"; then
    fail 'restart failure unexpectedly succeeded'
fi
[ "$ROLLBACK_HASH" = "$(sha256sum "$TMP/configs/04_outbounds.json" | awk '{print $1}')" ] || fail 'rollback did not restore outbound'
[ "$(cat "$TMP/etc/vpn_profile_name")" = 'OLD SAFE PROFILE' ] || fail 'rollback did not restore preferred profile'
[ "$(cat "$TMP/profile.filter")" = 'OLD|FILTER' ] || fail 'rollback did not restore active profile filter'
grep -Fq 'PRIMARY ERROR:' "$TMP/rb.err" || fail 'primary error not separated'
grep -Fq 'ROLLBACK ERROR/STATE: rollback success' "$TMP/rb.err" || fail 'rollback success not reported'
grep -Fq 'failed' "$HISTORY_FILE" || fail 'failed provider switch journal result missing'
grep -Fq 'Xray/XKeen runtime acceptance failed after provider apply' "$HISTORY_FILE" || fail 'failed provider switch journal reason missing'
if grep -Eq 'TEST-ID-A|TEST-PBK|TEST-SID|private-token|vless://' "$TMP/rb.out" "$TMP/rb.err" "$HISTORY_FILE"; then
    fail 'rollback path leaked credentials'
fi

# Core-only restart failure must restore snapshot and retry the same core-only
# restart path. It must not fall back to xkeen -restart.
cp "$TMP/configs/04_outbounds.json" "$TMP/configs/04_outbounds.core.good"
printf '%s\n' 'CORE OLD PROFILE' > "$TMP/etc/vpn_profile_name"
printf '%s\n' 'CORE|OLD' > "$TMP/profile.filter"
cat > "$TMP/bin/pidof" <<'EOF'
#!/bin/sh
[ "$1" = xray ] && { echo 4242; exit 0; }
exit 1
EOF
chmod 755 "$TMP/bin/pidof"
cat > "$TMP/bin/xkeen" <<EOF
#!/bin/sh
printf '%s\n' "\$*" >> "$TMP/xkeen-core-rb.calls"
exit 99
EOF
chmod 755 "$TMP/bin/xkeen"
cat > "$TMP/bin/core-restart-rb" <<EOF
#!/bin/sh
COUNT_FILE="$TMP/core-rb.count"
COUNT=0
[ -f "\$COUNT_FILE" ] && COUNT="\$(cat "\$COUNT_FILE")"
COUNT=\$((COUNT+1))
printf '%s\n' "\$COUNT" > "\$COUNT_FILE"
[ "\$COUNT" -eq 1 ] && exit 1
exit 0
EOF
chmod 755 "$TMP/bin/core-restart-rb"
CORE_HELPER="$TMP/bin/core-restart-rb"
export CORE_HELPER
CORE_RB_HASH="$(sha256sum "$TMP/configs/04_outbounds.json" | awk '{print $1}')"
if run_helper apply-core "$PROFILE_ID" > "$TMP/core-rb.out" 2> "$TMP/core-rb.err"; then
    fail 'core-only restart failure unexpectedly succeeded'
fi
[ "$CORE_RB_HASH" = "$(sha256sum "$TMP/configs/04_outbounds.json" | awk '{print $1}')" ] || fail 'core-only rollback did not restore outbound'
[ "$(cat "$TMP/etc/vpn_profile_name")" = 'CORE OLD PROFILE' ] || fail 'core-only rollback did not restore preferred profile'
[ "$(cat "$TMP/profile.filter")" = 'CORE|OLD' ] || fail 'core-only rollback did not restore active filter'
[ "$(cat "$TMP/core-rb.count")" = '2' ] || fail 'core-only rollback did not retry core restart after restoring snapshot'
[ ! -s "$TMP/xkeen-core-rb.calls" ] || fail 'core-only rollback fell back to xkeen restart'
grep -Fq 'ROLLBACK ERROR/STATE: rollback success' "$TMP/core-rb.err" || fail 'core-only rollback success not reported'
unset CORE_HELPER

# Source contract: endpoint-only mode must not contain a fixed wait and must
# explicitly dispatch to the core-only restart path.
grep -Fq '[ "$MODE" = apply-core ]' "$SCRIPT" || fail 'apply-core mode dispatch missing'
grep -Fq 'restart_xray_core no' "$SCRIPT" || fail 'apply-core does not use core-only restart'
grep -Fq 'restart_xray_core yes' "$SCRIPT" || fail 'apply-core rollback does not use core-only restart'

echo 'provider profile apply test PASS'
