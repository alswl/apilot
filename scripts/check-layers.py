#!/usr/bin/env python3
"""Check first-party production dependencies against the fixed Go guide layers."""
import json
from pathlib import Path
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[1]
PREFIX = 'github.com/alswl/apilot/'
ALLOWED = {
    'common': set(),
    'config': set(),
    'dal': {'common'},
    'integrations': {'common'},
    'managers': {'common'},
    'services': {'common', 'managers', 'integrations'},
    'server': {'common', 'config', 'dal', 'services'},
}

def main():
    raw = subprocess.check_output(['go', 'list', '-json', './...'], cwd=ROOT).decode()
    decoder = json.JSONDecoder()
    errors = []
    while raw.strip():
        raw = raw.lstrip()
        package, end = decoder.raw_decode(raw)
        raw = raw[end:]
        path = package['ImportPath'].removeprefix(PREFIX)
        if path.startswith('pkg/'):
            layer = path.split('/')[1]
            if layer not in ALLOWED or path != 'pkg/' + layer:
                errors.append(f'Unexpected application package: {path}')
            for dependency in package.get('Imports', []):
                if layer in {'services', 'managers', 'common'} and dependency in {'syscall', 'golang.org/x/sys/unix', 'golang.org/x/sys/windows'}:
                    errors.append(f'Platform syscall must live in an adapter: {path} -> {dependency}')
                if not dependency.startswith(PREFIX):
                    continue
                target = dependency.removeprefix(PREFIX)
                if not target.startswith('pkg/') or target.split('/')[1] not in ALLOWED.get(layer, set()):
                    errors.append(f'Forbidden dependency: {path} -> {target}')
        elif path == 'cmd/apilot':
            for dependency in package.get('Imports', []):
                if dependency.startswith(PREFIX) and dependency.removeprefix(PREFIX) not in {'pkg/common', 'pkg/config', 'pkg/services'}:
                    errors.append(f'Command bypasses services: {dependency}')
        elif path.startswith('internal/') and path not in {'internal/ui', 'internal/ui/locales', 'internal/presentation'}:
            errors.append(f'Unexpected internal package: {path}')
    for executable in ('apilot', 'apilot-app'):
        text = (ROOT / 'cmd' / executable / 'main.go').read_text()
        if 'os.Exit(Execute())' not in text:
            errors.append(f'Entry point must delegate to Execute: {executable}')
    if '.AddCommand(' in (ROOT / 'cmd/apilot/root.go').read_text():
        errors.append('root.go must not enumerate subcommands')
    for path in (ROOT / 'cmd/apilot').glob('*.go'):
        if path.name.endswith('_test.go') or path.name == 'root.go':
            continue
        text = path.read_text()
        if 'Use:' in text and ('func init()' not in text or '.AddCommand(' not in text):
            errors.append(f'Command must self-register in init: {path.name}')
    if errors:
        print('\n'.join(errors), file=sys.stderr)
        return 1
    print('Go guide layers and command/service boundary verified.')
    return 0

if __name__ == '__main__':
    sys.exit(main())
