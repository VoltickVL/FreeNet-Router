#!/bin/sh

set -eu

ROOT_DIR="$(CDPATH= cd "$(dirname "$0")/.." && pwd)"
BOOT="$ROOT_DIR/scripts/bootstrap_router_ultra.sh"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT INT TERM

fail() { echo "router stage-0 contract FAIL: $*" >&2; exit 1; }

sh -n "$BOOT"

make_fake_tools() {
    CASE_DIR="$1"
    mkdir -p "$CASE_DIR/bin" "$CASE_DIR/opt/bin"
    cat > "$CASE_DIR/bin/ndmc" <<'EOF'
#!/bin/sh
[ "${1:-}" = "-c" ] || exit 2
CMD="${2:-}"
case "$CMD" in
    "show version")
        cat "$FAKE_MODEL_FILE"
        ;;
    "show media")
        cat "$FAKE_MEDIA_FILE"
        ;;
    "opkg disk "*)
        printf '%s\n' "$CMD" > "$FAKE_COMMAND_LOG"
        mkdir -p "$FREENET_OPT_ROOT/bin"
        cat > "$FREENET_OPT_ROOT/bin/opkg" <<'OPKG'
#!/bin/sh
case "${1:-}" in
    update) exit 0 ;;
    print-architecture) printf '%s\n' "$FAKE_ARCH"; exit 0 ;;
    install)
        mkdir -p "$FREENET_OPT_ROOT/bin"
        cat > "$FREENET_OPT_ROOT/bin/curl" <<'CURL'
#!/bin/sh
OUT=""
while [ "$#" -gt 0 ]; do
    if [ "$1" = "-o" ]; then
        shift
        OUT="$1"
    fi
    shift
done
[ -n "$OUT" ] || exit 2
cp "$FAKE_BOOTSTRAP_SOURCE" "$OUT"
CURL
        chmod +x "$FREENET_OPT_ROOT/bin/curl"
        cat > "$FREENET_OPT_ROOT/bin/sh" <<'SH'
#!/bin/sh
exec /bin/sh "$@"
SH
        chmod +x "$FREENET_OPT_ROOT/bin/sh"
        exit 0
        ;;
    *) exit 2 ;;
esac
OPKG
        chmod +x "$FREENET_OPT_ROOT/bin/opkg"
        ;;
    *)
        exit 2
        ;;
esac
EOF
    chmod +x "$CASE_DIR/bin/ndmc"

    cat > "$CASE_DIR/freenet-bootstrap.sh" <<'EOF'
#!/bin/sh
printf 'BOOTSTRAP_OK\n' > "$FAKE_BOOTSTRAP_MARKER"
EOF
    chmod +x "$CASE_DIR/freenet-bootstrap.sh"
}

write_media_one() {
    FILE="$1"
    UUID="$2"
    cat > "$FILE" <<EOF
            media:
                 name: Media0
            partition:
                     uuid: $UUID
                   fstype: ext4
                    state: MOUNTED
                    total: 64000000000
                     free: 63000000000
EOF
}

run_case() {
    CASE_DIR="$1"
    export FREENET_OPT_ROOT="$CASE_DIR/opt"
    export FREENET_NDMC="$CASE_DIR/bin/ndmc"
    export FREENET_RELEASE_BASE="https://example.invalid/freenet"
    export FREENET_ENTWARE_WAIT_SECONDS=5
    export FREENET_ENTWARE_WAIT_STEP=1
    export FREENET_TMP_BOOTSTRAP="$CASE_DIR/downloaded-bootstrap.sh"
    export FAKE_MODEL_FILE="$CASE_DIR/model.txt"
    export FAKE_MEDIA_FILE="$CASE_DIR/media.txt"
    export FAKE_COMMAND_LOG="$CASE_DIR/command.log"
    export FAKE_BOOTSTRAP_SOURCE="$CASE_DIR/freenet-bootstrap.sh"
    export FAKE_BOOTSTRAP_MARKER="$CASE_DIR/bootstrap.marker"
    PATH="$CASE_DIR/bin:/bin:/usr/bin" sh "$BOOT"
}

# Netcraze Ultra NC-1812: AArch64 + one EXT4 -> online Entware + FreeNet handoff.
C1="$TMP/nc1812"
make_fake_tools "$C1"
printf '            model: Ultra (NC-1812)\n' > "$C1/model.txt"
write_media_one "$C1/media.txt" '11111111-2222-3333-4444-555555555555'
export FAKE_ARCH='aarch64-3.10'
OUT="$(run_case "$C1")"
printf '%s\n' "$OUT" | grep -Fq 'ENTWARE_ARCH=aarch64' || fail 'NC-1812 must select aarch64'
grep -Fq 'opkg disk 11111111-2222-3333-4444-555555555555:/ https://bin.entware.net/aarch64-k3.10/installer/aarch64-installer.tar.gz' "$C1/command.log" || fail 'NC-1812 online Entware command mismatch'
[ -f "$C1/bootstrap.marker" ] || fail 'NC-1812 did not hand off to FreeNet bootstrap'

# Keenetic Ultra KN-1811: same AArch64 contract.
C2="$TMP/kn1811"
make_fake_tools "$C2"
printf '            model: Ultra (KN-1811)\n' > "$C2/model.txt"
write_media_one "$C2/media.txt" 'aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee'
export FAKE_ARCH='aarch64-3.10'
OUT="$(run_case "$C2")"
printf '%s\n' "$OUT" | grep -Fq 'ENTWARE_ARCH=aarch64' || fail 'KN-1811 must select aarch64'
grep -Fq 'aarch64-k3.10/installer/aarch64-installer.tar.gz' "$C2/command.log" || fail 'KN-1811 Entware URL mismatch'
[ -f "$C2/bootstrap.marker" ] || fail 'KN-1811 did not hand off to FreeNet bootstrap'

# Legacy Keenetic Ultra KN-1810: MIPSel contract remains explicit.
C3="$TMP/kn1810"
make_fake_tools "$C3"
printf '            model: Ultra (KN-1810)\n' > "$C3/model.txt"
write_media_one "$C3/media.txt" 'bbbbbbbb-cccc-dddd-eeee-ffffffffffff'
export FAKE_ARCH='mipsel-3.4'
OUT="$(run_case "$C3")"
printf '%s\n' "$OUT" | grep -Fq 'ENTWARE_ARCH=mipsel' || fail 'KN-1810 must select mipsel'
grep -Fq 'mipselsf-k3.4/installer/mipsel-installer.tar.gz' "$C3/command.log" || fail 'KN-1810 Entware URL mismatch'

# Verified Giga models use the same one-command Stage-0 as Ultra.
# Giga KN-1010 / KN-1011 = MIPSel; Netcraze Giga NC-1012 = AArch64.
for MODEL_CASE in kn1010 kn1011 hero1011 nc1012; do
    case "$MODEL_CASE" in
        kn1010) MODEL_NAME='Giga (KN-1010)'; EXPECTED_ARCH='mipsel'; EXPECTED_URL='mipselsf-k3.4/installer/mipsel-installer.tar.gz' ;;
        kn1011) MODEL_NAME='Giga (KN-1011)'; EXPECTED_ARCH='mipsel'; EXPECTED_URL='mipselsf-k3.4/installer/mipsel-installer.tar.gz' ;;
        hero1011) MODEL_NAME='Hero (KN-1011)'; EXPECTED_ARCH='mipsel'; EXPECTED_URL='mipselsf-k3.4/installer/mipsel-installer.tar.gz' ;;
        nc1012) MODEL_NAME='Giga (NC-1012)'; EXPECTED_ARCH='aarch64'; EXPECTED_URL='aarch64-k3.10/installer/aarch64-installer.tar.gz' ;;
    esac
    GIGA="$TMP/$MODEL_CASE"
    make_fake_tools "$GIGA"
    printf '            model: %s\n' "$MODEL_NAME" > "$GIGA/model.txt"
    write_media_one "$GIGA/media.txt" '33333333-4444-5555-6666-777777777777'
    export FAKE_ARCH="$EXPECTED_ARCH"
    OUT="$(run_case "$GIGA")"
    printf '%s\n' "$OUT" | grep -Fq "ENTWARE_ARCH=$EXPECTED_ARCH" || fail "$MODEL_CASE wrong architecture"
    grep -Fq "opkg disk 33333333-4444-5555-6666-777777777777:/ https://bin.entware.net/$EXPECTED_URL" "$GIGA/command.log" || fail "$MODEL_CASE wrong Entware installer"
    [ -f "$GIGA/bootstrap.marker" ] || fail "$MODEL_CASE missing FreeNet bootstrap handoff"
done

# Unknown model must fail before opkg disk.
C4="$TMP/unknown"
make_fake_tools "$C4"
printf '            model: Giga (NC-9999)\n' > "$C4/model.txt"
write_media_one "$C4/media.txt" 'cccccccc-dddd-eeee-ffff-000000000000'
export FAKE_ARCH='aarch64-3.10'
if run_case "$C4" >"$C4/out" 2>&1; then
    fail 'unknown model must STOP'
fi
[ ! -f "$C4/command.log" ] || fail 'unknown model reached opkg mutation'

# Do not accept model identifiers from unrelated show-version fields.
SPOOF="$TMP/spoofed"
make_fake_tools "$SPOOF"
printf '            model: Unsupported (KN-9999)\n            note: Giga (NC-1012)\n' > "$SPOOF/model.txt"
write_media_one "$SPOOF/media.txt" 'dddddddd-eeee-ffff-0000-111111111111'
export FAKE_ARCH='aarch64-3.10'
if run_case "$SPOOF" >"$SPOOF/out" 2>&1; then fail 'spoofed model must STOP'; fi
[ ! -f "$SPOOF/command.log" ] || fail 'spoofed model reached opkg mutation'

# Zero EXT4 must fail closed.
C5="$TMP/noext4"
make_fake_tools "$C5"
printf '            model: Ultra (NC-1812)\n' > "$C5/model.txt"
cat > "$C5/media.txt" <<'EOF'
            partition:
                     uuid: dddddddd-eeee-ffff-0000-111111111111
                   fstype: ntfs
                    state: MOUNTED
EOF
export FAKE_ARCH='aarch64-3.10'
if run_case "$C5" >"$C5/out" 2>&1; then
    fail 'zero EXT4 must STOP'
fi
[ ! -f "$C5/command.log" ] || fail 'zero EXT4 reached opkg mutation'

# Multiple mounted EXT4 partitions must fail closed instead of guessing.
C6="$TMP/multiple"
make_fake_tools "$C6"
printf '            model: Ultra (NC-1812)\n' > "$C6/model.txt"
cat > "$C6/media.txt" <<'EOF'
            partition:
                     uuid: 11111111-1111-1111-1111-111111111111
                   fstype: ext4
                    state: MOUNTED
            partition:
                     uuid: 22222222-2222-2222-2222-222222222222
                   fstype: ext4
                    state: MOUNTED
EOF
export FAKE_ARCH='aarch64-3.10'
if run_case "$C6" >"$C6/out" 2>&1; then
    fail 'multiple EXT4 must STOP'
fi
[ ! -f "$C6/command.log" ] || fail 'multiple EXT4 reached opkg mutation'

# Existing Entware: skip router/model/storage mutation and hand off directly.
C7="$TMP/existing"
make_fake_tools "$C7"
export FREENET_OPT_ROOT="$C7/opt"
export FAKE_ARCH='aarch64-3.10'
# Reuse fake ndmc once to materialize the mock Entware toolchain, then remove it.
export FAKE_COMMAND_LOG="$C7/setup-command.log"
export FAKE_MODEL_FILE="$C7/model.txt"
export FAKE_MEDIA_FILE="$C7/media.txt"
export FAKE_BOOTSTRAP_SOURCE="$C7/freenet-bootstrap.sh"
export FAKE_BOOTSTRAP_MARKER="$C7/bootstrap.marker"
"$C7/bin/ndmc" -c 'opkg disk dummy:/ https://example.invalid/installer.tar.gz'
rm -f "$C7/bin/ndmc" "$C7/bootstrap.marker"
export FREENET_NDMC="$C7/bin/ndmc"
export FREENET_RELEASE_BASE='https://example.invalid/freenet'
export FREENET_TMP_BOOTSTRAP="$C7/downloaded-bootstrap.sh"
PATH="/bin:/usr/bin" sh "$BOOT" > "$C7/out"
grep -Fq 'ENTWARE=existing' "$C7/out" || fail 'existing Entware path not detected'
[ -f "$C7/bootstrap.marker" ] || fail 'existing Entware did not hand off to FreeNet'
[ ! -f "$C7/command.log" ] || fail 'existing Entware attempted router OPKG mutation'

# Static safety contract.
if grep -E 'opkg[[:space:]]+upgrade' "$BOOT" >/dev/null; then
    fail 'stage-0 must never run global opkg upgrade'
fi
if grep -Ei 'XKEEN_UI_REPO|fetch_one XKeen-UI|cp .*xkeen-ui|write_xkeen_ui_init|start_xkeen_ui' "$BOOT" >/dev/null; then
    fail 'stage-0 must not manage XKeen UI'
fi
README="$ROOT_DIR/README.md"
if grep -Ei 'XKeen[[:space:]-]*UI|xkeen-ui' "$README" >/dev/null; then
    fail 'README must not mention XKeen UI'
fi
grep -Fq 'more than one mounted EXT4 partition found; refusing to guess' "$BOOT" || fail 'multiple-disk STOP contract missing'
grep -Fq "Ultra (KN-1811)" "$BOOT" || fail 'KN-1811 mapping missing'
grep -Fq "Ultra (NC-1812)" "$BOOT" || fail 'NC-1812 mapping missing'
grep -Fq "Ultra (KN-1810)" "$BOOT" || fail 'KN-1810 mapping missing'
grep -Fq '"Giga (KN-1010)"' "$BOOT" || fail 'KN-1010 mapping missing'
grep -Fq '"Giga (KN-1011)"' "$BOOT" || fail 'KN-1011 mapping missing'
grep -Fq '"Giga (NC-1012)"' "$BOOT" || fail 'NC-1012 mapping missing'

echo 'router stage-0 contract PASS'
