#!/bin/sh
set -eu

ROOT_DIR="$(CDPATH= cd "$(dirname "$0")/.." && pwd)"
SCRIPT="$ROOT_DIR/scripts/apply_provider_profile.sh"
TMP="$(mktemp -d /tmp/freenet-provider-builtin-probe.XXXXXX)"
trap 'rm -rf "$TMP"' EXIT HUP INT TERM

fail() { echo "provider built-in probe test FAIL: $*" >&2; exit 1; }

mkdir -p "$TMP/bin" "$TMP/configs" "$TMP/dat" "$TMP/etc"
printf '%s\n' 'https://subscription.invalid/private-token' > "$TMP/sub.url"
printf '%s\n' 'Frankfurt, Germany, Extra' > "$TMP/profile.filter"
cat > "$TMP/sub.fixture" <<'EOF'
vless://TEST-ID-A@203.0.113.10:443?flow=xtls-rprx-vision&security=reality&type=tcp&fp=firefox&sni=example.test&pbk=TEST-PBK-A&sid=TEST-SID-A&spx=%2F#Frankfurt%2C%20Germany%2C%20Extra
EOF

cat > "$TMP/bin/curl" <<'EOF'
#!/bin/sh
case " $* " in
  *" --socks5-hostname "*) printf '204'; exit 0 ;;
esac
cat "$FAKE_SUB_FIXTURE"
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
[ "$#" -ge 2 ] || exit 9
[ "$1" = run ] || exit 9
if [ "$2" = -test ]; then
    [ "$#" -ge 4 ] || exit 9
    [ "$3" = -confdir ] || exit 9
    [ -d "$4" ] || exit 9
    exit 0
fi
[ "$2" = -confdir ] || exit 9
[ "$#" -ge 3 ] || exit 9
CONF_DIR="$3"
PORT="$(jq -r '.inbounds[0].port // empty' "$CONF_DIR/00_probe.json" 2>/dev/null)"
[ -n "$PORT" ] || exit 9
printf '%s\n' "$PORT" > "$FAKE_XRAY_LISTENER_FILE"
cleanup_listener() {
    rm -f "$FAKE_XRAY_LISTENER_FILE"
    exit 0
}
trap cleanup_listener TERM INT HUP
while :; do sleep 1; done
EOF
chmod 755 "$TMP/bin/xray"

cat > "$TMP/bin/netstat" <<'EOF'
#!/bin/sh
if [ -s "$FAKE_XRAY_LISTENER_FILE" ]; then
    PORT="$(cat "$FAKE_XRAY_LISTENER_FILE")"
    printf 'tcp        0      0 127.0.0.1:%s          0.0.0.0:*               LISTEN\n' "$PORT"
fi
EOF
chmod 755 "$TMP/bin/netstat"

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

cat > "$TMP/configs/01_log.json" <<'EOF'
{}
EOF
cat > "$TMP/configs/03_inbounds.json" <<'EOF'
{"inbounds":[]}
EOF
cat > "$TMP/configs/04_outbounds.json" <<'EOF'
{"outbounds":[{"tag":"direct","protocol":"freedom"},{"tag":"block","protocol":"blackhole"}]}
EOF
cat > "$TMP/configs/05_routing.json" <<'EOF'
{"routing":{"rules":[]}}
EOF

PROFILE_NAME='Frankfurt, Germany, Extra'
PROFILE_ID="$(printf '%s|%s|%s|%s|%s|%s' "$PROFILE_NAME" 'reality' 'tcp' 'example.test' '' '' | sha256sum | awk '{print substr($1,1,16)}')"

PATH="$TMP/bin:$PATH" \
FAKE_SUB_FIXTURE="$TMP/sub.fixture" \
FAKE_XRAY_LISTENER_FILE="$TMP/probe-listener.port" \
FREENET_SUB_FILE="$TMP/sub.url" \
FREENET_CONFIG_DIR="$TMP/configs" \
FREENET_ASSET_DIR="$TMP/dat" \
FREENET_PROFILE_FILE="$TMP/etc/vpn_profile_name" \
FREENET_FILTER_FILE="$TMP/profile.filter" \
FREENET_AUTOMATION_HISTORY="$TMP/history.log" \
FREENET_PROVIDER_SUBSCRIPTION_CACHE="$TMP/provider-subscription.lkg" \
FREENET_PROVIDER_SUBSCRIPTION_SOURCE="$TMP/provider-subscription.source" \
FREENET_XRAY_BIN="$TMP/bin/xray" \
FREENET_XKEEN_BIN="$TMP/bin/xkeen" \
FREENET_LOCK_DIR="$TMP/vpn-mutation.lock" \
FREENET_CURL_BIN="$TMP/bin/curl" \
FREENET_PROVIDER_ROUTE_PROBE_BIN="" \
sh "$SCRIPT" plan "$PROFILE_ID" > "$TMP/plan.out" 2> "$TMP/plan.err" || {
    cat "$TMP/plan.err" >&2 || true
    fail 'built-in provider route probe failed'
}

grep -Fq "PROFILE_ID=$PROFILE_ID" "$TMP/plan.out" || fail 'profile id missing'
grep -Fq 'CANDIDATE_XRAY_VALID=yes' "$TMP/plan.out" || fail 'candidate Xray validation missing'
grep -Fq 'CANDIDATE_ROUTE_OK=yes' "$TMP/plan.out" || fail 'built-in route probe did not prove application route'
grep -Fq 'MUTATION=NONE' "$TMP/plan.out" || fail 'plan must remain read-only'
[ ! -e "$TMP/probe-listener.port" ] || fail 'built-in probe left fake listener state behind'
if grep -Eq 'TEST-ID-A|TEST-PBK-A|TEST-SID-A|private-token|vless://' "$TMP/plan.out" "$TMP/plan.err"; then
    fail 'built-in route probe leaked provider credentials'
fi


PATH="$TMP/bin:$PATH" \
FAKE_SUB_FIXTURE="$TMP/sub.fixture" \
FAKE_XRAY_LISTENER_FILE="$TMP/probe-listener.port" \
FREENET_SUB_FILE="$TMP/sub.url" \
FREENET_CONFIG_DIR="$TMP/configs" \
FREENET_ASSET_DIR="$TMP/dat" \
FREENET_PROFILE_FILE="$TMP/etc/vpn_profile_name" \
FREENET_FILTER_FILE="$TMP/profile.filter" \
FREENET_AUTOMATION_HISTORY="$TMP/history.log" \
FREENET_PROVIDER_SUBSCRIPTION_CACHE="$TMP/provider-subscription.lkg" \
FREENET_PROVIDER_SUBSCRIPTION_SOURCE="$TMP/provider-subscription.source" \
FREENET_XRAY_BIN="$TMP/bin/xray" \
FREENET_XKEEN_BIN="$TMP/bin/xkeen" \
FREENET_LOCK_DIR="$TMP/vpn-mutation.lock" \
FREENET_CURL_BIN="$TMP/bin/curl" \
FREENET_PROVIDER_ROUTE_PROBE_BIN="" \
sh "$SCRIPT" apply "$PROFILE_ID" > "$TMP/apply.out" 2> "$TMP/apply.err" || {
    cat "$TMP/apply.err" >&2 || true
    fail 'built-in provider route probe apply failed'
}

grep -Fq '[FreeNet Provider] RESULT=SUCCESS' "$TMP/apply.out" || fail 'built-in apply success marker missing'
grep -Fq '[FreeNet Provider] ROLLBACK=NOT_NEEDED' "$TMP/apply.out" || fail 'built-in apply rollback marker missing'
jq -e '([.outbounds[] | select(.tag=="vless-reality")] | length) == 1' "$TMP/configs/04_outbounds.json" >/dev/null || fail 'built-in apply did not install exactly one VPN outbound'
[ "$(cat "$TMP/etc/vpn_profile_name")" = "$PROFILE_NAME" ] || fail 'built-in apply did not persist exact profile label'
[ "$(cat "$TMP/profile.filter")" = "$PROFILE_NAME" ] || fail 'built-in apply did not persist exact profile filter'
[ ! -e "$TMP/probe-listener.port" ] || fail 'built-in post-apply probe left fake listener state behind'
if grep -Eq 'TEST-ID-A|TEST-PBK-A|TEST-SID-A|private-token|vless://' "$TMP/apply.out" "$TMP/apply.err"; then
    fail 'built-in apply leaked provider credentials'
fi

echo 'provider built-in route probe PASS'
