#!/bin/sh

set -eu

ROOT_DIR="$(CDPATH= cd "$(dirname "$0")/.." && pwd)"
BOOT="$ROOT_DIR/scripts/bootstrap_entware.sh"
PINS="$ROOT_DIR/config/upstream-pins.env"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT INT TERM

fail() { echo "bootstrap contract FAIL: $*" >&2; exit 1; }

sh -n "$BOOT"
sh -n "$PINS"

# Bootstrap must never perform a global Entware upgrade.
if grep -E 'opkg[[:space:]]+upgrade' "$BOOT" "$PINS" >/dev/null; then
    fail 'global opkg upgrade is forbidden'
fi

# Exact pins are source of truth; no latest/download drift in bootstrap.
grep -Fq "XKEEN_VERSION='2.0'" "$PINS" || fail 'XKeen pin missing'
grep -Fq "XRAY_VERSION='v26.7.28'" "$PINS" || fail 'Xray pin missing'
grep -Fq "XKEEN_UI_VERSION='v1.1.3'" "$PINS" || fail 'XKeen UI pin missing'
grep -Fq "f5698bb218ada3b4022db26fafc39601c5f53b46b19eb76c9616325985807501" "$PINS" || fail 'ARM64 Xray digest missing'
grep -Fq "4779e1afba7dea12c64a72380f1d9737a12359f354014625a7f9d96f8d31e3fa" "$PINS" || fail 'MIPSLE Xray digest missing'
grep -Fq "aec600118fd1e7ee42e8d5e8d5c82cc5e8139e82ff1da029e9b81b7170fc028c" "$PINS" || fail 'MIPS Xray digest missing'
if grep -Fq 'releases/latest/' "$BOOT"; then
    fail 'bootstrap must not follow latest release'
fi

make_root() {
    R="$1"
    mkdir -p "$R/bin" "$R/sbin"
    cat > "$R/bin/opkg" <<'EOF'
#!/bin/sh
exit 0
EOF
    chmod +x "$R/bin/opkg"
}

R1="$TMP/entware"
make_root "$R1"
OUT="$(FREENET_ROOT="$R1" FREENET_PIN_FILE="$PINS" FREENET_ARCH_RAW='aarch64-3.10 150' "$BOOT" plan)"
echo "$OUT" | grep -Fq 'MODE=ENTWARE_ONLY' || fail 'clean Entware must classify ENTWARE_ONLY'
echo "$OUT" | grep -Fq 'ARCH=arm64-v8a' || fail 'ARM64 mapping failed'
echo "$OUT" | grep -Fq 'Xray-linux-arm64-v8a.zip' || fail 'ARM64 Xray asset mapping failed'
echo "$OUT" | grep -Fq 'MUTATION=NONE' || fail 'plan must be read-only'

# Complete existing stack is preserved.
printf '#!/bin/sh\n' > "$R1/sbin/xkeen"
printf '#!/bin/sh\n' > "$R1/sbin/xray"
chmod +x "$R1/sbin/xkeen" "$R1/sbin/xray"
OUT="$(FREENET_ROOT="$R1" FREENET_PIN_FILE="$PINS" FREENET_ARCH_RAW='aarch64-3.10 150' "$BOOT" plan)"
echo "$OUT" | grep -Fq 'MODE=READY_EXISTING_STACK' || fail 'complete stack must be preserved'

# Partial stack must stop instead of guessing.
rm -f "$R1/sbin/xray"
OUT="$(FREENET_ROOT="$R1" FREENET_PIN_FILE="$PINS" FREENET_ARCH_RAW='aarch64-3.10 150' "$BOOT" plan)"
echo "$OUT" | grep -Fq 'MODE=NEEDS_REVIEW' || fail 'partial stack must classify NEEDS_REVIEW'

# Architecture mappings are explicit.
R2="$TMP/mipsle"
make_root "$R2"
OUT="$(FREENET_ROOT="$R2" FREENET_PIN_FILE="$PINS" FREENET_ARCH_RAW='mipsel-3.4 100' "$BOOT" plan)"
echo "$OUT" | grep -Fq 'ARCH=mips32le' || fail 'MIPSLE mapping failed'
echo "$OUT" | grep -Fq 'Xray-linux-mips32le.zip' || fail 'MIPSLE Xray mapping failed'

R3="$TMP/unsupported"
make_root "$R3"
OUT="$(FREENET_ROOT="$R3" FREENET_PIN_FILE="$PINS" FREENET_ARCH_RAW='riscv64 100' "$BOOT" plan)"
echo "$OUT" | grep -Fq 'MODE=UNSUPPORTED_ARCH' || fail 'unsupported architecture must stop'

# This slice is staging-only: there must be no permanent install/apply commands.
if grep -E '(^|[[:space:]])(mv|cp)[[:space:]].*(/opt|\$ROOT)/(sbin|etc)|xkeen[[:space:]]+-restart|crontab[[:space:]]' "$BOOT" >/dev/null; then
    fail 'staging slice unexpectedly contains permanent runtime mutation'
fi

echo 'bootstrap contract PASS'
