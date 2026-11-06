#!/bin/sh
# FreeNet 0.9: stage one verified application alongside 0.8, without cutover.
# NEVER start/stop XKeen, Xray, legacy FreeNet or cron from this script.
set -eu
umask 077

VERSION=v0.9.0-dev
MODE="${1:-}"
BUNDLE="${2:-}"

say() { printf '%s\n' "$*"; }
stop() { say "STOP: $*" >&2; exit 1; }

case "$MODE" in plan|apply|status) ;; *) stop 'usage: stage.sh plan|apply|status [bundle-dir]' ;; esac
if [ "${FREENET_V09_TEST_MODE:-no}" = yes ]; then
    ROOT="${FREENET_V09_TEST_ROOT:-}"
    [ -n "$ROOT" ] || stop 'test root not set'
else
    ROOT=/opt
fi
APP="$ROOT/freenet-v09"
RELEASES="$APP/releases"
SLOT="$RELEASES/$VERSION"
TMP=""
LOCK_HELD=0
LOCK="$APP/.stage-lock"

cleanup() {
    if [ -n "$TMP" ]; then
        case "$TMP" in
            "$RELEASES"/.stage.*) [ ! -L "$TMP" ] && rm -rf "$TMP" 2>/dev/null || : ;;
        esac
    fi
    if [ "$LOCK_HELD" = 1 ]; then
        rmdir "$LOCK" 2>/dev/null || :
    fi
}
trap 'cleanup' 0
trap 'exit 1' 1 2 15

regular() { [ -f "$1" ] && [ ! -L "$1" ]; }
directory() { [ -d "$1" ] && [ ! -L "$1" ]; }

check_existing() {
    directory "$ROOT" || stop 'Entware root absent or symlink'
    regular "$ROOT/sbin/xray" || stop 'native Xray binary not verified'
    regular "$ROOT/sbin/xkeen" || stop 'native XKeen binary not verified'
    directory "$ROOT/etc/xray/configs" || stop 'Xray config directory not verified'
    # Existing FreeNet may be present or absent. Partial legacy install is STOP.
    LEGACY_BIN=0
    LEGACY_INIT=0
    regular "$ROOT/sbin/freenet-ui" && LEGACY_BIN=1
    regular "$ROOT/etc/init.d/S99freenet-ui" && LEGACY_INIT=1
    [ "$LEGACY_BIN" -eq "$LEGACY_INIT" ] || stop 'partial FreeNet 0.8 install'
    [ ! -L "$ROOT/sbin/freenet-ui" ] || stop 'legacy binary is symlink'
    [ ! -L "$ROOT/etc/init.d/S99freenet-ui" ] || stop 'legacy init is symlink'
    [ ! -L "$APP" ] || stop '0.9 root symlink'
    if [ -e "$APP" ]; then directory "$APP" || stop '0.9 root not a directory'; fi
    [ ! -L "$RELEASES" ] || stop '0.9 releases symlink'
    if [ -e "$RELEASES" ]; then directory "$RELEASES" || stop '0.9 releases not a directory'; fi
    [ ! -L "$SLOT" ] || stop '0.9 slot symlink'
    if [ -e "$SLOT" ]; then directory "$SLOT" || stop '0.9 slot not a directory'; fi
}

get_arch() {
    if [ "${FREENET_V09_TEST_MODE:-no}" = yes ]; then
        ARCH="${FREENET_V09_TEST_ARCH:-}"
    else
        [ -x "$ROOT/bin/opkg" ] || stop 'Entware opkg missing'
        ARCH_RAW="$("$ROOT/bin/opkg" print-architecture 2>/dev/null)" || stop 'Entware arch unknown'
        case "$ARCH_RAW" in
            *aarch64*) ARCH=arm64-v8a ;;
            *mipsel*) ARCH=mips32le ;;
            *mips*) ARCH=mips32 ;;
            *) stop 'unsupported Entware architecture' ;;
        esac
    fi
    case "$ARCH" in arm64-v8a|mips32le|mips32) ;; *) stop 'invalid architecture' ;; esac
    ASSET="freenet-v09-$ARCH"
}

verify_bundle() {
    [ -n "$BUNDLE" ] || stop 'bundle directory required'
    directory "$BUNDLE" || stop 'bundle path missing or symlink'
    FILE="$BUNDLE/$ASSET"
    MANIFEST="$BUNDLE/SHA256SUMS"
    regular "$FILE" || stop 'candidate binary missing or symlink'
    regular "$MANIFEST" || stop 'SHA256SUMS missing or symlink'
    command -v sha256sum >/dev/null 2>&1 || stop 'sha256sum missing'
    command -v awk >/dev/null 2>&1 || stop 'awk missing'
    COUNT="$(awk -v f="$ASSET" '$2==f{n++}END{print n+0}' "$MANIFEST")" || stop 'cannot read SHA256SUMS'
    [ "$COUNT" = 1 ] || stop 'manifest asset entry absent/duplicate'
    EXPECTED="$(awk -v f="$ASSET" '$2==f{print $1;exit}' "$MANIFEST")"
    [ "${#EXPECTED}" -eq 64 ] || stop 'invalid SHA-256 length'
    case "$EXPECTED" in *[!0123456789abcdef]*) stop 'invalid SHA-256 syntax' ;; esac
    ACTUAL="$(sha256sum "$FILE" | awk '{print $1}')" || stop 'cannot hash candidate'
    [ "$ACTUAL" = "$EXPECTED" ] || stop 'candidate SHA-256 mismatch'
}

verify_candidate() {
    FILE_TO_CHECK="$1"
    regular "$FILE_TO_CHECK" || stop 'staged binary missing/unsafe'
    GOT="$(sha256sum "$FILE_TO_CHECK" | awk '{print $1}')" || stop 'staged hash unavailable'
    [ "$GOT" = "$EXPECTED" ] || stop 'staged SHA-256 mismatch'
    CANDIDATE_VERSION="$("$FILE_TO_CHECK" -version 2>/dev/null)" || stop 'candidate -version failed'
    [ "$CANDIDATE_VERSION" = "$VERSION" ] || stop 'candidate version mismatch'
    SELFTEST="$("$FILE_TO_CHECK" -selftest 2>/dev/null)" || stop 'candidate selftest failed'
    [ "$SELFTEST" = SELFTEST=SUCCESS ] || stop 'candidate selftest invalid'
}

check_slot() {
    if [ -d "$SLOT" ]; then
        [ ! -L "$SLOT/bin" ] || stop 'slot bin symlink'
        verify_candidate "$SLOT/bin/freenet09"
        say "STAGE=ALREADY_PRESENT"
        return 0
    fi
    say "STAGE=READY"
}

check_existing
if [ "$MODE" = status ]; then
    if [ -d "$SLOT" ]; then
        say "STAGE=PRESENT (runtime NOT_VERIFIED)"
    else
        say "STAGE=ABSENT"
    fi
    say "MUTATION=NONE"
    exit 0
fi

get_arch
verify_bundle
# Candidate execution (-version/-selftest) never touches Xray/FreeNet/cron.
verify_candidate "$FILE"
if [ "$MODE" = plan ]; then
    check_slot
    say "VERSION=$VERSION"
    say "MUTATION=NONE"
    exit 0
fi

# apply only creates the isolated 0.9 tree. It does not add an init script,
# symlink, cron entry or listener; promotion is a different transaction.
if [ "${FREENET_V09_TEST_MODE:-no}" != yes ]; then
    [ "$(id -u)" = 0 ] || stop 'apply requires Entware root'
fi
if [ -e "$APP" ]; then directory "$APP" || stop '0.9 root changed'; else mkdir "$APP" || stop 'create 0.9 root failed'; fi
if ! mkdir "$LOCK" 2>/dev/null; then stop 'staging already active/lock unknown'; fi
LOCK_HELD=1
if [ -e "$RELEASES" ]; then directory "$RELEASES" || stop 'releases changed'; else mkdir "$RELEASES" || stop 'create releases failed'; fi
if [ -e "$SLOT" ]; then
    check_slot
    say "MUTATION=NONE"
    exit 0
fi
TMP="$(mktemp -d "$RELEASES/.stage.XXXXXX")" || stop 'cannot create isolated temporary slot'
mkdir "$TMP/bin" || stop 'cannot create stage bin'
cp "$FILE" "$TMP/bin/freenet09" || stop 'cannot copy candidate'
chmod 700 "$TMP/bin/freenet09" || stop 'cannot chmod candidate'
verify_candidate "$TMP/bin/freenet09"
printf '%s  %s\n' "$EXPECTED" 'bin/freenet09' > "$TMP/SHA256SUMS"
printf '%s\n' "$VERSION" > "$TMP/VERSION"
# Atomic rename on the same filesystem. No post-rename steps can fail.
mv "$TMP" "$SLOT" || stop 'atomic staged install failed'
TMP=""
say "STAGE=SUCCESS"
say "VERSION=$VERSION"
say "ACTIVE=UNCHANGED"
say "XKEEN_XRAY=UNCHANGED"
say "MUTATION=ISOLATED_APP_ONLY"
