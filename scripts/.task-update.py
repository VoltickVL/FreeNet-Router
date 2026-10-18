from pathlib import Path

def change(path, old, new, count=1):
    p=Path(path); s=p.read_text()
    assert s.count(old)==count, (path, s.count(old), old[:100])
    p.write_text(s.replace(old,new))

change('freenet-ui/main.go', '''func (a *app) handleSelfUpdateApply(w http.ResponseWriter, r *http.Request) {
	if automationMutationBlockedState() {
		writeJSON(w, http.StatusConflict, actionResult{Success: false, Error: "VPN transaction pending/unknown: self-update blocked"})
		return
	}
''', '''func (a *app) handleSelfUpdateApply(w http.ResponseWriter, r *http.Request) {
	// Updating application files is independent from VPN availability. The
	// helper serializes against live network writers and preserves VPN evidence.
	if err := a.selfUpdateStateAdmission(); err != nil {
		writeJSON(w, http.StatusConflict, actionResult{Success: false, Error: err.Error()})
		return
	}
''')
for name in ['freenet-ui/main.go','freenet-ui/self_update_recovery.go']:
    change(name, '"FREENET_UPDATE_LOCK_DIR="+a.cfg.UpdateLock,', '"FREENET_UPDATE_LOCK_DIR="+a.cfg.UpdateLock,\n\t\t"FREENET_UPDATE_MUTATION_LOCK="+a.cfg.LockPath,')
change('freenet-ui/self_update_recovery.go','\tstateKV := readStateFile(a.cfg.UpdateState)\n','\tif err := a.selfUpdateStateAdmission(); err != nil {\n\t\twriteJSON(w, http.StatusConflict, actionResult{Success: false, Error: err.Error()})\n\t\treturn\n\t}\n\tstateKV := readStateFile(a.cfg.UpdateState)\n')
change('freenet-ui/self_update_lock.go','\t\tif string(parts[i]) != helper {\n\t\t\tcontinue\n\t\t}\n','\t\tcandidate := string(parts[i])\n\t\tif candidate != helper && !temporaryRecoveryUpdaterPath(candidate) {\n\t\t\tcontinue\n\t\t}\n')
change('freenet-ui/self_update_lock.go','''			if cmdline, err := os.ReadFile(filepath.Join(root, strconv.Itoa(pid), "cmdline")); err == nil && updaterCmdlineMatches(cmdline, a.cfg.SelfUpdatePath) {
				return true, true
			}
''','''			cmdline, readErr := os.ReadFile(filepath.Join(root, strconv.Itoa(pid), "cmdline"))
			if readErr == nil && updaterCmdlineMatches(cmdline, a.cfg.SelfUpdatePath) {
				return true, true
			}
			if readErr != nil && !os.IsNotExist(readErr) {
				return false, false
			}
''')
change('freenet-ui/self_update_lock.go','case "", "IDLE", "CHECKING", "BUSY", "FAILED", "SUCCESS":','case "", "IDLE", "CHECKING", "SNAPSHOT", "BUSY", "FAILED", "SUCCESS":')
change('freenet-ui/self_update_lock.go','\tif err := os.RemoveAll(a.cfg.UpdateLock); err != nil {','\tif err := a.releaseStaleUpdateFence(); err != nil {\n\t\treturn err\n\t}\n\tif err := os.RemoveAll(a.cfg.UpdateLock); err != nil {')
p='scripts/self_update.sh'
change(p,'LOCK_DIR="${FREENET_UPDATE_LOCK_DIR:-/tmp/freenet-self-update.lock}"','''LOCK_DIR="${FREENET_UPDATE_LOCK_DIR:-/tmp/freenet-self-update.lock}"
MUTATION_LOCK_DIR="${FREENET_UPDATE_MUTATION_LOCK:-/tmp/blanc_xkeen_update.lock}"
CHECKPOINT_DIR="${FREENET_VPN_TRANSACTION_DIR:-$ROOT/var/lib/freenet/vpn-transaction.pending}"
AUTOMATION_STATE="${FREENET_AUTOMATION_STATE:-$ROOT/var/run/freenet-automation.state}"''')
change(p,'TEST_MODE="${FREENET_SELF_UPDATE_TEST_MODE:-no}"','''TEST_MODE="${FREENET_SELF_UPDATE_TEST_MODE:-no}"
if [ "$TEST_MODE" = yes ] && [ -z "${FREENET_UPDATE_MUTATION_LOCK:-}" ]; then
    MUTATION_LOCK_DIR="$ROOT/var/run/vpn-mutation.lock"
fi''')
change(p,'LOCK_HELD=0\nKEEP_LOCK=0','LOCK_HELD=0\nMUTATION_LOCK_HELD=0\nKEEP_LOCK=0')
change(p,'    if [ "$LOCK_HELD" = 1 ] && [ "$KEEP_LOCK" = 0 ]; then\n','''    if [ "$MUTATION_LOCK_HELD" = 1 ] && [ "$KEEP_LOCK" = 0 ]; then
        rm -rf "$MUTATION_LOCK_DIR" 2>/dev/null || true
        MUTATION_LOCK_HELD=0
    fi
    if [ "$LOCK_HELD" = 1 ] && [ "$KEEP_LOCK" = 0 ]; then
''')
change(p,'trap cleanup 0 1 2 15', '''trap cleanup 0
on_update_signal() {
    if [ "$MUTATED" = 1 ]; then
        trap '' 1 2 15
        fail_after_mutation 'update interrupted; restoring application snapshot'
    fi
    [ "$LOCK_HELD" = 1 ] && fail_before_mutation 'update interrupted before mutation'
    exit 1
}
trap on_update_signal 1 2 15''')
change(p,'asset_list() {\n',r'''recovery_bridge_target() {
    beta_tag "$CURRENT_VERSION" && [ "${1:-$TARGET_TAG}" = v0.7.18 ]
}

compatible_update_target() {
    COMPAT_TARGET="$1"
    if [ ! -e "$CHECKPOINT_DIR" ] && [ ! -L "$CHECKPOINT_DIR" ] &&
       ! grep -qs '^MUTATION_BLOCKED=yes$' "$AUTOMATION_STATE" 2>/dev/null; then
        return 0
    fi
    # The reviewed bridge runs only Recovery and preserves Beta VPN helpers.
    recovery_bridge_target "$COMPAT_TARGET" && return 0
    # Exact .6 introduced the protocol; later releases must declare it in
    # SHA-verified metadata (this is not inferred merely from a larger number).
    fetch_manifest "$COMPAT_TARGET" || return 1
    PROTOCOL_FILE="$TMP_DIR/recovery-protocol.json"
    download_verified_asset recovery-protocol.json "https://github.com/$REPO/releases/download/$COMPAT_TARGET/recovery-protocol.json" "$PROTOCOL_FILE" || return 1
    jq -e --arg tag "$COMPAT_TARGET" '.protocol == 2 and .tag == $tag' "$PROTOCOL_FILE" >/dev/null 2>&1
}

asset_list() {
    if recovery_bridge_target; then
        printf '%s\n' "freenet-ui-$ARCH" self_update.sh
        return
    fi
''')
change(p,'        download_url "https://api.github.com/repos/$REPO/releases/tags/$TAG" "$META" || return 1','''        if ! download_url "https://api.github.com/repos/$REPO/releases/tags/$TAG" "$META"; then
            fetch_manifest "$TAG" || return 1
            download_verified_asset release-metadata.json "https://github.com/$REPO/releases/download/$TAG/release-metadata.json" "$META" || return 1
        fi''')
change(p,'    for NAME in freenet vpn blanc_xkeen_update_outbounds.sh migrate_split_dns.sh apply_network_profile.sh apply_provider_profile.sh finalize_setup.sh bootstrap_entware.sh self_update.sh; do\n','''    if recovery_bridge_target; then
        sh -n "$TMP_DIR/self_update.sh" >/dev/null 2>&1
        return $?
    fi
    for NAME in freenet vpn blanc_xkeen_update_outbounds.sh migrate_split_dns.sh apply_network_profile.sh apply_provider_profile.sh finalize_setup.sh bootstrap_entware.sh self_update.sh; do
''')
change(p,'prepare_backup() {\n',r'''snapshot_preserved_state() {
    PRESERVED_OUT="$1"
    : > "$PRESERVED_OUT" || return 1
    for PRESERVED_PATH in "$ROOT/etc/freenet/freenet.conf" \
        "$ROOT/etc/freenet/vpn_profile_name" "$ROOT/etc/xray/blanc_profile_filter.regex" \
        "$ROOT/etc/xray/blanc_subscription.url"; do
        [ ! -L "$PRESERVED_PATH" ] || return 1
        if [ -f "$PRESERVED_PATH" ]; then
            sha256sum "$PRESERVED_PATH" >> "$PRESERVED_OUT" || return 1
        else
            [ ! -e "$PRESERVED_PATH" ] || return 1
            printf 'ABSENT %s\n' "$PRESERVED_PATH" >> "$PRESERVED_OUT" || return 1
        fi
    done
    if [ -e "$CHECKPOINT_DIR" ] || [ -L "$CHECKPOINT_DIR" ]; then
        [ -d "$CHECKPOINT_DIR" ] && [ ! -L "$CHECKPOINT_DIR" ] || return 1
        [ -z "$(find "$CHECKPOINT_DIR" -type l -print)" ] || return 1
        printf 'CHECKPOINT_PRESENT\n' >> "$PRESERVED_OUT" || return 1
        find "$CHECKPOINT_DIR" -type f -print | sort | while IFS= read -r PRESERVED_FILE; do
            sha256sum "$PRESERVED_FILE" || exit 1
        done >> "$PRESERVED_OUT" || return 1
    else
        printf 'CHECKPOINT_ABSENT\n' >> "$PRESERVED_OUT" || return 1
    fi
}

prepare_backup() {
''')
change(p,'    BACKUP_DIR="$ROOT/backups/freenet-web-update-$STAMP"\n    mkdir -p "$BACKUP_DIR" || return 1\n','''    BACKUP_DIR="$ROOT/backups/freenet-web-update-$STAMP.$$"
    umask 077
    mkdir -p "$ROOT/backups" || return 1
    mkdir "$BACKUP_DIR" || return 1
    chmod 700 "$BACKUP_DIR" || return 1
''')
change(p,'    snapshot_xray "$BACKUP_DIR/xray-hashes.before" || return 1\n','''    backup_one "$VERSION_FILE" version-marker || return 1
    snapshot_preserved_state "$BACKUP_DIR/preserved.before" || return 1
    snapshot_xray "$BACKUP_DIR/xray-hashes.before" || return 1
''')
for stage in ['after','rollback']:
    change(p,f'    snapshot_xray "$TMP_DIR/xray-hashes.{stage}" || return 1\n',f'    snapshot_preserved_state "$TMP_DIR/preserved.{stage}" || return 1\n    cmp -s "$BACKUP_DIR/preserved.before" "$TMP_DIR/preserved.{stage}" || return 1\n    snapshot_xray "$TMP_DIR/xray-hashes.{stage}" || return 1\n')
change(p,'    restart_ui "$CURRENT_VERSION" || return 1\n','    restore_one "$VERSION_FILE" version-marker || return 1\n    restart_ui "$CURRENT_VERSION" || return 1\n')
change(p,"    fetch_manifest \"$PLAN_TARGET\" || { plan_error 'release manifest is unavailable or incomplete'; return 1; }\n",'''    TARGET_TAG="$PLAN_TARGET"
    fetch_manifest "$PLAN_TARGET" || { plan_error 'release manifest is unavailable or incomplete'; return 1; }
    compatible_update_target "$PLAN_TARGET" || { plan_error 'target cannot preserve pending VPN recovery state; choose a recovery-protocol-2 release or the v0.7.18 recovery bridge'; return 1; }
''')
change(p,"    say 'MANIFEST_VERIFIED=yes'\n","    say 'MANIFEST_VERIFIED=yes'\n    say 'COMPATIBILITY_VERIFIED=yes'\n")
change(p,'    write_state FAILED "$TARGET_TAG" ', '    [ "$LOCK_HELD" = 1 ] && write_state FAILED "$TARGET_TAG" ',2)
change(p,"run_apply() {\n    detect_current || fail_before_mutation 'current FreeNet version is invalid'\n",r'''run_apply() {
    umask 077
    if ! mkdir "$LOCK_DIR" 2>/dev/null; then
        err 'update lock is already held; existing state preserved'
        exit 3
    fi
    LOCK_HELD=1
    if [ -e "$STATE_FILE" ] && [ ! -r "$STATE_FILE" ]; then
        KEEP_LOCK=1; err 'update state is unreadable'; exit 2
    fi
    # A missing lock never clears a previous unconfirmed application rollback.
    if [ -L "$STATE_FILE" ] || { [ -e "$STATE_FILE" ] && [ ! -f "$STATE_FILE" ]; } ||
       grep -Eq '^(STATE=(ROLLBACK_FAILED|UPDATING|RECONNECTING)|ROLLBACK_STATE=(FAILED_UNKNOWN|PENDING))$' "$STATE_FILE" 2>/dev/null; then
        KEEP_LOCK=1
        err 'previous application update is unresolved; state and backup preserved'
        exit 2
    fi
    printf '%s\n' "$$" > "$LOCK_DIR/owner.pid" || fail_before_mutation 'cannot persist update owner'
    printf '%s\n' "$TARGET_TAG" > "$LOCK_DIR/target" || fail_before_mutation 'cannot persist update target'
    if ! mkdir "$MUTATION_LOCK_DIR" 2>/dev/null; then
        fail_before_mutation 'another VPN/network operation is active; update not started'
    fi
    MUTATION_LOCK_HELD=1
    printf '%s\n' "$$" > "$MUTATION_LOCK_DIR/pid" || fail_before_mutation 'cannot persist network fence owner'
    printf '%s\n' "$LOCK_DIR" > "$MUTATION_LOCK_DIR/freenet-update-owner" || fail_before_mutation 'cannot persist fence ownership'
    detect_current || fail_before_mutation 'current FreeNet version is invalid'
''')
f=Path(p);s=f.read_text();a=s.index('    if ! mkdir "$LOCK_DIR" 2>/dev/null; then\n        write_state BUSY "$TARGET_TAG"');b=s.index('    make_tmp ||',a)
s=s[:a]+'''    compatible_update_target "$TARGET_TAG" || fail_before_mutation 'target cannot preserve pending VPN recovery state; choose a recovery-protocol-2 release or the v0.7.18 recovery bridge'
    write_state CHECKING "$TARGET_TAG" 'Проверяем выбранный exact release и SHA-256' '' NOT_NEEDED '' || fail_before_mutation 'cannot persist update state'

'''+s[b:];f.write_text(s)
change(p,"    MUTATED=1\n    write_state UPDATING \"$TARGET_TAG\" 'Применяем проверенные FreeNet assets' '' PENDING \"$BACKUP_DIR\" || true\n", "    write_state UPDATING \"$TARGET_TAG\" 'Применяем проверенные FreeNet assets' '' PENDING \"$BACKUP_DIR\" || fail_before_mutation 'cannot persist update checkpoint'\n    MUTATED=1\n")
change(p,"    restart_ui \"$TARGET_TAG\" || fail_after_mutation 'FreeNet UI restart failed'", "    [ \"$FAIL_STAGE\" != interrupt-after-replace ] || kill -TERM \"$$\"\n    restart_ui \"$TARGET_TAG\" || fail_after_mutation 'FreeNet UI restart failed'")
change(p,"    MUTATED=0\n    write_state SUCCESS \"$TARGET_TAG\" 'Выбранная версия FreeNet установлена и проверена' '' NOT_NEEDED \"$BACKUP_DIR\" || true\n", "    write_state SUCCESS \"$TARGET_TAG\" 'Выбранная версия FreeNet установлена и проверена' '' NOT_NEEDED \"$BACKUP_DIR\" || fail_after_mutation 'cannot persist update acceptance'\n    MUTATED=0\n")
