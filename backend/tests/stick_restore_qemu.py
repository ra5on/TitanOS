#!/usr/bin/env python3
"""Replace a dead stick: restore /config from the backup on the data volume.

Disposable UEFI QEMU media only. For each case:
  1. boot stick A, set up an owner, a RAID1 volume, a share and a host name,
     and wait for the automatic configuration backup on the volume;
  2. power off and throw stick A away;
  3. boot a fresh stick B, written from the same ZIP, with the same data
     disks (optionally one RAID1 member missing): the setup page must find
     the backup, and restoring it restarts the NAS;
  4. boot stick B again: the old owner logs in with the old password, the
     volume is mounted, share and host name are back, and no update result
     is reported that never happened.

Usage: backend/tests/stick_restore_qemu.py out/titanos-0.28.0.zip [--case btrfs|ext4|ext4-degraded] [--tcg]
"""
import argparse
import http.cookiejar
import importlib.util
import json
import os
import secrets
import shutil
import ssl
import subprocess
import tempfile
import time
import urllib.error
import urllib.request
import zipfile
from pathlib import Path

spec = importlib.util.spec_from_file_location('release_smoke', Path(__file__).resolve().parents[2] / 'scripts' / 'smoke-release.py')
helper = importlib.util.module_from_spec(spec)
spec.loader.exec_module(helper)

CASES = {
    # name: (file system, members present after the stick is replaced)
    'btrfs': ('btrfs', (1, 2)),
    'ext4': ('ext4', (1, 2)),
    'ext4-degraded': ('ext4', (1,)),
}


def make_stick(archive, directory, name):
    stick = directory / (name + '-files')
    with zipfile.ZipFile(archive) as packed:
        for item in packed.infolist():
            if not (stick / item.filename).resolve().is_relative_to(stick.resolve()):
                raise ValueError('unsafe ZIP path')
            if item.filename.startswith('config/') and not item.is_dir():
                raise ValueError('installation ZIP contains configuration')
        packed.extractall(stick)
    disk = directory / (name + '.img')
    with disk.open('wb') as f:
        f.truncate(4 * 1024**3)
    helper.command('sfdisk', '--quiet', '--label', 'dos', str(disk), input=b'start=1MiB, type=c, bootable\n')
    partition = str(disk) + '@@1M'
    helper.command('mformat', '-i', partition, '-F', '-v', 'TITANOS', '::')
    helper.command('mcopy', '-i', partition, '-s', *map(str, stick.iterdir()), '::')
    shutil.rmtree(stick)
    return disk


def run_case(archive, case, log, timeout, force_tcg):
    filesystem, survivors = CASES[case]
    os.environ['PATH'] += ':/usr/sbin:/sbin'
    report = {'case': case, 'filesystem': filesystem, 'membersAfterReplacement': len(survivors), 'steps': []}
    with tempfile.TemporaryDirectory(prefix='titanos-stick-', dir=log.parent) as tmp:
        directory = Path(tmp)
        stick_a = make_stick(archive, directory, 'stick-a')
        for index in (1, 2):
            helper.command('qemu-img', 'create', '-q', '-f', 'qcow2', str(directory / f'data{index}.qcow2'), '2G')
        port = helper.free_port()
        url = f'https://127.0.0.1:{port}'
        jar = http.cookiejar.CookieJar()
        opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(jar),
                                            urllib.request.HTTPSHandler(context=ssl._create_unverified_context()))

        def request(path, data=None, method=None, raw=False):
            req = urllib.request.Request(url + '/api/v1' + path,
                    data=json.dumps(data).encode() if data is not None else None,
                    headers={'Content-Type': 'application/json', 'Origin': url}, method=method)
            try:
                with opener.open(req, timeout=180) as response:
                    body = response.read()
                    return json.loads(body) if body and not raw else body
            except urllib.error.HTTPError as error:
                raise RuntimeError(f'{path}: {error.code} {error.read().decode()}') from error

        accel = ['-accel', 'kvm', '-cpu', 'host'] if not force_tcg and os.access('/dev/kvm', os.W_OK) else ['-accel', 'tcg', '-cpu', 'max']

        def boot(stick, members, label):
            shutil.copyfile('/usr/share/OVMF/OVMF_VARS_4M.fd', directory / 'OVMF_VARS.fd')
            args = ['qemu-system-x86_64', '-machine', 'q35', *accel, '-smp', '2', '-m', '3G',
                    '-drive', 'if=pflash,format=raw,readonly=on,file=/usr/share/OVMF/OVMF_CODE_4M.fd',
                    '-drive', f'if=pflash,format=raw,file={directory}/OVMF_VARS.fd',
                    '-device', 'qemu-xhci', '-drive', f'if=none,id=stick,format=raw,file={stick}',
                    '-device', 'usb-storage,drive=stick,removable=on,bootindex=0',
                    '-netdev', f'user,id=net0,hostfwd=tcp:127.0.0.1:{port}-:443', '-device', 'virtio-net-pci,netdev=net0',
                    '-smbios', 'type=11,value=io.systemd.boot.kernel-cmdline-extra=console=tty0 console=ttyS0,,115200',
                    '-display', 'none', '-serial', 'stdio', '-monitor', 'none', '-no-reboot']
            for index in members:
                args += ['-drive', f'if=none,id=data{index},format=qcow2,file={directory}/data{index}.qcow2',
                         '-device', f'virtio-blk-pci,drive=data{index},serial=RESTORE{index}']
            jar.clear()
            print(f'{case}: {label} ({accel[1]})', flush=True)
            serial = log.open('ab')
            process = subprocess.Popen(args, stdin=subprocess.DEVNULL, stdout=serial, stderr=subprocess.STDOUT)
            deadline = time.monotonic() + timeout
            while True:
                if process.poll() is not None:
                    raise RuntimeError(f'{label}: VM stopped before health (QEMU exit {process.returncode})')
                try:
                    if request('/health').get('ready'):
                        return process
                except (OSError, ValueError, RuntimeError):
                    pass
                if time.monotonic() > deadline:
                    process.kill()
                    raise TimeoutError(f'{label}: health')
                time.sleep(3)

        def wait_exit(process, label):
            try:
                process.wait(timeout=timeout)
            except subprocess.TimeoutExpired:
                process.kill()
                raise TimeoutError(f'{label}: the NAS did not go down')

        def step(text):
            print(f'{case}: {text}', flush=True)
            report['steps'].append(text)

        password = secrets.token_urlsafe(24)
        hostname = 'restore-' + case.split('-')[0]

        # 1. Stick A: set the NAS up and wait for the backup on the volume.
        process = boot(stick_a, (1, 2), 'stick A, first start')
        try:
            request('/setup', {'name': 'restoretester', 'password': password})
            disks = {d['serial']: d['name'] for d in request('/storage')['disks']}
            members = [disks['RESTORE1'], disks['RESTORE2']]
            request('/storage/volumes', {'name': 'tank', 'level': 'raid1', 'filesystem': filesystem, 'disks': members, 'erase': members, 'identities': {d['name']: d['identity'] for d in request('/storage')['disks']}})
            volume = next(v for v in request('/storage')['volumes'] if v['name'] == 'tank')
            assert volume['mounted'], volume
            request('/shares', {'name': 'Daten', 'volume': 'tank', 'smb': True})
            request('/system/hostname', {'hostname': hostname}, 'PUT')
            step(f'set up: owner, RAID1 {filesystem} volume, share and host name')
            if filesystem == 'ext4':
                # The mirror is new; a degraded start later needs it in sync.
                deadline = time.monotonic() + timeout
                while next(v for v in request('/storage')['volumes'] if v['name'] == 'tank').get('state') not in ('ok', 'healthy', 'clean'):
                    if time.monotonic() > deadline:
                        raise TimeoutError('RAID1 resync')
                    time.sleep(5)
            request('/config-backup/run', {})
            state = request('/config-backup')['backup']
            assert state['backups'] and state['backups'][0]['volume'] == 'tank', state
            newest = state['backups'][0]
            step(f'configuration backup on the volume: {newest["name"]} (version {newest["version"]})')
            request('/system/power', {'action': 'poweroff'})
        except BaseException:
            process.kill()
            raise
        wait_exit(process, 'stick A, power off')
        stick_a.unlink()
        step('stick A powered off and removed')

        # 2. Stick B, fresh from the same ZIP: find and restore the backup.
        stick_b = make_stick(archive, directory, 'stick-b')
        process = boot(stick_b, survivors, f'fresh stick B with {len(survivors)} of 2 disks')
        try:
            deadline = time.monotonic() + 240
            while True:
                found = request('/setup/backups')['backups']
                if found:
                    break
                if time.monotonic() > deadline:
                    raise AssertionError('the fresh stick found no backup on the disks')
                time.sleep(10)
            assert any(b['name'] == newest['name'] for b in found), found
            chosen = next(b for b in found if b['name'] == newest['name'])
            step(f'fresh stick found {len(found)} backup(s) on {chosen["device"]} (label {chosen.get("label")})')
            request('/setup/restore', {'device': chosen['device'], 'name': chosen['name']})
            step('restore accepted, the NAS restarts')
        except BaseException:
            process.kill()
            raise
        wait_exit(process, 'stick B, restart after restore')

        # 3. Stick B again: everything is back.
        process = boot(stick_b, survivors, 'stick B after the restore')
        try:
            try:
                request('/setup', {'name': 'intruder', 'password': secrets.token_urlsafe(24)})
            except RuntimeError as error:
                assert '409' in str(error), error
            else:
                raise AssertionError('setup still open after the restore')
            request('/login', {'name': 'restoretester', 'password': password})
            step('old owner logs in with the old password')
            deadline = time.monotonic() + 180
            while True:
                volume = next((v for v in request('/storage')['volumes'] if v['name'] == 'tank'), None)
                if volume and volume['mounted']:
                    break
                if time.monotonic() > deadline:
                    raise AssertionError(f'volume not mounted after the restore: {volume}')
                time.sleep(5)
            step(f'volume tank mounted, state {volume.get("state")}')
            shares = [s['name'] for s in request('/shares')]
            assert 'Daten' in shares, shares
            info = request('/system')
            assert info.get('hostname') == hostname, info.get('hostname')
            step('share and host name restored')
            kinds = [m.get('kind') for m in request('/notifications')['messages']]
            assert not any(k in ('update_installed', 'update_failed', 'update_rolled_back') for k in kinds), kinds
            step(f'no update result reported; notifications: {sorted(set(k for k in kinds if k))}')
            state = request('/config-backup')['backup']
            assert any(b['name'] == newest['name'] for b in state['backups']), state
            step('the old backups are still listed on the volume')
            request('/system/power', {'action': 'poweroff'})
        except BaseException:
            process.kill()
            raise
        wait_exit(process, 'stick B, power off')
    report['result'] = 'passed'
    return report


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('archive', type=Path)
    parser.add_argument('--case', choices=sorted(CASES), action='append')
    parser.add_argument('--log', type=Path, default=Path('out/stick-restore-serial.log'))
    parser.add_argument('--report', type=Path)
    parser.add_argument('--timeout', type=int, default=900)
    parser.add_argument('--tcg', action='store_true')
    args = parser.parse_args()
    args.log.parent.mkdir(parents=True, exist_ok=True)
    args.log.write_bytes(b'')
    reports = []
    for case in args.case or sorted(CASES):
        reports.append(run_case(args.archive, case, args.log, args.timeout, args.tcg))
    if args.report:
        args.report.write_text(json.dumps({'archive': args.archive.name, 'cases': reports}, indent=2, ensure_ascii=False) + '\n')
    print('stick restore: all cases passed', flush=True)


if __name__ == '__main__':
    main()
