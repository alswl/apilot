#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 1 || $# -gt 4 ]]; then
  printf 'Usage: %s OUTPUT_DIRECTORY [VERSION [CONNECTOR_ID [both|project|schema2|renamed|multi|multi-endpoint2|required-endpoint2|scope-edit|scope-edit-paged]]]\n' "$0" >&2
  exit 2
fi
mkdir -p "$1"
output=$(cd "$1" && pwd -P)
version=${2:-0.1.0}
id=${3:-synthetic-scopes}
scope_mode=${4:-both}
if [[ ! $id =~ ^[a-z0-9]+(-[a-z0-9]+)*$ || ! $version =~ ^[0-9]+\.[0-9]+\.[0-9]+$ || ( $scope_mode != both && $scope_mode != project && $scope_mode != schema2 && $scope_mode != renamed && $scope_mode != multi && $scope_mode != multi-endpoint2 && $scope_mode != required-endpoint2 && $scope_mode != scope-edit && $scope_mode != scope-edit-paged ) ]]; then
  printf 'Invalid synthetic Connector ID, version or scope mode\n' >&2
  exit 2
fi
protocol_dir=$(cd "$(dirname "$0")/../connectors/protocol" && pwd -P)
scratch=$(mktemp -d "$output/.build.XXXXXX")
trap 'rm -rf "$scratch"' EXIT
go -C "$protocol_dir" build -ldflags "-X main.fixtureConnectorID=$id -X main.fixtureReleaseVersion=$version -X main.fixtureScopeMode=$scope_mode" -o "$scratch/connector" ./cmd/apilot-fixture
go -C "$protocol_dir" run ./cmd/apilot-fixture-package -binary "$scratch/connector" -id "$id" -version "$version" -out "$output"
