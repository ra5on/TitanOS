#!/usr/bin/env python3
"""Check persisted autostart RAM before either container/VM daemon can start.

This reads only bounded, trusted metadata. In particular it never connects to
Docker or libvirt: doing so here could activate the daemon this check protects.
Swap is not physical RAM and cannot enlarge the future boot budget.
"""
import argparse
import json
import math
import os
from pathlib import Path
import re
import stat
import sys
import tempfile
import time
from xml.etree import ElementTree as ET

sys.path.insert(0, '/usr/lib/titan')
from titan.app_memory import GIB, MIB, system_reserve, vm_overhead
from titan.app_devices import raw_usb_container_ready
from titan.core import Error

MAX_FILE = 1024 * 1024
MAX_METADATA = 16 * MAX_FILE
MAX_CONTAINERS = 512
MAX_VMS = 256
MAX_MEMORY = 2 ** 63 - 1
CONTAINER_ID = re.compile(r'[a-f0-9]{64}')
UUID = re.compile(r'[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}')
MEMORY_SCALES = {'b': 1, 'bytes': 1, 'kb': 1000, 'kilobytes': 1000,
                 'k': 1024, 'kib': 1024, 'kibibytes': 1024,
                 'mb': 1000 ** 2, 'megabytes': 1000 ** 2, 'm': MIB,
                 'mib': MIB, 'mebibytes': MIB, 'gb': 1000 ** 3,
                 'gigabytes': 1000 ** 3, 'g': GIB, 'gib': GIB,
                 'gibibytes': GIB, 'tb': 1000 ** 4, 'terabytes': 1000 ** 4,
                 't': 1024 ** 4, 'tib': 1024 ** 4, 'tebibytes': 1024 ** 4}


class InvalidState(Exception):
    """No raw metadata, filenames or secrets belong in user-facing errors."""


def _trusted(info, owner, directory=False):
    valid_type = stat.S_ISDIR(info.st_mode) if directory else stat.S_ISREG(info.st_mode)
    if not valid_type or info.st_uid != owner or info.st_mode & 0o022:
        raise InvalidState()


def _directory(path, owner, parent=None):
    try:
        descriptor = os.open(path, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW, dir_fd=parent)
    except FileNotFoundError:
        return None
    except OSError:
        raise InvalidState() from None
    try:
        _trusted(os.fstat(descriptor), owner, directory=True)
    except BaseException:
        os.close(descriptor)
        raise
    return descriptor


class Metadata:
    def __init__(self, owner):
        self.owner = owner
        self.bytes = 0

    def read(self, path, parent=None, optional=False):
        try:
            descriptor = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK, dir_fd=parent)
        except FileNotFoundError:
            if optional:
                return None
            raise InvalidState() from None
        except OSError:
            raise InvalidState() from None
        try:
            before = os.fstat(descriptor)
            _trusted(before, self.owner)
            if not 0 < before.st_size <= MAX_FILE or self.bytes + before.st_size > MAX_METADATA:
                raise InvalidState()
            with os.fdopen(descriptor, 'rb', closefd=False) as stream:
                raw = stream.read(MAX_FILE + 1)
            after = os.fstat(descriptor)
            current = os.stat(path, dir_fd=parent, follow_symlinks=False)
            if (before.st_dev, before.st_ino, before.st_size, before.st_mtime_ns, before.st_ctime_ns) != (
                    after.st_dev, after.st_ino, after.st_size, after.st_mtime_ns, after.st_ctime_ns) or (
                    after.st_dev, after.st_ino, after.st_size, after.st_mtime_ns, after.st_ctime_ns) != (
                    current.st_dev, current.st_ino, current.st_size, current.st_mtime_ns, current.st_ctime_ns) or len(raw) != before.st_size:
                raise InvalidState()
            self.bytes += len(raw)
            return raw
        finally:
            os.close(descriptor)

    def json(self, path, parent=None, optional=False):
        raw = self.read(path, parent=parent, optional=optional)
        if raw is None:
            return None
        def unique(items):
            result = {}
            for key, value in items:
                if key in result:
                    raise InvalidState()
                result[key] = value
            return result
        try:
            value = json.loads(raw, object_pairs_hook=unique,
                               parse_constant=lambda _: (_ for _ in ()).throw(InvalidState()))
        except (ValueError, UnicodeError, RecursionError):
            raise InvalidState() from None
        if not isinstance(value, dict):
            raise InvalidState()
        return value


def _entries(descriptor, maximum):
    result = []
    with os.scandir(descriptor) as entries:
        for entry in entries:
            if len(result) >= maximum:
                raise InvalidState()
            result.append(entry.name)
    return sorted(result)


def docker_budget(root, metadata, daemon_config=None):
    """Conservatively count every future daemon restart, including foreign ones."""
    if daemon_config is not None:
        config = metadata.json(daemon_config, optional=True)
        if config is not None and config.get('data-root', '/var/lib/docker') != '/var/lib/docker':
            # Titan's supported fixed storage root must not silently miss a
            # foreign/customized Docker metadata tree.
            raise InvalidState()
    descriptor = _directory(root, metadata.owner)
    if descriptor is None:
        return {'count': 0, 'limit_bytes': 0}
    total = count = 0
    try:
        for identifier in _entries(descriptor, MAX_CONTAINERS):
            if not CONTAINER_ID.fullmatch(identifier):
                raise InvalidState()
            member = _directory(identifier, metadata.owner, descriptor)
            if member is None:
                raise InvalidState()
            try:
                host = metadata.json('hostconfig.json', member)
                policy = host.get('RestartPolicy')
                if not isinstance(policy, dict) or policy.get('Name') not in ('', 'no', 'on-failure', 'always', 'unless-stopped'):
                    raise InvalidState()
                if policy['Name'] in ('', 'no'):
                    continue
                config = metadata.json('config.v2.json', member)
                if config.get('ID') != identifier or not isinstance(config.get('State'), dict):
                    raise InvalidState()
                # Include on-failure too: restore behavior can depend on the
                # Engine version and persisted exit state after daemon failure.
                candidate = policy['Name'] in ('always', 'on-failure')
                if policy['Name'] == 'unless-stopped':
                    state = config['State']
                    flags = [state.get(key, False) for key in ('Running', 'Paused', 'Restarting')]
                    if any(type(flag) is not bool for flag in flags):
                        raise InvalidState()
                    stopped = config.get('HasBeenManuallyStopped')
                    started = config.get('HasBeenStartedBefore')
                    if stopped is not None and type(stopped) is not bool or started is not None and type(started) is not bool:
                        raise InvalidState()
                    # A clean daemon shutdown makes Running=false without a
                    # manual stop. Persisted stop/start flags distinguish this
                    # from a deliberately disabled service. Missing old flags
                    # are counted conservatively, never invented as zero.
                    candidate = any(flags) or not (stopped is True or started is False)
                if candidate:
                    try:
                        raw_usb_container_ready({'HostConfig': host, 'Config': config.get('Config') or {}})
                    except Error:
                        raise InvalidState() from None
                    ceiling = host.get('Memory')
                    if type(ceiling) is not int or not 0 < ceiling <= MAX_MEMORY:
                        raise InvalidState()
                    total += ceiling
                    if total > MAX_MEMORY:
                        raise InvalidState()
                    count += 1
            finally:
                os.close(member)
    finally:
        os.close(descriptor)
    return {'count': count, 'limit_bytes': total}


def vm_budget(root, metadata):
    """Resolve only standard libvirt autostart links inside its definition dir."""
    descriptor = _directory(root, metadata.owner)
    if descriptor is None:
        return {'count': 0, 'limit_bytes': 0}
    autostart = None
    total = count = 0
    seen = set()
    try:
        autostart = _directory('autostart', metadata.owner, descriptor)
        if autostart is None:
            return {'count': 0, 'limit_bytes': 0}
        for name in _entries(autostart, MAX_VMS):
            if not name.endswith('.xml') or name in ('.xml', '..xml') or len(name.encode()) > 255:
                raise InvalidState()
            info = os.stat(name, dir_fd=autostart, follow_symlinks=False)
            if not stat.S_ISLNK(info.st_mode) or info.st_uid != metadata.owner:
                raise InvalidState()
            target = os.readlink(name, dir_fd=autostart)
            # libvirt uses absolute links; its normal relative counterpart is
            # also accepted. Never follow an external path or nested symlink.
            if target not in (str(Path(root) / name), '../' + name):
                raise InvalidState()
            try:
                xml = metadata.read(name, descriptor).decode('utf-8')
                if '<!DOCTYPE' in xml.upper() or '<!ENTITY' in xml.upper():
                    raise InvalidState()
                domain = ET.fromstring(xml)
            except (ET.ParseError, ValueError, UnicodeError, LookupError, RecursionError):
                raise InvalidState() from None
            identifier = domain.findtext('uuid', '').lower()
            memory = domain.find('memory')
            if (domain.tag != 'domain' or not UUID.fullmatch(identifier) or identifier in seen or
                    len(domain.findall('uuid')) != 1 or len(domain.findall('memory')) != 1 or memory is None):
                raise InvalidState()
            scale = MEMORY_SCALES.get(memory.get('unit', 'KiB').lower())
            value = (memory.text or '').strip()
            if scale is None or not re.fullmatch(r'[0-9]{1,18}', value):
                raise InvalidState()
            assigned = int(value) * scale
            if not 0 < assigned <= MAX_MEMORY:
                raise InvalidState()
            total += assigned + vm_overhead(assigned)
            if total > MAX_MEMORY:
                raise InvalidState()
            count += 1
            seen.add(identifier)
    finally:
        if autostart is not None:
            os.close(autostart)
        os.close(descriptor)
    return {'count': count, 'limit_bytes': total}


def physical_memory(path='/proc/meminfo'):
    try:
        with open(path, 'rb') as stream:
            raw = stream.read(65537)
        if len(raw) > 65536:
            return 0
        matches = re.findall(rb'^MemTotal:\s+([0-9]{1,18}) kB\s*$', raw, re.M)
        value = int(matches[0]) * 1024 if len(matches) == 1 else 0
        return value if 0 < value <= MAX_MEMORY else 0
    except (OSError, ValueError):
        return 0


def evaluate(*, meminfo='/proc/meminfo', docker_root='/var/lib/docker/containers',
             vm_root='/etc/libvirt/qemu', daemon_config='/etc/docker/daemon.json', owner=0):
    total = physical_memory(meminfo)
    reserve = system_reserve(total) if total else 0
    result = {'ok': False, 'total_bytes': total, 'reserve_bytes': reserve,
              'required_bytes': reserve, 'reason': 'unknown_memory',
              'components': {}, 'checked_at': int(time.time())}
    if not total:
        return result
    metadata = Metadata(owner)
    try:
        for name, callback, directory in (('docker', docker_budget, docker_root), ('vms', vm_budget, vm_root)):
            budget = callback(directory, metadata, daemon_config) if name == 'docker' else callback(directory, metadata)
            result['components'][name] = budget
            required = result['required_bytes'] + budget['limit_bytes']
            result['required_bytes'] = min(required, MAX_MEMORY)
            if required > MAX_MEMORY:
                raise InvalidState()
    except (InvalidState, OSError, ValueError, OverflowError):
        result['reason'] = 'invalid_state'
        return result
    result['ok'] = result['required_bytes'] <= total
    result['reason'] = '' if result['ok'] else 'insufficient_memory'
    return result


def write_report(result, path='/run/titan-boot-memory.json', owner=0):
    path = Path(path)
    descriptor = _directory(path.parent, owner)
    if descriptor is None:
        raise InvalidState()
    temporary = None
    try:
        body = (json.dumps(result, separators=(',', ':'), allow_nan=False) + '\n').encode()
        if len(body) > 16384:
            raise InvalidState()
        fd, temporary = tempfile.mkstemp(prefix='.titan-boot-memory-', dir=path.parent)
        with os.fdopen(fd, 'wb') as stream:
            os.fchmod(stream.fileno(), 0o644)
            stream.write(body)
            stream.flush()
            os.fsync(stream.fileno())
        os.replace(temporary, path)
        os.fsync(descriptor)
    finally:
        if temporary is not None:
            try:
                os.unlink(temporary)
            except FileNotFoundError:
                pass
        os.close(descriptor)


def failure_message(result):
    if result['reason'] == 'insufficient_memory':
        required = math.ceil(result['required_bytes'] / GIB)
        return (f'RAM-Schutz: Für die automatisch startenden Dienste und Titan werden mindestens {required} GiB '
                f'RAM benötigt; vorhanden sind {result["total_bytes"] / GIB:.1f} GiB. '
                'RAM in Proxmox wieder erhöhen oder bei laufendem Dienst Autostart und RAM-Grenzen anpassen. '
                'Die Weboberfläche und der Dateimanager bleiben verfügbar. Swap zählt nicht als RAM-Kapazität.')
    return ('RAM-Schutz: Das Autostart-Budget kann nicht sicher geprüft werden. Docker und virtuelle Maschinen '
            'bleiben angehalten; die Weboberfläche und der Dateimanager bleiben verfügbar. '
            'RAM-Kapazität und bei laufendem Dienst die Autostart- und RAM-Einstellungen prüfen.')


def main(argv=None):
    parser = argparse.ArgumentParser(description='Titan Autostart-RAM-Schutz')
    parser.add_argument('--component', choices=('docker', 'vms', 'all'), default='all')
    parser.parse_args(argv)
    if os.geteuid() != 0:
        print('RAM-Schutz: Die Prüfung benötigt Systemrechte.', file=sys.stderr)
        return 1
    result = evaluate()
    try:
        write_report(result)
    except (InvalidState, OSError, ValueError, TypeError):
        print('RAM-Schutz: Der Prüfstatus konnte nicht sicher gespeichert werden; Dienststart angehalten.', file=sys.stderr)
        return 1
    if not result['ok']:
        print(failure_message(result), file=sys.stderr)
        return 1
    return 0


if __name__ == '__main__':
    sys.exit(main())
