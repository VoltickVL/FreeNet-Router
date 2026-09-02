#!/bin/sh

set -eu

ROOT_DIR="$(CDPATH= cd "$(dirname "$0")/.." && pwd)"
SCRIPT="$ROOT_DIR/scripts/list_subscription_profiles.sh"
TMP="$(mktemp -d /tmp/freenet-profile-test.XXXXXX)"
trap 'rm -rf "$TMP"' EXIT HUP INT TERM

fail() { echo "subscription profiles test FAIL: $*" >&2; exit 1; }

mkdir -p "$TMP/bin"
cat > "$TMP/sub.url" <<'EOF'
https://subscription.invalid/private-token
EOF
cat > "$TMP/fixture" <<'EOF'
vless://TEST-SECRET-A@203.0.113.10:443?security=reality&pbk=SECRET-PBK-A&sid=SECRET-SID-A#%F0%9F%87%A9%F0%9F%87%AA%20Frankfurt%2C%20Germany%2C%20Extra
vless://TEST-SECRET-B@198.51.100.20:8443?security=reality&pbk=SECRET-PBK-B&sid=SECRET-SID-B#%F0%9F%87%B5%F0%9F%87%B1%20Warsaw%2C%20Poland%2C%20Extra
vless://TEST-SECRET-C@192.0.2.30:443?security=reality#Expired%20Warsaw%20Extra
vless://TEST-SECRET-D@192.0.2.40:443?security=reality#Regular%20profile
EOF

cat > "$TMP/bin/curl" <<EOF
#!/bin/sh
cat "$TMP/fixture"
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

PATH="$TMP/bin:$PATH" FREENET_SUB_FILE="$TMP/sub.url" sh "$SCRIPT" > "$TMP/out.jsonl" 2> "$TMP/err"

[ "$(wc -l < "$TMP/out.jsonl" | tr -d ' ')" = 2 ] || fail 'expected exactly two active Extra profiles'
jq -s -e 'length == 2' "$TMP/out.jsonl" >/dev/null || fail 'output must be valid JSONL'
jq -s -e '.[0].name | contains("Frankfurt")' "$TMP/out.jsonl" >/dev/null || fail 'Frankfurt profile missing'
jq -s -e '.[1].name | contains("Warsaw")' "$TMP/out.jsonl" >/dev/null || fail 'Warsaw profile missing'
jq -s -e '.[0].address == "203.0.113.10" and .[0].port == 443' "$TMP/out.jsonl" >/dev/null || fail 'first endpoint mismatch'
jq -s -e '.[1].address == "198.51.100.20" and .[1].port == 8443' "$TMP/out.jsonl" >/dev/null || fail 'second endpoint mismatch'

if grep -Eq 'TEST-SECRET|SECRET-PBK|SECRET-SID|private-token|vless://|security=reality' "$TMP/out.jsonl"; then
    fail 'sanitized output leaked subscription/VLESS credential material'
fi
if grep -Eq 'Expired|Regular profile' "$TMP/out.jsonl"; then
    fail 'non-active/non-Extra profiles leaked into output'
fi

jq -s -e 'all(.[]; (.id | test("^[0-9a-f]{16}$")))' "$TMP/out.jsonl" >/dev/null || fail 'profile ids must be short safe hashes'

echo 'subscription profiles test PASS'
