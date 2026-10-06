"""Translate a bounded Compose subset into private Titan app directories."""
import copy
import re
from .core import Error

NAME=r'[a-zA-Z0-9][a-zA-Z0-9_-]{0,62}'
IMAGE=r'[a-z0-9][a-z0-9./_-]*(?::[A-Za-z0-9_.-]+(?:@sha256:[a-f0-9]{64})?|@sha256:[a-f0-9]{64})'

def validate_stack(value):
    if not isinstance(value,dict) or set(value)!={'primary','services'} or not isinstance(value['services'],dict) or not 1<=len(value['services'])<=8 or value['primary'] not in value['services']: raise Error('Ungültiger App-Containerverbund.')
    if len({name.lower() for name in value['services']})!=len(value['services']): raise Error('Container-Namen sind nicht eindeutig.')
    for name,service in value['services'].items():
        if not isinstance(name,str) or not re.fullmatch(NAME,name) or not isinstance(service,dict) or set(service)-{'image','environment','mounts','command','entrypoint','depends_on','ports','healthcheck','memory','shm_size','aliases','user'}: raise Error('Nicht unterstützte Containeroption.')
        if not isinstance(service.get('image'),str) or not re.fullmatch(IMAGE,service['image']): raise Error('Ungültiges Container-Image.')
        for key in ('memory', 'shm_size'):
            if key in service and (not isinstance(service[key], str) or not re.fullmatch(r'[1-9][0-9]{0,3}[mg]', service[key])): raise Error('Ungültiges Container-Speicherlimit.')
        if 'user' in service and (not isinstance(service['user'],str) or not re.fullmatch(r'[0-9]{1,9}(?::[0-9]{1,9})?',service['user'])): raise Error('Ungültiger Container-Benutzer.')
        aliases=service.get('aliases',[])
        if not isinstance(aliases,list) or len(aliases)>8 or any(not isinstance(alias,str) or not re.fullmatch(NAME,alias) for alias in aliases): raise Error('Ungültiger interner Containername.')
        env=service.get('environment',{})
        if not isinstance(env,dict) or len(env)>64 or any(not re.fullmatch(r'[A-Za-z_][A-Za-z0-9_]{0,63}',key) or not isinstance(val,str) or len(val)>1000 or '\0' in val for key,val in env.items()): raise Error('Ungültige Container-Umgebung.')
        for mount in service.get('mounts',[]):
            if not isinstance(mount,dict) or set(mount)-{'slot','target','readonly','option'} or not {'slot','target','readonly'}<=set(mount) or not isinstance(mount['slot'],str) or not re.fullmatch(r'[a-z0-9_-]{1,50}',mount['slot']) or not isinstance(mount['target'],str) or not mount['target'].startswith('/') or '..' in mount['target'].split('/') or '\0' in mount['target'] or len(mount['target'])>200 or mount['target'].startswith(('/proc','/sys','/dev','/run')) or type(mount['readonly']) is not bool or ('option' in mount and not re.fullmatch(r'stack_[a-zA-Z0-9_-]{1,100}',mount['option'])): raise Error('Ungültige Container-Dateizuordnung.')
        for key in ('command','entrypoint'):
            command=service.get(key)
            if command is not None and (not isinstance(command,list) or not 1<=len(command)<=64 or any(not isinstance(part,str) or len(part)>2000 or '$' in part or '\0' in part for part in command)): raise Error('Dynamische Container-Kommandos nicht unterstützt.')
        dependencies=service.get('depends_on',[])
        if not isinstance(dependencies,(list,dict)) or len(dependencies)>8: raise Error('Ungültige Container-Abhängigkeiten.')
        if isinstance(dependencies,dict):
            for dependency,options in dependencies.items():
                if not isinstance(options,dict) or set(options)!={'condition'} or options['condition'] not in ('service_started','service_healthy','service_completed_successfully'): raise Error('Ungültige Startbedingung.')
                if options['condition']=='service_healthy' and not value['services'].get(dependency,{}).get('healthcheck'): raise Error('Startbedingung benötigt einen Healthcheck.')
        for dependency in dependencies:
            if dependency not in value['services'] or dependency==name: raise Error('Ungültige Container-Abhängigkeit.')
        if not isinstance(service.get("mounts",[]),list) or len(service.get("mounts",[]))>16 or not isinstance(service.get("ports",[]),list) or len(service.get("ports",[]))>16: raise Error("Zu viele Container-Datei-/Portzuordnungen.")
        for mapping in service.get('ports',[]):
            if not isinstance(mapping,dict) or set(mapping)-{'target','published','protocol','option'} or not {'target','published','protocol'}<=set(mapping) or mapping['protocol'] not in ('tcp','udp') or any(type(mapping[k]) is not int or not 1<=mapping[k]<=65535 for k in ('target','published')) or ('option' in mapping and not re.fullmatch(r'stack_[a-zA-Z0-9_-]{1,100}',mapping['option'])): raise Error('Ungültiger Container-Port.')
        health=service.get('healthcheck')
        if health is not None:
            if not isinstance(health,dict) or set(health)-{'test','interval','timeout','start_period','retries'} or not isinstance(health.get('test'),list) or not health['test'] or health['test'][0] not in ('CMD','CMD-SHELL','NONE') or any(not isinstance(v,str) or '$' in v or len(v)>1000 for v in health['test']): raise Error('Nicht unterstützter Healthcheck.')
            for key in ('interval','timeout','start_period'):
                if key in health and not re.fullmatch(r'[1-9][0-9]{0,3}(?:ms|s|m|h)',str(health[key])): raise Error('Ungültiges Healthcheck-Intervall.')
            if 'retries' in health and (type(health['retries']) is not int or not 1<=health['retries']<=20): raise Error('Ungültige Healthcheck-Wiederholungen.')
    # Detect dependency cycles before Compose can create partial resources.
    def visit(name,parents):
        if name in parents: raise Error('Zyklische Container-Abhängigkeit.')
        for child in value['services'][name].get('depends_on',[]): visit(child,parents|{name})
    for name in value['services']: visit(name,set())
    aliases={}
    for name,service in value['services'].items():
        for alias in set([name,*service.get('aliases',[])]):
            if alias.lower() in aliases and aliases[alias.lower()]!=name: raise Error('Interne Container-Namen sind nicht eindeutig.')
            aliases[alias.lower()]=name
    return value


def build(app_id, recipe, directory, uid, gid, port, data_path, options, config_path=None):
    from .app_packages import selected_recipe
    recipe = selected_recipe(app_id, recipe, options)
    stack=validate_stack(recipe['stack']);primary=stack['primary']; names={key:app_id if key==primary else app_id+'-'+key.lower() for key in stack['services']};services={}
    for name,template in stack['services'].items():
        identifier=names[name];service={'image':template['image'],'container_name':'titan-'+identifier,'restart':'unless-stopped','mem_limit':template.get('memory',recipe['memory']),'cpus':2,'logging':{'driver':'json-file','options':{'max-size':'10m','max-file':'3'}},'labels':{'io.titan.managed':'true','io.titan.app':app_id},'networks':{'default':{'aliases':list(dict.fromkeys([name,*template.get('aliases',[])]))}},'environment':{},'volumes':[],'ports':[]}
        if any(isinstance(other.get('depends_on'),dict) and other['depends_on'].get(name,{}).get('condition') == 'service_completed_successfully' for other in stack['services'].values()):
            service['restart'] = 'no'
        for key,value in template.get('environment',{}).items():
            if value=='@uid': value=str(uid)
            elif value=='@gid': value=str(gid)
            elif value.startswith('@option:'):
                if value[8:] not in options:
                    if any(field['key']==value[8:] and field.get('optional') is True for field in recipe.get('install_schema',[])): continue
                    raise Error('Private App-Einstellungen fehlen; Installation erneut versuchen.')
                value=str(options[value[8:]])
            service['environment'][key]=value.replace('$','$$')
        for mount in template.get('mounts',[]):
            if mount.get('option') and not options.get(mount['option']): continue
            source=data_path if mount['slot']=='data' else str(config_path or directory+'/config')+'/'+mount['slot']
            service['volumes'].append({'type':'bind','source':str(source),'target':mount['target'],'read_only':mount['readonly'],'bind':{'create_host_path':False}})
        for mapping in template.get('ports',[]):
            key=mapping.get('option','stack_port_'+name+'_'+str(mapping['target'])+'_'+mapping['protocol'])
            if mapping.get('option') and key not in options: continue
            published=port if name==primary and mapping['target']==recipe['port'] and mapping['protocol']=='tcp' else options[key]
            service['ports'].append(str(published)+':'+str(mapping['target'])+'/'+mapping['protocol'])
        for key in ('command','entrypoint','healthcheck','shm_size','user'):
            if key in template: service[key]=copy.deepcopy(template[key])
        if template.get('depends_on'):
            dependency=template['depends_on']
            service['depends_on']={names[key]:options for key,options in dependency.items()} if isinstance(dependency,dict) else [names[key] for key in dependency]
        services[identifier]=service
    return {'services':services}



def _networks(doc, raw):
    """Imported network names become one private Compose bridge, never external."""
    definitions = doc.get('networks', {})
    if not isinstance(definitions, dict) or len(definitions) > 1:
        raise Error('Mehrere oder externe Vorlagennetze benötigen eine besondere Einrichtung.')
    for name, value in definitions.items():
        if not re.fullmatch(NAME, name) or not isinstance(value, (dict, type(None))):
            raise Error('Ungültiges Vorlagennetz.')
        value = value or {}
        if set(value) - {'driver', 'name'} or value.get('driver', 'bridge') != 'bridge':
            raise Error('Externe Netze, IPAM und spezielle Netzwerktreiber werden nicht übernommen.')
    for source in raw.values():
        values = source.get('networks', [])
        if isinstance(values, dict):
            if any(options not in (None, {}) for options in values.values()):
                raise Error('Feste Vorlagen-IP-Adressen werden nicht übernommen.')
            values = list(values)
        if not isinstance(values, list) or len(values) > 1 or any(key not in definitions for key in values):
            raise Error('Container verweist auf ein nicht unterstütztes Vorlagennetz.')


def translate(doc, label, repository, metadata=None):
    """Convert a bounded Compose document into Titan-owned, isolated slots.

    Source shell hooks, source host paths, fixed container names and global
    volumes are never executed/created. Service aliases preserve DB hostnames.
    A web entry must be declared by metadata or be the only published TCP port.
    """
    import shlex
    from .store_sources import line, localized, port, slug, login
    if not isinstance(doc, dict) or set(doc) - {'services', 'networks', 'volumes', 'version', 'name', 'x-casaos'}:
        raise Error('Zusätzliche Compose-Funktionen benötigen eine besondere Vorlage.')
    raw = doc.get('services', {})
    if not isinstance(raw, dict) or not 1 <= len(raw) <= 8:
        raise Error('Maximal acht Container pro App.')
    if any(not isinstance(source, dict) for source in raw.values()):
        raise Error('Ungültiger Container.')
    _networks(doc, raw)
    volumes = doc.get('volumes', {})
    if not isinstance(volumes, dict) or len(volumes) > 32:
        raise Error('Ungültige Volumes.')
    for key, value in volumes.items():
        if not isinstance(key, str) or not re.fullmatch(NAME, key) or value is not None and (not isinstance(value, dict) or set(value) - {'driver', 'name'} or value.get('driver', 'local') != 'local'):
            raise Error('Externe Volumes und Volume-Treiber werden nicht übernommen.')
    meta = dict(doc.get('x-casaos') or {})
    if metadata:
        if not isinstance(metadata, dict): raise Error('Ungültige App-Metadaten.')
        meta.update({'title': metadata.get('name', label), 'description': metadata.get('description', ''), 'port_map': metadata.get('port', ''), 'scheme': metadata.get('scheme', 'http')})
    web = None
    if meta.get('port_map') not in (None, ''):
        web, _ = port(meta['port_map'])
    primary = meta.get('main') if meta.get('main') in raw else None
    translated, settings, extra, slots, groups = {}, [], [], {}, {}
    for name, source in raw.items():
        ignored = {'container_name', 'restart', 'labels', 'x-casaos'}
        supported = {'image', 'environment', 'ports', 'volumes', 'command', 'entrypoint', 'depends_on', 'healthcheck', 'network_mode', 'networks', 'user', 'shm_size', 'mem_limit'}
        if set(source) - ignored - supported:
            raise Error('Diese App benötigt zusätzliche Hostrechte oder Laufzeitoptionen.')
        if source.get('network_mode') not in (None, 'bridge', 'host'):
            raise Error('Besondere Netzwerk-Namensräume erforderlich.')
        if source.get('network_mode') == 'host' and len(raw) > 1:
            raise Error('Host-Netz für Containerverbünde nicht unterstützt.')
        image = source['image']
        if not isinstance(image, str): raise Error('Ungültiges Container-Image.')
        image = image if ':' in image or '@' in image else image + ':latest'
        entry = {'image': image, 'environment': {}, 'mounts': [], 'ports': []}
        alias = source.get('container_name')
        if alias:
            if not isinstance(alias, str) or not re.fullmatch(NAME, alias): raise Error('Dynamische Container-Namen nicht unterstützt.')
            entry['aliases'] = [alias]
        for mapping in source.get('ports', []):
            if isinstance(mapping, int): mapping = str(mapping)
            if isinstance(mapping, str):
                parts = mapping.split(':')
                if len(parts) == 1: published, target = port(parts[0])[0], port(parts[0])[0]; protocol = port(parts[0])[1]
                elif len(parts) == 2: published = port(parts[0])[0]; target, protocol = port(parts[1])
                elif len(parts) == 3 and parts[0] in ('0.0.0.0', '127.0.0.1'): published = port(parts[1])[0]; target, protocol = port(parts[2])
                else: raise Error('Portbereiche oder dynamische Ports nicht unterstützt.')
                if len(parts) == 3 and parts[0] == '127.0.0.1': raise Error('Nur lokal erreichbare Dienste benötigen eine besondere Vorlage.')
            elif isinstance(mapping, dict) and not set(mapping) - {'target', 'published', 'protocol', 'mode', 'host_ip'}:
                if mapping.get('mode', 'ingress') != 'ingress' or mapping.get('host_ip', '0.0.0.0') != '0.0.0.0': raise Error('Besondere Portbindung erforderlich.')
                published = port(mapping.get('published', mapping['target']))[0]; target = port(mapping['target'])[0]; protocol = mapping.get('protocol', 'tcp')
            else: raise Error('Ungültige Portzuordnung.')
            entry['ports'].append({'published': published, 'target': target, 'protocol': protocol})
        if source.get('network_mode') == 'host':
            if web is None: raise Error('Host-Netz benötigt einen ausdrücklich angegebenen Webport.')
            if not entry['ports']: entry['ports'] = [{'published': web, 'target': web, 'protocol': 'tcp'}]
            primary = name
        for mapping in source.get('volumes', []):
            if isinstance(mapping, str):
                parts = mapping.split(':')
                if len(parts) not in (2, 3) or len(parts) == 3 and parts[2] not in ('ro', 'rw'): raise Error('Besondere Volume-Optionen erforderlich.')
                original, target = parts[:2]; readonly = len(parts) == 3 and parts[2] == 'ro'
            elif isinstance(mapping, dict) and not set(mapping) - {'type', 'source', 'target', 'read_only'} and mapping.get('type', 'bind') in ('bind', 'volume'):
                original, target = mapping['source'], mapping['target']; readonly = mapping.get('read_only', False)
            else: raise Error('Nicht unterstützte Dateizuordnung.')
            if not isinstance(original, str) or not original or '..' in original.split('/') or '$' in original or chr(0) in original:
                raise Error('Dynamische oder unsichere Datenpfade werden nicht übernommen.')
            if original.startswith(('/dev', '/proc', '/sys', '/etc', '/var/run', '/run', '/lib')):
                raise Error('Hostdateien erforderlich; explizite USB/GPU-Auswahl separat verwenden.')
            if not original.startswith(('/', '.')) and original not in volumes:
                raise Error('Benanntes Volume ist nicht deklariert: ' + original)
            if original not in slots:
                slots[original] = 'data' if target in ('/data', '/media', '/downloads', '/files', '/storage', '/usr/src/app/upload') and 'data' not in slots.values() else 'mount-' + str(len(slots) + 1)
            entry['mounts'].append({'slot': slots[original], 'target': target, 'readonly': readonly})
        env = source.get('environment', {})
        if isinstance(env, list):
            if any(not isinstance(part, str) or '=' not in part for part in env): raise Error('Host-Umgebungsvariablen werden nicht übernommen.')
            pairs = [part.split('=', 1) for part in env]
            if len({key for key, _ in pairs}) != len(pairs): raise Error('Doppelte Umgebungsvariable.')
            env = dict(pairs)
        if not isinstance(env, dict): raise Error('Ungültige Container-Umgebung.')
        for key, value in env.items():
            if key == 'PUID': entry['environment'][key] = '@uid'; continue
            if key == 'PGID': entry['environment'][key] = '@gid'; continue
            value = '' if value is None else str(value)
            if key == 'TZ' and '$' in value: value = 'Europe/Berlin'
            value = re.sub(r'\$\{[A-Za-z_][A-Za-z0-9_]*:-([^{}]*)\}', r'\1', value)
            secret = bool(re.search(r'password|secret|token|api.?key|app_key|(?:^|_)(?:pass|passwd|pwd)(?:_|$)', key, re.I))
            # A shared upstream DB password becomes one generated private value,
            # even when client/server call it DB_PASSWORD and POSTGRES_PASSWORD.
            database_secret=bool(re.fullmatch(r'(?:DB|POSTGRES|MYSQL|MARIADB)_PASSWORD',key))
            family='database-password' if database_secret else key
            group = ('secret', family, value) if secret and value else (key, value)
            if group not in groups:
                option = 'stack_env_' + str(len(settings)); groups[group] = option
                field = {'key': option, 'label': key + ' · ' + name, 'type': 'password' if secret else 'text', 'default': '' if secret or '$' in value else value, 'required': True, 'min_length': 1 if secret or '$' in value or not value else 0, 'max_length': 1000}
                if secret and value and '$' not in value and (database_secret or re.search(r'ROOT_PASSWORD|SECRET|TOKEN|API.?KEY|APP_KEY',key,re.I)):
                    field.update(generated=True, generator='base64-key' if key == 'APP_KEY' and value.startswith('base64:') else 'hex', min_length=32)
                if '[' in value or '<' in value and '>' in value:
                    field['default'] = ''; field['min_length'] = 1
                settings.append(field)
            entry['environment'][key] = '@option:' + groups[group]
        for key in ('command', 'entrypoint'):
            if key in source: entry[key] = shlex.split(source[key]) if isinstance(source[key], str) else source[key]
        for key in ('healthcheck', 'user', 'shm_size'):
            if key in source: entry[key] = source[key]
        if 'user' in entry and str(entry['user']).split(':')[0] not in ('0',):
            raise Error('Feste Container-Benutzer benötigen eine geprüfte Berechtigungszuordnung.')
        if 'mem_limit' in source:
            value = str(source['mem_limit']).lower()
            if not re.fullmatch(r'[1-9][0-9]{0,3}[mg]', value): raise Error('Nicht unterstütztes RAM-Limit.')
            entry['memory'] = value
        depends = source.get('depends_on', [])
        if isinstance(depends, dict):
            if any(not isinstance(options, dict) or set(options) - {'condition', 'required'} or options.get('required') is False for options in depends.values()): raise Error('Nicht unterstützte Startbedingung.')
            depends = {key: {'condition': options.get('condition', 'service_started')} for key, options in depends.items()}
        entry['depends_on'] = depends
        translated[name] = entry
    tcp = [(name, mapping) for name, entry in translated.items() for mapping in entry['ports'] if mapping['protocol'] == 'tcp']
    matches = [(name, mapping) for name, mapping in tcp if (web is None or mapping['published'] == web) and (primary is None or name == primary)]
    if len(matches) != 1: raise Error('Kein eindeutiger Webport; App benötigt eindeutige Metadaten.')
    primary, main_port = matches[0]; web = main_port['published']
    used = set()
    for name, service in translated.items():
        for mapping in service['ports']:
            endpoint = (mapping['published'], mapping['protocol'])
            if endpoint in used: raise Error('Doppelte veröffentlichte Ports in der Vorlage.')
            used.add(endpoint)
            if name == primary and mapping is main_port: continue
            key = 'stack_port_' + name + '_' + str(mapping['target']) + '_' + mapping['protocol']
            settings.append({'key': key, 'label': name + ' · Port ' + str(mapping['target']) + '/' + mapping['protocol'], 'type': 'number', 'default': mapping['published'], 'min': 1, 'max': 65535, 'required': True})
            extra.append({'option': key, 'target': mapping['target'], 'protocol': mapping['protocol'], 'service': name})
    stack = validate_stack({'primary': primary, 'services': translated})
    title = line(localized(meta.get('title')) or label, 80)
    return {'id': slug(label), 'name': title, 'description': line(localized(meta.get('description'))), 'image': translated[primary]['image'], 'port': main_port['target'], 'default_port': web, 'scheme': 'https' if meta.get('scheme') == 'https' else 'http', 'mount': None, 'config_mount': True, 'documentation': 'https://github.com/' + repository + '/tree/main/Apps/' + label, 'login_note': login(title), 'default_network': 'host' if raw[primary].get('network_mode') == 'host' else 'default', 'stack': stack, 'stack_fields': settings, 'stack_ports': extra}
