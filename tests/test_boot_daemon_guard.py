"""Main-process admission preserves systemd identity and leaves fresh evidence."""
import contextlib
import importlib.util
import json
import os
from pathlib import Path
import shutil
import stat
import subprocess
import tempfile
import time
from types import SimpleNamespace
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[1]
SPEC = importlib.util.spec_from_file_location('boot_daemon_guard', ROOT / 'image/boot-daemon-guard.py')
guard = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(guard)
BOOT = '12345678-1234-1234-1234-123456789abc'


def root_info(info):
    names = ('st_dev', 'st_ino', 'st_size', 'st_mtime_ns', 'st_ctime_ns', 'st_mtime', 'st_mode')
    return SimpleNamespace(st_uid=0, **{key: getattr(info, key) for key in names})


class BootDaemonGuardTests(unittest.TestCase):
    @contextlib.contextmanager
    def fixture(self):
        # Real file operations in an isolated directory; only test file owner
        # identities are promoted to root, never host programs or host state.
        with tempfile.TemporaryDirectory() as temporary, contextlib.ExitStack() as stack:
            directory = Path(temporary)
            report = directory / 'titan-boot-memory.json'
            boot = directory / 'boot-id'
            boot.write_text(BOOT + '\n')
            original_fstat, original_lstat = os.fstat, Path.lstat
            for key, value in (('RUN', directory), ('REPORT', report), ('BOOT_ID', boot)):
                stack.enter_context(patch.object(guard, key, value))
            stack.enter_context(patch.object(guard.os, 'geteuid', return_value=0))
            stack.enter_context(patch.object(guard.os, 'fstat', side_effect=lambda fd: root_info(original_fstat(fd))))
            stack.enter_context(patch.object(Path, 'lstat', side_effect=lambda path: root_info(original_lstat(path)), autospec=True))
            stack.enter_context(patch.object(guard, 'trusted_file', return_value=True))
            yield directory, report

    def fresh_report(self, report, reason='insufficient_memory', **overrides):
        data = {'ok': False, 'reason': reason, 'checked_at': int(time.time()),
                'total_bytes': 3 * 1024**3, 'reserve_bytes': 512 * 1024**2,
                'required_bytes': 5 * 1024**3, 'components': {}}
        data.update(overrides)
        temporary = report.with_suffix('.new')
        temporary.write_text(json.dumps(data))
        temporary.chmod(0o644)
        temporary.replace(report)

    def test_only_fixed_packaged_daemon_arguments_are_allowed(self):
        self.assertEqual(guard.daemon_arguments('docker', guard.DOCKER), list(guard.DOCKER))
        for args in ([guard.LIBVIRT], [guard.LIBVIRT, '--timeout', '120'],
                     [guard.LIBVIRT, '-t', '120', '--verbose'], [guard.LIBVIRT, '--timeout=120', '-l']):
            self.assertEqual(guard.daemon_arguments('vms', args), args)
        for component, args in (('docker', ['/bin/sh', '-c', 'dockerd']),
                                ('docker', [*guard.DOCKER, '--data-root=/home']),
                                ('vms', [guard.LIBVIRT, '--daemon']),
                                ('vms', [guard.LIBVIRT, '--timeout', '-1']),
                                ('vms', [guard.LIBVIRT, '-l', '--listen']),
                                ('vms', [guard.LIBVIRT, '--timeout=2', '-t', '3']),
                                ('vms', [guard.LIBVIRT, '$LIBVIRTD_ARGS']),
                                ('vms', [guard.LIBVIRT, '--config', '/tmp/conf']),
                                ('all', [guard.LIBVIRT])):
            with self.subTest(component=component, args=args), self.assertRaises(ValueError):
                guard.daemon_arguments(component, args)

    def test_guard_pass_execs_same_pid_exact_arguments_and_preserves_activation_environment(self):
        with self.fixture() as (directory, _), patch.object(guard.subprocess, 'run', return_value=SimpleNamespace(returncode=0)) as run, \
                patch.object(guard.os, 'execv') as execute, patch.dict(os.environ, {'LISTEN_PID': str(os.getpid()), 'LISTEN_FDS': '2', 'NOTIFY_SOCKET': '/run/notify'}):
            old = directory / guard.marker_name('docker')
            old.write_text('stale')
            self.assertEqual(guard.execute('docker', guard.DOCKER), guard.EXEC_FAILED)
            execute.assert_called_once_with(guard.DOCKER[0], list(guard.DOCKER))
            self.assertEqual(os.environ['LISTEN_PID'], str(os.getpid()))
            self.assertEqual(os.environ['LISTEN_FDS'], '2')
            self.assertEqual(os.environ['NOTIFY_SOCKET'], '/run/notify')
            self.assertFalse(old.exists())
            self.assertEqual(run.call_args.args[0], ['/usr/bin/python3', '-I', str(guard.GUARD), '--component', 'all'])
            self.assertEqual(run.call_args.kwargs['timeout'], 15)

    def test_each_denial_has_fresh_private_component_boot_and_monotonic_evidence(self):
        for component, args in (('docker', guard.DOCKER), ('vms', [guard.LIBVIRT])):
            for reason in ('insufficient_memory', 'invalid_state', 'unknown_memory'):
                with self.subTest(component=component, reason=reason), self.fixture() as (directory, report):
                    self.fresh_report(report)
                    old = directory / guard.marker_name(component)
                    old.write_text('stale')
                    start = time.clock_gettime_ns(time.CLOCK_MONOTONIC) // 1000
                    def check(*args, **kwargs):
                        self.assertFalse(old.exists())
                        self.fresh_report(report, reason)
                        return SimpleNamespace(returncode=1)
                    with patch.object(guard.subprocess, 'run', side_effect=check), patch.object(guard.os, 'execv') as execute:
                        self.assertEqual(guard.execute(component, args), 78)
                    value = json.loads(old.read_text())
                    self.assertEqual(set(value), {'format', 'component', 'boot_id', 'reason', 'denied_monotonic_us'})
                    self.assertEqual(value['format'], 'titan-boot-daemon-denial-v1')
                    self.assertEqual(value['component'], component)
                    self.assertEqual(value['boot_id'], BOOT)
                    self.assertEqual(value['reason'], reason)
                    self.assertIs(type(value['denied_monotonic_us']), int)
                    self.assertGreaterEqual(value['denied_monotonic_us'], start)
                    self.assertLessEqual(value['denied_monotonic_us'], time.clock_gettime_ns(time.CLOCK_MONOTONIC) // 1000)
                    self.assertEqual(stat.S_IMODE(old.stat().st_mode), 0o600)
                    self.assertFalse(list(directory.glob('.titan-boot-daemon-*')))
                    execute.assert_not_called()

    def test_invalid_command_or_missing_program_removes_old_marker_before_rejection(self):
        for invalid_args in (True, False):
            with self.subTest(invalid_args=invalid_args), self.fixture() as (directory, _):
                marker = directory / guard.marker_name('docker')
                marker.write_text('stale')
                with patch.object(guard, 'trusted_file', return_value=False), patch.object(guard.subprocess, 'run') as run:
                    code = guard.execute('docker', ['/bin/sh'] if invalid_args else guard.DOCKER)
                self.assertEqual(code, 77)
                self.assertFalse(marker.exists())
                run.assert_not_called()

    def test_malformed_invocation_also_removes_component_marker(self):
        with self.fixture() as (directory, _):
            marker = directory / guard.marker_name('vms')
            marker.write_text('stale')
            self.assertEqual(guard.main(['--component', 'vms', '--']), 77)
            self.assertFalse(marker.exists())

    def test_missing_unchanged_or_invalid_report_never_creates_guard_exit78(self):
        for case in ('missing', 'unchanged', 'stale', 'future', 'ok', 'unknown_reason', 'duplicate', 'oversize', 'symlink', 'writable'):
            with self.subTest(case=case), self.fixture() as (directory, report):
                if case == 'unchanged': self.fresh_report(report)
                def check(*args, **kwargs):
                    if case in ('missing', 'unchanged'): return SimpleNamespace(returncode=1)
                    self.fresh_report(report)
                    if case == 'stale': self.fresh_report(report, checked_at=1)
                    if case == 'future': self.fresh_report(report, checked_at=int(time.time()) + 60)
                    if case == 'ok': self.fresh_report(report, ok=True)
                    if case == 'unknown_reason': self.fresh_report(report, 'other')
                    if case == 'duplicate': report.write_text('{"ok":false,"ok":false}')
                    if case == 'oversize': report.write_text(' ' * 16385)
                    if case == 'writable': report.chmod(0o666)
                    if case == 'symlink':
                        target = report.with_suffix('.target')
                        report.replace(target)
                        report.symlink_to(target)
                    return SimpleNamespace(returncode=1)
                with patch.object(guard.subprocess, 'run', side_effect=check), patch.object(guard.os, 'execv') as execute:
                    self.assertEqual(guard.execute('docker', guard.DOCKER), 75)
                self.assertFalse((directory / guard.marker_name('docker')).exists())
                execute.assert_not_called()

    def test_guard_timeout_bad_exit_marker_failure_and_invalid_boot_identity_fail_closed(self):
        for case in ('timeout', 'error', 'bad_exit', 'marker_failure', 'boot_id'):
            with self.subTest(case=case), self.fixture() as (directory, report), contextlib.ExitStack() as stack:
                old = directory / guard.marker_name('vms')
                old.write_text('stale')
                def check(*args, **kwargs):
                    self.fresh_report(report)
                    if case == 'timeout': raise subprocess.TimeoutExpired('guard', 15)
                    if case == 'error': raise OSError('guard')
                    return SimpleNamespace(returncode=2 if case == 'bad_exit' else 1)
                if case == 'marker_failure': stack.enter_context(patch.object(guard, 'write_denial', side_effect=OSError()))
                if case == 'boot_id': guard.BOOT_ID.write_text('invalid')
                with patch.object(guard.subprocess, 'run', side_effect=check), patch.object(guard.os, 'execv') as execute:
                    self.assertEqual(guard.execute('vms', [guard.LIBVIRT]), 75)
                self.assertFalse(old.exists())
                execute.assert_not_called()

    def test_daemon_replaced_after_guard_is_rechecked_and_native_failure_leaves_no_marker(self):
        for replaced in (True, False):
            with self.subTest(replaced=replaced), self.fixture() as (directory, _), \
                    patch.object(guard.subprocess, 'run', return_value=SimpleNamespace(returncode=0)), \
                    patch.object(guard, 'trusted_file', side_effect=[True, True, not replaced]), \
                    patch.object(guard.os, 'execv', side_effect=OSError()) as execute:
                self.assertEqual(guard.execute('vms', [guard.LIBVIRT]), 77 if replaced else 74)
                self.assertEqual(execute.call_count, 0 if replaced else 1)
                self.assertFalse((directory / guard.marker_name('vms')).exists())

    def test_native_guard_binary_requires_root_owned_executable_regular_file(self):
        for mode, owner, allowed in ((stat.S_IFREG | 0o755, 0, True), (stat.S_IFREG | 0o755, 1000, False),
                                     (stat.S_IFREG | 0o775, 0, False), (stat.S_IFREG | 0o644, 0, False),
                                     (stat.S_IFLNK | 0o777, 0, False)):
            with self.subTest(mode=mode, owner=owner), patch.object(Path, 'lstat', return_value=SimpleNamespace(st_mode=mode, st_uid=owner)):
                self.assertEqual(guard.trusted_file('/fixed/program', True), allowed)

    def test_dropins_preserve_vendor_notify_activation_and_environment(self):
        docker = '[Service]\nType=notify\nExecStart=' + ' '.join(guard.DOCKER) + '\n'
        libvirt = '[Service]\nEnvironmentFile=-/etc/default/libvirtd\nType=notify\nExecStart=/usr/sbin/libvirtd $LIBVIRTD_ARGS\n'
        with self.fixture() as (directory, _):
            (directory / 'docker.service').write_text(docker)
            (directory / 'libvirtd.service').write_text(libvirt)
            prior = directory / 'docker.service.d/titan-memory.conf'
            prior.parent.mkdir()
            prior.write_text('[Service]\nExecStartPre=/old/titan-guard\n')
            guard.install_dropins(directory)
            self.assertEqual((directory / 'docker.service').read_text(), docker)
            self.assertEqual((directory / 'libvirtd.service').read_text(), libvirt)
            for service, component, command in (('docker', 'docker', ' '.join(guard.DOCKER)),
                                                ('libvirtd', 'vms', '/usr/sbin/libvirtd $LIBVIRTD_ARGS')):
                value = (directory / (service + '.service.d/titan-memory.conf')).read_text()
                self.assertIn('ExecStart=\nExecStart=' + guard.WRAPPER + ' --component ' + component + ' -- ' + command, value)
                self.assertIn('RestartPreventExitStatus=78', value)
                self.assertNotIn('ExecStartPre', value)
                self.assertNotIn('Type=', value)

    def test_unknown_vendor_command_fails_before_mutating_any_dropin(self):
        for command in ('/usr/sbin/dockerd -H unix:///tmp/docker.sock', '/bin/sh -c docker',
                        ' '.join(guard.DOCKER) + '; /bin/true', ' '.join(guard.DOCKER) + '\nExecStart=/bin/true'):
            with self.subTest(command=command), self.fixture() as (directory, _):
                (directory / 'docker.service').write_text('[Service]\nExecStart=' + command + '\n')
                (directory / 'libvirtd.service').write_text('[Service]\nExecStart=/usr/sbin/libvirtd $LIBVIRTD_ARGS\n')
                with self.assertRaises(ValueError): guard.install_dropins(directory)
                self.assertFalse((directory / 'docker.service.d').exists())

    def test_second_vendor_unit_is_validated_before_first_dropin_is_overwritten(self):
        with self.fixture() as (directory, _):
            (directory / 'docker.service').write_text('[Service]\nExecStart=' + ' '.join(guard.DOCKER) + '\n')
            (directory / 'libvirtd.service').write_text('[Service]\nExecStart=/usr/sbin/libvirtd --daemon\n')
            prior = directory / 'docker.service.d/titan-memory.conf'
            prior.parent.mkdir()
            prior.write_text('old guard, preserve until both units validated\n')
            with self.assertRaises(ValueError): guard.install_dropins(directory)
            self.assertEqual(prior.read_text(), 'old guard, preserve until both units validated\n')
            self.assertFalse((directory / 'libvirtd.service.d').exists())

    def test_foreign_or_writable_runtime_directory_cannot_leave_authorized_denial(self):
        for owner, mode in ((1000, 0o755), (0, 0o777)):
            with self.subTest(owner=owner, mode=mode), self.fixture(), \
                    patch.object(guard.os, 'fstat', return_value=SimpleNamespace(st_uid=owner, st_mode=stat.S_IFDIR | mode)), \
                    patch.object(guard.subprocess, 'run') as run:
                self.assertEqual(guard.execute('docker', guard.DOCKER), 75)
                run.assert_not_called()

    @unittest.skipUnless(shutil.which('dpkg-deb'), 'dpkg-deb required')
    def test_real_debian_package_contains_unchanged_guard_executables(self):
        with tempfile.TemporaryDirectory() as temporary:
            directory = Path(temporary)
            subprocess.run(['python3', str(ROOT / 'scripts/build-debian-package.py'), '--output', str(directory)],
                           check=True, capture_output=True)
            target = directory / 'unpacked'
            subprocess.run(['dpkg-deb', '--raw-extract', str(next(directory.glob('*.deb'))), str(target)],
                           check=True, capture_output=True)
            for name in ('boot-memory-guard.py', 'boot-daemon-guard.py'):
                installed = target / 'usr/share/titan' / name
                self.assertEqual(installed.read_bytes(), (ROOT / 'image' / name).read_bytes())
                self.assertEqual(stat.S_IMODE(installed.stat().st_mode) & 0o022, 0)

    def test_packaging_and_frozen_source_include_both_required_helpers(self):
        for file in ('scripts/build-debian-package.py', 'scripts/prepare-system-build.py'):
            value = (ROOT / file).read_text()
            self.assertIn('image/boot-memory-guard.py', value)
            self.assertIn('image/boot-daemon-guard.py', value)
        configure = (ROOT / 'image/debian/configure-guest.sh').read_text()
        self.assertIn('/usr/bin/python3 -I /usr/share/titan/boot-daemon-guard.py --install-dropins', configure)
        self.assertNotIn('ExecStartPre=/usr/bin/python3 /usr/share/titan/boot-memory-guard.py', configure)


if __name__ == '__main__':
    unittest.main()
