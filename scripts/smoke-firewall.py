#!/usr/bin/env python3
"""Firewall (nftables table inet titanos) in a disposable UEFI QEMU guest.

Boots the stick ZIP twice with user-mode networking and no data disks; the
host reaches the guest through hostfwd from 10.0.2.2 (zone home). A second
client lives in a network namespace inside the guest (veth, 192.168.77.2,
also home) so that it can be blocked without cutting off the test itself.

  a) image: nft, cryptsetup and wg (wireguard-tools) present; firewall off
     by default, no table, titanos-firewall-open ACCEPT rules first;
  b) switch on via the API: trial (pending), web UI still reachable from the
     host, confirm; table loaded, SSH and web UI from the host still work;
  c) a blocked service (SMB from home) on trial: the namespace client times
     out on 445 (dropped, not refused), the host's hostfwd to 445 gets no
     answer, the drop shows in the recent list; revert brings it back;
     a second trial without confirmation reverts by itself after two
     minutes (notice firewall_reverted);
  d) auto block: N wrong web logins from the namespace block it (web UI and
     ping dropped there), the host still works, unblock releases it;
  e) reboot: the firewall comes back on with the table (boot 2);
  f) switch off: table gone, titanos-firewall-open rules still first, port
     445 refused again instead of dropped.

Results go to <output>/firewall-result.json.
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
import socket
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

NS = 'fwt'
NAS = '192.168.77.1'
CLIENT = '192.168.77.2'


def smoke(archive, output, timeout, force_tcg=False):
    os.environ['PATH'] += ':/usr/sbin:/sbin'
    output.mkdir(parents=True, exist_ok=True)
    results = {'started': time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime()), 'zip': archive.name,
               'zipSha256': hashlib.sha256(archive.read_bytes()).hexdigest(), 'phases': {}}

    def record(phase, **evidence):
        results['phases'].setdefault(phase, {}).update(evidence)
        (output / 'firewall-result.json').write_text(json.dumps(results, indent=2) + '\n')
        print(f'[{phase}]', json.dumps(evidence)[:600], flush=True)

    with tempfile.TemporaryDirectory(prefix='titanos-firewall-', dir=output) as tmp:
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
        helper.command('ssh-keygen', '-q', '-t', 'ed25519', '-N', '', '-f', str(directory / 'key'))
        port, ssh_port, smb_port = helper.free_port(), helper.free_port(), helper.free_port()
        url = f'https://127.0.0.1:{port}'
        opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()),
                                             urllib.request.HTTPSHandler(context=ssl._create_unverified_context()))

        def call(path, data=None, method=None, timeout=60):
            req = urllib.request.Request(url + '/api/v1' + path,
                                         data=json.dumps(data).encode() if data is not None else None,
                                         headers={'Content-Type': 'application/json', 'Origin': url}, method=method)
            try:
                with opener.open(req, timeout=timeout) as response:
                    return response.status, response.read()
            except urllib.error.HTTPError as error:
                return error.code, error.read()

        def request(path, data=None, method=None, timeout=60):
            status, body = call(path, data, method, timeout)
            if status >= 400:
                raise RuntimeError(f'{method or ("POST" if data is not None else "GET")} {path}: {status} {body.decode(errors="replace")}')
            return json.loads(body) if body else None

        ssh_options = ['-F', '/dev/null', '-i', str(directory / 'key'), '-o', 'IdentitiesOnly=yes', '-o', 'BatchMode=yes',
                       '-o', 'StrictHostKeyChecking=accept-new', '-o', f'UserKnownHostsFile={directory}/known_hosts', '-o', 'ConnectTimeout=5']
        ssh = ['ssh', *ssh_options, '-p', str(ssh_port), 'fwtester@127.0.0.1']

        def remote(command, timeout=120, check=True):
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

        def tcp_from_ns(target_port, seconds=4):
            """open, refused or dropped (timeout) for a TCP connect from the namespace."""
            # remote() runs sh -e: keep the exit code instead of stopping there.
            result = remote(f'rc=0; ip netns exec {NS} timeout {seconds} bash -c "</dev/tcp/{NAS}/{target_port}" || rc=$?; echo rc=$rc', check=False)
            rc = result.stdout.strip().rsplit('rc=', 1)[-1]
            return {'0': 'open', '124': 'dropped'}.get(rc, 'refused')

        def tcp_from_host(target_port, seconds=6):
            """What the host sees through hostfwd: answer, closed (refused in the guest) or silence (dropped)."""
            with socket.create_connection(('127.0.0.1', target_port), timeout=seconds) as s:
                s.settimeout(seconds)
                try:
                    s.sendall(b'\x00' * 4)
                    data = s.recv(16)
                    return 'answer' if data else 'closed'
                except ConnectionResetError:
                    return 'closed'
                except socket.timeout:
                    return 'silence'

        def table():
            return remote('nft list table inet titanos 2>&1 || echo NO-TABLE')

        def firewall():
            return request('/firewall')

        # Settings version 2: zones × destinations as cells with policies.
        def nas_cell(cfg, zone):
            return next(c for c in cfg['cells'] if c['from'] == zone and c['to'] == 'nas')

        def smb_home(cfg):
            return any(p.get('service') == 'smb' and p['action'] == 'allow' and not p.get('disabled')
                       for p in nas_cell(cfg, 'home')['policies'])

        def health_from_host():
            return request('/health', timeout=10).get('ready')

        accel = ['-accel', 'kvm', '-cpu', 'host'] if not force_tcg and os.access('/dev/kvm', os.W_OK) else ['-accel', 'tcg', '-cpu', 'max']
        results['accel'] = accel[1]
        qemu = ['qemu-system-x86_64', '-machine', 'q35', *accel, '-smp', '2', '-m', '3G',
                '-drive', 'if=pflash,format=raw,readonly=on,file=/usr/share/OVMF/OVMF_CODE_4M.fd',
                '-drive', f'if=pflash,format=raw,file={directory}/OVMF_VARS.fd',
                '-device', 'qemu-xhci', '-drive', f'if=none,id=stick,format=raw,file={disk}',
                '-device', 'usb-storage,drive=stick,removable=on,bootindex=0',
                '-netdev', f'user,id=net0,hostfwd=tcp:127.0.0.1:{port}-:443,hostfwd=tcp:127.0.0.1:{ssh_port}-:22,hostfwd=tcp:127.0.0.1:{smb_port}-:445',
                '-device', 'virtio-net-pci,netdev=net0',
                '-smbios', 'type=11,value=io.systemd.boot.kernel-cmdline-extra=console=tty0 console=ttyS0,,115200',
                '-display', 'none', '-serial', 'stdio', '-monitor', 'none', '-no-reboot']
        account = secrets.token_urlsafe(18)

        def namespace():
            remote(f'ip netns add {NS}; ip link add fwt0 type veth peer name fwt1; ip link set fwt1 netns {NS}; '
                   f'ip addr add {NAS}/24 dev fwt0; ip link set fwt0 up; '
                   f'ip netns exec {NS} ip addr add {CLIENT}/24 dev fwt1; ip netns exec {NS} ip link set fwt1 up; '
                   f'ip netns exec {NS} ip link set lo up')

        def ns_web():
            r = remote(f'ip netns exec {NS} wget -q -T 5 -t 1 -O- --no-check-certificate https://{NAS}/api/v1/health || true', check=False)
            return 'ready' in r.stdout

        def ns_login(password):
            body = json.dumps({'name': 'fwtester', 'password': password})
            r = remote(f'rc=0; ip netns exec {NS} wget -q -T 5 -t 1 -O- --no-check-certificate '
                       f"--header 'Content-Type: application/json' --header 'Origin: https://{NAS}' "
                       f'--post-data {shlex.quote(body)} https://{NAS}/api/v1/login || rc=$?; echo rc=$rc', check=False)
            return r.stdout.strip().rsplit('rc=', 1)[-1]

        with (output / 'firewall-serial.log').open('wb') as serial:
            for boot in ('1', '2'):
                print(f'boot {boot} ({accel[1]})', flush=True)
                process = subprocess.Popen(qemu, stdin=subprocess.DEVNULL, stdout=serial, stderr=subprocess.STDOUT)
                try:
                    def ready():
                        if process.poll() is not None:
                            raise SystemExit(f'QEMU stopped before health (exit {process.returncode})')
                        answer = request('/health')
                        return answer if answer.get('ready') else None
                    wait(ready, 'health', timeout, 3)
                    if boot == '1':
                        request('/setup', {'name': 'fwtester', 'password': account})
                        request('/account/ssh-keys', {'key': (directory / 'key.pub').read_text().strip()})
                    else:
                        request('/login', {'name': 'fwtester', 'password': account})
                    request('/system/ssh', {'enabled': True}, 'PUT')
                    wait(lambda: subprocess.run([*ssh, 'true'], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL).returncode == 0, 'SSH', 60, 1)
                    namespace()

                    if boot == '1':
                        # a) image and defaults.
                        tools = remote('for t in nft cryptsetup wg iptables; do printf "%s=%s\\n" $t "$(command -v $t || echo missing)"; done; '
                                       'wg --version; nft --version; cryptsetup --version')
                        status = firewall()
                        iptables = remote('iptables -S INPUT | sed -n 2p; ip6tables -S INPUT | sed -n 2p; systemctl is-active titanos-firewall-open.service')
                        off_table = table()
                        assert 'missing' not in tools, tools
                        assert not status['config']['enabled'] and not status['active'], status
                        assert 'NO-TABLE' in off_table or 'No such file' in off_table, off_table
                        assert iptables.count('titanos-firewall-open') == 2 and iptables.endswith('active'), iptables
                        assert tcp_from_ns(445) == 'refused', 'SMB without shares refuses, not drops'
                        record('a', tools=tools.splitlines(), defaultEnabled=False, tableWhileOff=False,
                               openRules=iptables.splitlines(), port445FromNamespace='refused', ok=True)

                        # b) switch on with trial, confirm.
                        cfg = status['config']
                        cfg['enabled'] = True
                        on_trial = request('/firewall', cfg, 'PUT')
                        assert on_trial['pending'] and on_trial['pendingSeconds'] > 0, on_trial
                        reachable_on_trial = health_from_host()
                        trial_table = table()
                        assert 'admin on trial' in trial_table and '10.0.2.2' in trial_table, trial_table
                        confirmed = request('/firewall/confirm', {})
                        assert not confirmed['pending'] and confirmed['config']['enabled'] and confirmed['active'], confirmed
                        on_table = table()
                        assert 'admin on trial' not in on_table and 'comment "webui"' in on_table, on_table
                        ssh_ok = subprocess.run([*ssh, 'true']).returncode == 0
                        assert health_from_host() and ssh_ok and ns_web()
                        record('b', trialPending=on_trial['pendingSeconds'], webUIOnTrial=bool(reachable_on_trial),
                               adminOnTrialRule=True, confirmed=True, webUIAfterConfirm=True, sshAfterConfirm=ssh_ok,
                               namespaceWebUI=True, clientZone=confirmed.get('clientZone'), ok=True)

                        # c) SMB from home blocked on trial: dropped, recent list, revert; then automatic revert.
                        blocked = json.loads(json.dumps(confirmed['config']))
                        for cell in blocked['cells']:
                            if cell['to'] == 'nas':
                                cell['policies'] = [p for p in cell['policies'] if p.get('service') != 'smb']
                        request('/firewall/recent', method='DELETE')
                        request('/firewall', blocked, 'PUT')
                        ns_445 = tcp_from_ns(445)
                        # The host is the saving address and free while on trial; it is checked after confirming below.
                        drops = firewall()['recent']
                        assert ns_445 == 'dropped', ns_445
                        assert any(d.get('port') == 445 and d.get('source') == CLIENT for d in drops), drops
                        reverted = request('/firewall/revert', {})
                        assert not reverted['pending'] and smb_home(reverted['config']), reverted
                        after_revert = tcp_from_ns(445)
                        assert after_revert == 'refused', after_revert
                        # Confirmed block: the host's hostfwd now gets no answer either.
                        request('/firewall', blocked, 'PUT')
                        request('/firewall/confirm', {})
                        host_445 = tcp_from_host(smb_port)
                        assert host_445 == 'silence', host_445
                        restored = request('/firewall', confirmed['config'], 'PUT')
                        request('/firewall/confirm', {})
                        host_445_open = tcp_from_host(smb_port)
                        assert host_445_open == 'closed', host_445_open
                        # Automatic revert after two minutes without confirmation.
                        request('/firewall', blocked, 'PUT')
                        assert tcp_from_ns(445) == 'dropped'
                        started = time.monotonic()
                        wait(lambda: not firewall()['pending'], 'automatic revert', 200, 5)
                        auto_seconds = round(time.monotonic() - started, 1)
                        notices = request('/notifications')
                        kinds = [n.get('kind') for n in notices.get('messages') or []]
                        assert smb_home(firewall()['config']) and tcp_from_ns(445) == 'refused'
                        record('c', namespace445OnTrial=ns_445, droppedInRecentList=True, revert=True, afterRevert=after_revert,
                               host445Blocked=host_445, host445Restored=host_445_open, autoRevertSeconds=auto_seconds,
                               firewallRevertedNotice='firewall_reverted' in kinds, ok=True)

                        # d) automatic blocking after wrong web logins from the namespace.
                        attempts = 3
                        request('/firewall/autoblock', {'enabled': True, 'attempts': attempts, 'within': 5, 'minutes': 30,
                                                        'permanent': False, 'exemptLocal': False, 'exempt': []}, 'PUT')
                        codes = [ns_login('wrong-' + secrets.token_hex(4)) for _ in range(attempts)]
                        status = wait(lambda: (lambda s: s if s['blocked'] else None)(firewall()), 'block', 30, 1)
                        block = status['blocked'][0]
                        ns_web_blocked = ns_web()
                        ns_ping = remote(f'rc=0; ip netns exec {NS} ping -c1 -W2 {NAS} >/dev/null 2>&1 || rc=$?; echo $rc', check=False).stdout.strip()
                        blocked_table = table()
                        assert block['address'] == CLIENT and block['source'] == 'web', block
                        assert not ns_web_blocked and ns_ping != '0' and CLIENT in blocked_table
                        assert health_from_host(), 'the host is not blocked'
                        request('/firewall/unblock', {'address': block['address']})
                        wait(ns_web, 'namespace back after unblock', 30, 1)
                        assert not firewall()['blocked']
                        request('/firewall/autoblock', {'enabled': False, 'attempts': 10, 'within': 5, 'minutes': 60,
                                                        'permanent': False, 'exemptLocal': True, 'exempt': []}, 'PUT')
                        record('d', wrongLogins=attempts, loginExitCodes=codes, blocked=block, namespaceWebUIWhileBlocked=ns_web_blocked,
                               namespacePingWhileBlocked=ns_ping == '0', hostStillReachable=True, unblocked=True, ok=True)
                    else:
                        # e) after the reboot the firewall is on again.
                        status = firewall()
                        boot_table = table()
                        assert status['config']['enabled'] and status['active'] and 'comment "webui"' in boot_table, status
                        assert health_from_host() and ns_web()
                        record('e', enabledAfterReboot=True, tableAfterReboot=True, webUI=True, ok=True)

                        # f) switch off: no table, the open rules stay first.
                        cfg = status['config']
                        cfg['enabled'] = False
                        off = request('/firewall', cfg, 'PUT')
                        if off.get('pending'):
                            request('/firewall/confirm', {})
                        off_table = table()
                        iptables = remote('iptables -S INPUT | sed -n 2p; ip6tables -S INPUT | sed -n 2p')
                        assert 'NO-TABLE' in off_table or 'No such file' in off_table, off_table
                        assert iptables.count('titanos-firewall-open') == 2, iptables
                        assert tcp_from_ns(445) == 'refused'
                        record('f', pendingWhenSwitchingOff=bool(off.get('pending')), tableAfterOff=False,
                               openRules=iptables.splitlines(), port445='refused', ok=True)

                    print(f'boot {boot} passed', flush=True)
                    call('/system/power', {'action': 'poweroff' if boot == '2' else 'reboot'})
                    process.wait(timeout=120)
                except Exception as error:
                    results['failure'] = f'boot {boot}: {type(error).__name__}: {error}'
                    (output / 'firewall-result.json').write_text(json.dumps(results, indent=2) + '\n')
                    log = subprocess.run([*ssh, 'sudo journalctl -b -u titand.service --no-pager -n 150'], capture_output=True, text=True, timeout=30)
                    (output / f'firewall-titand-boot{boot}.log').write_text(log.stdout + log.stderr)
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
    (output / 'firewall-result.json').write_text(json.dumps(results, indent=2) + '\n')
    print('firewall smoke test passed', flush=True)


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument('zip', type=Path)
    parser.add_argument('--output', type=Path, default=Path('out/validation/firewall'))
    parser.add_argument('--timeout', type=int, default=420)
    parser.add_argument('--tcg', action='store_true', help='force CPU emulation to reproduce CI without KVM')
    args = parser.parse_args()
    smoke(args.zip.resolve(), args.output.resolve(), args.timeout, args.tcg)
