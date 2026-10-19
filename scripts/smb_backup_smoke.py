"""Real encrypted SMB3 copy/restore checks inside the disposable NAS guest.

A separate network namespace hosts Samba; only test paths on the throwaway
volume are used. Neither the developer's NAS nor host disks are involved.
"""
import json
import shlex
import time


def check_smb_backup(request, remote, password):
    # Not under .titanos: that folder is root's alone (0700) once titand
    # has put anything there, and the Samba account must reach the share.
    base = '/mnt/mirror/smb-copy-test'
    conf = '/run/titanos-smb-test/smb.conf'
    settings = f'''[global]
workgroup = TITANOS
server role = standalone server
server min protocol = SMB3
server smb encrypt = required
interfaces = 192.0.2.2
bind interfaces only = yes
smb ports = 445
pid directory = /run/titanos-smb-test
lock directory = /run/titanos-smb-test
state directory = /run/titanos-smb-test
cache directory = /run/titanos-smb-test
private dir = /run/titanos-smb-test
log file = /run/titanos-smb-test/log.%m
passdb backend = tdbsam:/run/titanos-smb-test/passdb.tdb
[Backup Space]
path = {base}
read only = no
valid users = storagetester
[ReadOnly]
path = {base}
read only = yes
valid users = storagetester
'''
    source = '/mnt/mirror/Fotos 2024/selected'
    target = {'id': 'smoke-nas', 'host': '192.0.2.2', 'username': 'storagetester', 'password': password,
              'share': 'Backup Space', 'folder': 'nas-copy'}

    def rejected(path, body, method='POST'):
        try:
            request(path, body, method)
        except RuntimeError:
            return
        raise AssertionError(f'{path} unexpectedly accepted')

    def run():
        request('/backups/smb-test/run', {})
        for _ in range(90):
            view = next(v for v in request('/backups')['jobs'] if v['job']['id'] == 'smb-test')
            if not view['state']['running']:
                assert not view['state']['lastError'], view
                assert view['state']['lastOK'], view
                return
            time.sleep(1)
        raise AssertionError('SMB backup did not finish')

    remote(f'mkdir -p {base} /run/titanos-smb-test; chown storagetester {base}; '
           f'printf %s {shlex.quote(settings)} > {conf}; '
           f'printf %s {shlex.quote(password + chr(10) + password + chr(10))} | smbpasswd -s -a -c {conf} storagetester; '
           'ip netns add titanos-smb-test; ip link add smbtest-host type veth peer name smbtest-peer; '
           'ip link set smbtest-peer netns titanos-smb-test; ip addr add 192.0.2.1/30 dev smbtest-host; ip link set smbtest-host up; '
           'ip netns exec titanos-smb-test ip addr add 192.0.2.2/30 dev smbtest-peer; '
           'ip netns exec titanos-smb-test ip link set smbtest-peer up; ip netns exec titanos-smb-test ip link set lo up; '
           f'ip netns exec titanos-smb-test smbd -D -s {conf}')
    try:
        shares = request('/backups/smb/discover', target)['shares']
        assert 'Backup Space' in shares and 'ReadOnly' in shares, shares
        rejected('/backups/smb/discover', dict(target, password='wrong-password'))
        rejected('/backups/smb', dict(target, share='ReadOnly'), 'PUT')
        overview = request('/backups/smb', target, 'PUT')
        assert password not in json.dumps(overview) and overview['smbTargets'][0]['share'] == 'Backup Space', overview
        # Config secrets must also be unavailable through the file manager.
        rejected('/files/text?root=stick&path=backup%2Fsmb-targets.json', None, 'GET')
        remote(f'mkdir -p {shlex.quote(source + "/nested")}; printf first > {shlex.quote(source + "/nested/file.txt")}; '
               f'printf retained > {shlex.quote(source + "/keep.txt")}; printf outside > {shlex.quote(source + "/../not-selected.txt")}')
        request('/backups', {'id': 'smb-test', 'sources': ['Fotos 2024'], 'folders': {'Fotos 2024': ['selected']},
                            'destination': {'type': 'smb-copy', 'targetId': 'smoke-nas', 'folder': 'job'},
                            'enabled': False, 'every': 'daily', 'time': '02:00', 'keep': {}})
        run()
        copied = base + '/nas-copy/job/Fotos 2024/selected'
        assert remote('cat ' + shlex.quote(copied + '/nested/file.txt')) == 'first'
        inode = remote('stat -c %i ' + shlex.quote(copied + '/nested/file.txt'))
        run()
        assert remote('stat -c %i ' + shlex.quote(copied + '/nested/file.txt')) == inode, 'unchanged file rewritten'
        remote(f'printf other > {shlex.quote(source + "/nested/file.txt")}; rm {shlex.quote(source + "/keep.txt")}')
        run()
        assert remote('cat ' + shlex.quote(copied + '/nested/file.txt')) == 'other'
        assert remote('cat ' + shlex.quote(copied + '/keep.txt')) == 'retained'
        remote('test ! -e ' + shlex.quote(base + '/nas-copy/job/Fotos 2024/not-selected.txt'))
        # Directly readable restoration, independent of restic or TitanOS.
        remote('cp -a ' + shlex.quote(copied) + ' /mnt/mirror/smb-restored; cmp ' + shlex.quote(copied + '/nested/file.txt') + ' /mnt/mirror/smb-restored/nested/file.txt')
        rejected('/backups/smb/smoke-nas', None, 'DELETE')
        request('/backups/smb-test', None, 'DELETE')
        request('/backups/smb/smoke-nas', None, 'DELETE')
        remote('test -z "$(find /run/titanos-backup -name credentials -print)"; ! mount | grep -q /run/titanos-backup/smb-')
        print('SMB backup: encrypted discovery, auth/readonly rejection, selected tree, unchanged/changed files, retained deletions, restore and cleanup passed', flush=True)
    finally:
        remote('ip netns pids titanos-smb-test | xargs -r kill; ip netns del titanos-smb-test; ip link del smbtest-host 2>/dev/null || true')
