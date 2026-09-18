#!/bin/sh
set -eu

ROOT_DIR="$(CDPATH= cd "$(dirname "$0")/.." && pwd)"
BRIDGE="$ROOT_DIR/scripts/legacy_update_bridge.sh"
TMP="$(mktemp -d /tmp/freenet-legacy-bridge-test.XXXXXX)"
trap 'rm -rf "$TMP"' EXIT HUP INT TERM

fail() {
    echo "legacy update bridge FAIL: $*" >&2
    exit 1
}

make_release() {
    D="$1"
    rm -rf "$D"
    mkdir -p "$D"
    cat > "$D/self_update.sh" <<'EOF'
#!/bin/sh
printf '%s|%s\n' "$1" "$2" > "$FREENET_BRIDGE_TEST_ARGS_FILE"
exit "${FREENET_BRIDGE_TEST_UPDATER_RC:-0}"
EOF
    chmod 755 "$D/self_update.sh"
    (
        cd "$D"
        sha256sum self_update.sh > SHA256SUMS
    )
}

D="$TMP/release"
ARGS="$TMP/args"
make_release "$D"

# Verified bridge delegates exact mode/tag and does not replace any persistent updater itself.
FREENET_BRIDGE_TEST_RELEASE_DIR="$D" \
FREENET_BRIDGE_TEST_ARGS_FILE="$ARGS" \
sh "$BRIDGE" plan v0.3.84 > "$TMP/plan.out" 2>&1 || {
    cat "$TMP/plan.out" >&2
    fail "verified plan delegation failed"
}
grep -Fq 'BRIDGE_MUTATION=NONE' "$TMP/plan.out" || fail "bridge mutation contract missing"
grep -Fq 'UPDATER_VERIFIED=yes' "$TMP/plan.out" || fail "verification success missing"
[ "$(cat "$ARGS")" = 'plan|v0.3.84' ] || fail "plan args changed"

# A one-shot transport failure must be retried.
rm -f "$ARGS"
FREENET_BRIDGE_TEST_RELEASE_DIR="$D" \
FREENET_BRIDGE_TEST_FAIL_ONCE=self_update.sh \
FREENET_BRIDGE_TEST_ARGS_FILE="$ARGS" \
sh "$BRIDGE" apply v0.3.84 > "$TMP/retry.out" 2>&1 || {
    cat "$TMP/retry.out" >&2
    fail "transient helper download was not retried"
}
[ "$(cat "$ARGS")" = 'apply|v0.3.84' ] || fail "apply args changed after retry"

# SHA mismatch is a hard STOP before delegating.
make_release "$D"
printf '%s\n' '#corrupt' >> "$D/self_update.sh"
rm -f "$ARGS"
if FREENET_BRIDGE_TEST_RELEASE_DIR="$D" \
   FREENET_BRIDGE_TEST_ARGS_FILE="$ARGS" \
   sh "$BRIDGE" apply v0.3.84 > "$TMP/sha.out" 2>&1; then
    fail "SHA mismatch unexpectedly succeeded"
fi
grep -Fq 'SHA-256 mismatch for self_update.sh' "$TMP/sha.out" || {
    cat "$TMP/sha.out" >&2
    fail "SHA mismatch primary error missing"
}
[ ! -e "$ARGS" ] || fail "bridge delegated unverified updater"

# Missing manifest entry is also a hard STOP.
make_release "$D"
printf '%s\n' '0000000000000000000000000000000000000000000000000000000000000000  other.sh' > "$D/SHA256SUMS"
rm -f "$ARGS"
if FREENET_BRIDGE_TEST_RELEASE_DIR="$D" \
   FREENET_BRIDGE_TEST_ARGS_FILE="$ARGS" \
   sh "$BRIDGE" plan v0.3.84 > "$TMP/manifest.out" 2>&1; then
    fail "missing manifest entry unexpectedly succeeded"
fi
grep -Fq 'SHA-256 manifest entry missing for self_update.sh' "$TMP/manifest.out" || {
    cat "$TMP/manifest.out" >&2
    fail "missing manifest primary error missing"
}
[ ! -e "$ARGS" ] || fail "bridge delegated without manifest verification"

# Delegate exit status must remain visible.
make_release "$D"
rm -f "$ARGS"
set +e
FREENET_BRIDGE_TEST_RELEASE_DIR="$D" \
FREENET_BRIDGE_TEST_ARGS_FILE="$ARGS" \
FREENET_BRIDGE_TEST_UPDATER_RC=7 \
sh "$BRIDGE" apply v0.3.84 > "$TMP/delegate.out" 2>&1
RC=$?
set -e
[ "$RC" -eq 7 ] || {
    cat "$TMP/delegate.out" >&2
    fail "delegated updater exit status was not preserved: $RC"
}
grep -Fq 'verified updater returned exit code 7' "$TMP/delegate.out" || fail "delegated failure diagnostic missing"

# Invalid target never reaches the release.
if FREENET_BRIDGE_TEST_RELEASE_DIR="$D" \
   FREENET_BRIDGE_TEST_ARGS_FILE="$ARGS" \
   sh "$BRIDGE" plan latest > "$TMP/tag.out" 2>&1; then
    fail "invalid tag unexpectedly succeeded"
fi
grep -Fq 'target release tag is invalid' "$TMP/tag.out" || fail "invalid tag diagnostic missing"

echo "legacy update bridge PASS"
