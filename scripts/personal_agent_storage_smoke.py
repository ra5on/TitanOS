"""Personal Agent storage on the disposable UEFI guest used by smoke-storage."""
import re
import shlex
import time
import urllib.error

BASE = '/personal-agent'
# The source and destination, source absent for the change and another boot, source
# returns, then the adopted data survive with only the destination attached.
MOVE_DISKS = ((1, 2), (2,), (2,), (1, 2), (2,))


def storage_error(error):
    return (isinstance(error.__cause__, urllib.error.HTTPError)
            and error.__cause__.code == 503 and '"agent_storage_unavailable"' in str(error))


def unavailable(request):
    # Both reading and a real attempt to queue a job must fail. Check for a
    # stray accepted write when the data become available again.
    for path, payload in (('/state', None), ('/jobs', {'tool': 'agent.example', 'requestId': 'qemu-must-not-exist'})):
        try:
            request(BASE + path, payload)
        except RuntimeError as error:
            if not storage_error(error):
                raise
        else:
            raise AssertionError(f'{path} was available without the required system data')


def ready(request, timeout=90):
    deadline = time.monotonic() + timeout
    while True:
        try:
            return request(BASE + '/state')
        except RuntimeError as error:
            if not storage_error(error) or time.monotonic() >= deadline:
                raise
            time.sleep(1)


def finished(request, job):
    deadline = time.monotonic() + 90
    while True:
        job = request(BASE + '/jobs/' + job['id'])
        if job['status'] == 'done':
            assert job['result']['text'] == '2 + 3 = 5', job
            assert job['completedSteps'] == job['totalSteps'] == 1, job
            assert [(r['step'], r['tool']) for r in job['receipts']] == [(1, 'agent.example')], job
            return job
        assert job['status'] in ('planned', 'running'), job
        if time.monotonic() >= deadline:
            raise TimeoutError(f"Agent job {job['id']} stayed {job['status']}")
        time.sleep(1)


def assert_state(state, jobs):
    assert len(state['jobs']) == len(jobs), state['jobs']
    assert len(state['chats']) == 1 and state['chats'][0]['id'] == jobs[0]['request']['chatId'], state['chats']
    observed = {j['id']: j for j in state['jobs']}
    for expected in jobs:
        actual = observed[expected['id']]
        assert actual['request'] == expected['request'], actual
        assert actual['request']['chatId'] == state['chats'][0]['id'], actual
        assert actual['status'] == 'done' and actual['result'] == expected['result'], actual
        assert actual['receipts'] == expected['receipts'], actual


def account_file(remote, volume, jobs):
    # Inspect the actual production path on the mounted test disk. Keep the
    # contents inside the guest; only the generated filename leaves it.
    directory = shlex.quote('/mnt/' + volume + '/.titanos/agent-v2')
    remote('mountpoint -q ' + shlex.quote('/mnt/' + volume))
    files = remote(f'cd {directory}; ls -1 *.json').splitlines()
    assert len(files) == 1 and re.fullmatch(r'[a-f0-9]{64}\.json', files[0]), files
    for job in jobs:
        assert re.fullmatch(r'[a-f0-9]{32}', job['id'])
        remote(f'cd {directory}; grep -qF {shlex.quote(job["id"])} {shlex.quote(files[0])}')


def worker_restart(request, remote, volume):
    remote("""systemctl is-active --quiet titanos-agent.service
pid=$(systemctl show -p MainPID --value titanos-agent.service)
uid=$(id -u _titanos-agent)
test "$uid" -gt 0
test "$(awk '/^Uid:/ {print $2, $3, $4, $5}' /proc/$pid/status)" = "$uid $uid $uid $uid"
for cap in CapInh CapPrm CapEff CapBnd CapAmb; do
    grep -Eq "^$cap:[[:space:]]+0+$" /proc/$pid/status
done
systemctl stop titanos-agent.service""")
    req = {'tool': 'agent.example', 'requestId': 'qemu-agent-restart', 'prompt': 'Vor dem Volumewechsel'}
    try:
        state = ready(request)
        assert state['jobs'] == state['chats'] == [], state
        job = request(BASE + '/jobs', req)
        assert job['status'] == 'planned' and not job.get('receipts'), job
        account_file(remote, volume, [job])
    finally:
        remote('systemctl start titanos-agent.service')
    job = finished(request, job)
    assert request(BASE + '/jobs', req)['id'] == job['id']
    assert_state(request(BASE + '/state'), [job])
    print(f'Agent: verified service UID, zero capabilities, one stored job/receipt on {volume}', flush=True)
    return job


def move_boot(boot, request, remote, jobs):
    # The health endpoint may be ready before storage finishes mounting.
    expect = {1: set(), 2: {'agentnew'}, 3: {'agentnew'},
              4: {'agentold', 'agentnew'}, 5: {'agentnew'}}[boot]
    deadline = time.monotonic() + 90
    while True:
        storage = request('/storage')
        volumes = {v['name'] for v in storage['volumes'] if v['mounted']}
        if volumes == expect:
            break
        if time.monotonic() >= deadline:
            raise AssertionError(f'boot {boot}: mounted {volumes}, expected {expect}')
        time.sleep(1)
    if boot == 1:
        unavailable(request)
        disks = {d['serial']: d for d in storage['disks']}
        for serial, name in [('BTRFS1', 'agentold'), ('BTRFS2', 'agentnew')]:
            disk = disks[serial]
            assert re.fullmatch(r'vd[a-z]+', disk['name']) and disk['identity'], disk
            request('/storage/volumes', {'name': name, 'level': 'raid0', 'filesystem': 'ext4',
                    'disks': [disk['name']], 'erase': [disk['name']], 'identities': {disk['name']: disk['identity']}})
            if name == 'agentold':
                request('/system/data', {'volume': name}, 'PUT')
        jobs.append(worker_restart(request, remote, 'agentold'))
    elif boot in (2, 3):
        if boot == 2:
            unavailable(request)
            settings = request('/system/data', {'volume': 'agentnew'}, 'PUT')
        else:
            settings = request('/system/data')
        assert settings['volume'] == 'agentnew', settings
        unavailable(request)
        remote("test -z \"$(find /mnt/agentnew/.titanos/agent-v2 -name '*.json' -print -quit 2>/dev/null)\"")
    elif boot == 4:
        assert_state(ready(request), jobs)
        account_file(remote, 'agentnew', jobs)
        second = request(BASE + '/jobs', {'tool': 'agent.example', 'requestId': 'qemu-after-return',
                         'prompt': 'Nach der Übernahme', 'chatId': jobs[0]['request']['chatId']})
        jobs.append(finished(request, second))
        assert_state(request(BASE + '/state'), jobs)
    else:
        assert_state(ready(request), jobs)
        for job in jobs:
            assert request(BASE + '/jobs', job['request'])['id'] == job['id']
        assert_state(request(BASE + '/state'), jobs)
        account_file(remote, 'agentnew', jobs)
    print(f'Agent volume move boot {boot} passed', flush=True)
