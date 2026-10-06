"""RAM protection is a verified reduced boot mode, not a generic health bypass."""
import json
import os
from pathlib import Path
import subprocess
import tempfile
import time
from types import SimpleNamespace
import unittest
from unittest.mock import MagicMock, patch

from titan import debian_updates as updates
from titan.app_memory import system_reserve
from titan.core import Error, atomic_json
from tests import test_debian_ab_updates as ab_fixture


def unit_status(*, unit='docker.service', state='failed', result='exit-code', status='78', argv=None):
    argv = argv or updates.DAEMON_COMMANDS[unit][1]
    return (f'LoadState=loaded\nActiveState={state}\nResult={result}\nExecMainCode=1\nExecMainStatus={status}\n'
            'ExecMainStartTimestampMonotonic=100\nExecMainExitTimestampMonotonic=300\nRestartPreventExitStatus=78\n'
            f'ExecStart={{ path=/usr/bin/python3 ; argv[]={argv} ; ignore_errors=no ; code=exited ; status={status} }}\n')


def socket_status(unit='docker.socket', state='active', result='success'):
    service, listener = {'docker.socket': ('docker.service', '/run/docker.sock'),
                         'libvirtd.socket': ('libvirtd.service', '/run/libvirt/libvirt-sock')}[unit]
    return (f'LoadState=loaded\nActiveState={state}\nSubState=listening\nResult={result}\n'
            f'Service={service}\nListen={listener} (Stream)\n')


class FreshGuardProofTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        root = Path(self.temporary.name)
        self.helper = root / 'guard.py'
        self.helper.write_text('trusted guard fixture')
        self.helper.chmod(0o644)
        self.report = root / 'guard.json'
        total = 3 * 1024**3
        reserve = system_reserve(total)
        self.value = {'ok': False, 'reason': 'insufficient_memory', 'total_bytes': total,
                      'reserve_bytes': reserve, 'required_bytes': reserve + 4 * 1024**3,
                      'checked_at': int(time.time()),
                      'components': {'docker': {'count': 1, 'limit_bytes': 4 * 1024**3},
                                     'vms': {'count': 0, 'limit_bytes': 0}}}
        for name, value in (('BOOT_MEMORY_GUARD', self.helper), ('BOOT_MEMORY_REPORT', self.report)):
            p = patch.object(updates, name, value); p.start(); self.addCleanup(p.stop)
        p = patch.object(updates.os, 'geteuid', return_value=0); p.start(); self.addCleanup(p.stop)
        # Root ownership is simulated only for these local fixtures; retain all
        # real inode/mode/time/size checks and exercise the actual file reader.
        original_fstat, original_lstat = os.fstat, Path.lstat
        def owned(info):
            fields = ('st_dev', 'st_ino', 'st_mode', 'st_size', 'st_mtime', 'st_mtime_ns', 'st_ctime_ns')
            return SimpleNamespace(st_uid=0, **{key: getattr(info, key) for key in fields})
        p = patch.object(updates.os, 'fstat', side_effect=lambda fd: owned(original_fstat(fd)))
        p.start(); self.addCleanup(p.stop)
        p = patch.object(Path, 'lstat', autospec=True, side_effect=lambda path: owned(original_lstat(path)))
        p.start(); self.addCleanup(p.stop)
        def execute(*args, **kwargs):
            atomic_json(self.report, self.value)
            return SimpleNamespace(returncode=1)
        p = patch.object(updates.subprocess, 'run', side_effect=execute)
        self.execute = p.start(); self.addCleanup(p.stop)

    def test_fresh_owned_shortage_and_consistent_inventory_are_accepted(self):
        result = updates._fresh_memory_block()
        self.assertEqual(result, {key: self.value[key] for key in ('total_bytes', 'reserve_bytes', 'required_bytes')})
        arguments = self.execute.call_args
        self.assertEqual(arguments.args[0], ['/usr/bin/python3', '-I', str(self.helper), '--component', 'all'])
        self.assertEqual(arguments.kwargs['timeout'], 15)
        self.assertEqual(arguments.kwargs['stderr'], subprocess.DEVNULL)

    def test_existing_report_without_fresh_write_is_not_an_exception(self):
        atomic_json(self.report, self.value)
        self.execute.side_effect = None
        self.execute.return_value = SimpleNamespace(returncode=1)
        self.assertIsNone(updates._fresh_memory_block())

    def test_unknown_invalid_success_and_stale_reports_are_rejected(self):
        original = dict(self.value)
        for changes in ({'ok': True, 'reason': ''}, {'reason': 'invalid_state'}, {'reason': 'unknown_memory'},
                        {'checked_at': int(time.time()) - 3600}, {'checked_at': int(time.time()) + 3600},
                        {'total_bytes': True}, {'required_bytes': self.value['total_bytes']},
                        {'reserve_bytes': 1}, {'required_bytes': self.value['required_bytes'] + 1},
                        {'components': {'docker': {'count': 1, 'limit_bytes': 4 * 1024**3}}}):
            with self.subTest(changes=changes):
                self.value = {**original, **changes}
                self.assertIsNone(updates._fresh_memory_block())

    def test_writable_helper_is_not_executed(self):
        self.helper.chmod(0o666)
        self.assertIsNone(updates._fresh_memory_block())
        self.execute.assert_not_called()

    def test_report_symlink_fifo_writable_oversized_and_duplicates_are_rejected(self):
        def write_raw(body):
            if self.report.exists() or self.report.is_symlink():
                self.report.unlink()
            self.report.write_bytes(body)
            self.report.chmod(0o644)
            return SimpleNamespace(returncode=1)
        def insecure(kind):
            write_raw(json.dumps(self.value).encode())
            if kind == 'symlink':
                self.report.unlink(); self.report.symlink_to(self.helper)
            elif kind == 'fifo':
                self.report.unlink(); os.mkfifo(self.report)
            elif kind == 'writable':
                self.report.chmod(0o666)
            elif kind == 'oversized':
                self.report.write_bytes(b' ' * 16385)
            else:
                self.report.write_text('{"ok":false,"ok":false}')
            return SimpleNamespace(returncode=1)
        for kind in ('symlink', 'fifo', 'writable', 'oversized', 'duplicate'):
            with self.subTest(kind=kind):
                self.execute.side_effect = lambda *args, kind=kind, **kwargs: insecure(kind)
                self.assertIsNone(updates._fresh_memory_block())

    def test_helper_success_other_exit_and_timeout_do_not_prove_blockage(self):
        self.execute.side_effect = None
        for code in (0, 2, -9):
            self.execute.return_value = SimpleNamespace(returncode=code)
            self.assertIsNone(updates._fresh_memory_block())
        self.execute.side_effect = subprocess.TimeoutExpired('guard', 15)
        self.assertIsNone(updates._fresh_memory_block())


class GuardUnitProofTests(unittest.TestCase):
    def setUp(self):
        p = patch.object(updates, '_daemon_denial', return_value=True)
        self.denial = p.start(); self.addCleanup(p.stop)

    def test_exact_guard_failure_is_required_not_a_substring_or_other_exit(self):
        with patch.object(updates, 'run', return_value=unit_status()):
            self.assertTrue(updates._guard_failed_unit('docker.service'))
        for text in (unit_status(state='active'), unit_status(result='timeout'), unit_status(status='0'),
                     unit_status(argv='/usr/bin/python3 /tmp/boot-memory-guard.py --component all'),
                     unit_status(argv=updates.DAEMON_COMMANDS['libvirtd.service'][1]),
                     unit_status(status='0') + '\nUnused=status=78',
                     unit_status().replace('ignore_errors=no', 'ignore_errors=yes')):
            with self.subTest(text=text), patch.object(updates, 'run', return_value=text):
                self.assertFalse(updates._guard_failed_unit('docker.service'))
        with patch.object(updates, 'run') as command:
            self.assertFalse(updates._guard_failed_unit('titan-web.service'))
            command.assert_not_called()

    def test_start_limit_exhaustion_requires_the_same_exact_guard_failure(self):
        with patch.object(updates, 'run', return_value=unit_status(result='start-limit-hit')):
            self.assertTrue(updates._guard_failed_unit('docker.service'))
        for text in (unit_status(result='start-limit-hit', status='0'),
                     unit_status(result='start-limit-hit', argv='/usr/bin/dockerd'),
                     unit_status(result='signal'), unit_status(result='timeout')):
            with self.subTest(text=text), patch.object(updates, 'run', return_value=text):
                self.assertFalse(updates._guard_failed_unit('docker.service'))

    def test_reloaded_main_status_needs_marker_and_exact_loaded_identity(self):
        good = unit_status()
        with patch.object(updates, 'run', return_value=good):
            self.denial.return_value = False
            self.assertFalse(updates._guard_failed_unit('docker.service'))
        self.denial.return_value = True
        for text in (good.replace('LoadState=loaded', 'LoadState=masked'),
                     good.replace('ExecMainCode=1', 'ExecMainCode=2'),
                     good.replace('RestartPreventExitStatus=78', 'RestartPreventExitStatus='),
                     good.replace('StartTimestampMonotonic=100', 'StartTimestampMonotonic=0'),
                     good.replace('ExitTimestampMonotonic=300', 'ExitTimestampMonotonic=99'),
                     good + 'ExecMainStatus=78\n'):
            with self.subTest(text=text), patch.object(updates, 'run', return_value=text):
                self.assertFalse(updates._guard_failed_unit('docker.service'))

    def test_runtime_shared_proof_checks_both_units_before_and_after_recompute(self):
        capacity = {'total_bytes': 3, 'reserve_bytes': 1, 'required_bytes': 4}
        with patch.object(updates, '_guard_failed_unit', return_value=True) as units, \
                patch.object(updates, '_fresh_memory_block', return_value=capacity) as fresh:
            self.assertEqual(updates.verified_boot_memory_block(), capacity)
        self.assertEqual([call.args[0] for call in units.call_args_list],
                         ['docker.service', 'libvirtd.service', 'docker.service', 'libvirtd.service'])
        fresh.assert_called_once()
        with patch.object(updates, '_guard_failed_unit', side_effect=[True, True, True, False]), \
                patch.object(updates, '_fresh_memory_block', return_value=capacity):
            self.assertIsNone(updates.verified_boot_memory_block())


class DenialMarkerTests(unittest.TestCase):
    def setUp(self):
        FreshGuardProofTests.setUp(self)
        self.marker = self.report.parent / 'marker.json'
        self.boot = self.report.parent / 'boot-id'
        self.boot.write_text('01234567-89ab-cdef-0123-456789abcdef\n')
        self.marker_value = {'format': 'titan-boot-daemon-denial-v1', 'component': 'docker',
            'boot_id': self.boot.read_text().strip(), 'reason': 'insufficient_memory', 'denied_monotonic_us': 200}
        for name, value in (('BOOT_DAEMON_GUARD', self.helper), ('BOOT_ID', self.boot),
                            ('DAEMON_MARKERS', {'docker.service': self.marker})):
            p = patch.object(updates, name, value); p.start(); self.addCleanup(p.stop)
        atomic_json(self.marker, self.marker_value)

    def test_marker_must_belong_to_current_boot_and_exact_main_lifetime(self):
        self.assertTrue(updates._daemon_denial('docker.service', 100, 300))
        for changes in ({'boot_id': 'ffffffff-ffff-ffff-ffff-ffffffffffff'}, {'component': 'vms'},
                        {'reason': 'invalid_state'}, {'reason': 'unknown_memory'}, {'denied_monotonic_us': 99},
                        {'denied_monotonic_us': 301}, {'denied_monotonic_us': True}, {'unexpected': 1}):
            with self.subTest(changes=changes):
                atomic_json(self.marker, {**self.marker_value, **changes})
                self.assertFalse(updates._daemon_denial('docker.service', 100, 300))

    def test_missing_writable_symlink_or_untrusted_wrapper_cannot_excuse_native_exit_78(self):
        self.marker.unlink()
        self.assertFalse(updates._daemon_denial('docker.service', 100, 300))
        atomic_json(self.marker, self.marker_value); self.marker.chmod(0o666)
        self.assertFalse(updates._daemon_denial('docker.service', 100, 300))
        self.marker.unlink(); self.marker.symlink_to(self.report)
        self.assertFalse(updates._daemon_denial('docker.service', 100, 300))
        self.marker.unlink(); atomic_json(self.marker, self.marker_value); self.helper.chmod(0o666)
        self.assertFalse(updates._daemon_denial('docker.service', 100, 300))


class GuardSocketTests(unittest.TestCase):
    def test_active_canonical_socket_does_not_activate_daemon(self):
        with patch.object(updates, 'run', return_value=socket_status()) as command, \
                patch.object(updates, 'verified_boot_memory_block') as guard:
            self.assertTrue(updates.verified_guard_socket('docker.socket'))
        guard.assert_not_called()
        self.assertEqual(command.call_args.args[0][:3], ['systemctl', 'show', 'docker.socket'])

    def test_failed_socket_requires_own_fresh_guard_and_stable_exact_identity(self):
        for unit in ('docker.socket', 'libvirtd.socket'):
            for result in ('service-start-limit-hit', 'trigger-limit-hit'):
                with self.subTest(unit=unit, result=result), \
                        patch.object(updates, 'run', return_value=socket_status(unit, 'failed', result)), \
                        patch.object(updates, 'verified_boot_memory_block', return_value={'total_bytes': 3}) as guard:
                    self.assertTrue(updates.verified_guard_socket(unit))
                    guard.assert_called_once_with(('docker.service' if unit == 'docker.socket' else 'libvirtd.service',))
        good = socket_status('docker.socket', 'failed', 'service-start-limit-hit')
        with patch.object(updates, 'run', side_effect=[good, good.replace('docker.sock', 'other.sock')]), \
                patch.object(updates, 'verified_boot_memory_block', return_value={'total_bytes': 3}):
            self.assertFalse(updates.verified_guard_socket('docker.socket'))

    def test_missing_masked_wrong_service_listener_and_unrelated_failures_are_fatal(self):
        for text in (socket_status().replace('loaded', 'masked'), socket_status().replace('docker.service', 'other.service'),
                     socket_status().replace('docker.sock', 'other.sock'), socket_status(state='failed', result='timeout'),
                     socket_status(state='failed', result='exit-code')):
            with self.subTest(text=text), patch.object(updates, 'run', return_value=text), \
                    patch.object(updates, 'verified_boot_memory_block') as guard:
                self.assertFalse(updates.verified_guard_socket('docker.socket'))
                guard.assert_not_called()
        with patch.object(updates, 'run', return_value=socket_status(state='failed', result='service-start-limit-hit')), \
                patch.object(updates, 'verified_boot_memory_block', return_value=None):
            self.assertFalse(updates.verified_guard_socket('docker.socket'))
        with patch.object(updates, 'run') as command:
            self.assertFalse(updates.verified_guard_socket('virtlogd.socket'))
            command.assert_not_called()


class GuardedBootHealthTests(unittest.TestCase):
    def setUp(self):
        ab_fixture.DebianABTests.setUp(self)
        p = patch.object(updates, '_daemon_denial', return_value=True)
        p.start(); self.addCleanup(p.stop)
    state = ab_fixture.DebianABTests.state

    def blocked_commands(self, args, **kwargs):
        self.commands.append(args)
        if args[0] == 'rauc':
            return json.dumps(self.rauc)
        if args == ['systemctl', 'is-active', '--quiet', 'docker.service']:
            raise Error('SECRET arbitrary stderr')
        if args[:2] == ['systemctl', 'show']:
            if args[2].endswith('.socket'):
                return socket_status(args[2])
            return unit_status(unit=args[2])
        if args[0] in ('docker', 'virsh'):
            raise AssertionError('Blocked daemon must not be socket activated')
        return ''

    def test_verified_ram_block_confirms_healthy_management_and_preserves_checks(self):
        updates.save_state(self.state())
        capacity = {'total_bytes': 3 * 1024**3, 'reserve_bytes': system_reserve(3 * 1024**3),
                    'required_bytes': 4 * 1024**3}
        with patch.object(updates, 'run', side_effect=self.blocked_commands), \
                patch.object(updates, '_fresh_memory_block', return_value=capacity), \
                patch.object(updates, '_guard_management_health') as management:
            result = updates.confirm_boot()
        management.assert_called_once_with(capacity)
        self.assertEqual(result['ram_protection'], {'active': True, 'blocked': ['docker', 'vms']})
        self.assertIn(['rauc', 'status', 'mark-good', 'booted'], self.commands)
        self.assertTrue(updates.BOOT_OK.exists())
        self.assertIn(['testparm', '-s'], self.commands)
        self.assertTrue(any(args[0] == 'curl' for args in self.commands))

    def test_all_core_units_still_required_under_ram_protection(self):
        for unit in ('titan-firstboot.service', 'titan-runtime.service', 'titan-agent.service',
                     'titan-web.service', 'titan-proxy.service', 'smbd.service', 'virtlogd.socket', 'virtlockd.socket'):
            with self.subTest(unit=unit):
                updates.save_state(self.state())
                self.commands.clear()
                def execute(args, **kwargs):
                    if args == ['systemctl', 'is-active', '--quiet', unit]:
                        raise Error('SECRET')
                    return self.blocked_commands(args, **kwargs)
                with patch.object(updates, 'run', side_effect=execute), \
                        patch.object(updates, '_fresh_memory_block', return_value={'total_bytes': 3}), \
                        patch.object(updates, '_guard_management_health'):
                    with self.assertRaises(Error) as failure:
                        updates.confirm_boot()
                self.assertIn(unit, str(failure.exception))
                self.assertNotIn('SECRET', str(failure.exception))
                self.assertFalse(any('mark-good' in args for args in self.commands))
                self.assertFalse(updates.BOOT_OK.exists())

    def test_stale_guard_or_unhealthy_management_never_confirms(self):
        for fresh, management_failure in ((None, False), ({'total_bytes': 3}, True)):
            with self.subTest(fresh=fresh, management_failure=management_failure):
                updates.save_state(self.state())
                self.commands.clear()
                with patch.object(updates, 'run', side_effect=self.blocked_commands), \
                        patch.object(updates, '_fresh_memory_block', return_value=fresh), \
                        patch.object(updates, '_guard_management_health',
                                     side_effect=Error('agent unhealthy') if management_failure else None):
                    with self.assertRaises(Error):
                        updates.confirm_boot()
                self.assertFalse(any('mark-good' in args for args in self.commands))
                self.assertFalse(updates.BOOT_OK.exists())

    def test_docker_guard_cannot_excuse_unrelated_libvirt_failure(self):
        updates.save_state(self.state())
        def execute(args, **kwargs):
            if args[:3] == ['systemctl', 'show', 'libvirtd.service']:
                return unit_status(unit='libvirtd.service', status='2')
            if args[0] == 'virsh':
                raise Error('SECRET daemon broken')
            return self.blocked_commands(args, **kwargs)
        with patch.object(updates, 'run', side_effect=execute), \
                patch.object(updates, '_fresh_memory_block', return_value={'total_bytes': 3}):
            with self.assertRaisesRegex(Error, 'libvirtd.service') as failure:
                updates.confirm_boot()
        self.assertNotIn('SECRET', str(failure.exception))
        self.assertFalse(any('mark-good' in args for args in self.commands))

    def test_active_docker_with_broken_api_cannot_use_guard_exception(self):
        updates.save_state(self.state())
        def execute(args, **kwargs):
            if args == ['systemctl', 'is-active', '--quiet', 'docker.service']:
                return ''
            if args[0] == 'docker':
                raise Error('SECRET')
            return self.blocked_commands(args, **kwargs)
        with patch.object(updates, 'run', side_effect=execute), \
                patch.object(updates, '_fresh_memory_block') as fresh:
            with self.assertRaisesRegex(Error, 'docker.service'):
                updates.confirm_boot()
        fresh.assert_not_called()
        self.assertFalse(any('mark-good' in args for args in self.commands))


class GuardManagementProbeTests(unittest.TestCase):
    def test_agent_status_memory_matches_fresh_guard_with_bounded_socket(self):
        connection = MagicMock()
        connection.__enter__.return_value = connection
        with patch.object(updates.socket, 'socket', return_value=connection), \
                patch('titan.rpc.send') as send, \
                patch('titan.rpc.receive', return_value={'result': {'memory_total': 3}}):
            updates._guard_management_health({'total_bytes': 3})
        connection.connect.assert_called_once_with('/run/titan/agent.sock')
        self.assertLessEqual(connection.settimeout.call_args.args[0], 15)
        self.assertEqual(send.call_args.args[1], {'operation': 'status', 'arguments': {}})

    def test_unhealthy_agent_unknown_and_mismatched_capacity_fail_closed(self):
        connection = MagicMock()
        connection.__enter__.return_value = connection
        for response in ({'error': 'SECRET'}, {'result': {}}, {'result': {'memory_total': True}},
                         {'result': {'memory_total': 8}}, []):
            with self.subTest(response=response), patch.object(updates.socket, 'socket', return_value=connection), \
                    patch('titan.rpc.send'), patch('titan.rpc.receive', return_value=response):
                with self.assertRaisesRegex(Error, 'titan-agent.service') as failure:
                    updates._guard_management_health({'total_bytes': 3})
                self.assertNotIn('SECRET', str(failure.exception))


if __name__ == '__main__':
    unittest.main()
