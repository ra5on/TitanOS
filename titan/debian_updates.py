"""Debian A/B updates: signed GitHub offers, RAUC installation, explicit reboot.

Persistent NAS state has a fixed compatibility schema. No apt operation mutates
the running OS and no update operation reboots the machine implicitly.
"""
import fcntl
import functools
import hashlib
import ipaddress
import json
import os
from pathlib import Path
import re
import shutil
import socket
import stat
import subprocess
import tempfile
import time
import urllib.parse
import urllib.request

from .core import Error, atomic_json
from .update_progress import report, tracked

FORMAT = 'titan-debian-ab-v1'
COMPATIBLE = 'titan-debian13-amd64-ab-v1'
INFO = Path('/usr/share/titan/image-info.json')
STATE = Path('/var/lib/titan-system/updates/state.json')
CACHE = Path('/var/lib/titan-system/updates/downloads')
SLOTS = {'A': 'rootfs.0', 'B': 'rootfs.1'}
BOOT_OK = Path('/run/titan-system-confirmed')
LOCK = Path('/run/lock/titan-system-update.lock')
BOOT_MEMORY_GUARD = Path('/usr/share/titan/boot-memory-guard.py')
BOOT_MEMORY_REPORT = Path('/run/titan-boot-memory.json')
BOOT_DAEMON_GUARD = Path('/usr/share/titan/boot-daemon-guard.py')
BOOT_ID = Path('/proc/sys/kernel/random/boot_id')
DAEMON_COMMANDS = {
    'docker.service': ('docker', '/usr/bin/python3 -I /usr/share/titan/boot-daemon-guard.py --component docker -- /usr/sbin/dockerd -H fd:// --containerd=/run/containerd/containerd.sock $DOCKER_OPTS'),
    'libvirtd.service': ('vms', '/usr/bin/python3 -I /usr/share/titan/boot-daemon-guard.py --component vms -- /usr/sbin/libvirtd $LIBVIRTD_ARGS'),
}
DAEMON_MARKERS = {unit: Path('/run/titan-boot-daemon-' + component + '.json')
                  for unit, (component, _) in DAEMON_COMMANDS.items()}
MAX_BUNDLE = 2 * 1024**3 - 1
DIGEST = re.compile(r'sha256:[a-f0-9]{64}')


def locked(function):
    @functools.wraps(function)
    def wrapper(*args, **kwargs):
        if not os.path.ismount('/var/lib/titan-system'):
            raise Error('Die persistente Systempartition ist nicht eingehängt.', 503)
        LOCK.parent.mkdir(parents=True, exist_ok=True)
        with LOCK.open('a') as lock:
            try:
                fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
            except BlockingIOError:
                raise Error('Ein Systemwechsel oder eine Startprüfung läuft bereits.', 409) from None
            return function(*args, **kwargs)
    return wrapper


def run(arguments, **kwargs):
    from .host import run as execute
    return execute(arguments, **kwargs)


def available():
    try:
        value = json.loads(INFO.read_text())
        return isinstance(value, dict) and value.get('platform') == 'debian-rauc'
    except (OSError, ValueError):
        return False


def validate_identity(value):
    from .updates import staged_version, architecture, release_metadata
    if (not isinstance(value, dict) or value.get('format') != FORMAT or
            value.get('platform') != 'debian-rauc' or value.get('compatible') != COMPATIBLE or
            value.get('architecture') != architecture() or type(value.get('state_schema')) is not int or value.get('state_schema') != 1 or
            not isinstance(value.get('release_id'), str) or not DIGEST.fullmatch(value['release_id']) or
            value.get('release_stage') not in ('alpha', 'beta', 'stable')):
        raise Error('Kein kompatibler Titan-Debian-Systemstand.', 409)
    staged_version(value.get('version'), value['release_stage'])
    release_metadata(value)
    accounts = value.get('system_accounts')
    if not isinstance(accounts, dict) or set(accounts) != {'users','groups'}:
        raise Error('Der Systemstand enthält keinen gültigen Kontenvertrag.', 409)
    for kind in ('users','groups'):
        entries = accounts[kind]
        if not isinstance(entries, dict) or not 1 <= len(entries) <= 256:
            raise Error('Ungültige Systemkonten im Update.', 409)
        for name, record in entries.items():
            if not isinstance(name, str) or not re.fullmatch(r'[A-Za-z_][A-Za-z0-9_.-]{0,63}', name):
                raise Error('Ungültiger Systemkontoname.', 409)
            if kind == 'users':
                if not isinstance(record, dict) or set(record) != {'uid','gid'}:
                    raise Error('Ungültige Systembenutzerkennung.', 409)
                numbers = record.values()
            else:
                numbers = [record]
            if any(type(number) is not int or not 0 <= number < 2**31 for number in numbers):
                raise Error('Ungültige Systemkontenkennung.', 409)
    return value


def image_info():
    from .updates import verify_manifest, PUBLIC_KEY
    value = verify_manifest(INFO, INFO.with_suffix('.json.sig'), PUBLIC_KEY)
    return validate_identity(value)


def load_state():
    from .updates import strict_json
    try:
        value = strict_json(STATE.read_bytes())
    except FileNotFoundError:
        return {'schema': 1, 'slots': {}}
    if (not isinstance(value, dict) or type(value.get('schema')) is not int or value.get('schema') != 1 or
            not isinstance(value.get('slots'), dict) or set(value['slots']) - set(SLOTS)):
        raise Error('Ungültiger Titan-Systemstatus. Keine Systemänderung ausgeführt.', 503)
    for slot, record in value['slots'].items():
        if not isinstance(record, dict) or type(record.get('confirmed')) is not bool:
            raise Error('Ungültiger Slot-Datensatz.', 503)
        validate_identity(record.get('identity'))
    pending = value.get('pending')
    if pending is not None and (not isinstance(pending, dict) or pending.get('slot') not in SLOTS or
            pending.get('kind') not in ('update', 'rollback') or
            not isinstance(pending.get('digest'), str) or not DIGEST.fullmatch(pending['digest'])):
        raise Error('Ungültiger vorbereiteter Systemwechsel.', 503)
    return value


def save_state(value):
    STATE.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
    atomic_json(STATE, value)
    directory = os.open(STATE.parent, os.O_RDONLY | os.O_DIRECTORY)
    try:
        os.fsync(directory)
    finally:
        os.close(directory)


def rauc_status():
    from .updates import strict_json
    value = strict_json(run(['rauc', 'status', '--detailed', '--output-format=json'], timeout=30))
    if not isinstance(value, dict) or value.get('compatible') != COMPATIBLE:
        raise Error('RAUC meldet keine passende Titan-Plattform.', 503)
    slots = {}
    for item in value.get('slots', []):
        if not isinstance(item, dict) or len(item) != 1:
            raise Error('Ungültiger RAUC-Slotstatus.', 503)
        name, detail = next(iter(item.items()))
        if name not in SLOTS.values() or not isinstance(detail, dict):
            raise Error('Unerwartetes RAUC-Slotlayout.', 503)
        bootname = detail.get('bootname')
        if (bootname not in SLOTS or SLOTS[bootname] != name or bootname in slots or
                detail.get('device') != '/dev/disk/by-partlabel/TITAN-' + bootname or
                detail.get('type') != 'ext4'):
            raise Error('RAUC-Geräte stimmen nicht mit dem Titan-Layout überein.', 503)
        slots[bootname] = detail
    current = [name for name, detail in slots.items() if detail.get('state') == 'booted']
    if set(slots) != set(SLOTS) or len(current) != 1:
        raise Error('Kein eindeutiger aktiver Titan-Systemslot.', 503)
    return value, slots, current[0]


def verify_devices(current, target):
    mounted = Path(run(['findmnt', '-nro', 'SOURCE', '/'], timeout=10).strip()).resolve(strict=True)
    devices = {name: Path('/dev/disk/by-partlabel/TITAN-' + name).resolve(strict=True) for name in SLOTS}
    if mounted != devices[current] or devices[current] == devices[target]:
        raise Error('Systempartitionen stimmen nicht mit dem gestarteten NAS überein.', 409)
    parents = {name: run(['lsblk', '-dnro', 'PKNAME', str(device)], timeout=10).strip() for name, device in devices.items()}
    numbers = {name: Path('/sys/class/block', device.name, 'partition').read_text().strip() for name, device in devices.items()}
    if not parents[current] or parents[current] != parents[target] or numbers != {'A':'3','B':'4'}:
        raise Error('Das A/B-Partitionslayout ist nicht eindeutig. Kein Systemupdate ausgeführt.', 409)


def deployment(slot, record):
    from .updates import release_metadata
    identity = validate_identity(record['identity'])
    return {'slot': slot, 'version': identity['version'], 'digest': identity['release_id'],
            'image': 'Debian 13 · System ' + slot, 'incompatible': False,
            'installed_at': record.get('installed_at'), 'confirmed': record['confirmed'],
            **release_metadata(identity)}


def system_status():
    from .updates import scheduled_reboot, release_metadata
    info = image_info()
    rauc, slots, current = rauc_status()
    state = load_state()
    record = state['slots'].get(current)
    if record and record['identity']['release_id'] != info['release_id']:
        raise Error('Gestartetes System und gespeicherter Slotstatus widersprechen sich.', 503)
    booted = deployment(current, record or {'identity': info, 'confirmed': False})
    pending = state.get('pending')
    if rauc.get('boot_primary') not in SLOTS.values():
        raise Error('Kein gültiger nächster Systemslot. RAUC-Status prüfen.', 503)
    next_slot = next(name for name, identifier in SLOTS.items() if identifier == rauc['boot_primary'])
    staged = None
    if next_slot != current:
        target = state['slots'].get(next_slot)
        if not target or not pending or pending['slot'] != next_slot or pending['digest'] != target['identity']['release_id']:
            raise Error('Nicht zugeordneter Systemwechsel. RAUC-Status prüfen.', 409)
        staged = deployment(next_slot, target)
    options = [deployment(name, item) for name, item in state['slots'].items()
               if name != current and item['confirmed'] and slots[name].get('boot_status') == 'good'
               and item['identity']['release_id'] != info['release_id']]
    schedule = scheduled_reboot()
    rollback_pending = bool(staged and pending['kind'] == 'rollback')
    healthy = booted['confirmed'] and BOOT_OK.is_file() and BOOT_OK.read_text().strip() == info['release_id']
    ready = healthy and not pending and not staged and schedule is None
    return {'platform': 'debian-rauc', 'update_kind': 'image', 'current': info['version'],
            **release_metadata(info),
            'current_stage': info['release_stage'], 'architecture': info['architecture'],
            'image_repository': 'ra5on/TitanOS', 'booted': booted,
            'staged': staged if not rollback_pending else None,
            'rollback': options[0] if options else None, 'rollback_options': options,
            'next_boot': staged or booted, 'rollback_queued': rollback_pending,
            'reboot_required': bool(staged), 'reboot_scheduled': schedule is not None,
            'reboot_schedule': schedule, 'automatic_reboot': False,
            'rollback_available': ready and bool(options), 'health_confirmed': healthy,
            'rollback_reason': ('Vorherigen bestätigten Systemstand auswählen; Neustart separat bestätigen.' if ready and options else
                'Noch kein vorheriger bestätigter Stand verfügbar, ein Wechsel ist vorbereitet oder die Startprüfung läuft.'),
            'last_failure': state.get('last_failure'),
            'output': 'Signierte Debian-Systemupdates auf zwei getrennten Systempartitionen.'}


def validate_manifest(value):
    validate_identity(value)
    if any(value.get(field) != 'passed' for field in ('boot_test', 'runtime_test', 'update_test', 'rollback_test')):
        raise Error('Das Systemupdate hat nicht alle Start-, Laufzeit- und Rollback-Prüfungen bestanden.', 409)
    bundle = value.get('bundle')
    if (not isinstance(bundle, dict) or not isinstance(bundle.get('name'), str) or
            not re.fullmatch(r'titan-[A-Za-z0-9.-]+-amd64\.raucb', bundle['name']) or
            type(bundle.get('size')) is not int or not 0 < bundle['size'] <= MAX_BUNDLE or
            not isinstance(bundle.get('sha256'), str) or not re.fullmatch(r'[a-f0-9]{64}', bundle['sha256']) or
            not isinstance(value.get('rootfs_sha256'), str) or not re.fullmatch(r'[a-f0-9]{64}', value['rootfs_sha256'])):
        raise Error('Ungültige signierte Systemupdate-Datei.', 409)
    return value


def matches_selection(manifest, installed, update_kind, channel=None):
    """Both release type and frozen app compatibility come from signed data."""
    from . import updates as common
    target, current = common.release_metadata(manifest), common.release_metadata(installed)
    app_order = (common.staged_version(target['titan_version'], target['titan_stage']) >
                 common.staged_version(current['titan_version'], current['titan_stage']))
    if update_kind == 'system' and target['release_kind'] != 'system':
        return False
    if target['release_kind'] == 'system':
        # A complete maintained bundle for a genuinely newer application can
        # upgrade Titan as well. This matters when stable OS revisions have
        # advanced past the newer application's original global release tag.
        if update_kind in ('all', 'titan') and app_order:
            return channel is None or common.allowed_stage(channel, target['titan_stage'])
        if update_kind == 'titan':
            return False
        if any(target[key] != current[key] for key in ('titan_version', 'titan_stage', 'titan_source_commit')):
            return False
        return target['system_revision'] > current['system_revision']
    # A global OS release line can advance independently of the application
    # version. Compare app identity as well so a later OS version never smuggles
    # an older Titan application onto the active NAS.
    return (common.staged_version(target['titan_version'], target['titan_stage']) >= common.staged_version(current['titan_version'], current['titan_stage'])
            and (channel is None or common.allowed_stage(channel, target['titan_stage'])))


def check(repo, channel='stable', token=None, update_kind='all'):
    from . import updates as common
    repo = common.repository(repo)
    common.selection_kind(update_kind)
    if channel not in common.STAGES:
        raise Error('Ungültiger Update-Kanal.')
    result = {'repository': repo, 'channel': channel, 'checked': time.time(), 'available': False,
              'update_kind': 'image', 'selection_kind': update_kind, 'automatic_reboot': False}
    try:
        info = image_info()
        result.update(system_status())
        result.update(common.release_metadata(info))
        releases = common.strict_json(common.fetch(f'https://api.github.com/repos/{repo}/releases?per_page=100', token))
        if not isinstance(releases, list):
            raise Error('Ungültige GitHub-Release-Liste.', 503)
        candidates = []
        for release in releases:
            if not isinstance(release, dict) or release.get('draft'):
                continue
            try:
                stage = common.release_stage(release)
                if common.allowed_stage(channel, stage):
                    candidates.append((common.staged_version(release['tag_name'], stage), release, stage))
            except (Error, KeyError):
                continue
        selected = None
        current_version = common.staged_version(info['version'], info['release_stage'])
        for candidate_version, release, stage in sorted(candidates, key=lambda item: item[0], reverse=True):
            if candidate_version <= current_version:
                continue
            manifest, assets = common.verified_release(release, token)
            validate_manifest(manifest)
            if (manifest['release_stage'] != stage or common.version(manifest['version']) != common.version(release['tag_name']) or
                    manifest['bundle']['name'] not in assets):
                raise Error('Signiertes Update und GitHub-Release stimmen nicht überein.')
            if not matches_selection(manifest, info, update_kind, channel):
                continue
            if manifest['system_accounts'] != info['system_accounts']:
                raise Error('Geänderte Systemkonten erfordern eine gesonderte Migration. Kein Update ausgeführt.', 409)
            selected = release, stage, manifest, assets
            break
        if selected is None:
            labels = {'all': 'Titan-Debian-Systemupdate', 'titan': 'Titan-Update', 'system': 'kompatibles System- und Sicherheitsupdate'}
            return {**result, 'message': 'Noch kein passendes ' + labels[update_kind] + ' veröffentlicht.'}
        release, stage, manifest, assets = selected
        metadata = common.release_metadata(manifest)
        ready = result['health_confirmed'] and not result['reboot_required'] and not result['reboot_scheduled']
        result.update(latest=release['tag_name'], latest_stage=stage, notes=release.get('body', ''),
                      url=release['html_url'], assets=assets, signed=True, signature_verified=True,
                      image='Debian A/B · ' + manifest['release_id'],
                      release_kind=metadata['release_kind'], latest_titan_version=metadata['titan_version'],
                      latest_titan_stage=metadata['titan_stage'], latest_titan_source_commit=metadata['titan_source_commit'],
                      latest_system_revision=metadata['system_revision'], package_changes=metadata['package_changes'],
                      security_summary=metadata['security_summary'],
                      available=ready and matches_selection(manifest, info, update_kind, channel) and common.staged_version(manifest['version'], stage) >
                      common.staged_version(info['version'], info['release_stage']))
        return result
    except Error as exc:
        return {**result, 'available': False, 'error': str(exc)}


def download(url, destination, bundle, token=None):
    from . import updates as common
    common.validate_url(url)
    if shutil.disk_usage(destination.parent).free < bundle['size'] + 512 * 1024**2:
        raise Error('Nicht genügend freier Platz für das Systemupdate.', 409)
    headers = {'User-Agent': 'Titan-System-Update', 'Accept': 'application/octet-stream'}
    if token and urllib.parse.urlsplit(url).hostname == 'api.github.com':
        headers['Authorization'] = 'Bearer ' + token
    digest, size, deadline = hashlib.sha256(), 0, time.monotonic() + 1800
    last_report = 0
    report('download', received=0, total=bundle['size'])
    try:
        with urllib.request.build_opener(common.Redirect()).open(urllib.request.Request(url, headers=headers), timeout=30) as response, destination.open('xb') as output:
            while block := response.read(1024 * 1024):
                size += len(block)
                if size > bundle['size'] or time.monotonic() > deadline:
                    raise Error('Systemupdate-Download überschreitet die geprüfte Größe oder Zeitgrenze.')
                digest.update(block)
                output.write(block)
                if time.monotonic() - last_report >= 0.5:
                    report('download', received=size, total=bundle['size'])
                    last_report = time.monotonic()
            output.flush()
            os.fsync(output.fileno())
    except Error:
        raise
    except (OSError, ValueError):
        raise Error('Systemupdate konnte nicht vollständig heruntergeladen werden.', 503) from None
    if size != bundle['size'] or digest.hexdigest() != bundle['sha256']:
        raise Error('Prüfsumme oder Größe des Systemupdates stimmt nicht.', 409)


def activate(slot, record, state, kind):
    _, _, current = rauc_status()
    verify_devices(current, slot)
    state['pending'] = {'slot': slot, 'digest': record['identity']['release_id'], 'kind': kind}
    save_state(state)
    run(['rauc', 'status', 'mark-active', SLOTS[slot]], timeout=60)
    status = system_status()
    if status['next_boot']['digest'] != record['identity']['release_id'] or not status['reboot_required']:
        raise Error('RAUC hat den vorbereiteten Systemwechsel nicht bestätigt.', 503)
    return status


@locked
@tracked
def install(repo, channel, expected_version, database, update_kind='all'):
    from . import updates as common
    token = common.read_token()
    common.selection_kind(update_kind)
    offer = check(repo, channel, token, update_kind)
    if offer.get('error') or not offer.get('available') or offer.get('latest') != expected_version:
        raise Error(offer.get('error') or 'Update-Angebot hat sich geändert. Bitte erneut prüfen.', 409)
    manifest, assets = common.verified_release({'assets': [{'name': name, 'url': url} for name, url in offer['assets'].items()], 'html_url': offer['url']}, token)
    validate_manifest(manifest)
    installed = image_info()
    if manifest['system_accounts'] != installed['system_accounts']:
        raise Error('Systemkontenvertrag des Updates ist nicht kompatibel.', 409)
    if (not matches_selection(manifest, installed, update_kind, channel) or
            common.staged_version(manifest['version'], manifest['release_stage']) <=
            common.staged_version(installed['version'], installed['release_stage'])):
        raise Error('Update-Bereich oder kompatibler Systemstand hat sich geändert.', 409)
    if (common.version(manifest['version']) != common.version(expected_version) or
            manifest['release_stage'] != offer.get('latest_stage') or
            not common.allowed_stage(channel, manifest['release_stage'])):
        raise Error('Update-Version oder Kanal hat sich geändert.', 409)
    current = system_status()
    if current['reboot_required'] or current['reboot_scheduled'] or not current['health_confirmed']:
        raise Error('Systemwechsel ist derzeit gesperrt.', 409)
    target = 'B' if current['booted']['slot'] == 'A' else 'A'
    report('backup')
    backup = common.backup_configuration(database)
    CACHE.mkdir(parents=True, exist_ok=True, mode=0o700)
    with tempfile.TemporaryDirectory(prefix='install-', dir=CACHE) as work:
        bundle = Path(work) / manifest['bundle']['name']
        download(assets[manifest['bundle']['name']], bundle, manifest['bundle'], token)
        report('verification')
        # RAUC performs its own certificate, compatible and verity verification.
        run(['rauc', '--conf=/etc/rauc/system.conf', 'info', '--keyring=/usr/share/titan/rauc-root.pem', str(bundle)], timeout=120)
        fresh = system_status()
        if fresh['booted'] != current['booted'] or fresh['reboot_required'] or fresh['reboot_scheduled']:
            raise Error('Systemstatus hat sich während des Downloads geändert.', 409)
        verify_devices(current['booted']['slot'], target)
        state = load_state()
        # Invalidate the overwritten rollback entry before writing. Interrupted
        # installs must never leave an old digest selectable for a partial slot.
        state['slots'].pop(target, None)
        save_state(state)
        report('writing')
        try:
            run(['rauc', 'install', str(bundle)], timeout=1800)
        except Error as exc:
            # The CLI often reports only 'Installing ... failed' on stderr;
            # the service journal contains the actual installer failure.
            try:
                detail = run(['journalctl', '-u', 'rauc.service', '-n', '35', '--no-pager', '-o', 'cat'], timeout=15)
            except Error:
                detail = ''
            raise Error('Systemupdate fehlgeschlagen. ' + str(exc) + ('\n' + detail[-3000:] if detail else ''), 503) from None
        report('confirming')
        _, slots, booted = rauc_status()
        detail = slots[target].get('slot_status', {})
        if (booted != current['booted']['slot'] or detail.get('checksum', {}).get('sha256') != manifest['rootfs_sha256'] or
                detail.get('bundle', {}).get('build') != manifest['release_id']):
            raise Error('Der inaktive Systemslot wurde nicht vollständig bestätigt.', 503)
        verify_devices(current['booted']['slot'], target)
        device = str(Path('/dev/disk/by-partlabel/TITAN-' + target).resolve(strict=True))
        # RAUC has finished and unmounted its post-install hook mount. Changing
        # an ext4 UUID while mounted is forbidden; verify that boundary first.
        mounted = run(['findmnt', '--json', '--list', '--output', 'SOURCE'], timeout=10)
        mounts = common.strict_json(mounted).get('filesystems', [])
        if any(str(Path(row.get('source', '').split('[')[0]).resolve()) == device for row in mounts):
            raise Error('Der neue Systemslot ist noch eingehängt. Keine Aktivierung.', 409)
        run(['tune2fs', '-U', 'random', device], timeout=120)
        run(['sync'], timeout=60)
        identity_keys = ('format', 'platform', 'compatible', 'architecture', 'state_schema', 'release_id',
                         'release_stage', 'version', 'system_accounts', 'source_commit', 'update_kind',
                         'titan_version', 'titan_stage', 'titan_source_commit', 'system_revision',
                         'package_changes', 'security_summary')
        record = {'identity': {key: manifest[key] for key in identity_keys if key in manifest},
                  'confirmed': False, 'installed_at': time.time(), 'rootfs_sha256': manifest['rootfs_sha256']}
        state['slots'][target] = record
        prepared = activate(target, record, state, 'update')
    return {'ok': True, 'version': expected_version, 'backup': backup, 'staged': prepared['staged'],
            **common.release_metadata(manifest),
            'update_kind': 'image', 'reboot_required': True, 'automatic_reboot': False,
            'message': 'Debian-Systemupdate geprüft und vorbereitet. Neustart separat bestätigen.'}


@locked
def rollback(repo, expected_digest, confirmation, database):
    from . import updates as common
    common.repository(repo)
    common.validate_system_action('update_rollback', {'expected_digest': expected_digest, 'confirmation': confirmation})
    current = system_status()
    target = next((item for item in current['rollback_options'] if item['digest'] == expected_digest), None)
    if not current['rollback_available'] or not target:
        raise Error('Der gewählte Rückkehrstand ist nicht mehr verfügbar.', 409)
    backup = common.backup_configuration(database)
    fresh = system_status()
    if fresh != current:
        raise Error('Systemstatus hat sich geändert. Rollback erneut auswählen.', 409)
    state = load_state()
    prepared = activate(target['slot'], state['slots'][target['slot']], state, 'rollback')
    return {'ok': True, 'backup': backup, 'rollback': prepared['next_boot'], 'rollback_queued': True,
            'reboot_required': True, 'automatic_reboot': False, 'message': 'Rollback vorbereitet; Neustart separat bestätigen.'}


@locked
def reboot(repo, expected_digest, confirmation, database):
    from . import updates as common
    common.repository(repo)
    common.validate_system_action('system_reboot', {'expected_digest': expected_digest, 'confirmation': confirmation})
    current = system_status()
    if current['reboot_scheduled'] or current['next_boot']['digest'] != expected_digest:
        raise Error('Geplanter Systemstand hat sich geändert oder ein Neustart ist bereits geplant.', 409)
    if run(['virsh', '-c', 'qemu:///system', 'list', '--name'], timeout=30).strip():
        raise Error('Laufende virtuelle Maschinen zuerst geordnet herunterfahren.', 409)
    backup = common.backup_configuration(database)
    fresh = system_status()
    if fresh['next_boot'] != current['next_boot'] or fresh['reboot_scheduled']:
        raise Error('Systemstatus hat sich geändert.', 409)
    run(['shutdown', '-r', '+1', 'Titan: bestätigter Systemneustart'], timeout=30)
    schedule = common.scheduled_reboot()
    if not schedule or schedule['mode'] != 'reboot':
        raise Error('Der Neustart wurde nicht bestätigt.', 503)
    return {'ok': True, 'backup': backup, 'next_boot': fresh['next_boot'], 'reboot_scheduled': True,
            'reboot_schedule': schedule, 'delay_seconds': 60, 'automatic_reboot': False}


def _unit_properties(unit, properties):
    try:
        text = run(['systemctl', 'show', unit, '--property=' + ','.join(properties)], timeout=15)
        if len(text) > 16384:
            return None
        rows = {}
        for line in text.splitlines():
            if '=' not in line:
                continue
            key, value = line.split('=', 1)
            if key in rows:
                return None
            rows[key] = value
        return rows
    except Error:
        return None


def _daemon_denial(unit, start, stop):
    """A root-owned current-boot marker belongs to this exact main execution."""
    from .updates import strict_json
    descriptor = None
    try:
        helper = BOOT_DAEMON_GUARD.lstat()
        if (os.geteuid() != 0 or not stat.S_ISREG(helper.st_mode) or helper.st_uid != 0 or
                helper.st_mode & 0o022):
            return False
        path = DAEMON_MARKERS[unit]
        descriptor = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
        before = os.fstat(descriptor)
        if (not stat.S_ISREG(before.st_mode) or before.st_uid != 0 or before.st_mode & 0o022 or
                not 0 < before.st_size <= 4096):
            return False
        with os.fdopen(descriptor, 'rb') as stream:
            descriptor = None
            raw = stream.read(4097)
            after = os.fstat(stream.fileno())
        current = path.lstat()
        fingerprint = lambda info: (info.st_dev, info.st_ino, info.st_size, info.st_mtime_ns, info.st_ctime_ns)
        if fingerprint(before) != fingerprint(after) or fingerprint(after) != fingerprint(current) or len(raw) != before.st_size:
            return False
        marker = strict_json(raw)
        boot = BOOT_ID.read_text().strip()
        return (isinstance(marker, dict) and set(marker) == {'format', 'component', 'boot_id', 'reason', 'denied_monotonic_us'} and
                marker['format'] == 'titan-boot-daemon-denial-v1' and
                marker['component'] == DAEMON_COMMANDS[unit][0] and
                re.fullmatch(r'[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}', boot) is not None and
                marker['boot_id'] == boot and marker['reason'] == 'insufficient_memory' and
                type(marker['denied_monotonic_us']) is int and
                0 < start <= marker['denied_monotonic_us'] <= stop)
    except (OSError, ValueError, TypeError, Error):
        return False
    finally:
        if descriptor is not None:
            os.close(descriptor)


def _guard_failed_unit(unit):
    """Only a marked guard failure of the main process excuses a stopped daemon.

    Main status survives daemon-reload; a native daemon's coincidental exit 78
    cannot pass because its wrapper first removes the denial marker.
    """
    if unit not in DAEMON_COMMANDS:
        return False
    rows = _unit_properties(unit, ('LoadState', 'ActiveState', 'Result', 'ExecMainCode', 'ExecMainStatus',
        'ExecMainStartTimestampMonotonic', 'ExecMainExitTimestampMonotonic', 'RestartPreventExitStatus', 'ExecStart'))
    if (not rows or rows.get('LoadState') != 'loaded' or rows.get('ActiveState') != 'failed' or
            rows.get('Result') not in ('exit-code', 'start-limit-hit') or
            rows.get('ExecMainCode') != '1' or rows.get('ExecMainStatus') != '78' or
            '78' not in rows.get('RestartPreventExitStatus', '').split()):
        return False
    try:
        raw_times = [rows[key] for key in ('ExecMainStartTimestampMonotonic', 'ExecMainExitTimestampMonotonic')]
        if any(not re.fullmatch(r'[0-9]{1,19}', value) for value in raw_times):
            return False
        start, stop = map(int, raw_times)
    except (ValueError, KeyError):
        return False
    if not 0 < start <= stop < 2**63:
        return False
    entries = re.findall(r'\{([^{}]*)\}', rows.get('ExecStart', ''))
    if len(entries) != 1:
        return False
    fields = {}
    for part in entries[0].split(';'):
        if '=' not in part:
            continue
        key, value = part.strip().split('=', 1)
        if key in fields:
            return False
        fields[key] = value
    if (fields.get('path') != '/usr/bin/python3' or fields.get('argv[]') != DAEMON_COMMANDS[unit][1] or
            fields.get('ignore_errors') != 'no'):
        return False
    return _daemon_denial(unit, start, stop)


def verified_guard_socket(unit):
    """A fixed socket may hit its start limit only with its own verified denial.

    This never opens the socket or activates a daemon. Missing/masked sockets,
    unrelated failures and changed service/listener definitions remain fatal.
    """
    expected = {'docker.socket': ('docker.service', '/run/docker.sock (Stream)'),
                'libvirtd.socket': ('libvirtd.service', '/run/libvirt/libvirt-sock (Stream)')}
    if unit not in expected:
        return False
    # Socket Service= is a unit-file directive, not a D-Bus property. systemd
    # exposes the resolved activation relationship through Unit.Triggers.
    properties = ('LoadState', 'ActiveState', 'SubState', 'Result', 'Triggers', 'Listen')
    rows = _unit_properties(unit, properties)
    service, listener = expected[unit]
    if (not rows or rows.get('LoadState') != 'loaded' or rows.get('Triggers', '').split() != [service] or
            rows.get('Listen') != listener):
        return False
    if (rows.get('ActiveState') == 'active' and rows.get('SubState') in ('listening', 'running') and
            rows.get('Result') == 'success'):
        return True
    if (rows.get('ActiveState') != 'failed' or
            rows.get('Result') not in ('service-start-limit-hit', 'trigger-limit-hit')):
        return False
    return bool(verified_boot_memory_block((service,))) and rows == _unit_properties(unit, properties)


def _fresh_memory_block():
    """Recompute offline: an old, malformed or untrusted report is no exemption."""
    from .app_memory import system_reserve
    from .updates import strict_json
    descriptor = None
    try:
        helper = BOOT_MEMORY_GUARD.lstat()
        if os.geteuid() != 0 or not stat.S_ISREG(helper.st_mode) or helper.st_uid != 0 or helper.st_mode & 0o022:
            return None
        try:
            previous = BOOT_MEMORY_REPORT.lstat()
            old = (previous.st_dev, previous.st_ino, previous.st_mtime_ns)
        except FileNotFoundError:
            old = None
        started = time.time()
        result = subprocess.run(['/usr/bin/python3', '-I', str(BOOT_MEMORY_GUARD), '--component', 'all'],
            stdin=subprocess.DEVNULL, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
            timeout=15, env={'PATH': '/usr/sbin:/usr/bin:/sbin:/bin', 'LC_ALL': 'C.UTF-8'})
        if result.returncode != 1:
            return None
        descriptor = os.open(BOOT_MEMORY_REPORT, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
        before = os.fstat(descriptor)
        if (not stat.S_ISREG(before.st_mode) or before.st_uid != 0 or before.st_mode & 0o022 or
                not 0 < before.st_size <= 16384 or
                (before.st_dev, before.st_ino, before.st_mtime_ns) == old):
            return None
        with os.fdopen(descriptor, 'rb') as stream:
            descriptor = None
            raw = stream.read(16385)
            after = os.fstat(stream.fileno())
        current = BOOT_MEMORY_REPORT.lstat()
        fingerprint = lambda info: (info.st_dev, info.st_ino, info.st_size, info.st_mtime_ns, info.st_ctime_ns)
        if fingerprint(before) != fingerprint(after) or fingerprint(after) != fingerprint(current) or len(raw) != before.st_size:
            return None
        value = strict_json(raw)
        now = time.time()
        fields = ('total_bytes', 'reserve_bytes', 'required_bytes')
        if (not isinstance(value, dict) or value.get('ok') is not False or
                value.get('reason') != 'insufficient_memory' or
                any(type(value.get(key)) is not int or not 0 < value[key] < 2**63 for key in fields) or
                value['required_bytes'] <= value['total_bytes'] or
                value['reserve_bytes'] != system_reserve(value['total_bytes']) or
                type(value.get('checked_at')) is not int or not started - 1 <= value['checked_at'] <= now + 1 or
                not started - 1 <= before.st_mtime <= now + 1):
            return None
        budgets = value.get('components')
        if not isinstance(budgets, dict) or set(budgets) != {'docker', 'vms'}:
            return None
        for budget in budgets.values():
            if (not isinstance(budget, dict) or
                    any(type(budget.get(key)) is not int or not 0 <= budget[key] < 2**63 for key in ('count', 'limit_bytes'))):
                return None
        if value['required_bytes'] != value['reserve_bytes'] + sum(item['limit_bytes'] for item in budgets.values()):
            return None
        return {key: value[key] for key in fields}
    except (OSError, ValueError, TypeError, subprocess.TimeoutExpired, Error):
        return None
    finally:
        if descriptor is not None:
            os.close(descriptor)


def _guard_management_health(report):
    """The reachable root agent must also report the freshly measured capacity."""
    from .rpc import receive, send
    deadline = time.monotonic() + 15
    try:
        with socket.socket(socket.AF_UNIX, socket.SOCK_STREAM) as stream:
            class Bounded:
                def remaining(self):
                    remaining = deadline - time.monotonic()
                    if remaining <= 0:
                        raise TimeoutError()
                    stream.settimeout(remaining)

                def recv(self, size):
                    self.remaining()
                    return stream.recv(size)

                def sendall(self, data):
                    self.remaining()
                    stream.sendall(data)

            bounded = Bounded()
            bounded.remaining()
            stream.connect('/run/titan/agent.sock')
            send(bounded, {'operation': 'status', 'arguments': {}})
            result = receive(bounded).get('result')
            if (not isinstance(result, dict) or type(result.get('memory_total')) is not int or
                    result['memory_total'] != report['total_bytes']):
                raise Error('Startprüfung: titan-agent.service meldet keinen gültigen Systemstatus.', 503)
    except (OSError, ValueError, TypeError, AttributeError, Error):
        raise Error('Startprüfung: titan-agent.service meldet keinen gültigen Systemstatus.', 503) from None


def verified_boot_memory_block(units=('docker.service', 'libvirtd.service')):
    """Shared runtime/health proof; no daemon activation and no agent dependency.

    Return bounded numeric capacity only after exact failed guard units and a
    fresh offline recomputation agree. Core service/program checks remain the
    caller's responsibility; unknown metadata is never an allowed RAM mode.
    """
    if (not isinstance(units, (tuple, list)) or not units or len(units) > 2 or
            len(set(units)) != len(units) or set(units) - {'docker.service', 'libvirtd.service'}):
        return None
    if not all(_guard_failed_unit(unit) for unit in units):
        return None
    report = _fresh_memory_block()
    if not report or not all(_guard_failed_unit(unit) for unit in units):
        return None
    return report


@locked
def confirm_boot():
    """Called once per boot after the complete NAS stack passes its start checks."""
    info = image_info()
    _, slots, current = rauc_status()
    state = load_state()
    existing = state['slots'].get(current)
    if existing and existing['identity']['release_id'] != info['release_id']:
        raise Error('Startprüfung: Slotkennung stimmt nicht mit dem gestarteten System überein.', 503)
    if (existing and existing['confirmed'] and BOOT_OK.is_file() and
            BOOT_OK.read_text().strip() == info['release_id']):
        # A manual restart of the health unit in this boot must not interpret
        # an update that is waiting for reboot as a failed boot attempt.
        return {'ok': True, 'slot': current, 'version': info['version'], 'already_confirmed': True}
    # Shared state must actually be on the dedicated data partition. A missing
    # initramfs bind mount must never be mistaken for a healthy empty NAS.
    for path in ('/var/lib/titan-system', '/etc', '/var/lib/titan', '/var/lib/docker',
                 '/var/lib/libvirt', '/var/lib/samba', '/var/srv/titan'):
        run(['mountpoint', '-q', path], timeout=10)
    for unit in ('titan-firstboot.service', 'titan-runtime.service', 'titan-agent.service',
                 'titan-web.service', 'titan-proxy.service',
                 'smbd.service', 'virtlogd.socket', 'virtlockd.socket'):
        # systemctl is-active with multiple units succeeds when ANY is active.
        # Each required service must be checked independently.
        try:
            run(['systemctl', 'is-active', '--quiet', unit], timeout=30)
        except Error:
            raise Error('Startprüfung: ' + unit + ' ist nicht verfügbar.', 503) from None
    memory_block = None
    blocked = []
    try:
        run(['systemctl', 'is-active', '--quiet', 'docker.service'], timeout=30)
    except Error:
        memory_block = verified_boot_memory_block(('docker.service',))
        if not memory_block:
            raise Error('Startprüfung: docker.service ist nicht verfügbar.', 503) from None
        blocked.append('docker')
    if 'docker' not in blocked:
        try:
            run(['docker', 'info', '--format', '{{.ServerVersion}}'], timeout=30)
        except Error:
            raise Error('Startprüfung: docker.service antwortet nicht.', 503) from None
    if _guard_failed_unit('libvirtd.service'):
        vm_block = verified_boot_memory_block(('libvirtd.service',))
        if not vm_block:
            raise Error('Startprüfung: libvirtd.service ist nicht verfügbar.', 503)
        memory_block = vm_block
        blocked.append('vms')
    else:
        try:
            run(['virsh', '-c', 'qemu:///system', 'list', '--all', '--name'], timeout=30)
        except Error:
            # Socket activation may have reached the guard only on this probe.
            vm_block = verified_boot_memory_block(('libvirtd.service',))
            if not vm_block:
                raise Error('Startprüfung: libvirtd.service antwortet nicht.', 503) from None
            memory_block = vm_block
            blocked.append('vms')
    for component, unit in (('docker', 'docker.socket'), ('vms', 'libvirtd.socket')):
        if component in blocked:
            if not verified_guard_socket(unit):
                raise Error('Startprüfung: ' + unit + ' ist nicht verfügbar.', 503)
        else:
            try:
                run(['systemctl', 'is-active', '--quiet', unit], timeout=30)
            except Error:
                raise Error('Startprüfung: ' + unit + ' ist nicht verfügbar.', 503) from None
    if blocked:
        _guard_management_health(memory_block)
    run(['testparm', '-s'], timeout=30)
    origin = urllib.parse.urlsplit(os.environ.get('TITAN_ORIGIN', ''))
    if (origin.scheme != 'https' or not origin.hostname or origin.port != 5000 or
            origin.username or origin.password or origin.path or origin.query or origin.fragment):
        raise Error('Startprüfung: Ungültige NAS-Adresse.', 503)
    connection = []
    try:
        ipaddress.ip_address(origin.hostname)
    except ValueError:
        connection = ['--connect-to', origin.netloc + ':127.0.0.1:5000']
    # TLS clients do not send SNI for literal IPs. In that case Caddy selects
    # the certificate by the socket's local IP, so keep the real NAS address.
    run(['curl', '--fail', '--silent', '--insecure', '--noproxy', '*', '--max-time', '15',
         *connection, origin.geturl() + '/api/session'], timeout=20)
    pending = state.get('pending')
    if pending and pending['slot'] == current and pending['digest'] != info['release_id']:
        raise Error('Startprüfung: Erwarteter Systemwechsel stimmt nicht überein.', 503)
    if pending and pending['slot'] != current:
        failed = pending['slot']
        run(['rauc', 'status', 'mark-bad', SLOTS[failed]], timeout=30)
        if failed in state['slots']:
            state['slots'][failed]['confirmed'] = False
        state['last_failure'] = {'slot': failed, 'digest': pending['digest'], 'time': time.time(),
                                 'reason': 'Der vorbereitete Stand hat seinen Start nicht bestätigt.'}
    run(['rauc', 'status', 'mark-good', 'booted'], timeout=30)
    run(['rauc', 'status', 'mark-active', 'booted'], timeout=30)
    record = existing or {'identity': info, 'installed_at': time.time()}
    record['confirmed'] = True
    state['slots'][current] = record
    state.pop('pending', None)
    save_state(state)
    BOOT_OK.write_text(info['release_id'] + '\n')
    return {'ok': True, 'slot': current, 'version': info['version'],
            **({'ram_protection': {'active': True, 'blocked': blocked}} if blocked else {})}


if __name__ == '__main__':
    confirm_boot()
