"""Cold backup recovery must pass fresh storage, hardware and RAM admission."""
import json
import threading
import unittest
from unittest.mock import patch

from titan.app_memory import GIB, check_start_memory
from titan.core import Error
import test_app_lifecycle_safety as lifecycle
import test_app_management as fixture
from test_app_usb_safety import SELECTED, USB
from test_boot_memory_budget import row as budget_row


class AppBackupSafetyTests(unittest.TestCase):
    setUp = fixture.AppManagementTests.setUp
    tearDown = fixture.AppManagementTests.tearDown
    command = fixture.AppManagementTests.command
    make_container = fixture.AppManagementTests.make_container
    managed_volume = fixture.AppManagementTests.managed_volume
    install = fixture.AppManagementTests.install
    member = lifecycle.AppLifecycleSafetyTests.member

    def stack(self, storage_id=None, hardware=None):
        app = 'titan-immich'
        self.host.op_app_install(app, 2283, storage_id=storage_id, hardware=hardware)
        definition = json.loads((self.host.directory / 'apps' / app / 'compose.json').read_text())
        for key, service in definition['services'].items():
            value = self.member(app, key)
            value['HostConfig']['RestartPolicy'] = {'Name': service['restart']}
            value['HostConfig']['Devices'] = [{'PathOnHost': v.split(':')[0],
                'PathInContainer': v.split(':')[1], 'CgroupPermissions': v.split(':')[2]}
                for v in service.get('devices', [])]
        self.calls.clear()
        return app

    def tar_change(self, change, fail=False, start_change=None):
        base = self.command
        def command(args, **kwargs):
            result = base(args, **kwargs)
            if args[:2] == ['docker', 'compose'] and args[-1] == 'stop':
                for value in self.containers.values():
                    value['State'].update(Status='exited', Running=False)
            if args[0] == 'tar':
                change()
                if fail:
                    raise Error('archive failed')
            if args[:2] == ['docker', 'start'] and start_change:
                start_change(args)
            return result
        self.runner.side_effect = command

    def archives(self):
        return list((self.host.directory / 'backups').glob('*.tar.gz'))

    def assert_denied(self, app, text):
        with self.assertRaisesRegex(Error, text) as caught:
            self.host.op_app_backup(app)
        archives = self.archives()
        self.assertEqual(len(archives), 1)
        self.assertIn(str(archives[0]), str(caught.exception))
        record = self.host.load('apps', [])[0]
        self.assertEqual(record['phase'], 'failed')
        self.assertIn(str(archives[0]), record['last_error'])
        self.assertFalse(any(call[:2] == ['docker', 'start'] for call in self.calls))
        self.assertTrue(all(value['State']['Status'] == 'exited' for value in self.containers.values()))

    def test_volume_uuid_replaced_during_archive_blocks_stack_resume_and_retains_backup(self):
        folder = self.managed_volume()
        app = self.stack(storage_id='volume:media')
        volume = self.host.load('volumes', [])[0]
        replacement = {**volume, 'uuid': '22222222-2222-4222-8222-222222222222'}
        self.tar_change(lambda: setattr(self.volume_ready, 'return_value', (replacement, folder.stat().st_dev)))
        self.assert_denied(app, 'ausgetauscht')

    def test_raw_usb_reused_during_archive_blocks_stack_resume_and_retains_backup(self):
        current = [SELECTED]
        with patch('titan.app_management.app_devices_inventory', side_effect=lambda: current), \
             patch('titan.app_devices.devices', side_effect=lambda: current):
            app = self.stack(hardware=[USB])
            self.tar_change(lambda: current.__setitem__(slice(None), [{**SELECTED, 'identity': 'b' * 64}]))
            self.assert_denied(app, 'ersetzt')

    def test_new_stopped_autostart_commitment_during_archive_blocks_real_boot_budget(self):
        app = self.stack()
        self.tar_change(lambda: self.containers.__setitem__('foreign', budget_row('f' * 64,
            memory=7 * GIB, restart='always', name='foreign')))
        with patch('titan.app_memory.check_start_memory', wraps=check_start_memory), \
             patch('titan.app_memory.memory_snapshot', return_value={'memory_total': 8 * GIB, 'memory_available': 7 * GIB}), \
             patch('titan.app_memory.vm_memory_reservations', return_value=[]), \
             patch('titan.app_memory.vm_boot_reservations', return_value=[]):
            self.assert_denied(app, 'Autostart-Budget')

    def test_live_ram_shortage_during_archive_blocks_real_current_budget(self):
        app = self.stack()
        available = {'memory_total': 8 * GIB, 'memory_available': 7 * GIB}
        self.tar_change(lambda: available.update(memory_available=128 * 1024 * 1024))
        with patch('titan.app_memory.check_start_memory', wraps=check_start_memory), \
             patch('titan.app_memory.memory_snapshot', side_effect=lambda *args: dict(available)), \
             patch('titan.app_memory.vm_memory_reservations', return_value=[]), \
             patch('titan.app_memory.vm_boot_reservations', return_value=[]):
            self.assert_denied(app, 'RAM-Engpass')

    def test_replaced_container_name_is_not_adopted_for_recovery(self):
        app = self.stack()
        key = app + '-database'
        old = self.containers[key]['Id']
        self.tar_change(lambda: self.containers[key].update(Id='e' * 64))
        self.assert_denied(app, 'ersetzt')
        self.assertNotEqual(self.containers[key]['Id'], old)

    def test_missing_private_options_do_not_hide_the_retained_archive_path(self):
        app = self.stack()
        self.tar_change(lambda: (self.host.directory / 'apps' / app / 'options.json').unlink())
        self.assert_denied(app, 'bleibt erhalten')

    def test_only_originally_running_services_resume_under_shared_lock(self):
        app = self.stack()
        excluded = app + '-redis'
        self.containers[excluded]['State'].update(Status='exited', Running=False)
        original = {value['Id'] for key, value in self.containers.items() if key != excluded}
        self.tar_change(lambda: None)
        observed = []
        base = self.runner.side_effect
        def command(args, **kwargs):
            if args[:2] == ['docker', 'start']:
                acquired = []
                def compete():
                    owns = self.host.app_memory_lock.acquire(blocking=False)
                    acquired.append(owns)
                    if owns:
                        self.host.app_memory_lock.release()
                thread = threading.Thread(target=compete)
                thread.start(); thread.join(1)
                self.assertFalse(thread.is_alive())
                observed.extend(acquired)
            return base(args, **kwargs)
        self.runner.side_effect = command
        result = self.host.op_app_backup(app)
        self.assertFalse(result['kept_stopped'])
        starts = [call for call in self.calls if call[:2] == ['docker', 'start']]
        self.assertEqual(len(starts), 1)
        self.assertEqual(set(starts[0][2:]), original)
        self.assertEqual(observed, [False])
        self.assertEqual(self.containers[excluded]['State']['Status'], 'exited')
        self.assertTrue(all(value['State']['Status'] == 'running'
            for key, value in self.containers.items() if key != excluded))

    def test_failed_archive_and_denied_recovery_report_both_without_retained_partial(self):
        app = self.stack()
        self.tar_change(lambda: self.containers[app].update(Id='e' * 64), fail=True)
        with self.assertRaisesRegex(Error, 'Sicherung fehlgeschlagen: archive failed') as caught:
            self.host.op_app_backup(app)
        self.assertIn('ersetzt', str(caught.exception))
        self.assertIn('unvollständige Archiv wurde entfernt', str(caught.exception))
        self.assertNotIn('bleibt erhalten', str(caught.exception))
        self.assertEqual(self.archives(), [])
        self.assertFalse(any(call[:2] == ['docker', 'start'] for call in self.calls))
        self.assertEqual(self.host.load('apps', [])[0]['phase'], 'failed')

    def test_partial_resume_failure_reports_actual_state_and_keeps_archive(self):
        app = self.stack()
        def partial(args):
            self.containers[app + '-database']['State'].update(Status='exited', Running=False)
        self.tar_change(lambda: None, start_change=partial)
        with self.assertRaisesRegex(Error, 'Nicht alle zuvor laufenden') as caught:
            self.host.op_app_backup(app)
        self.assertEqual(len(self.archives()), 1)
        self.assertIn(str(self.archives()[0]), str(caught.exception))
        self.assertEqual(self.containers[app]['State']['Status'], 'running')
        self.assertEqual(self.containers[app + '-database']['State']['Status'], 'exited')
        self.assertEqual(self.host.load('apps', [])[0]['phase'], 'failed')


if __name__ == '__main__':
    unittest.main()
