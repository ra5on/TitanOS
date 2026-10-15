"""Native CDI through the shipped Compose/Engine, in the disposable NAS only.

A synthetic CDI environment edit exercises the real request path without
claiming to test a physical GPU, its driver or CUDA.
"""
import json
import time


def check_cdi(request, remote):
    request('/docker/settings', {'enabled': True, 'volume': 'mirror'}, 'PUT')
    deadline = time.monotonic() + 90
    while not request('/docker')['running']:
        if time.monotonic() > deadline:
            raise TimeoutError('Docker startup')
        time.sleep(2)
    remote('''mkdir -p /etc/cdi /mnt/mirror/cdi-smoke/root/bin
cp /bin/busybox /mnt/mirror/cdi-smoke/root/bin/
ldd /bin/busybox | awk '{for(i=1;i<=NF;i++) if ($i ~ /^\\//) print $i}' | while read -r lib; do cp --parents "$lib" /mnt/mirror/cdi-smoke/root/; done
tar -C /mnt/mirror/cdi-smoke/root -c . | docker import - titanos-cdi-smoke:local
cat > /etc/cdi/titanos-smoke.json <<'CDI'
{"cdiVersion":"0.5.0","kind":"nvidia.com/gpu","devices":[{"name":"GPU-smoke","containerEdits":{"env":["TITAN_CDI_SMOKE=yes"]}}]}
CDI
''')
    compose = '''services:
  probe:
    image: titanos-cdi-smoke:local
    container_name: titanos-cdi-smoke
    command: ["/bin/busybox", "sh", "-c", "test $$TITAN_CDI_SMOKE = yes && /bin/busybox sleep 180"]
    devices: [nvidia.com/gpu=GPU-smoke]
    volumes: ["./data:/data"]
'''
    request('/docker/apps', {'name': 'cdi-smoke', 'compose': compose})
    deadline = time.monotonic() + 90
    while True:
        job = request('/docker')['jobs'].get('cdi-smoke', {})
        if job.get('state') == 'done':
            break
        assert job.get('state') != 'failed', job
        if time.monotonic() > deadline:
            raise TimeoutError('CDI Compose app')
        time.sleep(1)
    info = json.loads(remote('docker inspect titanos-cdi-smoke'))[0]
    assert info['State']['Running'], info['State']
    req = info['HostConfig']['DeviceRequests'][0]
    assert req['Driver'] == 'cdi' and req['DeviceIDs'] == ['nvidia.com/gpu=GPU-smoke'] and req['Count'] == 0, req
    assert not info['HostConfig']['Devices'], info['HostConfig']['Devices']
    remote('docker exec titanos-cdi-smoke /bin/busybox sh -c "echo retained > /data/marker"')
    # Reproduce the exact old invalid HostConfig with the original stored file.
    remote('docker rm -f titanos-cdi-smoke; if docker compose -p cdi-smoke -f /config/apps/cdi-smoke/compose.yaml --project-directory /mnt/mirror/appdata/cdi-smoke up -d --no-deps --pull never probe > /tmp/cdi-old-error 2>&1; then exit 1; fi; grep -F "no such file or directory" /tmp/cdi-old-error')
    info = json.loads(remote('docker inspect titanos-cdi-smoke'))[0]
    assert not info['State']['Running']
    assert info['HostConfig']['Devices'][0]['PathOnHost'] == 'nvidia.com/gpu=GPU-smoke'
    request('/docker/containers/' + info['Id'] + '/start', {})
    repaired = json.loads(remote('docker inspect titanos-cdi-smoke'))[0]
    assert repaired['State']['Running'] and repaired['Id'] != info['Id'], repaired['State']
    assert remote('docker exec titanos-cdi-smoke /bin/busybox cat /data/marker') == 'retained'
    request('/docker/apps/cdi-smoke', method='DELETE')
    request('/docker/settings', {'enabled': False, 'volume': 'mirror'}, 'PUT')
    remote('rm /etc/cdi/titanos-smoke.json; rm -r /mnt/mirror/cdi-smoke')
    print('Shipped Docker/Compose: CDI device request, failed legacy-container repair and retained app data passed', flush=True)
