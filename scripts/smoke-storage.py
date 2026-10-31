#!/usr/bin/env python3
from files_usb_smoke import check_drive_format, check_usb_files
from smb_backup_smoke import check_smb_backup
"""Btrfs/optional-tool regression in disposable UEFI QEMU disks only.

Boots four times: create/snapshot, normal remount, degraded boot/replace,
remount after replacement. No host block devices are attached.
"""
import argparse
import hashlib
import http.cookiejar
import importlib.util
import json
import os
from pathlib import Path
import secrets
import shutil
import shlex
import ssl
import subprocess
import tempfile
import time
import urllib.error
import urllib.request
import zipfile

from cdi_smoke import check_cdi
from quota_smoke import check_share_quotas
from vm_snapshot_smoke import check_vm_snapshots

spec = importlib.util.spec_from_file_location('release_smoke', Path(__file__).with_name('smoke-release.py'))
helper = importlib.util.module_from_spec(spec)
spec.loader.exec_module(helper)

# Driver plugins whose modules load without hardware (PCI drivers register and
# wait for devices); it87-extended needs a Super I/O chip. The Realtek vendor
# drivers also run their hand-over service, which finds no RTL8125/8126 here.
DRIVER_MODULES = {'wangxun-nic': ('libwx', 'ngbe', 'txgbe'), 'intel-qat': ('qat_4xxx', 'qat_420xx'),
                  'r8125': ('r8125',), 'r8126': ('r8126',)}


def driver_plugins(plugins):
    return [name for name in DRIVER_MODULES if (plugins / (name + '.raw')).is_file()]


def check_smb_share(request, remote, smb_port, password, directory):
    # A new share with capitals and a blank, created without naming anyone:
    # whoever creates it must be able to use it over SMB right away.
    request('/shares', {'name': 'Fotos 2024', 'volume': 'mirror', 'smb': True})
    share = next(s for s in request('/shares') if s['name'] == 'Fotos 2024')
    assert share['access'] == {'storagetester': 'rw'}, share
    upload = directory / 'smb-upload.txt'
    upload.write_text('über SMB\n')
    env = dict(os.environ, PASSWD=password)
    command = ['smbclient', '//127.0.0.1/Fotos 2024', '-p', str(smb_port), '-U', 'storagetester', '-m', 'SMB3',
               '-c', f'put "{upload}" "Neue Datei.txt"; get "Neue Datei.txt" "{directory}/smb-back.txt"']
    deadline = time.monotonic() + 60
    while True:
        result = subprocess.run(command, env=env, capture_output=True, text=True, timeout=60)
        if result.returncode == 0:
            break
        if time.monotonic() > deadline:
            raise AssertionError(f'share not reachable over SMB: {result.stdout}{result.stderr}')
        time.sleep(2)
    assert (directory / 'smb-back.txt').read_text() == 'über SMB\n'
    assert remote('cat "/mnt/mirror/Fotos 2024/Neue Datei.txt"') == 'über SMB'
    # Exercise the server independently of Windows Explorer's path handling.
    long_name = 'a' * 251 + '.txt'
    deep = 'nested-' + 'b' * 33
    commands = [f'mkdir "{deep}"', f'cd "{deep}"'] * 8
    commands += [f'put "{upload}" "{long_name}"', f'get "{long_name}" "{directory}/smb-long-back.txt"']
    subprocess.run(['smbclient', '//127.0.0.1/Fotos 2024', '-p', str(smb_port), '-U', 'storagetester', '-m', 'SMB3', '-c', '; '.join(commands)], env=env, check=True, capture_output=True, text=True, timeout=90)
    assert (directory / 'smb-long-back.txt').read_bytes() == upload.read_bytes()
    print('SMB: writable share, 255-byte filename and nested path longer than 260 characters passed', flush=True)


def check_uploads(request, opener, url, remote, password):
    state = request('/system/memory')
    assert state['settings']['cachePercent'] == 20 and not state.get('error'), state
    state = request('/system/memory', {'cachePercent': 40}, 'PUT')
    assert state['settings']['cachePercent'] == 40 and not state.get('error'), state
    assert int(remote('cat /proc/sys/vm/dirty_bytes')) > 0
    assert remote('cat /proc/sys/vm/dirty_ratio') == '0'
    root = 'volume/mirror'
    content = b'upload-memory-check-' * (1024 * 1024)
    part = request('/files/uploads', {'root': root, 'dir': 'appdata', 'name': 'upload.bin', 'size': len(content)})
    def chunk(path, data, method='PATCH'):
        req = urllib.request.Request(url + '/api/v1' + path, data=data, method=method, headers={'Origin': url, 'Content-Type': 'application/octet-stream'})
        with opener.open(req, timeout=90) as response:
            body = response.read()
            return json.loads(body) if body else None
    received = 8 * 1024 * 1024
    status = chunk('/files/uploads/' + part['id'] + '?offset=0', content[:received])
    assert status['offset'] == received, status
    remote('systemctl restart titand.service')
    for attempt in range(45):
        try:
            if request('/health').get('ready'):
                break
        except OSError:
            pass
        time.sleep(1)
    request('/login', {'name': 'storagetester', 'password': password})
    parts = request('/files/uploads?root=volume%2Fmirror&path=appdata')
    assert len(parts) == 1 and parts[0]['offset'] == received, parts
    chunk('/files/uploads/' + part['id'] + '/verify?offset=0', content[:received], 'POST')
    for offset in range(received, len(content), 8 * 1024 * 1024):
        chunk('/files/uploads/' + part['id'] + '?offset=' + str(offset), content[offset:offset + 8 * 1024 * 1024])
    request('/files/uploads/' + part['id'] + '/finish', {})
    actual = remote('sha256sum /mnt/mirror/appdata/upload.bin').split()[0]
    assert actual == hashlib.sha256(content).hexdigest(), actual
    assert request('/files/uploads?root=volume%2Fmirror&path=appdata') == []
    remote('rm /mnt/mirror/appdata/upload.bin')


def smoke(archive, plugins, log, timeout, workflows=False, online_virtio=False, workflows_only=False, force_tcg=False):
    os.environ['PATH'] += ':/usr/sbin:/sbin'
    with tempfile.TemporaryDirectory(prefix='titanos-storage-', dir=log.parent) as tmp:
        directory = Path(tmp)
        stick = directory / 'stick'
        with zipfile.ZipFile(archive) as packed:
            for item in packed.infolist():
                if not (stick / item.filename).resolve().is_relative_to(stick):
                    raise ValueError('unsafe ZIP path')
                if item.filename.startswith('config/') and not item.is_dir():
                    raise ValueError('installation ZIP contains configuration')
            packed.extractall(stick)
        disk = directory / 'stick.img'
        with disk.open('wb') as f:
            f.truncate(4 * 1024**3)
        helper.command('sfdisk', '--quiet', '--label', 'dos', str(disk), input=b'start=1MiB, type=c, bootable\n')
        partition = str(disk) + '@@1M'
        helper.command('mformat', '-i', partition, '-F', '-v', 'TITANOS', '::')
        helper.command('mcopy', '-i', partition, '-s', *map(str, stick.iterdir()), '::')
        shutil.copyfile('/usr/share/OVMF/OVMF_VARS_4M.fd', directory / 'OVMF_VARS.fd')
        for index in (1, 2, 3):
            helper.command('qemu-img', 'create', '-q', '-f', 'qcow2', str(directory / f'data{index}.qcow2'), '8G' if workflows else '2G')
        for index in (1, 2):
            helper.command('qemu-img', 'create', '-q', '-f', 'qcow2', str(directory / f'quota{index}.qcow2'), '1G')
        helper.command('ssh-keygen', '-q', '-t', 'ed25519', '-N', '', '-f', str(directory / 'key'))
        usb_disk = directory / 'external-exfat.img'
        with usb_disk.open('wb') as image:
            image.truncate(128 * 1024**2)
        helper.command('mkfs.exfat', '-L', 'EXFAT-TEST', str(usb_disk))
        port, ssh_port, smb_port = helper.free_port(), helper.free_port(), helper.free_port()
        url = f'https://127.0.0.1:{port}'
        opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()),
                                            urllib.request.HTTPSHandler(context=ssl._create_unverified_context()))

        def request(path, data=None, method=None):
            req = urllib.request.Request(url + '/api/v1' + path,
                    data=json.dumps(data).encode() if data is not None else None,
                    headers={'Content-Type': 'application/json', 'Origin': url}, method=method)
            try:
                with opener.open(req, timeout=180) as response:
                    data = response.read()
                    return json.loads(data) if data else None
            except urllib.error.HTTPError as error:
                raise RuntimeError(f'{path}: {error.code} {error.read().decode()}') from error

        ssh_options = ['-i', str(directory / 'key'), '-o', 'IdentitiesOnly=yes', '-o', 'BatchMode=yes',
                       '-o', 'StrictHostKeyChecking=accept-new', '-o', f'UserKnownHostsFile={directory}/known_hosts', '-o', 'ConnectTimeout=3']
        ssh = ['ssh', *ssh_options, '-p', str(ssh_port), 'storagetester@127.0.0.1']

        def remote(command, timeout=180):
            return subprocess.check_output([*ssh, 'sudo sh -ec ' + "'" + command.replace("'", "'\\''") + "'"], text=True, timeout=timeout).strip()

        def put(source, destination):
            with open(source, 'rb') as stream:
                subprocess.run([*ssh, 'sudo sh -c ' + shlex.quote('cat > ' + shlex.quote(destination))], stdin=stream, check=True)

        def volume():
            return next(v for v in request('/storage')['volumes'] if v['name'] == 'mirror')

        accel = ['-accel', 'kvm', '-cpu', 'host'] if not force_tcg and os.access('/dev/kvm', os.W_OK) else ['-accel', 'tcg', '-cpu', 'max']
        base_args = ['qemu-system-x86_64', '-machine', 'q35', *accel, '-smp', '2', '-m', '4G',
            '-drive', 'if=pflash,format=raw,readonly=on,file=/usr/share/OVMF/OVMF_CODE_4M.fd',
            '-drive', f'if=pflash,format=raw,file={directory}/OVMF_VARS.fd',
            '-device', 'qemu-xhci', '-drive', f'if=none,id=stick,format=raw,file={disk}',
            '-device', 'usb-storage,drive=stick,removable=on,bootindex=0',
            '-drive', f'if=none,id=external,format=raw,file={usb_disk}',
            '-device', 'usb-storage,drive=external,removable=on',
            '-netdev', f'user,id=net0,hostfwd=tcp:127.0.0.1:{port}-:443,hostfwd=tcp:127.0.0.1:{ssh_port}-:22,hostfwd=tcp:127.0.0.1:{smb_port}-:445',
            '-device', 'virtio-net-pci,netdev=net0',
            '-smbios', 'type=11,value=io.systemd.boot.kernel-cmdline-extra=console=tty0 console=ttyS0,,115200',
            '-display', 'none', '-serial', 'stdio', '-monitor', 'none', '-no-reboot']
        if workflows:
            # Extra emulated Ethernet ports only; QEMU hubs never touch host NICs.
            for index, hub in ((1, 1), (2, 1), (3, 2), (4, 2)):
                base_args += ['-netdev', f'hubport,id=extra{index},hubid={hub}',
                              '-device', f'virtio-net-pci,netdev=extra{index},mac=52:54:00:23:00:0{index}']
            from workflows023_smoke import checks as feature_checks
        password = secrets.token_urlsafe(24)
        with log.open('wb') as serial:
            for boot in (1, 2, 3, 4):
                args = list(base_args)
                for index in ((1, 2, 3) if boot < 3 else (1, 3)):
                    args += ['-drive', f'if=none,id=data{index},format=qcow2,file={directory}/data{index}.qcow2',
                             '-device', f'virtio-blk-pci,drive=data{index},serial=BTRFS{index}']
                for index in (1, 2):
                    args += ['-drive', f'if=none,id=quota{index},format=qcow2,file={directory}/quota{index}.qcow2',
                             '-device', f'virtio-blk-pci,drive=quota{index},serial=QUOTA{index}']
                print(f'Btrfs boot {boot} ({accel[1]})', flush=True)
                process = subprocess.Popen(args, stdin=subprocess.DEVNULL, stdout=serial, stderr=subprocess.STDOUT)
                try:
                    deadline = time.monotonic() + timeout
                    while True:
                        if process.poll() is not None:
                            raise RuntimeError(f'VM stopped before health (QEMU exit {process.returncode})')
                        try:
                            if request('/health').get('ready'):
                                break
                        except (OSError, ValueError):
                            pass
                        if time.monotonic() > deadline:
                            raise TimeoutError('health')
                        time.sleep(3)
                    if boot == 1:
                        request('/setup', {'name': 'storagetester', 'password': password})
                        request('/account/ssh-keys', {'key': (directory / 'key.pub').read_text().strip(), 'password': password})
                    else:
                        request('/login', {'name': 'storagetester', 'password': password})
                    request('/system/ssh', {'enabled': True}, 'PUT')
                    deadline = time.monotonic() + 45
                    while subprocess.run([*ssh, 'true'], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL).returncode:
                        if time.monotonic() > deadline:
                            raise TimeoutError('SSH')
                        time.sleep(1)
                    disks = {d['serial']: d['name'] for d in request('/storage')['disks']}
                    if boot != 1:
                        check_share_quotas(boot, request, remote, disks)
                    if boot == 1:
                        members = [disks['BTRFS1'], disks['BTRFS2']]
                        # Exercise real foreign RAID release, only on these fresh
                        # virtual disks. A partial selection must change nothing.
                        assert all(name.startswith('vd') for name in members), members
                        remote('mdadm --create /dev/md/old --run --metadata=1.2 --level=1 --raid-devices=2 ' + ' '.join('/dev/' + name for name in members) + '; mkfs.ext4 -q /dev/md/old')
                        try:
                            request('/storage/volumes', {'name': 'unsafe', 'level': 'raid0', 'filesystem': 'ext4', 'disks': members[:1], 'erase': members[:1], 'identities': {d['name']: d['identity'] for d in request('/storage')['disks']}})
                        except RuntimeError as error:
                            assert 'disk_unavailable' in str(error), error
                        else:
                            raise AssertionError('accepted partial foreign RAID selection')
                        remote('test -b /dev/md/old; mdadm --detail /dev/md/old')
                        request('/storage/volumes', {'name': 'mirror', 'level': 'raid1', 'filesystem': 'btrfs', 'disks': members, 'erase': members, 'identities': {d['name']: d['identity'] for d in request('/storage')['disks']}})
                        assert volume()['mounted'], volume()
                        remote('test -d /mnt/mirror/vms && test -d /mnt/mirror/appdata && test -d /mnt/mirror/iso')
                        # After mirror: the first new volume gets the default folders.
                        check_share_quotas(boot, request, remote, disks)
                        profiles = remote('btrfs filesystem usage /mnt/mirror')
                        assert 'Data,RAID1' in profiles and 'Metadata,RAID1' in profiles, profiles
                        request('/vms', {'name': 'disk-test', 'displayName': 'Home Assistant Test', 'template': 'linux', 'memoryMiB': 512, 'vcpus': 1, 'disks': [{'format': 'qcow2', 'sizeBytes': 1024**3, 'volume': 'mirror'}]})
                        created = next(vm for vm in request('/vms')['vms'] if vm['name'] == 'disk-test')
                        assert created['displayName'] == 'Home Assistant Test', created
                        remote('mkdir "/mnt/mirror/vms/Home Assistant"; qemu-img create -q -f qcow2 "/mnt/mirror/vms/Home Assistant/ha.qcow2" 1G')
                        request('/vms', {'name': 'home-assistant', 'displayName': 'Home Assistant', 'directory': 'Home Assistant', 'volume': 'mirror', 'template': 'linux', 'memoryMiB': 512, 'vcpus': 1, 'disks': [{'path': '/mnt/mirror/vms/Home Assistant/ha.qcow2', 'format': 'qcow2'}]})
                        remote('test ! -e /mnt/mirror/vms/home-assistant')
                        if not workflows_only:
                            check_usb_files(request, opener, url)
                            check_drive_format(request, remote, [m if m.startswith('/dev/') else '/dev/' + m for m in members])
                            check_uploads(request, opener, url, remote, password)
                            check_smb_share(request, remote, smb_port, password, directory)
                            check_smb_backup(request, remote, password)
                            check_cdi(request, remote)
                            snapshot_vm, snapshot = check_vm_snapshots(request, remote, put, directory, volume='mirror')
                            for attempt in range(60):
                                try:
                                    if request('/health').get('ready'): break
                                except OSError: pass
                                time.sleep(1)
                            request('/login', {'name': 'storagetester', 'password': password})
                            assert len(request(f'/vms/{snapshot_vm}/snapshots')['snapshots']) == 1
                            request(f'/vms/{snapshot_vm}/poweroff', {})
                            request(f'/vms/{snapshot_vm}/snapshots/{snapshot["id"]}', {'confirm': snapshot['name']}, 'DELETE')
                            request(f'/vms/{snapshot_vm}', {'deleteDisks': True, 'confirm': snapshot_vm}, 'DELETE')
                            remote('rm -r /mnt/mirror/snapshot-tools')
                            print('VM RAM/disk/UEFI/TPM restore on Btrfs and metadata persistence passed', flush=True)
                            print('Foreign RAID reuse, upload durability/recovery, memory budget and direct VM folder passed', flush=True)
                        if workflows:
                            feature_checks(boot, request, remote, put, directory, opener, url, password, online_virtio)

                        request('/vms/disk-test', {'displayName': 'Home Assistant NAS', 'memoryMiB': 512, 'vcpus': 1, 'network': created['network']}, 'PUT')
                        memory = request('/system')['memory']
                        assert memory['usedBytes'] + memory['freeBytes'] == memory['totalBytes'], memory
                        assert 0 <= memory['availableBytes'] <= memory['totalBytes'], memory
                        assert 'C' in remote('lsattr -d /mnt/mirror/vms/disk-test').split()[0]
                        assert 'C' in remote('lsattr /mnt/mirror/vms/disk-test/vdisk1.qcow2').split()[0]
                        remote('mkdir /mnt/mirror/documents; printf before > /mnt/mirror/documents/file')
                        request('/storage/volumes/mirror/snapshots', {'action': 'create', 'name': 'before'})
                        assert remote('btrfs property get /mnt/mirror/.titanos/snapshots/before ro') == 'ro=true'
                        remote('printf after > /mnt/mirror/documents/file')
                        request('/storage/volumes/mirror/snapshots', {'action': 'restore', 'name': 'before'})
                        assert remote('cat /mnt/mirror/restored/before/documents/file') == 'before'
                        assert remote('cat /mnt/mirror/documents/file') == 'after'
                        if plugins:
                            remote('mkdir /mnt/mirror/plugin-input; chown storagetester /mnt/mirror/plugin-input')
                            for name in ('intel-gpu-tools', 'coral-usb', *driver_plugins(plugins)):
                                files = [plugins / (name + suffix) for suffix in ('.raw', '.plugin.json')]
                                signed = (plugins / (name + '.plugin.json.minisig')).is_file()
                                if signed:
                                    files.append(plugins / (name + '.plugin.json.minisig'))
                                helper.command('scp', *ssh_options, '-P', str(ssh_port), *map(str, files), 'storagetester@127.0.0.1:/mnt/mirror/plugin-input/')
                                request('/plugins', {'path': '/mnt/mirror/plugin-input/' + name + '.plugin.json', 'volume': 'mirror', 'confirm': '' if signed else name})
                    else:
                        assert volume()['mounted'], volume()
                        remote('test -d /mnt/mirror/vms && test -d /mnt/mirror/appdata && test -d /mnt/mirror/iso')
                        if workflows:
                            feature_checks(boot, request, remote, put, directory, opener, url, password, online_virtio)
                        renamed = next(vm for vm in request('/vms')['vms'] if vm['name'] == 'disk-test')
                        assert renamed['displayName'] == 'Home Assistant NAS', renamed
                        assert remote('cat /mnt/mirror/restored/before/documents/file') == 'before'
                        assert remote('cat /mnt/mirror/documents/file') == 'after'
                        assert request('/storage/volumes/mirror/snapshots') == [{'name': 'before'}]
                    if boot == 3:
                        assert volume()['state'] == 'degraded', volume()
                        print(remote('btrfs filesystem show --raw /mnt/mirror'), flush=True)
                        request('/storage/volumes/mirror/disks', {'disk': disks['BTRFS3'], 'erase': True, 'identity': next(d['identity'] for d in request('/storage')['disks'] if d['name'] == disks['BTRFS3'])})
                        deadline = time.monotonic() + 180
                        while True:
                            result = remote('btrfs replace status -1 /mnt/mirror')
                            if 'finished' in result:
                                break
                            if 'canceled' in result.lower():
                                raise AssertionError(result + '\n' + remote('dmesg | tail -n 35'))
                            if time.monotonic() > deadline:
                                raise TimeoutError(result)
                            time.sleep(2)
                    if boot == 4:
                        request('/storage/volumes/mirror/check', {'action': 'start'})
                        deadline = time.monotonic() + 90
                        while True:
                            # Right after the start btrfs may have no status
                            # yet and exits 1; that is no failure, just early.
                            try:
                                if 'finished' in remote('btrfs scrub status -R /mnt/mirror'):
                                    break
                            except subprocess.CalledProcessError:
                                pass
                            if time.monotonic() > deadline: raise TimeoutError('scrub')
                            time.sleep(2)
                        time.sleep(6)  # background status sampler observes the completed scrub
                        assert volume()['state'] == 'ok' and len(volume()['members']) == 2, volume()
                        request('/storage/volumes/mirror/snapshots', {'action': 'delete', 'name': 'before', 'confirm': 'before'})
                        assert request('/storage/volumes/mirror/snapshots') == []
                    if plugins:
                        # Plugins load once the volume is mounted (degraded Btrfs: after its
                        # wait); module plugins rebuild the RAM module index first.
                        deadline = time.monotonic() + 90
                        while True:
                            installed = {p['name']: p for p in request('/plugins')['plugins'] if p['installed']}
                            if all(installed[n]['active'] for n in ('intel-gpu-tools', 'coral-usb', *driver_plugins(plugins))):
                                break
                            if time.monotonic() > deadline:
                                raise AssertionError({n: (p['active'], p['problem']) for n, p in installed.items()})
                            time.sleep(2)
                        remote('for tool in /usr/bin/intel_gpu_top /usr/bin/intel_gpu_frequency; do test -x "$tool"; ! ldd "$tool" | grep -q "not found"; done; test -x /usr/bin/titanos-coral-info')
                        # Driver plugins load their modules without matching hardware, at every boot.
                        for name, modules in DRIVER_MODULES.items():
                            if name in driver_plugins(plugins):
                                remote(' && '.join(f'test -d /sys/module/{m}' for m in modules))
                        # No matching hardware in QEMU: no recommendation for it.
                        assert not any(r['plugin'] in DRIVER_MODULES for r in request('/plugins')['recommendations']), request('/plugins')['recommendations']
                    print(f'Btrfs boot {boot}: data, snapshots, membership and tools passed', flush=True)
                    request('/system/power', {'action': 'reboot'})
                    process.wait(timeout=90)
                except Exception:
                    # Report host pressure separately from a guest/service failure.
                    print(f'QEMU process exit: {process.poll()}', flush=True)
                    subprocess.run(['free', '-m'], check=False)
                    subprocess.run(['df', '-h', str(log.parent)], check=False)
                    subprocess.run([*ssh, 'sudo journalctl -b -u titand.service --no-pager -n 80'], timeout=15)
                    raise
                finally:
                    if process.poll() is None:
                        process.terminate()
                        try:
                            process.wait(timeout=10)
                        except subprocess.TimeoutExpired:
                            process.kill()
                            process.wait()
        print('Btrfs and tools smoke test passed', flush=True)


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('zip', type=Path)
    parser.add_argument('--plugins', type=Path)
    parser.add_argument('--log', type=Path, default=Path('out/storage-smoke.log'))
    parser.add_argument('--timeout', type=int, default=420)
    parser.add_argument('--tcg', action='store_true', help='force CPU emulation to reproduce CI without KVM')
    parser.add_argument('--workflows023', action='store_true')
    parser.add_argument('--workflows-only', action='store_true', help='focus the additional checks without repeating the independent USB/SMB/snapshot suites')
    parser.add_argument('--online-virtio', action='store_true', help='also download and validate the official stable driver ISO')
    args = parser.parse_args()
    args.log.parent.mkdir(parents=True, exist_ok=True)
    smoke(args.zip.resolve(), args.plugins.resolve() if args.plugins else None, args.log, args.timeout, args.workflows023, args.online_virtio, args.workflows_only, args.tcg)
