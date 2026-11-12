#!/bin/bash
set -euo pipefail
cd "$(dirname "$0")/.."
all=$(go list ./...)
test -n "$all"
core=()
ui=()
while IFS= read -r package; do
  case "$package" in
    github.com/alswl/apilot/internal/ui|github.com/alswl/apilot/internal/ui/*|github.com/alswl/apilot/cmd/apilot-app) ui+=("$package") ;;
    *) core+=("$package") ;;
  esac
done <<< "$all"
test "$(printf '%s\n' "$all" | wc -l | tr -d ' ')" -eq "$((${#core[@]} + ${#ui[@]}))"
case "${1:-}" in
  --core-packages) printf '%s\n' "${core[@]}" ;;
  --ui-packages) printf '%s\n' "${ui[@]}" ;;
  '')
    dependencies=$(go list -deps -test "${core[@]}")
    while IFS= read -r package; do
      case "$package" in github.com/egoist/mygo|github.com/egoist/mygo/*) echo "Core depends on MyGo: $package" >&2; exit 1 ;; esac
    done <<< "$dependencies"
    echo "Core dependency boundary verified (${#core[@]} core, ${#ui[@]} UI packages)."
    ;;
  *) echo "Unknown option: $1" >&2; exit 2 ;;
esac
