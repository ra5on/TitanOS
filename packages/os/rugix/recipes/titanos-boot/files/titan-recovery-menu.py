#!/usr/bin/env python3
"""Publish validated Titan presentation data without altering Rugix boot state."""
import argparse
import contextlib
import json
import os
from pathlib import Path
import re
import subprocess
import tempfile

CONFIG = Path('/run/rugix/mounts/config')
LOADER = Path('/etc/titan/first.grub.cfg')
VERSION = re.compile(r'(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)')


def menu_values(metadata):
    if not isinstance(metadata, dict) or set(metadata) != {'versions', 'previous'}:
        raise ValueError('Invalid Titan boot menu metadata')
    versions = metadata['versions']
    if not isinstance(versions, dict) or not versions or set(versions) - {'a', 'b'}:
        raise ValueError('Invalid Titan boot menu slots')
    for version in versions.values():
        if not isinstance(version, str) or len(version) > 40 or not VERSION.fullmatch(version):
            raise ValueError('Invalid Titan boot menu version')
    values = {f'titan_{group}_version': version for group, version in versions.items()}
    previous = metadata['previous']
    if previous is not None:
        if (not isinstance(previous, dict) or previous.get('slot') not in versions
                or previous.get('version') != versions[previous['slot']] or len(versions) != 2):
            raise ValueError('Invalid Titan previous boot menu slot')
        values.update(titan_rollback_part='2' if previous['slot'] == 'a' else '3',
                      titan_rollback_version=previous['version'])
    return values


def encode_environment(values):
    encoded = '# GRUB Environment Block\n' + ''.join(f'{key}={value}\n' for key, value in sorted(values.items()))
    data = encoded.encode('ascii')
    if len(data) > 1024:
        raise ValueError('Titan boot environment is too large')
    return data + b'#' * (1024 - len(data))


def atomic_write(path, contents):
    fd, temporary = tempfile.mkstemp(prefix='.titan-menu-', dir=path.parent)
    try:
        with os.fdopen(fd, 'wb') as stream:
            stream.write(contents)
            stream.flush()
            os.fsync(stream.fileno())
        os.replace(temporary, path)
        descriptor = os.open(path.parent, os.O_RDONLY | os.O_DIRECTORY)
        try:
            os.fsync(descriptor)
        finally:
            os.close(descriptor)
    finally:
        with contextlib.suppress(FileNotFoundError):
            os.unlink(temporary)


def sync_recovery_menu(metadata):
    # Validate before making the EFI partition writable. No payload supplied by
    # users is ever executed as a GRUB script; only strict versions are encoded.
    contents = encode_environment(menu_values(metadata))
    if os.geteuid() != 0:
        raise PermissionError('Titan boot menu publication requires root')
    mounted = json.loads(subprocess.check_output(
        ['findmnt', '--json', '--mountpoint', str(CONFIG), '--output', 'TARGET,FSTYPE,OPTIONS'], text=True))
    filesystems = mounted.get('filesystems', [])
    if (len(filesystems) != 1 or filesystems[0].get('target') != str(CONFIG)
            or filesystems[0].get('fstype') != 'vfat' or CONFIG.is_symlink()):
        raise ValueError('The native Rugix EFI configuration partition is not mounted')
    target = CONFIG / 'rugpi'
    if not target.is_dir() or target.is_symlink() or not (target / 'primary.grubenv').is_file():
        raise ValueError('The native Rugix EFI boot state is missing')
    loader = LOADER.read_bytes()
    if not loader.startswith(b'# Titan recovery menu.') or len(loader) > 65536:
        raise ValueError('The installed Titan boot loader is invalid')
    read_only = 'ro' in filesystems[0].get('options', '').split(',')
    if read_only:
        subprocess.run(['mount', '-o', 'remount,rw', str(CONFIG)], check=True, timeout=15)
    try:
        # Installing the new first stage is also necessary for upgrades from
        # releases whose EFI partition still has the original Rugix script.
        atomic_write(target / 'titan-menu.grubenv', contents)
        if (target / 'grub.cfg').read_bytes() != loader:
            atomic_write(target / 'grub.cfg', loader)
    finally:
        if read_only:
            subprocess.run(['mount', '-o', 'remount,ro', str(CONFIG)], check=True, timeout=15)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--metadata', required=True)
    arguments = parser.parse_args()
    sync_recovery_menu(json.loads(arguments.metadata))


if __name__ == '__main__':
    main()
