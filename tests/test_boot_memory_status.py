import json
import os
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

from titan.components import boot_memory_report
from titan.core import Error
from tests import test_components as component_fixture


class BootMemoryReportTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.path = Path(self.temporary.name) / 'guard.json'
        self.value = {'ok': False, 'reason': 'insufficient_memory', 'total_bytes': 4 * 1024**3,
                      'reserve_bytes': 1024**3, 'required_bytes': 7 * 1024**3 + 1}

    def write(self, value=None):
        self.path.write_text(json.dumps(self.value if value is None else value))
        self.path.chmod(0o644)

    def test_known_failure_is_explained_without_raw_metadata_or_text(self):
        self.write({**self.value, 'message': 'SECRET', 'components': {'password': 'SECRET'}})
        result = boot_memory_report(self.path)
        self.assertFalse(result['ok'])
        self.assertIn('mindestens 8 GiB', result['message'])
        self.assertIn('4.0 GiB', result['message'])
        self.assertNotIn('SECRET', json.dumps(result))
        self.assertNotIn('components', result)

    def test_missing_symlink_fifo_oversized_and_foreign_writable_reports_are_ignored(self):
        self.assertIsNone(boot_memory_report(self.path))
        self.write(); self.path.chmod(0o666)
        self.assertIsNone(boot_memory_report(self.path))
        self.path.unlink(); os.mkfifo(self.path)
        self.assertIsNone(boot_memory_report(self.path))
        self.path.unlink(); self.path.symlink_to('/etc/passwd')
        self.assertIsNone(boot_memory_report(self.path))
        self.path.unlink(); self.path.write_text(' ' * 16385); self.path.chmod(0o644)
        self.assertIsNone(boot_memory_report(self.path))

    def test_unknown_reasons_invalid_counts_and_boolean_counts_are_rejected(self):
        for value in ({**self.value, 'reason': '<script>'}, {**self.value, 'ok': 0},
                      {**self.value, 'total_bytes': True}, {**self.value, 'required_bytes': -1},
                      {**self.value, 'reserve_bytes': 2**63}, []):
            with self.subTest(value=value):
                self.write(value)
                self.assertIsNone(boot_memory_report(self.path))

    def test_success_and_unmeasurable_state_do_not_claim_a_ram_shortage(self):
        self.write({**self.value, 'ok': True, 'reason': ''})
        self.assertEqual(boot_memory_report(self.path)['message'], '')
        self.write({**self.value, 'reason': 'invalid_state'})
        self.assertIn('nicht sicher geprüft', boot_memory_report(self.path)['message'])
        self.assertNotIn('mindestens', boot_memory_report(self.path)['message'])


class BootMemoryComponentTests(unittest.TestCase):
    setUp = component_fixture.ComponentTests.setUp
    tearDown = component_fixture.ComponentTests.tearDown

    def test_blocked_daemons_show_boot_ram_reason_but_healthy_daemons_remain_ready(self):
        guard = {'ok': False, 'message': 'RAM-Schutz aktiv'}
        def command(args, **kwargs):
            if args[:3] == ['docker', 'compose', 'version']:
                return 'compose version'
            raise Error('daemon stopped')
        availability = {'installed': True, 'available': True, 'missing': [], 'kvm': True}
        with patch('titan.components.boot_memory_report', return_value=guard), \
             patch('titan.components.shutil.which', return_value='/usr/bin/docker'), \
             patch('titan.components.availability', return_value=availability.copy()), \
             patch('titan.host.run', side_effect=command):
            result = self.host.op_components()
        for name in ('docker', 'vms'):
            self.assertFalse(result['components'][name]['available'])
            self.assertEqual(result['components'][name]['error'], 'RAM-Schutz aktiv')
        with patch('titan.components.boot_memory_report', return_value=guard), \
             patch('titan.components.shutil.which', return_value='/usr/bin/docker'), \
             patch('titan.components.availability', return_value=availability.copy()), \
             patch('titan.host.run', return_value='ready'):
            result = self.host.op_components()
        self.assertTrue(result['components']['docker']['available'])
        self.assertTrue(result['components']['vms']['available'])


if __name__ == '__main__':
    unittest.main()
