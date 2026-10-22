#!/bin/sh
set -eu

# Stop-only native regression. Never run this suite on a router. It compiles
# its own ELF and uses private roots and loopback ports, without HTTP/downloads.
ROOT_DIR="$(CDPATH= cd "$(dirname "$0")/.." && pwd)"
SCRIPT="${1:-$ROOT_DIR/scripts/self_update.sh}"
SUITE="$(mktemp -d /tmp/freenet-stop-test.XXXXXX)"
FIXTURE_PIDS=""

test_cleanup() {
    for test_pid in $FIXTURE_PIDS; do
        # These are only processes launched by this private test suite.
        if [ -d "/proc/$test_pid" ]; then
            test_birth="$(awk '{print $22}' "/proc/$test_pid/stat" 2>/dev/null || true)"
            test_saved="$(cat "$SUITE/birth.$test_pid" 2>/dev/null || true)"
            if [ -n "$test_saved" ] && [ "$test_birth" = "$test_saved" ]; then
                kill -KILL "$test_pid" 2>/dev/null || true
            fi
        fi
        wait "$test_pid" 2>/dev/null || true
    done
    rm -rf "$SUITE"
}
trap test_cleanup EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM
fail() { printf 'self update stop FAIL: %s\n' "$*" >&2; exit 1; }

cc -std=c11 -O2 -Wall -Wextra -Werror -pthread \
    "$ROOT_DIR/tests/fixtures/self_update_stop_daemon.c" -o "$SUITE/fixture"

# Load production helpers, not the plan/apply dispatcher. No production path is
# writable: source initialization is pinned to a private root and private locks.
awk '/^case "\$MODE" in$/ {exit} {print}' "$SCRIPT" > "$SUITE/helpers.sh"
FREENET_ROOT="$SUITE/initial-root"
FREENET_UPDATE_LOCK_DIR="$SUITE/initial-lock"
FREENET_UPDATE_MUTATION_LOCK="$SUITE/initial-network-lock"
FREENET_PROC_ROOT=/proc
set --
. "$SUITE/helpers.sh"
trap test_cleanup EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM

await_test_term() {
    term_tries=0
    while [ ! -s "$CONTROL/terms" ] && [ "$term_tries" -lt 500 ]; do term_tries=$((term_tries + 1)); sleep 0.02; done
    [ -s "$CONTROL/terms" ] || exit 2
}

new_case() {
    CASE_DIR="$SUITE/$1"
    ROOT="$CASE_DIR/root"
    TMP_DIR="$CASE_DIR/staging"
    BACKUP_DIR="$CASE_DIR/backup"
    CONTROL="$CASE_DIR/control"
    mkdir -p "$ROOT/sbin" "$ROOT/etc/init.d" "$ROOT/etc/freenet" "$TMP_DIR" "$BACKUP_DIR" "$CONTROL"
    cp "$SUITE/fixture" "$ROOT/sbin/freenet-ui"
    chmod 755 "$ROOT/sbin/freenet-ui"
    PORT="$("$SUITE/fixture" free-port)"
    printf 'UI_PORT=%s\n' "$PORT" > "$ROOT/etc/freenet/freenet.conf"
    cat > "$ROOT/etc/init.d/S99freenet-ui" <<EOF
#!/bin/sh
ENABLED=yes
PROCS=freenet-ui
ARGS="-listen 127.0.0.2:$PORT"
PREARGS=""
DESC="\$PROCS"
PATH=/opt/sbin:/opt/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
. /opt/etc/init.d/rc.func
EOF
    chmod 755 "$ROOT/etc/init.d/S99freenet-ui"
    ARCH=arm64-v8a
    UPDATE_PROC_ROOT=/proc
    UI_STOP_PENDING=0
    UI_STOP_PID=''; UI_STOP_BIRTH=''; UI_STOP_SOCKETS=''
    UI_STOP_STARTED=''; UI_STOP_DEADLINE=''
    UI_OLD_PIN=''; UI_STARTED_ID=''; UI_STARTED_PIN=''; UI_STARTED_SHA=''
    LAST_UPDATE_STAGE=''
    ui_prepare_service || fail "$1: private service specification"
}

start_fixture() {
    FREENET_STOP_TEST_DIR="$CONTROL" FREENET_STOP_TEST_MODE="$1" FREENET_STOP_TEST_DRAIN_SECONDS="$2" \
        "$UI_BIN" -listen "$UI_LISTEN" > "$CASE_DIR/fixture.out" 2>&1 &
    FIXTURE_PID=$!
    FIXTURE_PIDS="$FIXTURE_PIDS $FIXTURE_PID"
    awk '{print $22}' "/proc/$FIXTURE_PID/stat" > "$SUITE/birth.$FIXTURE_PID"
    tries=0
    while [ ! -s "$CONTROL/ready" ] && [ "$tries" -lt 100 ]; do tries=$((tries + 1)); sleep 0.05; done
    [ -s "$CONTROL/ready" ] || fail 'fixture did not bind its private sockets'
    UI_OLD_SHA="$(update_sha "$UI_BIN")"
    TEST_ID="$(ui_process_fingerprint "$FIXTURE_PID" "$UI_OLD_SHA")" || fail 'real process fingerprint'
    UI_OLD_PIN="$(ui_stable_pin "$TEST_ID")" || fail 'real process pin'
}

assert_one_term() {
    [ "$(wc -l < "$CONTROL/terms" | tr -d ' ')" = 1 ] || fail 'stop did not send exactly one TERM'
}

stop_ok() {
    if ! ui_stop_daemon "$UI_OLD_SHA" > "$CASE_DIR/stop.out" 2>&1; then
        cat "$CASE_DIR/stop.out" >&2
        fail "$1"
    fi
}

stop_refused() {
    if ui_stop_daemon "$UI_OLD_SHA" > "$CASE_DIR/stop.out" 2>&1; then fail "$1"; fi
}

# Both listeners outlive the leader, and drain one at a time past the old 10 s
# budget. Return with a real Z still present: socket teardown is not zombie reap.
new_case zombie-delayed-drain
start_fixture zombie 12
BEFORE_SOCKETS="$(ui_owned_listeners "$FIXTURE_PID")"
START="$(awk '{print int($1)}' /proc/uptime)"
stop_ok 'leader Z incorrectly ended native listener drain'
END="$(awk '{print int($1)}' /proc/uptime)"
[ $((END - START)) -ge 12 ] || fail 'stop returned before delayed socket closure'
[ -s "$CONTROL/closed" ] && [ -s "$CONTROL/first-closed" ] || fail 'both drain phases were not reached'
[ "$(awk '{print $3}' "/proc/$FIXTURE_PID/stat")" = Z ] || fail 'test did not retain its zombie leader'
[ -z "$(ui_listener_table)" ] || fail 'stop accepted an occupied port'
assert_one_term
printf '%s\n' 'stop regression: native Z + delayed two-socket drain PASS'

# A process that already owns a child pin may not yet have either/both sockets.
for CHILD_MODE in empty one; do
    new_case "child-$CHILD_MODE"
    start_fixture "$CHILD_MODE" 1
    UI_STARTED_ID="$FIXTURE_PID:$(awk '{print $22}' "/proc/$FIXTURE_PID/stat")"
    UI_STARTED_PIN="$UI_OLD_PIN"
    UI_OLD_PIN=''
    stop_ok 'partially started owned child could not be stopped'
    [ -z "$(ui_listener_table)" ] || fail 'child stop accepted an occupied port'
    assert_one_term
done
printf '%s\n' 'stop regression: owned child before/after one bind PASS'

# Even an owned child with an empty snapshot cannot claim another process's
# listener. The other ELF is outside the canonical service image path.
new_case child-with-foreign-listener
start_fixture empty 1
UI_STARTED_ID="$FIXTURE_PID:$(awk '{print $22}' "/proc/$FIXTURE_PID/stat")"
UI_STARTED_PIN="$UI_OLD_PIN"
UI_OLD_PIN=''
mkdir "$CASE_DIR/foreign-control"
FREENET_STOP_TEST_DIR="$CASE_DIR/foreign-control" FREENET_STOP_TEST_MODE=one FREENET_STOP_TEST_DRAIN_SECONDS=1 \
    "$SUITE/fixture" -listen "$UI_LISTEN" > "$CASE_DIR/foreign.out" 2>&1 &
FOREIGN_PID=$!
FIXTURE_PIDS="$FIXTURE_PIDS $FOREIGN_PID"
awk '{print $22}' "/proc/$FOREIGN_PID/stat" > "$SUITE/birth.$FOREIGN_PID"
tries=0
while [ ! -s "$CASE_DIR/foreign-control/ready" ] && [ "$tries" -lt 100 ]; do tries=$((tries + 1)); sleep 0.05; done
[ -s "$CASE_DIR/foreign-control/ready" ] || fail 'foreign listener fixture did not bind'
stop_refused 'owned child adopted another process listener'
[ "$LAST_UPDATE_STAGE" = daemon-listener ] || fail 'child foreign listener did not stop at ownership'
[ ! -s "$CONTROL/terms" ] && [ ! -s "$CASE_DIR/foreign-control/terms" ] || fail 'preflight refusal signalled a process'

# Empty sockets alone do not authorize start while the old lifetime is live.
new_case live-with-empty-port
start_fixture alive-empty 0
START="$(awk '{print int($1)}' /proc/uptime)"
stop_ok 'live daemon with an empty port was not waited'
END="$(awk '{print int($1)}' /proc/uptime)"
[ $((END - START)) -ge 3 ] || fail 'stop accepted a still-live old process'
assert_one_term

# A new inode at an expected address is not adopted into the retained proof.
new_case foreign-inode
start_fixture replace 0
stop_refused 'a replacement listener was accepted as the old one'
[ "$LAST_UPDATE_STAGE" = daemon-listener ] || fail 'replacement listener did not fail at ownership'
[ -s "$CONTROL/replaced" ] || fail 'replacement listener fault was not exercised'
assert_one_term
SAVED_DEADLINE="$UI_STOP_DEADLINE"
stop_refused 'rollback adopted a replacement listener'
[ "$UI_STOP_DEADLINE" = "$SAVED_DEADLINE" ] || fail 'rollback renewed stop deadline'
assert_one_term
printf '%s\n' 'stop regression: new inode and retained rollback refusal PASS'

# A port occupied before any stop witness remains an immediate refusal.
new_case no-owned-daemon
start_fixture zombie 1
UI_IMAGE_PATH="$CASE_DIR/not-the-daemon"
stop_refused 'an unowned pre-existing listener was adopted'
[ "$LAST_UPDATE_STAGE" = daemon-listener ] || fail 'unowned port did not fail at ownership'
[ ! -s "$CONTROL/terms" ] || fail 'unowned listener owner was signalled'

# Derive private proc evidence from a real owned process. Only subsequent
# identity evidence is faulted; the signal still targets our real fixture PID.
private_proc() {
    PROC_VIEW="$CASE_DIR/proc"
    mkdir -p "$PROC_VIEW/$FIXTURE_PID/fd" "$PROC_VIEW/net"
    for name in stat status cmdline; do cp "/proc/$FIXTURE_PID/$name" "$PROC_VIEW/$FIXTURE_PID/$name"; done
    ln -s "$UI_BIN" "$PROC_VIEW/$FIXTURE_PID/exe"
    for fd in /proc/"$FIXTURE_PID"/fd/*; do
        link="$(readlink "$fd" 2>/dev/null)" || continue
        case "$link" in 'socket:['*']') ln -s "$link" "$PROC_VIEW/$FIXTURE_PID/fd/${fd##*/}" ;; esac
    done
    cp /proc/net/tcp "$PROC_VIEW/net/tcp"
    [ ! -r /proc/net/tcp6 ] || cp /proc/net/tcp6 "$PROC_VIEW/net/tcp6"
    cp /proc/uptime "$PROC_VIEW/uptime"
    UPDATE_PROC_ROOT="$PROC_VIEW"
}

for PROC_FAULT in reused-birth malformed-stat missing-table missing-clock backwards-clock; do
    new_case "$PROC_FAULT"
    start_fixture zombie 120
    private_proc
    (
        await_test_term
        case "$PROC_FAULT" in
            reused-birth) awk '{$22=$22+1; print}' "$PROC_VIEW/$FIXTURE_PID/stat" > "$PROC_VIEW/stat.next"; mv "$PROC_VIEW/stat.next" "$PROC_VIEW/$FIXTURE_PID/stat" ;;
            malformed-stat) printf 'unknown\n' > "$PROC_VIEW/$FIXTURE_PID/stat" ;;
            missing-table) rm "$PROC_VIEW/net/tcp" ;;
            missing-clock) rm "$PROC_VIEW/uptime" ;;
            backwards-clock) awk '{printf "%.2f %s\n",$1-10,$2}' "$PROC_VIEW/uptime" > "$PROC_VIEW/uptime.next"; mv "$PROC_VIEW/uptime.next" "$PROC_VIEW/uptime" ;;
        esac
    ) &
    FAULT_PID=$!
    FIXTURE_PIDS="$FIXTURE_PIDS $FAULT_PID"
    awk '{print $22}' "/proc/$FAULT_PID/stat" > "$SUITE/birth.$FAULT_PID"
    stop_refused "$PROC_FAULT was accepted"
    wait "$FAULT_PID"
    case "$PROC_FAULT" in
        missing-table) [ "$LAST_UPDATE_STAGE" = daemon-listener ] || fail 'missing table did not stop at inspection' ;;
        missing-clock|backwards-clock) [ "$LAST_UPDATE_STAGE" = daemon-stop ] || fail 'bad clock did not stop bounded wait' ;;
        *) [ "$LAST_UPDATE_STAGE" = daemon-identity ] || fail 'bad lifetime did not stop at identity' ;;
    esac
    assert_one_term
done
printf '%s\n' 'stop regression: PID reuse and unreadable evidence PASS'

new_case bad-clock-before-term
start_fixture zombie 1
private_proc
printf 'unknown\n' > "$PROC_VIEW/uptime"
stop_refused 'invalid monotonic clock allowed TERM'
[ ! -s "$CONTROL/terms" ] || fail 'clock validation happened after TERM'
[ "$UI_STOP_PENDING" = 0 ] || fail 'pre-TERM clock refusal retained a stop witness'

# An empty retained socket snapshot must never turn a later listener into
# owned evidence. Exercise the production subset checker independently of bind.
UI_STOP_SOCKETS=''
if ui_stop_listener_subset '0200007F:1234 123456'; then fail 'empty snapshot adopted a listener'; fi

# Advance the private monotonic evidence across the real fixed 90 s deadline.
# Production has no timeout override; the >10 s positive test uses real time.
# A retry cannot renew this deadline or signal the same lifetime again.
new_case drain-timeout
start_fixture zombie 120
private_proc
START="$(ui_stop_clock)"
REAL_START="$(awk '{print int($1)}' /proc/uptime)"
(
    await_test_term
    awk '{$3="Z"; print}' "$PROC_VIEW/$FIXTURE_PID/stat" > "$PROC_VIEW/stat.next"
    mv "$PROC_VIEW/stat.next" "$PROC_VIEW/$FIXTURE_PID/stat"
    rm "$PROC_VIEW/$FIXTURE_PID/exe"
    for offset in 88 89 90 91 92; do
        printf '%s.00 0.00\n' "$((START + offset))" > "$PROC_VIEW/uptime.next"
        mv "$PROC_VIEW/uptime.next" "$PROC_VIEW/uptime"
        sleep 0.5
    done
) &
FAULT_PID=$!
FIXTURE_PIDS="$FIXTURE_PIDS $FAULT_PID"
awk '{print $22}' "/proc/$FAULT_PID/stat" > "$SUITE/birth.$FAULT_PID"
stop_refused 'persistent old listeners exceeded the bounded drain'
END="$(ui_stop_clock)"
[ $((UI_STOP_DEADLINE - START)) -eq 90 ] || fail 'fixed stop budget is not 90 seconds'
[ $((END - START)) -ge 90 ] && [ $((END - START)) -le 92 ] || fail 'stop did not obey the monotonic deadline'
[ $(( $(awk '{print int($1)}' /proc/uptime) - REAL_START )) -le 6 ] || fail 'stop used sleep-count instead of monotonic time'
wait "$FAULT_PID"
[ "$LAST_UPDATE_STAGE" = daemon-stop ] || fail 'persistent listeners lacked timeout classification'
assert_one_term
SAVED_DEADLINE="$UI_STOP_DEADLINE"
stop_refused 'rollback accepted persistent listeners after timeout'
[ "$UI_STOP_DEADLINE" = "$SAVED_DEADLINE" ] || fail 'rollback extended an exhausted stop budget'
[ $(( $(ui_stop_clock) - END )) -le 3 ] || fail 'rollback waited a new timeout'
assert_one_term
printf '%s\n' 'stop regression: fixed 90 s timeout and single TERM across rollback PASS'

# Rollback may complete immediately once the retained proc evidence shows an
# empty port, even past the old deadline. It may not wait a renewed budget.
# Native socket closure itself is covered by the first, real-time case above.
for table in tcp tcp6; do
    [ ! -f "$PROC_VIEW/net/$table" ] || awk 'NR==1' "$PROC_VIEW/net/$table" > "$PROC_VIEW/$table.empty"
    [ ! -f "$PROC_VIEW/$table.empty" ] || mv "$PROC_VIEW/$table.empty" "$PROC_VIEW/net/$table"
done
stop_ok 'rollback refused an already completed drain after timeout'
[ "$UI_STOP_PENDING" = 0 ] || fail 'completed rollback drain retained pending witness'
[ "$UI_STOP_DEADLINE" = "$SAVED_DEADLINE" ] || fail 'completed rollback drain changed its deadline'
assert_one_term

printf '%s\n' 'self update stop lifecycle PASS'
