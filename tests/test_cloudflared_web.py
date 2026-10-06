"""Cloudflared listener, credentials and legacy snapshot regressions."""
import copy
import json
import unittest
from unittest.mock import patch

from titan.app_networks import AppNetworkMixin
from titan.app_stores import bundled_stores
from titan.catalog import APPS, catalog, compose, observed_web_port, published_ports, validate_options
from titan.core import Error
from titan.docker_engine import DockerEngineMixin
from titan.store_recipes import recipes


class CloudflaredWebTests(unittest.TestCase):
    def setUp(self):
        source = next(row for row in bundled_stores() if any(app['id'] == 'cloudflared-web' for app in row['document']['apps']))
        self.item = copy.deepcopy(next(app for app in source['document']['apps'] if app['id'] == 'cloudflared-web'))
        self.document = {'schema': 1, 'name': 'Cloudflared test', 'apps': [self.item]}
        self.source = source['url']
        self.app, self.recipe = next(iter(recipes(self.document, self.source)[1].items()))
        self.recipe['docker_template'] = True
        self.options = {field['key']: 'My$Private-Password123' if field['type'] == 'password' else field['default']
                        for field in self.recipe['install_schema']}
        self.registry = patch.dict(APPS, {self.app: self.recipe})
        self.registry.start()
        self.addCleanup(self.registry.stop)

    def test_host_and_bridge_selected_port_drives_listener_mapping_and_reservation(self):
        self.assertEqual(self.recipe['default_port'], 14333)
        self.assertTrue(self.recipe['dynamic_web_port'])
        for mode in ('default', 'bridge', 'host'):
            for chosen in (14333, 20168):
                with self.subTest(mode=mode, chosen=chosen):
                    definition = compose(self.app, '/control/app', 991, 992, chosen, '/nas/data',
                                         self.options, network={'mode': mode})
                    service = definition['services'][self.app]
                    self.assertEqual(service['environment']['WEBUI_PORT'], str(chosen))
                    self.assertEqual(service['environment']['BASIC_AUTH_PASS'], 'My$$Private-Password123')
                    if mode == 'host':
                        self.assertEqual(service['network_mode'], 'host')
                        self.assertNotIn('ports', service)
                    else:
                        self.assertEqual(service['ports'], [f'{chosen}:{chosen}/tcp'])
                    self.assertEqual(published_ports(self.app, chosen, self.options, host_mode=mode == 'host'),
                                     [{'host': chosen, 'target': chosen, 'protocol': 'tcp', 'service': self.recipe['stack']['primary']}])

    def test_password_required_masked_and_public_catalog_has_no_private_value(self):
        password = next(field for field in self.recipe['install_schema'] if field['type'] == 'password')
        with self.assertRaises(Error):
            validate_options(self.app, {key: value for key, value in self.options.items() if key != password['key']})
        public = next(row for row in catalog()['apps'] if row['id'] == self.app)
        self.assertEqual(public['first_login']['mode'], 'install')
        self.assertNotIn('password', public['first_login'])
        self.assertNotIn(self.options[password['key']], json.dumps(public))

    def test_saved_legacy_snapshot_is_masked_without_mutation_or_password_rotation(self):
        original = copy.deepcopy(self.document)
        field = next(field for field in self.item['stack_fields'] if field['label'].startswith('BASIC_AUTH_PASS'))
        field.update(type='text', default='""', min_length=0)
        legacy = copy.deepcopy(self.document)
        _, rows = recipes(self.document, self.source)
        parsed = rows[self.app]
        masked = next(row for row in parsed['install_schema'] if row['key'] == field['key'])
        self.assertEqual(masked['type'], 'password')
        self.assertEqual(masked['default'], '')
        self.assertEqual(self.document, legacy)
        recipes(self.document, self.source)  # Re-reading does not add unsupported source keys.
        with patch.dict(APPS, rows):
            saved = {**self.options, field['key']: '""'}
            self.assertEqual(validate_options(self.app, saved), saved)
        self.assertNotEqual(original, legacy)

    def test_links_follow_observed_listener_instead_of_old_record_port(self):
        container = {'Id': 'a' * 64, 'Config': {
            'Env': ['WEBUI_PORT=18888', 'BASIC_AUTH_PASS=never-return-this'],
            'Labels': {'io.titan.managed': 'true', 'io.titan.app': self.app}},
            'State': {'Status': 'running'}, 'HostConfig': {'NetworkMode': 'host'}, 'NetworkSettings': {}}
        summary = AppNetworkMixin._app_address_summary(object(), container,
                    {'id': self.app, 'port': 20168, 'scheme': 'http'}, networks={},
                    addresses=[{'address': '192.168.10.18', 'family': 4}])
        self.assertEqual(summary['endpoints'][0]['url'], 'http://192.168.10.18:18888')
        self.assertEqual(DockerEngineMixin.engine_summary(container)['web_port'], 18888)
        self.assertNotIn('never-return-this', json.dumps(summary))
        self.assertNotIn('never-return-this', json.dumps(DockerEngineMixin.engine_summary(container)))
        for env in ([], ['WEBUI_PORT=0'], ['WEBUI_PORT=999999'], ['WEBUI_PORT=14333', 'WEBUI_PORT=18888']):
            container['Config']['Env'] = env
            self.assertIsNone(observed_web_port(self.app, container))
            observed = AppNetworkMixin._app_address_summary(object(), container,
                       {'id': self.app, 'port': 20168}, networks={},
                       addresses=[{'address': '192.168.10.18', 'family': 4}])
            self.assertEqual(observed['endpoints'], [])


if __name__ == '__main__':
    unittest.main()
