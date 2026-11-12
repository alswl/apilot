#!/usr/bin/env python3
"""Stage the CLI in MyGo's OS/architecture resource overlay."""
import argparse
from pathlib import Path
import shutil

ROOT = Path(__file__).resolve().parents[1]
parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('--os', choices=('darwin', 'linux', 'windows'), required=True)
parser.add_argument('--arch', choices=('amd64', 'arm64'), required=True)
args = parser.parse_args()
name = 'apilot.exe' if args.os == 'windows' else 'apilot'
source = ROOT / 'bin' / name
if not source.is_file():
    parser.error(f'Build the target CLI first: {source}')
# Earlier builds staged this generated binary without an architecture qualifier.
# Leaving it there would make MyGo also merge the stale executable on macOS.
legacy = ROOT / 'resources' / 'darwin' / 'bin' / 'apilot'
legacy.unlink(missing_ok=True)
destination = ROOT / 'resources' / f'{args.os}-{args.arch}' / 'bin' / name
destination.parent.mkdir(parents=True, exist_ok=True)
shutil.copy2(source, destination)
print(destination.relative_to(ROOT))
