#!/bin/sh

set -eu

ROOT_DIR="$(CDPATH= cd "$(dirname "$0")/.." && pwd)"
SCRIPT="$ROOT_DIR/scripts/apply_provider_profile.sh"
TMP="$(mktemp -d /tmp/freenet-provider-test.XXXXXX)"
trap 'rm -rf "$TMP"' EXIT HUP INT TERM

fail() { echo "provider profile test FAIL: $*" >&2; exit 1; }

mkdir -p "$TMP/bin" "$TMP/configs" "$TMP/dat" "$TMP/etc"
printf '%s\n' 'https://subscription.invalid/private-token' > "$TMP/sub.url"
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

run_helper() {
    PATH="$TMP/bin:$PATH" \
    FREENET_SUB_FILE="$TMP/sub.url" \
    FREENET_CONFIG_DIR="$TMP/configs" \
    FREENET_ASSET_DIR="$TMP/dat" \
    FREENET_PROFILE_FILE="$TMP/etc/vpn_profile_name" \
    FREENET_XRAY_BIN="$TMP/bin/xray" \
    FREENET_XKEEN_BIN="$TMP/bin/xkeen" \
    FREENET_CURL_BIN="$TMP/bin/curl" \
    sh "$SCRIPT" "$@"
}

# plan must be read-only and secret-safe.
OUT_HASH_BEFORE="$(sha256sum "$TMP/configs/04_outbounds.json" | awk '{print $1}')"
run_helper plan "$PROFILE_ID" > "$TMP/plan.out" 2> "$TMP/plan.err"
OUT_HASH_AFTER="$(sha256sum "$TMP/configs/04_outbounds.json" | awk '{print $1}')"
[ "$OUT_HASH_BEFORE" = "$OUT_HASH_AFTER" ] || fail 'plan mutated 04_outbounds.json'
[ ! -e "$TMP/etc/vpn_profile_name" ] || fail 'plan persisted preferred profile'
grep -Fq "PROFILE_ID=$PROFILE_ID" "$TMP/plan.out" || fail 'plan id missing'
grep -Fq "PROFILE_NAME=$PROFILE_NAME" "$TMP/plan.out" || fail 'plan name missing'
grep -Fq 'ENDPOINT=203.0.113.10:443' "$TMP/plan.out" || fail 'safe endpoint missing'
grep -Fq 'CANDIDATE_XRAY_VALID=yes' "$TMP/plan.out" || fail 'candidate validation missing'
grep -Fq 'MUTATION=NONE' "$TMP/plan.out" || fail 'plan must report MUTATION=NONE'
if grep -Eq 'TEST-ID-A|TEST-PBK|TEST-SID|private-token|vless://' "$TMP/plan.out" "$TMP/plan.err"; then
    fail 'plan leaked provider/subscription credentials'
fi

# apply installs exactly one vless-reality and preserves unrelated non-VLESS outbound.
run_helper apply "$PROFILE_ID" > "$TMP/apply.out" 2> "$TMP/apply.err"
grep -Fq '[FreeNet Provider] RESULT=SUCCESS' "$TMP/apply.out" || fail 'apply success missing'
grep -Fq '[FreeNet Provider] ROLLBACK=NOT_NEEDED' "$TMP/apply.out" || fail 'rollback status missing'
jq -e '([.outbounds[] | select(.tag=="vless-reality")] | length)==1' "$TMP/configs/04_outbounds.json" >/dev/null || fail 'vless-reality not installed exactly once'
jq -e 'any(.outbounds[]; .tag=="direct") and any(.outbounds[]; .tag=="block") and any(.outbounds[]; .tag=="keep-me")' "$TMP/configs/04_outbounds.json" >/dev/null || fail 'non-VLESS outbounds not preserved'
[ "$(cat "$TMP/etc/vpn_profile_name")" = "$PROFILE_NAME" ] || fail 'safe preferred profile name not persisted'
if grep -Eq 'TEST-ID-A|TEST-PBK|TEST-SID|private-token|vless://' "$TMP/apply.out" "$TMP/apply.err"; then
    fail 'apply leaked provider/subscription credentials'
fi

# A stale/nonexistent id must fail before live mutation.
HASH_VALID="$(sha256sum "$TMP/configs/04_outbounds.json" | awk '{print $1}')"
if run_helper apply 0123456789abcdef > "$TMP/miss.out" 2> "$TMP/miss.err"; then
    fail 'missing profile id unexpectedly succeeded'
fi
[ "$HASH_VALID" = "$(sha256sum "$TMP/configs/04_outbounds.json" | awk '{print $1}')" ] || fail 'missing id changed live outbound'

# Simulate running Xray + first restart failure: live files must roll back.
cp "$TMP/configs/04_outbounds.json" "$TMP/configs/04_outbounds.good"
printf '%s\n' 'OLD SAFE PROFILE' > "$TMP/etc/vpn_profile_name"
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
grep -Fq 'PRIMARY ERROR:' "$TMP/rb.err" || fail 'primary error not separated'
grep -Fq 'ROLLBACK ERROR/STATE: rollback success' "$TMP/rb.err" || fail 'rollback success not reported'
if grep -Eq 'TEST-ID-A|TEST-PBK|TEST-SID|private-token|vless://' "$TMP/rb.out" "$TMP/rb.err"; then
    fail 'rollback path leaked credentials'
fi

echo 'provider profile apply test PASS'
