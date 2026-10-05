"""Stopped manual-container edits with retained data and a recoverable snapshot."""
import contextlib
import re
from .core import Error
from .docker_engine import IMAGE, identifier, name

SECRET = re.compile(r'PASS(?:WORD)?|TOKEN|SECRET|PRIVATE|CREDENTIAL|AUTH|COOKIE|SESSION|DATABASE_URL|CONNECTION_STRING|(?:^|_)(?:KEY|DSN)(?:_|$)', re.I)
URL_SECRET = re.compile(r'[A-Za-z][A-Za-z0-9+.-]*://[^/@\s:]+:[^/@\s]*@|[?&](?:access_token|token|api_key|password|secret)=', re.I)
EDITABLE = {'ports', 'environment', 'network', 'restart', 'memory_mb', 'cpus'}


def secret_entry(key, value):
    return bool(SECRET.search(str(key)) or isinstance(value, str) and URL_SECRET.search(value))


def snapshot(host, row):
    labels = (row.get('Config') or {}).get('Labels') or {}
    original = row.get('Name', '').lstrip('/')
    if labels.get('io.titan.manual') != 'true' or labels.get('io.titan.managed') == 'true' or not original.startswith('titan-custom-'):
        raise Error('Nur eigene Titan-Container können hier bearbeitet werden. App-Pakete haben eigene Einstellungen.', 409)
    if host.engine_active(row):
        raise Error('Container vor dem Bearbeiten stoppen.', 409)
    config = row.get('Config') or {}
    current = row.get('HostConfig') or {}
    if current.get('Privileged') or current.get('CapAdd') or current.get('PidMode') or current.get('IpcMode') == 'host' or current.get('SecurityOpt'):
        raise Error('Individuell privilegierte Container werden nicht automatisch umgebaut.', 409)
    mounts = row.get('Mounts') or []
    storage = host._engine_storage_ready(row)
    if not mounts:
        raise Error('Container ohne eindeutigen Datenordner werden nicht automatisch umgebaut.', 409)
    if len(mounts) > 1 or any(mount.get('Type') != ('bind' if storage else 'volume') or not mount.get('RW') for mount in mounts):
        raise Error('Nur ein verwalteter Datenordner oder ein lokales Docker-Volume kann übernommen werden.', 409)
    memory, cpus = current.get('Memory', 0), current.get('NanoCpus', 0)
    if type(memory) is not int or memory < 64 * 1048576 or memory % 1048576 or type(cpus) is not int or cpus < 1000000000 or cpus % 1000000000:
        raise Error('Individuelle Ressourcenlimits benötigen eine manuelle Konfiguration.', 409)
    network = current.get('NetworkMode', 'bridge')
    if network == 'default':
        network = 'bridge'
    if network not in ('bridge', 'host', 'none') and len((row.get('NetworkSettings') or {}).get('Networks') or {}) != 1:
        raise Error('Container mit mehreren Netzwerken können nicht automatisch umgebaut werden.', 409)
    ports = []
    try:
        for target, bindings in (current.get('PortBindings') or {}).items():
            number, protocol = target.split('/')
            for binding in bindings or []:
                if binding.get('HostIp') not in ('', '0.0.0.0', None):
                    raise Error('Individuelle Bindungsadressen benötigen eine manuelle Konfiguration.', 409)
                ports.append({'published': int(binding['HostPort']), 'target': int(number), 'protocol': protocol})
    except (ValueError, TypeError, KeyError):
        raise Error('Vorhandene Portzuordnungen sind ungültig.', 409) from None
    devices = [item['id'] for item in host._engine_devices_ready(row)]
    result = {'name': name(original[len('titan-custom-'):]), 'image': config.get('Image', ''),
              'network': network, 'ports': ports, 'devices': devices, 'memory_mb': memory // 1048576,
              'cpus': cpus // 1000000000, 'restart': (current.get('RestartPolicy') or {}).get('Name') or 'no'}
    if mounts:
        result['target'] = mounts[0]['Destination']
        if storage:
            result['storage_id'] = storage['id']
        else:
            result['volume'] = mounts[0]['Name']
    # Validate the complete retained configuration before exposing an editor.
    host._engine_create_args(result)
    return result


def public_settings(host, row):
    result = snapshot(host, row)
    environment, hidden = {}, []
    for entry in (row.get('Config') or {}).get('Env') or []:
        if not isinstance(entry, str) or '=' not in entry:
            continue
        key, value = entry.split('=', 1)
        if secret_entry(key, value):
            hidden.append(key)
        else:
            environment[key] = value
    return {**{key: value for key, value in result.items() if key in EDITABLE},
            'environment': environment, 'preserved_secret_keys': hidden}


def update(host, container, settings):
    if not isinstance(settings, dict) or not settings or set(settings) - EDITABLE:
        raise Error('Ungültige Container-Einstellungen.')
    environment = settings.get('environment', {})
    if not isinstance(environment, dict) or any(secret_entry(key, value) for key, value in environment.items()):
        raise Error('Geheime Umgebungsvariablen bleiben erhalten und werden hier nicht bearbeitet.')
    container = identifier(container)
    lock = getattr(host, 'app_memory_lock', None)
    with lock if lock is not None else contextlib.nullcontext():
        row = host.engine_container(container)
        config = {**snapshot(host, row), **settings}
        host._engine_create_args(config)
        original = row['Name'].lstrip('/')
        host._engine_check_start_memory(config['memory_mb'] * 1048576, installation=True,name=original,restart=config['restart'])
        fresh = host.engine_container(container)
        if host.engine_active(fresh) or fresh.get('Name') != row.get('Name') or fresh.get('Config') != row.get('Config') or fresh.get('HostConfig') != row.get('HostConfig'):
            raise Error('Containerzustand wurde verändert. Ansicht aktualisieren.', 409)
        base = host.engine_summary(row)['image']
        if not re.fullmatch(IMAGE, base):
            raise Error('Originalimage konnte nicht geprüft werden.')
        image = host.engine_docker(['commit', '--change', 'LABEL io.titan.original_image=' + base, container], timeout=600).strip()
        if not re.fullmatch(r'sha256:[a-f0-9]{64}', image):
            raise Error('Container-Sicherung konnte nicht geprüft werden.', 503)
        fresh = host.engine_container(container)
        if host.engine_active(fresh) or fresh.get('Name') != row.get('Name') or fresh.get('Config') != row.get('Config') or fresh.get('HostConfig') != row.get('HostConfig'):
            raise Error('Containerzustand wurde während der Sicherung verändert. Ansicht aktualisieren.', 409)
        # Preserve the old container's pinned storage identity after a long
        # commit; recreation must not adopt a newly mounted replacement volume.
        host._engine_storage_ready(fresh)
        host._engine_devices_ready(fresh)
        host._engine_check_start_memory(config['memory_mb'] * 1048576, installation=True,name=original,restart=config['restart'])
        config['image'] = image
        old_policy = (row['HostConfig'].get('RestartPolicy') or {}).get('Name') or 'no'
        renamed = False; policy_changed = False
        try:
            if old_policy != 'no':
                host.engine_docker(['update','--restart','no',container],timeout=30); policy_changed = True
            host.engine_docker(['rename', container, 'titan-previous-' + container[:20]]); renamed = True
            result = host.op_docker_container_create(config)
        except Exception as error:
            # Only a freshly created, name- and ownership-verified replacement
            # can be removed. Volumes and user data are never removed here.
            try:
                replacements = host.engine_docker(['ps', '-aq', '--no-trunc', '--filter', 'name=^/' + original + '$']).splitlines() if renamed else []
                if len(replacements) > 1:
                    raise Error('Mehrere Ersatzcontainer gemeldet.', 409)
                if replacements:
                    replacement = identifier(replacements[0])
                    changed = host.engine_container(replacement)
                    if replacement == container or changed.get('Name') != '/' + original or (changed.get('Config') or {}).get('Labels', {}).get('io.titan.manual') != 'true':
                        raise Error('Ersatzcontainer konnte nicht sicher zugeordnet werden.', 409)
                    if host.engine_active(changed):
                        host.engine_docker(['stop', '--time', '30', replacement], timeout=60)
                    host.engine_docker(['rm', replacement], timeout=120)
                if renamed:host.engine_docker(['rename', container, original])
                if policy_changed:host.engine_docker(['update','--restart',old_policy,container],timeout=30)
            except Exception:
                raise Error('Änderung fehlgeschlagen. Der bisherige Container bleibt als gestoppte Sicherung erhalten; Wiederherstellung in Docker prüfen.', 503) from None
            reason = ' ' + str(error) if isinstance(error, Error) else ''
            raise Error('Änderung fehlgeschlagen; der bisherige Container wurde wiederhergestellt.' + reason, 503) from None
        return {**result, 'backup_container': container,
                'message': 'Einstellungen gespeichert und Container gestartet. Der vorherige Container bleibt als gestoppte Sicherung mit deaktiviertem Autostart erhalten. Daten und geheime Variablen bleiben erhalten.'}
