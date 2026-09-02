#!/bin/sh
set -eu

ROOT="$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT HUP INT TERM

FILTER="$TMP/filter.regex"
CONFIG_DIR="$TMP/configs"
UPDATER="$TMP/updater"
mkdir -p "$CONFIG_DIR"

printf '%s\n' 'Frankfurt|Germany|Германия' > "$FILTER"

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

echo "vpn transactional switch contract: PASS"
