#!/usr/bin/env python3
"""LUKS2 volume encryption with real cryptsetup, in disposable UEFI QEMU disks only.

Boots the stick ZIP four times with fresh qcow2 data disks (never host block
devices) and checks docs/ENCRYPTION.md against the real tools in the guest:

  a) encrypted mdadm RAID1 (ext4), encrypted Btrfs RAID1 and an encrypted
     RAID5 (for growing) via the API, data written with checksums;
  b) after a reboot every volume is locked, one volume_locked notice each,
     shares not offered, Docker not started, health ready (boot check);
  c) wrong passwords rejected with back-off, the right one unlocks, data,
     shares and the Docker data folder come back;
  d) lock while an SMB client is connected, unlock restores;
  e) change password, add a second one and unlock with it, header backup
     is a tar of LUKS headers;
  f) mdadm spare, RAID5 growth with cryptsetup resize and resize2fs,
     Btrfs disk added with the password;
  g) SSD read cache below dm-crypt (dmsetup ls --tree), reboot, unlock;
  h) delete: LUKS key slots erased and signatures gone on former members;
  i) unlock peak memory on a guest with 2 GiB RAM (argon2id 256 MiB).

Results go to <output>/encryption-result.json.
"""
import argparse
import hashlib
import http.cookiejar
import importlib.util
import io
import json
import os
from pathlib import Path
import secrets
import shlex
import shutil
import ssl
import subprocess
import tarfile
import tempfile
import time
import urllib.error
import urllib.request
import zipfile

spec = importlib.util.spec_from_file_location('release_smoke', Path(__file__).with_name('smoke-release.py'))
helper = importlib.util.module_from_spec(spec)
spec.loader.exec_module(helper)

LUKS_MAGIC = (b'LUKS\xba\xbe', b'SKUL\xba\xbe')
# Serial → size of the throwaway disks. MD: mdadm RAID1 (+ spare), BT: Btrfs
# RAID1 (+ added disk), R: RAID5 grown by R4, SSD1: read cache.
DISKS = {'MD1': '2G', 'MD2': '2G', 'MD3': '2G', 'BT1': '2G', 'BT2': '2G', 'BT3': '2G',
         'R1': '1G', 'R2': '1G', 'R3': '1G', 'R4': '1G', 'SSD1': '1G'}


def smoke(archive, output, timeout, force_tcg=False):
    os.environ['PATH'] += ':/usr/sbin:/sbin'
    output.mkdir(parents=True, exist_ok=True)
    results = {'started': time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime()), 'zip': archive.name,
               'zipSha256': hashlib.sha256(archive.read_bytes()).hexdigest(), 'phases': {}}

    def record(phase, **evidence):
        results['phases'].setdefault(phase, {}).update(evidence)
        (output / 'encryption-result.json').write_text(json.dumps(results, indent=2) + '\n')
        print(f'[{phase}]', json.dumps(evidence)[:600], flush=True)

    with tempfile.TemporaryDirectory(prefix='titanos-encryption-', dir=output) as tmp:
        directory = Path(tmp)
        stick = directory / 'stick'
        with zipfile.ZipFile(archive) as packed:
            for item in packed.infolist():
                if not (stick / item.filename).resolve().is_relative_to(stick):
                    raise ValueError('unsafe ZIP path')
            packed.extractall(stick)
        disk = directory / 'stick.img'
        with disk.open('wb') as f:
            f.truncate(4 * 1024**3)
        helper.command('sfdisk', '--quiet', '--label', 'dos', str(disk), input=b'start=1MiB, type=c, bootable\n')
        partition = str(disk) + '@@1M'
        helper.command('mformat', '-i', partition, '-F', '-v', 'TITANOS', '::')
        helper.command('mcopy', '-i', partition, '-s', *map(str, stick.iterdir()), '::')
        shutil.copyfile('/usr/share/OVMF/OVMF_VARS_4M.fd', directory / 'OVMF_VARS.fd')
        for serial, size in DISKS.items():
            helper.command('qemu-img', 'create', '-q', '-f', 'qcow2', str(directory / f'{serial}.qcow2'), size)
        helper.command('ssh-keygen', '-q', '-t', 'ed25519', '-N', '', '-f', str(directory / 'key'))
        port, ssh_port, smb_port = helper.free_port(), helper.free_port(), helper.free_port()
        url = f'https://127.0.0.1:{port}'
        opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()),
                                             urllib.request.HTTPSHandler(context=ssl._create_unverified_context()))

        def call(path, data=None, method=None, timeout=600):
            """Returns status, headers and body; never raises on HTTP errors."""
            req = urllib.request.Request(url + '/api/v1' + path,
                                         data=json.dumps(data).encode() if data is not None else None,
                                         headers={'Content-Type': 'application/json', 'Origin': url}, method=method)
            try:
                with opener.open(req, timeout=timeout) as response:
                    return response.status, response.headers, response.read()
            except urllib.error.HTTPError as error:
                return error.code, error.headers, error.read()

        def request(path, data=None, method=None, timeout=600):
            status, _, body = call(path, data, method, timeout)
            if status >= 400:
                raise RuntimeError(f'{method or ("POST" if data is not None else "GET")} {path}: {status} {body.decode(errors="replace")}')
            return json.loads(body) if body else None

        ssh_options = ['-F', '/dev/null', '-i', str(directory / 'key'), '-o', 'IdentitiesOnly=yes', '-o', 'BatchMode=yes',
                       '-o', 'StrictHostKeyChecking=accept-new', '-o', f'UserKnownHostsFile={directory}/known_hosts', '-o', 'ConnectTimeout=3']
        ssh = ['ssh', *ssh_options, '-p', str(ssh_port), 'enctester@127.0.0.1']

        def remote(command, timeout=300, check=True):
            result = subprocess.run([*ssh, 'sudo sh -ec ' + shlex.quote(command)], text=True, capture_output=True, timeout=timeout)
            if check and result.returncode:
                raise RuntimeError(f'guest command failed ({result.returncode}): {command}\n{result.stdout}{result.stderr}')
            return result.stdout.strip() if check else result

        def put(data, destination):
            subprocess.run([*ssh, 'sudo sh -c ' + shlex.quote('cat > ' + shlex.quote(destination))], input=data, check=True)

        def wait(check, what, limit=300, step=2):
            deadline = time.monotonic() + limit
            while True:
                try:
                    value = check()
                    if value:
                        return value
                except (OSError, RuntimeError, ValueError):
                    pass
                if time.monotonic() > deadline:
                    raise TimeoutError(what)
                time.sleep(step)

        def volume(name):
            return next(v for v in request('/storage')['volumes'] if v['name'] == name)

        def disks():
            return {d['serial']: d for d in request('/storage')['disks'] if d.get('serial')}

        def smb(share, commands, password):
            env = dict(os.environ, PASSWD=password)
            return subprocess.run(['smbclient', f'//127.0.0.1/{share}', '-p', str(smb_port), '-U', 'enctester', '-m', 'SMB3', '-c', commands],
                                  env=env, capture_output=True, text=True, timeout=90)

        def checksum(path):
            return remote(f'sha256sum {shlex.quote(path)}').split()[0]

        def unlock(name, password):
            started = time.monotonic()
            status, headers, body = call(f'/storage/volumes/{name}/unlock', {'password': password})
            return status, body.decode(errors='replace'), round(time.monotonic() - started, 2), headers

        def locked_state(names):
            state = {n: volume(n) for n in names}
            return {n: {'mounted': v['mounted'], 'encryption': v.get('encryption')} for n, v in state.items()}

        accel = ['-accel', 'kvm', '-cpu', 'host'] if not force_tcg and os.access('/dev/kvm', os.W_OK) else ['-accel', 'tcg', '-cpu', 'max']
        results['accel'] = accel[1]

        def qemu(memory):
            args = ['qemu-system-x86_64', '-machine', 'q35', *accel, '-smp', '2', '-m', memory,
                    '-drive', 'if=pflash,format=raw,readonly=on,file=/usr/share/OVMF/OVMF_CODE_4M.fd',
                    '-drive', f'if=pflash,format=raw,file={directory}/OVMF_VARS.fd',
                    '-device', 'qemu-xhci', '-drive', f'if=none,id=stick,format=raw,file={disk}',
                    '-device', 'usb-storage,drive=stick,removable=on,bootindex=0',
                    '-netdev', f'user,id=net0,hostfwd=tcp:127.0.0.1:{port}-:443,hostfwd=tcp:127.0.0.1:{ssh_port}-:22,hostfwd=tcp:127.0.0.1:{smb_port}-:445',
                    '-device', 'virtio-net-pci,netdev=net0',
                    '-smbios', 'type=11,value=io.systemd.boot.kernel-cmdline-extra=console=tty0 console=ttyS0,,115200',
                    '-display', 'none', '-serial', 'stdio', '-monitor', 'none', '-no-reboot']
            for serial in DISKS:
                args += ['-drive', f'if=none,id={serial},format=qcow2,file={directory}/{serial}.qcow2',
                         '-device', f'virtio-blk-pci,drive={serial},serial={serial}']
            return args

        pw = {name: secrets.token_urlsafe(18) for name in ('secure', 'vault', 'grow5', 'secure2', 'secondary')}
        account = secrets.token_urlsafe(18)
        sums = {}
        header_tar = {}
        boots = (('1', '4G'), ('2', '4G'), ('3', '4G'), ('4', '2G'))
        with (output / 'encryption-serial.log').open('wb') as serial:
            for boot, memory in boots:
                print(f'boot {boot}: {memory} RAM ({accel[1]})', flush=True)
                process = subprocess.Popen(qemu(memory), stdin=subprocess.DEVNULL, stdout=serial, stderr=subprocess.STDOUT)
                try:
                    booted = time.monotonic()
                    def ready():
                        if process.poll() is not None:
                            raise SystemExit(f'QEMU stopped before health (exit {process.returncode})')
                        answer = request('/health')
                        return answer if answer.get('ready') else None
                    health = wait(ready, 'health', timeout, 3)
                    ready_seconds = round(time.monotonic() - booted, 1)
                    if boot == '1':
                        request('/setup', {'name': 'enctester', 'password': account})
                        request('/account/ssh-keys', {'key': (directory / 'key.pub').read_text().strip()})
                    else:
                        request('/login', {'name': 'enctester', 'password': account})
                    request('/system/ssh', {'enabled': True}, 'PUT')
                    wait(lambda: subprocess.run([*ssh, 'true'], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL).returncode == 0, 'SSH', 60, 1)
                    names = {s: d['name'] for s, d in disks().items()}
                    ident = {s: d['identity'] for s, d in disks().items()}

                    if boot == '1':
                        record('environment', guestRAM=memory, kernel=remote('uname -r'),
                               cryptsetup=remote('cryptsetup --version'), dmCrypt=remote('modprobe dm_crypt && test -d /sys/module/dm_crypt && echo loaded'))
                        # a) create encrypted volumes and write data.
                        for name, level, fs, members in (('secure', 'raid1', 'ext4', ['MD1', 'MD2']),
                                                         ('vault', 'raid1', 'btrfs', ['BT1', 'BT2']),
                                                         ('grow5', 'raid5', 'ext4', ['R1', 'R2', 'R3'])):
                            chosen = [names[m] for m in members]
                            started = time.monotonic()
                            request('/storage/volumes', {'name': name, 'level': level, 'filesystem': fs, 'disks': chosen, 'erase': chosen,
                                                         'identities': {names[m]: ident[m] for m in members}, 'encrypt': True, 'password': pw[name]})
                            v = volume(name)
                            assert v['mounted'] and v['encryption'] and not v['encryption']['locked'], v
                            record('a', **{f'{name}Created': {'level': level, 'filesystem': fs, 'seconds': round(time.monotonic() - started, 1),
                                                              'encryption': v['encryption']}})
                        # Volume key and PBKDF of the real containers.
                        dump = remote('cryptsetup luksDump "$(cryptsetup status titanos-crypt-secure | awk \'/device:/{print $2}\')"')
                        field = lambda key: next(line.split(':', 1)[1].strip() for line in dump.splitlines() if line.strip().startswith(key + ':'))
                        # cryptsetup caps the lanes at the number of online CPUs (2 here).
                        threads, cpus = int(field('Threads')), int(remote('nproc'))
                        assert field('PBKDF') == 'argon2id' and field('Memory') == '262144' and threads == min(4, cpus), dump
                        record('a', luksDumpSecure={'pbkdf': 'argon2id', 'memoryKiB': 262144, 'threads': threads, 'guestCPUs': cpus,
                                                    'timeCost': field('Time cost'), 'cipher': field('Cipher'), 'keyBits': field('Cipher key')},
                               mapper=remote('ls /dev/mapper | grep titanos-crypt | sort').split(),
                               mounts=remote('findmnt -rn -o SOURCE,TARGET,FSTYPE | grep /mnt/').splitlines(),
                               btrfsDevices=remote('btrfs filesystem show /mnt/vault | grep path').splitlines())
                        assert '/dev/mapper/titanos-crypt-secure /mnt/secure ext4' in remote('findmnt -rn -o SOURCE,TARGET,FSTYPE /mnt/secure')
                        for share, name in (('docs', 'secure'), ('archive', 'vault')):
                            request('/shares', {'name': share, 'volume': name, 'smb': True})
                        for name in ('secure', 'vault', 'grow5'):
                            folder = f'/mnt/{name}/docs' if name == 'secure' else f'/mnt/{name}/archive' if name == 'vault' else f'/mnt/{name}/data'
                            remote(f'mkdir -p {folder}; head -c 48M /dev/urandom > {folder}/blob.bin; sync')
                            sums[name] = (f'{folder}/blob.bin', checksum(f'{folder}/blob.bin'))
                        request('/docker/settings', {'enabled': True, 'volume': 'secure'}, 'PUT')
                        root = wait(lambda: remote("docker info -f '{{.DockerRootDir}}'", check=False).stdout.strip(), 'docker', 180, 3)
                        remote('docker volume create lukstest >/dev/null')
                        record('a', dataWritten={k: v[1] for k, v in sums.items()}, dockerRoot=root, ok=True)

                    if boot == '2':
                        # b) locked after the reboot.
                        state = locked_state(['secure', 'vault', 'grow5'])
                        assert all(not s['mounted'] and s['encryption']['locked'] for s in state.values()), state
                        notices = wait(lambda: (lambda m: m if {'secure', 'vault', 'grow5'} <= {x['subject'] for x in m} else None)(
                            [x for x in request('/notifications')['messages'] if x['kind'] == 'volume_locked']), 'volume_locked notices', 120)
                        time.sleep(35)  # the notice watcher must not repeat it
                        notices = [x for x in request('/notifications')['messages'] if x['kind'] == 'volume_locked']
                        counts = {n: sum(1 for x in notices if x['subject'] == n) for n in ('secure', 'vault', 'grow5')}
                        assert counts == {'secure': 1, 'vault': 1, 'grow5': 1}, counts
                        smbconf = remote('testparm -s 2>/dev/null || true')
                        assert '[docs]' not in smbconf and '[archive]' not in smbconf, smbconf
                        listing = subprocess.run(['smbclient', '-L', '//127.0.0.1', '-p', str(smb_port), '-U', 'enctester', '-m', 'SMB3'],
                                                 env=dict(os.environ, PASSWD=account), capture_output=True, text=True, timeout=60)
                        assert 'docs' not in listing.stdout and 'archive' not in listing.stdout, listing.stdout
                        docker_state = remote('systemctl is-active docker.service', check=False).stdout.strip()
                        assert docker_state != 'active', docker_state
                        not_mounted = remote('mountpoint -q /mnt/secure && echo mounted || echo not-mounted')
                        boot_check = remote('systemctl show -p ActiveState -p Result titanos-boot-check.service')
                        record('b', state=state, notices=counts, smbSharesOffered=[s for s in ('docs', 'archive') if s in listing.stdout],
                               docker=docker_state, mountSecure=not_mounted, health=health, healthReadySeconds=ready_seconds,
                               bootCheckService=boot_check.split('\n'), ok=True)

                        # c) wrong passwords and back-off.
                        wrong = []
                        for attempt in range(6):
                            status, body, seconds, headers = unlock('secure', 'wrong-' + secrets.token_urlsafe(8))
                            wrong.append({'status': status, 'seconds': seconds, 'retryAfter': headers.get('Retry-After'),
                                          'code': json.loads(body).get('error') if body.startswith('{') else body[:80]})
                        assert [w['status'] for w in wrong[:5]] == [400] * 5, wrong
                        assert wrong[5]['status'] == 429 and int(wrong[5]['retryAfter']) >= 25, wrong
                        status, body, _, headers = unlock('secure', pw['secure'])
                        assert status == 429, (status, body)
                        retry = int(headers.get('Retry-After'))
                        view = volume('secure')['encryption']
                        record('c', wrongAttempts=wrong, rightPasswordDuringBackoff={'status': status, 'retryAfter': retry},
                               retryAfterSecondsInView=view.get('retryAfterSeconds'))
                        time.sleep(retry + 1)
                        status, body, seconds, _ = unlock('secure', pw['secure'])
                        assert status == 204, (status, body)
                        assert volume('secure')['mounted']
                        assert checksum(sums['secure'][0]) == sums['secure'][1]
                        for name in ('vault', 'grow5'):
                            s, b, sec, _ = unlock(name, pw[name])
                            assert s == 204, (name, s, b)
                            assert checksum(sums[name][0]) == sums[name][1], name
                            record('c', **{f'{name}UnlockSeconds': sec})
                        wait(lambda: '[docs]' in remote('testparm -s 2>/dev/null || true') and '[archive]' in remote('testparm -s 2>/dev/null || true'), 'shares back', 60)
                        got = wait(lambda: smb('docs', f'get blob.bin {directory}/smb-blob.bin', account).returncode == 0, 'SMB after unlock', 60)
                        assert hashlib.sha256((directory / 'smb-blob.bin').read_bytes()).hexdigest() == sums['secure'][1]
                        root = wait(lambda: remote("docker info -f '{{.DockerRootDir}}'", check=False).stdout.strip(), 'docker after unlock', 180, 3)
                        assert root.startswith('/mnt/secure/'), root
                        assert 'lukstest' in remote("docker volume ls -q")
                        record('c', unlockSeconds=seconds, checksumsMatch=True, smbAfterUnlock=True, dockerRoot=root,
                               dockerVolumeKept=True, passwordsKnown=volume('secure')['encryption']['passwords'], ok=True)

                        # d) lock while an SMB client is connected to the share.
                        client = subprocess.Popen(['smbclient', '//127.0.0.1/docs', '-p', str(smb_port), '-U', 'enctester', '-m', 'SMB3'],
                                                  env=dict(os.environ, PASSWD=account), stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                                                  stderr=subprocess.STDOUT, text=True)
                        client.stdin.write('ls\n')
                        client.stdin.flush()
                        sessions = wait(lambda: (lambda out: out if 'docs' in out else None)(remote('smbstatus -S 2>/dev/null || true')), 'SMB session', 30, 1)
                        started = time.monotonic()
                        status, _, body = call('/storage/volumes/secure/lock', {})
                        lock_seconds = round(time.monotonic() - started, 1)
                        assert status == 204, (status, body)
                        after = {'smbstatus': remote('smbstatus -S 2>/dev/null || true'),
                                 'mounted': remote('mountpoint -q /mnt/secure && echo yes || echo no'),
                                 'mapper': remote('test -e /dev/mapper/titanos-crypt-secure && echo open || echo closed'),
                                 'docker': remote('systemctl is-active docker.service', check=False).stdout.strip()}
                        assert 'docs' not in after['smbstatus'] and after['mounted'] == 'no' and after['mapper'] == 'closed', after
                        try:
                            client.stdin.write('ls\nexit\n')
                            client.stdin.flush()
                        except BrokenPipeError:
                            pass
                        client_out = client.communicate(timeout=60)[0]
                        v = volume('secure')
                        assert not v['mounted'] and v['encryption']['locked'], v
                        # Locked on purpose: no new waiting notice.
                        notices = [x for x in request('/notifications')['messages'] if x['kind'] == 'volume_locked' and x['subject'] == 'secure']
                        status, body, seconds, _ = unlock('secure', pw['secure'])
                        assert status == 204, (status, body)
                        assert checksum(sums['secure'][0]) == sums['secure'][1]
                        wait(lambda: smb('docs', 'ls', account).returncode == 0, 'SMB after second unlock', 60)
                        wait(lambda: remote("docker info -f '{{.DockerRootDir}}'", check=False).stdout.strip().startswith('/mnt/secure'), 'docker after relock', 180, 3)
                        record('d', smbSessionBeforeLock=sessions.splitlines()[-3:], lockSeconds=lock_seconds, afterLock=after,
                               clientAfterLock=client_out.strip().splitlines()[-4:], volumeLockedNoticesForSecure=len(notices),
                               unlockSeconds=seconds, ok=True)

                        # e) passwords and header backup.
                        def passwords(name, action, current, new=''):
                            status, _, body = call(f'/storage/volumes/{name}/passwords', {'action': action, 'current': current, 'password': new})
                            return status, body.decode(errors='replace')
                        status, body = passwords('secure', 'change', pw['secure'], pw['secure2'])
                        assert status == 204, body
                        status, body = passwords('secure', 'add', pw['secure2'], pw['secondary'])
                        assert status == 204, body
                        slots = volume('secure')['encryption']['passwords']
                        assert slots == 2, slots
                        status, body = passwords('secure', 'remove', pw['secure'])
                        assert status == 400 and 'volume_wrong_password' in body, (status, body)
                        assert call('/storage/volumes/secure/lock', {})[0] == 204
                        old = unlock('secure', pw['secure'])
                        assert old[0] == 400, old[:2]
                        second = unlock('secure', pw['secondary'])
                        assert second[0] == 204, second[:2]
                        assert checksum(sums['secure'][0]) == sums['secure'][1]
                        device = remote("cryptsetup status titanos-crypt-secure | awk '/device:/{print $2}'")
                        test_new = remote(f'printf %s {shlex.quote(pw["secure2"])} | cryptsetup open --test-passphrase --type luks --key-file=- {device} && echo ok')
                        tars = {}
                        for name in ('secure', 'vault'):
                            status, headers, body = call(f'/storage/volumes/{name}/luks-header')
                            assert status == 200 and headers.get('Content-Type') == 'application/x-tar', (status, headers)
                            with tarfile.open(fileobj=io.BytesIO(body)) as archive_file:
                                members = [m for m in archive_file.getmembers() if m.isfile()]
                                files = {m.name: archive_file.extractfile(m).read() for m in members}
                            headers_ok = [n for n, data in files.items() if data[:6] in LUKS_MAGIC]
                            tars[name] = {'bytes': len(body), 'files': sorted(files), 'luksHeaders': len(headers_ok)}
                            header_tar[name] = body
                            (directory / f'{name}-luks-header.tar').write_bytes(body)
                        assert tars['secure']['luksHeaders'] == 1 and tars['vault']['luksHeaders'] == 2, tars
                        record('e', changed=True, added=True, keySlots=slots, removeWithOldPasswordRejected=True,
                               oldPasswordAfterChange=old[0], unlockWithSecondPassword=second[0], newPasswordTest=test_new,
                               headerBackups=tars, ok=True)

                        # f) mdadm spare, RAID5 growth through LUKS, Btrfs disk with password.
                        request('/storage/volumes/secure/disks', {'disk': names['MD3'], 'erase': True, 'identity': ident['MD3']})
                        member_states = {m['disk']: m['state'] for m in volume('secure')['members']}
                        assert member_states.get(names['MD3']) == 'spare', member_states
                        wait(lambda: volume('grow5')['state'] == 'ok' and not volume('grow5').get('sync'), 'RAID5 initial sync', 600, 5)
                        before = {'luksSectors': remote("cryptsetup status titanos-crypt-grow5 | awk '/^ *size:/{print $2}'"),
                                  'fsBytes': remote("df -B1 --output=size /mnt/grow5 | tail -1")}
                        growth = {'disk': names['R4'], 'identity': ident['R4'], 'recoveryVolume': 'vault', 'erase': True, 'confirm': 'grow5'}
                        preview = request('/storage/volumes/grow5/growth/preview', growth)
                        status, _, body = call('/storage/volumes/grow5/growth', growth)
                        assert status == 202, (status, body)
                        def growth_done():
                            g = request('/storage/growth')
                            if g['operation'].get('error') or g['error']:
                                raise AssertionError(g)
                            return not g['pending'] and not g['active']
                        wait(growth_done, 'RAID5 growth', 1200, 5)
                        after = {'luksSectors': remote("cryptsetup status titanos-crypt-grow5 | awk '/^ *size:/{print $2}'"),
                                 'fsBytes': remote("df -B1 --output=size /mnt/grow5 | tail -1"),
                                 'raidDisks': remote("mdadm --detail /dev/md/grow5 | awk '/Raid Devices/{print $4}'")}
                        assert int(after['luksSectors']) > int(before['luksSectors']) * 1.4, (before, after)
                        assert int(after['fsBytes']) > int(before['fsBytes']) * 1.4, (before, after)
                        assert checksum(sums['grow5'][0]) == sums['grow5'][1]
                        bad = call('/storage/volumes/vault/disks', {'disk': names['BT3'], 'erase': True, 'identity': ident['BT3'], 'profile': 'raid1', 'password': 'not-the-password'})
                        assert bad[0] == 400 and b'volume_wrong_password' in bad[2], bad[:3:2]
                        untouched = remote(f'blkid -p /dev/{names["BT3"]} || echo blank')
                        request('/storage/volumes/vault/disks', {'disk': names['BT3'], 'erase': True, 'identity': ident['BT3'], 'profile': 'raid1', 'password': pw['vault']})
                        wait(lambda: volume('vault')['state'] == 'ok', 'Btrfs balance', 600, 5)
                        btrfs = remote('btrfs filesystem show /mnt/vault | grep path').splitlines()
                        assert len(btrfs) == 3 and all('/dev/mapper/titanos-crypt-vault-' in line for line in btrfs), btrfs
                        enc = volume('vault')['encryption']
                        assert enc['members'] == 3 and enc['unlocked'] == 3, enc
                        assert checksum(sums['vault'][0]) == sums['vault'][1]
                        record('f', mdadmSpare=member_states, raid5Preview={k: preview.get(k) for k in ('currentBytes', 'plannedBytes', 'targetDisks')},
                               raid5Before=before, raid5After=after, btrfsWrongPasswordRejected=True, btrfsDiskUntouchedAfterWrongPassword=untouched,
                               btrfsDevices=btrfs, btrfsEncryption=enc, ok=True)

                        # g) SSD read cache, effective at the next unlock.
                        request('/storage/volumes/secure/cache', {'disk': names['SSD1'], 'mode': 'writethrough', 'erase': True, 'identity': ident['SSD1']}, 'PUT')
                        record('g', cacheConfigured=volume('secure').get('cache'))

                    if boot == '3':
                        state = locked_state(['secure', 'vault', 'grow5'])
                        assert all(s['encryption']['locked'] for s in state.values()), state
                        status, body, seconds, _ = unlock('secure', pw['secondary'])
                        assert status == 204, (status, body)
                        tree = remote('dmsetup ls --tree')
                        table = remote('dmsetup table titanos-crypt-secure | cut -d" " -f1-3,6-')
                        deps = remote('dmsetup deps -o devname titanos-crypt-secure; dmsetup deps -o devname titanos-cache-secure')
                        assert 'titanos-cache-secure' in deps.splitlines()[0], deps
                        assert 'md' in deps.splitlines()[1], deps
                        assert checksum(sums['secure'][0]) == sums['secure'][1]
                        cache = remote('dmsetup status titanos-cache-secure')
                        for name in ('vault', 'grow5'):
                            s, b, sec, _ = unlock(name, pw[name])
                            assert s == 204, (name, s, b)
                            assert checksum(sums[name][0]) == sums[name][1], name
                        record('g', dmsetupTree=tree.splitlines(), cryptDeps=deps.splitlines(), cacheStatus=cache.split()[:12],
                               cryptTarget=table.split()[:3], unlockSeconds=seconds, checksumsMatch=True,
                               healthReadyWhileLocked=health.get('ready'), ok=True)

                    if boot == '4':
                        # i) unlock memory on a 2 GiB guest.
                        state = locked_state(['secure', 'vault', 'grow5'])
                        assert all(s['encryption']['locked'] for s in state.values()), state
                        meminfo = remote("awk '/MemTotal|MemAvailable/{print $1,$2}' /proc/meminfo")
                        script = ('while [ ! -e /run/stop-sampler ]; do for p in $(pidof cryptsetup); do '
                                  'awk \'/VmHWM/{print "H",$2}\' /proc/$p/status 2>/dev/null; done; '
                                  'awk \'/MemAvailable/{print "A",$2}\' /proc/meminfo; sleep 0.05; done > /run/sampler.log')
                        put(script.encode(), '/run/sampler.sh')
                        remote('rm -f /run/stop-sampler; systemd-run --unit=luks-sampler sh /run/sampler.sh')
                        unlocks = {}
                        for name, password in (('vault', pw['vault']), ('secure', pw['secondary']), ('grow5', pw['grow5'])):
                            s, b, sec, _ = unlock(name, password)
                            assert s == 204, (name, s, b)
                            unlocks[name] = sec
                        remote('touch /run/stop-sampler; sleep 1')
                        samples = remote('cat /run/sampler.log').splitlines()
                        hwm = [int(x.split()[1]) for x in samples if x.startswith('H ')]
                        avail = [int(x.split()[1]) for x in samples if x.startswith('A ')]
                        oom = remote("journalctl -k -b --no-pager | grep -ciE 'out of memory|oom-kill|invoked oom-killer' || true")
                        for name in ('secure', 'vault', 'grow5'):
                            assert checksum(sums[name][0]) == sums[name][1], name
                        slots = remote('cryptsetup luksDump "$(cryptsetup status titanos-crypt-secure | awk \'/device:/{print $2}\')" | grep -E "Memory:|PBKDF:"')
                        assert hwm and max(hwm) >= 200 * 1024, hwm[-5:]
                        assert oom == '0', oom
                        record('i', guestRAM=memory, meminfoBeforeUnlock=meminfo.splitlines(), cryptsetupPeakRSSKiB=max(hwm),
                               samples=len(samples), minMemAvailableKiB=min(avail) if avail else None, oomEvents=int(oom),
                               unlockSeconds=unlocks, keySlotPBKDF=slots.splitlines(), titandState=remote('systemctl is-active titand'),
                               healthReadyWhileLocked=health.get('ready'), ok=True)

                        # h) delete: one unlocked mdadm volume, one locked Btrfs volume.
                        md_members = remote("mdadm --detail /dev/md/grow5 | awk '/active sync/{print $NF}'").split()
                        md_offsets = remote("cryptsetup status titanos-crypt-grow5 | awk '/offset:/{print $2}'")
                        assert call('/storage/volumes/vault/lock', {})[0] == 204
                        vault_parts = remote("blkid -t TYPE=crypto_LUKS -o device | sort").split()
                        vault_parts = [p for p in vault_parts if any(p.startswith('/dev/' + names[s]) for s in ('BT1', 'BT2', 'BT3'))]
                        starts = {p: remote(f'cat /sys/class/block/{Path(p).name}/start') for p in vault_parts}
                        luks_uuids = {p: remote(f'cryptsetup luksUUID {p}') for p in vault_parts}
                        parents = {p: '/dev/' + remote(f'lsblk -ndo PKNAME {p}') for p in vault_parts}
                        assert len(vault_parts) == 3, vault_parts
                        request('/storage/volumes/grow5', {'confirm': 'grow5'}, 'DELETE')
                        request('/shares/archive', method='DELETE')  # the share only, its data stays
                        request('/storage/volumes/vault', {'confirm': 'vault'}, 'DELETE')
                        names_now = [v['name'] for v in request('/storage')['volumes']]
                        assert 'grow5' not in names_now and 'vault' not in names_now, names_now
                        checks = {}
                        for s in ('R1', 'R2', 'R3', 'R4', 'BT1', 'BT2', 'BT3'):
                            dev = '/dev/' + names[s]
                            parts = remote(f'lsblk -nrpo NAME {dev}').split()
                            is_luks = [p for p in parts if remote(f'cryptsetup isLuks {p} && echo yes || echo no') == 'yes']
                            magic = remote(f"head -c 64M {dev} | LC_ALL=C grep -c -a -P 'LUKS\\xba\\xbe|SKUL\\xba\\xbe' || true")
                            checks[s] = {'devices': parts, 'isLuks': is_luks, 'luksMagicIn64MiB': int(magic)}
                        assert all(not c['isLuks'] and not c['luksMagicIn64MiB'] for c in checks.values()), checks
                        # Can an old header backup bring the data back? Restore the
                        # backup from e) at the former start of one Btrfs member.
                        put(header_tar['vault'], '/run/vault-header.tar')
                        # A member that the backup from e) knows (BT3 came later).
                        part = next(p for p in vault_parts if luks_uuids[p].encode() in header_tar['vault'])
                        disk_dev = parents[part]
                        attack = remote(
                            'set +e; mkdir -p /run/hdr; tar -xf /run/vault-header.tar -C /run/hdr; '
                            f'loop=$(losetup -f --show -o $(({starts[part]}*512)) {disk_dev}); '
                            'for f in $(find /run/hdr -type f -name "*.img" -o -type f -name "*header*" | grep -v txt); do '
                            '  u=$(cryptsetup luksUUID "$f" 2>/dev/null) || continue; '
                            '  cryptsetup luksHeaderRestore --batch-mode "$loop" --header-backup-file "$f" >/dev/null 2>&1 || continue; '
                            f'  if printf %s {shlex.quote(pw["vault"])} | cryptsetup open --test-passphrase --type luks --key-file=- "$loop" 2>/dev/null; '
                            '  then echo "opened-with-$u"; break; else echo "no-$u"; fi; '
                            'done; losetup -d "$loop"', check=False)
                        record('h', mdMembers=md_members, mdLuksOffsetSectors=md_offsets, btrfsMemberParts=vault_parts,
                               deletedVolumes=['grow5', 'vault'], afterDelete=checks,
                               oldHeaderBackupRestoreAtFormerOffset=attack.stdout.strip() + attack.stderr.strip()[-200:], ok=True)
                        results['oldHeaderBackupReopensErasedMember'] = 'opened-with-' in attack.stdout

                    print(f'boot {boot} passed', flush=True)
                    status, _, body = call('/system/power', {'action': 'poweroff' if boot == boots[-1][0] else 'reboot'})
                    process.wait(timeout=120)
                except Exception as error:
                    results['failure'] = f'boot {boot}: {type(error).__name__}: {error}'
                    (output / 'encryption-result.json').write_text(json.dumps(results, indent=2) + '\n')
                    print(f'QEMU process exit: {process.poll()}', flush=True)
                    log = subprocess.run([*ssh, 'sudo journalctl -b -u titand.service --no-pager -n 150'], capture_output=True, text=True, timeout=30)
                    (output / f'encryption-titand-boot{boot}.log').write_text(log.stdout + log.stderr)
                    raise
                finally:
                    if process.poll() is None:
                        process.terminate()
                        try:
                            process.wait(timeout=10)
                        except subprocess.TimeoutExpired:
                            process.kill()
                            process.wait()
    results['completed'] = time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime())
    (output / 'encryption-result.json').write_text(json.dumps(results, indent=2) + '\n')
    print('encryption smoke test passed', flush=True)


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument('zip', type=Path)
    parser.add_argument('--output', type=Path, default=Path('out/validation/encryption'))
    parser.add_argument('--timeout', type=int, default=420)
    parser.add_argument('--tcg', action='store_true', help='force CPU emulation to reproduce CI without KVM')
    args = parser.parse_args()
    smoke(args.zip.resolve(), args.output.resolve(), args.timeout, args.tcg)
