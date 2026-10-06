#!/usr/bin/env python3
"""Fixed main-process admission for Debian's Docker and libvirt daemons.

The offline check runs before exec in this same PID, preserving systemd's
socket-activation descriptors and notify identity. Exit 78 is reserved for an
offline guard denial; only a separate fresh insufficient-memory proof can
authorize Titan's restricted management mode.
"""
import json
import os
from pathlib import Path
import re
import shlex
import stat
import subprocess
import sys
import time

GUARD = Path('/usr/share/titan/boot-memory-guard.py')
REPORT = Path('/run/titan-boot-memory.json')
RUN = Path('/run')
BOOT_ID = Path('/proc/sys/kernel/random/boot_id')
BLOCKED = 78
INVALID = 77
CHECK_FAILED = 75
EXEC_FAILED = 74
DOCKER = ('/usr/sbin/dockerd', '-H', 'fd://', '--containerd=/run/containerd/containerd.sock')
LIBVIRT = '/usr/sbin/libvirtd'
WRAPPER = '/usr/bin/python3 -I /usr/share/titan/boot-daemon-guard.py'


def daemon_arguments(component, arguments):
    if not isinstance(arguments, (tuple, list)) or any(type(arg) is not str or '\0' in arg for arg in arguments):
        raise ValueError('Invalid daemon arguments')
    if component == 'docker':
        if tuple(arguments) != DOCKER:
            raise ValueError('Unsupported Docker command')
        return list(arguments)
    if component != 'vms' or not arguments or arguments[0] != LIBVIRT:
        raise ValueError('Unsupported daemon')
    remaining = list(arguments[1:])
    seen = set()
    while remaining:
        flag = remaining.pop(0)
        if flag in ('--listen', '-l', '--verbose', '-v'):
            key = 'listen' if flag in ('--listen', '-l') else 'verbose'
        elif flag in ('--timeout', '-t'):
            key = 'timeout'
            if not remaining or not re.fullmatch(r'[0-9]{1,5}', remaining.pop(0)):
                raise ValueError('Invalid libvirt timeout')
        elif re.fullmatch(r'--timeout=[0-9]{1,5}', flag):
            key = 'timeout'
        else:
            raise ValueError('Unsupported libvirt option')
        if key in seen:
            raise ValueError('Duplicate libvirt option')
        seen.add(key)
    return list(arguments)


def trusted_file(path, executable=False):
    info = Path(path).lstat()
    return (stat.S_ISREG(info.st_mode) and info.st_uid == 0 and not info.st_mode & 0o022
            and (not executable or bool(info.st_mode & 0o111)))


def run_directory():
    descriptor = os.open(RUN, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    info = os.fstat(descriptor)
    if info.st_uid != 0 or info.st_mode & 0o022:
        os.close(descriptor)
        raise ValueError('Untrusted runtime directory')
    return descriptor


def marker_name(component):
    if component not in ('docker', 'vms'):
        raise ValueError('Unsupported component')
    return 'titan-boot-daemon-' + component + '.json'


def clear_marker(component):
    """Remove old evidence before argument, program or guard checks."""
    if os.geteuid() != 0:
        raise ValueError('System rights required')
    descriptor = run_directory()
    try:
        try:
            os.unlink(marker_name(component), dir_fd=descriptor)
            os.fsync(descriptor)
        except FileNotFoundError:
            pass
    finally:
        os.close(descriptor)


def fingerprint(info):
    return (info.st_dev, info.st_ino, info.st_size, info.st_mtime_ns, info.st_ctime_ns)


def denial_reason(previous, started):
    """Require a newly replaced trusted guard report, never a cached failure."""
    descriptor = os.open(REPORT, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    try:
        before = os.fstat(descriptor)
        if (not stat.S_ISREG(before.st_mode) or before.st_uid != 0 or before.st_mode & 0o022 or
                not 0 < before.st_size <= 16384 or fingerprint(before) == previous):
            raise ValueError('Untrusted guard report')
        with os.fdopen(descriptor, 'rb', closefd=False) as stream:
            raw = stream.read(16385)
        after = os.fstat(descriptor)
        if (fingerprint(before) != fingerprint(after) or fingerprint(after) != fingerprint(REPORT.lstat()) or
                len(raw) != before.st_size):
            raise ValueError('Guard report changed')
    finally:
        os.close(descriptor)
    def unique(items):
        result = {}
        for key, value in items:
            if key in result:
                raise ValueError('Duplicate report key')
            result[key] = value
        return result
    value = json.loads(raw, object_pairs_hook=unique,
                       parse_constant=lambda _: (_ for _ in ()).throw(ValueError('Invalid report number')))
    now = time.time()
    if (not isinstance(value, dict) or value.get('ok') is not False or
            value.get('reason') not in ('insufficient_memory', 'invalid_state', 'unknown_memory') or
            type(value.get('checked_at')) is not int or not started - 1 <= value['checked_at'] <= now + 1 or
            not started - 1 <= before.st_mtime <= now + 1):
        raise ValueError('Invalid fresh denial')
    return value['reason']


def write_denial(component, reason):
    boot = BOOT_ID.read_text().strip()
    if not re.fullmatch(r'[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}', boot):
        raise ValueError('Invalid boot identity')
    value = {'format': 'titan-boot-daemon-denial-v1', 'component': component,
             'boot_id': boot, 'reason': reason,
             'denied_monotonic_us': time.clock_gettime_ns(time.CLOCK_MONOTONIC) // 1000}
    if value['denied_monotonic_us'] <= 0:
        raise ValueError('Invalid monotonic time')
    body = (json.dumps(value, separators=(',', ':'), allow_nan=False) + '\n').encode()
    descriptor = run_directory()
    temporary = '.titan-boot-daemon-' + component + '-' + os.urandom(16).hex()
    created = False
    try:
        output = os.open(temporary, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600,
                         dir_fd=descriptor)
        created = True
        with os.fdopen(output, 'wb') as stream:
            os.fchmod(stream.fileno(), 0o600)
            stream.write(body)
            stream.flush()
            os.fsync(stream.fileno())
        os.replace(temporary, marker_name(component), src_dir_fd=descriptor, dst_dir_fd=descriptor)
        os.fsync(descriptor)
    finally:
        if created:
            try:
                os.unlink(temporary, dir_fd=descriptor)
            except FileNotFoundError:
                pass
        os.close(descriptor)


def execute(component, arguments):
    try:
        clear_marker(component)
    except (OSError, ValueError, TypeError):
        return CHECK_FAILED
    try:
        command = daemon_arguments(component, arguments)
        if os.geteuid() != 0 or not trusted_file(command[0], True) or not trusted_file(GUARD):
            return INVALID
    except (OSError, ValueError, TypeError):
        return INVALID
    try:
        try:
            previous = fingerprint(REPORT.lstat())
        except FileNotFoundError:
            previous = None
        started = time.time()
        result = subprocess.run(['/usr/bin/python3', '-I', str(GUARD), '--component', 'all'],
                                stdin=subprocess.DEVNULL, timeout=15, check=False)
    except (OSError, subprocess.TimeoutExpired):
        return CHECK_FAILED
    if result.returncode == 1:
        try:
            write_denial(component, denial_reason(previous, started))
        except (OSError, ValueError, TypeError, UnicodeError, RecursionError):
            return CHECK_FAILED
        return BLOCKED
    if result.returncode != 0:
        return CHECK_FAILED
    # A replaced/removed executable must not become an authorized guard exit.
    try:
        if not trusted_file(command[0], True):
            return INVALID
        os.execv(command[0], command)
    except OSError:
        return EXEC_FAILED
    return EXEC_FAILED  # exec never returns during normal execution.


def packaged_start(component, text):
    """Preserve only supported vendor ExecStart lines, including libvirt env."""
    section, commands = '', []
    for raw in text.splitlines():
        line = raw.strip()
        if not line or line.startswith(('#', ';')):
            continue
        if line.startswith('['):
            section = line
        elif section == '[Service]' and line.startswith('ExecStart='):
            commands.append(line[len('ExecStart='):])
    if len(commands) != 1 or '\\' in commands[0] or '\n' in commands[0]:
        raise ValueError('Unsupported packaged ExecStart')
    command = commands[0]
    if component == 'vms' and command == LIBVIRT + ' $LIBVIRTD_ARGS':
        return command
    parsed = shlex.split(command)
    daemon_arguments(component, parsed)
    # No variable expansion, command prefixes, shell or systemd specifiers.
    if command != ' '.join(parsed) or any(char in command for char in '$%;`'):
        raise ValueError('Unsupported packaged command expansion')
    return command


def dropin(component, command):
    return ('[Unit]\nRequires=titan-firstboot.service\nAfter=titan-firstboot.service\n'
            '[Service]\nExecStart=\nExecStart=' + WRAPPER + ' --component ' + component +
            ' -- ' + command + '\nRestartPreventExitStatus=78\n')


def install_dropins(unit_root=Path('/usr/lib/systemd/system')):
    """Image builder only: validate both vendor commands before any mutation."""
    if os.geteuid() != 0:
        raise ValueError('System rights required')
    prepared = []
    for service, component in (('docker', 'docker'), ('libvirtd', 'vms')):
        unit = unit_root / (service + '.service')
        if not trusted_file(unit) or not 0 < unit.stat().st_size <= 65536:
            raise ValueError('Invalid packaged daemon unit')
        command = packaged_start(component, unit.read_text())
        daemon_arguments(component, [LIBVIRT] if '$LIBVIRTD_ARGS' in command else shlex.split(command))
        prepared.append((unit_root / (service + '.service.d') / 'titan-memory.conf', dropin(component, command)))
    for target, text in prepared:
        target.parent.mkdir(mode=0o755, exist_ok=True)
        if target.exists() or target.is_symlink():
            if not trusted_file(target):
                raise ValueError('Invalid previous Titan dropin')
        # Replaces precisely the old Titan ExecStartPre dropin. Vendor pre-start
        # commands and every other packaged directive remain untouched.
        target.write_text(text)
        target.chmod(0o644)


def main(arguments=None):
    arguments = list(sys.argv[1:] if arguments is None else arguments)
    if arguments == ['--install-dropins']:
        try:
            install_dropins()
            return 0
        except (OSError, ValueError):
            print('Titan: unsupported packaged daemon configuration.', file=sys.stderr)
            return INVALID
    if len(arguments) < 4 or arguments[0] != '--component' or arguments[2] != '--':
        if len(arguments) >= 2 and arguments[0] == '--component' and arguments[1] in ('docker', 'vms'):
            try:
                clear_marker(arguments[1])
            except (OSError, ValueError, TypeError):
                return CHECK_FAILED
        return INVALID
    return execute(arguments[1], arguments[3:])


if __name__ == '__main__':
    sys.exit(main())
