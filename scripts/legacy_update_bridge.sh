#!/bin/sh
set -u

# Emergency compatibility bridge for legacy FreeNet releases whose bundled
# self-update helper cannot reliably bootstrap into the current updater.
#
# The bridge itself never replaces live files. It only downloads the exact
# target release manifest + self_update.sh, verifies SHA-256, then delegates
# to that verified updater.

REPO="${FREENET_REPO:-VoltickVL/FreeNet-Router}"
MODE="${1:-plan}"
TARGET_TAG="${2:-}"
RETRIES="${FREENET_BRIDGE_RETRIES:-4}"
TEST_RELEASE_DIR="${FREENET_BRIDGE_TEST_RELEASE_DIR:-}"
TEST_FAIL_ONCE="${FREENET_BRIDGE_TEST_FAIL_ONCE:-}"
TMP_DIR=""
LAST_ERROR=""

say() { printf '%s\n' "$*"; }
err() { printf '[FreeNet Legacy Bridge] ERROR: %s\n' "$*" >&2; }

cleanup() {
    [ -n "$TMP_DIR" ] && rm -rf "$TMP_DIR" 2>/dev/null || true
}
trap cleanup EXIT HUP INT TERM

valid_tag() {
    printf '%s\n' "$1" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+$'
}

make_tmp() {
    TMP_DIR="$(mktemp -d /tmp/freenet-legacy-bridge.XXXXXX 2>/dev/null)" || return 1
    [ -d "$TMP_DIR" ]
}

bootstrap_ip() {
    H="$1"
    command -v nslookup >/dev/null 2>&1 || return 1
    for DNS in 77.88.8.8 8.8.8.8; do
        IP="$(nslookup "$H" "$DNS" 2>/dev/null | awk '
            /^Name:/ {seen=1; next}
            seen && /^Address [0-9]+:/ {if ($3 ~ /^[0-9]+\./) {print $3; exit}}
            seen && /^Address:/ {if ($2 ~ /^[0-9]+\./) {print $2; exit}}
        ')"
        [ -n "$IP" ] && { printf '%s\n' "$IP"; return 0; }
    done
    return 1
}

url_host() {
    printf '%s\n' "$1" | sed -n 's#^https://\([^/]*\)/.*#\1#p'
}

download_once() {
    URL="$1"
    OUT="$2"
    NAME="$3"

    if [ -n "$TEST_RELEASE_DIR" ]; then
        if [ -n "$TEST_FAIL_ONCE" ] && [ "$TEST_FAIL_ONCE" = "$NAME" ] && [ ! -f "$TMP_DIR/.failed-once-$NAME" ]; then
            : > "$TMP_DIR/.failed-once-$NAME"
            LAST_ERROR="temporary download failure for $NAME"
            return 1
        fi
        [ -f "$TEST_RELEASE_DIR/$NAME" ] || {
            LAST_ERROR="release asset missing: $NAME"
            return 1
        }
        cp "$TEST_RELEASE_DIR/$NAME" "$OUT" || {
            LAST_ERROR="cannot copy release asset: $NAME"
            return 1
        }
        return 0
    fi

    CUR="$URL"
    HOP=0
    while [ "$HOP" -lt 8 ]; do
        HOP=$((HOP + 1))
        HOST="$(url_host "$CUR")"
        [ -n "$HOST" ] || {
            LAST_ERROR="invalid HTTPS URL for $NAME"
            return 1
        }

        HDR="$TMP_DIR/headers.$NAME.$HOP"
        BODY="$TMP_DIR/body.$NAME.$HOP"
        ERRFILE="$TMP_DIR/curl.$NAME.$HOP.err"
        rm -f "$HDR" "$BODY" "$ERRFILE"

        IP="$(bootstrap_ip "$HOST" 2>/dev/null || true)"
        if [ -n "$IP" ]; then
            curl -fsS --connect-timeout 20 --max-time 180 --resolve "$HOST:443:$IP" -D "$HDR" "$CUR" -o "$BODY" 2>"$ERRFILE"
            RC=$?
        else
            curl -fsS --connect-timeout 20 --max-time 180 -D "$HDR" "$CUR" -o "$BODY" 2>"$ERRFILE"
            RC=$?
        fi

        if [ "$RC" -ne 0 ]; then
            LAST_ERROR="download failed for $NAME from $HOST"
            return 1
        fi

        CODE="$(awk '/^HTTP\// {code=$2} END {print code}' "$HDR")"
        case "$CODE" in
            200|206)
                mv -f "$BODY" "$OUT" || {
                    LAST_ERROR="cannot save downloaded asset: $NAME"
                    return 1
                }
                return 0
                ;;
            301|302|303|307|308)
                LOC="$(sed -n 's/^[Ll]ocation:[[:space:]]*//p' "$HDR" | tr -d '\r' | tail -n 1)"
                [ -n "$LOC" ] || {
                    LAST_ERROR="redirect without Location for $NAME"
                    return 1
                }
                case "$LOC" in
                    https://*) CUR="$LOC" ;;
                    /*) CUR="https://$HOST$LOC" ;;
                    *)
                        LAST_ERROR="unsupported redirect for $NAME"
                        return 1
                        ;;
                esac
                ;;
            *)
                LAST_ERROR="HTTP ${CODE:-UNKNOWN} while downloading $NAME from $HOST"
                return 1
                ;;
        esac
    done

    LAST_ERROR="too many redirects while downloading $NAME"
    return 1
}

download_with_retry() {
    URL="$1"
    OUT="$2"
    NAME="$3"
    ATTEMPT=1
    while [ "$ATTEMPT" -le "$RETRIES" ]; do
        rm -f "$OUT" 2>/dev/null || true
        if download_once "$URL" "$OUT" "$NAME"; then
            return 0
        fi
        [ "$ATTEMPT" -lt "$RETRIES" ] && sleep "$ATTEMPT"
        ATTEMPT=$((ATTEMPT + 1))
    done
    LAST_ERROR="${LAST_ERROR:-download failed for $NAME} after $RETRIES attempts"
    return 1
}

manifest_expected() {
    awk '$2=="self_update.sh" {print $1; exit}' "$TMP_DIR/SHA256SUMS"
}

verify_updater() {
    EXPECTED="$(manifest_expected)"
    printf '%s\n' "$EXPECTED" | grep -Eq '^[0-9a-f]{64}$' || {
        LAST_ERROR="SHA-256 manifest entry missing for self_update.sh"
        return 1
    }

    ACTUAL="$(sha256sum "$TMP_DIR/self_update.sh" 2>/dev/null | awk '{print $1}')"
    [ "$EXPECTED" = "$ACTUAL" ] || {
        LAST_ERROR="SHA-256 mismatch for self_update.sh"
        return 1
    }
    return 0
}

case "$MODE" in
    plan|apply) ;;
    *)
        err "usage: legacy_update_bridge.sh <plan|apply> <vX.Y.Z>"
        exit 2
        ;;
esac

[ -n "$TARGET_TAG" ] || {
    err "target release tag is required"
    exit 2
}
valid_tag "$TARGET_TAG" || {
    err "target release tag is invalid"
    exit 2
}

for T in curl sha256sum awk grep sed tr tail mktemp cp chmod mv sleep; do
    command -v "$T" >/dev/null 2>&1 || {
        err "required command missing: $T"
        exit 2
    }
done

make_tmp || {
    err "cannot create temporary bridge directory"
    exit 2
}

BASE="https://github.com/$REPO/releases/download/$TARGET_TAG"

say "BRIDGE_TARGET=$TARGET_TAG"
say "BRIDGE_MODE=$MODE"
say "BRIDGE_MUTATION=NONE"

download_with_retry "$BASE/SHA256SUMS" "$TMP_DIR/SHA256SUMS" "SHA256SUMS" || {
    err "$LAST_ERROR"
    exit 2
}

download_with_retry "$BASE/self_update.sh" "$TMP_DIR/self_update.sh" "self_update.sh" || {
    err "$LAST_ERROR"
    exit 2
}

verify_updater || {
    err "$LAST_ERROR"
    exit 2
}

chmod 755 "$TMP_DIR/self_update.sh" || {
    err "cannot make verified updater executable"
    exit 2
}

say "UPDATER_VERIFIED=yes"

sh "$TMP_DIR/self_update.sh" "$MODE" "$TARGET_TAG"
RC=$?
if [ "$RC" -eq 0 ]; then
    exit 0
fi

err "verified updater returned exit code $RC"
exit "$RC"
