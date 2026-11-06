#!/bin/sh
# Fetch an exact official FreeNet 0.9 release and delegate to isolated stage.
# No "latest", no legacy updater, no XKeen/Xray changes, no background jobs.
set -eu
umask 077

say() { printf '%s\n' "$*"; }
stop() { say "STOP: $*" >&2; exit 1; }

MODE="${1:-}"
case "$MODE" in plan|apply|status) ;; *) stop 'usage: prepare.sh plan|apply|status' ;; esac
[ "$#" -eq 1 ] || stop 'unexpected arguments'
SCRIPT_DIR="$(CDPATH= cd "$(dirname "$0")" 2>/dev/null && pwd)" || stop 'installer directory unavailable'
STAGE="$SCRIPT_DIR/stage.sh"
VERSION_FILE="$SCRIPT_DIR/../VERSION"
[ -f "$STAGE" ] && [ ! -L "$STAGE" ] || stop 'verified staging helper unavailable'
[ -f "$VERSION_FILE" ] && [ ! -L "$VERSION_FILE" ] || stop 'exact VERSION unavailable'
VERSION="$(tr -d '\r\n' < "$VERSION_FILE")"
case "$VERSION" in v0.9.0|v0.9.0-dev) ;; *) stop 'unsupported 0.9 version' ;; esac

if [ "$MODE" = status ]; then
    sh "$STAGE" status
    exit $?
fi

if [ "${FREENET_V09_TEST_MODE:-no}" = yes ]; then
    ARCH="${FREENET_V09_TEST_ARCH:-}"
else
    # Development builds are never fetched from a mutable branch/latest URL.
    [ "$VERSION" = v0.9.0 ] || stop '0.9 release not published: production auto-stage disabled'
    [ -x /opt/bin/opkg ] || stop 'Entware opkg unavailable'
    ARCH_RAW="$(/opt/bin/opkg print-architecture 2>/dev/null)" || stop 'cannot inspect Entware architecture'
    case "$ARCH_RAW" in
        *aarch64*) ARCH=arm64-v8a ;;
        *mipsel*) ARCH=mips32le ;;
        *mips*) ARCH=mips32 ;;
        *) stop 'unsupported Entware architecture' ;;
    esac
fi
case "$ARCH" in arm64-v8a|mips32le|mips32) ;; *) stop 'invalid Entware architecture' ;; esac
ASSET="freenet-v09-$ARCH"
TMP="$(mktemp -d /tmp/freenet-v09-download.XXXXXX)" || stop 'cannot allocate temporary release download'
cleanup() {
    case "$TMP" in /tmp/freenet-v09-download.*) [ ! -L "$TMP" ] && rm -rf "$TMP" 2>/dev/null || : ;; esac
}
trap 'cleanup' 0
trap 'exit 1' 1 2 15

if [ "${FREENET_V09_TEST_MODE:-no}" = yes ]; then
    # Offline fixtures: this mode is never selected by normal Browser Setup.
    SOURCE="${FREENET_V09_TEST_RELEASE_DIR:-}"
    [ -n "$SOURCE" ] && [ -d "$SOURCE" ] && [ ! -L "$SOURCE" ] || stop 'offline release fixture missing'
    for NAME in SHA256SUMS "$ASSET"; do
        [ -f "$SOURCE/$NAME" ] && [ ! -L "$SOURCE/$NAME" ] || stop 'offline release file missing/unsafe'
        cp "$SOURCE/$NAME" "$TMP/$NAME" || stop 'offline release copy failed'
    done
else
    command -v curl >/dev/null 2>&1 || stop 'curl unavailable'
    RELEASE_URL="https://github.com/VoltickVL/FreeNet-Router/releases/download/$VERSION"
    for NAME in SHA256SUMS "$ASSET"; do
        # HTTPS is required for both origin and redirect chain.
        # Download exactly one official release, never /latest.
        curl --fail --silent --show-error --location \
            --proto '=https' --proto-redir '=https' \
            --connect-timeout 20 --max-time 180 --max-filesize 83886080 \
            --output "$TMP/$NAME" "$RELEASE_URL/$NAME" \
            || stop "official release asset unavailable: $NAME"
    done
fi

# The reusable staging helper performs SHA-256, exact version/selftest,
# Entware/core/legacy preflight and controlled atomic isolated installation.
sh "$STAGE" "$MODE" "$TMP" || stop 'stage verification/apply failed; native core unchanged'
