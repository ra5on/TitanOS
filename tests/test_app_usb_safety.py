"""Raw USB cannot expose host storage or silently adopt a reused address."""
import json
import os
from pathlib import Path
import stat
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import Mock, patch

from titan.app_devices import (apply, devices, hardware_ready, native_hardware,
                               raw_usb_container_ready, usb_peripheral)
from titan.core import Error

USB = '/dev/bus/usb/001/004'
SELECTED = {'id': USB, 'path': USB, 'label': 'USB peripheral', 'kind': 'usb',
            'group': 0, 'identity': 'a' * 64}


class AppUSBTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.sys = self.root / 'sys'
        self.sys.mkdir()

    def usb(self, port='1-2', serial='unique-peripheral', device_class='00', interface_class='03', address=4):
        device = self.sys / port
        device.mkdir()
        for key, value in {'idVendor': '1234', 'idProduct': 'abcd', 'serial': serial,
                           'busnum': '1', 'devnum': str(address), 'bDeviceClass': device_class}.items():
            (device / key).write_text(value)
        interface = device / (port + ':1.0')
        interface.mkdir()
        if interface_class is not None:
            (interface / 'bInterfaceClass').write_text(interface_class)
        return device, interface

    def row(self, selected=None):
        return {'HostConfig': {'Devices': [{'PathOnHost': USB, 'PathInContainer': USB, 'CgroupPermissions': 'rw'}]},
                'Config': {'Labels': {'io.titan.hardware': json.dumps([selected or SELECTED])}}}

    def test_storage_hubs_network_and_unknown_interfaces_never_have_raw_access(self):
        for code in ('08', '09', '02', '0a', 'e0', '', '99', None):
            with self.subTest(code=code):
                device, interface = self.usb(interface_class=code)
                self.assertIsNone(usb_peripheral(USB, self.sys))
                import shutil
                shutil.rmtree(device)
        device, interface = self.usb()
        self.assertIsNotNone(usb_peripheral(USB, self.sys))
        (interface / 'bInterfaceClass').unlink()
        self.assertIsNone(usb_peripheral(USB, self.sys))

    def test_composite_and_vendor_specific_interfaces_cannot_hide_host_storage_or_nic(self):
        device, interface = self.usb(interface_class='ff')
        for kind in ('block', 'net'):
            directory = interface / kind
            directory.mkdir()
            self.assertIsNone(usb_peripheral(USB, self.sys))
            directory.rmdir()
        second = device / (device.name + ':1.1')
        second.mkdir()
        (second / 'bInterfaceClass').write_text('08')
        self.assertIsNone(usb_peripheral(USB, self.sys))

    def test_missing_duplicate_serial_or_ambiguous_bus_address_is_blocked(self):
        device, _ = self.usb(serial='')
        self.assertIsNone(usb_peripheral(USB, self.sys))
        (device / 'serial').write_text('unique-peripheral')
        self.assertIsNotNone(usb_peripheral(USB, self.sys))
        other, _ = self.usb(port='1-3', address=5)
        self.assertIsNone(usb_peripheral(USB, self.sys))
        (other / 'serial').write_text('different')
        self.assertIsNotNone(usb_peripheral(USB, self.sys))
        (other / 'devnum').write_text('4')
        self.assertIsNone(usb_peripheral(USB, self.sys))

    def test_inventory_filters_raw_usb_while_serial_tty_remains_available(self):
        device, interface = self.usb(interface_class='08')
        dev = self.root / 'dev'
        raw = dev / 'bus/usb/001/004'
        raw.parent.mkdir(parents=True)
        raw.touch()
        tty = dev / 'ttyUSB0'
        tty.touch()
        actual_stat = Path.stat
        def metadata(path, *args, **kwargs):
            if path in (raw, tty):
                return SimpleNamespace(st_mode=stat.S_IFCHR | 0o600, st_rdev=os.makedev(189, 3) if path == raw else os.makedev(188, 0), st_gid=20)
            return actual_stat(path, *args, **kwargs)
        with patch.object(Path, 'stat', metadata):
            self.assertEqual([row['path'] for row in devices(dev, self.sys)], [str(tty)])
            (interface / 'bInterfaceClass').write_text('03')
            found = devices(dev, self.sys)
            self.assertEqual(len(found), 2)
            self.assertIn('identity', next(row for row in found if row['path'] == str(raw)))

    def test_compose_pins_identity_and_reused_address_never_passes_readiness(self):
        definition = apply({'services': {'app': {}}}, 'app', [SELECTED])
        self.assertEqual(json.loads(definition['services']['app']['labels']['io.titan.hardware']), [SELECTED])
        hardware_ready([SELECTED], [SELECTED])
        for changed in ([], [{**SELECTED, 'identity': 'b' * 64}], [{**SELECTED, 'group': 20}]):
            with self.assertRaises(Error): hardware_ready([SELECTED], changed)
        legacy = {key: value for key, value in SELECTED.items() if key != 'identity'}
        with self.assertRaisesRegex(Error, 'Identität'): hardware_ready([legacy], [SELECTED])

    def test_offline_guard_checks_sysfs_and_char_device_without_any_cli(self):
        device, interface = self.usb()
        identity = usb_peripheral(USB, self.sys)['identity']
        row = self.row({**SELECTED, 'identity': identity})
        node = SimpleNamespace(st_mode=stat.S_IFCHR | 0o600, st_rdev=os.makedev(189, 3))
        actual_stat = Path.stat
        def metadata(path, *args, **kwargs):
            return node if str(path) == USB else actual_stat(path, *args, **kwargs)
        with patch('subprocess.run', side_effect=AssertionError('Offline check must not activate a daemon')), patch.object(Path, 'stat', metadata):
            raw_usb_container_ready(row, self.sys)
            (device / 'serial').write_text('new-device-at-same-address')
            with self.assertRaisesRegex(Error, 'ersetzt'): raw_usb_container_ready(row, self.sys)
            (device / 'serial').write_text('unique-peripheral')
            (interface / 'bInterfaceClass').write_text('08')
            with self.assertRaises(Error): raw_usb_container_ready(row, self.sys)
            (interface / 'bInterfaceClass').write_text('03')
            node.st_rdev = os.makedev(8, 0)
            with self.assertRaises(Error): raw_usb_container_ready(row, self.sys)
            node.st_rdev = os.makedev(189, 4)
            with self.assertRaises(Error): raw_usb_container_ready(row, self.sys)

    def test_missing_fingerprint_and_changed_mapping_fail_closed(self):
        row = self.row()
        row['Config']['Labels'] = {}
        with self.assertRaisesRegex(Error, 'Identität'): raw_usb_container_ready(row)
        with self.assertRaisesRegex(Error, 'Identität'): native_hardware(row)
        row = self.row()
        row['HostConfig']['Devices'][0]['PathOnHost'] = '/dev/bus/usb/001/005'
        with self.assertRaisesRegex(Error, 'Gerätezuordnung'): native_hardware(row)


class AppUSBControllersTests(unittest.TestCase):
    def engine(self):
        from test_docker_engine import EngineTests
        fixture = EngineTests()
        fixture.setUp()
        self.addCleanup(fixture.doCleanups)
        return fixture

    def test_native_create_does_not_start_reused_usb_after_slow_pull(self):
        from test_docker_engine import ID
        fixture = self.engine()
        current = [SELECTED]
        def command(arguments, **kwargs):
            if arguments[0] == 'create': current[:] = [{**SELECTED, 'identity': 'b' * 64}]
            return ID
        fixture.engine.engine_docker.side_effect = command
        with patch('titan.app_devices.devices', side_effect=lambda: current), self.assertRaisesRegex(Error, 'ersetzt'):
            fixture.engine.op_docker_container_create({'name': 'web', 'image': 'nginx:stable', 'devices': [USB]})
        commands = [call.args[0] for call in fixture.engine.engine_docker.call_args_list]
        self.assertEqual([call[0] for call in commands], ['create'])
        labels = [command[index + 1] for command in commands for index, part in enumerate(command[:-1]) if part == '--label']
        self.assertEqual(json.loads(next(value.split('=', 1)[1] for value in labels if value.startswith('io.titan.hardware='))), [SELECTED])

    def test_hardware_edit_rechecks_selected_identity_after_commit_before_rename(self):
        from test_docker_engine import ID
        fixture = self.engine()
        fixture.engine.engine_container = Mock(return_value=fixture.manual_stopped())
        current = [SELECTED]
        def command(arguments, **kwargs):
            if arguments[0] == 'commit': current[:] = [{**SELECTED, 'identity': 'b' * 64}]
            return 'sha256:' + 'c' * 64
        fixture.engine.engine_docker.side_effect = command
        with patch('titan.app_devices.devices', side_effect=lambda: current), self.assertRaisesRegex(Error, 'ersetzt'):
            fixture.engine.op_docker_container_hardware(ID, [USB])
        self.assertFalse(any(call.args[0][0] in ('rename', 'update', 'start') for call in fixture.engine.engine_docker.call_args_list))

    def test_settings_rechecks_original_identity_after_commit_before_rename(self):
        from test_docker_settings import DockerSettingsTests
        from test_docker_engine import ID
        fixture = DockerSettingsTests()
        fixture.setUp(); fixture.configure()
        self.addCleanup(fixture.doCleanups)
        fixture.row['Config']['Labels']['io.titan.hardware'] = json.dumps([SELECTED])
        fixture.row['HostConfig']['Devices'] = [{'PathOnHost': USB, 'PathInContainer': USB, 'CgroupPermissions': 'rw'}]
        current = [SELECTED]
        def command(arguments, **kwargs):
            if arguments[0] == 'commit': current[:] = [{**SELECTED, 'identity': 'b' * 64}]
            return 'sha256:' + 'c' * 64
        fixture.engine.engine_docker.side_effect = command
        with patch('titan.app_devices.devices', side_effect=lambda: current), self.assertRaisesRegex(Error, 'ersetzt'):
            fixture.engine.op_docker_container_settings(ID, {'memory_mb': 768})
        self.assertFalse(any(call.args[0][0] in ('rename', 'update', 'start') for call in fixture.engine.engine_docker.call_args_list))

    def test_compose_rechecks_after_create_and_never_starts_replaced_hardware(self):
        from test_app_management import AppManagementTests
        fixture = AppManagementTests()
        fixture.setUp()
        self.addCleanup(fixture.tearDown)
        current = [SELECTED]
        original = fixture.command
        def command(arguments, **kwargs):
            result = original(arguments, **kwargs)
            if arguments[:2] == ['docker', 'compose'] and arguments[6:7] == ['create']:
                current[:] = [{**SELECTED, 'identity': 'b' * 64}]
            return result
        fixture.runner.side_effect = command
        with patch('titan.app_management.app_devices_inventory', return_value=[SELECTED]), patch('titan.app_devices.devices', side_effect=lambda: current):
            with self.assertRaisesRegex(Error, 'ersetzt'):
                fixture.host.op_app_install('jellyfin', 8096, hardware=[USB])
        self.assertEqual(fixture.containers['jellyfin']['State']['Status'], 'created')
        self.assertFalse(any(command[0] in ('start', 'up') for command in fixture.compose_commands()))
        saved = fixture.host.load('apps', [])[0]['hardware'][0]
        self.assertEqual(saved['identity'], SELECTED['identity'])

    def test_daemon_autostart_rejects_unpinned_usb_without_blocking_stopped_opt_out(self):
        from test_boot_memory_guard import OfflineBootMemoryTests
        fixture = OfflineBootMemoryTests()
        fixture.setUp()
        self.addCleanup(fixture.doCleanups)
        directory = fixture.container(policy='always')
        host = json.loads((directory / 'hostconfig.json').read_text())
        host['Devices'] = [{'PathOnHost': USB, 'PathInContainer': USB, 'CgroupPermissions': 'rw'}]
        fixture.write(directory / 'hostconfig.json', host)
        for policy in ('always', 'on-failure', 'unless-stopped'):
            host['RestartPolicy']['Name'] = policy
            fixture.write(directory / 'hostconfig.json', host)
            result = fixture.evaluate()
            self.assertFalse(result['ok'])
            self.assertEqual(result['reason'], 'invalid_state')
        config = json.loads((directory / 'config.v2.json').read_text())
        config['HasBeenManuallyStopped'] = True
        fixture.write(directory / 'config.v2.json', config)
        self.assertTrue(fixture.evaluate()['ok'])
        host['RestartPolicy']['Name'] = 'no'
        fixture.write(directory / 'hostconfig.json', host)
        self.assertTrue(fixture.evaluate()['ok'])


if __name__ == '__main__':
    unittest.main()
