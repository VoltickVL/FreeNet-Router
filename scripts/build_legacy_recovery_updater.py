#!/usr/bin/env python3
"""Build the signed v0.7.18 compatibility updater from the canonical engine.

The engine remains the source of download/metadata/SHA/backup/rollback logic.
Only the initial Beta -> maintenance hop replaces two application-owned files;
legacy VPN helpers, cron, credentials and the pending checkpoint stay intact.
The generated file is hashed in SHA256SUMS BEFORE any router downloads it.
No verified file is patched on the router and no unsigned code is sourced.
"""
from pathlib import Path
import sys

POLICY = r'''
# BEGIN FREENET LEGACY BETA HANDOFF POLICY
legacy_binary_only_handoff() {
    HANDOFF_TARGET="${TARGET_TAG:-${LATEST_OVERRIDE:-}}"
    [ "$HANDOFF_TARGET" = v0.7.18 ] && beta_tag "$CURRENT_VERSION"
}

asset_list() {
    if legacy_binary_only_handoff; then
        printf '%s\n' "freenet-ui-$ARCH" self_update.sh
    else
        legacy_engine_asset_list
    fi
}

validate_stage() {
    if legacy_binary_only_handoff; then
        [ "$FAIL_STAGE" != staging ] || return 1
        [ -s "$TMP_DIR/freenet-ui-$ARCH" ] || return 1
        sh -n "$TMP_DIR/self_update.sh"
    else
        legacy_engine_validate_stage
    fi
}

run_plan() {
    legacy_engine_run_plan
    HANDOFF_PLAN_RC=$?
    if [ "$HANDOFF_PLAN_RC" -eq 0 ] && legacy_binary_only_handoff; then
        say 'RECOVERY_BINARY_ONLY=yes'
        say 'COMPONENTS=FreeNet recovery UI; verified self-update helper'
        say 'EXPECTED_DELTA=replace only FreeNet UI and self-update helper; restart only FreeNet UI into recovery-only mode; preserve installed Beta VPN helpers'
        say 'EXPECTED_NO_DELTA=VPN checkpoint; subscription; Xray configs and credentials; XKeen and Xray; DNS and routing; provider/VPN helpers; cron and automation preferences'
    fi
    return "$HANDOFF_PLAN_RC"
}
# END FREENET LEGACY BETA HANDOFF POLICY
'''


def build(source: str) -> str:
    for name in ('asset_list', 'validate_stage', 'run_plan'):
        signature = '\n' + name + '() {'
        if source.count(signature) != 1:
            raise ValueError('canonical updater function changed: ' + name)
        source = source.replace(signature, '\nlegacy_engine_' + name + '() {', 1)
    anchor = '\ncase "$MODE" in\n    plan) run_plan ;'
    if source.count(anchor) != 1:
        raise ValueError('canonical updater dispatch changed')
    return source.replace(anchor, '\n' + POLICY + anchor, 1)


def main() -> None:
    if len(sys.argv) != 3:
        raise SystemExit('usage: build_legacy_recovery_updater.py ENGINE OUTPUT')
    source_path, output_path = map(Path, sys.argv[1:])
    if source_path.resolve() == output_path.resolve():
        raise SystemExit('refusing to overwrite canonical source')
    output = build(source_path.read_text(encoding='utf-8'))
    output_path.parent.mkdir(parents=True, exist_ok=True)
    output_path.write_text(output, encoding='utf-8', newline='\n')
    output_path.chmod(0o755)


if __name__ == '__main__':
    main()
