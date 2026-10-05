"""Deployment invariants for imported templates and offline store snapshots."""
import copy
import io
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch
import zipfile

from titan.app_packages import prepare_options
from titan.app_stores import StoreMixin
from titan.catalog import APPS, catalog, compose, published_ports, requested_host_ports, validate_options
from titan.compose_templates import translate
from titan.core import Error
from titan.store_recipes import recipes
from titan.store_sources import BIGBEAR, LINUXSERVER, casaos_document, linuxserver_document


class MemoryHost(StoreMixin):
    def __init__(self): self.values = {}
    def load(self, key, default): return self.values.get(key, default)
    def save(self, key, value): self.values[key] = copy.deepcopy(value)


class DockerTemplateTests(unittest.TestCase):
    def lsio(self):
        return {'name': 'example', 'stable': True, 'description': 'Example',
            'architectures': [{'arch': 'x86_64'}, {'arch': 'arm64'}],
            'config': {'ports': [
                {'internal': '8080', 'external': '8081', 'desc': 'HTTP Web UI', 'optional': False},
                {'internal': '1900/udp', 'external': '1900', 'desc': 'Discovery', 'optional': True}],
                'volumes': [{'path': '/config', 'optional': False}, {'path': '/movies', 'optional': False},
                            {'path': '/downloads', 'optional': True}],
                'env_vars': [{'name': 'PUID', 'value': '0'}, {'name': 'PASSWORD', 'value': 'public-secret', 'optional': False},
                             {'name': 'OPTIONAL_VALUE', 'value': 'disabled-by-default', 'optional': True}]}}

    def parse(self, item, source=LINUXSERVER):
        return next(iter(recipes({'schema': 1, 'name': 'Test', 'apps': [item]}, source)[1].items()))

    def test_lsio_multiple_mounts_optional_settings_and_safe_defaults(self):
        doc, skipped = linuxserver_document({'data': {'repositories': {'linuxserver': [self.lsio()]}}})
        self.assertFalse(skipped)
        app, recipe = self.parse(doc['apps'][0])
        with patch.dict(APPS, {app: recipe}):
            values = {field['key']: 'private-secret-123456789' for field in recipe['install_schema'] if field['type'] == 'password'}
            result = compose(app, '/control/app', 991, 992, 18080, '/nas/data', values, config_path='/nas/config')
            service = result['services'][app]
            self.assertEqual(service['ports'], ['18080:8080/tcp'])
            self.assertEqual({mount['target'] for mount in service['volumes']}, {'/config', '/movies'})
            self.assertEqual(service['environment']['PUID'], '991')
            self.assertNotIn('OPTIONAL_VALUE', service['environment'])
            optional_port = next(field for field in recipe['install_schema'] if field['type'] == 'number')
            optional_mount = next(field for field in recipe['install_schema'] if field['type'] == 'boolean')
            values.update({optional_port['key']: 11900, optional_mount['key']: True})
            service = compose(app, '/control/app', 991, 992, 18080, '/nas/data', values, config_path='/nas/config')['services'][app]
            self.assertIn('11900:1900/udp', service['ports'])
            self.assertIn('/downloads', {mount['target'] for mount in service['volumes']})
            self.assertTrue(all(mount['source'].startswith('/nas/') for mount in service['volumes']))
            self.assertEqual(recipe['architectures'], ['x86_64', 'arm64'])
        self.assertNotIn('public-secret', json.dumps(doc))

    def test_refresh_assigns_unique_all_port_defaults_and_preserves_optional_suggestions(self):
        first=self.lsio();second=copy.deepcopy(first);second['name']='example-two'
        document,skipped=linuxserver_document({'data':{'repositories':{'linuxserver':[first,second]}}})
        with patch.dict(APPS,{},clear=True):
            with patch('titan.app_stores.fetch_document',return_value=(copy.deepcopy(document),skipped)):
                refreshed,_=StoreMixin.store_document(LINUXSERVER)
            _,parsed=recipes(refreshed,LINUXSERVER);APPS.update(parsed)
            defaults=[]
            for item in parsed.values():
                defaults.append(item['default_port'])
                field=next(field for field in item['install_schema'] if field['type']=='number')
                self.assertNotIn('default',field);defaults.append(field['suggested_default'])
            self.assertEqual(len(defaults),len(set(defaults)))
            with patch('titan.app_stores.fetch_document',return_value=(copy.deepcopy(document),skipped)):
                again,_=StoreMixin.store_document(LINUXSERVER)
            self.assertEqual(again,refreshed)

    def test_dns_standard_ports_and_optional_low_ports_survive_translation_and_refresh(self):
        item=self.lsio();item['config']['ports']=[
            {'internal':'80','external':'80','desc':'HTTP Web UI'},
            {'internal':'53','external':'53','desc':'DNS TCP'},
            {'internal':'53/udp','external':'53','desc':'DNS UDP'},
            {'internal':'67/udp','external':'67','desc':'DHCP','optional':True}]
        document,_=linuxserver_document({'data':{'repositories':{'linuxserver':[item]}}})
        self.assertEqual(document['apps'][0]['default_port'],80)
        with patch.dict(APPS,{},clear=True),patch('titan.app_stores.fetch_document',return_value=(document,[])):
            refreshed,_=StoreMixin.store_document(LINUXSERVER)
            key,recipe=self.parse(refreshed['apps'][0]);recipe['docker_template']=True;APPS[key]=recipe
            options=prepare_options(key,{field['key']:'private-secret-123456789' for field in recipe['install_schema'] if field['type']=='password'})
            expected={(80,'tcp'),(53,'tcp'),(53,'udp')}
            self.assertEqual({(row['host'],row['protocol']) for row in published_ports(key,80,options)},expected)
            self.assertEqual({(row['host'],row['protocol']) for row in requested_host_ports(key,80)},expected)
            optional=next(field for field in recipe['install_schema'] if field['type']=='number' and field.get('optional'))
            self.assertEqual(optional['min'],1);self.assertEqual(optional['suggested_default'],67)
            options[optional['key']]=67
            self.assertIn((67,'udp'),{(row['host'],row['protocol']) for row in requested_host_ports(key,80,options)})
            definition=compose(key,'/control/'+key,991,992,80,'/nas/data',options)
            self.assertIn('53:53/tcp',definition['services'][key]['ports'])
            self.assertIn('53:53/udp',definition['services'][key]['ports'])

    def stack(self):
        return {'services': {'web': {'image': 'example/web:1', 'ports': ['8080:80'],
            'environment': {'DB_HOST': 'original-db', 'DB_PASSWORD': 'published-password', 'ADMIN_PASSWORD': 'published-password'},
            'depends_on': ['database'], 'volumes': ['web-data:/data']},
            'database': {'image': 'postgres:16', 'container_name': 'original-db',
            'environment': {'POSTGRES_PASSWORD': 'published-password'}, 'volumes': ['db-data:/var/lib/postgresql/data']}},
            'volumes': {'web-data': {'name': 'global-web-data', 'driver': 'local'}, 'db-data': {'name': 'global-db-data', 'driver': 'local'}}}

    def test_bigbear_shared_db_secret_generated_once_and_aliases_preserved(self):
        template = translate(self.stack(), 'example', 'owner/store', {'name': 'Example', 'port': '8080'})
        app, recipe = self.parse(template, BIGBEAR)
        with patch.dict(APPS, {app: recipe}):
            user = {field['key']: 'chosen-admin-password-123' if field['type'] == 'password' else field['default']
                    for field in recipe['install_schema'] if not field.get('generated')}
            options = prepare_options(app, user)
            result = compose(app, '/control/app', 991, 992, 18080, '/nas/data', options, config_path='/nas/config')
            web = result['services'][app]; database = result['services'][app + '-database']
            self.assertEqual(web['environment']['DB_PASSWORD'], database['environment']['POSTGRES_PASSWORD'])
            self.assertNotEqual(web['environment']['DB_PASSWORD'], web['environment']['ADMIN_PASSWORD'])
            self.assertEqual(web['environment']['ADMIN_PASSWORD'], 'chosen-admin-password-123')
            self.assertIn('original-db', database['networks']['default']['aliases'])
            self.assertNotEqual(database['container_name'], 'original-db')
            self.assertNotIn('global-db-data', json.dumps(result))
            self.assertEqual(prepare_options(app, user, options), options)
            with self.assertRaises(Error): compose(app, '/control/app', 991, 992, 18080, '/nas/data', user)
        self.assertNotIn('published-password', json.dumps(template))

    def test_special_host_access_broken_volumes_and_ambiguous_webports_are_rejected(self):
        for mutate in (
                lambda doc: doc['services']['web'].update(privileged=True),
                lambda doc: doc['services']['web'].update(volumes=['/var/run/docker.sock:/socket']),
                lambda doc: doc['services']['web'].update(volumes=['undeclared:/data']),
                lambda doc: doc['services']['web'].update(user='1000:1000'),
                lambda doc: doc.update(networks={'shared': {'external': True}}),
                lambda doc: doc['services']['web'].update(command='echo ${HOST_SECRET}')):
            source = self.stack(); mutate(source)
            with self.assertRaises((Error, ValueError)): translate(source, 'example', 'owner/store', {'port': '8080'})
        source = self.stack(); source['services']['web']['ports'].append('8443:443')
        with self.assertRaisesRegex(Error, 'eindeutiger Webport'): translate(source, 'example', 'owner/store')

    def test_archive_reads_compose_yaml_and_metadata_without_aliases_or_duplicate_keys(self):
        def archive(source):
            output = io.BytesIO()
            with zipfile.ZipFile(output, 'w') as store:
                store.writestr('Apps/example/compose.yaml', source)
                store.writestr('Apps/example/metadata.json', json.dumps({'name': 'Example', 'port': '8080'}))
            return output.getvalue()
        doc, skipped = casaos_document(archive(json.dumps(self.stack())), 'owner/store')
        self.assertFalse(skipped); self.assertEqual(doc['apps'][0]['name'], 'Example')
        for source in ('services: {}\nservices: {}\n', 'services: &source\n  app: *source\n'):
            with self.assertRaises(ValueError): casaos_document(archive(source), 'owner/store')

    def test_refresh_preserves_installed_same_id_recipe_and_offline_snapshot(self):
        host = MemoryHost(); url = 'https://raw.githubusercontent.com/example/store/main/catalog.json'
        template = translate(self.stack(), 'example', 'owner/store', {'port': '8080'})
        old = {'schema': 1, 'name': 'Example', 'apps': [template]}; new = copy.deepcopy(old)
        new['apps'][0]['image'] = 'example/web:2'; new['apps'][0]['stack']['services']['web']['image'] = 'example/web:2'
        key = self.parse(template, url)[0]
        try:
            with patch.object(host, 'store_document', return_value=(old, [])): host.op_app_store_add(url, trusted=True)
            host.values['apps'] = [{'id': key}]
            store = next(row for row in host.store_records() if row['url'] == url)
            with patch.object(host, 'store_document', return_value=(new, [])): host.op_app_store_refresh(store['id'])
            self.assertEqual(APPS[key]['image'], 'example/web:1')
            new['apps'][0]['image'] = 'example/web:3'; new['apps'][0]['stack']['services']['web']['image'] = 'example/web:3'
            with patch.object(host, 'store_document', return_value=(new, [])): host.op_app_store_refresh(store['id'])
            self.assertEqual(APPS[key]['image'], 'example/web:1')
            self.assertEqual(next(row for row in host.store_records() if row['id']==store['id'])['retained'][0]['image'],'example/web:1')
            APPS.pop(key); host.initialize_app_stores()
            self.assertEqual(APPS[key]['image'], 'example/web:1')
            before = copy.deepcopy(host.store_records())
            with patch.object(host, 'store_document', side_effect=Error('Offline')):
                with self.assertRaises(Error): host.op_app_store_refresh(store['id'])
            self.assertEqual(host.store_records(), before)
            host.op_app_store_toggle(store['id'], False)
            self.assertFalse(any(app['store_url'] == url for app in host.op_catalog()['apps']))
            self.assertEqual(host.op_catalog()['installed_recipes'][0]['id'], key)
        finally: APPS.pop(key, None)

    def test_bundled_templates_all_render_bounded_compose_with_complete_required_values(self):
        public = catalog()['apps']; self.assertGreater(len(public), 50)
        endpoints = set()
        for app in public:
            key = app['id']; recipe = APPS[key]
            user = {field['key']: (field.get('default') or 'test-private-value-123456789012345678901234567890')
                    for field in recipe['install_schema'] if field.get('required') and not field.get('generated')}
            values = prepare_options(key, user)
            with self.subTest(app=key):
                definition = compose(key, '/control/' + key, 991, 992, app['default_port'], '/nas/data/' + key,
                                     values, config_path='/nas/config/' + key)
                self.assertNotIn('stack', app); self.assertNotIn('environment', app)
                for field in app['install_schema']: self.assertFalse(field.get('generated'))
                for mapping in published_ports(key, app['default_port'], values):
                    endpoint = (mapping['host'], mapping['protocol'])
                    # Standard privileged ports are intentionally shared defaults
                    # for mutually exclusive DNS/HTTP applications. Installs must
                    # reserve and check the actual protocol-specific endpoints.
                    if mapping['host']>=1024:self.assertNotIn(endpoint, endpoints)
                    endpoints.add(endpoint)
                    self.assertNotIn(mapping['host'], {5000, 5001, 5101})
                for service in definition['services'].values():
                    self.assertNotIn('privileged', service); self.assertNotIn('devices', service)
                    self.assertTrue(service['mem_limit'])
                    self.assertTrue(all(mount['source'].startswith('/nas/') and mount['bind']['create_host_path'] is False
                                        for mount in service['volumes']))


if __name__ == '__main__': unittest.main()
