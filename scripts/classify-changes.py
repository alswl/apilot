#!/usr/bin/env python3
"""Conservative change classification. Diff errors always require full checks."""
import argparse
import os
from pathlib import PurePosixPath
import subprocess

DOC_ROOTS = {'README.md', 'CONTRIBUTING.md', 'GLOSSARY.md', 'LICENSE', 'LICENSE.md'}
DOC_SUFFIXES = {'.md', '.png', '.jpg', '.jpeg', '.svg', '.webp'}

def requires_full(paths, event, baseline_available):
    if event in ('tag', 'workflow_dispatch') or not baseline_available or not paths:
        return True
    return any(path not in DOC_ROOTS and not (
        path.startswith('docs/')
        and PurePosixPath(path).suffix.lower() in DOC_SUFFIXES
    ) for path in paths)

def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--event', default=os.environ.get('GITHUB_EVENT_NAME', ''))
    parser.add_argument('--base', default='')
    args = parser.parse_args()
    event = 'tag' if os.environ.get('GITHUB_REF', '').startswith('refs/tags/') else args.event
    paths = []
    valid = False
    if args.base and set(args.base) != {'0'}:
        try:
            base = args.base
            if event == 'pull_request':
                base = subprocess.check_output(['git', 'merge-base', '--', base, 'HEAD'], text=True).strip()
            raw = subprocess.check_output(['git', 'diff', '--no-renames', '--name-only', '-z', base, 'HEAD', '--'])
            paths = [os.fsdecode(p) for p in raw.split(b'\0') if p]
            valid = True
        except subprocess.CalledProcessError:
            pass
    full = requires_full(paths, event, valid)
    result = f'full={str(full).lower()}\n'
    print(result, end='')
    if output := os.environ.get('GITHUB_OUTPUT'):
        with open(output, 'a') as f:
            f.write(result)

if __name__ == '__main__':
    main()
