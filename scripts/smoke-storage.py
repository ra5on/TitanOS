#!/usr/bin/env python3
"""Btrfs/optional-tool regression in disposable UEFI QEMU disks only.

Boots four times: create/snapshot, normal remount, degraded boot/replace,
remount after replacement. No host block devices are attached.
"""
import argparse
import http.cookiejar
import importlib.util
import json
import os
from pathlib import Path
import secrets
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


def smoke(archive, plugins, log, timeout):
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
            helper.command('qemu-img', 'create', '-q', '-f', 'qcow2', str(directory / f'data{index}.qcow2'), '2G')
        helper.command('ssh-keygen', '-q', '-t', 'ed25519', '-N', '', '-f', str(directory / 'key'))
        port, ssh_port = helper.free_port(), helper.free_port()
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

        def remote(command):
            return subprocess.check_output([*ssh, 'sudo sh -ec ' + "'" + command.replace("'", "'\\''") + "'"], text=True, timeout=90).strip()

        def volume():
            return next(v for v in request('/storage')['volumes'] if v['name'] == 'mirror')

        accel = ['-accel', 'kvm', '-cpu', 'host'] if os.access('/dev/kvm', os.W_OK) else ['-accel', 'tcg', '-cpu', 'max']
        base_args = ['qemu-system-x86_64', '-machine', 'q35', *accel, '-smp', '2', '-m', '4G',
            '-drive', 'if=pflash,format=raw,readonly=on,file=/usr/share/OVMF/OVMF_CODE_4M.fd',
            '-drive', f'if=pflash,format=raw,file={directory}/OVMF_VARS.fd',
            '-device', 'qemu-xhci', '-drive', f'if=none,id=stick,format=raw,file={disk}',
            '-device', 'usb-storage,drive=stick,removable=on,bootindex=0',
            '-netdev', f'user,id=net0,hostfwd=tcp:127.0.0.1:{port}-:443,hostfwd=tcp:127.0.0.1:{ssh_port}-:22',
            '-device', 'virtio-net-pci,netdev=net0',
            '-smbios', 'type=11,value=io.systemd.boot.kernel-cmdline-extra=console=tty0 console=ttyS0,,115200',
            '-display', 'none', '-serial', 'stdio', '-monitor', 'none', '-no-reboot']
        password = secrets.token_urlsafe(24)
        with log.open('wb') as serial:
            for boot in (1, 2, 3, 4):
                args = list(base_args)
                for index in ((1, 2, 3) if boot < 3 else (1, 3)):
                    args += ['-drive', f'if=none,id=data{index},format=qcow2,file={directory}/data{index}.qcow2',
                             '-device', f'virtio-blk-pci,drive=data{index},serial=BTRFS{index}']
                print(f'Btrfs boot {boot} ({accel[1]})', flush=True)
                process = subprocess.Popen(args, stdin=subprocess.DEVNULL, stdout=serial, stderr=subprocess.STDOUT)
                try:
                    deadline = time.monotonic() + timeout
                    while True:
                        if process.poll() is not None:
                            raise RuntimeError('VM stopped before health')
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
                        request('/account/ssh-keys', {'key': (directory / 'key.pub').read_text().strip()})
                    else:
                        request('/login', {'name': 'storagetester', 'password': password})
                    request('/system/ssh', {'enabled': True}, 'PUT')
                    deadline = time.monotonic() + 45
                    while subprocess.run([*ssh, 'true'], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL).returncode:
                        if time.monotonic() > deadline:
                            raise TimeoutError('SSH')
                        time.sleep(1)
                    disks = {d['serial']: d['name'] for d in request('/storage')['disks']}
                    if boot == 1:
                        members = [disks['BTRFS1'], disks['BTRFS2']]
                        request('/storage/volumes', {'name': 'mirror', 'level': 'raid1', 'filesystem': 'btrfs', 'disks': members, 'erase': members})
                        assert volume()['mounted'], volume()
                        profiles = remote('btrfs filesystem usage /mnt/mirror')
                        assert 'Data,RAID1' in profiles and 'Metadata,RAID1' in profiles, profiles
                        request('/vms', {'name': 'disk-test', 'template': 'linux', 'memoryMiB': 512, 'vcpus': 1, 'disks': [{'format': 'qcow2', 'sizeBytes': 1024**3, 'volume': 'mirror'}]})
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
                            for name in ('intel-gpu-tools', 'coral-usb'):
                                files = [plugins / (name + suffix) for suffix in ('.raw', '.plugin.json')]
                                signed = (plugins / (name + '.plugin.json.minisig')).is_file()
                                if signed:
                                    files.append(plugins / (name + '.plugin.json.minisig'))
                                helper.command('scp', *ssh_options, '-P', str(ssh_port), *map(str, files), 'storagetester@127.0.0.1:/mnt/mirror/plugin-input/')
                                request('/plugins', {'path': '/mnt/mirror/plugin-input/' + name + '.plugin.json', 'volume': 'mirror', 'confirm': '' if signed else name})
                    else:
                        assert volume()['mounted'], volume()
                        assert remote('cat /mnt/mirror/restored/before/documents/file') == 'before'
                        assert remote('cat /mnt/mirror/documents/file') == 'after'
                        assert request('/storage/volumes/mirror/snapshots') == [{'name': 'before'}]
                    if boot == 3:
                        assert volume()['state'] == 'degraded', volume()
                        print(remote('btrfs filesystem show --raw /mnt/mirror'), flush=True)
                        request('/storage/volumes/mirror/disks', {'disk': disks['BTRFS3'], 'erase': True})
                        deadline = time.monotonic() + 180
                        while True:
                            result = remote('btrfs replace status -1 /mnt/mirror')
                            if 'finished' in result:
                                break
                            if time.monotonic() > deadline:
                                raise TimeoutError(result)
                            time.sleep(2)
                    if boot == 4:
                        request('/storage/volumes/mirror/check', {'action': 'start'})
                        deadline = time.monotonic() + 90
                        while 'finished' not in remote('btrfs scrub status -R /mnt/mirror'):
                            if time.monotonic() > deadline: raise TimeoutError('scrub')
                            time.sleep(2)
                        time.sleep(6)  # background status sampler observes the completed scrub
                        assert volume()['state'] == 'ok' and len(volume()['members']) == 2, volume()
                        request('/storage/volumes/mirror/snapshots', {'action': 'delete', 'name': 'before', 'confirm': 'before'})
                        assert request('/storage/volumes/mirror/snapshots') == []
                    if plugins:
                        installed = {p['name']: p for p in request('/plugins')['plugins'] if p['installed']}
                        assert all(installed[n]['active'] for n in ('intel-gpu-tools', 'coral-usb')), installed
                        remote('for tool in /usr/bin/intel_gpu_top /usr/bin/intel_gpu_frequency; do test -x "$tool"; ! ldd "$tool" | grep -q "not found"; done; test -x /usr/bin/titanos-coral-info')
                    print(f'Btrfs boot {boot}: data, snapshots, membership and tools passed', flush=True)
                    request('/system/power', {'action': 'reboot'})
                    process.wait(timeout=90)
                except Exception:
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
    args = parser.parse_args()
    args.log.parent.mkdir(parents=True, exist_ok=True)
    smoke(args.zip.resolve(), args.plugins.resolve() if args.plugins else None, args.log, args.timeout)
