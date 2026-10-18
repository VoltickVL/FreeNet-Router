from pathlib import Path
p=Path('freenet-ui/self_update_admission.go');s=p.read_text()
a='\tif (rollback == "PENDING" || state == "UPDATING" || state == "RECONNECTING") && !a.updateLockHeld() {'
b='''\tswitch rollback {
	case "", "NOT_NEEDED", "SUCCESS", "PENDING":
	default:
		return errors.New("Неизвестный результат отката приложения; состояние сохранено для Recovery.")
	}
'''+a
assert s.count(a)==1;s=s.replace(a,b);p.write_text(s)
p=Path('scripts/self_update.sh');s=p.read_text()
a='run_apply() {\n    umask 077'
b='''previous_update_receipt_safe() {
    [ -e "$STATE_FILE" ] || [ -L "$STATE_FILE" ] || return 0
    [ -f "$STATE_FILE" ] && [ ! -L "$STATE_FILE" ] && [ -r "$STATE_FILE" ] || return 1
    [ "$(wc -c < "$STATE_FILE")" -le 131072 ] || return 1
    OLD_UPDATE_STATE="$(sed -n 's/^STATE=//p' "$STATE_FILE")"
    OLD_UPDATE_ROLLBACK="$(sed -n 's/^ROLLBACK_STATE=//p' "$STATE_FILE")"
    case "$OLD_UPDATE_STATE" in ''|IDLE|BUSY|CHECKING|SNAPSHOT|FAILED|SUCCESS) ;; *) return 1 ;; esac
    case "$OLD_UPDATE_ROLLBACK" in ''|NOT_NEEDED|SUCCESS) return 0 ;; *) return 1 ;; esac
}

run_apply() {
    umask 077'''
assert s.count(a)==1;s=s.replace(a,b)
a='    LOCK_HELD=1\n    if [ -e "$STATE_FILE" ]'
b='''    LOCK_HELD=1
    if ! previous_update_receipt_safe; then
        KEEP_LOCK=1
        err 'previous application receipt is unknown; original state preserved'
        exit 2
    fi
    if [ -e "$STATE_FILE" ]'''
assert s.count(a)==1;s=s.replace(a,b);p.write_text(s)
p=Path('freenet-ui/self_update_admission_test.go');s=p.read_text()
s+='''
func TestUpdateOperationSlotAndUnknownRollback(t *testing.T) {
 a := &app{sem: make(chan struct{}, 1)}
 first, ok := a.reserveApplicationUpdateSlot()
 if !ok { t.Fatal("free slot unavailable") }
 if _, ok := a.reserveApplicationUpdateSlot(); ok { t.Fatal("overlapping application mutation accepted") }
 first(); first()
 release, ok := a.reserveApplicationUpdateSlot()
 if !ok { t.Fatal("idempotent release lost slot") }; release()
 root := t.TempDir(); state := filepath.Join(root, "state")
 a.cfg.UpdateState = state
 os.WriteFile(state, []byte("STATE=FAILED\\nROLLBACK_STATE=UNKNOWN\\n"), 0600)
 if a.selfUpdateStateAdmission() == nil { t.Fatal("unknown rollback result accepted") }
}
''';p.write_text(s)
p=Path('tests/test_self_update_lifecycle.sh');s=p.read_text();a="echo 'self-update lifecycle:"
pos=s.index(a)
s=s[:pos]+'''# Unknown textual rollback is not permission to start over.
make_root "$R"
printf 'STATE=FAILED\\nROLLBACK_STATE=UNKNOWN\\n' > "$R/var/run/update.state"
BEFORE_UNKNOWN="$(cat "$R/var/run/update.state")"
if run_apply "$R" "$D" '' > "$TMP/unknown-receipt.out" 2>&1; then fail 'unknown receipt accepted'; fi
[ "$BEFORE_UNKNOWN" = "$(cat "$R/var/run/update.state")" ] || fail 'unknown receipt overwritten'

'''+s[pos:];p.write_text(s)
