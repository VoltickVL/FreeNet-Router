#!/bin/sh
set -eu

upper="$(printf '\111\123\120')"
lower="$(printf '\151\163\160')"
self="$(basename "$0")"
scan_paths=".github README.md bootstrap.sh config docs freenet-ui scripts tests"
failed=0

check_marker() {
  marker="$1"
  hits="$(grep -RInF --exclude="$self" -- "$marker" $scan_paths 2>/dev/null || true)"
  if [ -n "$hits" ]; then
    echo "[retired-network-product] unexpected marker: $marker" >&2
    echo "$hits" >&2
    failed=1
  fi
}

check_absent_path() {
  path="$1"
  if [ -e "$path" ]; then
    echo "[retired-network-product] retired artifact still exists: $path" >&2
    failed=1
  fi
}

check_marker "${upper}_ID"
check_marker "${lower}Profiles"
check_marker "${lower}Select"
check_marker "top${upper}Value"
check_marker "policy_${lower}"
check_marker "${lower}-presets"
check_marker "${lower}_presets"
check_marker "recommended_dns_mode"
check_marker "active_${lower}"

check_absent_path "config/${lower}-presets.json"
check_absent_path "docs/${upper}-PRESETS-RU.md"
check_absent_path "freenet-ui/policy_${lower}_templates.go"
check_absent_path "freenet-ui/policy_${lower}_templates_test.go"

if [ "$failed" -ne 0 ]; then
  exit 1
fi

echo "retired network product layer: clean"
