"""Extend the existing disposable-QEMU storage test with two real catalog apps.

Only its new qcow2 data disks are enlarged. No host Docker socket is used.
"""
import argparse
import base64
import importlib.util
import json
import secrets
import shlex
import subprocess
import sys
import time
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('--zip', type=Path, required=True, help='locally built installation ZIP')
parser.add_argument('--apps-dir', type=Path, default=ROOT.parent / 'TitanOS-Apps')
parser.add_argument('--output', type=Path, default=ROOT / 'out/validation/storage-apps-qemu')
parser.add_argument('--timeout', type=int, default=420, help='boot readiness timeout in seconds')
args = parser.parse_args()
OUTPUT = args.output.resolve()
OUTPUT.mkdir(parents=True, exist_ok=True)
# Preserve exactly which catalog templates were tested, before test credentials.
for app in ('nextcloud', 'mosquitto'):
    source = args.apps_dir.resolve() / app / 'compose.yaml'
    module = ROOT / 'web/node_modules/yaml/dist/index.js'
    code = "import fs from 'node:fs'; import YAML from " + json.dumps(module.as_uri()) + "; console.log(JSON.stringify(YAML.parse(fs.readFileSync(process.argv[1], 'utf8'))));"
    data = subprocess.check_output(['node', '--input-type=module', '-e', code, str(source)], text=True)
    (OUTPUT / (app + '-template.json')).write_text(data)
sys.path.insert(0, str(ROOT / 'scripts'))
spec = importlib.util.spec_from_file_location('storage_test', ROOT / 'scripts/smoke-storage.py')
storage = importlib.util.module_from_spec(spec)
spec.loader.exec_module(storage)
original_command = storage.helper.command


def command(*args, **kwargs):
    if args[:2] == ('qemu-img', 'create') and args[-1] == '2G':
        args = (*args[:-1], '16G')
    return original_command(*args, **kwargs)


storage.helper.command = command
original_cdi = storage.check_cdi
results = []


def apps(request, remote):
    original_cdi(request, remote)
    request('/docker/settings', {'enabled': True, 'volume': 'mirror'}, 'PUT')

    def wait(check, label, seconds=600):
        deadline = time.monotonic() + seconds
        while time.monotonic() < deadline:
            value = check()
            if value:
                return value
            time.sleep(3)
        raise TimeoutError(label)

    wait(lambda: request('/docker')['running'], 'Docker startup', 90)

    def install(name, config):
        request('/docker/apps', {'name': name, 'compose': json.dumps(config)})
        def completed():
            job = request('/docker')['jobs'].get(name, {})
            if job.get('state') == 'failed':
                raise AssertionError(f'{name}: installation failed: {job}')
            return job.get('state') == 'done'
        wait(completed, name + ' installation', 900)

    def container(name, service):
        return remote('docker ps -aq --filter ' + shlex.quote('label=com.docker.compose.project=' + name)
                      + ' --filter ' + shlex.quote('label=com.docker.compose.service=' + service))

    def inventory(name):
        ids = remote('docker ps -aq --filter ' + shlex.quote('label=com.docker.compose.project=' + name)).split()
        data = json.loads(remote('docker inspect ' + ' '.join(ids)))
        return [{'name': d['Name'], 'image': d['Config']['Image'], 'image_id': d['Image'],
                 'running': d['State']['Running']} for d in data]

    def remove(name):
        request('/docker/apps/' + name, method='DELETE')
        wait(lambda: not remote('docker ps -aq --filter ' + shlex.quote('label=com.docker.compose.project=' + name)), name + ' removal', 90)

    nextcloud = json.loads((OUTPUT / 'nextcloud-template.json').read_text())
    password, db_password, root_password = (secrets.token_urlsafe(24) for _ in range(3))
    for service in nextcloud['services'].values():
        env = service.get('environment', {})
        if 'MYSQL_PASSWORD' in env:
            env['MYSQL_PASSWORD'] = db_password
        if 'MARIADB_ROOT_PASSWORD' in env:
            env['MARIADB_ROOT_PASSWORD'] = root_password
    web = nextcloud['services']['nextcloud']
    web['environment'].update(NEXTCLOUD_ADMIN_USER='catalogtest', NEXTCLOUD_ADMIN_PASSWORD=password,
                              NEXTCLOUD_TRUSTED_DOMAINS='127.0.0.1 localhost')
    web['ports'] = ['18080:80']
    name = 'qemu-nextcloud'
    print('Catalog Nextcloud/MariaDB/cron: installing real upstream images inside QEMU', flush=True)
    install(name, nextcloud)
    cid = container(name, 'nextcloud')

    def http(path, method='GET', content=None, auth=False):
        headers = 'Content-Type: text/plain\r\n'
        if auth:
            headers += 'Authorization: Basic ' + base64.b64encode(('catalogtest:' + password).encode()).decode() + '\r\n'
        options = {'http': {'method': method, 'header': headers, 'ignore_errors': True, 'timeout': 15}}
        if content is not None:
            options['http']['content'] = content
        # PHP runs in the real web container and performs an actual HTTP request.
        code = '$o=json_decode(' + json.dumps(json.dumps(options)) + ',true);'
        code += '$b=@file_get_contents(' + json.dumps('http://127.0.0.1' + path) + ',false,stream_context_create($o));'
        code += 'echo json_encode(["headers"=>$http_response_header??[],"body"=>$b]);'
        response = json.loads(remote('docker exec ' + cid + ' php -r ' + shlex.quote(code), timeout=60))
        status = response['headers'][0] if response['headers'] else ''
        return status, response['body']

    def ready():
        status, body = http('/status.php')
        if '200' not in status or not body:
            return False
        data = json.loads(body)
        return data if data.get('installed') and not data.get('maintenance') and not data.get('needsDbUpgrade') else False

    version = wait(ready, 'Nextcloud setup', 900)
    assert json.loads(remote('/bin/busybox wget -q -O- http://127.0.0.1:18080/status.php'))['installed']
    path = '/remote.php/dav/files/catalogtest/qemu-validation.txt'
    assert '401' in http(path)[0]
    marker = 'TitanOS QEMU synthetic WebDAV persistence check\n'
    assert '201' in http(path, 'PUT', marker, True)[0]
    assert http(path, auth=True)[1] == marker
    images = inventory(name)
    assert len(images) == 3 and all(i['running'] for i in images), images
    remove(name)
    install(name, nextcloud)
    cid = container(name, 'nextcloud')
    wait(ready, 'Nextcloud after recreation')
    assert http(path, auth=True)[1] == marker
    assert all(i['running'] for i in inventory(name))
    results.append({'app': 'Nextcloud + MariaDB + cron', 'version': version, 'images': images,
                    'checks': ['TitanOS Compose API installation', 'published HTTP port', 'anonymous WebDAV rejected',
                               'authenticated WebDAV upload/download', 'data/account persistence after app removal and recreation']})
    print('PASS catalog Nextcloud/MariaDB/cron: HTTP, authentication, WebDAV and persistent data after app recreation', flush=True)
    remove(name)

    mqtt = json.loads((OUTPUT / 'mosquitto-template.json').read_text())
    mqtt_password = secrets.token_hex(24)
    mqtt['services']['mosquitto']['environment'].update(MQTT_USERNAME='qemutest', MQTT_PASSWORD=mqtt_password)
    mqtt['services']['mosquitto']['ports'] = ['18883:1883']
    name = 'qemu-mosquitto'
    print('Catalog Mosquitto: installing real upstream image inside QEMU', flush=True)
    install(name, mqtt)
    cid = container(name, 'mosquitto')
    prefix = 'docker exec ' + cid + ' '
    auth = ' -h 127.0.0.1 -u qemutest -P ' + shlex.quote(mqtt_password)
    marker = 'TitanOS-QEMU-retained-message'
    remote(prefix + 'mosquitto_pub' + auth + ' -t titanos/validation -m ' + shlex.quote(marker) + ' -r')
    assert remote(prefix + 'mosquitto_sub' + auth + ' -t titanos/validation -C 1 -W 10') == marker
    remote('if ' + prefix + 'mosquitto_pub -h 127.0.0.1 -t titanos/validation -m anonymous >/dev/null 2>&1; then exit 1; fi')
    remote('if ' + prefix + 'mosquitto_pub -h 127.0.0.1 -u qemutest -P wrong -t titanos/validation -m denied >/dev/null 2>&1; then exit 1; fi')
    images = inventory(name)
    remove(name)
    install(name, mqtt)
    cid = container(name, 'mosquitto')
    prefix = 'docker exec ' + cid + ' '
    assert remote(prefix + 'mosquitto_sub' + auth + ' -t titanos/validation -C 1 -W 10') == marker
    results.append({'app': 'Eclipse Mosquitto', 'images': images,
                    'checks': ['TitanOS Compose API installation', 'authenticated MQTT publish/subscribe',
                               'anonymous and wrong-password rejection', 'retained message after app recreation']})
    print('PASS catalog Mosquitto: authenticated MQTT, denied anonymous/invalid credentials and retained data after recreation', flush=True)
    remove(name)
    (OUTPUT / 'catalog-apps-result.json').write_text(json.dumps(results, indent=2) + '\n')
    request('/docker/settings', {'enabled': False, 'volume': 'mirror'}, 'PUT')


storage.check_cdi = apps
if __name__ == '__main__':
    storage.smoke(args.zip.resolve(), None, OUTPUT / 'storage-apps-serial.log', args.timeout)
