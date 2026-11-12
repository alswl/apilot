#!/usr/bin/env python3
"""Read-only source checks shared by local development and CI."""
import json
from pathlib import Path
import re
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[1]

def files():
    raw = subprocess.check_output(['git', 'ls-files', '--cached', '--others', '--exclude-standard', '-z', '*.go'], cwd=ROOT)
    return sorted({str(ROOT / p.decode()) for p in raw.split(b'\0') if p and not p.startswith(b'hack/makefile-go/') and (ROOT / p.decode()).is_file()})

def main():
    mode = sys.argv[1]
    if mode in ('format', 'fmt'):
        args = ['gofmt', '-w' if mode == 'fmt' else '-l', *files()]
        result = subprocess.run(args, text=True, capture_output=True, check=True)
        if result.stdout:
            print(result.stdout, end='')
            if mode == 'format':
                return 1
    elif mode == 'version':
        declared = re.search(r'const Version = "([^"]+)"', (ROOT / 'pkg/common/version.go').read_text()).group(1)
        packaged = json.loads((ROOT / 'mygo.json').read_text())['version']
        if declared != packaged:
            raise ValueError(f'Version mismatch: {declared} != {packaged}')
        print(f'App/CLI source version: {declared}')
    else:
        raise ValueError(f'Unknown source check: {mode}')
    return 0

if __name__ == '__main__':
    sys.exit(main())
