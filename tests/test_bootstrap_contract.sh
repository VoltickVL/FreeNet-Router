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

run_plan() {
    R="$1"
    A="$2"
    FREENET_ROOT="$R" FREENET_PIN_FILE="$PINS" FREENET_ARCH_RAW="$A" sh "$BOOT" plan
}

R1="$TMP/entware"
make_root "$R1"
OUT="$(run_plan "$R1" 'aarch64-3.10 150')"
echo "$OUT" | grep -Fq 'MODE=ENTWARE_ONLY' || fail 'clean Entware must classify ENTWARE_ONLY'
echo "$OUT" | grep -Fq 'ARCH=arm64-v8a' || fail 'ARM64 mapping failed'
echo "$OUT" | grep -Fq 'Xray-linux-arm64-v8a.zip' || fail 'ARM64 Xray asset mapping failed'
echo "$OUT" | grep -Fq 'MUTATION=NONE' || fail 'plan must be read-only'

# Complete existing stack + configs is preserved.
printf '#!/bin/sh\n' > "$R1/sbin/xkeen"
printf '#!/bin/sh\n' > "$R1/sbin/xray"
chmod +x "$R1/sbin/xkeen" "$R1/sbin/xray"
mkdir -p "$R1/etc/xray/configs"
printf '{}\n' > "$R1/etc/xray/configs/01_log.json"
OUT="$(run_plan "$R1" 'aarch64-3.10 150')"
echo "$OUT" | grep -Fq 'MODE=READY_EXISTING_STACK' || fail 'complete stack must be preserved'

# Partial stack/config must stop instead of guessing.
rm -f "$R1/sbin/xray"
OUT="$(run_plan "$R1" 'aarch64-3.10 150')"
echo "$OUT" | grep -Fq 'MODE=NEEDS_REVIEW' || fail 'partial stack must classify NEEDS_REVIEW'

# Architecture mappings are explicit.
R2="$TMP/mipsle"
make_root "$R2"
OUT="$(run_plan "$R2" 'mipsel-3.4 100')"
echo "$OUT" | grep -Fq 'ARCH=mips32le' || fail 'MIPSLE mapping failed'
echo "$OUT" | grep -Fq 'Xray-linux-mips32le.zip' || fail 'MIPSLE Xray mapping failed'

R3="$TMP/unsupported"
make_root "$R3"
OUT="$(run_plan "$R3" 'riscv64 100')"
echo "$OUT" | grep -Fq 'MODE=UNSUPPORTED_ARCH' || fail 'unsupported architecture must stop'

# Permanent apply is gated and transactional.
grep -Fq 'apply requires MODE=ENTWARE_ONLY' "$BOOT" || fail 'apply is not gated to clean Entware-only state'
grep -Fq 'opkg_cmd update' "$BOOT" || fail 'targeted dependency refresh missing'
grep -Fq 'opkg_cmd install $PACKAGES' "$BOOT" || fail 'targeted dependency install missing'
grep -Fq "PACKAGES='ca-bundle curl jq libc libssp librt libpthread ip-full iptables ipset coreutils-uname coreutils-nohup unzip'" "$BOOT" || fail 'dependency allowlist changed unexpectedly'
grep -Fq "printf '0\\n' | /opt/sbin/xkeen -io" "$BOOT" || fail 'pinned XKeen offline registration contract missing'
grep -Fq '/opt/sbin/xkeen -auto off' "$BOOT" || fail 'pre-setup autostart must remain off'
grep -Fq '/opt/sbin/xkeen -dns off' "$BOOT" || fail 'pre-setup proxy DNS must remain off'
grep -Fq 'run -test -confdir' "$BOOT" || fail 'Xray validation missing'
grep -Fq 'ROLLBACK: restoring pre-bootstrap core stack' "$BOOT" || fail 'rollback path missing'
grep -Fq 'ROLLBACK ERROR: FAILED/UNKNOWN' "$BOOT" || fail 'rollback failure state missing'
grep -Fq 'CORE_APPLY=SUCCESS' "$BOOT" || fail 'success marker missing'
grep -Fq 'XKEEN_AUTOSTART=off' "$BOOT" || fail 'bootstrap state must require setup wizard before XKeen autostart'
grep -Fq 'PROXY_DNS=off' "$BOOT" || fail 'bootstrap state must require setup wizard before DNS policy'

# Core bootstrap never receives VPN credentials/provider data.
if grep -Ei 'subscription.*url=|uuid=|publicKey|shortId|vless://' "$BOOT" >/dev/null; then
    fail 'core bootstrap contains credential/provider material'
fi

echo 'bootstrap contract PASS'
