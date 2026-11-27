"""Guest side of smoke-agent-legacy.py. Requires a fresh isolated test volume."""
import contextlib
import datetime
import hashlib
import http.cookiejar
import http.server
import json
import os
import secrets
import shutil
import subprocess
import threading
import time
import urllib.error
import urllib.request
from pathlib import Path

import agent_legacy_pim_probe as pim_probe

BASE = 'http://127.0.0.1:45852'
OLD = '/api/v1/agent'
NEW = '/api/v1/personal-agent'
VOLUME = Path('/mnt/testdaten')
OWNER = 'systemtester'
GUEST = 'gast'
BUTTON = 'Cutover-Testknopf'


class Client:
    def __init__(self):
        self.opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))

    def request(self, path, data=None, method=None):
        if not path.startswith('/api/v1/'):
            raise ValueError('expected a TitanOS test API path')
        call = urllib.request.Request(BASE + path, data=json.dumps(data).encode() if data is not None else None,
                                      headers={'Origin': BASE, 'Content-Type': 'application/json'}, method=method)
        with self.opener.open(call, timeout=15) as response:
            value = response.read()
            return json.loads(value) if value else None

    def expect(self, code, path, data=None, method=None):
        try:
            self.request(path, data, method)
        except urllib.error.HTTPError as e:
            assert e.code == code, (path, e.code, e.read())
        else:
            raise AssertionError(f'{path}: expected {code}')


def until(label, check, seconds=35):
    deadline = time.monotonic() + seconds
    while time.monotonic() < deadline:
        result = check()
        if result:
            return result
        time.sleep(0.2)
    raise AssertionError('timed out: ' + label)


@contextlib.contextmanager
def daemon(which):
    logfile = Path('/tmp') / (which + '.log')
    with logfile.open('wb') as log:
        process = subprocess.Popen(['/testbin/' + which, '-dev', '-dev-listen', '127.0.0.1:45852', '-config', '/config'],
                                   stdout=log, stderr=log)
        try:
            def ready():
                assert process.poll() is None, 'daemon exited'
                try:
                    return Client().request('/api/v1/health')['ready']
                except OSError:
                    return False
            until(which, ready)
            yield logfile
        finally:
            process.terminate()
            process.wait(timeout=30)


class Model(http.server.BaseHTTPRequestHandler):
    """A deterministic, local Ollama fixture requests one ordinary text-file change."""
    def log_message(self, *_args):
        pass

    def do_GET(self):
        values = {'/api/version': {'version': '0.40.14'}, '/api/tags': {'models': [{'name': 'm'}]},
                  '/api/ps': {'models': [{'name': 'm', 'context_length': 32768, 'size_vram': 1}]}}
        self.reply(values.get(self.path, {}))

    def do_POST(self):
        body = json.loads(self.rfile.read(int(self.headers['Content-Length'])))
        if self.path == '/api/show':
            self.reply({'capabilities': ['completion', 'tools']})
            return
        assert self.path == '/api/chat', self.path
        messages = body.get('messages') or []
        message = {'role': 'assistant', 'content': 'Fertig.'}
        if messages and messages[-1]['role'] == 'user':
            for file in ('kontrolle.txt', 'offen.txt'):
                if messages[-1]['content'] == 'Schreibe in ' + file:
                    message = {'role': 'assistant', 'content': '', 'tool_calls': [{'function': {'name': 'file_write',
                               'arguments': {'root': 'volume/testdaten', 'path': file, 'content': 'geprüft', 'replace': True}}}]}
        if body.get('stream', True):
            self.reply({'message': message, 'done': False}, {'message': {'role': 'assistant', 'content': ''}, 'done': True})
        else:
            self.reply({'message': message, 'done': True})

    def reply(self, *objects):
        data = b'\n'.join(json.dumps(obj).encode() for obj in objects) + b'\n'
        self.send_response(200)
        self.send_header('Content-Type', 'application/json')
        self.send_header('Content-Length', str(len(data)))
        self.end_headers()
        self.wfile.write(data)


def mqtt_binary(name):
    extracted = Path('/mqtt-tools/usr')
    if extracted.is_dir():
        libdir = extracted / 'lib' / 'x86_64-linux-gnu'
        return [str(extracted / ('sbin' if name == 'mosquitto' else 'bin') / name)], dict(os.environ, LD_LIBRARY_PATH=str(libdir))
    executable = shutil.which(name)
    if not executable:
        raise RuntimeError(f'{name} is required')
    return [executable], os.environ


@contextlib.contextmanager
def start_fixtures():
    model = http.server.ThreadingHTTPServer(('127.0.0.1', 0), Model)
    threading.Thread(target=model.serve_forever, daemon=True).start()
    config = Path('/tmp/mosquitto.conf')
    config.write_text('listener 1883 127.0.0.1\nallow_anonymous true\npersistence false\n')
    cmd, env = mqtt_binary('mosquitto')
    broker = subprocess.Popen([*cmd, '-c', str(config)], env=env, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    try:
        yield model.server_port
    finally:
        broker.terminate()
        broker.wait(timeout=10)
        model.shutdown()
        model.server_close()


def publish(topic):
    cmd, env = mqtt_binary('mosquitto_pub')
    subprocess.run([*cmd, '-h', '127.0.0.1', '-q', '1', '-t', topic, '-m', 'run'], env=env, check=True, timeout=5)


def discover_button():
    cmd, env = mqtt_binary('mosquitto_sub')
    result = subprocess.run([*cmd, '-h', '127.0.0.1', '-t', 'homeassistant/button/#', '-W', '1', '-F', '%p'],
                            capture_output=True, env=env, timeout=5)
    if result.returncode not in (0, 27):
        raise RuntimeError(result.stderr.decode())
    for line in result.stdout.splitlines():
        if line and line != b'(null)':
            obj = json.loads(line)
            if obj.get('name') == 'KI-Agent: ' + BUTTON and obj['payload_press'] == 'run':
                return obj['command_topic']
    return None


def pending(client, name):
    path = VOLUME / name
    path.write_text('unverändert')
    created = client.request(OLD + '/chats', {'message': 'Schreibe in ' + name})
    route = f"{OLD}/chats/{created['id']}"

    def read():
        chat = client.request(route)
        assert chat['state'] not in ('error', 'stopped'), chat
        return chat if chat['state'] == 'waiting' else None
    chat = until('genuine pending file change', read)
    assert chat['pending']['tool'] == 'file_write' and path.read_text() == 'unverändert'
    return route + '/decide', {'id': chat['pending']['id'], 'approve': True}


def native_settings(client, port):
    client.request(OLD + '/settings', {'enabled': True, 'volume': 'testdaten', 'model': 'm',
                   'ollama': f'http://127.0.0.1:{port}', 'policy': {'preset': 'standard'},
                   'quickOff': True, 'users': [GUEST]}, 'PUT')


def before(clients, port):
    owner, guest = clients[OWNER], clients[GUEST]
    owner.request('/api/v1/system/data', {'volume': 'testdaten'}, 'PUT')
    native_settings(owner, port)
    guest.expect(403, OLD + '/settings')
    guestchat = guest.request(OLD + '/chats', {'draft': True})['id']
    owner.expect(404, f'{OLD}/chats/{guestchat}')
    # Positive control: the same owner and tool really can overwrite only after approval.
    control, approve = pending(owner, 'kontrolle.txt')
    owner.request(control, approve)
    until('old approved file changed', lambda: (VOLUME / 'kontrolle.txt').read_text() == 'geprüft')
    waiting, approve = pending(owner, 'offen.txt')
    guest.expect(404, waiting, approve)
    assert (VOLUME / 'offen.txt').read_text() == 'unverändert'

    future = (datetime.date.today() + datetime.timedelta(days=2)).isoformat()
    timer = owner.request(OLD + '/schedules/new', {'name': BUTTON, 'task': BUTTON, 'kind': 'reminder',
                          'every': 'once', 'date': future, 'at': '12:00', 'enabled': True, 'button': True}, 'PUT')['schedules'][0]
    # An ordinary HA integration uses its assigned admin and the retained MQTT discovery record.
    mqtt = {'enabled': True, 'host': '127.0.0.1', 'port': 1883, 'interval': 5,
            'groups': ['system'], 'commands': ['agent']}
    guest.expect(403, '/api/v1/homeassistant/settings', {'account': OWNER, 'mqtt': mqtt}, 'PUT')
    owner.request('/api/v1/homeassistant/settings', {'account': OWNER, 'mqtt': mqtt}, 'PUT')
    topic = until('old HA button discovery', discover_button)
    publish(topic)

    def old_button_ran():
        schedules = owner.request(OLD + '/schedules')['schedules']
        return next(x for x in schedules if x['id'] == timer['id']).get('chat')
    chat = until('legacy MQTT button executed its reminder', old_button_ran)
    history = owner.request(f'{OLD}/chats/{chat}')
    assert history['origin'] == 'reminder' and history['schedule'] == timer['id']
    reminders = [row['id'] for row in owner.request(OLD + '/chats')['chats'] if row['origin'] == 'reminder']
    assert reminders == [chat]
    print('Old binary: owner approval wrote control file, guest refused; MQTT button ran one real reminder.', flush=True)
    return waiting, approve, topic, timer['id'], guestchat


def digest_archive():
    directory = VOLUME / '.titanos/agent'
    paths = [Path('/config/system/agent.json')]
    for name in (OWNER, GUEST):
        paths.extend((directory / name / 'chats').glob('*.json'))
        schedule = directory / name / 'schedules.json'
        if schedule.is_file():
            paths.append(schedule)
    return {str(path): hashlib.sha256(path.read_bytes()).hexdigest() for path in paths}


def after(clients, evidence, logfile):
    waiting, approve, topic, schedule, guestchat = evidence
    for client in clients.values():
        client.expect(404, waiting, approve)
        client.expect(404, f'{OLD}/schedules/{schedule}/run', {})
        client.expect(404, f'{OLD}/chats/{guestchat}')
        state = client.request(NEW + '/state')
        assert state['chats'] == state['jobs'] == []
    owner, guest = clients[OWNER], clients[GUEST]
    guest.expect(403, NEW + '/limits/' + OWNER)
    owner.request(NEW + '/limits/' + GUEST)
    until('candidate subscribed via original HA connection', lambda:
          owner.request('/api/v1/homeassistant/settings')['status']['mqtt'])
    assert discover_button() is None, 'candidate advertised the old button'
    publish(topic)
    until('candidate rejected the earlier MQTT command on its live connection', lambda:
          any('command from Home Assistant ignored' in line and topic in line for line in logfile.read_text().splitlines()))
    assert (VOLUME / 'offen.txt').read_text() == 'unverändert'

    jobs = {}
    for name, client in clients.items():
        request = {'tool': 'agent.example', 'requestId': 'gleiche-id-pro-konto', 'prompt': 'Auftrag ' + name}
        job = client.request(NEW + '/jobs', request)
        route = f"{NEW}/jobs/{job['id']}"
        def finished():
            value = client.request(route)
            assert value['status'] in ('planned', 'running', 'done'), value
            return value if value['status'] == 'done' else None
        done = until('isolated personal example', finished)
        assert done['result']['text'] == '2 + 3 = 5' and len(done['receipts']) == 1
        assert client.request(NEW + '/jobs', request)['id'] == job['id']
        jobs[name] = job['id']
    assert len(set(jobs.values())) == 2
    for name, other in ((OWNER, GUEST), (GUEST, OWNER)):
        client = clients[name]
        client.expect(404, NEW + '/jobs/' + jobs[other])
        assert [j['id'] for j in client.request(NEW + '/state')['jobs']] == [jobs[name]]
        hits = json.dumps(client.request(NEW + '/search?q=Auftrag'))
        assert jobs[name] in hits and jobs[other] not in hits
    print('Candidate: old approval and MQTT press had zero effects; one isolated new job per account.', flush=True)


def run():
    assert Path('/mnt/testdaten').is_mount() and not (VOLUME / '.titanos').exists()
    passwords = {name: secrets.token_urlsafe(24) for name in (OWNER, GUEST)}
    with start_fixtures() as port:
        with daemon('stable'):
            clients = {name: Client() for name in passwords}
            clients[OWNER].request('/api/v1/setup', {'name': OWNER, 'password': passwords[OWNER]})
            clients[OWNER].request('/api/v1/users', {'name': GUEST, 'password': passwords[GUEST], 'role': 'user'})
            clients[GUEST].request('/api/v1/login', {'name': GUEST, 'password': passwords[GUEST]})
            evidence = before(clients, port)
            app_evidence = pim_probe.before(clients)
        archive = digest_archive()
        assert len(archive) >= 6
        with daemon('candidate') as logfile:
            clients = {name: Client() for name in passwords}
            for name, client in clients.items():
                client.request('/api/v1/login', {'name': name, 'password': passwords[name]})
            after(clients, evidence, logfile)
            pim_probe.after(clients, app_evidence)
            assert digest_archive() == archive, 'legacy private files changed'
        print(f'Both native accounts authenticated after replacement; {len(archive)} archived files byte-identical.')


if __name__ == '__main__':
    run()
