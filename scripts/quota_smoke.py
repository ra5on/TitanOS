"""Share quotas on real XFS and ext4, in the disposable NAS only.

Boot 1 makes one single-disk volume of each file system on its own virtual
disk, switches share quotas on and gives a share a 64 MiB limit. They take
effect at the next mount, so boot 2 checks that an ordinary account cannot
write past the limit, that the usage is reported and that the share shows
the limit as its size (what SMB clients see).
"""
import time

LIMIT = 64 << 20
VOLUMES = (('quotaxfs', 'xfs', 'QUOTA1'), ('quotaext', 'ext4', 'QUOTA2'))


def check_share_quotas(boot, request, remote, disks):
    if boot == 1:
        for name, filesystem, serial in VOLUMES:
            disk = disks[serial]
            assert disk.startswith('vd'), disk
            request('/storage/volumes', {'name': name, 'level': 'raid0', 'filesystem': filesystem, 'disks': [disk], 'erase': [disk]})
            request(f'/storage/volumes/{name}/quotas', {'enabled': True}, 'PUT')
            request('/shares', {'name': name + '-limited', 'volume': name, 'access': {'storagetester': 'rw'}, 'quota': LIMIT})
            volume = next(v for v in request('/storage')['volumes'] if v['name'] == name)
            assert volume['shareQuotas'] == 'pending', volume
        print('Share quotas switched on, pending until the next start', flush=True)
        return
    if boot != 2:
        return
    for name, filesystem, _ in VOLUMES:
        volume = next(v for v in request('/storage')['volumes'] if v['name'] == name)
        assert volume['mounted'] and volume['shareQuotas'] == 'active', volume
        share = name + '-limited'
        deadline = time.monotonic() + 60
        while True:
            status = next(s for s in request('/shares') if s['name'] == share).get('quotaStatus') or {}
            if status.get('state') == 'active':
                break
            if time.monotonic() > deadline:
                raise TimeoutError(f'{share}: {status}')
            time.sleep(2)
        assert status['limit'] == LIMIT, status
        path = f'/mnt/{name}/{share}'
        # An ordinary account must be stopped by the kernel at the limit.
        result = remote(f'if runuser -u storagetester -- dd if=/dev/zero of={path}/fill bs=1M count=100 conv=fsync status=none 2>/tmp/dd.err; then echo written; else cat /tmp/dd.err; fi')
        assert 'written' not in result and 'quota' in result.lower(), result
        size = int(remote(f'stat -c %s {path}/fill'))
        assert size <= LIMIT, size
        # Statfs inside the share reports the limit, as SMB clients see it.
        total = int(remote(f'stat -f -c "%b %S" {path} | awk "{{print \\$1*\\$2}}"'))
        assert LIMIT * 0.9 <= total <= LIMIT * 1.1, total
        deadline = time.monotonic() + 45
        while True:
            used = next(s for s in request('/shares') if s['name'] == share)['quotaStatus']['used']
            if used >= LIMIT * 0.9:
                break
            if time.monotonic() > deadline:
                raise AssertionError(f'{share}: used {used}')
            time.sleep(5)
        remote(f'rm {path}/fill')
        print(f'Share quota enforced on {filesystem}: {size} bytes written, share size {total}', flush=True)
