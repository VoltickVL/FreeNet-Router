#!/bin/sh
set -eu

ROOT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
HELPER="$ROOT_DIR/freenet-ui/auto_vpn.sh"
TMP="$(mktemp -d /tmp/freenet-auto-vpn-test.XXXXXX)"
trap 'rm -rf "$TMP"' EXIT HUP INT TERM

mkdir -p "$TMP/bin" "$TMP/etc/freenet" "$TMP/etc/xray/configs" "$TMP/etc/xray/dat" "$TMP/var/run" "$TMP/var/log" "$TMP/backups"
CONFIG="$TMP/etc/freenet/freenet.conf"
SUB="$TMP/etc/xray/blanc_subscription.url"
PROFILE="$TMP/etc/freenet/vpn_profile_name"
FILTER="$TMP/etc/xray/blanc_profile_filter.regex"
OUT="$TMP/etc/xray/configs/04_outbounds.json"
STATE="$TMP/var/run/freenet-automation.state"
HISTORY="$TMP/var/log/freenet-automation.history"
CRON="$TMP/crontab"
SUB_DATA="$TMP/subscription.txt"

cat > "$CONFIG" <<'EOF'
AUTO_VPN_V1=no
AUTO_VPN_V1_INTERVAL=manual
AUTO_ENDPOINT_UPDATE=no
AUTO_ENDPOINT_CRON='0 * * * *'
AUTO_VPN_FAILOVER=no
AUTO_VPN_FAILOVER_CRON='*/5 * * * *'
AUTO_XKEEN_GEODATA=yes
AUTO_XKEEN_GEODATA_CRON='30 6 * * *'
EOF
printf '%s\n' 'https://example.test/sub' > "$SUB"
printf '%s\n' 'PL Warsaw, Poland, Extra' > "$PROFILE"
printf '%s\n' 'PL Warsaw, Poland, Extra' > "$FILTER"
printf '%s\n' '15 4 * * * /opt/bin/foreign-job' > "$CRON"

write_out() {
    cat > "$OUT" <<EOF
{"outbounds":[{"tag":"vless-reality","protocol":"vless","settings":{"vnext":[{"address":"$1","port":$2}]}}]}
EOF
}

cat > "$TMP/bin/crontab" <<'EOF'
#!/bin/sh
if [ "${1:-}" = "-l" ]; then
    cat "$TEST_CRON" 2>/dev/null || true
    exit 0
fi
cp "$1" "$TEST_CRON"
EOF

cat > "$TMP/bin/curl" <<'EOF'
#!/bin/sh
case " $* " in
    *"%{time_connect}"*) printf '%s' "${TEST_CONNECT_TIME:-0.050}"; exit 0 ;;
esac
cat "$TEST_SUB_DATA"
EOF

cat > "$TMP/bin/updater" <<'EOF'
#!/bin/sh
if [ "${TEST_UPDATER_FAIL:-no}" = yes ]; then
    echo '[blanc-xkeen] ERROR: simulated failure' >&2
    exit 1
fi
cat > "$FREENET_OUT_FILE" <<EOT
{"outbounds":[{"tag":"vless-reality","protocol":"vless","settings":{"vnext":[{"address":"${TEST_NEW_ADDRESS:-9.9.9.9}","port":${TEST_NEW_PORT:-443}}]}}]}
EOT
exit 0
EOF

cat > "$TMP/bin/xkeen" <<'EOF'
#!/bin/sh
exit 0
EOF
cat > "$TMP/bin/xray" <<'EOF'
#!/bin/sh
exit 0
EOF
cat > "$TMP/bin/pidof" <<'EOF'
#!/bin/sh
[ "${1:-}" = xray ] && exit 0
exit 1
EOF
chmod 755 "$TMP/bin/"*

export PATH="$TMP/bin:$PATH"
export TEST_CRON="$CRON" TEST_SUB_DATA="$SUB_DATA"
export FREENET_ROOT="$TMP" FREENET_CONFIG_FILE="$CONFIG" FREENET_SUB_FILE="$SUB"
export FREENET_PROFILE_FILE="$PROFILE" FREENET_FILTER_FILE="$FILTER" FREENET_CONFIG_DIR="$TMP/etc/xray/configs"
export FREENET_OUT_FILE="$OUT" FREENET_ASSET_DIR="$TMP/etc/xray/dat"
export FREENET_UPDATER_BIN="$TMP/bin/updater" FREENET_XKEEN_BIN="$TMP/bin/xkeen" FREENET_XRAY_BIN="$TMP/bin/xray"
export FREENET_CRONTAB_BIN="$TMP/bin/crontab" FREENET_AUTOMATION_STATE="$STATE" FREENET_AUTOMATION_HISTORY="$HISTORY"
export FREENET_AUTO_VPN_BACKUP="$TMP/backups/auto-vpn-last" FREENET_AUTO_VPN_LOCK="$TMP/auto-vpn.lock"
export FREENET_CURL_BIN="$TMP/bin/curl"

sh "$HELPER" configure --enabled true --interval 1h --cron '0 * * * *' >/dev/null
grep -q '^AUTO_VPN_V1=yes$' "$CONFIG"
grep -q '^AUTO_VPN_V1_INTERVAL=1h$' "$CONFIG"
grep -q '^AUTO_ENDPOINT_UPDATE=yes$' "$CONFIG"
grep -q '^AUTO_ENDPOINT_CRON='"'"'0 \* \* \* \*'"'"'$' "$CONFIG"
grep -q '/opt/lib/freenet/auto_vpn.sh run' "$CRON"
grep -q '/opt/bin/foreign-job' "$CRON"
grep -q '/opt/sbin/xkeen -ug' "$CRON"

sh "$HELPER" configure --enabled true --interval manual >/dev/null
grep -q '^AUTO_VPN_V1=yes$' "$CONFIG"
grep -q '^AUTO_VPN_V1_INTERVAL=manual$' "$CONFIG"
grep -q '^AUTO_ENDPOINT_UPDATE=no$' "$CONFIG"
! grep -q '^[^#].*/opt/lib/freenet/auto_vpn.sh run' "$CRON"

write_out 5.6.7.8 443
cat > "$SUB_DATA" <<'EOF'
vless://u@5.6.7.8:443?security=reality#PL Warsaw, Poland, Extra
EOF
sh "$HELPER" run > "$TMP/same.out"
grep -q '^RESULT=same$' "$TMP/same.out"
grep -q '^LAST_RESULT=same$' "$STATE"
grep -q '"address":"5.6.7.8"' "$OUT"

cat > "$SUB_DATA" <<'EOF'
vless://u@9.9.9.9:443?security=reality#PL Warsaw, Poland, Extra
vless://u@8.8.8.8:443?security=reality#PL Warsaw, Poland, Extra
EOF
sh "$HELPER" run > "$TMP/ambiguous.out"
grep -q '^RESULT=ambiguous$' "$TMP/ambiguous.out"
grep -q '"address":"5.6.7.8"' "$OUT"

cat > "$SUB_DATA" <<'EOF'
vless://u@9.9.9.9:443?security=reality#PL Warsaw, Poland, Extra
EOF
export TEST_NEW_ADDRESS=9.9.9.9 TEST_NEW_PORT=443 TEST_UPDATER_FAIL=no
sh "$HELPER" run > "$TMP/updated.out"
grep -q '^RESULT=updated$' "$TMP/updated.out"
grep -q '"address":"9.9.9.9"' "$OUT"
[ "$(cat "$PROFILE")" = 'PL Warsaw, Poland, Extra' ]
[ "$(cat "$FILTER")" = 'PL Warsaw, Poland, Extra' ]
[ -s "$TMP/backups/auto-vpn-last/04_outbounds.json" ]
grep -q '^LAST_RESULT=updated$' "$STATE"

write_out 5.6.7.8 443
export TEST_UPDATER_FAIL=yes TEST_NEW_ADDRESS=9.9.9.9
if sh "$HELPER" run > "$TMP/fail.out" 2>&1; then
    echo 'expected failed updater path' >&2
    exit 1
fi
grep -q 'ROLLBACK=SUCCESS' "$TMP/fail.out"
grep -q '"address":"5.6.7.8"' "$OUT"
grep -q '^LAST_RESULT=failed$' "$STATE"

[ -s "$HISTORY" ]
! grep -q 'vless://' "$HISTORY"
! grep -q 'example.test' "$HISTORY"

printf '%s\n' 'AUTO VPN v1 contract: PASS'
