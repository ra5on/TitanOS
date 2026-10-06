#!/usr/bin/env python3
"""Verify the complete current checkout without importing or publishing source."""
import json
import os
from pathlib import Path
import subprocess
import sys

from release_identity import REPOSITORY, validate_release

MINIMUM_SOURCE_FILES = 2200
REQUIRED_SOURCE_FILES = (
    'package.json', 'LICENSE.md', 'UPSTREAM.md', 'README.upstream.md',
    '.titan/imported-source.json', '.titan/release-public.pem', '.titan/release.json',
    'packages/titand/package.json', 'packages/titand/source/index.ts',
    'packages/ui/package.json', 'packages/os/build.sh', 'packages/os/titanos.Dockerfile',
    'packages/os/rugix/recipes/setup-rugix/files/bootstrapping.toml',
)


def verify_source(root):
    root = Path(root).resolve()
    repository = os.environ.get('GITHUB_REPOSITORY')
    if repository and repository != REPOSITORY:
        raise ValueError(f'TitanOS images can be built only in {REPOSITORY}')
    tracked = subprocess.check_output(['git', '-C', str(root), 'ls-files', '-z']).decode().split('\0')
    names = set(filter(None, tracked))
    if len(names) < MINIMUM_SOURCE_FILES or not set(REQUIRED_SOURCE_FILES) <= names:
        raise ValueError('The checkout does not contain the complete TitanOS source and attribution')
    if any(not os.path.lexists(root / name) for name in names):
        raise ValueError('Tracked TitanOS source is missing from the checkout')
    for name in REQUIRED_SOURCE_FILES:
        path = root / name
        if not path.is_file() or not path.resolve().is_relative_to(root) or not path.stat().st_size:
            raise ValueError(f'Invalid required source file: {name}')
    release = json.loads((root / '.titan/release.json').read_text())
    validate_release(release)
    provenance = json.loads((root / '.titan/imported-source.json').read_text())
    if (release.get('upstreamCommit') != provenance.get('upstreamCommit')
            or release.get('upstreamTag') != provenance.get('upstreamTag')
            or release.get('upstreamArchiveSha256') != provenance.get('archiveSha256')):
        raise ValueError('The retained upstream provenance does not match the release metadata')
    print(f'Verified complete TitanOS {release["version"]} checkout: {len(names)} tracked files')


if __name__ == '__main__':
    verify_source(sys.argv[1] if len(sys.argv) == 2 else Path(__file__).resolve().parents[1])
