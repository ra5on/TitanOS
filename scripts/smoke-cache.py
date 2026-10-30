#!/usr/bin/env python3
"""SSD cache (dm-cache) with real device-mapper, in disposable UEFI QEMU disks only.

Boots the stick ZIP six times with fresh qcow2 data disks (never host block
devices) and checks docs/SSD-CACHE.md against the real kernel in the guest.
Volumes: "plain" (mdadm RAID1, ext4, cache SSD1) and "secret" (encrypted
mdadm RAID1, ext4, cache SSD2); FREE1 is a free disk as a control.

  a) write cache (writeback) switched on, effective after a reboot; data
     written, `dmsetup status` shows a cache in writeback mode;
  b) write cache switched off while a writer in the guest keeps writing:
     the cache leaves the configuration, the device turns into a `linear`
     mapping of the array, after a reboot every checksum matches and
     e2fsck -fn is clean (frozen file system, read-only loop on the array);
  c) read cache (writethrough): one start without the SSD mounts the volume
     without cache (volume_cache_missing, `stale` saved), a file is changed
     in place; the next start with the SSD begins with an empty cache
     (`stale` gone) and reads the changed data, not the cached old blocks;
  d) deleting the encrypted volume with a write cache leaves no LUKS header
     on the former md members nor anywhere on the cache SSD;
  e) the cache SSDs are not offered in GET /usb/disks, also while the
     encrypted volume is locked;
  f) writeback -> writethrough through the API (after writing back),
     `dmsetup status` shows writethrough.

Results go to <output>/cache-result.json.
"""
import argparse
import hashlib
import http.cookiejar
import importlib.util
import json
import os
from pathlib import Path
import secrets
import shlex
import shutil
import ssl
import subprocess
import tempfile
import time
import urllib.error
import urllib.request
import zipfile

spec = importlib.util.spec_from_file_location('release_smoke', Path(__file__).with_name('smoke-release.py'))
helper = importlib.util.module_from_spec(spec)
spec.loader.exec_module(helper)

LUKS_MAGIC = r'LUKS\xba\xbe|SKUL\xba\xbe'
# Serial → size of the throwaway disks. MD: RAID1 of "plain", EN: RAID1 of
# "secret" (encrypted), SSD1/SSD2: their caches (SATA), FREE1: control disk (SATA).
DISKS = {'MD1': '1G', 'MD2': '1G', 'SSD1': '1G', 'EN1': '1G', 'EN2': '1G', 'SSD2': '1G', 'FREE1': '512M'}
# q35 AHCI port of the disks that GET /usb/disks must be able to see (port 2
# is the default CD-ROM).
SATA = {'SSD1': 0, 'SSD2': 1, 'FREE1': 3}
WRITER_FILES = 24  # files of 8 MiB written while the write cache is switched off


def smoke(archive, output, timeout, force_tcg=False, hold=False):
    os.environ['PATH'] += ':/usr/sbin:/sbin'
    output.mkdir(parents=True, exist_ok=True)
    results = {'started': time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime()), 'zip': archive.name,
               'zipSha256': hashlib.sha256(archive.read_bytes()).hexdigest(), 'phases': {}}

    def record(phase, **evidence):
        results['phases'].setdefault(phase, {}).update(evidence)
        (output / 'cache-result.json').write_text(json.dumps(results, indent=2) + '\n')
        print(f'[{phase}]', json.dumps(evidence)[:600], flush=True)

    with tempfile.TemporaryDirectory(prefix='titanos-cache-', dir=output) as tmp:
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
        port, ssh_port = helper.free_port(), helper.free_port()
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
        ssh = ['ssh', *ssh_options, '-p', str(ssh_port), 'cachetester@127.0.0.1']

        def remote(command, timeout=300, check=True):
            result = subprocess.run([*ssh, 'sudo sh -ec ' + shlex.quote(command)], text=True, capture_output=True, timeout=timeout)
            if check and result.returncode:
                raise RuntimeError(f'guest command failed ({result.returncode}): {command}\n{result.stdout}{result.stderr}')
            return result.stdout.strip() if check else result

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

        def storage():
            return request('/storage')

        def volume(name):
            return next(v for v in storage()['volumes'] if v['name'] == name)

        def disks():
            return {d['serial']: d for d in storage()['disks'] if d.get('serial')}

        def usb_serials():
            return sorted(d.get('serial') or d.get('device') for d in request('/usb/disks')['disks'])

        def signatures(device):
            """blkid -p of every partition of a guest disk: what udev and
            /usb/disks see there (a cache partition can hold a copy of the
            array's first block, with the file system's UUID)."""
            parts = remote(f'lsblk -nrpo NAME,TYPE {device} | awk \'$2=="part"{{print $1}}\'').split()
            return {p: remote(f'blkid -p -o export {p} 2>/dev/null | grep -E "^(TYPE|UUID|LABEL)=" || true').split() for p in parts}

        def config(name):
            """The volume as saved in /config/storage/volumes.json."""
            saved = json.loads(remote('cat /config/storage/volumes.json'))
            saved = saved['volumes'] if isinstance(saved, dict) else saved
            return next(v for v in saved if v['name'] == name)

        def checksum(path, direct=False):
            """sha256 of a guest file; direct bypasses the page cache (O_DIRECT)."""
            if direct:
                return remote(f'dd if={shlex.quote(path)} iflag=direct bs=1M status=none | sha256sum').split()[0]
            return remote(f'sha256sum {shlex.quote(path)}').split()[0]

        def write(path, mib, notrunc=False):
            """Writes random data, returns the sha256 of what was written."""
            flags = 'conv=notrunc,fsync' if notrunc else 'conv=fsync'
            return remote(f'head -c {mib}M /dev/urandom > /run/cache-blob; sha256sum /run/cache-blob | cut -d" " -f1; '
                          f'dd if=/run/cache-blob of={shlex.quote(path)} bs=1M {flags} status=none; rm /run/cache-blob')

        def dm_status(name):
            out = remote(f'dmsetup status {name}')
            fields = out.split()
            # 0 <len> cache <mbs> <mu>/<mt> <cbs> <cu>/<ct> <rh> <rm> <wh> <wm> <dem> <pro> <dirty> <#f> <features>...
            info = {'raw': out, 'target': fields[2] if len(fields) > 2 else ''}
            if info['target'] == 'cache':
                used, total = fields[6].split('/')
                info.update(usedBlocks=int(used), totalBlocks=int(total), readHits=int(fields[7]), readMisses=int(fields[8]),
                            dirty=int(fields[13]),
                            mode=next((f for f in fields[15:] if f in ('writeback', 'writethrough', 'passthrough')), 'writeback'))
            return info

        def notices(kind, subject):
            return [m for m in request('/notifications')['messages'] if m['kind'] == kind and m['subject'] == subject]

        def mount_source(name):
            return remote(f'findmnt -n -o SOURCE /mnt/{name}')

        def ext4_errors():
            return remote("dmesg | grep -E 'EXT4-fs (error|warning)|I/O error|device-mapper: cache.*(error|fail)' || true")

        accel = ['-accel', 'kvm', '-cpu', 'host'] if not force_tcg and os.access('/dev/kvm', os.W_OK) else ['-accel', 'tcg', '-cpu', 'max']
        results['accel'] = accel[1]

        def qemu(without=()):
            args = ['qemu-system-x86_64', '-machine', 'q35', *accel, '-smp', '2', '-m', '4G',
                    '-drive', 'if=pflash,format=raw,readonly=on,file=/usr/share/OVMF/OVMF_CODE_4M.fd',
                    '-drive', f'if=pflash,format=raw,file={directory}/OVMF_VARS.fd',
                    '-device', 'qemu-xhci', '-drive', f'if=none,id=stick,format=raw,file={disk}',
                    '-device', 'usb-storage,drive=stick,removable=on,bootindex=0',
                    '-netdev', f'user,id=net0,hostfwd=tcp:127.0.0.1:{port}-:443,hostfwd=tcp:127.0.0.1:{ssh_port}-:22',
                    '-device', 'virtio-net-pci,netdev=net0',
                    '-smbios', 'type=11,value=io.systemd.boot.kernel-cmdline-extra=console=tty0 console=ttyS0,,115200',
                    '-display', 'none', '-serial', 'stdio', '-monitor', 'none', '-no-reboot']
            for serial in DISKS:
                if serial in without:
                    continue
                # Default discard=ignore: blkdiscard succeeds in the guest but
                # the old blocks stay readable, the strict case for d).
                args += ['-drive', f'if=none,id={serial},format=qcow2,file={directory}/{serial}.qcow2']
                if serial in SATA:
                    # GET /usb/disks only lists real transports (not virtio):
                    # SSDs and the control disk sit on the q35 AHCI ports.
                    rotation = ',rotation_rate=1' if serial.startswith('SSD') else ''
                    args += ['-device', f'ide-hd,drive={serial},bus=ide.{SATA[serial]},serial={serial}{rotation}']
                else:
                    args += ['-device', f'virtio-blk-pci,drive={serial},serial={serial}']
            return args

        password = secrets.token_urlsafe(18)
        account = secrets.token_urlsafe(18)
        sums = {}  # guest path → sha256 expected on the volume
        state = {}
        # Boot number → disks left out.
        boots = (('1', ()), ('2', ()), ('3', ()), ('4', ()), ('5', ('SSD1',)), ('6', ()))
        with (output / 'cache-serial.log').open('wb') as serial:
            for boot, without in boots:
                print(f'boot {boot}: without {list(without) or "nothing"} ({accel[1]})', flush=True)
                process = subprocess.Popen(qemu(without), stdin=subprocess.DEVNULL, stdout=serial, stderr=subprocess.STDOUT)
                try:
                    def ready():
                        if process.poll() is not None:
                            raise SystemExit(f'QEMU stopped before health (exit {process.returncode})')
                        answer = request('/health')
                        return answer if answer.get('ready') else None
                    wait(ready, 'health', timeout, 3)
                    if boot == '1':
                        request('/setup', {'name': 'cachetester', 'password': account})
                        request('/account/ssh-keys', {'key': (directory / 'key.pub').read_text().strip(), 'password': account})
                    else:
                        request('/login', {'name': 'cachetester', 'password': account})
                    request('/system/ssh', {'enabled': True}, 'PUT')
                    wait(lambda: subprocess.run([*ssh, 'true'], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL).returncode == 0, 'SSH', 60, 1)
                    present = disks()
                    names = {s: d['name'] for s, d in present.items()}
                    ident = {s: d['identity'] for s, d in present.items()}

                    if boot == '1':
                        record('environment', kernel=remote('uname -r'), dmsetup=remote('dmsetup --version').splitlines(),
                               dmCache=remote('modprobe dm_cache && modprobe dm_cache_smq && test -d /sys/module/dm_cache && echo loaded'),
                               tools={t: remote(f'command -v {t} || echo missing') for t in ('e2fsck', 'fsfreeze', 'blkdiscard', 'cryptsetup', 'mdadm', 'blockdev')})
                        for name, members, encrypt in (('plain', ['MD1', 'MD2'], False), ('secret', ['EN1', 'EN2'], True)):
                            chosen = [names[m] for m in members]
                            body = {'name': name, 'level': 'raid1', 'filesystem': 'ext4', 'disks': chosen, 'erase': chosen,
                                    'identities': {names[m]: ident[m] for m in members}}
                            if encrypt:
                                body.update(encrypt=True, password=password)
                            request('/storage/volumes', body)
                            assert volume(name)['mounted'], volume(name)
                        usb_before = usb_serials()
                        assert {'SSD1', 'SSD2', 'FREE1'} <= set(usb_before), usb_before
                        for name, ssd in (('plain', 'SSD1'), ('secret', 'SSD2')):
                            request(f'/storage/volumes/{name}/cache', {'disk': names[ssd], 'mode': 'writeback', 'erase': True,
                                                                         'identity': ident[ssd]}, 'PUT')
                        view = {n: volume(n).get('cache') for n in ('plain', 'secret')}
                        assert all(v and v['mode'] == 'writeback' and not v['active'] for v in view.values()), view
                        usb_after = usb_serials()
                        assert 'SSD1' not in usb_after and 'SSD2' not in usb_after and 'FREE1' in usb_after, usb_after
                        remote('mkdir -p /mnt/plain/a /mnt/plain/b /mnt/plain/c /mnt/secret/d')
                        sums['/mnt/plain/a/before.bin'] = write('/mnt/plain/a/before.bin', 32)
                        sums['/mnt/secret/d/before.bin'] = write('/mnt/secret/d/before.bin', 32)
                        record('a', cacheConfigured=view, effectiveBeforeReboot=False, mountBeforeReboot=mount_source('plain'))
                        record('e', usbBeforeCache=usb_before, usbAfterCache=usb_after)

                    if boot == '2':
                        # e) secret is locked: its SSD must still not be offered.
                        sec = volume('secret')
                        assert not sec['mounted'] and sec['encryption']['locked'], sec
                        usb_locked = usb_serials()
                        assert 'SSD2' not in usb_locked and 'SSD1' not in usb_locked and 'FREE1' in usb_locked, usb_locked
                        dm_locked = remote('dmsetup ls')
                        record('e', usbWhileLocked=usb_locked, secretCache=sec.get('cache'), dmsetupWhileLocked=dm_locked.splitlines(), ok=True)

                        # a) write cache active after the reboot.
                        status = dm_status('titanos-cache-plain')
                        assert status['target'] == 'cache' and status['mode'] == 'writeback', status
                        source = mount_source('plain')
                        assert source == '/dev/mapper/titanos-cache-plain', source
                        sums['/mnt/plain/a/after.bin'] = write('/mnt/plain/a/after.bin', 64)
                        status_after = dm_status('titanos-cache-plain')
                        record('a', ssd1SignaturesWhileActive=signatures('/dev/' + names['SSD1']))
                        for path in ('/mnt/plain/a/before.bin', '/mnt/plain/a/after.bin'):
                            assert checksum(path) == sums[path], path
                        record('a', mountSource=source, statusAtStart=status, statusAfterWrite=status_after,
                               table=remote('dmsetup table titanos-cache-plain').split()[:4] + ['…'],
                               view=volume('plain')['cache'], ok=True)

                        # b) switch the write cache off while a writer keeps writing.
                        script = ('set -e; mkdir -p /run/cachewriter; i=1; while [ $i -le %d ]; do '
                                  'head -c 8M /dev/urandom > /run/cachewriter/blob; '
                                  'sha256sum /run/cachewriter/blob | cut -d" " -f1 > /run/cachewriter/f$i.sha; '
                                  'dd if=/run/cachewriter/blob of=/mnt/plain/b/f$i.bin bs=1M status=none; '
                                  'sleep 0.5; i=$((i+1)); done; rm /run/cachewriter/blob; sync -f /mnt/plain/b; touch /run/cachewriter/done') % WRITER_FILES
                        remote('systemd-run --unit=cachewriter sh -ec ' + shlex.quote(script))
                        wait(lambda: remote('ls /run/cachewriter/ | grep -c sha || true') >= '2', 'writer started', 60, 1)
                        dirty_before = dm_status('titanos-cache-plain')
                        # Diagnostic: the first cache block can be a copy of the
                        # array's first block (ext4 of "plain", same UUID).
                        record('b', ssd1SignaturesBeforeDisable=signatures('/dev/' + names['SSD1']))
                        started = time.monotonic()
                        status_code, _, body = call('/storage/volumes/plain/cache', method='DELETE')
                        assert status_code == 204, (status_code, body)
                        removing = volume('plain').get('cache')
                        writer_during = remote('systemctl is-active cachewriter', check=False).stdout.strip()
                        wait(lambda: volume('plain').get('cache') is None, 'cache leaves the configuration', 900, 3)
                        forgotten = round(time.monotonic() - started, 1)
                        writer_at_forget = remote('systemctl is-active cachewriter', check=False).stdout.strip()
                        table = remote('dmsetup table titanos-cache-plain 2>/dev/null || echo gone')
                        assert table == 'gone' or table.split()[2] == 'linear', table
                        assert 'cache' not in config('plain'), config('plain')
                        wait(lambda: remote('test -e /run/cachewriter/done && echo done', check=False).stdout.strip() == 'done',
                             'writer finished', 600, 2)
                        for i in range(1, WRITER_FILES + 1):
                            sums[f'/mnt/plain/b/f{i}.bin'] = remote(f'cat /run/cachewriter/f{i}.sha')
                        remote('sync')
                        removed = wait(lambda: notices('volume_cache_removed', 'plain'), 'volume_cache_removed notice', 60)
                        # Forgotten means free: no copy of the array's blocks
                        # may look like the volume's file system any more.
                        # (udev only re-probes at the next start, so check the
                        # partitions directly: titand zeroes their first MiB.)
                        ssd1_after = signatures('/dev/' + names['SSD1'])
                        assert len(ssd1_after) == 2 and not any(ssd1_after.values()), ssd1_after
                        for part in ssd1_after:
                            assert remote(f'cmp -s -n 1048576 {part} /dev/zero && echo zero || echo data') == 'zero', part
                        record('b', statusBeforeDisable=dirty_before, viewAfterDisable=removing, writerWhenDisabled=writer_during,
                               secondsUntilForgotten=forgotten, writerWhenForgotten=writer_at_forget,
                               tableAfter=table.split()[:5], mountSource=mount_source('plain'), filesWritten=WRITER_FILES,
                               noticeRemoved=len(removed), ssd1Signatures=ssd1_after, usbAfterRemoval=usb_serials(), kernelErrors=ext4_errors().splitlines())

                        # f) unlock secret; writeback → writethrough while dirty.
                        status_code, _, body = call('/storage/volumes/secret/unlock', {'password': password})
                        assert status_code == 204, (status_code, body)
                        deps = remote('dmsetup deps -o devname titanos-crypt-secret')
                        assert 'titanos-cache-secret' in deps, deps
                        assert dm_status('titanos-cache-secret')['mode'] == 'writeback'
                        assert checksum('/mnt/secret/d/before.bin') == sums['/mnt/secret/d/before.bin']
                        sums['/mnt/secret/d/f.bin'] = write('/mnt/secret/d/f.bin', 96)
                        before = dm_status('titanos-cache-secret')
                        started = time.monotonic()
                        request('/storage/volumes/secret/cache', {'mode': 'writethrough'}, 'PUT')
                        switched = round(time.monotonic() - started, 1)
                        after = dm_status('titanos-cache-secret')
                        assert after['mode'] == 'writethrough' and after['dirty'] == 0, after
                        assert 'writethrough' in remote('dmsetup table titanos-cache-secret').split(), 'table'
                        assert volume('secret')['cache']['mode'] == 'writethrough' and config('secret')['cache']['mode'] == 'writethrough'
                        for path in ('/mnt/secret/d/before.bin', '/mnt/secret/d/f.bin'):
                            assert checksum(path) == sums[path], path
                        record('f', depsCrypt=deps.split(), beforeSwitch=before, secondsToSwitch=switched, afterSwitch=after, ok=True)

                        # d) back to writeback, make blocks dirty, delete the volume.
                        request('/storage/volumes/secret/cache', {'mode': 'writeback'}, 'PUT')
                        assert dm_status('titanos-cache-secret')['mode'] == 'writeback'
                        write('/mnt/secret/d/g.bin', 96)
                        dirty = dm_status('titanos-cache-secret')
                        md_members = remote("mdadm --detail /dev/md/secret | awk '/active sync/{print $NF}'").split()
                        luks_before = {s: int(remote(f"LC_ALL=C grep -c -a -P '{LUKS_MAGIC}' /dev/{names[s]} || true"))
                                       for s in ('EN1', 'EN2', 'SSD2')}
                        assert luks_before['EN1'] and luks_before['EN2'], luks_before
                        started = time.monotonic()
                        request('/storage/volumes/secret', {'confirm': 'secret'}, 'DELETE')
                        deleted = round(time.monotonic() - started, 1)
                        assert 'secret' not in [v['name'] for v in storage()['volumes']]
                        mapper = remote('dmsetup ls')
                        assert 'titanos-cache-secret' not in mapper and 'titanos-crypt-secret' not in mapper, mapper
                        checks = {}
                        for s in ('EN1', 'EN2', 'SSD2'):
                            dev = '/dev/' + names[s]
                            parts = remote(f'lsblk -nrpo NAME {dev}').split()
                            is_luks = [p for p in parts if remote(f'cryptsetup isLuks {p} && echo yes || echo no') == 'yes']
                            magic = remote(f"LC_ALL=C grep -c -a -P '{LUKS_MAGIC}' {dev} || true")
                            checks[s] = {'devices': parts, 'isLuks': is_luks, 'luksMagicWholeDisk': int(magic),
                                         'blkid': remote(f'blkid -p {dev} 2>&1 || true')}
                        checks['SSD2']['signatures'] = signatures('/dev/' + names['SSD2'])
                        checks['SSD2']['firstMiBZero'] = {p: remote(f'cmp -s -n 1048576 {p} /dev/zero && echo yes || echo no') == 'yes'
                                                          for p in checks['SSD2']['signatures']}
                        assert all(checks['SSD2']['firstMiBZero'].values()), checks['SSD2']
                        assert not any(checks['SSD2']['signatures'].values()), checks['SSD2']
                        assert all(not c['isLuks'] and not c['luksMagicWholeDisk'] for c in checks.values()), checks
                        record('d', statusBeforeDelete=dirty, mdMembers=md_members, luksMagicBefore=luks_before,
                               secondsToDelete=deleted, mapperAfter=mapper.splitlines(), afterDelete=checks, ok=True)

                    if boot == '3':
                        # b) after the reboot: no cache, data and file system intact.
                        assert volume('plain').get('cache') is None
                        source = mount_source('plain')
                        assert 'titanos-cache' not in source, source
                        assert remote('dmsetup ls | grep -c titanos-cache-plain || true') == '0'
                        bad = [p for p, s in sums.items() if p.startswith('/mnt/plain/') and checksum(p) != s]
                        assert not bad, bad
                        md = remote('readlink -f "$(findmnt -n -o SOURCE /mnt/plain)"')
                        # e2fsck refuses a mounted device; a frozen file system
                        # is consistent on disk and a read-only loop device
                        # (device-mapper would need an exclusive claim) shows it.
                        fsck = remote(
                            'set +e; sync; fsfreeze -f /mnt/plain; '
                            f'loop=$(losetup -r -f --show {md}); '
                            'e2fsck -fn "$loop" > /run/fsck.log 2>&1; rc=$?; '
                            'losetup -d "$loop"; fsfreeze -u /mnt/plain; tail -n 3 /run/fsck.log; echo rc=$rc', check=False)
                        assert fsck.returncode == 0 and fsck.stdout.strip().endswith('rc=0'), fsck.stdout + fsck.stderr
                        errors = ext4_errors()
                        assert not errors, errors
                        usb_now = usb_serials()
                        assert 'SSD1' in usb_now, (usb_now, signatures('/dev/' + names['SSD1']), remote('lsblk -o NAME,SERIAL,FSTYPE,UUID'))
                        record('b', afterReboot={'mountSource': source, 'checksumsMatch': len([p for p in sums if p.startswith('/mnt/plain/')]),
                                                 'e2fsck': fsck.stdout.strip().splitlines(), 'kernelErrors': [], 'usb': usb_now}, ok=True)
                        # c) read cache on SSD1, effective after the next start.
                        request('/storage/volumes/plain/cache', {'disk': names['SSD1'], 'mode': 'writethrough', 'erase': True,
                                                                 'identity': ident['SSD1']}, 'PUT')
                        record('c', configured=volume('plain')['cache'])

                    if boot == '4':
                        status = dm_status('titanos-cache-plain')
                        assert status['mode'] == 'writethrough', status
                        sums['/mnt/plain/c/data.bin'] = write('/mnt/plain/c/data.bin', 32)
                        state['oldC'] = sums['/mnt/plain/c/data.bin']
                        # Read it a few times past the page cache so that smq
                        # promotes its blocks onto the SSD.
                        for _ in range(6):
                            assert checksum('/mnt/plain/c/data.bin', direct=True) == state['oldC']
                        warm = dm_status('titanos-cache-plain')
                        assert warm['usedBlocks'] > 0, warm
                        state['warmUsed'] = warm['usedBlocks']
                        record('c', boot4={'mountSource': mount_source('plain'), 'statusWarm': warm})

                    if boot == '5':
                        # Without SSD1: mounted without cache, stale saved.
                        v = volume('plain')
                        assert v['mounted'] and v['cache']['missing'] and not v['cache']['active'], v
                        source = mount_source('plain')
                        assert 'titanos-cache' not in source, source
                        missing = wait(lambda: notices('volume_cache_missing', 'plain'), 'volume_cache_missing notice', 60)
                        saved = config('plain')['cache']
                        assert saved.get('stale') is True, saved
                        assert checksum('/mnt/plain/c/data.bin') == state['oldC']
                        sums['/mnt/plain/c/data.bin'] = write('/mnt/plain/c/data.bin', 32, notrunc=True)
                        assert sums['/mnt/plain/c/data.bin'] != state['oldC']
                        record('c', boot5={'view': v['cache'], 'mountSource': source, 'notices': len(missing), 'savedCache': saved,
                                           'changed': sums['/mnt/plain/c/data.bin']})

                    if boot == '6':
                        status = dm_status('titanos-cache-plain')
                        assert status['mode'] == 'writethrough', status
                        assert mount_source('plain') == '/dev/mapper/titanos-cache-plain'
                        saved = config('plain')['cache']
                        assert not saved.get('stale'), saved
                        assert status['usedBlocks'] < state['warmUsed'], (status, state)
                        remote('sync; echo 3 > /proc/sys/vm/drop_caches')
                        cached = checksum('/mnt/plain/c/data.bin')
                        direct = checksum('/mnt/plain/c/data.bin', direct=True)
                        assert cached == direct == sums['/mnt/plain/c/data.bin'] != state['oldC'], (cached, direct)
                        bad = [p for p, s in sums.items() if p.startswith('/mnt/plain/') and checksum(p) != s]
                        assert not bad, bad
                        errors = ext4_errors()
                        assert not errors, errors
                        record('c', boot6={'statusAtStart': status, 'savedCache': saved, 'readsChangedData': True,
                                           'statusAfterRead': dm_status('titanos-cache-plain')}, ok=True)

                    print(f'boot {boot} passed', flush=True)
                    call('/system/power', {'action': 'poweroff' if boot == boots[-1][0] else 'reboot'})
                    process.wait(timeout=180)
                except Exception as error:
                    results['failure'] = f'boot {boot}: {type(error).__name__}: {error}'
                    (output / 'cache-result.json').write_text(json.dumps(results, indent=2) + '\n')
                    print(f'QEMU process exit: {process.poll()}', flush=True)
                    if process.poll() is None:
                        log = subprocess.run([*ssh, 'sudo journalctl -b -u titand.service --no-pager -n 200; sudo dmesg | tail -n 80'],
                                             capture_output=True, text=True, timeout=30)
                        (output / f'cache-titand-boot{boot}.log').write_text(log.stdout + log.stderr)
                        if hold:
                            print(f'holding the VM for inspection: ssh {shlex.join(ssh)}; API {url}', flush=True)
                            process.wait()
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
    (output / 'cache-result.json').write_text(json.dumps(results, indent=2) + '\n')
    print('SSD cache smoke test passed', flush=True)


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument('zip', type=Path)
    parser.add_argument('--output', type=Path, default=Path('out/validation/cache'))
    parser.add_argument('--timeout', type=int, default=420)
    parser.add_argument('--tcg', action='store_true', help='force CPU emulation to reproduce CI without KVM')
    parser.add_argument('--hold', action='store_true', help='on failure keep the VM running for inspection (local debugging)')
    args = parser.parse_args()
    smoke(args.zip.resolve(), args.output.resolve(), args.timeout, args.tcg, args.hold)
