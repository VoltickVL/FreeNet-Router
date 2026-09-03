#!/bin/sh
set -eu

ROOT="$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT HUP INT TERM

FILTER="$TMP/filter.regex"
CONFIG_DIR="$TMP/configs"
ASSET_DIR="$TMP/dat"
UPDATER="$TMP/updater"
SUB_FILE="$TMP/subscription.url"
CURL_MOCK="$TMP/curl"
CURL_DATA="$TMP/curl.data"
CURL_MODE="$TMP/curl.mode"
CURL_COUNT="$TMP/curl.count"
UPDATER_CALLED="$TMP/updater.called"
XRAY_MOCK="$TMP/xray"
PIDOF_MOCK="$TMP/pidof"
XRAY_STATE="$TMP/xray.state"
FAILOVER_STATE="$TMP/failover.last"
mkdir -p "$CONFIG_DIR" "$ASSET_DIR"

printf '%s\n' 'Frankfurt|Germany|Германия' > "$FILTER"
printf '%s\n' 'https://subscription.example/key' > "$SUB_FILE"

cat > "$UPDATER" <<'EOF'
#!/bin/sh
[ "${FREENET_ACTION_REASON:-}" = "switch" ] || exit 20
[ -n "${FREENET_FILTER_OVERRIDE:-}" ] || exit 21
exit 7
EOF
chmod 755 "$UPDATER"

if FREENET_FILTER_FILE="$FILTER" \
   FREENET_CONFIG_DIR="$CONFIG_DIR" \
   FREENET_UPDATER_BIN="$UPDATER" \
   sh "$ROOT/scripts/vpn" pl >/dev/null 2>&1; then
    echo "vpn switch unexpectedly succeeded" >&2
    exit 1
fi

grep -q 'Germany' "$FILTER"
! grep -q 'Poland' "$FILTER"

cat > "$UPDATER" <<'EOF'
#!/bin/sh
[ "${FREENET_ACTION_REASON:-}" = "switch" ] || exit 30
[ -n "${FREENET_FILTER_OVERRIDE:-}" ] || exit 31
printf '%s\n' "$FREENET_FILTER_OVERRIDE" > "$FREENET_FILTER_FILE"
exit 0
EOF
chmod 755 "$UPDATER"

FREENET_FILTER_FILE="$FILTER" \
FREENET_CONFIG_DIR="$CONFIG_DIR" \
FREENET_UPDATER_BIN="$UPDATER" \
sh "$ROOT/scripts/vpn" pl >/dev/null

grep -q 'Poland' "$FILTER"

cat > "$UPDATER" <<'EOF'
#!/bin/sh
[ "${FREENET_ACTION_REASON:-}" = "refresh" ] || exit 40
[ -z "${FREENET_FILTER_OVERRIDE:-}" ] || exit 41
exit 0
EOF
chmod 755 "$UPDATER"

FREENET_FILTER_FILE="$FILTER" \
FREENET_CONFIG_DIR="$CONFIG_DIR" \
FREENET_UPDATER_BIN="$UPDATER" \
sh "$ROOT/scripts/vpn" reload >/dev/null

# Manual rotation selects a different endpoint, passes it through a temporary
# filter and never narrows the persisted broad country/profile filter.
cat > "$CONFIG_DIR/04_outbounds.json" <<'EOF'
{"outbounds":[{"tag":"vless-reality","settings":{"vnext":[{"address":"10.0.0.1","port":443}]}}]}
EOF
printf '%s\n' 'Warsaw|Poland|Polska|Польша' > "$FILTER"
cat > "$CURL_DATA" <<'EOF'
vless://uuid1@10.0.0.1:443?security=reality#Poland-Warsaw-Extra-1
vless://uuid2@10.0.0.2:443?security=reality#Poland-Warsaw-Extra-2
EOF
cat > "$CURL_MOCK" <<EOF
#!/bin/sh
cat "$CURL_DATA"
EOF
chmod 755 "$CURL_MOCK"

cat > "$UPDATER" <<EOF
#!/bin/sh
[ "\${FREENET_ACTION_REASON:-}" = "rotate" ] || exit 50
[ "\${FREENET_FILTER_FILE:-}" != "$FILTER" ] || exit 51
selection="\$(cat "\$FREENET_FILTER_FILE")"
printf '%s\n' 'vless://uuid2@10.0.0.2:443?security=reality#Poland-Warsaw-Extra-2' | grep -Ei "\$selection" >/dev/null || exit 52
if printf '%s\n' 'vless://uuid1@10.0.0.1:443?security=reality#Poland-Warsaw-Extra-1' | grep -Ei "\$selection" >/dev/null; then exit 53; fi
printf '%s\n' called > "$UPDATER_CALLED"
exit 0
EOF
chmod 755 "$UPDATER"

FREENET_FILTER_FILE="$FILTER" \
FREENET_CONFIG_DIR="$CONFIG_DIR" \
FREENET_UPDATER_BIN="$UPDATER" \
FREENET_SUB_FILE="$SUB_FILE" \
FREENET_CURL_BIN="$CURL_MOCK" \
sh "$ROOT/scripts/vpn" rotate >/dev/null

test -f "$UPDATER_CALLED"
grep -q 'Warsaw|Poland|Polska|Польша' "$FILTER"
! grep -q '10.0.0.2' "$FILTER"

# No manual alternative is a terminal no-mutation result.
rm -f "$UPDATER_CALLED"
cat > "$CURL_DATA" <<'EOF'
vless://uuid1@10.0.0.1:443?security=reality#Poland-Warsaw-Extra-1
EOF
cat > "$UPDATER" <<EOF
#!/bin/sh
printf '%s\n' called > "$UPDATER_CALLED"
exit 0
EOF
chmod 755 "$UPDATER"

set +e
FREENET_FILTER_FILE="$FILTER" \
FREENET_CONFIG_DIR="$CONFIG_DIR" \
FREENET_UPDATER_BIN="$UPDATER" \
FREENET_SUB_FILE="$SUB_FILE" \
FREENET_CURL_BIN="$CURL_MOCK" \
sh "$ROOT/scripts/vpn" rotate >/dev/null 2>&1
rc=$?
set -e
[ "$rc" -eq 3 ]
test ! -e "$UPDATER_CALLED"
grep -q 'Warsaw|Poland|Polska|Польша' "$FILTER"

# Failover baseline mocks: local Xray must be running and its live confdir must
# validate before endpoint reachability may trigger any rotation.
cat > "$XRAY_MOCK" <<'EOF'
#!/bin/sh
[ "${1:-}" = run ] || exit 1
[ "${2:-}" = -test ] || exit 1
exit 0
EOF
chmod 755 "$XRAY_MOCK"
printf '%s\n' yes > "$XRAY_STATE"
cat > "$PIDOF_MOCK" <<EOF
#!/bin/sh
[ "\${1:-}" = xray ] || exit 1
[ "\$(cat "$XRAY_STATE" 2>/dev/null)" = yes ]
EOF
chmod 755 "$PIDOF_MOCK"

cat > "$CONFIG_DIR/04_outbounds.json" <<'EOF'
{"outbounds":[{"tag":"vless-reality","settings":{"vnext":[{"address":"10.0.0.1","port":443}]}},{"tag":"dns-out","protocol":"dns"}]}
EOF
printf '%s\n' 'Warsaw|Poland|Polska|Польша' > "$FILTER"
cat > "$CURL_DATA" <<'EOF'
vless://uuid1@10.0.0.1:443?security=reality#Poland-Warsaw-Extra-1
vless://uuid2@10.0.0.2:443?security=reality#Poland-Warsaw-Extra-2
vless://uuid3@10.0.0.3:443?security=reality#Poland-Warsaw-Extra-3
EOF

# One curl mock serves both TCP probes and fresh-subscription fetches.
# Probe success is represented by curl's real verbose contract: a
# "Connected to" line, irrespective of later TLS/HTTP result.
cat > "$CURL_MOCK" <<EOF
#!/bin/sh
url=''
for arg in "\$@"; do
    case "\$arg" in https://*) url="\$arg" ;; esac
done
mode="\$(cat "$CURL_MODE" 2>/dev/null)"
case "\$url" in
    https://subscription.example/key)
        cat "$CURL_DATA"
        exit 0
        ;;
    https://10.0.0.1:443/)
        count="\$(cat "$CURL_COUNT" 2>/dev/null || echo 0)"
        count=\$((count + 1))
        printf '%s\n' "\$count" > "$CURL_COUNT"
        case "\$mode" in
            healthy) echo '* Connected to 10.0.0.1 port 443' >&2 ;;
            transient) [ "\$count" -ge 2 ] && echo '* Connected to 10.0.0.1 port 443' >&2 ;;
        esac
        exit 35
        ;;
    https://10.0.0.2:443/)
        case "\$mode" in
            failover-success) echo '* Connected to 10.0.0.2 port 443' >&2 ;;
        esac
        exit 35
        ;;
    https://10.0.0.3:443/)
        case "\$mode" in
            failover-third) echo '* Connected to 10.0.0.3 port 443' >&2 ;;
        esac
        exit 35
        ;;
esac
exit 7
EOF
chmod 755 "$CURL_MOCK"

run_failover() {
    FREENET_FILTER_FILE="$FILTER" \
    FREENET_CONFIG_DIR="$CONFIG_DIR" \
    FREENET_ASSET_DIR="$ASSET_DIR" \
    FREENET_XRAY_BIN="$XRAY_MOCK" \
    FREENET_PIDOF_BIN="$PIDOF_MOCK" \
    FREENET_UPDATER_BIN="$UPDATER" \
    FREENET_SUB_FILE="$SUB_FILE" \
    FREENET_CURL_BIN="$CURL_MOCK" \
    FREENET_FAILOVER_PROBES=3 \
    FREENET_FAILOVER_DELAY_SEC=0 \
    FREENET_FAILOVER_CONNECT_TIMEOUT=1 \
    FREENET_FAILOVER_MAX_TIME=1 \
    FREENET_FAILOVER_COOLDOWN_SEC="${TEST_COOLDOWN_SEC:-0}" \
    FREENET_FAILOVER_STATE_FILE="$FAILOVER_STATE" \
    sh "$ROOT/scripts/vpn" failover
}

# Healthy endpoint => zero mutation.
rm -f "$UPDATER_CALLED" "$CURL_COUNT" "$FAILOVER_STATE"
printf '%s\n' healthy > "$CURL_MODE"
cat > "$UPDATER" <<EOF
#!/bin/sh
printf '%s\n' called > "$UPDATER_CALLED"
exit 60
EOF
chmod 755 "$UPDATER"
run_failover > "$TMP/healthy.out" 2>&1
grep -Fq 'current endpoint reachable; no mutation' "$TMP/healthy.out"
test ! -e "$UPDATER_CALLED"
[ "$(cat "$CURL_COUNT")" -eq 1 ]

# A single transient failure followed by recovery => zero mutation.
rm -f "$UPDATER_CALLED" "$CURL_COUNT" "$FAILOVER_STATE"
printf '%s\n' transient > "$CURL_MODE"
run_failover > "$TMP/transient.out" 2>&1
grep -Fq 'endpoint recovered on probe 2/3; no mutation' "$TMP/transient.out"
test ! -e "$UPDATER_CALLED"
[ "$(cat "$CURL_COUNT")" -eq 2 ]

# Confirmed current-endpoint failure + reachable alternate => exactly one
# transactional failover call, using a temporary exact endpoint filter.
rm -f "$UPDATER_CALLED" "$CURL_COUNT" "$FAILOVER_STATE"
printf '%s\n' failover-success > "$CURL_MODE"
cat > "$UPDATER" <<EOF
#!/bin/sh
[ "\${FREENET_ACTION_REASON:-}" = failover ] || exit 70
[ "\${FREENET_FILTER_FILE:-}" != "$FILTER" ] || exit 71
selection="\$(cat "\$FREENET_FILTER_FILE")"
printf '%s\n' 'vless://uuid2@10.0.0.2:443?security=reality#Poland-Warsaw-Extra-2' | grep -Ei "\$selection" >/dev/null || exit 72
printf '%s\n' called >> "$UPDATER_CALLED"
exit 0
EOF
chmod 755 "$UPDATER"
run_failover > "$TMP/failover.out" 2>&1
grep -Fq 'failed all 3 TCP probes' "$TMP/failover.out"
grep -Fq 'reachable alternative 10.0.0.2:443' "$TMP/failover.out"
[ "$(wc -l < "$UPDATER_CALLED" | tr -d ' ')" -eq 1 ]
test -s "$FAILOVER_STATE"
grep -q 'Warsaw|Poland|Polska|Польша' "$FILTER"
! grep -q '10.0.0.2' "$FILTER"

# Confirmed failure but no reachable alternate => terminal no-mutation.
rm -f "$UPDATER_CALLED" "$CURL_COUNT" "$FAILOVER_STATE"
printf '%s\n' failover-none > "$CURL_MODE"
cat > "$UPDATER" <<EOF
#!/bin/sh
printf '%s\n' called > "$UPDATER_CALLED"
exit 0
EOF
chmod 755 "$UPDATER"
set +e
run_failover > "$TMP/no-alt.out" 2>&1
rc=$?
set -e
[ "$rc" -eq 4 ]
grep -Fq 'no reachable alternative endpoint' "$TMP/no-alt.out"
test ! -e "$UPDATER_CALLED"

# Local Xray outage is not an endpoint failure and must never rotate.
rm -f "$UPDATER_CALLED" "$CURL_COUNT" "$FAILOVER_STATE"
printf '%s\n' no > "$XRAY_STATE"
printf '%s\n' failover-success > "$CURL_MODE"
set +e
run_failover > "$TMP/xray-offline.out" 2>&1
rc=$?
set -e
[ "$rc" -eq 2 ]
grep -Fq 'local Xray is offline' "$TMP/xray-offline.out"
test ! -e "$UPDATER_CALLED"
test ! -e "$CURL_COUNT"
printf '%s\n' yes > "$XRAY_STATE"

# Cooldown suppresses repeated rotation churn before probes/mutation.
rm -f "$UPDATER_CALLED" "$CURL_COUNT"
date +%s > "$FAILOVER_STATE"
printf '%s\n' failover-success > "$CURL_MODE"
TEST_COOLDOWN_SEC=600
export TEST_COOLDOWN_SEC
run_failover > "$TMP/cooldown.out" 2>&1
grep -Fq 'suppressed by cooldown' "$TMP/cooldown.out"
test ! -e "$UPDATER_CALLED"
test ! -e "$CURL_COUNT"
TEST_COOLDOWN_SEC=0
export TEST_COOLDOWN_SEC

sh -n "$ROOT/scripts/vpn"

echo "vpn refresh/rotate/failover transactional contract: PASS"
