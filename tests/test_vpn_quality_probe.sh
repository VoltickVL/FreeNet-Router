#!/bin/sh
set -eu

ROOT="$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT HUP INT TERM

CONFIG_DIR="$TMP/configs"
ASSET_DIR="$TMP/dat"
STATE_FILE="$TMP/quality.state"
XRAY="$TMP/xray"
PIDOF="$TMP/pidof"
CURL="$TMP/curl"
PROBE="$TMP/route-probe"
mkdir -p "$CONFIG_DIR" "$ASSET_DIR"

write_direct_outbound() {
    cat > "$CONFIG_DIR/04_outbounds.json" <<'EOF'
{"outbounds":[{"tag":"vless-reality","settings":{"vnext":[{"address":"10.0.0.1","port":443}]}}]}
EOF
}

write_split_outbound() {
    cat > "$CONFIG_DIR/04_outbounds.json" <<'EOF'
{"outbounds":[{"tag":"vless-reality","settings":{"vnext":[{"address":"10.0.0.1","port":443}]}},{"tag":"dns-out","protocol":"dns"}]}
EOF
}

write_direct_outbound

cat > "$XRAY" <<'EOF'
#!/bin/sh
[ "${1:-}" = run ] || exit 1
[ "${2:-}" = -test ] || exit 1
exit 0
EOF
chmod 755 "$XRAY"

cat > "$PIDOF" <<'EOF'
#!/bin/sh
[ "${1:-}" = xray ] || exit 1
[ "${XRAY_RUNNING:-yes}" = yes ]
EOF
chmod 755 "$PIDOF"

cat > "$CURL" <<'EOF'
#!/bin/sh
echo '* Connected to 10.0.0.1 port 443' >&2
exit 35
EOF
chmod 755 "$CURL"

cat > "$PROBE" <<'EOF'
#!/bin/sh
case "${QUALITY_MODE:-healthy}" in
  healthy) echo 'RTT_MS=85'; exit 0 ;;
  slow) echo 'RTT_MS=1800'; exit 0 ;;
  fail) exit 1 ;;
  unknown) exit 2 ;;
esac
EOF
chmod 755 "$PROBE"

run_probe() {
    FREENET_CONFIG_DIR="$CONFIG_DIR" \
    FREENET_ASSET_DIR="$ASSET_DIR" \
    FREENET_XRAY_BIN="$XRAY" \
    FREENET_PIDOF_BIN="$PIDOF" \
    FREENET_CURL_BIN="$CURL" \
    FREENET_VPN_QUALITY_PROBE_BIN="$PROBE" \
    FREENET_VPN_QUALITY_STATE_FILE="$STATE_FILE" \
    FREENET_VPN_QUALITY_DOWN_STREAK=3 \
    FREENET_VPN_QUALITY_RTT_DEGRADED_MS=1200 \
    sh "$ROOT/scripts/vpn_quality_probe.sh"
}

# Direct DNS without dns-out is a valid VPN-quality baseline and must not be blocked.
QUALITY_MODE=healthy; export QUALITY_MODE
run_probe > "$TMP/direct-healthy.out"
grep -Fq 'VPN_PROCESS=UP' "$TMP/direct-healthy.out"
grep -Fq 'ENDPOINT_REACHABLE=yes' "$TMP/direct-healthy.out"
grep -Fq 'VPN_ROUTE_OK=yes' "$TMP/direct-healthy.out"
grep -Fq 'DNS_OK=unknown' "$TMP/direct-healthy.out"
grep -Fq 'VPN_QUALITY=HEALTHY' "$TMP/direct-healthy.out"
grep -Fq 'RTT_MS=85' "$TMP/direct-healthy.out"
grep -Fq 'FAILURE_STREAK=0' "$TMP/direct-healthy.out"
grep -Fq 'MUTATION=NONE' "$TMP/direct-healthy.out"
[ "$(stat -c '%a' "$STATE_FILE")" = 600 ]
! grep -Eq 'vless://|subscription|uuid|publicKey|shortId' "$STATE_FILE"

# Presence of dns-out is an independent fact and does not alter VPN quality semantics.
write_split_outbound
run_probe > "$TMP/split-healthy.out"
grep -Fq 'DNS_OK=yes' "$TMP/split-healthy.out"
grep -Fq 'VPN_QUALITY=HEALTHY' "$TMP/split-healthy.out"

# Return to direct topology for the remaining route-quality policy tests.
write_direct_outbound

QUALITY_MODE=slow; export QUALITY_MODE
run_probe > "$TMP/slow.out"
grep -Fq 'VPN_ROUTE_OK=yes' "$TMP/slow.out"
grep -Fq 'VPN_QUALITY=DEGRADED' "$TMP/slow.out"
grep -Fq 'RTT_MS=1800' "$TMP/slow.out"

QUALITY_MODE=fail; export QUALITY_MODE
run_probe > "$TMP/fail1.out"
grep -Fq 'ENDPOINT_REACHABLE=yes' "$TMP/fail1.out"
grep -Fq 'VPN_ROUTE_OK=no' "$TMP/fail1.out"
grep -Fq 'VPN_QUALITY=DEGRADED' "$TMP/fail1.out"
grep -Fq 'FAILURE_STREAK=1' "$TMP/fail1.out"

run_probe > "$TMP/fail2.out"
run_probe > "$TMP/fail3.out"
grep -Fq 'VPN_QUALITY=DOWN' "$TMP/fail3.out"
grep -Fq 'FAILURE_STREAK=3' "$TMP/fail3.out"

QUALITY_MODE=healthy; export QUALITY_MODE
run_probe > "$TMP/recovered.out"
grep -Fq 'VPN_QUALITY=HEALTHY' "$TMP/recovered.out"
grep -Fq 'FAILURE_STREAK=0' "$TMP/recovered.out"

set +e
XRAY_RUNNING=no QUALITY_MODE=healthy run_probe > "$TMP/xray-offline.out"
rc=$?
set -e
[ "$rc" -eq 1 ]
grep -Fq 'VPN_PROCESS=DOWN' "$TMP/xray-offline.out"
grep -Fq 'VPN_QUALITY=DOWN' "$TMP/xray-offline.out"
grep -Fq 'local Xray is offline' "$TMP/xray-offline.out"

QUALITY_MODE=unknown; export QUALITY_MODE
run_probe > "$TMP/unknown.out"
grep -Fq 'VPN_ROUTE_OK=unknown' "$TMP/unknown.out"
grep -Fq 'VPN_QUALITY=UNKNOWN' "$TMP/unknown.out"

sh -n "$ROOT/scripts/vpn_quality_probe.sh"
echo 'vpn read-only route quality contract: PASS'
