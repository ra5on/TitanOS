#!/usr/bin/env python3
"""Full A/B lifecycle in an explicitly disposable QEMU overlay, never the IMG.

The candidate is served locally only to this QEMU guest. The GitHub transport
is replaced inside a test-only Python process; RAUC, signatures, disk writes,
health checks, NAS state and rollback/reboot APIs are real production code.
No fixtures, private keys or test accounts are written into the released IMG.
"""
import argparse
import base64
import hashlib
import http.server
import importlib.util
import json
import os
import re
from pathlib import Path
import shutil
import socket
import subprocess
import tempfile
import threading
import time

ROOT=Path(__file__).resolve().parents[1]
spec=importlib.util.spec_from_file_location('runtime_smoke', ROOT/'scripts/smoke-runtime.py')
runtime=importlib.util.module_from_spec(spec);spec.loader.exec_module(runtime)


def command(args, **kwargs):
    return subprocess.run(args, check=True, **kwargs)


def sha(path):
    with Path(path).open('rb') as stream:return hashlib.file_digest(stream,'sha256').hexdigest()


def published_baseline_metadata(kind, version, image_sha256, bundle_sha256=None):
    if kind not in ('published-image', 'published-system') or not isinstance(version, str) or not re.fullmatch(r'[0-9]+\.[0-9]+\.[0-9]+(?:-(?:alpha|beta)\.[0-9]+)?', version):
        raise ValueError('Invalid published baseline identity.')
    if not isinstance(image_sha256, str) or not re.fullmatch(r'[a-f0-9]{64}', image_sha256):
        raise ValueError('Invalid published baseline image hash.')
    result = {'baseline_source': 'published-system-release' if kind == 'published-system' else 'published-release',
              'baseline_version': version, 'baseline_sha256': image_sha256}
    if kind == 'published-system':
        if not isinstance(bundle_sha256, str) or not re.fullmatch(r'[a-f0-9]{64}', bundle_sha256):
            raise ValueError('Invalid published baseline bundle hash.')
        result['baseline_bundle_sha256'] = bundle_sha256
    elif bundle_sha256 is not None:
        raise ValueError('Image baseline must not claim a bundle hash.')
    return result


class Agent:
    def __init__(self, path):self.path=str(path)
    def call(self, method, arguments=None):
        with socket.socket(socket.AF_UNIX) as sock:
            sock.settimeout(15);sock.connect(self.path)
            sock.sendall(json.dumps({'execute':method,'arguments':arguments or {}}).encode()+b'\n')
            with sock.makefile('rb') as stream:
                for _ in range(20):
                    line=stream.readline(1024*1024)
                    if not line:break
                    value=json.loads(line.lstrip(b'\xff'))
                    if 'error' in value:raise RuntimeError('Guest agent rejected '+method)
                    if 'return' in value:return value['return']
        raise RuntimeError('No bounded guest agent response')
    def execute(self, args, timeout=180):
        value=self.call('guest-exec',{'path':args[0],'arg':args[1:],'capture-output':True})
        deadline=time.monotonic()+timeout
        while time.monotonic()<deadline:
            status=self.call('guest-exec-status',{'pid':value['pid']})
            if status.get('exited'):
                out=base64.b64decode(status.get('out-data','')).decode(errors='replace')
                err=base64.b64decode(status.get('err-data','')).decode(errors='replace')
                if status.get('exitcode')!=0:
                    # Commands never contain credentials; diagnostics stay bounded.
                    raise RuntimeError('Guest command failed: '+err[-4000:])
                return out
            time.sleep(1)
        raise RuntimeError('Guest command timed out')
    def python(self, code, timeout=180):
        return self.execute(['/usr/bin/python3','-c',code], timeout)
    def ready(self, slot, version, previous_boot=None, require_health=True):
        deadline=time.monotonic()+360
        while time.monotonic()<deadline:
            try:
                boot=self.execute(['/bin/cat','/proc/sys/kernel/random/boot_id'],timeout=10).strip()
                if previous_boot==boot:
                    time.sleep(2);continue
                status=json.loads(self.python("import sys,json;sys.path.insert(0,'/usr/lib/titan');from titan.debian_updates import system_status;print(json.dumps(system_status()))"))
                if (not require_health or status['health_confirmed']) and status['booted']['slot']==slot and status['current']==version:
                    return boot,status
            except (OSError,ValueError,RuntimeError):pass
            time.sleep(3)
        raise RuntimeError('Expected healthy slot/version did not start: '+slot+' '+version)


def launch_guest(work, accel, memory_mb=8192):
    if memory_mb not in (3072,8192) or accel not in ('kvm','tcg'):
        raise ValueError('Invalid disposable guest resource plan')
    for name in ('qmp.sock','qga.sock'):
        path=work/name
        if path.exists():
            if not path.is_socket():raise RuntimeError('Disposable guest socket was replaced')
            path.unlink()
    with (work/'console.log').open('ab') as console:
        return subprocess.Popen(['qemu-system-x86_64','-accel',accel,'-machine','q35','-cpu','host' if accel=='kvm' else 'max',
            '-m',str(memory_mb),'-smp','2','-display','none','-monitor','none','-serial','stdio',
            '-qmp','unix:'+str(work/'qmp.sock')+',server=on,wait=off',
            '-drive','if=pflash,format=raw,readonly=on,file=/usr/share/OVMF/OVMF_CODE_4M.fd',
            '-drive','if=pflash,format=raw,file='+str(work/'vars.fd'),
            '-drive','id=titan-system,if=virtio,format=qcow2,file='+str(work/'test.qcow2'),
            '-device','virtio-serial-pci','-chardev','socket,path='+str(work/'qga.sock')+',server=on,wait=off,id=qga',
            '-device','virtserialport,chardev=qga,name=org.qemu.guest_agent.0',
            '-netdev','user,id=net0,hostfwd=tcp:127.0.0.1:15000-:5000,hostfwd=tcp:127.0.0.1:15445-:445',
            '-device','virtio-net-pci,netdev=net0'],stdout=console,stderr=console)


# This tiny private image needs no registry and is never part of the IMG/bundle.
# Copy only the trusted guest's sleep executable and its bounded ELF runtime.
GUEST_BOOT_MEMORY_SETUP=r"""
import io,json,re,subprocess,tarfile,time
from pathlib import Path
name='titan-ci-boot-memory';image='titan-ci-boot-memory:local'
if subprocess.check_output(['docker','ps','-aq','--filter','name=^/'+name+'$'],text=True).strip():
    raise RuntimeError('Disposable boot-memory container already exists')
ldd=subprocess.check_output(['/usr/bin/ldd','/bin/sleep'],text=True,timeout=15)
paths={'/bin/sleep'}|set(re.findall(r'(?:^|\s)(/[^\s()]+)',ldd))
if len(paths)>16 or any(len(path)>512 for path in paths):raise RuntimeError('Invalid bounded ELF runtime')
archive=io.BytesIO();directories=set();total=0
with tarfile.open(fileobj=archive,mode='w') as output:
    for path in sorted(paths):
        source=Path(path);size=source.stat().st_size
        if not source.is_file() or not 0<size<=8*1024**2:raise RuntimeError('Invalid runtime file')
        total+=size
        if total>16*1024**2:raise RuntimeError('Private runtime exceeds memory bound')
        for parent in reversed(source.parents):
            target=str(parent).lstrip('/')
            if target and target not in directories:
                node=tarfile.TarInfo(target);node.type=tarfile.DIRTYPE;node.mode=0o755
                output.addfile(node);directories.add(target)
        node=tarfile.TarInfo(path.lstrip('/'));node.size=size;node.mode=0o755
        with source.open('rb') as stream:output.addfile(node,stream)
subprocess.run(['docker','import','-',image],input=archive.getvalue(),stdout=subprocess.DEVNULL,check=True,timeout=60)
identifier=subprocess.check_output(['docker','create','--name',name,'--restart','always','--memory','4g',
    '--memory-swap','4g','--network','none',image,'/bin/sleep','86400'],text=True,timeout=30).strip()
if not re.fullmatch('[a-f0-9]{64}',identifier):raise RuntimeError('Invalid private container identity')
subprocess.run(['docker','start',identifier],stdout=subprocess.DEVNULL,check=True,timeout=30)
time.sleep(11)
subprocess.run(['docker','stop','--time','5',identifier],stdout=subprocess.DEVNULL,check=True,timeout=15)
value=json.loads(subprocess.check_output(['docker','inspect',identifier],text=True,timeout=15))[0]
if (value['State']['Running'] is not False or value['HostConfig']['Memory']!=4*1024**3 or
    value['HostConfig']['MemorySwap']!=4*1024**3 or value['HostConfig']['RestartPolicy']['Name']!='always'):
    raise RuntimeError('Stopped test container does not provide the boot commitment')
print('prepared')
"""


GUEST_BOOT_MEMORY_OBSERVE=r"""
import json,os,re,stat,subprocess
fd=os.open('/run/titan-boot-memory.json',os.O_RDONLY|os.O_NOFOLLOW)
with os.fdopen(fd,'rb') as stream:
    info=os.fstat(stream.fileno())
    if not stat.S_ISREG(info.st_mode) or info.st_uid!=0 or info.st_mode&0o022 or info.st_size>16384:
        raise RuntimeError('Invalid boot-memory report')
    report=json.loads(stream.read(16385))
fields=('ok','reason','total_bytes','reserve_bytes','required_bytes')
value={'report':{key:report[key] for key in fields},'services':{}}
for unit in ('docker.service','libvirtd.service'):
    text=subprocess.check_output(['systemctl','show',unit,'--property=ActiveState,Result,ExecStartPre'],text=True,timeout=15)
    rows=dict(line.split('=',1) for line in text.splitlines() if '=' in line)
    pre=rows.get('ExecStartPre','')
    guard_failed=False
    if len(pre)<=16384:
        for entry in re.findall(r'\{([^{}]*)\}',pre):
            command=dict(part.strip().split('=',1) for part in entry.split(';') if '=' in part)
            guard_failed=guard_failed or (command.get('path')=='/usr/bin/python3' and
                command.get('argv[]')=='/usr/bin/python3 /usr/share/titan/boot-memory-guard.py --component all' and
                command.get('ignore_errors')=='no' and command.get('code')=='exited' and command.get('status')=='1')
    value['services'][unit]={'active':rows.get('ActiveState')=='active',
        'failed':rows.get('ActiveState')=='failed' and rows.get('Result') in ('exit-code','start-limit-hit'),
        'guard_failed':guard_failed}
print(json.dumps(value,separators=(',',':')))
"""


BOOT_MEMORY_FIELDS={'local_test_image_created','stopped_always_container_persisted','reduced_physical_ram_verified',
    'docker_start_blocked','libvirt_start_blocked','insufficient_memory_report_verified','web_status_usable',
    'file_manager_usable','physical_ram_restored_verified','daemon_autostarts_recovered','test_container_image_removed'}


def boot_memory_lifecycle(agent, client, restart, slot, version, previous_boot):
    """Real 8 -> 3 -> 8 GiB cold boots; never simulate the guest's MemTotal."""
    initial=client.request('/api/status',timeout=8)
    if initial.get('demo') is not False or not 7*1024**3<=initial.get('memory_total',0)<=8*1024**3:
        raise RuntimeError('Boot-memory lifecycle must begin in the real eight-GiB guest')
    if agent.python(GUEST_BOOT_MEMORY_SETUP,timeout=120).strip()!='prepared':
        raise RuntimeError('Private boot-memory test container was not created')
    restart(3072)
    reduced,status=agent.ready(slot,version,previous_boot,require_health=False)
    deadline=time.monotonic()+90
    while True:
        try:report=blocked_boot_observation(json.loads(agent.python(GUEST_BOOT_MEMORY_OBSERVE,timeout=45)))
        except (OSError,ValueError,RuntimeError):report=None
        if report is not None:break
        if time.monotonic()>=deadline:
            raise RuntimeError('Actual reduced-RAM boot did not block both daemons through the RAM guard')
        time.sleep(2)
    session=client.request('/api/session',timeout=8)
    status=client.request('/api/status',timeout=8)
    listing=client.request('/api/files?share=%40system&path=var%2Fsrv%2Ftitan%2Fshares%2Fab-persist',timeout=8)
    if (session.get('user',{}).get('role')!='admin' or status.get('demo') is not False or
        status.get('memory_total')!=report['total_bytes'] or not isinstance(listing.get('entries'),list) or
        not any(item.get('name')=='rollback-sentinel' for item in listing['entries'])):
        raise RuntimeError('Web session, real RAM metrics or file manager failed while daemon starts were blocked')
    restart(8192)
    restored,status=agent.ready(slot,version,reduced)
    current=client.request('/api/status',timeout=8)
    if current.get('demo') is not False or not 7*1024**3<=current.get('memory_total',0)<=8*1024**3:
        raise RuntimeError('Disposable guest physical RAM was not restored')
    components=client.request('/api/components',timeout=45).get('components',{})
    if any(components.get(name,{}).get('daemon') is not True for name in ('docker','vms')):
        raise RuntimeError('Docker/libvirt did not recover after physical RAM restoration')
    recovered=agent.python("import json,subprocess;v=json.loads(subprocess.check_output(['docker','inspect','titan-ci-boot-memory'],text=True))[0];assert v['State']['Running'] is True and v['HostConfig']['Memory']==4*1024**3 and v['HostConfig']['RestartPolicy']['Name']=='always';print('recovered')",timeout=30)
    if recovered.strip()!='recovered':raise RuntimeError('Actual daemon autostart did not recover')
    agent.execute(['/usr/bin/docker','rm','--force','titan-ci-boot-memory'],timeout=30)
    agent.execute(['/usr/bin/docker','image','rm','titan-ci-boot-memory:local'],timeout=30)
    cleanup=agent.python("import subprocess;assert not subprocess.check_output(['docker','ps','-aq','--filter','name=^/titan-ci-boot-memory$'],text=True).strip();assert not subprocess.check_output(['docker','images','-q','titan-ci-boot-memory:local'],text=True).strip();print('clean')",timeout=30)
    if cleanup.strip()!='clean':raise RuntimeError('Private boot-memory test artifacts were not removed')
    return restored,{key:True for key in sorted(BOOT_MEMORY_FIELDS)}


def blocked_boot_observation(value):
    if not isinstance(value,dict):return None
    report=value.get('report')
    if (not isinstance(report,dict) or set(report)!={'ok','reason','total_bytes','reserve_bytes','required_bytes'} or
        report['ok'] is not False or report['reason']!='insufficient_memory' or
        any(type(report[key]) is not int for key in ('total_bytes','reserve_bytes','required_bytes')) or
        not 2*1024**3<report['total_bytes']<=3*1024**3 or not 0<report['reserve_bytes']<report['total_bytes'] or
        report['required_bytes']!=4*1024**3+report['reserve_bytes'] or
        not isinstance(value.get('services'),dict)):return None
    for unit in ('docker.service','libvirtd.service'):
        service=value['services'].get(unit)
        if (not isinstance(service,dict) or set(service)!={'active','failed','guard_failed'} or
                service['active'] is not False or service['failed'] is not True or service['guard_failed'] is not True):return None
    return report


def qmp(path, method, arguments=None):
    with socket.socket(socket.AF_UNIX) as sock:
        sock.settimeout(10);sock.connect(str(path))
        with sock.makefile('rb') as stream:
            assert 'QMP' in json.loads(stream.readline())
            for request in ({'execute':'qmp_capabilities'}, {'execute':method,'arguments':arguments or {}}):
                sock.sendall(json.dumps(request).encode()+b'\n')
                for _ in range(30):
                    value=json.loads(stream.readline())
                    if 'error' in value:raise RuntimeError('QMP command failed')
                    if 'return' in value:break
                else:raise RuntimeError('QMP response missing')


def main():
    parser=argparse.ArgumentParser();parser.add_argument('image',type=Path);parser.add_argument('bundle',type=Path)
    parser.add_argument('--baseline-image',type=Path)
    parser.add_argument('--baseline-version',default='0.4.6-alpha.1')
    parser.add_argument('--baseline-kind', choices=('published-image', 'published-system'), default='published-image')
    parser.add_argument('--baseline-bundle-sha256')
    parser.add_argument('--confirm-disposable-guest',action='store_true');args=parser.parse_args()
    if args.baseline_kind == 'published-system':
        if not args.baseline_image or not isinstance(args.baseline_bundle_sha256, str) or not re.fullmatch(r'[a-f0-9]{64}', args.baseline_bundle_sha256):
            parser.error('A published-system baseline requires its prepared image and verified bundle SHA256.')
    elif args.baseline_bundle_sha256 is not None:
        parser.error('A baseline bundle SHA256 is only valid for a published-system baseline.')
    assert os.environ.get('GITHUB_ACTIONS')=='true' and args.confirm_disposable_guest
    image=args.image.resolve();bundle=args.bundle.resolve()
    assert image.is_file() and bundle.is_file()
    original=sha(image);original_bundle=sha(bundle);directory=image.parent
    baseline_image=args.baseline_image.resolve() if args.baseline_image else image
    baseline_hash=sha(baseline_image)
    identity=json.loads((directory/'ab-input/image-info.json').read_text())
    report={'format':'titan-debian-ab-smoke-v1','ok':False,'checks':[]}
    def passed(name):report['checks'].append(name);print(name+': passed',flush=True)
    proc=None;server=None
    with tempfile.TemporaryDirectory(prefix='titan-ab-smoke-') as temporary:
        work=Path(temporary);agent=Agent(work/'qga.sock')
        try:
            command(['qemu-img','create','-q','-f','qcow2','-F','raw','-b',str(baseline_image),str(work/'test.qcow2')])
            if args.baseline_image:
                baseline=args.baseline_version
                report.update(published_baseline_metadata(args.baseline_kind, baseline, baseline_hash, args.baseline_bundle_sha256))
            else:
                baseline='2.9.99' if os.environ.get('TITAN_INITIAL_RELEASE') == 'true' else '0.4.5-alpha.1'
                report.update(baseline_source='fresh-install-bootstrap', baseline_version=baseline, baseline_sha256=baseline_hash)
                command(['python3','scripts/system-release-metadata.py','identity','--version',baseline,'--accounts',str(directory/'ab-input/system-accounts.json'),'--output',str(work/'image-info.json')])
                # Signed older system identity only in the private overlay; complete
                # root filesystem replacement then proves A -> B -> A transitions.
                command(['guestfish','-a',str(work/'test.qcow2'),'-m','/dev/sda3','upload',str(work/'image-info.json'),'/usr/share/titan/image-info.json'])
                command(['guestfish','-a',str(work/'test.qcow2'),'-m','/dev/sda3','upload',str(work/'image-info.json.sig'),'/usr/share/titan/image-info.json.sig'])
            (work/'factory-probe').write_text('baseline factory default\n')
            command(['guestfish','-a',str(work/'test.qcow2'),'-m','/dev/sda3','upload',str(work/'factory-probe'),'/etc/titan-ci-factory'])
            shutil.copyfile('/usr/share/OVMF/OVMF_VARS_4M.fd',work/'vars.fd')
            accel='kvm' if os.access('/dev/kvm',os.R_OK|os.W_OK) else 'tcg'
            proc=launch_guest(work,accel)
            def restart(memory_mb):
                nonlocal proc
                try:agent.execute(['/usr/bin/systemctl','poweroff','--no-block'],timeout=20)
                except (OSError,RuntimeError):pass
                # A cold restart only follows a clean, fully completed shutdown.
                # Killing a guest here would hide metadata persistence failures.
                proc.wait(timeout=90)
                proc=launch_guest(work,accel,memory_mb)
            boot,status=agent.ready('A',baseline);passed('baseline_boot_health')
            if args.baseline_image:passed('published_release_baseline')
            else:passed('fresh_install_bootstrap_baseline')
            client=runtime.GuestClient(work/'qmp.sock');smoke=runtime.RuntimeSmoke(client);smoke.setup()
            client.action('share_create',{'name':'ab-persist','readers':[smoke.username],'writers':[smoke.username]})
            share=next(item for item in client.request('/api/shares') if item['name']=='ab-persist')
            assert share['path']=='/var/srv/titan/shares/ab-persist'
            agent.python("from pathlib import Path;p=Path('/var/srv/titan/shares/ab-persist/rollback-sentinel');p.write_text('preserved across A/B update and rollback\\n')")
            agent.python("from pathlib import Path;assert Path('/etc/titan-ci-factory').read_text()=='baseline factory default\\n';Path('/etc/titan-ci-local').write_text('persistent local change\\n')")
            snapshot_code="""import hashlib,json,subprocess
from pathlib import Path
items={p:hashlib.sha256(Path(p).read_bytes()).hexdigest() for p in ['/etc/passwd','/etc/shadow','/etc/group','/etc/samba/titan-shares.conf','/etc/titan-ci-local','/var/srv/titan/shares/ab-persist/rollback-sentinel']}
items['acl']=subprocess.check_output(['getfacl','-p','/var/srv/titan/shares/ab-persist'],text=True)
print(json.dumps(items,sort_keys=True))"""
            snapshot=agent.python(snapshot_code)
            # Grow this named private overlay while running, then exercise the
            # actual installed grow helper and check newly usable data capacity.
            before=int(agent.execute(['/usr/bin/stat','-f','-c','%b','/var/lib/titan-system']).strip())
            qmp(work/'qmp.sock','block_resize',{'device':'titan-system','size':64*1024**3})
            agent.python("""import subprocess,time
from pathlib import Path
for _ in range(30):
    if int(Path('/sys/class/block/vda/size').read_text()) >= 64*1024**3//512:break
    time.sleep(1)
else:raise RuntimeError('VirtIO disk size did not refresh')
""")
            agent.execute(['/bin/bash','/usr/share/titan/debian-grow-data.sh'])
            after=int(agent.execute(['/usr/bin/stat','-f','-c','%b','/var/lib/titan-system']).strip())
            assert after>before+3*1024**3//4096
            passed('proxmox_style_data_growth')
            class Handler(http.server.BaseHTTPRequestHandler):
                def do_GET(self):
                    if self.path!='/candidate.raucb':self.send_error(404);return
                    self.send_response(200);self.send_header('Content-Length',str(bundle.stat().st_size));self.end_headers()
                    with bundle.open('rb') as source:shutil.copyfileobj(source,self.wfile)
                def log_message(self,*unused):pass
            server=http.server.ThreadingHTTPServer(('127.0.0.1',0),Handler)
            threading.Thread(target=server.serve_forever,daemon=True).start()
            agent.execute(['/usr/bin/curl','--fail','--silent','--show-error','--max-time','600',
                           'http://10.0.2.2:'+str(server.server_port)+'/candidate.raucb','-o','/var/lib/titan-system/candidate.raucb'],timeout=660)
            assert agent.execute(['/usr/bin/sha256sum','/var/lib/titan-system/candidate.raucb']).split()[0]==sha(bundle)
            manifest={**identity,'bundle':{'name':bundle.name,'size':bundle.stat().st_size,'sha256':sha(bundle)},
                      'rootfs_sha256':sha(directory/'bundle/rootfs.ext4'),
                      **{key:'passed' for key in ('boot_test','runtime_test','update_test','rollback_test')}}
            # Local unpublished test offer. This injects transport only; all
            # production install checks and RAUC signature/verity checks run.
            install_code="""import sys,json,shutil
from pathlib import Path
from unittest.mock import patch
sys.path.insert(0,'/usr/lib/titan')
from titan import debian_updates as u
manifest=json.loads(MANIFEST)
assets={manifest['bundle']['name']:'https://api.github.com/test-bundle','manifest.json':'https://api.github.com/test-manifest','manifest.json.sig':'https://api.github.com/test-signature'}
offer={'available':True,'latest':manifest['version'],'latest_stage':manifest['release_stage'],'assets':assets,'url':'https://github.com/ra5on/TitanOS/releases/test'}
with patch.object(u,'check',return_value=offer),patch('titan.updates.verified_release',return_value=(manifest,assets)),patch.object(u,'download',side_effect=lambda url,path,bundle,token:shutil.copyfile('/var/lib/titan-system/candidate.raucb',path)):
    result=u.install('ra5on/TitanOS','alpha',manifest['version'],Path('/var/lib/titan/titan.sqlite3'))
assert result['reboot_required'] is True and result['automatic_reboot'] is False
print('prepared')
""".replace('MANIFEST',repr(json.dumps(manifest)))
            agent.python(install_code,timeout=2100)
            staged=client.request('/api/updates/system');assert staged['next_boot']['slot']=='B' and staged['booted']['slot']=='A'
            assert agent.execute(['/bin/cat','/proc/sys/kernel/random/boot_id']).strip()==boot
            passed('signed_update_staged_without_reboot')
            client.action('system_reboot',{'expected_digest':identity['release_id'],'confirmation':'NEUSTART'})
            boot,status=agent.ready('B',identity['version'],boot)
            assert agent.python(snapshot_code)==snapshot
            assert status['rollback_available'] and status['rollback_options'][0]['slot']=='A'
            agent.python("from pathlib import Path;assert not Path('/etc/titan-ci-factory').exists()")
            passed('update_boot_and_preserved_accounts_acls_data')
            boot,report['boot_memory_guard']=boot_memory_lifecycle(agent,client,restart,'B',identity['version'],boot)
            status=json.loads(agent.python("import sys,json;sys.path.insert(0,'/usr/lib/titan');from titan.debian_updates import system_status;print(json.dumps(system_status()))"))
            assert agent.python(snapshot_code)==snapshot
            passed('boot_memory_guard_reduced_ram_and_recovery')
            # Existing browser session and API are used to select the real old slot.
            client.request('/api/session')
            old=status['rollback_options'][0]
            client.action('update_rollback',{'expected_digest':old['digest'],'confirmation':'ROLLBACK'})
            client.action('system_reboot',{'expected_digest':old['digest'],'confirmation':'NEUSTART'})
            boot,status=agent.ready('A',baseline,boot)
            assert agent.python(snapshot_code)==snapshot
            agent.python("from pathlib import Path;assert Path('/etc/titan-ci-factory').read_text()=='baseline factory default\\n'")
            passed('factory_defaults_follow_selected_slot')
            passed('manual_rollback_and_preserved_accounts_acls_data')
            # Deliberately make the *inactive test slot* unbootable. GRUB must
            # consume its one attempt and choose healthy A after the test reset.
            agent.python("""import subprocess
from pathlib import Path
subprocess.run(['mount','/dev/disk/by-partlabel/TITAN-B','/mnt'],check=True)
try:
    p=Path('/mnt/vmlinuz');assert p.is_symlink();p.unlink()
finally:subprocess.run(['umount','/mnt'],check=True)
""")
            target=next(x for x in status['rollback_options'] if x['slot']=='B')
            client.action('update_rollback',{'expected_digest':target['digest'],'confirmation':'ROLLBACK'})
            client.action('system_reboot',{'expected_digest':target['digest'],'confirmation':'NEUSTART'})
            deadline=time.monotonic()+180
            while time.monotonic()<deadline:
                try:agent.call('guest-ping')
                except (OSError,ValueError,RuntimeError):break
                time.sleep(3)
            else:raise RuntimeError('Scheduled fallback test reboot never began')
            # QGA has stopped for shutdown. Allow the one GRUB attempt to fail
            # before the explicit reset; no hardware watchdog is assumed.
            time.sleep(30)
            qmp(work/'qmp.sock','system_reset')
            boot,status=agent.ready('A',baseline,boot)
            assert status['last_failure']['slot']=='B' and not status['rollback_options']
            assert agent.python(snapshot_code)==snapshot
            passed('failed_candidate_fallback_after_reset')
            report['ok']=True
        finally:
            if proc is not None and not report['ok']:
                try:
                    print('TITAN_AB_RAUC_DIAGNOSTICS',flush=True)
                    print(agent.execute(['/usr/bin/journalctl','-u','rauc.service','-n','100','--no-pager','-o','cat'],timeout=20)[-20000:],flush=True)
                    print(agent.execute(['/usr/bin/df','-h'],timeout=20),flush=True)
                except (OSError,ValueError,RuntimeError) as diagnostic_error:
                    print('Guest diagnostics unavailable: '+str(diagnostic_error),flush=True)
                subprocess.run(['df','-h',str(directory)],check=False)
            if proc is not None:
                proc.terminate()
                try:proc.wait(timeout=15)
                except subprocess.TimeoutExpired:proc.kill();proc.wait(timeout=10)
            if server is not None:server.shutdown();server.server_close()
            if (work/'console.log').exists():
                shutil.copyfile(work/'console.log',directory/'ab-console.log')
                if not report['ok']:
                    print('TITAN_AB_CONSOLE_TAIL',flush=True)
                    print((work/'console.log').read_text(errors='replace')[-20000:],flush=True)
            report['baseline_image_unchanged']=sha(baseline_image)==baseline_hash
            report['raw_image_unchanged']=sha(image)==original and report['baseline_image_unchanged']
            report['bundle_unchanged']=sha(bundle)==original_bundle
            report['ok']=report['ok'] and report['raw_image_unchanged'] and report['bundle_unchanged']
            (directory/'ab-test.json').write_text(json.dumps(report,indent=2)+'\n')
    assert report['ok']


if __name__=='__main__':main()
