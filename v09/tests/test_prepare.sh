#!/bin/sh
# Offline acceptance: exact source, no user downloads, no native/0.8 writes.
set -eu
INSTALLER="$(CDPATH= cd "$(dirname "$0")/../installer" && pwd)/prepare.sh"
ROOT_DIR="$(mktemp -d /tmp/freenet-v09-auto-test.XXXXXX)"
trap 'rm -rf "$ROOT_DIR"' 0
ENTWARE="$ROOT_DIR/opt"
RELEASE="$ROOT_DIR/release"
mkdir -p "$ENTWARE/sbin" "$ENTWARE/etc/init.d" "$ENTWARE/etc/xray/configs" "$RELEASE"
printf '%s\n' 'IMMUTABLE-08' > "$ENTWARE/sbin/freenet-ui"
printf '%s\n' '#!/bin/sh' 'exit 0' > "$ENTWARE/etc/init.d/S99freenet-ui"
printf '%s\n' '#!/bin/sh' 'exit 0' > "$ENTWARE/sbin/xkeen"
printf '%s\n' '#!/bin/sh' 'exit 0' > "$ENTWARE/sbin/xray"
printf '%s\n' 'NO-LEAK-PRIVATE-NATIVE' > "$ENTWARE/etc/xray/configs/04_outbounds.json"
printf '%s\n' '* * * * * /opt/bin/legacy-vpn-task' > "$ROOT_DIR/old-cron.fixture"
cat > "$RELEASE/freenet-v09-arm64-v8a" <<'EOF'
#!/bin/sh
case "${1:-}" in
    -version) echo 'v0.9.0-dev' ;;
    -selftest) echo 'SELFTEST=SUCCESS' ;;
    *) exit 1 ;;
esac
EOF
chmod 600 "$RELEASE/freenet-v09-arm64-v8a"
manifest() { (cd "$RELEASE" && sha256sum freenet-v09-arm64-v8a > SHA256SUMS); }
manifest

run() {
    FREENET_V09_TEST_MODE=yes FREENET_V09_TEST_ROOT="$ENTWARE" \
        FREENET_V09_TEST_ARCH=arm64-v8a FREENET_V09_TEST_RELEASE_DIR="$RELEASE" \
        sh "$INSTALLER" "$@"
}
fail() { echo "FAIL: $*" >&2; exit 1; }
must_stop() {
    if run "$@" > "$ROOT_DIR/last.out" 2>&1;then
        cat "$ROOT_DIR/last.out"
        fail "unexpectedly accepted: $*"
    fi
    grep -q 'STOP:' "$ROOT_DIR/last.out" || fail "no STOP reason: $*"
}
native_hashes() {
    sha256sum "$ENTWARE/sbin/freenet-ui" "$ENTWARE/sbin/xkeen" "$ENTWARE/sbin/xray" \
        "$ENTWARE/etc/init.d/S99freenet-ui" "$ENTWARE/etc/xray/configs/04_outbounds.json" \
        "$ROOT_DIR/old-cron.fixture" | awk '{print $1}' | tr '\n' ':'
}
BEFORE="$(native_hashes)"

# Unreleased development version must NEVER trigger a public download.
if FREENET_V09_TEST_MODE=no sh "$INSTALLER" plan > "$ROOT_DIR/production.out" 2>&1; then
    fail 'unreleased candidate allowed production download'
fi
grep -q 'release not published' "$ROOT_DIR/production.out" || fail 'missing safe development release gate'

OUTPUT="$(run plan)"
printf '%s\n' "$OUTPUT" | grep -q '^STAGE=READY$' || fail 'automatic plan not ready'
[ ! -e "$ENTWARE/freenet-v09" ] || fail 'planning wrote to /opt'
[ "$(native_hashes)" = "$BEFORE" ] || fail 'plan changed existing stack'

OUTPUT="$(run apply)"
printf '%s\n' "$OUTPUT" | grep -q '^STAGE=SUCCESS$' || fail 'automatic staging failed'
printf '%s\n' "$OUTPUT" | grep -q '^ACTIVE=UNCHANGED$' || fail '0.9 took control without cutover'
[ -x "$ENTWARE/freenet-v09/releases/v0.9.0-dev/bin/freenet09" ] || fail 'staged binary absent'
[ "$(native_hashes)" = "$BEFORE" ] || fail 'staging changed existing stack'
[ ! -e "$ENTWARE/etc/init.d/S99freenet09" ] || fail 'staging unexpectedly installed a daemon'

OUTPUT="$(run apply)"
printf '%s\n' "$OUTPUT" | grep -q '^STAGE=ALREADY_PRESENT$' || fail 'repeat auto-stage not idempotent'
run status | grep -q 'STAGE=PRESENT' || fail 'status did not detect stage'
[ "$(native_hashes)" = "$BEFORE" ] || fail 'repeat changed existing stack'

rm -rf "$ENTWARE/freenet-v09"
printf '%s\n' 'CORRUPTED-ASSET' >> "$RELEASE/freenet-v09-arm64-v8a"
must_stop apply
[ ! -e "$ENTWARE/freenet-v09" ] || fail 'corrupt download created slot'
[ "$(native_hashes)" = "$BEFORE" ] || fail 'corrupt download altered existing stack'
manifest

# A correctly hashed but incompatible candidate must also fail before writing.
sed 's/v0\.9\.0-dev/v0.8.0-legacy/g' "$RELEASE/freenet-v09-arm64-v8a" > "$ROOT_DIR/wrong"
mv "$ROOT_DIR/wrong" "$RELEASE/freenet-v09-arm64-v8a"
manifest
must_stop apply
[ ! -e "$ENTWARE/freenet-v09" ] || fail 'wrong version installed'

# Missing, duplicate or symlinked release data are always STOP.
rm "$RELEASE/SHA256SUMS"
must_stop plan
manifest
mv "$RELEASE/freenet-v09-arm64-v8a" "$ROOT_DIR/real"
ln -s "$ROOT_DIR/real" "$RELEASE/freenet-v09-arm64-v8a"
must_stop plan
rm "$RELEASE/freenet-v09-arm64-v8a"
mv "$ROOT_DIR/real" "$RELEASE/freenet-v09-arm64-v8a"
if FREENET_V09_TEST_MODE=yes FREENET_V09_TEST_ROOT="$ENTWARE" \
    FREENET_V09_TEST_ARCH=unknown FREENET_V09_TEST_RELEASE_DIR="$RELEASE" \
    sh "$INSTALLER" plan > "$ROOT_DIR/arch.out" 2>&1;then fail 'invalid architecture accepted';fi
grep -q 'STOP:' "$ROOT_DIR/arch.out" || fail 'architecture STOP reason missing'

[ "$(native_hashes)" = "$BEFORE" ] || fail 'negative tests touched 0.8 or native core'
printf '%s\n' 'PASS: exact release auto-stage, fail closed and legacy preservation'
