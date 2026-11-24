#!/usr/bin/env python3
"""Install and sign the allowed helper before sealing the macOS bundle and DMG."""
import argparse
import hashlib
import json
from pathlib import Path
import shutil
import subprocess
import tempfile

ROOT = Path(__file__).resolve().parents[2]


def run(*args, **kwargs):
    return subprocess.run(args, check=True, **kwargs)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('app', type=Path)
    parser.add_argument('--arch', required=True, choices=('arm64', 'amd64'))
    parser.add_argument('--skip-dmg', action='store_true')
    parser.add_argument('--skip-notarize', action='store_true')
    args = parser.parse_args()
    app = args.app.resolve()
    config = json.loads((ROOT / 'desktop/mygo.json').read_text())
    mac = config.get('macos', {})
    identity = mac.get('signingIdentity') or '-'
    production = identity != '-'
    sign = ['codesign', '--force', '--sign', identity]
    sign += ['--options', 'runtime', '--timestamp'] if production else ['--timestamp=none']

    helpers = app / 'Contents/Helpers/connectors'
    manifests = app / 'Contents/Resources/connectors'
    for directory in (helpers, manifests):
        directory.mkdir(parents=True, exist_ok=False)
    source = ROOT / 'packaging/darwin/connectors'
    allowlist = json.loads((source / 'allowlist.json').read_text())
    for connector_id, name in allowlist.items():
        binary = ROOT / 'bin' / name
        build_info = run('go', 'version', '-m', binary, capture_output=True, text=True).stdout
        if 'apilot_controlled_github' in build_info:
            parser.error(f'Controlled test build cannot ship: {binary}')
        helper = Path(shutil.copy2(binary, helpers / name))
        run(*sign, helper)
        digest = hashlib.sha256(helper.read_bytes()).hexdigest()
        metadata = (source / f'{connector_id}.json.in').read_text()
        (manifests / f'{connector_id}.json').write_text(
            metadata.replace('@ARCH@', args.arch).replace('@ENTRYPOINT_SHA256@', digest)
        )
    run(*sign, '--preserve-metadata=identifier,entitlements', app)
    run('codesign', '--verify', '--deep', '--strict', app)
    run('python3', ROOT / 'scripts/verify-bundled-connectors.py', app, '--arch', args.arch)
    if args.skip_dmg:
        return

    dmg = app.parent / f"{config['slug']}-{config['version']}-{args.arch}.dmg"
    with tempfile.TemporaryDirectory(prefix='apilot-dmg-') as temp:
        stage = Path(temp)
        run('ditto', app, stage / app.name)
        (stage / 'Applications').symlink_to('/Applications')
        run('hdiutil', 'create', '-ov', '-fs', 'APFS', '-format', 'UDZO', '-volname',
            mac.get('dmgTitle', config['name']), '-srcfolder', stage, dmg)
    if production:
        run(*sign, dmg)
    notarize = mac.get('notarize')
    if notarize and not args.skip_notarize:
        command = ['xcrun', 'notarytool', 'submit', str(dmg), '--wait',
                   '--keychain-profile', notarize['keychainProfile']]
        if notarize.get('keychain'):
            command += ['--keychain', notarize['keychain']]
        run(*command)
        run('xcrun', 'stapler', 'staple', dmg)
        run('xcrun', 'stapler', 'staple', app)


if __name__ == '__main__':
    main()
