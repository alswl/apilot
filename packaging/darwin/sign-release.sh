#!/bin/bash
# Release-only: import credentials into an isolated, temporary keychain.
set -euo pipefail
cd "$(dirname "$0")/../.."
: "${APPLE_CERTIFICATE_P12_BASE64:?Missing Developer ID certificate secret}"
: "${APPLE_CERTIFICATE_PASSWORD:?Missing certificate password secret}"
: "${APPLE_SIGNING_IDENTITY:?Missing Developer ID Application identity}"
: "${APPLE_API_KEY_P8_BASE64:?Missing App Store Connect API key secret}"
: "${APPLE_API_KEY_ID:?Missing API key ID}"
: "${APPLE_API_ISSUER_ID:?Missing API issuer ID}"
case "$APPLE_SIGNING_IDENTITY" in
  'Developer ID Application: '*) ;;
  *) echo 'Expected a Developer ID Application identity.' >&2; exit 1 ;;
esac
work=$(mktemp -d)
keychain="$work/apilot-signing.keychain-db"
keychain_password=$(openssl rand -hex 24)
security list-keychains -d user > "$work/original-keychains"
cp mygo.json "$work/mygo.json"
cleanup() {
  set +e
  cp "$work/mygo.json" mygo.json
  python3 - "$work/original-keychains" <<'PY'
import shlex, subprocess, sys
subprocess.run(['security', 'list-keychains', '-d', 'user', '-s', *shlex.split(open(sys.argv[1]).read())], check=True)
PY
  security delete-keychain "$keychain" || true
  rm -rf "$work"
}
trap cleanup EXIT
printf '%s' "$APPLE_CERTIFICATE_P12_BASE64" | base64 --decode > "$work/certificate.p12"
printf '%s' "$APPLE_API_KEY_P8_BASE64" | base64 --decode > "$work/AuthKey.p8"
chmod 600 "$work/certificate.p12" "$work/AuthKey.p8"
security create-keychain -p "$keychain_password" "$keychain"
security set-keychain-settings -lut 21600 "$keychain"
security unlock-keychain -p "$keychain_password" "$keychain"
security import "$work/certificate.p12" -k "$keychain" -P "$APPLE_CERTIFICATE_PASSWORD" -T /usr/bin/codesign
security set-key-partition-list -S apple-tool:,apple:,codesign: -s -k "$keychain_password" "$keychain" > /dev/null
python3 - "$work/original-keychains" "$keychain" <<'PY'
import shlex, subprocess, sys
subprocess.run(['security', 'list-keychains', '-d', 'user', '-s', sys.argv[2], *shlex.split(open(sys.argv[1]).read())], check=True)
PY
xcrun notarytool store-credentials apilot-notary --key "$work/AuthKey.p8" --key-id "$APPLE_API_KEY_ID" --issuer "$APPLE_API_ISSUER_ID" --keychain "$keychain" > /dev/null
python3 - "$keychain" <<'PY'
import json, os, sys
from pathlib import Path
p = Path('mygo.json')
c = json.loads(p.read_text())
c['macos']['signingIdentity'] = os.environ['APPLE_SIGNING_IDENTITY']
c['macos']['notarize'] = {'keychainProfile': 'apilot-notary', 'keychain': sys.argv[1]}
p.write_text(json.dumps(c, indent=2) + '\n')
PY
# MyGo signs nested executables, enables hardened runtime/timestamps, builds
# the DMG, submits it once, and staples the DMG and original App bundle.
make app MYGO_BUILD_FLAGS=
xcrun stapler validate build/darwin-arm64/Apilot.app
version=$(python3 -c 'import json; print(json.load(open("mygo.json"))["version"])')
xcrun stapler validate "build/darwin-arm64/apilot-$version-arm64.dmg"
spctl --assess --type execute --verbose build/darwin-arm64/Apilot.app
# Distribute the very same signed CLI that Apple scanned inside the App.
cp build/darwin-arm64/Apilot.app/Contents/Resources/bin/apilot bin/apilot
codesign --verify --strict bin/apilot
