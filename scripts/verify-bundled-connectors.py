#!/usr/bin/env python3
"""Verify the reserved Connector directories of a production App bundle."""
import argparse
import hashlib
import json
import os
import re
import subprocess
from pathlib import Path

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('app', type=Path)
parser.add_argument('--arch', required=True, choices=('arm64', 'amd64'))
args = parser.parse_args()
allowlist_path = Path(__file__).resolve().parents[1] / 'packaging/darwin/connectors/allowlist.json'
allowlist = json.loads(allowlist_path.read_text())
app = args.app.resolve()
resources = app / 'Contents/Resources/connectors'
helpers = app / 'Contents/Helpers/connectors'

for directory, expected in (
    (resources, {connector + '.json' for connector in allowlist}),
    (helpers, set(allowlist.values())),
):
    if not directory.is_dir() or directory.resolve() != directory.absolute():
        parser.error(f'Invalid Connector directory: {directory}')
    if {entry.name for entry in directory.iterdir()} != expected:
        parser.error(f'Unexpected Connector inventory: {directory}')

for connector, name in allowlist.items():
    manifest = resources / (connector + '.json')
    executable = helpers / name
    for path in (manifest, executable):
        if path.is_symlink() or not path.is_file():
            parser.error(f'Invalid Connector file: {path}')
    if not os.access(executable, os.X_OK):
        parser.error(f'Connector entrypoint is not executable: {executable}')
    subprocess.run(['lipo', str(executable), '-verify_arch', args.arch], check=True)
    metadata = json.loads(manifest.read_text())
    expected = {
        'connector_id': connector,
        'manifest_version': 1,
        'os': 'darwin',
        'arch': args.arch,
        'entrypoint': name,
        'entrypoint_sha256': hashlib.sha256(executable.read_bytes()).hexdigest(),
    }
    if any(metadata.get(key) != value for key, value in expected.items()):
        parser.error(f'Connector manifest mismatch: {manifest}')
    if not re.fullmatch(r'\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?', metadata.get('release_version', '')):
        parser.error(f'Invalid Connector release version: {manifest}')
    if 1 not in metadata.get('protocol_versions', []):
        parser.error(f'Connector protocol v1 missing: {manifest}')
