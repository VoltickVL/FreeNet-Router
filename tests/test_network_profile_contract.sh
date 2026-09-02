#!/bin/sh

set -eu

ROOT_DIR="$(CDPATH= cd "$(dirname "$0")/.." && pwd)"
SCRIPT="$ROOT_DIR/scripts/apply_network_profile.sh"

fail() { echo "network profile contract FAIL: $*" >&2; exit 1; }

sh -n "$SCRIPT"

grep -Fq 'rostelecom)' "$SCRIPT" || fail 'Rostelecom preset missing'
grep -Fq 'podryad)' "$SCRIPT" || fail 'Podryad must remain a separate preset'
grep -Fq 'vladlink)' "$SCRIPT" || fail 'Vladlink must remain a separate preset'
grep -Fq 'alliancetelecom)' "$SCRIPT" || fail 'AllianceTelecom must remain a separate preset'

grep -Fq "REASON='Подряд имеет отдельный preset ID" "$SCRIPT" || fail 'Podryad runtime gate missing'
grep -Fq "REASON='Владлинк имеет отдельный preset ID" "$SCRIPT" || fail 'Vladlink clean-room gate missing'
grep -Fq "REASON='АльянсТелеком имеет отдельный preset ID" "$SCRIPT" || fail 'AllianceTelecom clean-room gate missing'

grep -Fq 'PORT53_OWNER=ndnproxy' "$SCRIPT" || fail 'ndnproxy fact reporting missing'
grep -Fq 'expected 11111' "$SCRIPT" || fail 'Xray GID acceptance missing'
grep -Fq 'proxy_dns=on' "$SCRIPT" || fail 'XKeen proxy_dns preflight missing'
grep -Fq 'VLESS_PROFILE=' "$SCRIPT" || fail 'VLESS presence fact missing'
grep -Fq 'MUTATION=NONE' "$SCRIPT" || fail 'read-only plan marker missing'
grep -Fq 'EXPECTED_NO_DELTA=no new Xray listener :53; no VLESS credential rewrite; no subscription secret change' "$SCRIPT" || fail 'no-delta boundary missing'

grep -Fq '"$MIGRATE_SCRIPT"' "$SCRIPT" || fail 'transactional migration engine delegation missing'
grep -Fq 'ROLLBACK ERROR/STATE: FAILED/UNKNOWN' "$SCRIPT" || fail 'rollback unknown state missing'
grep -Fq 'RESULT=SUCCESS' "$SCRIPT" || fail 'success marker missing'

if grep -Ei 'subscription.*url=|uuid=|publicKey|shortId|vless://' "$SCRIPT" >/dev/null; then
    fail 'network profile controller contains secret material'
fi

# The controller must not implement arbitrary custom shell commands.
if grep -Eq 'eval[[:space:]]+.*(ISP|DNS)|sh[[:space:]]+-c[[:space:]]+.*(ISP|DNS)' "$SCRIPT"; then
    fail 'network profile values must not become shell commands'
fi

echo 'network profile contract PASS'
