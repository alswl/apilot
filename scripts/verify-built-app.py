#!/usr/bin/env python3
"""Ensure platform probes use the binary produced from this working tree."""
import hashlib
import json
import os
from pathlib import Path
import platform
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[1]
STAMP = ROOT / 'tmp/build-identity.json'

def fingerprint():
    paths = subprocess.check_output(['git', 'ls-files', '--cached', '--others', '--exclude-standard', '-z'], cwd=ROOT).split(b'\0')
    digest = hashlib.sha256()
    for item in sorted(set(paths)):
        if not item:
            continue
        p = ROOT / os.fsdecode(item)
        if (p.parts[len(ROOT.parts)] in {'core', 'desktop', 'connectors', 'packaging', 'hack', 'scripts'} or p.name in {'Makefile', 'go.mod', 'go.sum', 'mygo.json'}) and p.is_file() and not p.is_symlink():
            digest.update(item + b'\0' + p.read_bytes())
    return digest.hexdigest()

app = Path(os.environ.get('APILOT_VERIFY_APP', str(ROOT / 'build/darwin-arm64/Apilot.app/Contents/MacOS/Apilot'))).resolve()
if not app.is_file():
    raise SystemExit(f'Build App first: {app}')
subprocess.run(['lipo', str(app), '-verify_arch', platform.machine()], check=True)
bundle = app.parents[2]
subprocess.run(['python3', str(ROOT / 'scripts/verify-bundled-connectors.py'), str(bundle), '--arch', platform.machine()], check=True)
helper = bundle / 'Contents/Helpers/connectors/apilot-github-issues'
subprocess.run(['codesign', '--verify', '--strict', str(helper)], check=True)
identity = {'connector_sha256': hashlib.sha256(helper.read_bytes()).hexdigest(), 'source': fingerprint(), 'app': str(app), 'sha256': hashlib.sha256(app.read_bytes()).hexdigest()}
if '--record' in sys.argv:
    STAMP.parent.mkdir(parents=True, exist_ok=True)
    STAMP.write_text(json.dumps(identity, indent=2) + '\n')
elif not STAMP.is_file() or json.loads(STAMP.read_text()) != identity:
    raise SystemExit('App does not match this working tree; run make app before skipping the build.')
print('Verified current App build identity.')
