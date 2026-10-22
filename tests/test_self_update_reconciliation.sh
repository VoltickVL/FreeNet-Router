#!/bin/sh
set -eu

# CI integration contract. Do not run this suite on a router. The native fixture
# owns real loopback sockets and /proc identities; only release downloads are
# supplied from a private fixture directory. TEST_MODE is deliberately disabled.
ROOT_DIR="$(CDPATH= cd "$(dirname "$0")/.." && pwd)"
SCRIPT="$ROOT_DIR/scripts/self_update.sh"
SUITE="$(mktemp -d /tmp/freenet-update-reconcile-test.XXXXXX)"
BASE_PATH="$PATH"
FROM=v0.9.10
TO=v0.9.11
ASSETS='freenet-ui-arm64-v8a freenet vpn blanc_xkeen_update_outbounds.sh migrate_split_dns.sh apply_network_profile.sh apply_provider_profile.sh finalize_setup.sh bootstrap_entware.sh upstream-pins.env self_update.sh'
CASE_DIR=""
export FREENET_FIXTURE_SCRIPT="$SCRIPT" FREENET_FIXTURE_TO="$TO"

fail() {
    printf 'self update reconciliation FAIL: %s\n' "$*" >&2
    if [ -n "$CASE_DIR" ] && [ -f "$CASE_DIR/command.out" ]; then
        tail -n 30 "$CASE_DIR/command.out" >&2
    fi
    exit 1
}

fixture_alive() {
    fa_pid="$1"
    case "$fa_pid" in ''|*[!0-9]*) return 1 ;; esac
    fa_exe="$(readlink "/proc/$fa_pid/exe" 2> "$SUITE/readlink.err")" || return 1
    case "$fa_exe" in
        "$SUITE"/*/root/sbin/freenet-ui|"$SUITE"/*/root/sbin/freenet-ui\ \(deleted\)|"$SUITE"/*/root/sbin/xray) ;;
        *) return 1 ;;
    esac
    kill -0 "$fa_pid" 2> "$SUITE/kill.err"
}

stop_fixtures() {
    for sf_info in "$SUITE"/*/control/process.*; do
        [ -f "$sf_info" ] || continue
        sf_pid="${sf_info##*.}"
        if fixture_alive "$sf_pid"; then
            kill -TERM "$sf_pid" 2> "$SUITE/kill.err" || true
        fi
    done
    sf_count=0
    while [ "$sf_count" -lt 100 ]; do
        sf_live=no
        for sf_info in "$SUITE"/*/control/process.*; do
            [ -f "$sf_info" ] || continue
            sf_pid="${sf_info##*.}"
            wait "$sf_pid" 2> "$SUITE/wait.err" || true
            fixture_alive "$sf_pid" && sf_live=yes
        done
        [ "$sf_live" = yes ] || return 0
        sf_count=$((sf_count + 1))
        sleep 0.05
    done
    for sf_info in "$SUITE"/*/control/process.*; do
        [ -f "$sf_info" ] || continue
        sf_pid="${sf_info##*.}"
        fixture_alive "$sf_pid" && kill -KILL "$sf_pid" 2> "$SUITE/kill.err" || true
        wait "$sf_pid" 2> "$SUITE/wait.err" || true
    done
    sf_count=0
    while [ "$sf_count" -lt 100 ]; do
        sf_live=no
        for sf_info in "$SUITE"/*/control/process.*; do
            [ -f "$sf_info" ] || continue
            sf_pid="${sf_info##*.}"
            fixture_alive "$sf_pid" && sf_live=yes
        done
        [ "$sf_live" = yes ] || return 0
        sf_count=$((sf_count + 1))
        sleep 0.05
    done
    printf '%s\n' 'fixture process exit was not confirmed; private files retained' >&2
    return 1
}

cleanup() {
    stop_fixtures || return 1
    rm -rf "$SUITE"
}
trap cleanup EXIT HUP INT TERM

asset_destination() {
    case "$1" in
        freenet-ui-arm64-v8a) printf '%s\n' "$R/sbin/freenet-ui" ;;
        freenet|vpn|blanc_xkeen_update_outbounds.sh) printf '%s\n' "$R/bin/$1" ;;
        upstream-pins.env) printf '%s\n' "$R/etc/freenet/$1" ;;
        *) printf '%s\n' "$R/lib/freenet/$1" ;;
    esac
}

make_release() {
    mr_dir="$1"; mr_version="$2"; mr_binary="$3"
    mkdir -p "$mr_dir"
    cp "$mr_binary" "$mr_dir/freenet-ui-arm64-v8a"
    for mr_name in freenet vpn blanc_xkeen_update_outbounds.sh migrate_split_dns.sh apply_network_profile.sh apply_provider_profile.sh finalize_setup.sh bootstrap_entware.sh; do
        printf '#!/bin/sh\n# synthetic application asset %s\nexit 0\n' "$mr_version" > "$mr_dir/$mr_name"
    done
    cp "$SCRIPT" "$mr_dir/self_update.sh"
    printf 'PIN_POLICY_VERSION=%s\n' "$mr_version" > "$mr_dir/upstream-pins.env"
    printf '{"tag_name":"%s","draft":false,"prerelease":false}\n' "$mr_version" > "$mr_dir/release.json"
    for mr_name in $ASSETS; do chmod 755 "$mr_dir/$mr_name"; done
    chmod 600 "$mr_dir/upstream-pins.env" "$mr_dir/release.json"
    (cd "$mr_dir" && sha256sum $ASSETS > SHA256SUMS)
    chmod 600 "$mr_dir/SHA256SUMS"
}

# The target filename exercises the updater's supported architecture mapping;
# its contents are native CI ELF so executable inode and /proc tests are real.
go build -trimpath -ldflags "-X main.fixtureVersion=$FROM" -o "$SUITE/from-ui" "$ROOT_DIR/tests/fixtures/self_update_daemon.go"
go build -trimpath -ldflags "-X main.fixtureVersion=$TO" -o "$SUITE/to-ui" "$ROOT_DIR/tests/fixtures/self_update_daemon.go"
FROM_RELEASE="$SUITE/releases/from"
TO_RELEASE="$SUITE/releases/to"
make_release "$FROM_RELEASE" "$FROM" "$SUITE/from-ui"
make_release "$TO_RELEASE" "$TO" "$SUITE/to-ui"
TO_UI_SHA="$(sha256sum "$SUITE/to-ui" | awk '{print $1}')"
REAL_CP="$(command -v cp)"
REAL_MV="$(command -v mv)"
REAL_SYNC="$(command -v sync)"
REAL_CURL="$(command -v curl)"

new_case() {
    stop_fixtures
    CASE_DIR="$SUITE/$1"
    R="$CASE_DIR/root"
    CONTROL="$CASE_DIR/control"
    mkdir -p "$R/sbin" "$R/bin" "$R/lib/freenet" "$R/etc/freenet" \
        "$R/etc/xray/configs" "$R/etc/init.d" "$R/var/run" "$R/var/lib/freenet" \
        "$R/backups" "$CONTROL" "$CASE_DIR/bin"
    chmod 700 "$R" "$R/var/run" "$R/backups" "$CONTROL"
    for nc_name in $ASSETS; do
        cp -p "$FROM_RELEASE/$nc_name" "$(asset_destination "$nc_name")"
    done
    cp -p "$SUITE/from-ui" "$R/sbin/xray"
    PORT="$("$SUITE/from-ui" free-port)"
    printf 'UI_PORT=%s\n' "$PORT" > "$R/etc/freenet/freenet.conf"
    printf '%s\n' "$FROM" > "$R/etc/freenet/version"
    printf '%s\n' 'Fixture profile' > "$R/etc/freenet/vpn_profile_name"
    printf '%s\n' '^fixture$' > "$R/etc/xray/blanc_profile_filter.regex"
    printf '%s\n' 'https://subscription.example.invalid/fixture' > "$R/etc/xray/blanc_subscription.url"
    printf '%s\n' '{"outbounds":[{"tag":"fixture","protocol":"freedom"}]}' > "$R/etc/xray/configs/04_outbounds.json"
    printf '%s\n' '{"routing":{"rules":[]}}' > "$R/etc/xray/configs/05_routing.json"
    chmod 600 "$R/etc/freenet/freenet.conf" "$R/etc/freenet/version" "$R/etc/freenet/vpn_profile_name" \
        "$R/etc/xray/blanc_profile_filter.regex" "$R/etc/xray/blanc_subscription.url" "$R/etc/xray/configs/"*.json
    # This is the stock init grammar parsed as DATA. The /opt source line must
    # never execute: runtime control must use the proved daemon, not rc.func.
    cat > "$R/etc/init.d/S99freenet-ui" <<EOF
#!/bin/sh
ENABLED=yes
PROCS=freenet-ui
ARGS="-listen 127.0.0.1:$PORT"
PREARGS=""
DESC="\$PROCS"
PATH=/opt/sbin:/opt/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
. /opt/etc/init.d/rc.func
EOF
    chmod 755 "$R/etc/init.d/S99freenet-ui"
    export FREENET_ROOT="$R" FREENET_CURRENT_VERSION="$FROM" FREENET_ARCH=arm64-v8a \
        FREENET_LATEST_TAG="$TO" FREENET_VERSION_FILE="$R/etc/freenet/version" \
        FREENET_UPDATE_STATE_FILE="$R/var/run/update.state" FREENET_UPDATE_LOCK_DIR="$R/var/run/update.lock" \
        FREENET_UPDATE_MUTATION_LOCK="$R/var/run/network.lock" \
        FREENET_VPN_TRANSACTION_DIR="$R/var/lib/freenet/vpn-transaction.pending" \
        FREENET_AUTOMATION_STATE="$R/var/run/automation.state" FREENET_PROC_ROOT=/proc \
        FREENET_SELF_UPDATE_TEST_MODE=no FREENET_TEST_RELEASE_DIR="$TO_RELEASE" \
        FREENET_FIXTURE_CONTROL="$CONTROL" FREENET_FIXTURE_FAULT='' \
        FREENET_TEST_FAIL_STAGE='' FREENET_TEST_ROLLBACK_FAIL=no \
        FREENET_TEST_DOWNLOAD_FAIL_ONCE='' FREENET_TEST_VERIFY_FAIL_ONCE=''
    PATH="$BASE_PATH"
    export PATH
    start_xray
}

wait_ready() {
    wr_pid="$1"; wr_count=0
    while [ "$wr_count" -lt 100 ]; do
        [ ! -s "$CONTROL/ready.$wr_pid" ] || return 0
        fixture_alive "$wr_pid" || fail 'native fixture exited before ready'
        wr_count=$((wr_count + 1))
        sleep 0.05
    done
    fail 'native fixture readiness exceeded five seconds'
}

start_daemon() {
    "$R/sbin/freenet-ui" -listen "127.0.0.1:$PORT" > "$CASE_DIR/daemon.out" 2>&1 &
    DAEMON_PID=$!
    wait_ready "$DAEMON_PID"
}

start_cli() {
    "$R/sbin/freenet-ui" automation-health-watch --config "$R/etc/freenet/freenet.conf" > "$CASE_DIR/cli.out" 2>&1 &
    CLI_PID=$!
    wait_ready "$CLI_PID"
}

start_xray() {
    XRAY_LOCATION_CONFDIR="$R/etc/xray/configs" FREENET_XRAY_PROBE='' \
        "$R/sbin/xray" run > "$CASE_DIR/xray.out" 2>&1 &
    XRAY_PID=$!
    wait_ready "$XRAY_PID"
}

start_marked_probe() {
    ln -s "$R/sbin/xray" "$R/bin/fn-xray-probe"
    XRAY_LOCATION_CONFDIR="$R/etc/xray/configs" FREENET_XRAY_PROBE=1 \
        "$R/bin/fn-xray-probe" run > "$CASE_DIR/probe.out" 2>&1 &
    PROBE_PID=$!
    wait_ready "$PROBE_PID"
    [ "$(cat "/proc/$PROBE_PID/comm")" = fn-xray-probe ] || fail 'fixture did not create the real marked probe role'
}

run_apply() {
    timeout 100 sh -c 'exec sh "$FREENET_FIXTURE_SCRIPT" apply "$FREENET_FIXTURE_TO"' > "$CASE_DIR/command.out" 2>&1
}

state_value() { sed -n "s/^$1=//p" "$FREENET_UPDATE_STATE_FILE"; }

assert_http_version() {
    ah_expected="$1"
    [ "$(curl -q -fsS --noproxy '*' --connect-timeout 1 --max-time 2 "http://127.0.0.1:$PORT/healthz")" = ok ] || fail 'HTTP health not accepted'
    [ "$(curl -q -fsS --noproxy '*' --connect-timeout 1 --max-time 2 "http://127.0.0.1:$PORT/versionz")" = "$ah_expected" ] || fail 'HTTP version does not match receipt'
}

assert_assets() {
    aa_release="$1"
    for aa_name in $ASSETS; do
        cmp -s "$aa_release/$aa_name" "$(asset_destination "$aa_name")" || fail "application asset mismatch: $aa_name"
    done
}

assert_cli_untouched() {
    fixture_alive "$CLI_PID" || fail 'updater terminated the same-executable CLI worker'
    [ ! -e "$CONTROL/signal.$CLI_PID" ] || fail 'CLI worker received an updater signal'
}

# A daemon and cron worker share an ELF/name, but only the socket owner can be
# restarted. The first two target version responses are deliberately incorrect.
new_case daemon-and-cli
start_daemon
OLD_DAEMON_PID="$DAEMON_PID"
start_cli
printf '%s\n' 'wrong:2' > "$CONTROL/$TO.versionz"
run_apply || fail 'real daemon update failed'
[ "$(state_value STATE)" = SUCCESS ] || fail 'runtime acceptance did not commit SUCCESS'
[ "$(state_value ROLLBACK_STATE)" = NOT_NEEDED ] || fail 'successful update recorded rollback'
[ -s "$CONTROL/signal.$OLD_DAEMON_PID" ] || fail 'identified old HTTP daemon was not stopped'
assert_cli_untouched
assert_http_version "$TO"
assert_assets "$TO_RELEASE"
[ "$(cat "$FREENET_VERSION_FILE")" = "$TO" ] || fail 'target version marker missing'
[ ! -e "$FREENET_UPDATE_LOCK_DIR" ] && [ ! -e "$FREENET_UPDATE_MUTATION_LOCK" ] || fail 'successful update retained active fences'

# A CLI process alone is not an HTTP service and must not cause "already
# running" acceptance. A real target listener must be created without killing it.
new_case cli-only
start_cli
run_apply || fail 'CLI-only baseline prevented a real daemon start'
assert_cli_untouched
assert_http_version "$TO"
[ "$(state_value STATE)" = SUCCESS ] || fail 'CLI-only update lacked actual acceptance'

# A native target remains executing while rollback replaces its image. An
# in-place cp would fail ETXTBSY. Observe the actual rollback copy boundary, then
# require full old-version health acceptance, all assets, and the live CLI.
new_case running-inode-rollback
start_daemon
start_cli
cat > "$CASE_DIR/bin/cp" <<'EOF'
#!/bin/sh
case " $* " in
    *'/asset-1.before '*)
        for f in "$FREENET_FIXTURE_CONTROL"/process.*; do
            [ -f "$f" ] || continue
            [ "$(cat "$f")" = "daemon $FREENET_FIXTURE_TARGET_VERSION" ] || continue
            p="${f##*.}"
            [ -r "/proc/$p/exe" ] || continue
            actual="$(sha256sum "/proc/$p/exe" | awk '{print $1}')"
            [ "$actual" = "$FREENET_FIXTURE_TARGET_SHA" ] || continue
            live_inode="$(stat -L -c '%d:%i' "$FREENET_ROOT/sbin/freenet-ui")"
            running_inode="$(stat -L -c '%d:%i' "/proc/$p/exe")"
            if [ "$live_inode" = "$running_inode" ]; then
                printf '%s\n' 'running target owns the destination inode' > "$FREENET_FIXTURE_CONTROL/rollback-running-inode"
            fi
        done
        ;;
esac
exec "$FREENET_FIXTURE_REAL_CP" "$@"
EOF
chmod 755 "$CASE_DIR/bin/cp"
export FREENET_FIXTURE_REAL_CP="$REAL_CP" FREENET_FIXTURE_TARGET_VERSION="$TO" FREENET_FIXTURE_TARGET_SHA="$TO_UI_SHA"
PATH="$CASE_DIR/bin:$BASE_PATH"; export PATH
printf '%s\n' stall > "$CONTROL/$TO.healthz"
STARTED_AT="$(date +%s)"
if run_apply; then fail 'stalled target health was accepted'; else APPLY_RC=$?; fi
[ "$APPLY_RC" -ne 124 ] || fail 'runtime acceptance exceeded its outer test bound'
[ "$(( $(date +%s) - STARTED_AT ))" -lt 90 ] || fail 'stalled HTTP acceptance was not bounded'
[ -s "$CONTROL/rollback-running-inode" ] || fail 'rollback did not exercise a still-running destination inode'
[ "$(state_value STATE)" = FAILED ] && [ "$(state_value ROLLBACK_STATE)" = SUCCESS ] || fail 'atomic rollback was not fully accepted'
assert_assets "$FROM_RELEASE"
assert_http_version "$FROM"
assert_cli_untouched
[ "$(cat "$FREENET_VERSION_FILE")" = "$FROM" ] || fail 'rollback version marker changed'

# The reconciliation cases below begin with a genuine UNKNOWN updater receipt.
# Restoring fixture files/start is setup, not part of the command under test.
new_case reconcile
start_daemon
start_cli
FREENET_TEST_FAIL_STAGE=accept; FREENET_TEST_ROLLBACK_FAIL=yes
export FREENET_TEST_FAIL_STAGE FREENET_TEST_ROLLBACK_FAIL
if run_apply; then fail 'faulted apply did not produce UNKNOWN'; else UNKNOWN_RC=$?; fi
[ "$UNKNOWN_RC" -ne 124 ] || fail 'UNKNOWN fixture setup timed out'
[ "$(state_value STATE)" = ROLLBACK_FAILED ] && [ "$(state_value ROLLBACK_STATE)" = FAILED_UNKNOWN ] || fail 'fixture is not a real unresolved receipt'
BACKUP="$(state_value BACKUP_DIR)"
case "$BACKUP" in "$R/backups/"*) ;; *) fail 'backup escaped private fixture root' ;; esac
[ -d "$FREENET_UPDATE_LOCK_DIR" ] && [ -d "$FREENET_UPDATE_MUTATION_LOCK" ] || fail 'UNKNOWN did not keep both fences'
cp "$FREENET_UPDATE_STATE_FILE" "$CASE_DIR/original.state"
UNKNOWN_SHA="$(sha256sum "$CASE_DIR/original.state" | awk '{print $1}')"
stop_fixtures
RESTORE_INDEX=0
for RESTORE_NAME in $ASSETS; do
    RESTORE_INDEX=$((RESTORE_INDEX + 1))
    RESTORE_DEST="$(asset_destination "$RESTORE_NAME")"
    cp -p "$BACKUP/asset-$RESTORE_INDEX.before" "$RESTORE_DEST.fixture-restore"
    mv -f "$RESTORE_DEST.fixture-restore" "$RESTORE_DEST"
done
cp -p "$BACKUP/version-marker.before" "$FREENET_VERSION_FILE"
FREENET_TEST_FAIL_STAGE=''; FREENET_TEST_ROLLBACK_FAIL=no
FREENET_TEST_RELEASE_DIR="$FROM_RELEASE"
export FREENET_TEST_FAIL_STAGE FREENET_TEST_ROLLBACK_FAIL FREENET_TEST_RELEASE_DIR
start_xray
RESTORED_XRAY_PID="$XRAY_PID"
start_daemon
RESTORED_PID="$DAEMON_PID"
start_cli
start_marked_probe
assert_assets "$FROM_RELEASE"
assert_http_version "$FROM"
cmp -s "$CASE_DIR/original.state" "$FREENET_UPDATE_STATE_FILE" || fail 'setup changed the unresolved receipt'

snapshot_paths() {
    sp_output="$1"; shift
    {
        for sp_path in "$@"; do
            if [ -L "$sp_path" ]; then
                printf 'LINK %s %s\n' "$sp_path" "$(readlink "$sp_path")"
            elif [ -d "$sp_path" ]; then
                find "$sp_path" -print | LC_ALL=C sort | while IFS= read -r sp_item; do
                    if [ -L "$sp_item" ]; then
                        printf 'LINK %s %s\n' "$sp_item" "$(readlink "$sp_item")"
                    elif [ -f "$sp_item" ]; then
                        printf 'FILE %s %s ' "$sp_item" "$(stat -c '%a:%u:%g:%h:%s:%d:%i:%Y' "$sp_item")"
                        sha256sum "$sp_item"
                    elif [ -d "$sp_item" ]; then
                        printf 'DIR %s %s\n' "$sp_item" "$(stat -c '%a:%u:%g:%d:%i' "$sp_item")"
                    else
                        printf 'OTHER %s\n' "$sp_item"
                    fi
                done
            elif [ -f "$sp_path" ]; then
                printf 'FILE %s %s ' "$sp_path" "$(stat -c '%a:%u:%g:%h:%s:%d:%i:%Y' "$sp_path")"
                sha256sum "$sp_path"
            else
                printf 'ABSENT %s\n' "$sp_path"
            fi
        done
    } > "$sp_output"
}

snapshot_guarded() {
    snapshot_paths "$1" "$R/sbin" "$R/bin" "$R/lib" "$R/etc" "$R/backups" \
        "$FREENET_UPDATE_STATE_FILE" "$FREENET_UPDATE_LOCK_DIR" "$FREENET_UPDATE_MUTATION_LOCK" \
        "$FREENET_VPN_TRANSACTION_DIR"
}

run_reconcile_plan() {
    timeout 40 sh -c 'exec sh "$FREENET_FIXTURE_SCRIPT" reconcile-rollback-plan' > "$CASE_DIR/command.out" 2>&1
}

run_reconcile() {
    FREENET_FIXTURE_RECEIPT="$1"; export FREENET_FIXTURE_RECEIPT
    timeout 40 sh -c 'exec sh "$FREENET_FIXTURE_SCRIPT" reconcile-rollback "$FREENET_FIXTURE_RECEIPT"' > "$CASE_DIR/command.out" 2>&1
}

assert_unknown_unchanged() {
    cmp -s "$CASE_DIR/original.state" "$FREENET_UPDATE_STATE_FILE" || fail 'unconfirmed reconciliation changed the UNKNOWN receipt'
    [ "$(state_value STATE)" = ROLLBACK_FAILED ] && [ "$(state_value ROLLBACK_STATE)" = FAILED_UNKNOWN ] || fail 'unconfirmed reconciliation normalized UNKNOWN'
    [ -e "$FREENET_UPDATE_LOCK_DIR/owner.pid" ] && [ -e "$FREENET_UPDATE_MUTATION_LOCK/pid" ] || fail 'reconciliation removed an original fence'
    [ ! -e "$FREENET_UPDATE_LOCK_DIR.reconcile-guard" ] || fail 'command leaked its temporary serialization guard'
}

assert_refused() {
    ar_label="$1"
    snapshot_guarded "$CASE_DIR/refusal.before"
    if run_reconcile "$UNKNOWN_SHA"; then fail "unsafe reconciliation accepted: $ar_label"; else ar_rc=$?; fi
    [ "$ar_rc" -ne 124 ] || fail "unbounded refusal: $ar_label"
    snapshot_guarded "$CASE_DIR/refusal.after"
    cmp -s "$CASE_DIR/refusal.before" "$CASE_DIR/refusal.after" || fail "refusal wrote protected state: $ar_label"
    [ ! -e "$FREENET_UPDATE_LOCK_DIR.reconcile-guard" ] || fail "refusal retained private guard: $ar_label"
    for ar_tmp in "$FREENET_UPDATE_STATE_FILE".reconcile.*; do
        [ ! -e "$ar_tmp" ] || fail "refusal retained a staged terminal receipt: $ar_label"
    done
}

# A read-only plan must prove restored bytes AND the real HTTP daemon, without
# even rewriting an equivalent receipt, backup or lock file.
snapshot_paths "$CASE_DIR/plan.before" "$R"
run_reconcile_plan || fail 'fully restored UNKNOWN was not plan-ready'
grep -qx 'READY=yes' "$CASE_DIR/command.out" || fail 'reconciliation plan readiness missing'
grep -qx "RECEIPT_SHA256=$UNKNOWN_SHA" "$CASE_DIR/command.out" || fail 'plan did not pin exact original receipt'
grep -qx 'MUTATION=NONE' "$CASE_DIR/command.out" || fail 'plan must report no persistent mutation'
snapshot_paths "$CASE_DIR/plan.after" "$R"
cmp -s "$CASE_DIR/plan.before" "$CASE_DIR/plan.after" || fail 'read-only reconciliation plan wrote persistent state'
assert_cli_untouched

# The caller must acknowledge this exact receipt, not an old plan or a merely
# well-formed digest. There is no apply/recovery fallback after a refusal.
snapshot_guarded "$CASE_DIR/hash.before"
if run_reconcile 0000000000000000000000000000000000000000000000000000000000000000; then
    fail 'wrong expected receipt SHA was accepted'
fi
snapshot_guarded "$CASE_DIR/hash.after"
cmp -s "$CASE_DIR/hash.before" "$CASE_DIR/hash.after" || fail 'wrong receipt SHA changed persistent state'
assert_unknown_unchanged

# Every one of the 11 application assets is covered by the FROM manifest. Keep
# the original inode aside so a running daemon is restored exactly after each
# negative case; never write to an executing ELF in place, even in test setup.
for BAD_NAME in $ASSETS; do
    BAD_DEST="$(asset_destination "$BAD_NAME")"
    mv "$BAD_DEST" "$CASE_DIR/saved-asset"
    cp -p "$CASE_DIR/saved-asset" "$BAD_DEST"
    printf '\nfixture asset mismatch\n' >> "$BAD_DEST"
    assert_refused "live asset $BAD_NAME differs from FROM"
    rm "$BAD_DEST"
    mv "$CASE_DIR/saved-asset" "$BAD_DEST"
done

# Matching a forged backup to a forged live asset is insufficient: both must
# match the verified exact FROM release. This is not just a backup/live cmp.
mv "$R/bin/freenet" "$CASE_DIR/saved-live"
mv "$BACKUP/asset-2.before" "$CASE_DIR/saved-backup"
printf '#!/bin/sh\n# same forged live and backup\nexit 0\n' > "$R/bin/freenet"
chmod 755 "$R/bin/freenet"
cp -p "$R/bin/freenet" "$BACKUP/asset-2.before"
assert_refused 'forged backup and live asset agree but FROM manifest differs'
rm "$R/bin/freenet" "$BACKUP/asset-2.before"
mv "$CASE_DIR/saved-live" "$R/bin/freenet"
mv "$CASE_DIR/saved-backup" "$BACKUP/asset-2.before"

for BAD_PATH in "$FREENET_VERSION_FILE" "$R/etc/freenet/freenet.conf" \
    "$R/etc/freenet/vpn_profile_name" "$R/etc/xray/blanc_profile_filter.regex" \
    "$R/etc/xray/blanc_subscription.url" "$R/etc/xray/configs/04_outbounds.json"; do
    mv "$BAD_PATH" "$CASE_DIR/saved-protected"
    cp -p "$CASE_DIR/saved-protected" "$BAD_PATH"
    printf '\nfixture mismatch\n' >> "$BAD_PATH"
    assert_refused 'version or preserved VPN/Xray evidence differs'
    rm "$BAD_PATH"
    mv "$CASE_DIR/saved-protected" "$BAD_PATH"
done

# Symlink aliases with matching bytes are not owned original evidence.
for LINK_PATH in "$FREENET_UPDATE_STATE_FILE" "$BACKUP/asset-2.before" \
    "$R/bin/freenet" "$FREENET_UPDATE_LOCK_DIR/owner.pid" \
    "$FREENET_UPDATE_MUTATION_LOCK/freenet-update-owner"; do
    mv "$LINK_PATH" "$CASE_DIR/saved-link-target"
    ln -s "$CASE_DIR/saved-link-target" "$LINK_PATH"
    assert_refused 'symlink evidence with otherwise matching bytes'
    rm "$LINK_PATH"
    mv "$CASE_DIR/saved-link-target" "$LINK_PATH"
done

mv "$BACKUP" "$CASE_DIR/saved-backup-dir"
ln -s "$CASE_DIR/saved-backup-dir" "$BACKUP"
assert_refused 'backup directory is a symlink'
rm "$BACKUP"
mv "$CASE_DIR/saved-backup-dir" "$BACKUP"

cp -p "$FREENET_UPDATE_LOCK_DIR/owner.pid" "$CASE_DIR/saved-app-owner"
cp -p "$FREENET_UPDATE_MUTATION_LOCK/pid" "$CASE_DIR/saved-network-owner"
printf '%s\n' "$$" > "$FREENET_UPDATE_LOCK_DIR/owner.pid"
printf '%s\n' "$$" > "$FREENET_UPDATE_MUTATION_LOCK/pid"
assert_refused 'fence owner does not match the original backup updater'
cp -p "$CASE_DIR/saved-app-owner" "$FREENET_UPDATE_LOCK_DIR/owner.pid"
cp -p "$CASE_DIR/saved-network-owner" "$FREENET_UPDATE_MUTATION_LOCK/pid"

mv "$FREENET_UPDATE_MUTATION_LOCK/freenet-update-owner" "$CASE_DIR/saved-owner-marker"
printf '%s\n' "$R/var/run/unrelated.lock" > "$FREENET_UPDATE_MUTATION_LOCK/freenet-update-owner"
chmod 600 "$FREENET_UPDATE_MUTATION_LOCK/freenet-update-owner"
assert_refused 'network fence belongs to a different updater'
rm "$FREENET_UPDATE_MUTATION_LOCK/freenet-update-owner"
mv "$CASE_DIR/saved-owner-marker" "$FREENET_UPDATE_MUTATION_LOCK/freenet-update-owner"

chmod 666 "$FREENET_UPDATE_STATE_FILE"
assert_refused 'receipt is writable by unrelated users'
chmod 600 "$FREENET_UPDATE_STATE_FILE"

# An independently running official write-mode child must block reconciliation.
# It is the native fixture's harmless CLI loop, so the test starts no real apply.
"$R/sbin/freenet-ui" apply > "$CASE_DIR/active-writer.out" 2>&1 &
ACTIVE_WRITER_PID=$!
wait_ready "$ACTIVE_WRITER_PID"
assert_refused 'another actual process is in an official mutation mode'
fixture_alive "$ACTIVE_WRITER_PID" || fail 'writer refusal killed another process'
[ ! -e "$CONTROL/signal.$ACTIVE_WRITER_PID" ] || fail 'writer refusal signalled another process'
kill -TERM "$ACTIVE_WRITER_PID"
wait "$ACTIVE_WRITER_PID"

# A same-executable unknown worker is neither silently ignored nor signalled.
"$R/sbin/freenet-ui" fixture-unsupported-mode > "$CASE_DIR/unknown-worker.out" 2>&1 &
UNKNOWN_WORKER_PID=$!
wait_ready "$UNKNOWN_WORKER_PID"
assert_refused 'unsupported same-executable process role'
fixture_alive "$UNKNOWN_WORKER_PID" || fail 'identity refusal killed an unknown process'
[ ! -e "$CONTROL/signal.$UNKNOWN_WORKER_PID" ] || fail 'identity refusal signalled an unknown process'
kill -TERM "$UNKNOWN_WORKER_PID"
wait "$UNKNOWN_WORKER_PID"

# A healthy version response from a previous daemon is never enough after that
# daemon has exited. The official CLI process remains alive, but owns no port.
kill -TERM "$RESTORED_PID"
wait "$RESTORED_PID"
assert_refused 'CLI-only runtime has no HTTP daemon'
assert_cli_untouched
start_daemon
RESTORED_PID="$DAEMON_PID"
assert_http_version "$FROM"

mv "$FREENET_UPDATE_MUTATION_LOCK" "$CASE_DIR/saved-network-lock"
assert_refused 'the second original owned fence is absent'
mv "$CASE_DIR/saved-network-lock" "$FREENET_UPDATE_MUTATION_LOCK"

mkdir "$FREENET_VPN_TRANSACTION_DIR"
printf '%s\n' PENDING > "$FREENET_VPN_TRANSACTION_DIR/state"
chmod 700 "$FREENET_VPN_TRANSACTION_DIR"
chmod 600 "$FREENET_VPN_TRANSACTION_DIR/state"
# Keep the saved checkpoint evidence coherent with live state: otherwise the
# old hash comparison would reject this fixture before the new no-checkpoint
# rule was exercised. An unchanged PENDING still cannot be reconciled away.
mv "$BACKUP/preserved.before" "$CASE_DIR/saved-preserved-before"
sed 's/^CHECKPOINT_ABSENT$/CHECKPOINT_PRESENT/' "$CASE_DIR/saved-preserved-before" > "$BACKUP/preserved.before"
sha256sum "$FREENET_VPN_TRANSACTION_DIR/state" >> "$BACKUP/preserved.before"
chmod 600 "$BACKUP/preserved.before"
assert_refused 'application reconciliation cannot normalize a pending VPN transaction'
rm "$BACKUP/preserved.before"
mv "$CASE_DIR/saved-preserved-before" "$BACKUP/preserved.before"
rm "$FREENET_VPN_TRANSACTION_DIR/state"
rmdir "$FREENET_VPN_TRANSACTION_DIR"

# Derive a complete private proc view from the live daemon. It must first pass
# the real plan, before it can be used to prove deterministic identity failures.
# Positive update/rollback/reconciliation cases otherwise use actual /proc.
PROC_FIXTURE="$CASE_DIR/proc"
mkdir -p "$PROC_FIXTURE/$RESTORED_PID/fd" "$PROC_FIXTURE/net"
cp "/proc/$RESTORED_PID/stat" "$PROC_FIXTURE/$RESTORED_PID/stat"
cp "/proc/$RESTORED_PID/status" "$PROC_FIXTURE/$RESTORED_PID/status"
cp "/proc/$RESTORED_PID/cmdline" "$PROC_FIXTURE/$RESTORED_PID/cmdline"
cp "/proc/$RESTORED_PID/comm" "$PROC_FIXTURE/$RESTORED_PID/comm"
ln -s "$R/sbin/freenet-ui" "$PROC_FIXTURE/$RESTORED_PID/exe"
mkdir "$PROC_FIXTURE/$RESTORED_XRAY_PID"
for XRAY_PROC_FILE in stat status cmdline comm; do
    cp "/proc/$RESTORED_XRAY_PID/$XRAY_PROC_FILE" "$PROC_FIXTURE/$RESTORED_XRAY_PID/$XRAY_PROC_FILE"
done
printf 'XRAY_LOCATION_CONFDIR=%s\000FREENET_XRAY_PROBE=\000' "$R/etc/xray/configs" > "$PROC_FIXTURE/$RESTORED_XRAY_PID/environ"
ln -s "$R/sbin/xray" "$PROC_FIXTURE/$RESTORED_XRAY_PID/exe"
for PROC_FD in /proc/"$RESTORED_PID"/fd/*; do
    PROC_LINK="$(readlink "$PROC_FD" 2> "$CASE_DIR/proc-read.err")" || continue
    case "$PROC_LINK" in socket:\[*\]) ln -s "$PROC_LINK" "$PROC_FIXTURE/$RESTORED_PID/fd/${PROC_FD##*/}" ;; esac
done
cp /proc/net/tcp "$PROC_FIXTURE/net/tcp"
if [ -r /proc/net/tcp6 ]; then cp /proc/net/tcp6 "$PROC_FIXTURE/net/tcp6"; fi
FREENET_PROC_ROOT="$PROC_FIXTURE"; export FREENET_PROC_ROOT
run_reconcile_plan || fail 'real-derived proc fixture did not pass the complete positive proof'
grep -qx 'READY=yes' "$CASE_DIR/command.out" || fail 'private proc fixture was not actually admitted'
cp "$PROC_FIXTURE/$RESTORED_PID/stat" "$CASE_DIR/original-proc-stat"
cp "$PROC_FIXTURE/$RESTORED_XRAY_PID/stat" "$CASE_DIR/original-xray-stat"
mv "$PROC_FIXTURE/$RESTORED_XRAY_PID" "$CASE_DIR/absent-xray"
assert_refused 'production Xray is absent rather than confirmed running'
mv "$CASE_DIR/absent-xray" "$PROC_FIXTURE/$RESTORED_XRAY_PID"
mv "$PROC_FIXTURE/$RESTORED_PID/stat" "$CASE_DIR/missing-stat"
assert_refused 'daemon process stat is missing'
mv "$CASE_DIR/missing-stat" "$PROC_FIXTURE/$RESTORED_PID/stat"
ORIGINAL_UPDATER_PID="$(cat "$FREENET_UPDATE_LOCK_DIR/owner.pid")"
mkdir "$PROC_FIXTURE/$ORIGINAL_UPDATER_PID"
printf '%s\n' sh > "$PROC_FIXTURE/$ORIGINAL_UPDATER_PID/comm"
printf 'sh\000fixture-idle\000' > "$PROC_FIXTURE/$ORIGINAL_UPDATER_PID/cmdline"
ln -s /bin/sh "$PROC_FIXTURE/$ORIGINAL_UPDATER_PID/exe"
assert_refused 'the original updater PID still exists'
rm "$PROC_FIXTURE/$ORIGINAL_UPDATER_PID/comm" "$PROC_FIXTURE/$ORIGINAL_UPDATER_PID/cmdline" "$PROC_FIXTURE/$ORIGINAL_UPDATER_PID/exe"
rmdir "$PROC_FIXTURE/$ORIGINAL_UPDATER_PID"

cat > "$CASE_DIR/bin/curl" <<'EOF'
#!/bin/sh
"$FREENET_FIXTURE_REAL_CURL" "$@"
rc=$?
if [ "$rc" = 0 ] && [ "$FREENET_FIXTURE_FAULT" = birth-after-http ]; then
    case " $* " in
        *'/versionz '*)
            if [ ! -e "$FREENET_FIXTURE_CONTROL/birth-changed" ]; then
                stat_file="$FREENET_PROC_ROOT/$FREENET_FIXTURE_DAEMON_PID/stat"
                awk '{$22=$22+1; print}' "$stat_file" > "$stat_file.changed"
                "$FREENET_FIXTURE_REAL_MV" "$stat_file.changed" "$stat_file"
                printf '%s\n' changed > "$FREENET_FIXTURE_CONTROL/birth-changed"
            fi
            ;;
    esac
fi
exit "$rc"
EOF
chmod 755 "$CASE_DIR/bin/curl"
export FREENET_FIXTURE_REAL_CURL="$REAL_CURL" FREENET_FIXTURE_REAL_MV="$REAL_MV" \
    FREENET_FIXTURE_DAEMON_PID="$RESTORED_PID" FREENET_FIXTURE_XRAY_PID="$RESTORED_XRAY_PID"
PATH="$CASE_DIR/bin:$BASE_PATH"; FREENET_FIXTURE_FAULT=birth-after-http
export PATH FREENET_FIXTURE_FAULT
assert_refused 'process birth changes after HTTP health/version but before identity recheck'
[ -s "$CONTROL/birth-changed" ] || fail 'PID lifetime negative did not reach its intended race boundary'
fixture_alive "$RESTORED_PID" || fail 'identity race signalled the real daemon'
cp "$CASE_DIR/original-proc-stat" "$PROC_FIXTURE/$RESTORED_PID/stat"
FREENET_PROC_ROOT=/proc; FREENET_FIXTURE_FAULT=''; PATH="$BASE_PATH"
export FREENET_PROC_ROOT FREENET_FIXTURE_FAULT PATH

# Faults occur at real production durability and commit boundaries. The sync
# observer verifies prospective archive files exist before any terminal rename.
cat > "$CASE_DIR/bin/cp" <<'EOF'
#!/bin/sh
destination=''
for argument in "$@"; do destination="$argument"; done
if [ "$FREENET_FIXTURE_FAULT" = archive-copy-failure ]; then
    case "$destination" in
        "$FREENET_ROOT/var/lib/freenet/update-rollback-reconciliation/"*/original.state)
            printf '%s\n' refused > "$FREENET_FIXTURE_CONTROL/archive-copy-refused"
            exit 1
            ;;
    esac
fi
exec "$FREENET_FIXTURE_REAL_CP" "$@"
EOF
cat > "$CASE_DIR/bin/sync" <<'EOF'
#!/bin/sh
archive="$FREENET_ROOT/var/lib/freenet/update-rollback-reconciliation/$FREENET_FIXTURE_RECEIPT.$PPID"
for name in original.state proof SHA256SUMS terminal.state; do
    [ -s "$archive/$name" ] || exit 91
done
[ "$(sha256sum "$archive/original.state" | awk '{print $1}')" = "$FREENET_FIXTURE_RECEIPT" ] || exit 92
printf '%s\n' "$archive" > "$FREENET_FIXTURE_CONTROL/archive-before-sync"
case "$FREENET_FIXTURE_FAULT" in
    archive-sync-failure) exit 1 ;;
esac
"$FREENET_FIXTURE_REAL_SYNC" "$@" || exit $?
printf '%s\n' "$archive" > "$FREENET_FIXTURE_CONTROL/archive-synced"
case "$FREENET_FIXTURE_FAULT" in
    archive-proof-race)
        printf '%s\n' 'INJECTED_CHANGE=yes' >> "$archive/proof"
        ;;
    receipt-race)
        awk '/^PRIMARY_ERROR=/ {$0="PRIMARY_ERROR=fixture concurrent evidence"} {print}' "$FREENET_UPDATE_STATE_FILE" > "$FREENET_FIXTURE_CONTROL/injected-receipt"
        chmod 600 "$FREENET_FIXTURE_CONTROL/injected-receipt"
        "$FREENET_FIXTURE_REAL_CP" -p "$FREENET_FIXTURE_CONTROL/injected-receipt" "$FREENET_UPDATE_STATE_FILE.injected"
        "$FREENET_FIXTURE_REAL_MV" "$FREENET_UPDATE_STATE_FILE.injected" "$FREENET_UPDATE_STATE_FILE"
        ;;
    owner-race)
        printf '%s\n' '999999999' > "$FREENET_UPDATE_LOCK_DIR/owner.pid"
        ;;
    birth-after-proof)
        stat_file="$FREENET_PROC_ROOT/$FREENET_FIXTURE_DAEMON_PID/stat"
        awk '{$22=$22+1; print}' "$stat_file" > "$stat_file.changed"
        "$FREENET_FIXTURE_REAL_MV" "$stat_file.changed" "$stat_file"
        ;;
    xray-birth-after-proof)
        stat_file="$FREENET_PROC_ROOT/$FREENET_FIXTURE_XRAY_PID/stat"
        awk '{$22=$22+1; print}' "$stat_file" > "$stat_file.changed"
        "$FREENET_FIXTURE_REAL_MV" "$stat_file.changed" "$stat_file"
        ;;
esac
exit 0
EOF
cat > "$CASE_DIR/bin/mv" <<'EOF'
#!/bin/sh
source=''; destination=''
for argument in "$@"; do source="$destination"; destination="$argument"; done
if [ "$destination" = "$FREENET_UPDATE_STATE_FILE" ] && [ -f "$source" ] && grep -qx 'STATE=FAILED' "$source"; then
    archive="$(sed -n 's/^RECONCILIATION_ARCHIVE=//p' "$source")"
    case "$archive" in "$FREENET_ROOT/var/lib/freenet/update-rollback-reconciliation/$FREENET_FIXTURE_RECEIPT."*) ;; *) exit 93 ;; esac
    [ "$(cat "$FREENET_FIXTURE_CONTROL/archive-synced")" = "$archive" ] || exit 94
    [ "$(sha256sum "$archive/original.state" | awk '{print $1}')" = "$FREENET_FIXTURE_RECEIPT" ] || exit 95
    cmp -s "$source" "$archive/terminal.state" || exit 96
    cmp -s "$archive/SHA256SUMS" "$FREENET_TEST_RELEASE_DIR/SHA256SUMS" || exit 97
    printf '%s\n' "$archive" > "$FREENET_FIXTURE_CONTROL/terminal-boundary"
    [ "$FREENET_FIXTURE_FAULT" != terminal-rename-failure ] || exit 1
fi
exec "$FREENET_FIXTURE_REAL_MV" "$@"
EOF
chmod 755 "$CASE_DIR/bin/cp" "$CASE_DIR/bin/sync" "$CASE_DIR/bin/mv"
export FREENET_FIXTURE_REAL_CP="$REAL_CP" FREENET_FIXTURE_REAL_SYNC="$REAL_SYNC"
PATH="$CASE_DIR/bin:$BASE_PATH"; export PATH

FREENET_FIXTURE_FAULT=archive-copy-failure; export FREENET_FIXTURE_FAULT
assert_refused 'archive could not preserve the original receipt'
[ -s "$CONTROL/archive-copy-refused" ] || fail 'archive-copy negative stopped before the intended write boundary'
assert_unknown_unchanged

for DURABILITY_FAULT in archive-sync-failure terminal-rename-failure archive-proof-race; do
    rm -f "$CONTROL/archive-before-sync" "$CONTROL/archive-synced" "$CONTROL/terminal-boundary"
    FREENET_FIXTURE_FAULT="$DURABILITY_FAULT"; export FREENET_FIXTURE_FAULT
    assert_refused "$DURABILITY_FAULT"
    [ -s "$CONTROL/archive-before-sync" ] || fail 'durability negative stopped before the intended archive boundary'
    if [ "$DURABILITY_FAULT" = terminal-rename-failure ]; then
        [ -s "$CONTROL/terminal-boundary" ] || fail 'terminal rename failure was not reached'
    fi
    assert_unknown_unchanged
done

# A concurrent new receipt remains authoritative. Reconciliation must not
# overwrite it with the previously planned terminal result or restore anything.
FREENET_FIXTURE_FAULT=receipt-race; export FREENET_FIXTURE_FAULT
rm -f "$CONTROL/injected-receipt"
if run_reconcile "$UNKNOWN_SHA"; then fail 'receipt changed after proof but was still committed'; else RACE_RC=$?; fi
[ "$RACE_RC" -ne 124 ] || fail 'receipt-race refusal exceeded its bound'
[ -s "$CONTROL/injected-receipt" ] || fail 'receipt race never reached the durability boundary'
cmp -s "$CONTROL/injected-receipt" "$FREENET_UPDATE_STATE_FILE" || fail 'concurrent UNKNOWN receipt was overwritten'
[ "$(state_value STATE)" = ROLLBACK_FAILED ] && [ "$(state_value ROLLBACK_STATE)" = FAILED_UNKNOWN ] || fail 'receipt race normalized UNKNOWN'
cp -p "$CASE_DIR/original.state" "$FREENET_UPDATE_STATE_FILE"

FREENET_FIXTURE_FAULT=owner-race; export FREENET_FIXTURE_FAULT
if run_reconcile "$UNKNOWN_SHA"; then fail 'changed lock owner was committed'; else RACE_RC=$?; fi
[ "$RACE_RC" -ne 124 ] || fail 'owner-race refusal exceeded its bound'
[ "$(cat "$FREENET_UPDATE_LOCK_DIR/owner.pid")" = 999999999 ] || fail 'concurrent fence owner was overwritten'
cmp -s "$CASE_DIR/original.state" "$FREENET_UPDATE_STATE_FILE" || fail 'owner race normalized UNKNOWN'
cp -p "$CASE_DIR/saved-app-owner" "$FREENET_UPDATE_LOCK_DIR/owner.pid"

# Here both complete runtime samples can be internally valid, but they identify
# different lifetimes. Comparing the two full proofs must reject the handoff.
FREENET_PROC_ROOT="$PROC_FIXTURE"; FREENET_FIXTURE_FAULT=birth-after-proof
export FREENET_PROC_ROOT FREENET_FIXTURE_FAULT
assert_refused 'daemon birth differs between two otherwise complete proofs'
[ "$(awk '{print $22}' "$PROC_FIXTURE/$RESTORED_PID/stat")" != "$(awk '{print $22}' "$CASE_DIR/original-proc-stat")" ] || fail 'second-proof lifetime race did not execute'
cp "$CASE_DIR/original-proc-stat" "$PROC_FIXTURE/$RESTORED_PID/stat"
FREENET_FIXTURE_FAULT=xray-birth-after-proof; export FREENET_FIXTURE_FAULT
assert_refused 'production Xray lifetime differs between complete proofs'
[ "$(awk '{print $22}' "$PROC_FIXTURE/$RESTORED_XRAY_PID/stat")" != "$(awk '{print $22}' "$CASE_DIR/original-xray-stat")" ] || fail 'Xray lifetime race did not reach its boundary'
cp "$CASE_DIR/original-xray-stat" "$PROC_FIXTURE/$RESTORED_XRAY_PID/stat"
FREENET_PROC_ROOT=/proc; FREENET_FIXTURE_FAULT=''
export FREENET_PROC_ROOT FREENET_FIXTURE_FAULT

# Successful confirmation is the only receipt mutation. It preserves live
# process lifetime, all application/VPN files, original backup and both fences.
assert_unknown_unchanged
snapshot_paths "$CASE_DIR/success.before" "$R/sbin" "$R/bin" "$R/lib" "$R/etc" "$R/backups" \
    "$FREENET_UPDATE_LOCK_DIR" "$FREENET_UPDATE_MUTATION_LOCK" "$FREENET_VPN_TRANSACTION_DIR"
DAEMON_BIRTH_BEFORE="$(awk '{print $22}' "/proc/$RESTORED_PID/stat")"
XRAY_BIRTH_BEFORE="$(awk '{print $22}' "/proc/$RESTORED_XRAY_PID/stat")"
rm -f "$CONTROL/archive-before-sync" "$CONTROL/archive-synced" "$CONTROL/terminal-boundary"
run_reconcile "$UNKNOWN_SHA" || fail 'fully proven rollback was not reconciled'
grep -qx 'RESULT=RECONCILED_ROLLBACK' "$CASE_DIR/command.out" || fail 'reconciliation result marker missing'
grep -qx 'FENCES=PRESERVED' "$CASE_DIR/command.out" || fail 'reconciliation did not retain fences'
grep -qx 'MUTATION=APPLICATION_RECEIPT_ONLY' "$CASE_DIR/command.out" || fail 'reconciliation mutation boundary changed'
[ "$(state_value STATE)" = FAILED ] && [ "$(state_value ROLLBACK_STATE)" = SUCCESS ] || fail 'terminal receipt is not known FAILED/SUCCESS'
[ "$(state_value FROM_VERSION)" = "$FROM" ] && [ "$(state_value TARGET_VERSION)" = "$TO" ] || fail 'original version provenance changed'
[ "$(state_value PRIMARY_ERROR)" = "$(sed -n 's/^PRIMARY_ERROR=//p' "$CASE_DIR/original.state")" ] || fail 'original primary failure was lost'
snapshot_paths "$CASE_DIR/success.after" "$R/sbin" "$R/bin" "$R/lib" "$R/etc" "$R/backups" \
    "$FREENET_UPDATE_LOCK_DIR" "$FREENET_UPDATE_MUTATION_LOCK" "$FREENET_VPN_TRANSACTION_DIR"
cmp -s "$CASE_DIR/success.before" "$CASE_DIR/success.after" || fail 'reconciliation changed assets, backup, protected files or fences'
fixture_alive "$RESTORED_PID" || fail 'reconciliation stopped the accepted daemon'
[ "$(awk '{print $22}' "/proc/$RESTORED_PID/stat")" = "$DAEMON_BIRTH_BEFORE" ] || fail 'reconciliation restarted the daemon'
assert_cli_untouched
[ ! -e "$CONTROL/signal.$RESTORED_PID" ] || fail 'reconciliation signalled the daemon'
fixture_alive "$RESTORED_XRAY_PID" || fail 'reconciliation stopped production Xray'
[ "$(awk '{print $22}' "/proc/$RESTORED_XRAY_PID/stat")" = "$XRAY_BIRTH_BEFORE" ] || fail 'reconciliation restarted production Xray'
[ ! -e "$CONTROL/signal.$RESTORED_XRAY_PID" ] || fail 'reconciliation signalled production Xray'
fixture_alive "$PROBE_PID" || fail 'reconciliation stopped a recognized read-only probe'
[ ! -e "$CONTROL/signal.$PROBE_PID" ] || fail 'reconciliation signalled a recognized read-only probe'
ARCHIVE="$(state_value RECONCILIATION_ARCHIVE)"
[ "$(cat "$CONTROL/terminal-boundary")" = "$ARCHIVE" ] || fail 'terminal commit was not preceded by durable exact evidence'
cmp -s "$CASE_DIR/original.state" "$ARCHIVE/original.state" || fail 'archive lost the exact original UNKNOWN receipt'
cmp -s "$FREENET_UPDATE_STATE_FILE" "$ARCHIVE/terminal.state" || fail 'archived terminal receipt differs from committed bytes'
cmp -s "$FROM_RELEASE/SHA256SUMS" "$ARCHIVE/SHA256SUMS" || fail 'exact FROM release binding missing from archive'
[ -s "$ARCHIVE/proof" ] || fail 'durable runtime/file/ownership proof missing'
[ "$(stat -c '%a' "$ARCHIVE")" = 700 ] || fail 'archive directory is not private'
for ARCHIVE_FILE in original.state proof SHA256SUMS terminal.state; do
    [ "$(stat -c '%a' "$ARCHIVE/$ARCHIVE_FILE")" = 600 ] || fail 'archived evidence is not private'
done
[ ! -e "$FREENET_UPDATE_LOCK_DIR.reconcile-guard" ] || fail 'successful command retained its temporary guard'
assert_http_version "$FROM"

# A known terminal receipt cannot be reinterpreted as another UNKNOWN recovery,
# and normal apply remains fenced until the separate official unlock flow.
cp "$FREENET_UPDATE_STATE_FILE" "$CASE_DIR/terminal.state"
if run_reconcile "$UNKNOWN_SHA"; then fail 'terminal receipt was reconciled a second time'; fi
cmp -s "$CASE_DIR/terminal.state" "$FREENET_UPDATE_STATE_FILE" || fail 'repeat command rewrote known terminal receipt'
FREENET_TEST_RELEASE_DIR="$TO_RELEASE"; export FREENET_TEST_RELEASE_DIR
if run_apply; then fail 'reconciliation silently unlocked a new update'; fi
cmp -s "$CASE_DIR/terminal.state" "$FREENET_UPDATE_STATE_FILE" || fail 'fenced apply changed terminal receipt'
[ -d "$FREENET_UPDATE_LOCK_DIR" ] && [ -d "$FREENET_UPDATE_MUTATION_LOCK" ] || fail 'reconciliation released original fences'

printf '%s\n' 'self update daemon and rollback reconciliation contract PASS'
