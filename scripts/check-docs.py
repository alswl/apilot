#!/usr/bin/env python3
"""Validate public local links and pinned source integrity without Go or network."""
import hashlib
import json
from pathlib import Path
import re
import sys
from urllib.parse import unquote, urlsplit

ROOT = Path(__file__).resolve().parents[1]

def check():
    errors = []
    files = [ROOT / name for name in ('README.md', 'CONTRIBUTING.md', 'GLOSSARY.md')]
    for folder in ('docs',):
        files.extend((ROOT / folder).rglob('*.md'))
    for path in files:
        text = re.sub(r'^```.*?^```[^\n]*', '', path.read_text(), flags=re.M | re.S)
        if '/Users/' in text or 'file://' in text:
            errors.append(f'{path.relative_to(ROOT)}: private absolute path')
        for target in re.findall(r'\[[^\]]+\]\(([^)]+)\)', text):
            target = target.strip().strip('<>')
            uri = urlsplit(target)
            if uri.scheme or uri.netloc or not uri.path:
                continue
            resolved = (path.parent / unquote(uri.path)).resolve()
            if not resolved.is_relative_to(ROOT) or not resolved.exists():
                errors.append(f'{path.relative_to(ROOT)}: broken local link {target}')
    manifest = json.loads((ROOT / 'docs/standards/sources.json').read_text())
    for source in manifest:
        if not re.fullmatch('[a-f0-9]{40}', source['commit']):
            errors.append('Invalid upstream commit')
        for name, digest in source['files'].items():
            path = ROOT / name
            if not path.is_file() or hashlib.sha256(path.read_bytes()).hexdigest() != digest:
                errors.append(f'Pinned source differs from manifest: {name}')
    for error in errors:
        print(error, file=sys.stderr)
    if not errors:
        print(f'Public documentation and {len(manifest)} pinned sources verified.')
    return bool(errors)

if __name__ == '__main__':
    sys.exit(check())
