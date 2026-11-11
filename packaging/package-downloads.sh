#!/bin/sh
set -eu
cd "$(dirname "$0")/.."

app_path=build/darwin-arm64/apilot.app
output_dir=dist
if [ "$(uname -s)" != Darwin ] || [ "$(uname -m)" != arm64 ]; then
  echo 'Packaging requires macOS arm64.' >&2
  exit 1
fi

test -d "$app_path"
test -x bin/apilot
codesign --verify --deep --strict "$app_path"
./bin/apilot version --json > /dev/null
mkdir -p "$output_dir"
rm -f "$output_dir/apilot-macos-arm64.zip" "$output_dir/apilot-cli-macos-arm64.tar.gz"
ditto -c -k --sequesterRsrc --keepParent "$app_path" "$output_dir/apilot-macos-arm64.zip"
tar -C bin -czf "$output_dir/apilot-cli-macos-arm64.tar.gz" apilot
python3 - <<'PY'
import datetime, hashlib, json, platform, subprocess
from pathlib import Path

def command(*args):
    return subprocess.check_output(args, text=True).strip()

info = {
    'version': json.loads(Path('packaging/mygo.json').read_text())['version'],
    'source_commit': command('git', 'rev-parse', 'HEAD'),
    'source_dirty': bool(command('git', 'status', '--porcelain')),
    'app_binary_sha256': hashlib.sha256(Path('build/darwin-arm64/apilot.app/Contents/MacOS/apilot').read_bytes()).hexdigest(),
    'cli_binary_sha256': hashlib.sha256(Path('bin/apilot').read_bytes()).hexdigest(),
    'go_version': command('go', 'version'),
    'platform': platform.platform(),
    'architecture': platform.machine(),
    'built_at': datetime.datetime.now(datetime.timezone.utc).isoformat(),
    'signing': 'ad-hoc',
    'notarized': False,
}
Path('dist/build-info.json').write_text(json.dumps(info, indent=2) + '\n')
PY
(
  cd "$output_dir"
  shasum -a 256 apilot-macos-arm64.zip apilot-cli-macos-arm64.tar.gz build-info.json > SHA256SUMS
  shasum -a 256 --check SHA256SUMS
)

verify_dir=$(mktemp -d)
trap 'rm -rf "$verify_dir"' EXIT HUP INT TERM
ditto -x -k "$output_dir/apilot-macos-arm64.zip" "$verify_dir"
codesign --verify --deep --strict "$verify_dir/apilot.app"
tar -xzf "$output_dir/apilot-cli-macos-arm64.tar.gz" -C "$verify_dir"
test -x "$verify_dir/apilot"
"$verify_dir/apilot" version --json > /dev/null
