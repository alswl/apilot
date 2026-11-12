#!/bin/bash
set -euo pipefail
cd "$(dirname "$0")/.."
# These shared packages must stay independent of the desktop and OS adapters.
for target in linux windows; do
  GOOS="$target" GOARCH=amd64 CGO_ENABLED=0 go build ./pkg/common ./pkg/config ./pkg/managers ./internal/presentation
  echo "Shared packages compiled for $target/amd64 (no application/runtime claim)."
done
