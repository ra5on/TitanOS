"""Offline startup admission never activates the daemons it protects."""
import contextlib
import importlib.util
import io
import json
import os
from pathlib import Path
import stat
import tempfile
import unittest
from unittest.mock import patch

from titan.app_memory import GIB, MIB, system_reserve, vm_overhead

ROOT = Path(__file__).resolve().parents[1]
SPEC = importlib.util.spec_from_file_location('titan_offline_boot_guard', ROOT / 'image/boot-memory-guard.py')
guard = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(guard)


class OfflineBootMemoryTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.owner = os.geteuid()
        self.docker = self.root / 'docker' / 'containers'
        self.qemu = self.root / 'qemu'
        self.meminfo = self.root / 'meminfo'
        self.daemon = self.root / 'daemon.json'
        self.memory(8 * GIB)

    def memory(self, total, swap=0):
        self.meminfo.write_text(f'MemTotal: {total // 1024} kB\nMemAvailable: 1 kB\nSwapTotal: {swap // 1024} kB\n')

    def write(self, path, value):
        path.parent.mkdir(parents=True, exist_ok=True)
        for directory in (path.parent, *path.parent.parents):
            if directory == self.root:
                break
            directory.chmod(0o755)
        path.write_text(json.dumps(value) if isinstance(value, (dict, list)) else value)
        path.chmod(0o600)
        return path

    def container(self, number=1, *, policy='unless-stopped', memory=GIB, running=False,
                  manual=False, started=True, flags=True):
        identifier = f'{number:064x}'
        directory = self.docker / identifier
        self.write(directory / 'hostconfig.json', {'Memory': memory, 'RestartPolicy': {'Name': policy}})
        value = {'ID': identifier, 'State': {'Running': running, 'Paused': False, 'Restarting': False}}
        if flags:
            value.update(HasBeenManuallyStopped=manual, HasBeenStartedBefore=started)
        self.write(directory / 'config.v2.json', value)
        return directory

    def vm(self, number=1, *, memory=1, unit='GiB', relative=False, xml=None):
        name = f'guest-{number}.xml'
        identifier = f'11111111-1111-1111-1111-{number:012x}'
        definition = self.qemu / name
        self.write(definition, xml or f'<domain><uuid>{identifier}</uuid><memory unit="{unit}">{memory}</memory><currentMemory unit="GiB">0</currentMemory></domain>')
        autostart = self.qemu / 'autostart'
        autostart.mkdir(exist_ok=True)
        autostart.chmod(0o755)
        link = autostart / name
        link.symlink_to('../' + name if relative else definition)
        return definition, link

    def evaluate(self):
        # Any daemon CLI use would violate the offline startup boundary.
        with patch('subprocess.run', side_effect=AssertionError('daemon/socket activation forbidden')):
            return guard.evaluate(meminfo=self.meminfo, docker_root=self.docker, vm_root=self.qemu,
                                  daemon_config=self.daemon, owner=self.owner)

    def test_fresh_missing_or_empty_metadata_allows_initial_install(self):
        missing = self.evaluate()
        self.assertTrue(missing['ok'])
        self.assertEqual(missing['required_bytes'], system_reserve(8 * GIB))
        self.docker.mkdir(parents=True)
        self.docker.chmod(0o755)
        (self.qemu / 'autostart').mkdir(parents=True)
        self.qemu.chmod(0o755)
        (self.qemu / 'autostart').chmod(0o755)
        self.assertEqual(self.evaluate()['components'], {'docker': {'count': 0, 'limit_bytes': 0}, 'vms': {'count': 0, 'limit_bytes': 0}})

    def test_combined_full_ram_limits_and_vm_overhead_block_after_ram_reduction(self):
        self.container(memory=4 * GIB)
        self.vm(memory=2)
        self.memory(4 * GIB, swap=64 * GIB)
        result = self.evaluate()
        self.assertFalse(result['ok'])
        self.assertEqual(result['reason'], 'insufficient_memory')
        self.assertEqual(result['required_bytes'], 6 * GIB + vm_overhead(2 * GIB) + system_reserve(4 * GIB))
        self.assertEqual(result['total_bytes'], 4 * GIB)
        self.memory(8 * GIB)
        self.assertTrue(self.evaluate()['ok'])

    def test_stopped_always_and_on_failure_are_conservative_boot_candidates(self):
        self.container(1, policy='always', manual=True, started=False)
        self.container(2, policy='on-failure', manual=True, started=False)
        self.assertEqual(self.evaluate()['components']['docker'], {'count': 2, 'limit_bytes': 2 * GIB})

    def test_clean_shutdown_unless_stopped_is_counted_but_manual_stop_is_not(self):
        self.container(1, running=False, manual=False, started=True)
        self.container(2, running=False, manual=True, started=True, memory=0)
        self.container(3, running=False, manual=False, started=False, memory=0)
        self.assertEqual(self.evaluate()['components']['docker'], {'count': 1, 'limit_bytes': GIB})

    def test_active_state_overrides_stale_manual_stop_flag(self):
        self.container(running=True, manual=True)
        self.assertEqual(self.evaluate()['components']['docker']['limit_bytes'], GIB)

    def test_missing_old_stop_flags_are_counted_not_assumed_stopped(self):
        self.container(flags=False)
        self.assertEqual(self.evaluate()['components']['docker']['count'], 1)

    def test_disabled_unlimited_container_does_not_prevent_daemon_boot(self):
        self.container(policy='no', memory=0)
        self.assertTrue(self.evaluate()['ok'])
        self.assertEqual(self.evaluate()['components']['docker']['count'], 0)

    def test_unlimited_noninteger_unknown_policies_and_invalid_flags_fail_closed(self):
        for field, value in (('Memory', 0), ('Memory', True), ('Memory', '1g'), ('Memory', -1),
                             ('Memory', 2**63), ('RestartPolicy', {'Name': 'mystery'})):
            with self.subTest(field=field, value=value):
                directory = self.container()
                host = json.loads((directory / 'hostconfig.json').read_text())
                host[field] = value
                self.write(directory / 'hostconfig.json', host)
                self.assertEqual(self.evaluate()['reason'], 'invalid_state')
        directory = self.container()
        config = json.loads((directory / 'config.v2.json').read_text())
        config['HasBeenManuallyStopped'] = 'false'
        self.write(directory / 'config.v2.json', config)
        self.assertEqual(self.evaluate()['reason'], 'invalid_state')

    def test_malformed_missing_duplicate_json_and_mismatched_identity_are_rejected(self):
        directory = self.container()
        host = directory / 'hostconfig.json'
        for value in ('{', '[]', '{"Memory":1,"Memory":2}', '{"Memory":NaN}'):
            with self.subTest(value=value):
                self.write(host, value)
                self.assertEqual(self.evaluate()['reason'], 'invalid_state')
        self.container()
        self.write(directory / 'config.v2.json', {'ID': 'f' * 64, 'State': {}})
        self.assertEqual(self.evaluate()['reason'], 'invalid_state')
        host.unlink()
        self.assertEqual(self.evaluate()['reason'], 'invalid_state')

    def test_symlinks_fifos_writable_and_oversized_container_metadata_are_rejected(self):
        directory = self.container()
        host = directory / 'hostconfig.json'
        host.chmod(0o666)
        self.assertEqual(self.evaluate()['reason'], 'invalid_state')
        host.unlink(); os.mkfifo(host)
        self.assertEqual(self.evaluate()['reason'], 'invalid_state')
        host.unlink(); host.symlink_to(self.meminfo)
        self.assertEqual(self.evaluate()['reason'], 'invalid_state')
        host.unlink(); self.write(host, ' ' * (guard.MAX_FILE + 1))
        self.assertEqual(self.evaluate()['reason'], 'invalid_state')

    def test_symlinked_container_directory_is_not_followed(self):
        directory = self.container()
        moved = self.root / 'foreign'
        directory.rename(moved)
        directory.symlink_to(moved, target_is_directory=True)
        self.assertEqual(self.evaluate()['reason'], 'invalid_state')

    def test_foreign_owner_and_inventory_caps_fail_closed(self):
        self.container(1); self.container(2)
        with patch.object(guard, 'MAX_CONTAINERS', 1):
            self.assertEqual(self.evaluate()['reason'], 'invalid_state')
        result = guard.evaluate(meminfo=self.meminfo, docker_root=self.docker, vm_root=self.qemu,
                                daemon_config=self.daemon, owner=self.owner + 1)
        self.assertEqual(result['reason'], 'invalid_state')
        with patch.object(guard, 'MAX_METADATA', 1):
            self.assertEqual(self.evaluate()['reason'], 'invalid_state')

    def test_sum_overflow_still_emits_bounded_valid_report_fields(self):
        self.container(memory=guard.MAX_MEMORY)
        result = self.evaluate()
        self.assertEqual(result['reason'], 'invalid_state')
        self.assertEqual(result['required_bytes'], guard.MAX_MEMORY)

    def test_atomically_replaced_metadata_during_read_is_not_a_stale_budget(self):
        directory = self.container()
        host = directory / 'hostconfig.json'
        original_stat = guard.os.stat
        replaced = False
        def altered_stat(path, *args, **kwargs):
            nonlocal replaced
            if path == 'hostconfig.json' and not replaced:
                replaced = True
                fresh = directory / 'fresh.json'
                self.write(fresh, {'Memory': 7 * GIB, 'RestartPolicy': {'Name': 'always'}})
                fresh.replace(host)
            return original_stat(path, *args, **kwargs)
        with patch.object(guard.os, 'stat', side_effect=altered_stat):
            self.assertEqual(self.evaluate()['reason'], 'invalid_state')

    def test_nondefault_docker_data_root_is_not_silently_skipped(self):
        self.write(self.daemon, {'data-root': '/somewhere-else'})
        self.assertEqual(self.evaluate()['reason'], 'invalid_state')
        self.write(self.daemon, {'data-root': '/var/lib/docker', 'log-driver': 'json-file'})
        self.assertTrue(self.evaluate()['ok'])

    def test_libvirt_stopped_autostart_memory_not_current_memory_and_relative_links(self):
        self.vm(memory=1, relative=True)
        self.vm(2, memory=1024, unit='MiB')
        self.assertEqual(self.evaluate()['components']['vms'], {'count': 2, 'limit_bytes': 2 * (GIB + vm_overhead(GIB))})

    def test_non_autostart_vm_does_not_reserve_ram(self):
        definition, link = self.vm(memory=900)
        link.unlink()
        self.assertTrue(self.evaluate()['ok'])
        self.assertEqual(self.evaluate()['components']['vms']['count'], 0)

    def test_libvirt_external_dangling_and_regular_autostart_links_are_rejected(self):
        definition, link = self.vm()
        link.unlink(); link.symlink_to(self.meminfo)
        self.assertEqual(self.evaluate()['reason'], 'invalid_state')
        link.unlink(); link.symlink_to('../not-present.xml')
        self.assertEqual(self.evaluate()['reason'], 'invalid_state')
        link.unlink(); self.write(link, definition.read_text())
        self.assertEqual(self.evaluate()['reason'], 'invalid_state')

    def test_libvirt_malformed_unlimited_duplicate_and_entity_definitions_fail_closed(self):
        identifier = '11111111-1111-1111-1111-000000000001'
        for xml in ('<domain>', f'<domain><uuid>{identifier}</uuid><memory>0</memory></domain>',
                    f'<domain><uuid>{identifier}</uuid><memory unit="invalid">1</memory></domain>',
                    f'<domain><uuid>{identifier}</uuid><memory>1</memory><memory>2</memory></domain>',
                    '<!DOCTYPE domain [<!ENTITY x "secret">]><domain><uuid>&x;</uuid><memory>1</memory></domain>'):
            with self.subTest(xml=xml):
                definition, link = self.vm(xml=xml)
                self.assertEqual(self.evaluate()['reason'], 'invalid_state')
                link.unlink()
        self.vm()
        definition, _ = self.vm(2)
        self.write(definition, (self.qemu / 'guest-1.xml').read_text())
        self.assertEqual(self.evaluate()['reason'], 'invalid_state')

    def test_utf16_entity_xml_and_symlinked_definition_are_not_followed(self):
        definition, _ = self.vm()
        definition.write_bytes('<!DOCTYPE domain [<!ENTITY x "secret">]><domain><uuid>&x;</uuid><memory>1</memory></domain>'.encode('utf-16'))
        self.assertEqual(self.evaluate()['reason'], 'invalid_state')
        definition.unlink(); definition.symlink_to(self.meminfo)
        self.assertEqual(self.evaluate()['reason'], 'invalid_state')

    def test_unknown_total_memory_is_fail_closed_and_swap_cannot_replace_it(self):
        for text in ('SwapTotal: 8388608 kB\n', 'MemTotal: 0 kB\n', 'MemTotal: -1 kB\n',
                     'MemTotal: 4096 kB\nMemTotal: 4096 kB\n', 'MemTotal: True kB\n'):
            with self.subTest(text=text):
                self.meminfo.write_text(text)
                self.assertEqual(self.evaluate()['reason'], 'unknown_memory')
        self.meminfo.unlink()
        self.assertEqual(self.evaluate()['total_bytes'], 0)

    def test_report_is_atomic_regular_bounded_and_no_metadata_secrets_escape(self):
        directory = self.container()
        config = json.loads((directory / 'config.v2.json').read_text())
        config['Config'] = {'Env': ['PASSWORD=secret-value']}
        self.write(directory / 'config.v2.json', config)
        result = self.evaluate()
        path = self.root / 'report.json'
        path.symlink_to(self.meminfo)
        old_memory = self.meminfo.read_text()
        guard.write_report(result, path, owner=self.owner)
        self.assertTrue(stat.S_ISREG(path.lstat().st_mode))
        self.assertEqual(path.stat().st_uid, self.owner)
        self.assertEqual(path.stat().st_mode & 0o022, 0)
        self.assertLessEqual(path.stat().st_size, 16384)
        self.assertEqual(json.loads(path.read_text()), result)
        self.assertEqual(self.meminfo.read_text(), old_memory)
        self.assertNotIn('secret-value', path.read_text())
        self.assertEqual(list(self.root.glob('.titan-boot-memory-*')), [])

    def test_cli_components_all_use_combined_budget_and_status_write_failures_block(self):
        blocked = {'ok': False, 'reason': 'insufficient_memory', 'required_bytes': 4 * GIB + 1, 'total_bytes': 4 * GIB}
        for component in ('docker', 'vms', 'all'):
            with self.subTest(component=component), patch.object(guard.os, 'geteuid', return_value=0), \
                 patch.object(guard, 'evaluate', return_value=blocked) as evaluate, \
                 patch.object(guard, 'write_report') as report, contextlib.redirect_stderr(io.StringIO()) as output:
                self.assertEqual(guard.main(['--component', component]), 1)
                evaluate.assert_called_once_with()
                report.assert_called_once_with(blocked)
                self.assertIn('mindestens 5 GiB', output.getvalue())
        with patch.object(guard.os, 'geteuid', return_value=0), \
             patch.object(guard, 'evaluate', return_value={'ok': True}), \
             patch.object(guard, 'write_report', side_effect=OSError('secret-path')), \
             contextlib.redirect_stderr(io.StringIO()) as output:
            self.assertEqual(guard.main([]), 1)
            self.assertNotIn('secret-path', output.getvalue())

    def test_daemon_guard_cannot_make_web_agent_or_filemanager_require_a_daemon(self):
        runtime = (ROOT / 'image/titan-runtime.service').read_text()
        self.assertIn('TimeoutStartSec=30', runtime)
        for name in ('agent', 'web', 'proxy'):
            text = (ROOT / f'packaging/titan-{name}.service').read_text()
            requirements = [line for line in text.splitlines() if line.startswith(('Requires=', 'Requisite=', 'BindsTo='))]
            self.assertFalse(any(any(service in line for service in ('docker', 'libvirt', 'titan-runtime')) for line in requirements))
        configure = (ROOT / 'image/debian/configure-guest.sh').read_text()
        self.assertIn('for task_service in docker libvirtd;', configure)
        self.assertIn('ExecStartPre=/usr/bin/python3 /usr/share/titan/boot-memory-guard.py --component all', configure)
        self.assertNotIn('libvirtd.socket.d/titan-memory.conf', configure)
        self.assertNotIn('docker.socket.d/titan-memory.conf', configure)


if __name__ == '__main__':
    unittest.main()
