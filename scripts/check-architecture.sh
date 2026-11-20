#!/bin/bash
set -euo pipefail
cd "$(dirname "$0")/.."
case "${1:-}" in
  --core-packages) go -C core list ./... ;;
  --ui-packages) go -C desktop list ./... ;;
  '')
    python3 scripts/check-layers.py
    dependencies=$(go -C core list -deps -test ./...)
    while IFS= read -r package; do
      case "$package" in github.com/egoist/mygo|github.com/egoist/mygo/*) echo "Core depends on MyGo: $package" >&2; exit 1 ;; esac
    done <<< "$dependencies"
    echo 'Core dependency boundary verified.'
    ;;
  *) echo "Unknown option: $1" >&2; exit 2 ;;
esac
