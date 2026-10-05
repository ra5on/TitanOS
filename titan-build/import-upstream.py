#!/usr/bin/env python3
"""Bootstrap the complete, pinned Titan source without losing migration seed files.

The initial TitanOS checkout contains our new controls and selected source
changes. Import the last complete public Titan tree, not pristine Umbrel, then
restore every tracked seed file before applying the idempotent fork helpers.
Only Git-tracked source is fetched and staged; private working data is excluded.
"""
from __future__ import annotations

import json
import os
from pathlib import Path, PurePosixPath
import shutil
import stat
import subprocess
import sys
import tarfile
import tempfile

TARGET_REPOSITORY = 'ra5on/TitanOS'
SOURCE_REPOSITORY = 'https://github.com/ra5on/Titan.git'
SOURCE_COMMIT = '8446de77a70ee8f72a256cf9082266435d1d1a17'
SOURCE_TREE = '0258437695140f1c2cefb9f225a0805b7a84a438'
MINIMUM_SOURCE_FILES = 2200
REQUIRED_SOURCE_FILES = (
    'package.json', 'LICENSE.md', 'UPSTREAM.md', 'README.upstream.md',
    '.titan/imported-source.json',
    'packages/umbreld/package.json', 'packages/umbreld/source/index.ts',
    'packages/ui/package.json', 'packages/os/build.sh',
    'packages/os/umbrelos.Dockerfile',
    'packages/os/rugix/recipes/setup-rugix/files/bootstrapping.toml',
)
FORBIDDEN_PARTS = {'.git', '.secrets', 'node_modules', '__pycache__'}
FORBIDDEN_ROOTS = {'worktrees', 'dist'}


def git(root: Path, *args: str) -> str:
    return subprocess.check_output(['git', '-C', str(root), *args], text=True).strip()


def tracked_files(root: Path) -> list[str]:
    value = subprocess.check_output(['git', '-C', str(root), 'ls-files', '-z'])
    return [name.decode('utf-8') for name in value.split(b'\0') if name]


def safe_path(name: str) -> PurePosixPath:
    path = PurePosixPath(name)
    if (not name or path.is_absolute() or '\\' in name or '\x00' in name
            or any(part in {'', '.', '..'} for part in name.split('/'))
            or any(part.lower() in FORBIDDEN_PARTS for part in path.parts)
            or path.parts[0] in FORBIDDEN_ROOTS):
        raise ValueError(f'Unsafe source path: {name!r}')
    return path


def safe_link(path: PurePosixPath, target: str) -> None:
    link = PurePosixPath(target)
    if not target or link.is_absolute() or '\\' in target or '\x00' in target:
        raise ValueError(f'Unsafe source symlink: {path}')
    parts = list(path.parent.parts)
    for part in link.parts:
        if part == '..':
            if not parts:
                raise ValueError(f'Source symlink escapes checkout: {path}')
            parts.pop()
        elif part != '.':
            parts.append(part)
    if any(part.lower() in FORBIDDEN_PARTS for part in parts) or (parts and parts[0] in FORBIDDEN_ROOTS):
        raise ValueError(f'Source symlink targets private working data: {path}')


def extract_source(archive: Path, destination: Path) -> list[str]:
    with tarfile.open(archive) as source:
        members = source.getmembers()
        seen = set()
        paths = []
        for member in members:
            # Git archive emits directory names with a trailing slash.
            name = member.name.rstrip('/') if member.isdir() else member.name
            path = safe_path(name)
            if name in seen:
                raise ValueError(f'Duplicate source archive path: {name}')
            seen.add(name)
            if member.issym():
                safe_link(path, member.linkname)
            elif not (member.isfile() or member.isdir()):
                raise ValueError(f'Unsupported source archive entry: {name}')
            if not member.isdir():
                paths.append(name)
        # Validate the entire archive before extracting any member. The data
        # filter also prevents writes through an escaping ancestor symlink.
        source.extractall(destination, members=members, filter='data')
    return paths


def copy_source_file(source: Path, destination: Path) -> None:
    destination.parent.mkdir(parents=True, exist_ok=True)
    if destination.is_symlink() or destination.is_file():
        destination.unlink()
    if source.is_symlink():
        destination.symlink_to(os.readlink(source))
    elif source.is_file():
        shutil.copy2(source, destination)
    else:
        raise ValueError(f'Source is not a regular file or symlink: {source}')


def source_complete(root: Path, minimum: int = MINIMUM_SOURCE_FILES) -> bool:
    return (all((root / name).is_file() for name in REQUIRED_SOURCE_FILES)
            and len(tracked_files(root)) >= minimum)


def fetch_source(destination: Path, repository: str, commit: str, tree: str) -> tuple[Path, list[str]]:
    clone = destination / 'git'
    clone.mkdir()
    subprocess.run(['git', 'init', '--quiet', str(clone)], check=True)
    subprocess.run(['git', '-C', str(clone), 'fetch', '--quiet', '--no-tags', '--depth=1', repository, commit], check=True)
    if git(clone, 'rev-parse', 'FETCH_HEAD^{commit}') != commit or git(clone, 'rev-parse', 'FETCH_HEAD^{tree}') != tree:
        raise ValueError('Pinned Titan source commit or tree does not match')
    archive = destination / 'source.tar'
    with archive.open('wb') as output:
        subprocess.run(['git', '-C', str(clone), 'archive', '--format=tar', commit], stdout=output, check=True)
    extracted = destination / 'source'
    extracted.mkdir()
    return extracted, extract_source(archive, extracted)


def bootstrap(root: Path, repository: str = SOURCE_REPOSITORY,
              commit: str = SOURCE_COMMIT, tree: str = SOURCE_TREE,
              minimum: int = MINIMUM_SOURCE_FILES) -> list[str]:
    if source_complete(root, minimum):
        return []
    seed_files = tracked_files(root)
    for name in seed_files:
        path = safe_path(name)
        source = root / name
        mode = source.lstat().st_mode
        if not (stat.S_ISREG(mode) or stat.S_ISLNK(mode)):
            raise ValueError(f'Unsupported seed file: {name}')
        if source.is_symlink():
            safe_link(path, os.readlink(source))
    with tempfile.TemporaryDirectory(prefix='titan-source-bootstrap-') as temporary:
        temporary = Path(temporary)
        source, imported_files = fetch_source(temporary, repository, commit, tree)
        if len(imported_files) < minimum or not all((source / name).is_file() for name in REQUIRED_SOURCE_FILES):
            raise ValueError('Pinned Titan source is incomplete')
        if (source / 'LICENSE.md').read_bytes() != (root / 'LICENSE.md').read_bytes():
            raise ValueError('The complete upstream license must remain unchanged')
        if (source / '.titan/release-public.pem').read_bytes() != (root / '.titan/release-public.pem').read_bytes():
            raise ValueError('Migration must retain the existing Titan signing public key')
        seed = temporary / 'seed'
        for name in seed_files:
            copy_source_file(root / name, seed / name)
        for name in imported_files:
            destination = root / name
            # A checkout must never write through a pre-existing symlink into
            # working data or outside this repository.
            if not destination.parent.resolve().is_relative_to(root.resolve()):
                raise ValueError(f'Source destination escapes checkout: {name}')
            copy_source_file(source / name, destination)
        for name in seed_files:
            copy_source_file(seed / name, root / name)
        provenance = root / '.titan/source-bootstrap.json'
        if '.titan/source-bootstrap.json' not in seed_files:
            provenance.write_text(json.dumps({
                'sourceRepository': repository, 'sourceCommit': commit,
                'sourceTree': tree, 'seedCommit': git(root, 'rev-parse', 'HEAD'),
                'sourceFiles': len(imported_files), 'preservedSeedFiles': sorted(seed_files),
            }, indent=2) + '\n')
            imported_files.append('.titan/source-bootstrap.json')
    return imported_files


def configure_fork(root: Path) -> None:
    for name in ('prepare-cloud-auth.py', 'configure-branding.py', 'configure-updater.py'):
        subprocess.run([sys.executable, str(root / 'titan-build' / name), str(root)], check=True)


def persist_source_changes(root: Path, imported_files: list[str]) -> None:
    if os.environ.get('GITHUB_REPOSITORY') != TARGET_REPOSITORY:
        raise ValueError('Source publication is permitted only in ra5on/TitanOS')
    git(root, 'config', 'user.name', 'Titan Build')
    git(root, 'config', 'user.email', '115256194+ra5on@users.noreply.github.com')
    paths = sorted(set(tracked_files(root) + imported_files))
    # An explicit path list avoids staging ignored/untracked working data,
    # credentials, dependencies, or artifacts through a blanket --force add.
    with tempfile.NamedTemporaryFile(prefix='titan-source-paths-') as names:
        names.write(b''.join(name.encode('utf-8') + b'\0' for name in paths)); names.flush()
        subprocess.run(['git', '-C', str(root), '--literal-pathspecs', 'add', '--all', '--force',
                        f'--pathspec-from-file={names.name}', '--pathspec-file-nul'], check=True)
    changed = subprocess.run(['git', '-C', str(root), 'diff', '--cached', '--quiet'])
    if changed.returncode == 0:
        return
    if changed.returncode != 1:
        raise ValueError('Unable to inspect configured source changes')
    release = json.loads((root / '.titan/release.json').read_text())
    git(root, 'commit', '-m', f'Import and configure complete TitanOS {release["version"]} source')
    git(root, 'push', 'origin', 'HEAD:main')


def main() -> None:
    root = Path(__file__).resolve().parent.parent
    if os.environ.get('GITHUB_ACTIONS') == 'true' and os.environ.get('GITHUB_REPOSITORY') != TARGET_REPOSITORY:
        raise ValueError('Run this migration bootstrap only in ra5on/TitanOS')
    imported_files = bootstrap(root)
    release = json.loads((root / '.titan/release.json').read_text())
    provenance = json.loads((root / '.titan/imported-source.json').read_text())
    if provenance.get('upstreamCommit') != release.get('upstreamCommit'):
        raise ValueError('The complete source does not match the declared upstream baseline')
    configure_fork(root)
    if os.environ.get('GITHUB_ACTIONS') == 'true':
        persist_source_changes(root, imported_files)
    print(f'Complete Titan source ready at {git(root, "rev-parse", "HEAD")}; pinned fork and seed controls retained.')


if __name__ == '__main__':
    main()
