#!/bin/sh
set -eu

ROOT="$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT HUP INT TERM

FILTER="$TMP/filter.regex"
CONFIG_DIR="$TMP/configs"
UPDATER="$TMP/updater"
SUB_FILE="$TMP/subscription.url"
CURL_MOCK="$TMP/curl"
CURL_DATA="$TMP/curl.data"
UPDATER_CALLED="$TMP/updater.called"
mkdir -p "$CONFIG_DIR"

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

# Rotation selects a different endpoint, passes it through a temporary filter,
# and never narrows the persisted broad country/profile filter.
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

# No alternative is a terminal no-mutation result: updater must not run and
# the current broad filter must stay intact.
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

sh -n "$ROOT/scripts/vpn"

echo "vpn refresh/rotate transactional contract: PASS"
