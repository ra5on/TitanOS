#!/usr/bin/env python3
"""Bridge API integration on fresh disposable UEFI media, never host networking.

Uses an existing installation ZIP and injects the current test binary via SSH
into the guest RAM on each boot. This is not a test of a newly packaged release.
"""
import argparse
import http.cookiejar
import hashlib
import json
import os
from pathlib import Path
import secrets
import shlex
import shutil
import socket
import ssl
import subprocess
import tempfile
import time
import urllib.error
import urllib.request
import zipfile

ROOT = Path(__file__).resolve().parents[1]


def free_port():
    with socket.socket() as sock:
        sock.bind(('127.0.0.1', 0))
        return sock.getsockname()[1]


def run(*args, **kwargs):
    return subprocess.run(args, check=True, stdout=subprocess.DEVNULL, **kwargs)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--zip', type=Path, required=True)
    parser.add_argument('--binary', type=Path, default=ROOT / 'backend/bin/titand')
    parser.add_argument('--output', type=Path, default=ROOT / 'out/validation/vm-bridge-qemu')
    parser.add_argument('--timeout', type=int, default=600)
    args = parser.parse_args()
    os.environ['PATH'] += ':/usr/sbin:/sbin'
    args.output.mkdir(parents=True, exist_ok=True)
    (args.output / 'results.json').unlink(missing_ok=True)
    binary_hash = hashlib.sha256(args.binary.read_bytes()).hexdigest()
    with tempfile.TemporaryDirectory(prefix='bridge-qemu-', dir=args.output) as tmp:
        folder = Path(tmp)
        stick = folder / 'stick'
        with zipfile.ZipFile(args.zip) as archive:
            for item in archive.infolist():
                if not (stick / item.filename).resolve().is_relative_to(stick):
                    raise ValueError('unsafe installation ZIP')
                if item.filename.startswith('config/') and not item.is_dir():
                    raise ValueError('ZIP contains user configuration')
            archive.extractall(stick)
        disk = folder / 'stick.img'
        with disk.open('wb') as output:
            output.truncate(4 * 1024**3)
        run('sfdisk', '--quiet', '--label', 'dos', str(disk), input=b'start=1MiB, type=c, bootable\n')
        fat = str(disk) + '@@1M'
        run('mformat', '-i', fat, '-F', '-v', 'TITANOS', '::')
        run('mcopy', '-i', fat, '-s', *map(str, stick.iterdir()), '::')
        shutil.copyfile('/usr/share/OVMF/OVMF_VARS_4M.fd', folder / 'vars.fd')
        run('ssh-keygen', '-q', '-t', 'ed25519', '-N', '', '-f', str(folder / 'ssh-key'))
        port, ssh_port = free_port(), free_port()
        url = f'https://127.0.0.1:{port}'
        opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()),
                    urllib.request.HTTPSHandler(context=ssl._create_unverified_context()))

        def request(path, data=None, method=None):
            req = urllib.request.Request(url + '/api/v1' + path,
                data=json.dumps(data).encode() if data is not None else None,
                headers={'Content-Type': 'application/json', 'Origin': url}, method=method)
            with opener.open(req, timeout=35) as response:
                body = response.read()
                return json.loads(body) if body else None

        def wait(check, seconds=None):
            deadline = time.monotonic() + (seconds or args.timeout)
            while True:
                try:
                    value = check()
                    if value:
                        return value
                except (OSError, ValueError, subprocess.CalledProcessError):
                    pass
                if time.monotonic() > deadline:
                    raise TimeoutError('guest check did not converge')
                time.sleep(2)

        ssh = ['ssh', '-i', str(folder / 'ssh-key'), '-p', str(ssh_port), '-o', 'BatchMode=yes',
               '-o', 'ConnectTimeout=3', '-o', 'StrictHostKeyChecking=accept-new',
               '-o', f'UserKnownHostsFile={folder}/known_hosts', 'bridgetest@127.0.0.1']

        def remote(script):
            return subprocess.check_output([*ssh, 'sudo sh -ec ' + shlex.quote(script)],
                                            text=True, stderr=subprocess.STDOUT, timeout=45).strip()

        def inject():
            with args.binary.open('rb') as binary:
                run(*ssh, 'cat > /tmp/titand-bridge-test', stdin=binary)
            remote('install -m755 /tmp/titand-bridge-test /usr/bin/titand.new; mv /usr/bin/titand.new /usr/bin/titand; systemctl restart titand')
            wait(lambda: request('/health').get('ready'))
            request('/login', {'name': 'bridgetest', 'password': password})

        qemu = ['qemu-system-x86_64', '-machine', 'q35', '-accel', 'tcg', '-cpu', 'max', '-smp', '2', '-m', '2G',
                '-drive', 'if=pflash,format=raw,readonly=on,file=/usr/share/OVMF/OVMF_CODE_4M.fd',
                '-drive', f'if=pflash,format=raw,file={folder}/vars.fd', '-device', 'qemu-xhci',
                '-drive', f'if=none,id=stick,format=raw,file={disk}',
                '-device', 'usb-storage,drive=stick,removable=on,bootindex=0',
                '-netdev', f'user,id=n0,hostfwd=tcp:127.0.0.1:{port}-:443,hostfwd=tcp:127.0.0.1:{ssh_port}-:22',
                '-device', 'virtio-net-pci,netdev=n0',
                '-smbios', 'type=11,value=io.systemd.boot.kernel-cmdline-extra=console=tty0 console=ttyS0,,115200',
                '-display', 'none', '-serial', 'stdio', '-monitor', 'none', '-no-reboot']
        password = secrets.token_urlsafe(24)
        results = []
        for boot in (1, 2):
            with (args.output / f'boot-{boot}.log').open('wb') as log:
                process = subprocess.Popen(qemu, stdin=subprocess.DEVNULL, stdout=log, stderr=subprocess.STDOUT)
                try:
                    def ready():
                        if process.poll() is not None:
                            raise RuntimeError('QEMU exited; see serial log')
                        return request('/health').get('ready')
                    wait(ready)
                    if boot == 1:
                        request('/setup', {'name': 'bridgetest', 'password': password})
                        request('/account/ssh-keys', {'key': (folder / 'ssh-key.pub').read_text().strip(), 'password': password})
                        request('/system/ssh', {'enabled': True}, 'PUT')
                    else:
                        request('/login', {'name': 'bridgetest', 'password': password})
                    wait(lambda: remote('true') == '')
                    inject()
                    state = request('/network')['network']
                    if boot == 1:
                        original = state['device']
                        for confirm in (False, True):
                            try:
                                request('/network/bridge', {'onlyUnconfigured': True})
                            except OSError:
                                pass  # Expected possible interruption while moving the address.
                            wait(lambda: (s := request('/network')['network'])['pending'] and s['device'] == 'br0' and s['addresses'])
                            assert not request('/network')['network']['bridgeReady']
                            assert not request('/vms')['bridge']
                            if not confirm:
                                wait(lambda: not (s := request('/network')['network'])['pending'] and not s['config']['bridge'] and s['device'] == original, 170)
                                remote('test ! -d /sys/class/net/br0')
                                results.append('unconfirmed bridge rolled back after two minutes')
                                print(results[-1], flush=True)
                            else:
                                request('/network/confirm', {})
                                assert request('/network')['network']['bridgeReady']
                                assert request('/vms')['bridge']
                        remote('ip netns add bridge-guest; ip link add bridge-tap type veth peer name eth0 netns bridge-guest; ip link set bridge-tap master br0; ip link set bridge-tap up; ip -n bridge-guest link set lo up; ip -n bridge-guest link set eth0 up')
                        remote("""cat > /root/bridge-dhcp-hook <<'HOOK'
#!/bin/sh
[ "$1" = bound ] || exit 0
ip address replace "$ip/24" dev "$interface"
ip route replace default via "$router" dev "$interface"
printf '%s' "$ip" > /run/bridge-guest-ip
HOOK
chmod 755 /root/bridge-dhcp-hook
ip netns exec bridge-guest busybox udhcpc -i eth0 -n -q -t 5 -T 2 -s /root/bridge-dhcp-hook
ip netns exec bridge-guest ping -c 1 -W 3 10.0.2.2
""")
                        before = remote('cat /run/bridge-guest-ip')
                        request('/network/bridge', {})
                        remote('systemctl restart titand')
                        wait(lambda: request('/health').get('ready'))
                        request('/login', {'name': 'bridgetest', 'password': password})
                        assert not request('/network')['network']['pending']
                        assert remote('cat /run/bridge-guest-ip') == before
                        remote('test -d /sys/class/net/br0/brif/bridge-tap; ip netns exec bridge-guest ping -c 1 -W 3 10.0.2.2; ip link delete bridge-tap; ip netns delete bridge-guest')
                        results.append('confirmed bridge, guest DHCP, connectivity and service restart passed')
                    else:
                        assert state['configured'] and state['config']['bridge'] and state['device'] == 'br0', state
                        member = state['config']['ports'][0]
                        assert remote('cat /sys/class/net/br0/address') == remote(f'cat /sys/class/net/{shlex.quote(member)}/address')
                        assert not any(a['family'] == 'inet' for a in json.loads(remote(f'ip -j address show dev {shlex.quote(member)}'))[0]['addr_info'])
                        results.append('bridge persisted after NAS reboot; MAC and address ownership verified')
                    print(results[-1], flush=True)
                    remote('systemctl poweroff')
                    process.wait(timeout=60)
                except Exception:
                    try:
                        (args.output / f'failure-{boot}.log').write_text(remote('ip -brief address; bridge link; journalctl -b -u titand -u systemd-networkd --no-pager -n 100'))
                    except Exception:
                        pass
                    raise
                finally:
                    if process.poll() is None:
                        process.terminate()
                        try:
                            process.wait(timeout=10)
                        except subprocess.TimeoutExpired:
                            process.kill()
                            process.wait()
        (args.output / 'results.json').write_text(json.dumps({'checks': results, 'baseZip': str(args.zip), 'binary': str(args.binary), 'binarySHA256': binary_hash, 'scope': 'injected current binary, fresh disposable USB, TCG; no real hardware or newly packaged release'}, indent=2) + '\n')


if __name__ == '__main__':
    main()
