"""Exercise the legacy workload on a release-signed predecessor in the disposable upgrade VM."""
import datetime
import json
import re
import runpy
import shlex
import subprocess
import urllib.error
from pathlib import Path

SCRIPTS = Path(__file__).resolve().parent
KEYS = SCRIPTS.parent / 'build/mkosi.extra/usr/share/titanos/update-keys'
LEGACY = '/api/v1/agent'
PERSONAL = '/api/v1/personal-agent'
OWNER = 'systemtester'
MARKER = 'cutover-legacy-nur-im-archiv'


def verify_previous(version, directory, keys):
    archive = directory / f'titanos-{version}.zip'
    manifest = directory / 'manifest.json'
    # CI also builds with rehearsal.pub. Only the published release key family
    # is eligible to authenticate both historical artifacts.
    trusted = sorted(keys.glob('titanos-release-*.pub')) + sorted(keys.glob('titanos-recovery-*.pub'))
    for key in trusted:
        if all(subprocess.run(['minisign', '-V', '-q', '-p', str(key), '-m', str(path)],
                              capture_output=True).returncode == 0 for path in (manifest, archive)):
            break
    else:
        raise ValueError('old manifest and ZIP must share an official release signature')
    payload = json.loads(manifest.read_bytes())
    if (payload['channel'], payload['tag'], payload['version']) != ('stable', 'v' + version, version):
        raise ValueError('signed manifest does not identify the selected stable release')
    return archive, payload


def previous_release(version, directory):
    if not re.fullmatch(r'[0-9]+\.[0-9]+\.[0-9]+', version):
        raise ValueError('expected a stable release version')
    tag = 'v' + version
    repo = 'ra5on/TitanOS'
    release = json.loads(subprocess.check_output(['gh', 'api', f'repos/{repo}/releases/tags/{tag}']))
    if release['draft'] or release['prerelease'] or release['tag_name'] != tag:
        raise ValueError('predecessor is not a published stable release')
    download = runpy.run_path(str(SCRIPTS / 'repack-release.py'))['download']
    for name in ['manifest.json', f'titanos-{version}.zip']:
        for asset in [name, name + '.minisig']:
            download(f'https://github.com/{repo}/releases/download/{tag}/{asset}', directory / asset)
    return verify_previous(version, directory, KEYS)


class Cutover:
    def __init__(self, directory, ssh_port, request, wait, rehearsal_public_key):
        self.request = request
        self.wait = wait
        self.rehearsal_public_key = rehearsal_public_key
        key = directory / 'cutover-ssh'
        subprocess.run(['ssh-keygen', '-q', '-t', 'ed25519', '-N', '', '-f', str(key)], check=True)
        self.pub = key.with_suffix('.pub').read_text().strip()
        self.ssh = ['ssh', '-i', str(key), '-o', 'IdentitiesOnly=yes', '-o', 'BatchMode=yes',
                    '-o', 'StrictHostKeyChecking=accept-new', '-o', 'ConnectTimeout=5',
                    '-o', f'UserKnownHostsFile={directory}/known_hosts', '-p', str(ssh_port),
                    OWNER + '@127.0.0.1']

    def remote(self, cmd):
        return subprocess.check_output([*self.ssh, 'sudo -n sh -ec ' + shlex.quote(cmd)],
                                       timeout=35, text=True).strip()

    def ssh_ready(self):
        return subprocess.run([*self.ssh, 'true'], capture_output=True, timeout=10).returncode == 0

    def old(self, password, old_manifest):
        self.request('/api/v1/account/ssh-keys', {'key': self.pub, 'password': password})
        self.request('/api/v1/system/ssh', {'enabled': True}, method='PUT')
        self.wait('owner SSH on disposable predecessor', self.ssh_ready, 90)
        digest = self.remote('sha256sum /usr/bin/titand').split()[0]
        assert digest == old_manifest['app']['sha256'], 'the booted daemon differs from the signed Stable'
        assert self.remote('systemctl show titanos-agent.service -p LoadState --value || true') == 'not-found'
        key = self.rehearsal_public_key
        assert key.is_file() and key.name == 'rehearsal.pub'
        # Enroll only the rehearsal's public key on the VM's volatile live root.
        subprocess.run([*self.ssh, 'sudo -n tee /usr/share/titanos/update-keys/rehearsal.pub'],
                       input=key.read_bytes(), stdout=subprocess.DEVNULL, check=True, timeout=20)
        self.request('/api/v1/system/data', {'volume': 'testdaten'}, method='PUT')
        self.request(LEGACY + '/settings', {'enabled': True, 'volume': 'testdaten', 'instruction': MARKER,
                                            'ollama': 'http://127.0.0.1:11434'}, method='PUT')
        assert self.request(LEGACY + '/settings')['instruction'] == MARKER
        self.chat = self.request(LEGACY + '/chats', {'draft': True})['id']
        assert re.fullmatch(r'[0-9a-f]{16}', self.chat)
        old_chat = f'{LEGACY}/chats/{self.chat}'
        self.request(old_chat, {'title': MARKER}, method='PATCH')
        assert self.request(old_chat)['title'] == MARKER
        # Exercise an actual old schedule without a model, mail or external recipient.
        name = 'Alter lokaler Kontrolllauf'
        response = self.request(LEGACY + '/schedules/new', {'name': name, 'task': name, 'kind': 'reminder',
                                'every': 'daily', 'at': '12:00', 'enabled': False}, method='PUT')
        schedule = next(x['id'] for x in response['schedules'] if x['name'] == name)
        assert re.fullmatch(r'[0-9a-f]{16}', schedule)
        self.old_run = self.request(f'{LEGACY}/schedules/{schedule}/run', {})['chat']
        assert re.fullmatch(r'[0-9a-f]{16}', self.old_run)
        history = self.request(f'{LEGACY}/chats/{self.old_run}')
        assert history['origin'] == 'reminder' and history['schedule'] == schedule

    def hashes(self):
        base = f'/mnt/testdaten/.titanos/agent/{OWNER}'
        files = ['/efi/config/system/agent.json', f'{base}/chats/{self.chat}.json',
                 f'{base}/chats/{self.old_run}.json', f'{base}/schedules.json']
        return self.remote('mountpoint -q /mnt/testdaten; sha256sum ' + shlex.join(files))

    def arm(self):
        """After the old updater staged B, just before the clean A shutdown."""
        response = self.request(LEGACY + '/schedules/new', {'name': MARKER, 'task': MARKER, 'kind': 'reminder',
                                'every': 'minutes', 'interval': 5, 'enabled': True}, method='PUT')
        entry = next(x for x in response['schedules'] if x['name'] == MARKER)
        assert entry['enabled'] and re.fullmatch(r'[0-9a-f]{16}', entry['id'])
        self.schedule = entry['id']
        self.due = int(datetime.datetime.fromisoformat(entry['next']).timestamp())
        assert 0 < self.due - int(self.remote('date +%s')) <= 300
        self.snapshot = self.hashes()
        print('Signed predecessor ran a reminder and saved the old chat and a second, enabled timer.', flush=True)

    def new(self):
        self.wait('owner SSH after system upgrade', self.ssh_ready, 90)
        self.remote('systemctl is-active --quiet titanos-agent.service')
        assert self.remote('systemctl show -p User --value titanos-agent.service') == '_titanos-agent'
        for method, path in [('GET', LEGACY + '/settings'), ('GET', f'{LEGACY}/chats/{self.chat}'),
                             ('POST', f'{LEGACY}/schedules/{self.schedule}/run')]:
            try:
                self.request(path, {} if method == 'POST' else None, method=method)
            except urllib.error.HTTPError as error:
                assert error.code == 404, (path, error.code)
            else:
                raise AssertionError('legacy route still active: ' + path)
        state = self.wait('personal data volume available', lambda: self.request(PERSONAL + '/state'), 90)
        assert not state['jobs'] and not state['chats'] and MARKER not in json.dumps(state)
        body = {'tool': 'agent.example', 'requestId': 'upgrade-from-stable', 'prompt': 'Kontrollauftrag'}
        job = self.request(PERSONAL + '/jobs', body)

        def finished():
            current = self.request(f"{PERSONAL}/jobs/{job['id']}")
            assert current['status'] in ('planned', 'running', 'done'), current['status']
            return current if current['status'] == 'done' else None

        done = self.wait('personal worker executed example', finished, 100)
        assert done['result']['text'] == '2 + 3 = 5'
        assert [(r['step'], r['tool']) for r in done['receipts']] == [(1, 'agent.example')]
        assert self.request(PERSONAL + '/jobs', body)['id'] == job['id']
        # Expire the old timer and allow for the legacy scheduler's one-minute tick.
        self.wait('old recurring reminder would have run', lambda: int(self.remote('date +%s')) >= self.due + 65, 390)
        assert self.snapshot == self.hashes(), 'legacy config, chat or schedule changed'
        state = self.request(PERSONAL + '/state')
        assert [x['id'] for x in state['jobs']] == [job['id']]
        assert MARKER not in json.dumps(state)
        messages = self.request('/api/v1/notifications')['messages']
        assert not any(MARKER in json.dumps(m) for m in messages), 'retired reminder ran'
        print('Archived config, chats and due timer unchanged; the personal worker completed one job.', flush=True)
