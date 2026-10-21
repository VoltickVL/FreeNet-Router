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
vless://TEST-ID-WS@203.0.113.10:443?security=tls&type=ws&fp=firefox&sni=tls.example.test&host=cdn.example.test&path=%2Fws#Frankfurt%2C%20Germany%2C%20Extra
vless://TEST-ID-GRPC@203.0.113.10:443?security=tls&type=grpc&sni=grpc.example.test#Paris%2C%20France%2C%20Extra
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

cat > "$TMP/bin/provider-route-probe" <<EOF
#!/bin/sh
MODE="\$(cat "$TMP/route-probe.mode" 2>/dev/null || echo pass)"
case "\$MODE" in
  fail-all) exit 1 ;;
  fail-live-always)
    [ "\$1" = "$TMP/configs/04_outbounds.json" ] && exit 1
    ;;
  fail-live-once)
    if [ "\$1" = "$TMP/configs/04_outbounds.json" ]; then
      COUNT=0
      [ -f "$TMP/route-probe-live.count" ] && COUNT="\$(cat "$TMP/route-probe-live.count")"
      COUNT=\$((COUNT+1))
      printf '%s\n' "\$COUNT" > "$TMP/route-probe-live.count"
      [ "\$COUNT" -eq 1 ] && exit 1
    fi
    ;;
esac
[ -s "\$1" ] || exit 1
exit 0
EOF
chmod 755 "$TMP/bin/provider-route-probe"
printf '%s\n' pass > "$TMP/route-probe.mode"

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
PROFILE_ID="$(printf '%s|%s|%s|%s|%s|%s' "$PROFILE_NAME" 'reality' 'tcp' 'example.test' '' '' | sha256sum | awk '{print substr($1,1,16)}')"
WS_PROFILE_ID="$(printf '%s|%s|%s|%s|%s|%s' "$PROFILE_NAME" 'tls' 'ws' 'tls.example.test' 'cdn.example.test' '/ws' | sha256sum | awk '{print substr($1,1,16)}')"
HISTORY_FILE="$TMP/history.log"

run_helper() {
    PATH="$TMP/bin:$PATH" \
    FREENET_SUB_FILE="$TMP/sub.url" \
    FREENET_CONFIG_DIR="$TMP/configs" \
    FREENET_ASSET_DIR="$TMP/dat" \
    FREENET_PROFILE_FILE="$TMP/etc/vpn_profile_name" \
    FREENET_FILTER_FILE="$TMP/profile.filter" \
    FREENET_AUTOMATION_HISTORY="$HISTORY_FILE" \
    FREENET_PROVIDER_SUBSCRIPTION_CACHE="$TMP/provider-subscription.lkg" \
    FREENET_PROVIDER_SUBSCRIPTION_SOURCE="$TMP/provider-subscription.source" \
    FREENET_XRAY_BIN="$TMP/bin/xray" \
    FREENET_XKEEN_BIN="$TMP/bin/xkeen" \
    FREENET_XRAY_CORE_RESTART_HELPER="${CORE_HELPER:-}" \
    FREENET_LOCK_DIR="$TMP/vpn-mutation.lock" \
    FREENET_VPN_TRANSACTION_DIR="$TMP/provider-transaction.pending" \
    FREENET_AUTOMATION_STATE="$TMP/automation.state" \
    FREENET_CURL_BIN="$TMP/bin/curl" \
    FREENET_PROVIDER_ROUTE_PROBE_BIN="$TMP/bin/provider-route-probe" \
    sh "$SCRIPT" "$@"
}

# plan must be read-only and secret-safe.
OUT_HASH_BEFORE="$(sha256sum "$TMP/configs/04_outbounds.json" | awk '{print $1}')"
FILTER_BEFORE="$(cat "$TMP/profile.filter")"
if ! run_helper plan "$PROFILE_ID" > "$TMP/plan.out" 2> "$TMP/plan.err"; then
    cat "$TMP/plan.err" >&2 || true
    fail 'initial Reality/TCP provider plan failed'
fi
OUT_HASH_AFTER="$(sha256sum "$TMP/configs/04_outbounds.json" | awk '{print $1}')"
[ "$OUT_HASH_BEFORE" = "$OUT_HASH_AFTER" ] || fail 'plan mutated 04_outbounds.json'
[ "$FILTER_BEFORE" = "$(cat "$TMP/profile.filter")" ] || fail 'plan mutated active profile filter'
[ ! -e "$TMP/etc/vpn_profile_name" ] || fail 'plan persisted preferred profile'
[ ! -e "$HISTORY_FILE" ] || fail 'plan wrote provider switch journal event'
grep -Fq "PROFILE_ID=$PROFILE_ID" "$TMP/plan.out" || fail 'plan id missing'
grep -Fq "PROFILE_NAME=$PROFILE_NAME" "$TMP/plan.out" || fail 'plan name missing'
grep -Fq 'ENDPOINT=203.0.113.10:443' "$TMP/plan.out" || fail 'safe endpoint missing'
grep -Fq 'CANDIDATE_XRAY_VALID=yes' "$TMP/plan.out" || fail 'candidate Xray validation missing'
grep -Fq 'CANDIDATE_ROUTE_OK=yes' "$TMP/plan.out" || fail 'candidate application route validation missing'
grep -Fq 'MUTATION=NONE' "$TMP/plan.out" || fail 'plan must report MUTATION=NONE'
if grep -Eq 'TEST-ID-A|TEST-PBK|TEST-SID|private-token|vless://' "$TMP/plan.out" "$TMP/plan.err"; then
    fail 'plan leaked provider/subscription credentials'
fi

# The same endpoint/name with a different supported transport must have a
# distinct logical ID and render the exact TLS/WS stream settings.
[ "$PROFILE_ID" != "$WS_PROFILE_ID" ] || fail 'transport variants collapsed to one profile id'
if ! run_helper apply "$WS_PROFILE_ID" > "$TMP/ws-apply.out" 2> "$TMP/ws-apply.err"; then
    cat "$TMP/ws-apply.err" >&2 || true
    fail 'TLS/WS provider apply failed'
fi
grep -Fq '[FreeNet Provider] RESULT=SUCCESS' "$TMP/ws-apply.out" || fail 'TLS/WS provider apply failed'
jq -e '
  ([.outbounds[] | select(.tag=="vless-reality")] | length)==1 and
  (.outbounds[] | select(.tag=="vless-reality") | .streamSettings.network)=="ws" and
  (.outbounds[] | select(.tag=="vless-reality") | .streamSettings.security)=="tls" and
  (.outbounds[] | select(.tag=="vless-reality") | .streamSettings.tlsSettings.serverName)=="tls.example.test" and
  (.outbounds[] | select(.tag=="vless-reality") | .streamSettings.wsSettings.path)=="/ws" and
  (.outbounds[] | select(.tag=="vless-reality") | .streamSettings.wsSettings.headers.Host)=="cdn.example.test" and
  ((.outbounds[] | select(.tag=="vless-reality") | .streamSettings | has("realitySettings")) | not)
' "$TMP/configs/04_outbounds.json" >/dev/null || fail 'TLS/WS provider render parity failed'
if grep -Eq 'TEST-ID-WS|private-token|vless://' "$TMP/ws-apply.out" "$TMP/ws-apply.err"; then
    fail 'TLS/WS apply leaked provider/subscription credentials'
fi

# Unsupported transports must not be addressable by provider profile ID.
GRPC_PROFILE_ID="$(printf '%s|%s|%s|%s|%s|%s' 'Paris, France, Extra' 'tls' 'grpc' 'grpc.example.test' '' '' | sha256sum | awk '{print substr($1,1,16)}')"
if run_helper plan "$GRPC_PROFILE_ID" > "$TMP/grpc.out" 2> "$TMP/grpc.err"; then
    fail 'unsupported gRPC transport unexpectedly became selectable'
fi

[ -s "$TMP/provider-subscription.lkg" ] || fail 'fresh plan did not persist secure provider LKG'
[ -s "$TMP/provider-subscription.source" ] || fail 'fresh plan did not persist provider source fingerprint'
[ "$(stat -c '%a' "$TMP/provider-subscription.lkg" 2>/dev/null || stat -f '%Lp' "$TMP/provider-subscription.lkg" 2>/dev/null)" = "600" ] || fail 'secure provider LKG mode is not 0600'

# Once a source-bound secure LKG exists, exact plan must not depend on another
# provider fetch. This is the real stale-catalog WORK/MOM regression.
cat > "$TMP/bin/curl" <<EOF
#!/bin/sh
echo called >> "$TMP/curl-after-cache.calls"
exit 1
EOF
chmod 755 "$TMP/bin/curl"
if ! run_helper plan "$PROFILE_ID" > "$TMP/cache-plan.out" 2> "$TMP/cache-plan.err"; then
    cat "$TMP/cache-plan.err" >&2 || true
    fail 'secure-LKG provider plan failed'
fi
grep -Fq 'CANDIDATE_XRAY_VALID=yes' "$TMP/cache-plan.out" || fail 'secure-LKG plan did not validate candidate'
[ ! -s "$TMP/curl-after-cache.calls" ] || fail 'matching secure LKG still performed provider fetch'
if grep -Eq 'TEST-ID-A|TEST-PBK|TEST-SID|private-token|vless://' "$TMP/cache-plan.out" "$TMP/cache-plan.err"; then
    fail 'secure-LKG plan leaked credentials'
fi

# A different subscription source must never reuse credential-bearing LKG.
cp "$TMP/sub.url" "$TMP/sub.url.good"
printf '%s\n' 'https://other-subscription.invalid/private-token' > "$TMP/sub.url"
CACHE_HASH_BEFORE="$(sha256sum "$TMP/configs/04_outbounds.json" | awk '{print $1}')"
FILTER_CACHE_BEFORE="$(cat "$TMP/profile.filter")"
if run_helper plan "$PROFILE_ID" > "$TMP/source-mismatch.out" 2> "$TMP/source-mismatch.err"; then
    fail 'source-mismatched secure LKG unexpectedly passed'
fi
[ "$CACHE_HASH_BEFORE" = "$(sha256sum "$TMP/configs/04_outbounds.json" | awk '{print $1}')" ] || fail 'source mismatch mutated live outbound'
[ "$FILTER_CACHE_BEFORE" = "$(cat "$TMP/profile.filter")" ] || fail 'source mismatch mutated active filter'
grep -Fq 'secure provider cache is missing or does not match' "$TMP/source-mismatch.err" || fail 'source mismatch did not fail with safe provider-cache reason'
if grep -Eq 'TEST-ID-A|TEST-PBK|TEST-SID|private-token|vless://' "$TMP/source-mismatch.out" "$TMP/source-mismatch.err"; then
    fail 'source mismatch leaked credentials'
fi
mv "$TMP/sub.url.good" "$TMP/sub.url"

# Restore successful fresh-fetch stub for remaining tests that intentionally
# exercise direct shell behavior after deleting/overwriting state.
cat > "$TMP/bin/curl" <<EOF
#!/bin/sh
cat "$TMP/sub.fixture"
EOF
chmod 755 "$TMP/bin/curl"

# A fresh logical candidate that cannot prove an application route must fail
# before any live mutation.
printf '%s\n' fail-all > "$TMP/route-probe.mode"
PREFLIGHT_HASH="$(sha256sum "$TMP/configs/04_outbounds.json" | awk '{print $1}')"
PREFLIGHT_FILTER="$(cat "$TMP/profile.filter")"
if run_helper apply "$PROFILE_ID" > "$TMP/preflight-route.out" 2> "$TMP/preflight-route.err"; then
    fail 'provider apply mutated despite failed candidate application route'
fi
[ "$PREFLIGHT_HASH" = "$(sha256sum "$TMP/configs/04_outbounds.json" | awk '{print $1}')" ] || fail 'failed candidate route changed live outbound'
[ "$PREFLIGHT_FILTER" = "$(cat "$TMP/profile.filter")" ] || fail 'failed candidate route changed active filter'
grep -Fq 'candidate VPN application route validation failed' "$TMP/preflight-route.err" || fail 'candidate route failure reason missing'
printf '%s\n' pass > "$TMP/route-probe.mode"

# Provider mutation must share the updater lock and fail closed before touching live state.
mkdir -p "$TMP/vpn-mutation.lock"
printf '%s\n' "$$" > "$TMP/vpn-mutation.lock/pid"
LOCK_HASH="$(sha256sum "$TMP/configs/04_outbounds.json" | awk '{print $1}')"
LOCK_FILTER="$(cat "$TMP/profile.filter")"
if run_helper apply "$PROFILE_ID" > "$TMP/locked.out" 2> "$TMP/locked.err"; then
    fail 'provider apply unexpectedly ran while shared VPN mutation lock was held'
fi
[ "$LOCK_HASH" = "$(sha256sum "$TMP/configs/04_outbounds.json" | awk '{print $1}')" ] || fail 'busy provider apply changed live outbound'
[ "$LOCK_FILTER" = "$(cat "$TMP/profile.filter")" ] || fail 'busy provider apply changed active filter'
grep -Fq 'PRIMARY ERROR: another VPN mutation is already running' "$TMP/locked.err" || fail 'busy provider apply primary error missing'
grep -Fq 'ROLLBACK ERROR/STATE: no live apply' "$TMP/locked.err" || fail 'busy provider apply must be NOT_APPLIED'
rm -rf "$TMP/vpn-mutation.lock"

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

# If the new live VPN route fails after mutation, the same transaction must
# restore outbound/profile/filter and prove the restored route before reporting rollback success.
POST_HASH="$(sha256sum "$TMP/configs/04_outbounds.json" | awk '{print $1}')"
POST_PROFILE="$(cat "$TMP/etc/vpn_profile_name")"
POST_FILTER="$(cat "$TMP/profile.filter")"
rm -f "$TMP/route-probe-live.count"
printf '%s\n' fail-live-once > "$TMP/route-probe.mode"
if run_helper apply "$PROFILE_ID" > "$TMP/post-route.out" 2> "$TMP/post-route.err"; then
    fail 'post-apply route failure unexpectedly succeeded'
fi
[ "$POST_HASH" = "$(sha256sum "$TMP/configs/04_outbounds.json" | awk '{print $1}')" ] || fail 'post-apply route rollback did not restore outbound'
[ "$POST_PROFILE" = "$(cat "$TMP/etc/vpn_profile_name")" ] || fail 'post-apply route rollback did not restore preferred profile'
[ "$POST_FILTER" = "$(cat "$TMP/profile.filter")" ] || fail 'post-apply route rollback did not restore active filter'
grep -Fq 'PRIMARY ERROR: live VPN application route validation failed after provider apply' "$TMP/post-route.err" || fail 'post-apply route primary error missing'
grep -Fq 'ROLLBACK ERROR/STATE: rollback success' "$TMP/post-route.err" || fail 'post-apply route rollback was not verified'
printf '%s\n' pass > "$TMP/route-probe.mode"

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

# apply-core must fail closed before live mutation when Xray is stopped.
# This prevents rollback from changing an originally-stopped runtime into running.
cat > "$TMP/bin/pidof" <<'EOF'
#!/bin/sh
exit 1
EOF
chmod 755 "$TMP/bin/pidof"
cat > "$TMP/bin/xkeen" <<EOF
#!/bin/sh
printf '%s\n' "\$*" >> "$TMP/xkeen-stopped.calls"
exit 0
EOF
chmod 755 "$TMP/bin/xkeen"
STOPPED_HASH="$(sha256sum "$TMP/configs/04_outbounds.json" | awk '{print $1}')"
STOPPED_FILTER="$(cat "$TMP/profile.filter")"
STOPPED_PROFILE="$(cat "$TMP/etc/vpn_profile_name")"
if run_helper apply-core "$PROFILE_ID" > "$TMP/stopped-core.out" 2> "$TMP/stopped-core.err"; then
    fail 'apply-core unexpectedly mutated with stopped Xray'
fi
[ "$STOPPED_HASH" = "$(sha256sum "$TMP/configs/04_outbounds.json" | awk '{print $1}')" ] || fail 'stopped Xray apply-core changed outbound'
[ "$STOPPED_FILTER" = "$(cat "$TMP/profile.filter")" ] || fail 'stopped Xray apply-core changed filter'
[ "$STOPPED_PROFILE" = "$(cat "$TMP/etc/vpn_profile_name")" ] || fail 'stopped Xray apply-core changed preferred profile'
[ ! -s "$TMP/xkeen-stopped.calls" ] || fail 'stopped Xray apply-core invoked XKeen before STOP'
grep -Fq 'ROLLBACK ERROR/STATE: no live apply' "$TMP/stopped-core.err" || fail 'stopped Xray STOP was misclassified as live rollback'

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

# The original VPN may already be down when emergency failover starts.
# Restoring identical files and reviving Xray must not claim that unreachable
# original Internet service was restored, and must retain the durable STOP.
cat > "$TMP/bin/core-restart-old-down" <<EOF
#!/bin/sh
COUNT_FILE="$TMP/old-down-core.count"
COUNT=0
[ -f "\$COUNT_FILE" ] && COUNT="\$(cat "\$COUNT_FILE")"
COUNT=\$((COUNT+1))
printf '%s\n' "\$COUNT" > "\$COUNT_FILE"
[ "\$COUNT" -eq 1 ] && exit 7
exit 0
EOF
chmod 755 "$TMP/bin/core-restart-old-down"
CORE_HELPER="$TMP/bin/core-restart-old-down"
export CORE_HELPER
printf '%s\n' 'CORE OLD DOWN' > "$TMP/etc/vpn_profile_name"
printf '%s\n' 'CORE|DOWN' > "$TMP/profile.filter"
printf '%s\n' fail-live-always > "$TMP/route-probe.mode"
OLD_DOWN_HASH="$(sha256sum "$TMP/configs/04_outbounds.json" | awk '{print $1}')"
if run_helper apply-core "$PROFILE_ID" > "$TMP/old-down.out" 2> "$TMP/old-down.err"; then
    fail 'old VPN unreachable scenario unexpectedly reported success'
fi
[ "$OLD_DOWN_HASH" = "$(sha256sum "$TMP/configs/04_outbounds.json" | awk '{print $1}')" ] || fail 'old-down rollback did not restore outbound'
[ "$(cat "$TMP/etc/vpn_profile_name")" = 'CORE OLD DOWN' ] || fail 'old-down rollback profile mismatch'
[ "$(cat "$TMP/profile.filter")" = 'CORE|DOWN' ] || fail 'old-down rollback filter mismatch'
[ "$(cat "$TMP/old-down-core.count")" = '2' ] || fail 'old-down rollback did not restart restored Xray'
grep -Fq 'stage=restart-helper; code=7' "$TMP/old-down.err" || fail 'core restart stage and numeric code missing'
grep -Fq 'ROLLBACK DETAIL: files=RESTORED; core=RESTORED; vpn=UNREACHABLE' "$TMP/old-down.err" || fail 'old-down rollback phases conflated'
grep -Fq 'ROLLBACK ERROR/STATE: rollback success' "$TMP/old-down.err" || fail 'verified file/core restoration conflated with old VPN reachability'
[ -d "$TMP/provider-transaction.pending" ] || fail 'old-down rollback discarded private checkpoint'
if run_helper apply-core "$PROFILE_ID" > "$TMP/old-down-blocked.out" 2> "$TMP/old-down-blocked.err"; then
    fail 'pending rollback unexpectedly allowed another VPN apply'
fi
grep -Fq 'pending VPN transaction requires read-only reconciliation' "$TMP/old-down-blocked.err" || fail 'old-down durable guard missing'
if grep -Eq 'TEST-ID-A|TEST-PBK|TEST-SID|private-token|vless://' "$TMP/old-down.out" "$TMP/old-down.err" "$TMP/old-down-blocked.err"; then
    fail 'old-down stage detail leaked credentials'
fi
# Production recovery: explicit user-selected VPN may recover from the exact
# old-down pending checkpoint. The original snapshot and ACTUAL live snapshot
# must both survive, then the selected HTTPS works and the checkpoint archives.
CORE_HELPER="$TMP/bin/core-restart"
export CORE_HELPER
printf '%s\n' pass > "$TMP/route-probe.mode"
RECOVERY_ORIGINAL_HASH="$(sha256sum "$TMP/provider-transaction.pending/out.before" | awk '{print $1}')"
if run_helper recover-core "$PROFILE_ID" > "$TMP/recovery-no-confirm.out" 2> "$TMP/recovery-no-confirm.err"; then
    fail 'manual recovery accepted missing explicit confirmation'
fi
[ -d "$TMP/provider-transaction.pending" ] || fail 'unconfirmed recovery removed original checkpoint'
[ ! -e "$TMP/provider-transaction.pending/recovery.pending" ] || fail 'unconfirmed recovery created new snapshot'

ln -s "$TMP/configs" "$TMP/provider-transaction.pending/recovery.pending"
if FREENET_PROVIDER_RECOVERY_MANUAL=1 FREENET_PROVIDER_RTT_MANUAL=1 run_helper recover-core "$PROFILE_ID" > "$TMP/recovery-interrupted.out" 2> "$TMP/recovery-interrupted.err"; then
    fail 'incomplete prior recovery unexpectedly allowed another mutation'
fi
[ "$(sha256sum "$TMP/configs/04_outbounds.json" | awk '{print $1}')" = "$OLD_DOWN_HASH" ] || fail 'interrupted recovery changed live outbound'
rm -f "$TMP/provider-transaction.pending/recovery.pending"

if ! FREENET_PROVIDER_RECOVERY_MANUAL=1 FREENET_PROVIDER_RTT_MANUAL=1 run_helper recover-core "$PROFILE_ID" > "$TMP/recovery-ok.out" 2> "$TMP/recovery-ok.err"; then
    cat "$TMP/recovery-ok.err" >&2 || true
    fail 'manual recovery could not safely replace a pending AUTO VPN server'
fi
grep -Fq '[FreeNet Provider] RECOVERY=COMPLETED' "$TMP/recovery-ok.out" || fail 'manual recovery missing durable terminal receipt'
grep -Fq '[FreeNet Provider] RESULT=SUCCESS' "$TMP/recovery-ok.out" || fail 'manual recovery did not confirm live HTTPS'
[ ! -e "$TMP/provider-transaction.pending" ] || fail 'accepted manual recovery left stale pending STOP'
RECOVERY_ARCHIVE="$(find "$TMP/vpn-transaction.recovered" -mindepth 1 -maxdepth 1 -type d | head -n 1)"
[ -n "$RECOVERY_ARCHIVE" ] && [ -d "$RECOVERY_ARCHIVE" ] || fail 'manual recovery discarded original protected evidence'
[ "$(sha256sum "$RECOVERY_ARCHIVE/out.before" | awk '{print $1}')" = "$RECOVERY_ORIGINAL_HASH" ] || fail 'manual recovery changed original snapshot'
[ -s "$RECOVERY_ARCHIVE/recovery.pending/out.before" ] || fail 'manual recovery lost second live snapshot'
[ -s "$RECOVERY_ARCHIVE/recovery.pending/state" ] || fail 'manual recovery lost second durable marker'
[ "$(cat "$TMP/etc/vpn_profile_name")" = "$PROFILE_NAME" ] || fail 'manual recovery failed to persist selected server'
if grep -Eq 'TEST-ID-A|TEST-PBK|TEST-SID|private-token|vless://' "$TMP/recovery-ok.out" "$TMP/recovery-ok.err"; then
    fail 'manual recovery leaked credentials'
fi
unset CORE_HELPER

# Reset only isolated test fixtures, never an actual router checkpoint.
rm -rf "$TMP/provider-transaction.pending" "$TMP/vpn-mutation.lock"
printf '%s\n' pass > "$TMP/route-probe.mode"
unset CORE_HELPER

# Source contract: endpoint-only mode must keep XKeen/netfilter ownership,
# use canonical synchronous XKeen start after the controlled Xray kill, and
# never fall back to full XKeen restart.
grep -Fq '[ "$MODE" = apply-core ]' "$SCRIPT" || fail 'apply-core mode dispatch missing'
grep -Fq 'restart_xray_core no' "$SCRIPT" || fail 'apply-core does not use core-only restart'
grep -Fq 'restart_xray_core yes' "$SCRIPT" || fail 'apply-core rollback does not use core-only restart'
grep -Fq 'XKEEN_FOREGROUND=1 production_core_command "$XKEEN_BIN" -start' "$SCRIPT" || fail 'core-only restart must use canonical synchronous XKeen start'
if sed -n '/restart_xray_core() {/,/^}/p' "$SCRIPT" | grep -Fq 'nohup "$XRAY_BIN" run'; then
    fail 'core-only restart bypasses canonical XKeen start with direct Xray launch'
fi
grep -Fq '"$XKEEN_BIN" -restart' "$SCRIPT" || fail 'normal provider apply restart semantics were unexpectedly removed'
grep -Fq 'FREENET_LOCK_DIR:-/tmp/blanc_xkeen_update.lock' "$SCRIPT" || fail 'provider helper does not share canonical updater lock'
grep -Fq 'FREENET_LOCK_DIR:-/tmp/blanc_xkeen_update.lock' "$ROOT_DIR/scripts/blanc_xkeen_update_outbounds.sh" || fail 'updater helper canonical lock contract changed'
grep -Fq 'tag:"direct",protocol:"freedom",streamSettings:{sockopt:{mark:255}}' "$SCRIPT" || fail 'new DIRECT outbound is missing XKeen self-bypass mark 255'

# Fault injection: abruptly kill the writer after the first committed live
# outbound. Persistent private snapshots and STOP must survive the process.
cat > "$TMP/bin/mv" <<EOF
#!/bin/sh
if [ "\$3" = "$TMP/configs/04_outbounds.json" ]; then
    /bin/mv "\$@" || exit 3
    PARENT="\$(ps -o ppid= -p "\$\$" | tr -d ' ')"
    kill -KILL "\$PARENT"
    exit 9
fi
exec /bin/mv "\$@"
EOF
chmod 755 "$TMP/bin/mv"
rm -rf "$TMP/vpn-mutation.lock"
if run_helper apply "$PROFILE_ID" > "$TMP/killed-apply.out" 2> "$TMP/killed-apply.err"; then
    fail 'fault injection did not kill provider helper'
fi
[ -d "$TMP/provider-transaction.pending" ] || fail 'SIGKILL lost durable VPN checkpoint'
[ -s "$TMP/provider-transaction.pending/out.before" ] || fail 'SIGKILL lost private outbound backup'
[ -s "$TMP/provider-transaction.pending/profile.before" ] || fail 'SIGKILL lost private profile backup'
[ -s "$TMP/provider-transaction.pending/filter.before" ] || fail 'SIGKILL lost private filter backup'
[ -s "$TMP/provider-transaction.pending/state" ] || fail 'SIGKILL lost transaction marker'
[ "$(stat -c %a "$TMP/provider-transaction.pending")" = 700 ] || fail 'checkpoint directory permissions are unsafe'
for ITEM in out.before profile.before filter.before state; do
    [ "$(stat -c %a "$TMP/provider-transaction.pending/$ITEM")" = 600 ] || fail 'checkpoint file permissions are unsafe'
done
rm -f "$TMP/bin/mv"
if run_helper apply "$PROFILE_ID" > "$TMP/blocked-apply.out" 2> "$TMP/blocked-apply.err"; then
    fail 'a second apply crossed PENDING checkpoint'
fi
grep -Fq 'pending VPN transaction requires read-only reconciliation' "$TMP/blocked-apply.err" ||
    fail 'PENDING reason was not reported'
if run_helper core-restart > "$TMP/blocked-core.out" 2> "$TMP/blocked-core.err"; then
    fail 'core restart crossed PENDING checkpoint'
fi
if grep -Eq 'TEST-ID-A|TEST-PBK|TEST-SID|private-token|vless://' "$TMP/blocked-apply.err" "$TMP/blocked-core.err" "$TMP/killed-apply.err"; then
    fail 'checkpoint path leaked credentials in diagnostics'
fi

# Linux kill -0 also succeeds for zombie PIDs; the core-only cutover must
# distinguish a retired core, an uninterruptible live core and a reused PID.
LIFECYCLE_FUNC="$TMP/xray-lifecycle-function.sh"
sed -n '/^xray_pid_confirmed_gone() {/,/^}/p; /^xray_pid_lifecycle() {/,/^}/p' "$SCRIPT" > "$LIFECYCLE_FUNC"
[ -s "$LIFECYCLE_FUNC" ] || fail 'PID lifecycle reader is missing'
. "$LIFECYCLE_FUNC"
PROC_FIX="$TMP/proc-fixture"
mkdir -p "$PROC_FIX"
# /proc/self/stat in awk exposes the live parent test shell PID.
TEST_PID="$(awk '{print $4}' /proc/self/stat)"
case "$TEST_PID" in ''|*[!0-9]*) fail 'test PID was not numeric' ;; esac
write_process_state() {
    FIX_PID="$1"
    FIX_START="$2"
    FIX_STATE="$3"
    mkdir -p "$PROC_FIX/$FIX_PID"
    printf '%s (xray) %s' "$FIX_PID" "$FIX_STATE" > "$PROC_FIX/$FIX_PID/stat"
    FIX_FIELD=4
    while [ "$FIX_FIELD" -lt 22 ]; do
        printf ' 0' >> "$PROC_FIX/$FIX_PID/stat"
        FIX_FIELD=$((FIX_FIELD + 1))
    done
    printf ' %s 0\n' "$FIX_START" >> "$PROC_FIX/$FIX_PID/stat"
    printf 'Name:\txray\nState:\t%s (fixture)\n' "$FIX_STATE" > "$PROC_FIX/$FIX_PID/status"
}
write_process_state "$TEST_PID" 123456 Z
kill -0 "$TEST_PID" 2>/dev/null || fail 'PID-0 fixture does not reproduce a live kernel PID'
[ "$(xray_pid_lifecycle "$TEST_PID" 123456 "$PROC_FIX")" = zombie ] ||
    fail 'zombie was misclassified as running by PID lifecycle reader'
write_process_state "$TEST_PID" 123456 S
[ "$(xray_pid_lifecycle "$TEST_PID" 123456 "$PROC_FIX")" = live:S ] ||
    fail 'live Xray state was misclassified as exited'
write_process_state "$TEST_PID" 123456 D
[ "$(xray_pid_lifecycle "$TEST_PID" 123456 "$PROC_FIX")" = live:D ] ||
    fail 'D-state Xray must remain a live unsafe process'
[ "$(xray_pid_lifecycle "$TEST_PID" 999999 "$PROC_FIX")" = reused ] ||
    fail 'reused Xray PID was not detected'
printf 'truncated stat' > "$PROC_FIX/$TEST_PID/stat"
[ "$(xray_pid_lifecycle "$TEST_PID" 123456 "$PROC_FIX")" = unknown ] ||
    fail 'uninspectable core PID must remain UNKNOWN'
[ "$(xray_pid_lifecycle 999999 123456 "$PROC_FIX")" = gone ] ||
    fail 'absent core PID must be classified as gone'
# These apply to both the forward and rollback core-only paths.
grep -Fq 'xray_pid_lifecycle "$OLD_PID" "$OLD_START"' "$SCRIPT" ||
    fail 'core cutover does not inspect process identity during termination'
grep -Fq 'CORE_RESTART_EXIT="old-core-pid-reused"' "$SCRIPT" ||
    fail 'PID reuse does not fail closed'
grep -Fq 'CORE_RESTART_EXIT="old-core-uninterruptible"' "$SCRIPT" ||
    fail 'uninterruptible core has no specific STOP result'
grep -Fq 'CORE_RESTART_STAGE="process-inventory-after-stop"' "$SCRIPT" ||
    fail 'restart can bypass the post-stop isolated process fence'

# The final pidof inventory must allow bounded reaping only for the
# original identity-verified zombie. A foreign process must never be ignored.
# The helper runs with TMP/bin prepended by run_helper. Source-level
# fixtures execute in this test shell and need the identical PATH.
PATH="$TMP/bin:$PATH"
export PATH
INVENTORY_FUNC="$TMP/xray-inventory-function.sh"
sed -n '/^wait_xray_post_stop_inventory() {/,/^}/p' "$SCRIPT" > "$INVENTORY_FUNC"
[ -s "$INVENTORY_FUNC" ] || fail 'post-stop Xray inventory helper is missing'
. "$INVENTORY_FUNC"
# Avoid 20 real sleeps in the unreaped-zombie fault injection.
short_pause() { :; }
OLD_PID="$TEST_PID"
OLD_START=123456
TEST_PIDOF_COUNT_FILE="$TMP/post-stop-pidof-count"
export TEST_PID TEST_PIDOF_COUNT_FILE
cat > "$TMP/bin/pidof" <<'EOF'
#!/bin/sh
[ "$1" = xray ] || exit 1
case "${TEST_PIDOF_MODE:-}" in
  zombie-clears)
    N=0
    [ -f "$TEST_PIDOF_COUNT_FILE" ] && N="$(cat "$TEST_PIDOF_COUNT_FILE")"
    N=$((N + 1))
    printf '%s\n' "$N" > "$TEST_PIDOF_COUNT_FILE"
    [ "$N" -le 2 ] && { printf '%s\n' "$TEST_PID"; exit 0; }
    exit 1
    ;;
  original)
    printf '%s\n' "$TEST_PID"
    ;;
  foreign)
    printf '%s\n' 424242
    ;;
  mixed)
    printf '%s %s\n' "$TEST_PID" 424242
    ;;
  *)
    exit 1
    ;;
esac
EOF
chmod 755 "$TMP/bin/pidof"

write_process_state "$TEST_PID" 123456 Z
TEST_PIDOF_MODE=zombie-clears
export TEST_PIDOF_MODE
rm -f "$TEST_PIDOF_COUNT_FILE"
CORE_RESTART_EXIT="not-run"
wait_xray_post_stop_inventory "$PROC_FIX" || fail 'original zombie did not clear in bounded inventory'
[ "$(cat "$TEST_PIDOF_COUNT_FILE")" = 3 ] || fail 'inventory did not wait for original zombie to disappear'

TEST_PIDOF_MODE=original
export TEST_PIDOF_MODE
CORE_RESTART_EXIT="not-run"
if wait_xray_post_stop_inventory "$PROC_FIX"; then
    fail 'unreaped original zombie wrongly allowed XKeen startup'
fi
[ "$CORE_RESTART_EXIT" = old-core-zombie-unreaped ] || fail 'unreaped zombie did not receive a specific STOP result'

write_process_state "$TEST_PID" 123456 S
CORE_RESTART_EXIT="not-run"
if wait_xray_post_stop_inventory "$PROC_FIX"; then
    fail 'live original process wrongly allowed XKeen startup'
fi
[ "$CORE_RESTART_EXIT" = old-core-still-running ] || fail 'live process was not classified as STOP'

write_process_state "$TEST_PID" 123456 D
CORE_RESTART_EXIT="not-run"
if wait_xray_post_stop_inventory "$PROC_FIX"; then
    fail 'uninterruptible Xray wrongly allowed XKeen startup'
fi
[ "$CORE_RESTART_EXIT" = old-core-uninterruptible ] || fail 'D-state process was not classified as STOP'

write_process_state "$TEST_PID" 999999 Z
CORE_RESTART_EXIT="not-run"
if wait_xray_post_stop_inventory "$PROC_FIX"; then
    fail 'reused zombie PID wrongly allowed XKeen startup'
fi
[ "$CORE_RESTART_EXIT" = old-core-pid-reused ] || fail 'reused zombie was not classified as STOP'

printf 'truncated stat' > "$PROC_FIX/$TEST_PID/stat"
CORE_RESTART_EXIT="not-run"
if wait_xray_post_stop_inventory "$PROC_FIX"; then
    fail 'unknown original identity wrongly allowed XKeen startup'
fi
[ "$CORE_RESTART_EXIT" = old-core-identity-unknown ] || fail 'unreadable identity was not classified as STOP'

write_process_state "$TEST_PID" 123456 Z
TEST_PIDOF_MODE=foreign
export TEST_PIDOF_MODE
CORE_RESTART_EXIT="not-run"
if wait_xray_post_stop_inventory "$PROC_FIX"; then
    fail 'foreign Xray PID was ignored'
fi
[ "$CORE_RESTART_EXIT" = unexpected-xray-process ] || fail 'foreign process lacked fail-closed STOP'

TEST_PIDOF_MODE=mixed
export TEST_PIDOF_MODE
CORE_RESTART_EXIT="not-run"
if wait_xray_post_stop_inventory "$PROC_FIX"; then
    fail 'original zombie + foreign Xray were wrongly allowed'
fi
[ "$CORE_RESTART_EXIT" = unexpected-xray-process ] || fail 'mixed inventory lacked fail-closed STOP'

OLD_PID=""
TEST_PIDOF_MODE=foreign
export TEST_PIDOF_MODE
CORE_RESTART_EXIT="not-run"
if wait_xray_post_stop_inventory "$PROC_FIX"; then
    fail 'unexpected Xray was allowed without a captured old identity'
fi
[ "$CORE_RESTART_EXIT" = unexpected-xray-process ] || fail 'unknown old identity was not STOP'
[ -z "$(grep -E '(^|[[:space:];])kill[[:space:]]' "$INVENTORY_FUNC" || true)" ] || fail 'post-stop inventory must not signal any PID'


# Exercise the actual proc reader while the task vanishes after the readable
# check but before awk opens stat. All process visibility/signals below are
# fixture functions in a subshell: no real task is signalled or started.
(
    PROC_FIX="$TMP/proc-exit-race"
    TEST_PID=314159
    TEST_NEW_PID=314160
    TEST_READ_MODE="$TMP/core-race-read-mode"
    TEST_READ_COUNT="$TMP/core-race-read-count"
    TEST_READ_EVENTS="$TMP/core-race-read-events"
    TEST_LIVE="$TMP/core-race-live"
    TEST_NEW_LIVE="$TMP/core-race-new-live"
    TEST_INVENTORY="$TMP/core-race-inventory"
    TEST_SIGNALS="$TMP/core-race-signals"
    TEST_STARTS="$TMP/core-race-starts"
    TEST_UNEXPECTED="$TMP/core-race-unexpected"
    TEST_PERMIT_TERM=no
    TEST_TERM_MODE=final-gone
    export TEST_NEW_PID TEST_NEW_LIVE TEST_INVENTORY TEST_STARTS

    prepare_lifecycle_fixture() {
        rm -rf "$PROC_FIX"
        mkdir -p "$PROC_FIX"
        write_process_state "$TEST_PID" 123456 S
        printf 'normal\n' > "$TEST_READ_MODE"
        printf '0\n' > "$TEST_READ_COUNT"
        printf 'yes\n' > "$TEST_LIVE"
        printf 'no\n' > "$TEST_NEW_LIVE"
        printf '%s\n' "$TEST_PID" > "$TEST_INVENTORY"
        : > "$TEST_READ_EVENTS"
        : > "$TEST_SIGNALS"
        : > "$TEST_STARTS"
        : > "$TEST_UNEXPECTED"
        TEST_PERMIT_TERM=no
    }

    kill() {
        if [ "$#" -eq 2 ] && [ "$1" = -0 ]; then
            case "$2" in
                "$TEST_PID") [ "$(cat "$TEST_LIVE")" = yes ]; return $? ;;
                "$TEST_NEW_PID") [ "$(cat "$TEST_NEW_LIVE")" = yes ]; return $? ;;
            esac
        elif [ "$#" -eq 1 ] && [ "$1" = "$TEST_PID" ] && [ "$TEST_PERMIT_TERM" = yes ]; then
            printf 'TERM\n' >> "$TEST_SIGNALS"
            write_process_state "$TEST_PID" 123456 Z
            printf '%s\n' "$TEST_TERM_MODE" > "$TEST_READ_MODE"
            printf '0\n' > "$TEST_READ_COUNT"
            return 0
        fi
        printf 'unexpected-signal\n' >> "$TEST_UNEXPECTED"
        return 1
    }

    awk() {
        [ "$#" -eq 2 ] || { printf 'unexpected-awk\n' >> "$TEST_UNEXPECTED"; return 2; }
        TEST_AWK_FILE="$2"
        # restart_xray_core has a literal initial /proc read. Redirect only
        # this exact synthetic PID; the production reader uses its fixture root.
        [ "$TEST_AWK_FILE" != "/proc/$TEST_PID/stat" ] || TEST_AWK_FILE="$PROC_FIX/$TEST_PID/stat"
        if [ "$TEST_AWK_FILE" = "$PROC_FIX/$TEST_PID/stat" ]; then
            TEST_AWK_MODE="$(cat "$TEST_READ_MODE")"
            case "$TEST_AWK_MODE" in
                final-gone|final-visible-empty)
                    TEST_AWK_COUNT="$(cat "$TEST_READ_COUNT")"
                    TEST_AWK_COUNT=$((TEST_AWK_COUNT + 1))
                    printf '%s\n' "$TEST_AWK_COUNT" > "$TEST_READ_COUNT"
                    if [ "$TEST_AWK_COUNT" -eq 2 ]; then
                        case "$TEST_AWK_MODE" in final-gone) TEST_AWK_MODE=gone ;; *) TEST_AWK_MODE=visible-empty ;; esac
                    else
                        TEST_AWK_MODE=normal
                    fi
                    ;;
            esac
            case "$TEST_AWK_MODE" in
                gone|gone-live|visible-empty)
                    [ -r "$TEST_AWK_FILE" ] || { printf 'race-not-readable\n' >> "$TEST_UNEXPECTED"; return 2; }
                    printf '%s\n' "$TEST_AWK_MODE" >> "$TEST_READ_EVENTS"
                    printf 'normal\n' > "$TEST_READ_MODE"
                    if [ "$TEST_AWK_MODE" = visible-empty ]; then
                        : > "$TEST_AWK_FILE"
                    else
                        rm -rf "$PROC_FIX/$TEST_PID"
                        : > "$TEST_INVENTORY"
                    fi
                    [ "$TEST_AWK_MODE" = gone-live ] || printf 'no\n' > "$TEST_LIVE"
                    ;;
                error)
                    printf 'error\n' >> "$TEST_READ_EVENTS"
                    return 2
                    ;;
            esac
        fi
        case "$TEST_AWK_FILE" in
            "$PROC_FIX/$TEST_PID/stat"|"$PROC_FIX/$TEST_PID/status")
                command awk "$1" "$TEST_AWK_FILE"
                ;;
            *) printf 'unexpected-proc-read\n' >> "$TEST_UNEXPECTED"; return 2 ;;
        esac
    }

    assert_lifecycle_case() {
        TEST_CASE="$1"
        TEST_EXPECTED_START="$2"
        TEST_WANT="$3"
        prepare_lifecycle_fixture
        case "$TEST_CASE" in
            gone|gone-live|visible-empty|error) printf '%s\n' "$TEST_CASE" > "$TEST_READ_MODE" ;;
            missing-stat) rm -f "$PROC_FIX/$TEST_PID/stat" ;;
            missing-stat-denied) rm -f "$PROC_FIX/$TEST_PID/stat"; printf 'denied\n' > "$TEST_LIVE" ;;
            missing-status) rm -f "$PROC_FIX/$TEST_PID/status"; printf 'no\n' > "$TEST_LIVE" ;;
            absent) rm -rf "$PROC_FIX/$TEST_PID"; printf 'no\n' > "$TEST_LIVE" ;;
            missing-root) rm -rf "$PROC_FIX"; printf 'no\n' > "$TEST_LIVE" ;;
            dangling-pid) rm -rf "$PROC_FIX/$TEST_PID"; ln -s "$PROC_FIX/missing-task" "$PROC_FIX/$TEST_PID"; printf 'no\n' > "$TEST_LIVE" ;;
            reused) write_process_state "$TEST_PID" 999999 S ;;
            zombie) write_process_state "$TEST_PID" 123456 Z ;;
            unknown-state) write_process_state "$TEST_PID" 123456 '?' ;;
            truncated) printf 'truncated stat\n' > "$PROC_FIX/$TEST_PID/stat" ;;
            *) fail 'unknown lifecycle fixture case' ;;
        esac
        if TEST_GOT="$(xray_pid_lifecycle "$TEST_PID" "$TEST_EXPECTED_START" "$PROC_FIX")"; then :; else fail "lifecycle $TEST_CASE returned a reader error"; fi
        [ "$TEST_GOT" = "$TEST_WANT" ] || fail "lifecycle $TEST_CASE: expected $TEST_WANT, got $TEST_GOT"
        [ ! -s "$TEST_SIGNALS" ] && [ ! -s "$TEST_UNEXPECTED" ] || fail "lifecycle $TEST_CASE escaped read-only fixture"
        case "$TEST_CASE" in
            gone|gone-live|visible-empty|error)
                [ "$(cat "$TEST_READ_EVENTS")" = "$TEST_CASE" ] || fail "lifecycle $TEST_CASE did not reach the actual read boundary"
                ;;
        esac
    }

    assert_lifecycle_case gone 123456 gone
    assert_lifecycle_case gone-live 123456 unknown
    assert_lifecycle_case visible-empty 123456 unknown
    assert_lifecycle_case error 123456 unknown
    assert_lifecycle_case missing-stat 123456 unknown
    assert_lifecycle_case missing-stat-denied 123456 unknown
    assert_lifecycle_case missing-status 123456 unknown
    assert_lifecycle_case absent '' unknown
    assert_lifecycle_case absent invalid unknown
    assert_lifecycle_case missing-root 123456 unknown
    assert_lifecycle_case dangling-pid 123456 unknown
    assert_lifecycle_case reused 123456 reused
    assert_lifecycle_case zombie 123456 zombie
    assert_lifecycle_case unknown-state 123456 unknown
    assert_lifecycle_case truncated 123456 unknown

    # Run the production preflight and termination loop. Only the proc root
    # is injected into the real reader; no lifecycle result is stubbed.
    LOOP_FUNCTIONS="$TMP/core-race-loop-functions.sh"
    sed -n '/^production_core_command() (/,/^)/p; /^single_xray_pid() {/,/^}/p; /^core_restart_preflight() {/,/^}/p; /^restart_xray_core() {/,/^}/p' "$SCRIPT" > "$LOOP_FUNCTIONS"
    . "$LOOP_FUNCTIONS"
    READER_FUNCTION="$TMP/core-race-reader-function.sh"
    sed -n '/^xray_pid_lifecycle() {/,/^}/p' "$SCRIPT" | sed '1s/xray_pid_lifecycle/xray_pid_lifecycle_fixture/' > "$READER_FUNCTION"
    . "$READER_FUNCTION"
    xray_pid_lifecycle() { xray_pid_lifecycle_fixture "$1" "$2" "$PROC_FIX"; }
    pidof() {
        [ "$#" -eq 1 ] && [ "$1" = xray ] || { printf 'unexpected-pidof\n' >> "$TEST_UNEXPECTED"; return 1; }
        [ -s "$TEST_INVENTORY" ] || return 1
        cat "$TEST_INVENTORY"
    }
    short_pause() { :; }
    CORE_RESTART_HELPER=""
    XKEEN_BIN="$TMP/bin/core-race-xkeen"
    cat > "$XKEEN_BIN" <<'EOF'
#!/bin/sh
[ "$#" -eq 1 ] && [ "$1" = -start ] && [ "$XKEEN_FOREGROUND" = 1 ] || exit 23
printf 'start\n' >> "$TEST_STARTS"
printf '%s\n' "$TEST_NEW_PID" > "$TEST_INVENTORY"
printf 'yes\n' > "$TEST_NEW_LIVE"
EOF
    chmod 755 "$XKEEN_BIN"

    prepare_lifecycle_fixture
    TEST_PERMIT_TERM=yes
    TEST_TERM_MODE=final-gone
    core_restart_preflight || fail 'production preflight rejected the known fixture core'
    restart_xray_core yes || fail "production termination race: $CORE_RESTART_STAGE/$CORE_RESTART_EXIT"
    [ "$CORE_RESTART_STAGE" = ready ] && [ "$CORE_RESTART_EXIT" = 0 ] || fail 'retired core did not reach readiness'
    [ "$(cat "$TEST_READ_COUNT")" = 2 ] && [ "$(cat "$TEST_READ_EVENTS")" = gone ] || fail 'race was not injected on the final identity read'
    [ "$(cat "$TEST_SIGNALS")" = TERM ] && [ "$(cat "$TEST_STARTS")" = start ] || fail 'cutover did not use exactly one TERM and one startup'
    [ ! -s "$TEST_UNEXPECTED" ] || fail 'production cutover escaped the isolated fixture'

    prepare_lifecycle_fixture
    TEST_PERMIT_TERM=yes
    TEST_TERM_MODE=final-visible-empty
    core_restart_preflight || fail 'negative fixture preflight unexpectedly failed'
    if restart_xray_core yes; then fail 'uninspectable visible task permitted startup'; fi
    [ "$CORE_RESTART_STAGE" = terminate-old-core ] && [ "$CORE_RESTART_EXIT" = old-core-identity-unknown ] || fail 'visible task lost its fail-closed identity result'
    [ "$(cat "$TEST_READ_COUNT")" = 2 ] && [ "$(cat "$TEST_READ_EVENTS")" = visible-empty ] || fail 'negative final-read race was not reached'
    [ "$(cat "$TEST_SIGNALS")" = TERM ] && [ ! -s "$TEST_STARTS" ] || fail 'unknown core triggered a forced signal or startup'
    [ ! -s "$TEST_UNEXPECTED" ] || fail 'negative cutover escaped the isolated fixture'
)

echo 'provider profile apply test PASS'
