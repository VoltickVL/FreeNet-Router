#!/bin/sh
# Offline contract tests. These fixtures never touch the real /opt or router.
set -eu
SCRIPT="$(CDPATH= cd "$(dirname "$0")/.." && pwd)/installer/stage.sh"
TEST_DIR="$(mktemp -d /tmp/freenet-v09-stage-test.XXXXXX)"
trap 'rm -rf "$TEST_DIR"' 0
ROOT="$TEST_DIR/opt"
BUNDLE="$TEST_DIR/bundle"
ASSET=freenet-v09-arm64-v8a
mkdir -p "$ROOT/sbin" "$ROOT/etc/init.d" "$ROOT/etc/xray/configs" "$BUNDLE"
printf '%s\n' 'legacy-08-immutable' > "$ROOT/sbin/freenet-ui"
printf '%s\n' '#!/bin/sh' 'exit 0' > "$ROOT/etc/init.d/S99freenet-ui"
printf '%s\n' '#!/bin/sh' 'exit 0' > "$ROOT/sbin/xkeen"
printf '%s\n' '#!/bin/sh' 'exit 0' > "$ROOT/sbin/xray"
printf '%s\n' 'DO-NOT-TOUCH-SECRET' > "$ROOT/etc/xray/configs/04_outbounds.json"
printf '%s\n' '* * * * * /opt/bin/legacy-autovpn' > "$ROOT/legacy-cron.fixture"

cat > "$BUNDLE/$ASSET" <<'EOF'
#!/bin/sh
case "${1:-}" in
    -version) printf '%s\n' 'v0.9.0-dev' ;;
    -selftest) printf '%s\n' 'SELFTEST=SUCCESS' ;;
    *) exit 1 ;;
esac
EOF
# Real release downloads might have mode 0600; staging must still work.
chmod 600 "$BUNDLE/$ASSET"
make_manifest() { (cd "$BUNDLE" && sha256sum "$ASSET" > SHA256SUMS); }
make_manifest

run_stage() {
    FREENET_V09_TEST_MODE=yes FREENET_V09_TEST_ROOT="$ROOT" \
        FREENET_V09_TEST_ARCH=arm64-v8a sh "$SCRIPT" "$@"
}
fail() { printf '%s\n' "FAIL: $*" >&2; exit 1; }
must_fail() {
    if run_stage "$@" > "$TEST_DIR/fail.out" 2>&1; then
        cat "$TEST_DIR/fail.out"
        fail "operation unexpectedly succeeded: $*"
    fi
    grep -q 'STOP:' "$TEST_DIR/fail.out" || fail "missing STOP reason: $*"
}
snapshot_native() {
    sha256sum "$ROOT/sbin/freenet-ui" "$ROOT/etc/init.d/S99freenet-ui" \
        "$ROOT/sbin/xkeen" "$ROOT/sbin/xray" \
        "$ROOT/etc/xray/configs/04_outbounds.json" "$ROOT/legacy-cron.fixture" \
        | awk '{print $1}' | tr '\n' ':'
}
BEFORE="$(snapshot_native)"

PLAN="$(run_stage plan "$BUNDLE")"
printf '%s\n' "$PLAN" | grep -q '^STAGE=READY$' || fail 'stage plan not ready'
printf '%s\n' "$PLAN" | grep -q '^MUTATION=NONE$' || fail 'plan must not mutate'
[ ! -e "$ROOT/freenet-v09" ] || fail 'plan created persistent directory'
[ "$(snapshot_native)" = "$BEFORE" ] || fail 'plan changed native/legacy files'

APPLY="$(run_stage apply "$BUNDLE")"
printf '%s\n' "$APPLY" | grep -q '^STAGE=SUCCESS$' || fail 'stage apply failed'
printf '%s\n' "$APPLY" | grep -q '^ACTIVE=UNCHANGED$' || fail 'staging activated a controller'
[ -f "$ROOT/freenet-v09/releases/v0.9.0-dev/bin/freenet09" ] || fail 'staged binary not installed'
[ -f "$ROOT/freenet-v09/releases/v0.9.0-dev/SHA256SUMS" ] || fail 'staged manifest absent'
[ ! -e "$ROOT/etc/init.d/S99freenet-v09" ] || fail 'staging created an init service'
[ ! -e "$ROOT/etc/init.d/S98freenet-v09" ] || fail 'staging created an init service'
[ "$(snapshot_native)" = "$BEFORE" ] || fail 'apply changed legacy/native files'
[ ! -d "$ROOT/freenet-v09/.stage-lock" ] || fail 'stage lock leaked'

REPEAT="$(run_stage apply "$BUNDLE")"
printf '%s\n' "$REPEAT" | grep -q '^STAGE=ALREADY_PRESENT$' || fail 'repeat stage not idempotent'
[ "$(snapshot_native)" = "$BEFORE" ] || fail 'repeat changed legacy/native files'

# Existing slot with a different SHA is a hard stop, never overwritten.
printf '%s\n' 'corrupted-slot' >> "$ROOT/freenet-v09/releases/v0.9.0-dev/bin/freenet09"
must_fail apply "$BUNDLE"
[ "$(snapshot_native)" = "$BEFORE" ] || fail 'failed stage changed legacy/native files'
rm -rf "$ROOT/freenet-v09"

# Corrupted download checksum.
printf '%s\n' 'tampered' >> "$BUNDLE/$ASSET"
must_fail apply "$BUNDLE"
[ ! -e "$ROOT/freenet-v09" ] || fail 'bad checksum created stage root'
[ "$(snapshot_native)" = "$BEFORE" ] || fail 'bad checksum changed legacy/native files'

# Correct checksum with the wrong embedded build version.
sed 's/v0\.9\.0-dev/v0.8.0-legacy/g' "$BUNDLE/$ASSET" > "$TEST_DIR/wrong"
mv "$TEST_DIR/wrong" "$BUNDLE/$ASSET"
make_manifest
must_fail apply "$BUNDLE"
[ ! -e "$ROOT/freenet-v09" ] || fail 'bad version created stage root'

# Duplicate manifest entries cannot be accepted.
sed 's/v0\.8\.0-legacy/v0.9.0-dev/g' "$BUNDLE/$ASSET" > "$TEST_DIR/good"
mv "$TEST_DIR/good" "$BUNDLE/$ASSET"
make_manifest
cat "$BUNDLE/SHA256SUMS" >> "$BUNDLE/SHA256SUMS"
must_fail plan "$BUNDLE"
make_manifest

# Symlink native core and partial legacy installs are unknown, not repaired.
mv "$ROOT/sbin/xray" "$TEST_DIR/native-real"
ln -s "$TEST_DIR/native-real" "$ROOT/sbin/xray"
must_fail plan "$BUNDLE"
rm "$ROOT/sbin/xray"
mv "$TEST_DIR/native-real" "$ROOT/sbin/xray"
mv "$ROOT/etc/init.d/S99freenet-ui" "$TEST_DIR/old-init"
must_fail plan "$BUNDLE"
mv "$TEST_DIR/old-init" "$ROOT/etc/init.d/S99freenet-ui"

# An existing symlinked v09 root must not redirect writes elsewhere.
ln -s "$TEST_DIR" "$ROOT/freenet-v09"
must_fail apply "$BUNDLE"
rm "$ROOT/freenet-v09"

[ "$(snapshot_native)" = "$BEFORE" ] || fail 'negative tests changed existing 0.8/XKeen/Xray'
printf '%s\n' 'PASS: FreeNet 0.9 A/B staging, fail-closed checks and legacy preservation'
