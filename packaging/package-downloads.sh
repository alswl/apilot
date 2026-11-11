#!/bin/sh
set -eu
cd "$(dirname "$0")/.."

app_path=build/darwin-arm64/Apilot.app
output_dir=dist
if [ "$(uname -s)" != Darwin ] || [ "$(uname -m)" != arm64 ]; then
  echo 'Packaging requires macOS arm64.' >&2
  exit 1
fi

test -d "$app_path"
test -x bin/apilot
codesign --verify --deep --strict "$app_path"
./bin/apilot version --json | python3 -c 'import json,sys; from pathlib import Path; assert json.load(sys.stdin)["data"]["app_version"] == json.loads(Path("packaging/mygo.json").read_text())["version"], "CLI and App versions must match"'
mkdir -p "$output_dir"
# MyGo builds the drag-to-Applications image together with the App.
version=$(python3 -c 'import json; print(json.load(open("packaging/mygo.json"))["version"])')
dmg_path="build/darwin-arm64/apilot-$version-arm64.dmg"
hdiutil verify "$dmg_path"
cp "$dmg_path" "$output_dir/apilot-macos-arm64.dmg"
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
    'app_binary_sha256': hashlib.sha256(Path('build/darwin-arm64/Apilot.app/Contents/MacOS/Apilot').read_bytes()).hexdigest(),
    'cli_binary_sha256': hashlib.sha256(Path('bin/apilot').read_bytes()).hexdigest(),
    'go_version': command('go', 'version'),
    'platform': platform.platform(),
    'architecture': platform.machine(),
    'built_at': datetime.datetime.now(datetime.timezone.utc).isoformat(),
    'signing': 'developer-id' if 'Authority=Developer ID Application:' in subprocess.run(['codesign', '-dv', 'build/darwin-arm64/Apilot.app'], capture_output=True, text=True, check=True).stderr else 'ad-hoc',
    'notarized': subprocess.run(['xcrun', 'stapler', 'validate', 'build/darwin-arm64/Apilot.app'], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL).returncode == 0,
}
Path('dist/build-info.json').write_text(json.dumps(info, indent=2) + '\n')
PY
(
  cd "$output_dir"
  shasum -a 256 apilot-macos-arm64.dmg apilot-macos-arm64.zip apilot-cli-macos-arm64.tar.gz build-info.json > SHA256SUMS
  shasum -a 256 --check SHA256SUMS
)

verify_dir=$(mktemp -d)
trap 'rm -rf "$verify_dir"' EXIT HUP INT TERM
ditto -x -k "$output_dir/apilot-macos-arm64.zip" "$verify_dir"
codesign --verify --deep --strict "$verify_dir/Apilot.app"
tar -xzf "$output_dir/apilot-cli-macos-arm64.tar.gz" -C "$verify_dir"
test -x "$verify_dir/apilot"
"$verify_dir/apilot" version --json > /dev/null
