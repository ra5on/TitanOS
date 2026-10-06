"""Validate deployment data shared by bundled and imported stores."""
import hashlib
import copy
import re
import urllib.parse
from .core import Error, integer

def text(value, limit=200):
    if not isinstance(value, str) or not value or len(value) > limit or any(ord(c) < 32 for c in value):
        raise Error('Katalog enthält ungültigen Text.')
    return value


def recipes(document, source):
    if not isinstance(document, dict) or set(document) != {'schema', 'name', 'apps'} or document['schema'] != 1:
        raise Error('Titan-AppStore-Schema 1 benötigt: schema, name und apps. CasaOS-Archive sind noch nicht direkt kompatibel.')
    name = text(document['name'], 80)
    if not isinstance(document['apps'], list) or not 1 <= len(document['apps']) <= 400:
        raise Error('Ein Store benötigt 1 bis 400 Apps.')
    result = {}
    prefix = 's' + hashlib.sha256(source.encode()).hexdigest()[:10] + '-'
    for item in document['apps']:
        allowed = {'id', 'name', 'category', 'scheme', 'description', 'image', 'port', 'default_port', 'mount', 'memory', 'documentation', 'login_note', 'environment', 'config_mount', 'ports', 'settings','stack','stack_fields','stack_ports','default_network','storage_bindings','port_bindings','architectures','upstream_version','template_status'}
        required = {'id', 'name', 'description', 'image', 'port', 'documentation', 'login_note'}
        if not isinstance(item, dict) or set(item) - allowed or required - set(item):
            raise Error('App enthält fehlende oder nicht unterstützte Felder.')
        slug = text(item['id'], 18)
        if not re.fullmatch('[a-z][a-z0-9_-]{0,17}', slug):
            raise Error('Ungültige App-ID.')
        identifier = prefix + slug
        if identifier in result:
            raise Error('Doppelte App-ID.')
        image = text(item['image'], 250)
        if not re.fullmatch(r'[a-z0-9][a-z0-9./_-]*(?::[A-Za-z0-9_.-]+(?:@sha256:[a-f0-9]{64})?|@sha256:[a-f0-9]{64})', image):
            raise Error('Container-Image mit explizitem Tag oder SHA256-Digest angeben.')
        mount = item.get('mount', '/data')
        if mount is not None and (not isinstance(mount, str) or not re.fullmatch(r'/[a-zA-Z0-9_/-]{1,80}', mount) or mount.startswith(('/proc', '/sys', '/dev', '/etc', '/config'))):
            raise Error('Ungültiges Datenziel im Container.')
        memory = item.get('memory', '1g')
        if not isinstance(memory, str) or not re.fullmatch(r'(?:[1-9]|1[0-6])g', memory):
            raise Error('RAM-Limit muss zwischen 1g und 16g liegen.')
        documentation = text(item['documentation'], 1000)
        if not documentation.startswith('https://') or urllib.parse.urlsplit(documentation).username:
            raise Error('HTTPS-Dokumentationslink erforderlich.')
        environment = item.get('environment', {})
        if not isinstance(environment, dict) or len(environment) > 32 or any(not re.fullmatch('[A-Z_][A-Z0-9_]{0,63}', key) for key in environment):
            raise Error('Ungültige Umgebungsvariablen.')
        environment = {key: text(value, 1000).replace('$', '$$') for key, value in environment.items()}
        if type(item.get('config_mount', True)) is not bool:
            raise Error('config_mount muss true oder false sein.')
        scheme = item.get('scheme', 'http')
        if scheme not in ('http', 'https'): raise Error('Web-Schema muss http oder https sein.')
        port = integer(item['port'], 1, 65535)
        fields, extra = [], []
        ports = item.get('ports', [])
        settings = item.get('settings', [])
        if not isinstance(ports, list) or len(ports) > 8 or not isinstance(settings, list) or len(settings) > 16:
            raise Error('Maximal acht zusätzliche Ports und 16 App-Einstellungen.')
        for index, entry in enumerate(ports):
            if not isinstance(entry, dict) or set(entry) != {'target', 'published', 'protocol'} or entry['protocol'] not in ('tcp', 'udp'):
                raise Error('Port benötigt target, published und protocol (tcp oder udp).')
            target, published = integer(entry['target'], 1, 65535), integer(entry['published'], 1024, 65535)
            key = f'port_{index}'
            fields.append({'key': key, 'label': f'Port {target}/{entry["protocol"]}', 'type': 'number',
                           'default': published, 'min': 1024, 'max': 65535, 'required': True})
            extra.append({'option': key, 'target': target, 'protocol': entry['protocol']})
        for index, entry in enumerate(settings):
            if not isinstance(entry, dict) or set(entry) != {'env', 'label', 'default', 'secret'} or type(entry['secret']) is not bool:
                raise Error('App-Einstellung benötigt env, label, default und secret.')
            if not isinstance(entry['env'], str) or not re.fullmatch('[A-Z_][A-Z0-9_]{0,63}', entry['env']) or entry['env'] in {'PUID', 'PGID'}:
                raise Error('Ungültige einstellbare Umgebungsvariable.')
            if any(field.get('env') == entry['env'] for field in fields):
                raise Error('Doppelte einstellbare Umgebungsvariable.')
            default = entry['default']
            if default != '':
                text(default, 1000)
            if entry['secret'] and default:
                raise Error('Persönliche Geheimnisse dürfen nicht im öffentlichen Store vorbelegt werden.')
            fields.append({'key': f'setting_{index}', 'label': text(entry['label'], 80), 'env': entry['env'],
                           'type': 'password' if entry['secret'] else 'text', 'default': default,
                           'min_length': 1 if entry['secret'] else 0, 'max_length': 1000, 'required': True})
        stack_extra = {}
        if 'stack' in item:
            from .compose_templates import validate_stack
            stack_extra = {'stack': validate_stack(item['stack'])}
            if item.get('default_network','default') not in ('default','host') or item.get('default_network')=='host' and len(item['stack']['services'])>1: raise Error('Ungültiges Standardnetz.')
            stack_extra['default_network']=item.get('default_network','default')
            # These fields are adapter-generated; validate all keys and limits.
            stack_fields=item.get('stack_fields',[])
            if not isinstance(stack_fields,list) or len(stack_fields)>64: raise Error('Zu viele Container-Einstellungen.')
            keys=set()
            for field in stack_fields:
                if not isinstance(field,dict) or set(field)-{'key','label','type','default','required','min','max','min_length','max_length','optional','enabled_by_default','suggested_default','help','generated','generator'} or not re.fullmatch(r'stack_[a-zA-Z0-9_-]{1,100}',field.get('key','')) or field['key'] in keys or field.get('type') not in ('text','password','number','boolean'): raise Error('Ungültige Container-Einstellung.')
                keys.add(field['key']); text(field.get('label'),100)
                if type(field.get('required')) is not bool: raise Error('Ungültige Pflichtangabe.')
                for flag in ('optional','enabled_by_default','generated'):
                    if flag in field and type(field[flag]) is not bool: raise Error('Ungültige optionale Container-Einstellung.')
                if field.get('optional') and field.get('required'): raise Error('Optionale Container-Einstellung darf keine Pflichtangabe sein.')
                if field.get('generated') and (field['type']!='password' or field.get('generator') not in ('hex','base64-key')): raise Error('Ungültige Schlüsselgenerierung.')
                if 'help' in field: text(field['help'],500)
                if field['type']=='number':
                    if field.get('min') not in (1,1024) or field.get('max')!=65535: raise Error('Ungültiger Portbereich.')
                    if field.get('required') or 'default' in field: integer(field.get('default'),field['min'],65535)
                    if 'suggested_default' in field: integer(field['suggested_default'],field['min'],65535)
                elif field['type']=='boolean':
                    if type(field.get('default',False)) is not bool: raise Error('Ungültige Ordnerauswahl.')
                else:
                    default=field.get('default','')
                    if not isinstance(default,str) or len(default)>1000 or field.get('max_length')!=1000 or field.get('min_length') not in (0,1,32) or field['type']=='password' and default: raise Error('Ungültige Container-Textvorgabe.')
                    if 'suggested_default' in field and (field['type']=='password' or not isinstance(field['suggested_default'],str) or len(field['suggested_default'])>1000): raise Error('Ungültige optionale Textvorgabe.')
            ports=item.get('stack_ports',[])
            if not isinstance(ports,list) or len(ports)>32 or any(not isinstance(p,dict) or set(p)!={'option','target','protocol','service'} or p['option'] not in keys or p['protocol'] not in ('tcp','udp') or p['service'] not in item['stack']['services'] for p in ports): raise Error('Ungültige Container-Verbindungsports.')
            for service in item['stack']['services'].values():
                for value in service.get('environment',{}).values():
                    if value.startswith('@option:') and value[8:] not in keys: raise Error('Container-Einstellung fehlt.')
                for binding in service.get('mounts',[]):
                    if binding.get('option') and not any(field['key']==binding['option'] and field['type']=='boolean' for field in stack_fields): raise Error('Ordnerauswahl fehlt.')
                for mapping in service.get('ports',[]):
                    if mapping.get('option') and not any(field['key']==mapping['option'] and field['type']=='number' for field in stack_fields): raise Error('Portauswahl fehlt.')
            for mapping in ports:
                if not any(p['target']==mapping['target'] and p['protocol']==mapping['protocol'] for p in item['stack']['services'][mapping['service']].get('ports',[])): raise Error('Container-Port fehlt.')
            fields.extend(stack_fields);extra.extend(ports)
        architectures=item.get('architectures',[])
        if not isinstance(architectures,list) or len(architectures)>8 or any(value not in ('amd64','x86_64','arm64','aarch64','armhf') for value in architectures): raise Error('Ungültige Image-Architektur.')
        result[identifier] = {'name': text(item['name'], 80), 'description': text(item['description'], 500),
            'image': image, 'port': port, 'scheme': scheme, 'default_port': integer(item.get('default_port', max(port, 8080)), 1 if item.get('stack') else 1024, 65535),
            'mount': mount, 'memory': memory, 'environment': environment, 'config_mount': item.get('config_mount', True),
            'category': text(item.get('category', 'LinuxServer.io' if source.startswith('https://api.linuxserver.io/') else 'Eigene Stores'), 80), 'color': '#6478db', 'symbol': '▦', 'documentation': documentation,
            'first_login': {'mode': 'documentation' if source.startswith(('https://api.linuxserver.io/', 'https://github.com/', 'https://codeload.github.com/')) else 'setup', 'instructions': text(item['login_note'], 2000), 'documentation': documentation},
            **({'upstream_name': image.split('/')[-1].split(':')[0]} if image.startswith('lscr.io/linuxserver/') else {}),
            'note': text(item['login_note'], 2000), 'store_name': name, 'store_url': source,
            'install_schema': fields, 'extra_ports': extra, 'architectures':architectures,
            'upstream_version':text(item.get('upstream_version','latest'),128),
            'template_status':text(item.get('template_status','metadata-validated'),80), **stack_extra}
        if item.get('stack'):
            # Public summaries are derived from validated deployment data, never
            # copied from untrusted documentation where mappings may disagree.
            stack=item['stack']; primary=stack['primary']
            result[identifier]['storage_bindings']=[{**binding,'service':service_name,'optional':bool(binding.get('option'))} for service_name,service in stack['services'].items() for binding in service.get('mounts',[])]
            result[identifier]['port_bindings']=[]
            for service_name,service in stack['services'].items():
                for binding in service.get('ports',[]):
                    main=service_name==primary and binding['target']==port and binding['protocol']=='tcp'
                    field=next((field for field in stack_fields if field['key']==binding.get('option')),None)
                    result[identifier]['port_bindings'].append({**binding,'service':service_name,
                        'published':result[identifier]['default_port'] if main else field.get('default',field.get('suggested_default')) if field else binding['published'],
                        'optional':bool(field and not field.get('required'))})
            # This verified image supports changing its actual listener. One
            # web-port control must drive the environment, mapping and links.
            # Keep the old option key so installed private configurations remain
            # readable; never rewrite their secrets or running containers here.
            if (image.startswith(('wisdomsky/cloudflared-web:', 'ghcr.io/wisdomsky/cloudflared-web:'))
                    and len(stack['services']) == 1):
                fields = copy.deepcopy(fields)
                result[identifier]['install_schema'] = fields
                reference = stack['services'][primary].get('environment', {}).get('WEBUI_PORT', '')
                controlled = next((field for field in fields if reference == '@option:' + field['key']), None)
                if controlled is not None:
                    result[identifier].update(dynamic_web_port=True, web_port_option=controlled['key'])
                    controlled.update(controlled_by_web_port=True,
                                      help='Wird automatisch aus „Port für die Weboberfläche“ übernommen.')
                password_reference = stack['services'][primary].get('environment', {}).get('BASIC_AUTH_PASS', '')
                password = next((field for field in fields if password_reference == '@option:' + field['key']), None)
                if password is not None:
                    # Also mask fields from previously saved catalog snapshots.
                    # The existing owner-only options file is left untouched.
                    legacy_password = password['type'] != 'password'
                    result[identifier]['web_password_option'] = password['key']
                    password.update(type='password', default='', required=True,
                                    min_length=0 if legacy_password else 1,
                                    help='Dein eigenes Passwort für die Tunnel-Weboberfläche. Ein gespeichertes Passwort bleibt beim Bearbeiten erhalten.')
                result[identifier]['first_login'] = {
                    'mode': 'install', 'documentation': documentation,
                    'instructions': 'Mit dem Benutzernamen aus BASIC_AUTH_USER (Vorauswahl admin) und deinem bei der Installation gewählten BASIC_AUTH_PASS anmelden. Den Tunnel-Token anschließend in der App eintragen.'}
    return name, result
