"""Read public store metadata and translate supported single-container recipes.

No upstream Compose document is executed. Host paths, commands and privileges
never cross the adapter boundary. Unsupported templates are reported explicitly.
"""
import hashlib
import io
import json
import re
import urllib.request
import zipfile
from .core import Error

LINUXSERVER = 'https://api.linuxserver.io/api/v1/images?include_config=true&include_deprecated=false'
BIGBEAR = 'https://github.com/bigbeartechworld/big-bear-dockge'
PRESETS = [{'id': 'linuxserver', 'name': 'LinuxServer.io', 'url': LINUXSERVER, 'format': 'linuxserver', 'default': True},
           {'id': 'bigbear', 'name': 'Big Bear', 'url': BIGBEAR, 'format': 'compose', 'default': True}]

def slug(value):
    name = re.sub('[^a-z0-9_-]', '-', str(value).lower()).strip('-')
    if not name or not name[0].isalpha(): name = 'app-' + name
    return name if len(name) <= 18 else name[:11] + '-' + hashlib.sha256(name.encode()).hexdigest()[:6]

def line(value, limit=500):
    return re.sub(r'\s+', ' ', str(value or '')).strip()[:limit] or 'App des ausgewählten Stores.'

def port(value):
    match = re.fullmatch(r'([0-9]{1,5})(?:/(tcp|udp))?', str(value))
    if not match or not 1 <= int(match[1]) <= 65535: raise ValueError('Portbereiche oder dynamische Ports')
    return int(match[1]), match[2] or 'tcp'

def login(name):
    return ('Diese importierte Vorlage hat noch keinen von Titan geprüften Standardzugang. Anmeldung und Ersteinrichtung stehen in der direkt verlinkten Anleitung des Herausgebers. ' + name)[:2000]

def linuxserver_document(data):
    """Translate documented API config; examples never become host paths."""
    items = data['data']['repositories']['linuxserver']
    if not isinstance(items, list) or len(items) > 500: raise ValueError('Ungültiger LinuxServer-Katalog')
    apps, skipped = [], []
    for item in items:
        name = item.get('name', '') if isinstance(item, dict) else ''
        try:
            if not isinstance(item, dict) or not re.fullmatch(r'[a-z0-9][a-z0-9_.-]{0,80}', name): raise ValueError('Ungültiger Image-Name')
            if item.get('deprecated') or not item.get('stable'): raise ValueError('Kein stabiles Image')
            cfg = item.get('config') or {}
            for key in ('caps', 'devices', 'networking', 'hostname', 'security_opt', 'privileged', 'mac_address'):
                if cfg.get(key): raise ValueError('Besondere Host- oder Geräteanforderungen: ' + key)
            if any(not row.get('optional') for row in cfg.get('custom', [])): raise ValueError('Zusätzliche Laufzeitoptionen erforderlich')
            raw_ports = cfg.get('ports', [])
            if not isinstance(raw_ports, list) or len(raw_ports) > 16: raise ValueError('Zu viele Ports')
            web = next((row for row in raw_ports if not row.get('optional') and port(row['internal'])[1] == 'tcp' and re.search(r'web|gui|http|interface', row.get('desc', ''), re.I)), None)
            if web is None: raise ValueError('Keine eindeutige Weboberfläche')
            target, _ = port(web['internal']); external = port(web['external'])[0]
            service = {'image': 'lscr.io/linuxserver/' + name + ':latest', 'environment': {'PUID': '@uid', 'PGID': '@gid', 'TZ': 'Europe/Berlin'}, 'mounts': [], 'ports': []}
            settings, extras, bindings, port_bindings = [], [], [], []
            endpoints = set()
            for index, row in enumerate(raw_ports):
                internal, protocol = port(row['internal']); published = port(row['external'])[0]
                if (internal, protocol) in endpoints: raise ValueError('Doppelte Containerports')
                endpoints.add((internal, protocol))
                mapping = {'target': internal, 'published': published, 'protocol': protocol}
                summary = {'service': 'app', 'target': internal, 'published': published, 'protocol': protocol, 'optional': bool(row.get('optional')), 'help': line(row.get('desc'), 500)}
                if row is not web:
                    key = 'stack_port_app_' + str(internal) + '_' + protocol
                    optional = bool(row.get('optional'))
                    mapping['option'] = key
                    field = {'key': key, 'label': 'Port ' + str(internal) + '/' + protocol, 'type': 'number', 'min': 1, 'max': 65535, 'required': not optional, 'optional': optional, 'enabled_by_default': not optional, 'help': line(row.get('desc'), 500)}
                    if not optional: field['default'] = published
                    else: field['suggested_default'] = published
                    settings.append(field); extras.append({'option': key, 'target': internal, 'protocol': protocol, 'service': 'app'}); summary['option'] = key
                service['ports'].append(mapping); port_bindings.append(summary)
            volumes = cfg.get('volumes', [])
            if not isinstance(volumes, list) or len(volumes) > 16: raise ValueError('Zu viele Speicherzuordnungen')
            targets = set()
            for index, row in enumerate(volumes):
                path = row['path']
                if not isinstance(path, str) or not re.fullmatch(r'/[a-zA-Z0-9_./-]{1,160}', path) or '..' in path.split('/') or path.startswith(('/dev', '/proc', '/sys', '/etc', '/lib', '/var/run', '/run')) or path in targets: raise ValueError('Hostabhängige oder doppelte Datenziele')
                targets.add(path)
                slot = 'config' if path == '/config' else 'data' if path == '/data' else 'mount-' + str(index + 1)
                mount = {'slot': slot, 'target': path, 'readonly': False}
                binding = {'slot': slot, 'target': path, 'readonly': False, 'optional': bool(row.get('optional')), 'help': line(row.get('desc'), 500)}
                if row.get('optional'):
                    key = 'stack_mount_' + str(index)
                    mount['option'] = key; binding['option'] = key
                    settings.append({'key': key, 'label': 'Ordner ' + path + ' einbinden', 'type': 'boolean', 'default': False, 'required': False, 'optional': True, 'enabled_by_default': False, 'help': line(row.get('desc'), 500)})
                service['mounts'].append(mount); bindings.append(binding)
            for env in cfg.get('env_vars', []):
                key = env['name']
                if key in ('PUID', 'PGID', 'TZ'): continue
                if not isinstance(key, str) or not re.fullmatch(r'[A-Za-z_][A-Za-z0-9_]{0,63}', key): raise ValueError('Ungültige Umgebungsvariable')
                secret = bool(re.search(r'password|secret|token|api.?key|app_key', key, re.I)); optional = bool(env.get('optional'))
                value = str(env.get('value') or '')
                if '$' in value or '[' in value or '<' in value and '>' in value: value = ''
                option = 'stack_env_' + str(len(settings))
                field = {'key': option, 'label': key, 'type': 'password' if secret else 'text', 'required': not optional, 'min_length': 0 if optional or value else 1, 'max_length': 1000, 'optional': optional, 'enabled_by_default': not optional, 'help': line(env.get('desc'), 500)}
                if not optional: field['default'] = '' if secret else value
                elif value and not secret: field['suggested_default'] = value
                if key == 'APP_KEY' and not optional: field.update(generated=True, generator='base64-key', min_length=32)
                settings.append(field); service['environment'][key] = '@option:' + option
            from .compose_templates import validate_stack
            stack = validate_stack({'primary': 'app', 'services': {'app': service}})
            architectures = [row['arch'] for row in item.get('architectures', []) if isinstance(row, dict) and row.get('arch') in ('amd64','x86_64','arm64','aarch64','armhf')]
            apps.append({'id': slug(name), 'name': name, 'description': line(item.get('description')), 'image': service['image'], 'port': target,
                'scheme': 'https' if re.search(r'\bhttps\b', web.get('desc', ''), re.I) and not re.search(r'\bhttp\b', web.get('desc', ''), re.I) else 'http', 'default_port': external, 'mount': None, 'config_mount': True,
                'documentation': 'https://docs.linuxserver.io/images/docker-' + name + '/', 'login_note': login(name), 'stack': stack, 'stack_fields': settings, 'stack_ports': extras, 'storage_bindings': bindings, 'port_bindings': port_bindings,
                'architectures': list(dict.fromkeys(architectures)), 'upstream_version': line(item.get('version') or 'latest', 128), 'template_status': 'metadata-validated'})
        except (ValueError, KeyError, TypeError, AttributeError, Error) as exc:
            skipped.append({'name': line(name, 80), 'reason': line(str(exc), 200)})
    return {'schema': 1, 'name': 'LinuxServer.io', 'apps': apps}, skipped

def localized(value):
    return value.get('de_DE') or value.get('en_US') or next(iter(value.values()), '') if isinstance(value,dict) else value

def casaos_document(raw, name):
    import yaml
    apps, skipped = [], []
    with zipfile.ZipFile(io.BytesIO(raw)) as archive:
        files = archive.infolist()
        if len(files) > 12000: raise ValueError('Zu viele Archiveinträge')
        total = sum(row.file_size for row in files)
        if total > 512 * 1024**2: raise ValueError('Archiv entpackt zu groß')
        for row in files:
            if not row.filename.endswith(('docker-compose.yml','docker-compose.yaml','compose.yml','compose.yaml')): continue
            label = row.filename.split('/')[-2]
            try:
                if row.file_size > 128 * 1024: raise ValueError('Vorlage zu groß')
                source = archive.read(row)
                # Reject aliases; bounded YAML can still expand recursively.
                if any(isinstance(token, (yaml.tokens.AliasToken, yaml.tokens.AnchorToken)) for token in yaml.scan(source)): raise ValueError('YAML-Verweise nicht unterstützt')
                doc = safe_yaml(source)
                metadata_path = row.filename.rsplit('/', 1)[0] + '/metadata.json'
                metadata = json.loads(archive.read(metadata_path)) if metadata_path in archive.namelist() else None
                from .compose_templates import translate
                apps.append(translate(doc,label,name,metadata))
            except (ValueError, KeyError, TypeError, AttributeError, Error, yaml.YAMLError) as exc:
                skipped.append({'name':line(label,80),'reason':line(str(exc),200)})
    if not apps: raise ValueError('Keine kompatiblen App-Vorlagen gefunden')
    return {'schema':1,'name':name.split('/')[-1],'apps':apps}, skipped

def download(url, limit):
    class NoRedirect(urllib.request.HTTPRedirectHandler):
        def redirect_request(self,*args,**kwargs): raise Error('Store-Weiterleitungen sind nicht erlaubt.')
    with urllib.request.build_opener(NoRedirect).open(urllib.request.Request(url,headers={'User-Agent':'Titan-AppStore/2'}),timeout=30) as response:
        raw=response.read(limit+1)
    if len(raw)>limit: raise Error('Store-Download überschreitet das Größenlimit.')
    return raw

def github_document(url):
    # Read only small deployment files; store artwork is never downloaded.
    from concurrent.futures import ThreadPoolExecutor
    repo = url.removeprefix('https://github.com/')
    if not re.fullmatch(r'[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+', repo): raise Error('GitHub-Store ungültig.')
    info = json.loads(download('https://api.github.com/repos/' + repo, 128*1024))
    branch = info['default_branch']
    if not re.fullmatch(r'[A-Za-z0-9_.-]+', branch): raise Error('Store-Branch nicht unterstützt.')
    commit=json.loads(download('https://api.github.com/repos/'+repo+'/commits/'+branch, 1024*1024)).get('sha')
    if not re.fullmatch(r'[a-f0-9]{40}',str(commit)): raise Error('Store-Snapshot konnte nicht bestimmt werden.')
    tree = json.loads(download('https://api.github.com/repos/' + repo + '/git/trees/' + commit + '?recursive=1', 4*1024**2))
    if tree.get('truncated'): raise Error('Store-Verzeichnis ist zu groß.')
    if not re.fullmatch(r'[a-f0-9]{40}', str(tree.get('sha', ''))): raise Error('Store-Snapshot konnte nicht bestimmt werden.')
    paths = [row['path'] for row in tree['tree'] if row.get('type') == 'blob' and row['path'].endswith(('docker-compose.yml', 'docker-compose.yaml', 'compose.yml', 'compose.yaml', 'metadata.json')) and '/Apps/' in '/' + row['path']]
    if not paths or len(paths) > 2000: raise Error('Store benötigt 1 bis 1000 App-Vorlagen.')
    def get(path):
        if not re.fullmatch(r'[A-Za-z0-9_./-]+', path) or '..' in path.split('/'): raise Error('Unsicherer Vorlagenpfad.')
        return path, download('https://raw.githubusercontent.com/' + repo + '/' + commit + '/' + path, 128*1024)
    content = io.BytesIO()
    with zipfile.ZipFile(content, 'w', compression=zipfile.ZIP_DEFLATED) as archive:
        total = 0
        with ThreadPoolExecutor(max_workers=6) as pool:
            for path, raw in pool.map(get, paths):
                total += len(raw)
                if total > 16*1024**2: raise Error('Store-Vorlagen überschreiten 16 MiB.')
                archive.writestr(path, raw)
    return casaos_document(content.getvalue(), repo)


def fetch_document(url):
    if url.startswith('https://github.com/'):
        return github_document(url)
    if url == LINUXSERVER:
        return linuxserver_document(json.loads(download(url,8*1024**2)))
    if url.startswith('https://codeload.github.com/'):
        match=re.fullmatch(r'https://codeload.github.com/([A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+)/zip/refs/heads/[A-Za-z0-9_.-]+',url)
        if not match: raise Error('GitHub-Archiv-URL ist ungültig.')
        return casaos_document(download(url,96*1024**2),match[1])
    return json.loads(download(url,1024**2)), []


def safe_yaml(source):
    import yaml
    class UniqueLoader(yaml.SafeLoader):
        pass
    def mapping(loader, node, deep=False):
        result = {}
        for key_node, value_node in node.value:
            key = loader.construct_object(key_node, deep=deep)
            if not isinstance(key, (str, int)) or key in result: raise ValueError('Doppelte oder ungültige YAML-Schlüssel')
            result[key] = loader.construct_object(value_node, deep=deep)
        return result
    UniqueLoader.add_constructor(yaml.resolver.BaseResolver.DEFAULT_MAPPING_TAG, mapping)
    return yaml.load(source, Loader=UniqueLoader)
