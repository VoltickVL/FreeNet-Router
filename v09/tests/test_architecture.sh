#!/bin/sh
# One enforceable boundary for every PR against experiment/v0.9-clean.
# This is deliberately not a replacement for reviewer / runtime acceptance.
set -eu

fail() { printf '%s\n' "ARCHITECTURE STOP: $*" >&2; exit 1; }

allowed_path() {
    case "$1" in
        v09/*|AGENTS.md|.github/workflows/v09-isolated.yml) return 0 ;;
        *) return 1 ;;
    esac
}

if [ "${1:-}" = "--selftest" ]; then
    for path in v09/cmd/freenet09/main.go v09/PRODUCT_CONTRACT.md AGENTS.md .github/workflows/v09-isolated.yml; do
        allowed_path "$path" || fail "allowlist rejected legitimate v0.9 file: $path"
    done
    for path in freenet-ui/main.go scripts/self_update.sh bootstrap.sh install.sh deploy/S99freenet-ui .github/workflows/release.yml; do
        if allowed_path "$path"; then
            fail "allowlist accepted legacy path: $path"
        fi
    done
    printf '%s\n' 'ARCHITECTURE_SELFTEST=SUCCESS'
    exit 0
fi

[ -f AGENTS.md ] || fail 'repository-wide agent rules absent'
[ -f v09/PRODUCT_CONTRACT.md ] || fail 'v0.9 product contract absent'
[ -f v09/go.mod ] || fail 'clean Go module absent'
grep -Fxq 'module github.com/VoltickVL/FreeNet-Router/v09' v09/go.mod ||
    fail 'v09 must stay an independent module'
if grep -Eq '^[[:space:]]*(replace|require)[[:space:]]' v09/go.mod; then
    fail 'new dependency/legacy reference needs explicit architecture review'
fi
if grep -R -nE 'github.com/VoltickVL/FreeNet-Router/(freenet-ui|scripts)|\.\./\.\./freenet-ui' \
    --include='*.go' v09/cmd 2>/dev/null; then
    fail '0.9 source imports legacy backend'
fi

[ -f v09/VERSION ] || fail 'v09 VERSION absent'
version="$(tr -d '\r\n' < v09/VERSION)"
case "$version" in
    v0.9.0-dev) ;;
    v0.9.0)
        # Never silently publish a partial preview as the final working build.
        fail 'stable 0.9 release requires separate full acceptance/release cycle' ;;
    *) fail "unexpected v09 version $version" ;;
esac

base_sha="${1:-}"
target_branch="${2:-}"
if [ "$target_branch" = experiment/v0.9-clean ]; then
    [ -n "$base_sha" ] || fail 'PR base SHA absent'
    git cat-file -e "$base_sha^{commit}" || fail 'PR base SHA unavailable'
    git diff --name-only "$base_sha" HEAD > /tmp/v09-changed-paths.$$
    trap 'rm -f /tmp/v09-changed-paths.$$' EXIT HUP INT TERM
    while IFS= read -r path; do
        [ -n "$path" ] || continue
        allowed_path "$path" || fail "v09 PR touches legacy/foreign code: $path"
    done < /tmp/v09-changed-paths.$$
    printf '%s\n' 'V09_SCOPE=ISOLATED'
else
    printf '%s\n' 'V09_SCOPE=NOT_A_CLEAN_BRANCH_PR'
fi
printf '%s\n' 'ARCHITECTURE=SUCCESS'
