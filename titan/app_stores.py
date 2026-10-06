"""Explicitly trusted, bounded GitHub catalog imports; no arbitrary Compose execution."""
import hashlib
import contextlib
import copy
import json
from pathlib import Path
import re
import urllib.parse
import urllib.request
from .core import Error, integer
from .catalog import APPS, catalog, update_recipes, recipes_snapshot, PROTECTED_HOST_PORTS
from .store_sources import PRESETS, LINUXSERVER, BIGBEAR, fetch_document
from .store_recipes import text, recipes


def store_url(value):
    value = text(value, 1000)
    if re.fullmatch(r'https://github.com/[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+', value): return value
    if value == LINUXSERVER or re.fullmatch(r'https://codeload.github.com/[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+/zip/refs/heads/[A-Za-z0-9_.-]+', value):
        return value
    url = urllib.parse.urlsplit(value)
    if (url.scheme != 'https' or url.netloc != 'raw.githubusercontent.com' or url.query or url.fragment
            or not re.fullmatch(r'/[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+/[A-Za-z0-9_./-]+\.json', url.path)
            or '..' in url.path.split('/')):
        raise Error('Direkte HTTPS-URL einer Titan-Katalogdatei auf raw.githubusercontent.com angeben.')
    return value


class StoreMixin:
    def store_records(self):
        # New stack fields must never reach a previous version's startup parser.
        # Keep both old source registries intact for a system rollback.
        records = self.load('app-store-sources-v2', None)
        if records is None: records = self.load('app-store-sources', None)
        result=copy.deepcopy(records if records is not None else self.load('app-stores', []))
        for store in result:
            if 'icewhaletech/casaos-appstore' in store.get('url','').lower(): store.update(enabled=False, retired=True)
        # Bundled validated snapshots work offline; user choices override them.
        known={row.get('url') for row in result}
        for row in bundled_stores():
            if row['url'] not in known: result.append(row)
        return result

    def save_store_records(self, records):
        self.save('app-store-sources-v2', records)

    def initialize_app_stores(self):
        document = json.loads((Path(__file__).parent / 'titan-app-store.json').read_text())
        _, parsed = recipes(document, LINUXSERVER)
        for app in parsed.values(): app.update(titan_recipe=True,store_name='Titan AppStore',catalog_status='preparation')
        update_recipes(parsed)
        for store in self.store_records():
            _, parsed = recipes(store['document'], store_url(store['url']))
            for item in parsed.values(): item.update(docker_template=True, catalog_status='available')
            update_recipes(parsed)
            if store.get('retained'):
                _, archived = recipes({**store['document'], 'apps':store['retained']}, store['url'])
                for item in archived.values(): item.update(docker_template=True, catalog_status='available')
                update_recipes(archived)

    def op_catalog(self):
        result = catalog()
        enabled={row['url'] for row in self.store_records() if row.get('enabled',True) and not row.get('retired')}
        result['apps']=[row for row in result['apps'] if row.get('store_url') in enabled]
        installed = {row['id'] for row in self.load('apps', [])}
        result['installed_recipes'] = [app for app in catalog(include_legacy=True)['apps'] if app['id'] in installed]
        return result

    def op_app_stores(self):
        return {'stores':[{key:row.get(key) for key in ('id','name','url','enabled','skipped','snapshot','updated_at')} | {'apps':len(row['document']['apps'])} for row in self.store_records() if not row.get('retired')], 'presets':copy.deepcopy(PRESETS)}

    def op_app_store_toggle(self, store, enabled):
        if type(enabled) is not bool:
            raise Error('Store-Auswahl ist ungültig.')
        stores = self.store_records()
        if store == 'linuxserver' and not any(row['url'] == LINUXSERVER for row in stores):
            document = json.loads((Path(__file__).parent / 'titan-app-store.json').read_text())
            stores.append({'id':'linuxserver','name':'LinuxServer.io','url':LINUXSERVER,'document':document})
        found = next((row for row in stores if row['id'] == store), None)
        if found is None: raise Error('Store nicht gefunden.', 404)
        found['enabled'] = enabled
        self.save_store_records( stores)
        return {'ok': True, 'enabled': enabled}

    def op_app_store_refresh(self, store):
        stores = self.store_records()
        found = next((row for row in stores if row['id'] == store), None)
        if store == 'linuxserver' and found is None:
            return self.op_app_store_add(LINUXSERVER, trusted=True)
        if found is None: raise Error('Store nicht gefunden.', 404)
        # Fetch/validate outside the installation lock. Only publish the final
        # snapshot under the short configuration lock shared with registrations.
        document, skipped = self.store_document(found['url'])
        name, parsed = recipes(document, found['url'])
        with getattr(self,'app_config_lock',contextlib.nullcontext()):
            stores=self.store_records()
            found=next((row for row in stores if row['id']==store),None)
            if found is None: raise Error('Store wurde während der Aktualisierung entfernt.',409)
            installed={row['id'] for row in self.load('apps',[])}
            retained=[]
            # Prefer the already pinned version when a newer same-ID recipe was
            # fetched on a preceding refresh. It must survive repeated refreshes.
            originals={}
            for app in found['document']['apps']+found.get('retained',[]):
                _,old=recipes({**found['document'],'apps':[app]},found['url'])
                originals[next(iter(old))]=app
            for key,app in originals.items():
                if key in installed: retained.append(app)
            found.update(document=document,skipped=skipped,name=name,retained=retained)
            self.save_store_records(stores)
            changes=dict(parsed)
            if retained:
                _,pinned=recipes({**document,'apps':retained},found['url']);changes.update(pinned)
            for item in changes.values(): item.update(docker_template=True,catalog_status='available')
            old={key for key,value in recipes_snapshot().items() if value.get('store_url')==found['url']}
            update_recipes(changes,old-set(changes)-installed)
        return {'ok':True,'apps':len(parsed),'skipped':len(skipped),'installed_preserved':len(retained)}

    @staticmethod
    def store_document(url):
        try:
            document, skipped = fetch_document(url)
            if url == LINUXSERVER or url.startswith(('https://codeload.github.com/', 'https://github.com/')):
                valid = []
                for app in document['apps']:
                    try: recipes({**document, 'apps':[app]}, url)
                    except Error as exc: skipped.append({'name': app['name'], 'reason': str(exc)})
                    else: valid.append(app)
                # Retain chosen defaults across refreshes; assign free defaults
                # to newly discovered templates without changing internal ports.
                used = {5000, 5001, 5101}
                previous_recipes=recipes_snapshot()
                for row in previous_recipes.values():
                    used.add(row.get('default_port', row['port']))
                    used.update(field.get('default',field.get('suggested_default')) for field in row.get('install_schema', []) if field['type'] == 'number')
                candidate = 18000
                prefix = 's' + hashlib.sha256(url.encode()).hexdigest()[:10] + '-'
                def free_port():
                    nonlocal candidate
                    while candidate in used: candidate += 1
                    if candidate>65535: raise Error('Keine freien Standardports im Katalog verfügbar.')
                    value=candidate;used.add(value);candidate+=1
                    return value
                for app in valid:
                    previous = previous_recipes.get(prefix + app['id'], {})
                    if previous:
                        app['default_port'] = previous['default_port']
                    elif app['default_port']<1024 and (app['default_port'],'tcp') not in PROTECTED_HOST_PORTS:
                        # DNS/HTTP standard ports must retain their protocol
                        # meaning; actual conflicts are checked at installation.
                        used.add(app['default_port'])
                    else:
                        app['default_port'] = free_port()
                    for index, entry in enumerate(app.get('ports', [])):
                        prior = next((field for field in previous.get('install_schema', []) if field['key'] == f'port_{index}'), None)
                        if prior: entry['published'] = prior['default']
                        else: entry['published'] = free_port()
                    # Compose-backed adapters expose complete port fields instead
                    # of the legacy single-container `ports` array. Preserve their
                    # defaults across refresh and allocate unique new suggestions.
                    for field in app.get('stack_fields',[]):
                        if field['type']!='number':continue
                        prior=next((entry for entry in previous.get('install_schema',[]) if entry['key']==field['key'] and entry['type']=='number'),None)
                        value=prior.get('default',prior.get('suggested_default')) if prior else None
                        original=field.get('default',field.get('suggested_default'))
                        protocol=next((mapping['protocol'] for mapping in app.get('stack_ports',[]) if mapping['option']==field['key']),'tcp')
                        if value is None and original is not None and original<1024 and (original,protocol) not in PROTECTED_HOST_PORTS:value=original;used.add(value)
                        if value is None:value=free_port()
                        field['default' if field.get('required') or 'default' in field else 'suggested_default']=value
                document['apps'] = valid
                if url == LINUXSERVER:
                    curated = {row.get('upstream_name', key) for key, row in previous_recipes.items() if not row.get('store_url')}
                    skipped = [row for row in skipped if row['name'] not in curated]
            return document, skipped
        except (OSError, ValueError, KeyError, TypeError) as exc:
            raise Error('Store konnte nicht geladen werden: ' + type(exc).__name__) from None

    def op_app_store_add(self, url, trusted=False):
        if trusted is not True:
            raise Error('Vertrauen in den Store ausdrücklich bestätigen.')
        url = store_url(url)
        if 'icewhaletech/casaos-appstore' in url.lower(): raise Error('Der CasaOS-Store wird nicht mehr angeboten. LinuxServer.io bleibt der Standard.')
        stores = self.store_records()
        if any(row['url'] == url for row in stores):
            raise Error('Store ist bereits hinzugefügt. Vorlagen bleiben bis zum Entfernen unverändert.', 409)
        if len(stores) >= 20:
            raise Error('Maximal 20 eigene Stores.')
        document, skipped = self.store_document(url)
        name, parsed = recipes(document, url)
        identifier = hashlib.sha256(url.encode()).hexdigest()[:10]
        self.save_store_records( stores + [{'id': identifier, 'name': name, 'url': url, 'document': document, 'enabled': True, 'skipped': skipped}])
        for item in parsed.values(): item.update(docker_template=True,catalog_status='available')
        update_recipes(parsed)
        return {'ok': True, 'name': name, 'apps': len(parsed)}

    def op_app_store_remove(self, store):
        stores = self.store_records()
        found = next((row for row in stores if row['id'] == store), None)
        if found is None:
            raise Error('Store nicht gefunden.', 404)
        if found['url'] == LINUXSERVER:
            raise Error('Der Standardstore kann deaktiviert, aber nicht entfernt werden.', 409)
        _, parsed = recipes({**found['document'], 'apps':found['document']['apps'] + found.get('retained', [])}, found['url'])
        if any(row['id'] in parsed for row in self.load('apps', [])):
            raise Error('Zuerst die installierten Apps dieses Stores entfernen. Deren Daten bleiben erhalten.', 409)
        self.save_store_records( [row for row in stores if row['id'] != store])
        for identifier in parsed:
            update_recipes({},[identifier])
        return {'ok': True}


def bundled_stores():
    path=Path(__file__).parent/'docker-template-catalog.json'
    if not path.is_file(): return []
    document=json.loads(path.read_text())
    return [{**copy.deepcopy(row),'enabled':True} for row in document['sources']]
