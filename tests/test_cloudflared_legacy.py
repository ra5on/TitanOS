"""Real managed-app paths for previously installed Cloudflared templates."""
import copy
import json
from pathlib import Path
from unittest.mock import patch
import unittest

import test_app_management as management_fixtures
import test_app_networks as network_fixtures
from titan.app_stores import bundled_stores
from titan.catalog import APPS, validate_options
from titan.core import Error
from titan.store_recipes import recipes


class CloudflaredLegacyTests(unittest.TestCase):
    network = staticmethod(network_fixtures.AppNetworkTests.network)

    def command(self, arguments, **kwargs):
        result = network_fixtures.AppNetworkTests.command(self, arguments, **kwargs)
        # Multi-container-capable app startup uses Compose up --no-recreate.
        # The generic fixture updates bridge allocation only for start/restart;
        # an actual up also allocates endpoint IDs and bridge addresses.
        if arguments[:2] == ['docker', 'compose'] and '-f' in arguments and arguments[6] == 'up':
            app = arguments[3][6:]
            item = self.containers[app]
            item['State']['Running'] = True
            for name, attached in item['NetworkSettings']['Networks'].items():
                if name == 'host':
                    continue
                info = self.networks[name]
                attached.update(NetworkID=info['Id'], EndpointID='e' * 64,
                                IPAddress=attached.get('IPAddress') or '172.17.0.10')
        return result

    def setUp(self):
        network_fixtures.AppNetworkTests.setUp(self)
        source = next(row for row in bundled_stores()
                      if any(app['id'] == 'cloudflared-web' for app in row['document']['apps']))
        fresh = copy.deepcopy(next(app for app in source['document']['apps']
                                   if app['id'] == 'cloudflared-web'))
        self.source = source['url']
        self.document = {'schema': 1, 'name': 'Cloudflared legacy', 'apps': [fresh]}
        self.app, self.fresh_recipe = next(iter(recipes(self.document, self.source)[1].items()))
        self.fresh_recipe['docker_template'] = True
        legacy_document = copy.deepcopy(self.document)
        legacy_document['apps'][0]['default_port'] = 20168
        password = next(field for field in legacy_document['apps'][0]['stack_fields']
                        if field['label'].startswith('BASIC_AUTH_PASS'))
        password.update(type='text', default='""', min_length=0)
        self.legacy_recipe = next(iter(recipes(legacy_document, self.source)[1].values()))
        self.legacy_recipe['docker_template'] = True
        self.old_recipe = copy.deepcopy(self.legacy_recipe)
        self.old_recipe.pop('dynamic_web_port', None)
        self.old_recipe.pop('web_port_option', None)
        self.old_recipe['install_schema'] = copy.deepcopy(legacy_document['apps'][0]['stack_fields'])
        self.registry = patch.dict(APPS, {self.app: self.legacy_recipe})
        self.registry.start()

    def tearDown(self):
        self.registry.stop()
        management_fixtures.AppManagementTests.tearDown(self)

    def make_container(self, app, state='running'):
        # The Docker fixture must read the saved Compose file, as the actual
        # create/start command does. Re-rendering with the new recipe here would
        # hide an unwanted legacy listener/port migration during this test.
        item = network_fixtures.AppNetworkTests.make_container(self, app, state)
        definition = json.loads((self.host.directory / 'apps' / app / 'compose.json').read_text())
        service = definition['services'][app]
        item['Config']['Env'] = [key + '=' + str(value).replace('$$', '$')
                                 for key, value in service.get('environment', {}).items()]
        bindings = {}
        for mapping in service.get('ports', []):
            published, target = mapping.split(':')
            if '/' not in target:
                target += '/tcp'
            bindings[target] = [{'HostIp': '', 'HostPort': published}]
        item['HostConfig']['PortBindings'] = bindings
        item['NetworkSettings']['Ports'] = {
            key: [{'HostIp': '0.0.0.0', 'HostPort': binding['HostPort']} for binding in values]
            for key, values in bindings.items()}
        return item

    def legacy_install(self, mode='default', listener=14333, password='""'):
        port = 14333 if mode == 'host' else 20168
        values = {field['key']: field.get('default', '') for field in self.old_recipe['install_schema']}
        values['stack_env_0'] = str(listener)
        values['stack_env_6'] = password
        # Model installation by the earlier release, before its replacement
        # began requiring Basic Auth on newly installed tunnel interfaces.
        with patch.dict(APPS, {self.app: self.old_recipe}), \
                patch.object(self.host, '_app_require_web_password', return_value=None):
            self.host._prepare_app_install(self.app, port, options=values, network={'mode': mode})
            self.make_container(self.app, 'running')
            self.host.managed_app(self.app)
        self.path = self.host.directory / 'apps' / self.app / 'compose.json'
        self.options_path = self.path.with_name('options.json')
        self.saved_definition = self.path.read_bytes()
        self.saved_options = self.options_path.read_bytes()
        self.calls.clear()
        return values

    def check_legacy_lifecycle(self, mode, listener=14333):
        self.legacy_install(mode, listener)
        self.host.managed_app(self.app)
        details = self.host.op_app_details(self.app)
        self.assertEqual(details['app']['state'], 'running')
        self.assertEqual(details['container']['endpoints'][0]['port'],
                         listener if mode == 'host' else 20168)
        for action in ('stop', 'start', 'restart'):
            self.host.op_app_action(self.app, action)
            self.assertEqual(self.path.read_bytes(), self.saved_definition)
            self.assertEqual(self.options_path.read_bytes(), self.saved_options)
        self.assertEqual(self.containers[self.app]['State']['Status'], 'running')
        actual_listener = next(value.split('=', 1)[1] for value in self.containers[self.app]['Config']['Env']
                               if value.startswith('WEBUI_PORT='))
        self.assertEqual(actual_listener, str(listener))
        # Recreate a missing owned container using the saved legacy definition.
        self.containers.clear()
        self.host.op_app_action(self.app, 'start')
        self.assertEqual(self.path.read_bytes(), self.saved_definition)
        self.assertEqual(self.options_path.read_bytes(), self.saved_options)
        self.assertIn('WEBUI_PORT=' + str(listener), self.containers[self.app]['Config']['Env'])
        self.assertEqual(self.host.op_app_details(self.app)['app']['state'], 'running')

    def test_legacy_default_bridge_management_and_lifecycle_keep_saved_mapping(self):
        self.check_legacy_lifecycle('default')

    def test_legacy_explicit_bridge_management_and_lifecycle_keep_saved_mapping(self):
        self.check_legacy_lifecycle('bridge')

    def test_legacy_host_custom_listener_management_and_lifecycle_keep_saved_port(self):
        self.check_legacy_lifecycle('host', listener=18888)

    def test_explicit_settings_change_migrates_port_without_rotating_saved_password(self):
        self.legacy_install(password='existing-private-password')
        record = copy.deepcopy(self.host.managed_app(self.app))
        marker = Path(record['data']) / 'keep-data.txt'
        marker.write_text('unchanged data')
        self.host.op_app_action(self.app, 'stop')
        self.host.op_app_settings(self.app, 18889, options=None)
        definition = json.loads(self.path.read_text())['services'][self.app]
        self.assertEqual(definition['environment']['WEBUI_PORT'], '18889')
        self.assertEqual(definition['ports'], ['18889:18889/tcp'])
        self.assertEqual(json.loads(self.options_path.read_text())['stack_env_6'], 'existing-private-password')
        current = self.host.managed_app(self.app)
        self.assertEqual(current['port'], 18889)
        self.assertEqual(current['data'], record['data'])
        self.assertEqual(current['config_path'], record['config_path'])
        self.assertEqual(marker.read_text(), 'unchanged data')
        self.host.op_app_action(self.app, 'start')
        self.assertEqual(self.host.op_app_details(self.app)['container']['endpoints'][0]['port'], 18889)

    def test_unrelated_unsafe_definition_changes_remain_rejected(self):
        self.legacy_install()
        original = json.loads(self.saved_definition)
        for change in ({'privileged': True}, {'image': 'evil/image:1'}, {'cpus': 999},
                       {'environment': {'WEBUI_PORT': '14333', 'INJECTED': 'changed'}}):
            with self.subTest(change=change):
                changed = copy.deepcopy(original)
                changed['services'][self.app].update(change)
                self.path.write_text(json.dumps(changed))
                with self.assertRaises(Error):
                    self.host.managed_app(self.app)
                self.calls.clear()
                with self.assertRaises(Error):
                    self.host.op_app_action(self.app, 'start')
                self.assertFalse(any(call[:2] == ['docker', 'compose'] for call in self.calls))
        self.path.write_bytes(self.saved_definition)

    def test_legitimately_empty_legacy_password_remains_readable_and_unrotated(self):
        self.legacy_install(password='')
        self.host.managed_app(self.app)
        self.assertEqual(self.host._app_options(self.app)['stack_env_6'], '')
        self.host.op_app_action(self.app, 'stop')
        self.host.op_app_action(self.app, 'start')
        self.assertEqual(self.options_path.read_bytes(), self.saved_options)
        self.assertEqual(self.path.read_bytes(), self.saved_definition)
        self.assertEqual(self.host.op_app_details(self.app)['app']['state'], 'running')

    def test_record_without_digest_still_needs_and_accepts_exact_legacy_definition(self):
        self.legacy_install()
        self.host._app_patch_record(self.app, {'definition_digest': None})
        self.host.managed_app(self.app)
        self.assertEqual(self.host.op_app_details(self.app)['app']['state'], 'running')
        self.host.op_app_action(self.app, 'stop')
        self.host.op_app_action(self.app, 'start')
        self.assertEqual(self.path.read_bytes(), self.saved_definition)
        changed = json.loads(self.saved_definition)
        changed['services'][self.app]['privileged'] = True
        self.path.write_text(json.dumps(changed))
        with self.assertRaises(Error):
            self.host.managed_app(self.app)

    def test_details_redact_legacy_password_in_actual_log_response_and_settings(self):
        secret = 'legacy-private-password'
        self.legacy_install(password=secret)
        self.logs.return_value = 'ENVIRONMENT: BASIC_AUTH_PASS=' + secret
        details = self.host.op_app_details(self.app)
        self.assertNotIn(secret, json.dumps(details))
        self.assertIn('[ausgeblendet]', details['logs'])
        self.assertFalse(any(field['key'] == 'stack_env_6' for field in details['settings']))

    def test_custom_legacy_host_listener_is_reserved_against_other_apps(self):
        self.legacy_install('host', listener=18888)
        record = self.host.managed_app(self.app)
        self.assertEqual([(entry['host'], entry['target']) for entry in self.host._app_installed_ports(record)],
                         [(18888, 18888)])
        self.calls.clear()
        with self.assertRaises(Error) as rejected:
            self.host._prepare_app_install('heimdall', 18888)
        self.assertEqual(rejected.exception.status, 409)
        self.assertFalse((self.host.directory / 'apps' / 'heimdall').exists())
        self.assertFalse(any(call[:2] == ['docker', 'compose'] for call in self.calls))

    def test_hardware_edit_keeps_saved_legacy_listener_and_password(self):
        self.legacy_install('host', listener=18888, password='legacy-private-password')
        self.host.op_app_action(self.app, 'stop')
        self.host.op_app_hardware(self.app, [])
        self.assertEqual(self.path.read_bytes(), self.saved_definition)
        self.assertEqual(self.options_path.read_bytes(), self.saved_options)
        self.host.op_app_action(self.app, 'start')
        self.assertEqual(self.host.op_app_details(self.app)['container']['endpoints'][0]['port'], 18888)

    def test_new_install_from_old_cached_schema_cannot_disable_auth_with_empty_password(self):
        values = {field['key']: field.get('default', '') for field in self.legacy_recipe['install_schema']}
        values['stack_env_6'] = ''
        with self.assertRaises(Error):
            self.host._prepare_app_install(self.app, 14333, options=values, network={'mode': 'host'})
        self.assertFalse((self.host.directory / 'apps' / self.app).exists())
        self.assertFalse(any(call[:2] == ['docker', 'compose'] and len(call) > 6
                             and call[6] in ('create', 'up', 'pull', 'start') for call in self.calls))

    def test_fresh_installation_still_requires_nonempty_password(self):
        with patch.dict(APPS, {self.app: self.fresh_recipe}):
            values = {field['key']: field.get('default', '') for field in self.fresh_recipe['install_schema']}
            with self.assertRaises(Error):
                validate_options(self.app, values)
            self.calls.clear()
            with self.assertRaises(Error):
                self.host._prepare_app_install(self.app, 14333, options=values, network={'mode': 'host'})
            self.assertFalse(any(call[:2] == ['docker', 'compose'] for call in self.calls))
            self.assertFalse((self.host.directory / 'apps' / self.app).exists())


if __name__ == '__main__':
    unittest.main()
