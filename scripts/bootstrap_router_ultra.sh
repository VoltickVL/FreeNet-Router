#!/bin/sh

# FreeNet stage-0 bootstrap for stock KeeneticOS / Netcraze OS.
# Intended to be streamed over the router's built-in SSH server into:
#   exec /bin/sh
#
# Responsibilities are deliberately narrow:
# 1) if Entware already exists, hand off directly to the normal FreeNet bootstrap;
# 2) otherwise identify an explicitly supported Ultra/Giga model and one EXT4 partition;
# 3) use the router CLI online "opkg disk <disk> <url>" installer;
# 4) wait for /opt/bin/opkg, verify architecture, install only ca-bundle/curl;
# 5) download and execute the published FreeNet bootstrap.
#
# No XKeen UI installation or lifecycle management belongs here.

PATH="/bin:/sbin:/usr/bin:/usr/sbin"
export PATH

OPT_ROOT="${FREENET_OPT_ROOT:-/opt}"
OPKG="$OPT_ROOT/bin/opkg"
CURL="$OPT_ROOT/bin/curl"
OPT_SH="$OPT_ROOT/bin/sh"
NDMC="${FREENET_NDMC:-ndmc}"
RELEASE_BASE="${FREENET_RELEASE_BASE:-https://github.com/VoltickVL/FreeNet-Router/releases/latest/download}"
WAIT_SECONDS="${FREENET_ENTWARE_WAIT_SECONDS:-240}"
WAIT_STEP="${FREENET_ENTWARE_WAIT_STEP:-2}"
TMP_BOOTSTRAP="${FREENET_TMP_BOOTSTRAP:-/tmp/freenet-bootstrap.sh}"

say() { printf '%s\n' "$*"; }
info() { printf '\n[FreeNet Stage-0] %s\n' "$*"; }
err() { printf '[FreeNet Stage-0] ERROR: %s\n' "$*" >&2; }
stop() { err "$1"; exit "${2:-1}"; }

have_cmd() {
    command -v "$1" >/dev/null 2>&1
}

router_cli() {
    # Empty LD_LIBRARY_PATH avoids inherited /opt library collisions on systems
    # where Entware has already been mounted partially.
    LD_LIBRARY_PATH= "$NDMC" -c "$1"
}

detect_model() {
    MODEL_TEXT="$(router_cli 'show version' 2>/dev/null)" || stop 'cannot read router model via CLI'
    # Accept only the actual model field, not incidental names elsewhere in
    # "show version" (e.g. serial/product notes). Never infer CPU from "Giga".
    MODEL_LINE="$(printf '%s\n' "$MODEL_TEXT" | sed -n 's/^[[:space:]]*model:[[:space:]]*//p' | head -n 1)"
    [ -n "$MODEL_LINE" ] || stop 'router model field is absent; Entware selection stopped'
    case "$MODEL_LINE" in
        "Ultra (KN-1811)"|"Ultra (NC-1812)"|"Giga (NC-1012)")
            EXPECTED_ARCH='aarch64'
            ENTWARE_URL='https://bin.entware.net/aarch64-k3.10/installer/aarch64-installer.tar.gz'
            ;;
        "Ultra (KN-1810)"|"Giga (KN-1010)"|"Giga (KN-1011)"|"Hero (KN-1011)")
            EXPECTED_ARCH='mipsel'
            ENTWARE_URL='https://bin.entware.net/mipselsf-k3.4/installer/mipsel-installer.tar.gz'
            ;;
        *)
            printf '%s\n' "$MODEL_TEXT" >&2
            stop 'unsupported/unknown router model; automatic Entware selection stopped'
            ;;
    esac
    say "[FreeNet Stage-0] MODEL=$MODEL_LINE"
    say "[FreeNet Stage-0] ENTWARE_ARCH=$EXPECTED_ARCH"
}

detect_single_ext4() {
    MEDIA_TEXT="$(router_cli 'show media' 2>/dev/null)" || stop 'cannot read attached storage via CLI'
    EXT4_LIST="$(printf '%s\n' "$MEDIA_TEXT" | awk '
        function emit() {
            if (uuid != "" && fs == "ext4" && state == "MOUNTED") {
                print uuid ":/"
            }
        }
        /^[[:space:]]*partition:/ {
            emit()
            uuid=""; fs=""; state=""
            next
        }
        /^[[:space:]]*uuid:/ {
            sub(/^[[:space:]]*uuid:[[:space:]]*/, "", $0)
            uuid=$0
            next
        }
        /^[[:space:]]*fstype:/ {
            sub(/^[[:space:]]*fstype:[[:space:]]*/, "", $0)
            fs=$0
            next
        }
        /^[[:space:]]*state:/ {
            sub(/^[[:space:]]*state:[[:space:]]*/, "", $0)
            state=$0
            next
        }
        END { emit() }
    ')"
    EXT4_LIST="$(printf '%s\n' "$EXT4_LIST" | sed '/^[[:space:]]*$/d')"
    COUNT="$(printf '%s\n' "$EXT4_LIST" | awk 'NF { n++ } END { print n+0 }')"
    case "$COUNT" in
        1)
            OPKG_DISK="$(printf '%s\n' "$EXT4_LIST" | head -n 1)"
            say "[FreeNet Stage-0] EXT4=$OPKG_DISK"
            ;;
        0)
            stop 'no mounted EXT4 partition found; format/mount exactly one USB partition in the router UI'
            ;;
        *)
            printf '%s\n' "$EXT4_LIST" >&2
            stop 'more than one mounted EXT4 partition found; refusing to guess the OPKG target'
            ;;
    esac
}

wait_for_entware() {
    ELAPSED=0
    while :; do
        if [ -x "$OPKG" ] && "$OPKG" print-architecture >/tmp/freenet-stage0-arch.log 2>/dev/null; then
            say "[FreeNet Stage-0] ENTWARE_READY=yes"
            return 0
        fi
        [ "$ELAPSED" -lt "$WAIT_SECONDS" ] || {
            tail -n 20 /tmp/freenet-stage0-arch.log 2>/dev/null || true
            stop "Entware did not become ready within $WAIT_SECONDS seconds"
        }
        sleep "$WAIT_STEP"
        ELAPSED=$((ELAPSED + WAIT_STEP))
    done
}

verify_entware_arch() {
    ARCH_TEXT="$("$OPKG" print-architecture 2>/dev/null)" || stop 'Entware opkg exists but print-architecture failed'
    case "$EXPECTED_ARCH" in
        aarch64)
            printf '%s\n' "$ARCH_TEXT" | grep -q 'aarch64' || stop 'installed Entware architecture does not match expected aarch64'
            ;;
        mipsel)
            printf '%s\n' "$ARCH_TEXT" | grep -Eq 'mipsel|mipselsf' || stop 'installed Entware architecture does not match expected mipsel'
            ;;
        *)
            stop 'internal architecture contract error'
            ;;
    esac
}

handoff_freenet() {
    "$OPKG" update || stop 'Entware opkg update failed'
    "$OPKG" install ca-bundle curl || stop 'cannot install ca-bundle/curl in Entware'
    [ -x "$CURL" ] || stop "curl was not installed at $CURL"
    [ -x "$OPT_SH" ] || stop "Entware shell was not found at $OPT_SH"

    info 'Downloading published FreeNet bootstrap...'
    "$CURL" -fLsS "$RELEASE_BASE/bootstrap.sh" -o "$TMP_BOOTSTRAP" || stop 'cannot download FreeNet bootstrap'
    [ -s "$TMP_BOOTSTRAP" ] || stop 'downloaded FreeNet bootstrap is empty'
    "$OPT_SH" "$TMP_BOOTSTRAP"
}

info 'Preflight'

if [ -x "$OPKG" ]; then
    say '[FreeNet Stage-0] ENTWARE=existing'
    EXPECTED_ARCH=''
    handoff_freenet
    exit $?
fi

have_cmd "$NDMC" || stop 'router CLI helper ndmc is unavailable'
have_cmd awk || stop 'awk is unavailable in the stock shell'
have_cmd sed || stop 'sed is unavailable in the stock shell'
have_cmd grep || stop 'grep is unavailable in the stock shell'
have_cmd sleep || stop 'sleep is unavailable in the stock shell'

detect_model
detect_single_ext4

info 'Installing Entware through router CLI'
say "[FreeNet Stage-0] PLAN=opkg disk $OPKG_DISK $ENTWARE_URL"
router_cli "opkg disk $OPKG_DISK $ENTWARE_URL" >/tmp/freenet-stage0-opkg.log 2>&1 || {
    tail -n 30 /tmp/freenet-stage0-opkg.log 2>/dev/null || true
    stop 'router rejected online Entware installation; no FreeNet mutation started'
}

wait_for_entware
verify_entware_arch
handoff_freenet
