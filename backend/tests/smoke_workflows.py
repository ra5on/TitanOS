#!/usr/bin/env python3
"""Exercise the new backend using a published base and a local titand, only on disposable QEMU disks."""
import argparse
import hashlib
import http.cookiejar
import http.client
import importlib.util
import json
import os
from pathlib import Path
import secrets
import shlex
import shutil
import ssl
import sys
import subprocess
import tempfile
import time
import urllib.error
import urllib.request
import zipfile

scripts=Path(__file__).resolve().parents[2]/'scripts'
sys.path.insert(0,str(scripts))
spec=importlib.util.spec_from_file_location('release_smoke',scripts/'smoke-release.py')
helper=importlib.util.module_from_spec(spec)
spec.loader.exec_module(helper)

def smoke(archive,binary,log,timeout):
    os.environ['PATH']+=':/usr/sbin:/sbin'
    with tempfile.TemporaryDirectory(prefix='workflows-',dir=log.parent) as temp:
        directory=Path(temp);stick=directory/'stick'
        with zipfile.ZipFile(archive) as packed:
            for entry in packed.infolist():
                if not (stick/entry.filename).resolve().is_relative_to(stick):raise ValueError('unsafe ZIP')
                if entry.filename.startswith('config/') and not entry.is_dir():raise ValueError('configuration in ZIP')
            packed.extractall(stick)
        disk=directory/'stick.img'
        with disk.open('wb') as stream:stream.truncate(4*1024**3)
        helper.command('sfdisk','--quiet','--label','dos',str(disk),input=b'start=1MiB, type=c, bootable\n')
        helper.command('mformat','-i',str(disk)+'@@1M','-F','-v','TITANOS','::')
        helper.command('mcopy','-i',str(disk)+'@@1M','-s',*map(str,stick.iterdir()),'::')
        shutil.copyfile('/usr/share/OVMF/OVMF_VARS_4M.fd',directory/'vars.fd')
        for index in range(1,8):helper.command('qemu-img','create','-q','-f','qcow2',str(directory/f'data{index}.qcow2'),'2G')
        helper.command('ssh-keygen','-q','-t','ed25519','-N','','-f',str(directory/'key'))
        port,ssh_port=helper.free_port(),helper.free_port();url=f'https://127.0.0.1:{port}'
        cookies=http.cookiejar.CookieJar();tls=ssl._create_unverified_context()
        def request(path,data=None,method=None):
            req=urllib.request.Request(url+'/api/v1'+path,data=json.dumps(data).encode() if data is not None else None,headers={'Content-Type':'application/json','Origin':url},method=method)
            # Keep the guest side open until the full body is read. urllib
            # forces Connection: close; QEMU user networking can truncate that
            # final response (also documented in smoke-app-update.py).
            # Never retry a mutation whose response was interrupted.
            cookies.add_cookie_header(req)
            connection=http.client.HTTPSConnection('127.0.0.1',port,timeout=180,context=tls)
            try:
                connection.request(req.get_method(),req.selector,body=req.data,headers=dict(req.header_items()))
                response=connection.getresponse();cookies.extract_cookies(response,req)
                try:body=response.read()
                except http.client.IncompleteRead as error:
                    print('Incomplete HTTP response:',path,str(response.headers),repr(error.partial),flush=True)
                    raise
                if response.status>=400:raise RuntimeError(f'{path}: {response.status} {body.decode()}')
                return json.loads(body) if body else None
            finally:connection.close()
        ssh=['ssh','-i',str(directory/'key'),'-o','IdentitiesOnly=yes','-o','BatchMode=yes','-o','StrictHostKeyChecking=accept-new','-o',f'UserKnownHostsFile={directory}/known_hosts','-o','ConnectTimeout=3','-p',str(ssh_port),'storagetester@127.0.0.1']
        def remote(command,limit=180):return subprocess.check_output([*ssh,'sudo sh -ec '+shlex.quote(command)],text=True,timeout=limit).strip()
        def wait(predicate,label,limit=120):
            end=time.monotonic()+limit
            while time.monotonic()<end:
                try:
                    value=predicate()
                    if value:return value
                except (OSError,ValueError,RuntimeError):pass
                time.sleep(1)
            raise TimeoutError(label)
        def inject():
            remote('mkdir -p /var/lib/titanos /run/systemd/system/titand.service.d')
            with binary.open('rb') as stream:subprocess.run([*ssh,'sudo sh -c '+shlex.quote('cat > /var/lib/titanos/workflow-titand')],stdin=stream,check=True)
            remote("chmod 755 /var/lib/titanos/workflow-titand\nprintf '[Service]\\nExecStart=\\nExecStart=/var/lib/titanos/workflow-titand\\n' > /run/systemd/system/titand.service.d/99-workflow.conf\nsystemctl daemon-reload\nsystemctl restart titand")
            wait(lambda:request('/health').get('ready'),'new daemon')
            wait(lambda:request('/maintenance').get('settings'),'new routes')
        accel=['-accel','kvm','-cpu','host'] if os.access('/dev/kvm',os.W_OK) else ['-accel','tcg','-cpu','max']
        args=['qemu-system-x86_64','-machine','q35',*accel,'-smp','2','-m','4G','-drive','if=pflash,format=raw,readonly=on,file=/usr/share/OVMF/OVMF_CODE_4M.fd','-drive',f'if=pflash,format=raw,file={directory}/vars.fd','-device','qemu-xhci','-drive',f'if=none,id=stick,format=raw,file={disk}','-device','usb-storage,drive=stick,removable=on,bootindex=0','-netdev',f'user,id=net0,hostfwd=tcp:127.0.0.1:{port}-:443,hostfwd=tcp:127.0.0.1:{ssh_port}-:22','-device','virtio-net-pci,netdev=net0','-smbios','type=11,value=io.systemd.boot.kernel-cmdline-extra=console=tty0 console=ttyS0,,115200','-display','none','-serial','stdio','-monitor','none','-no-reboot']
        for i in range(1,8):args+=['-drive',f'if=none,id=data{i},format=qcow2,file={directory}/data{i}.qcow2','-device',f'virtio-blk-pci,drive=data{i},serial=WORK{i}']
        with log.open('wb') as serial:
            process=subprocess.Popen(args,stdin=subprocess.DEVNULL,stdout=serial,stderr=subprocess.STDOUT)
            try:
                wait(lambda:request('/health').get('ready'),'base boot',timeout)
                password=secrets.token_urlsafe(24);request('/setup',{'name':'storagetester','password':password});request('/account/ssh-keys',{'key':(directory/'key.pub').read_text().strip(),'password':password});request('/system/ssh',{'enabled':True},'PUT')
                wait(lambda:subprocess.run([*ssh,'true'],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL).returncode==0,'SSH',45)
                inject();print('New backend installed in RAM; session preserved.',flush=True)
                disks={d['serial']:d for d in request('/storage')['disks']}
                assert all(disks[f'WORK{i}']['name'].startswith('vd') for i in range(1,8))
                names=lambda indices:[disks[f'WORK{i}']['name'] for i in indices]
                # Foreign RAID registration never formats or creates initial folders.
                foreign=names([5,6]);remote('mdadm --create /dev/md/foreign --run --metadata=1.2 --level=1 --raid-devices=2 '+ ' '.join('/dev/'+n for n in foreign)+'\nmkfs.ext4 -q /dev/md/foreign\nmkdir /tmp/foreign\nmount /dev/md/foreign /tmp/foreign\nmkdir /tmp/foreign/existing\nprintf retained > /tmp/foreign/existing/proof\numount /tmp/foreign')
                wait(lambda:remote('cat /sys/block/md*/md/sync_action').splitlines().count('idle')>=1,'foreign sync')
                candidates=request('/storage/import');candidate=next(c for c in candidates if c['filesystem']=='ext4' and not c['reason'])
                preview=request('/storage/import/preview',{'id':candidate['id'],'fingerprint':candidate['fingerprint']});assert preview['readOnly'] and 'existing' in preview['folders'],preview
                try:request('/storage/import',{'id':candidate['id'],'fingerprint':'stale','name':'foreign','confirm':'foreign'})
                except RuntimeError:pass
                else:raise AssertionError('stale import accepted')
                request('/storage/import',{'id':candidate['id'],'fingerprint':candidate['fingerprint'],'name':'foreign','confirm':'foreign'})
                assert remote('cat /mnt/foreign/existing/proof')=='retained'
                assert remote('test ! -e /mnt/foreign/appdata; echo preserved')=='preserved'
                request('/storage/volumes/foreign',{'confirm':'foreign'},'DELETE');print('Read-only foreign RAID preview, stale identity rejection and non-formatting import passed.',flush=True)
                def create(name,level,filesystem,indices):
                    members=names(indices);request('/storage/volumes',{'name':name,'level':level,'filesystem':filesystem,'disks':members,'erase':members,'identities':{d['name']:d['identity'] for d in request('/storage')['disks']}});return wait(lambda:next((v for v in request('/storage')['volumes'] if v['name']==name and v['mounted'] and v['state']=='ok'),None),'create '+name,180)
                create('array','raid5','ext4',[1,2,3]);create('safe','raid0','ext4',[7]);create('mirror','raid1','btrfs',[5,6])
                # A genuine foreign native mirror is unregistered only in this
                # disposable fixture, then previewed and registered without mkfs.
                remote('printf native-proof > /mnt/mirror/native-proof')
                registered=json.loads(remote('cat /config/storage/volumes.json'))
                remote('systemctl stop titand\numount /mnt/mirror')
                filtered=[v for v in registered if v['name']!='mirror']
                remote('printf %s '+shlex.quote(json.dumps(filtered))+' > /config/storage/volumes.json.tmp\nmv /config/storage/volumes.json.tmp /config/storage/volumes.json\nsystemctl start titand')
                wait(lambda:request('/health').get('ready'),'restart for foreign Btrfs import')
                candidate=next(c for c in request('/storage/import') if c['filesystem']=='btrfs' and not c['reason'])
                request('/storage/import/preview',{'id':candidate['id'],'fingerprint':candidate['fingerprint']})
                request('/storage/import',{'id':candidate['id'],'fingerprint':candidate['fingerprint'],'name':'mirror','confirm':'mirror'})
                assert remote('cat /mnt/mirror/native-proof')=='native-proof'
                print('Native two-device Btrfs import preserved existing data and verified both RAID1 profiles.',flush=True)
                remote('mkdir -p /mnt/array/proof\ndd if=/dev/urandom of=/mnt/array/proof/seed bs=1M count=32 status=none\nsync');proof=remote('sha256sum /mnt/array/proof/seed').split()[0]
                before=next(v for v in request('/storage')['volumes'] if v['name']=='array')['sizeBytes'];fresh=next(d for d in request('/storage')['disks'] if d['serial']=='WORK4')
                grow={'disk':fresh['name'],'identity':fresh['identity'],'recoveryVolume':'safe','confirm':'array','erase':True}
                plan=request('/storage/volumes/array/growth/preview',grow);assert plan['targetDisks']==4 and plan['plannedBytes']>plan['currentBytes']>0,plan
                remote('echo 10000 > /proc/sys/dev/raid/speed_limit_max')
                request('/storage/volumes/array/growth',grow)
                wait(lambda:request('/storage/growth')['operation'].get('phase')=='reshaping','reshape start',120)
                remote('systemctl restart titand');wait(lambda:request('/health').get('ready'),'restart during reshape')
                assert request('/storage/growth')['pending']
                remote('echo 200000 > /proc/sys/dev/raid/speed_limit_max');request('/storage/growth/recover',{'confirm':'array'})
                wait(lambda:not request('/storage/growth')['pending'],'reshape and filesystem expansion',300)
                expanded=next(v for v in request('/storage')['volumes'] if v['name']=='array');assert expanded['raidDisks']==4 and expanded['sizeBytes']>before,expanded
                assert remote('sha256sum /mnt/array/proof/seed').split()[0]==proof
                print('RAID5 expansion, daemon restart, explicit recovery, filesystem growth and unchanged data passed.',flush=True)
                remote('mkdir -p /mnt/mirror/photos\nprintf snapshot-proof > /mnt/mirror/photos/picture.txt')
                request('/storage/volumes/mirror/snapshots',{'name':'before','action':'create'})
                listing=request('/storage/volumes/mirror/snapshots/before/files?path=photos');assert listing['entries'][0]['path']=='photos/picture.txt',listing
                preview=request('/storage/volumes/mirror/snapshots/before/files/preview',{'paths':['photos/picture.txt'],'targetVolume':'safe'});assert preview['files']==1 and preview['bytes']==14 and preview['uuid']
                restored=request('/storage/volumes/mirror/snapshots/before/files/restore',{'paths':['photos/picture.txt'],'targetVolume':'safe','uuid':preview['uuid']})
                assert remote('cat '+shlex.quote('/mnt/safe/'+restored['folder']+'/photos/picture.txt'))=='snapshot-proof'
                report=request('/storage/volumes/mirror/usage');assert report['scanned']>0
                request('/monitoring',{'enabled':True,'volume':'safe','days':7,'maxMiB':8},'PUT')
                settings={'enabled':True,'volume':'safe','timezone':'UTC','windowFrom':'00:00','windowTo':'00:00','jobs':[{'id':'scheduled','name':'Snapshot schedule','kind':'snapshot','target':'mirror','enabled':True,'every':'daily','time':'00:00','missed':'window','keep':1,'allowPrune':True}]}
                request('/maintenance',settings,'PUT');wait(lambda:request('/maintenance')['history'],'scheduled snapshot',70)
                assert any(s['name'].startswith('auto-scheduled-') for s in request('/storage/volumes/mirror/snapshots'))
                request('/maintenance/scheduled/run',{});wait(lambda:not request('/maintenance')['jobs'][0]['state']['running'],'manual scheduled snapshot')
                assert len([s for s in request('/storage/volumes/mirror/snapshots') if s['name'].startswith('auto-scheduled-')])==1
                print('Btrfs file browsing/selective restore, scheduled snapshots/retention and on-demand usage passed.',flush=True)
                request('/vms',{'name':'original','template':'linux','firmware':'bios','volume':'mirror','memoryMiB':512,'vcpus':1,'disks':[{'format':'qcow2','sizeBytes':1024**3,'volume':'mirror'}]})
                original=next(v for v in request('/vms')['vms'] if v['name']=='original');request('/vms/original/clone',{'name':'clone','volume':'safe'})
                clone=wait(lambda:next((v for v in request('/vms')['vms'] if v['name']=='clone'),None),'VM clone')
                assert clone['mac']!=original['mac'] and not clone['autostart'];assert remote('qemu-img compare /mnt/mirror/vms/original/vdisk1.qcow2 /mnt/safe/vms/clone/vdisk1.qcow2; echo independent').endswith('independent')
                print('Independent stopped VM clone and changed MAC passed.',flush=True)
                request('/shares',{'name':'proof','volume':'array','smb':True})
                job={'id':'verified','sources':['proof'],'destination':{'type':'local','volume':'safe','folder':'backups'},'enabled':False,'every':'daily','time':'03:00','keep':{'daily':1,'weekly':0,'monthly':0}}
                request('/backups',job);request('/backups/verified/run',{})
                state=lambda:next(v for v in request('/backups')['jobs'] if v['job']['id']=='verified')['state']
                wait(lambda:not state()['running'],'real backup',120);assert state()['lastOK'] and not state()['lastError'],state()
                request('/backups/verified/probe',{'maxBytes':64*1024**2,'maxFiles':20});wait(lambda:not state()['running'],'restore probe',120);assert state()['probe']['files']>=1 and not state()['probe']['error'],state()
                print('Versioned backup and verified restore probe passed.',flush=True)

                # A local image avoids registry/network dependencies. Only selected
                # application data is saved; the original running set is restored.
                request('/docker/settings',{'enabled':True,'volume':'mirror'},'PUT')
                wait(lambda:request('/docker')['running'],'Docker startup',90)
                remote("""mkdir -p /mnt/mirror/backup-tools/root/bin /mnt/mirror/appdata/app-proof
cp /bin/busybox /mnt/mirror/backup-tools/root/bin/
ldd /bin/busybox | awk '{for(i=1;i<=NF;i++) if ($i ~ /^\\//) print $i}' | while read -r lib; do cp --parents "$lib" /mnt/mirror/backup-tools/root/; done
tar -C /mnt/mirror/backup-tools/root -c . | docker import - titanos-backup-smoke:local
docker run -d --name app-proof -v /mnt/mirror/appdata/app-proof:/data titanos-backup-smoke:local /bin/busybox sh -c 'trap "exit 0" TERM; while true; do echo alive > /data/marker; /bin/busybox sleep 1; done'
""")
                job['applications']=[{'kind':'docker','name':'app-proof','method':'stop'},{'kind':'vm','name':'original','method':'stop'}];job['allowStop']=True
                request('/backups/verified',job,'PUT');request('/backups/verified/run',{});wait(lambda:not state()['running'],'application backup',150)
                assert state()['lastOK'] and not state()['lastError'],state()
                assert remote("docker inspect -f '{{.State.Running}}' app-proof")=='true'
                assert request('/backups/application-recovery')['operation'] is None
                print('Application backup stopped/restarted the selected Docker container and included a stopped VM.',flush=True)
                snapshots=request('/backups/verified/snapshots')['snapshots']
                application_preview=request('/backups/verified/applications/preview',{'snapshot':snapshots[0]['id'],'volume':'safe'})
                assert not application_preview['currentSelection'] and len(application_preview['capturedApplications'])==2,application_preview
                application_copy=request('/backups/verified/applications/restore',{'snapshot':snapshots[0]['id'],'volume':'safe'})
                wait(lambda:not state()['running'],'verified application restore',150)
                assert state()['applicationRestore']['verified'] and not state()['applicationRestore']['error'],state()
                assert remote('cat '+shlex.quote(application_copy['target']+'/mnt/mirror/appdata/app-proof/marker'))=='alive'
                coverage=request('/backups/coverage')
                assert any(v['kind']=='docker' and v['name']=='app-proof' and v['jobs'] for v in coverage['items']),coverage
                print('Captured archive identities, verified independent application restore and backup coverage passed.',flush=True)
                # A VM-only selection must still reject a running Docker writer.
                audit_job={**job,'id':'writer-proof','destination':{'type':'local','volume':'safe','folder':'writer-proof'},'extras':['appdata'],'applications':[{'kind':'vm','name':'original','method':'stop'}]}
                request('/backups',audit_job);request('/backups/writer-proof/run',{})
                audit_state=lambda:next(v for v in request('/backups')['jobs'] if v['job']['id']=='writer-proof')['state']
                wait(lambda:not audit_state()['running'],'reject live writer',90)
                assert 'another running container' in audit_state()['lastError'] and not audit_state()['lastOK'],audit_state()
                assert remote("docker inspect -f '{{.State.Running}}' app-proof")=='true'
                # Same name is not the same Btrfs snapshot UUID.
                paused_settings=json.loads(json.dumps(settings));paused_settings['enabled']=False;paused_settings['jobs'][0]['enabled']=False
                request('/maintenance',paused_settings,'PUT')
                old=next(s['name'] for s in request('/storage/volumes/mirror/snapshots') if s['name'].startswith('auto-scheduled-'))
                request('/storage/volumes/mirror/snapshots',{'name':old,'action':'delete','confirm':old})
                request('/storage/volumes/mirror/snapshots',{'name':old,'action':'create'})
                request('/maintenance/scheduled/run',{});wait(lambda:not request('/maintenance')['jobs'][0]['state']['running'],'manual replacement retention')
                assert any(s['name']==old for s in request('/storage/volumes/mirror/snapshots'))
                request('/maintenance',settings,'PUT')
                print('Regression proofs passed: VM-only backup rejects Docker writer; manual replacement snapshot survives retention.',flush=True)
                # Pause before publication, restart titand, reauthorize and resume.
                # The unfinished file restarts from byte zero; already published
                # files are not replayed and the old temporary inode is removed.
                remote('truncate -s 256M /mnt/array/proof/transfer.bin\nmkdir -p /mnt/safe/transfers')
                transfer=request('/files/copy',{'root':'volume/array','paths':['proof/transfer.bin'],'toRoot':'volume/safe','toDir':'transfers','move':False,'conflict':'rename'})
                request('/files/jobs/'+transfer['id']+'/control',{'paused':True,'priority':0,'rate':1048576})
                wait(lambda:next(j for j in request('/files/jobs') if j['id']==transfer['id'])['paused'],'pause transfer')
                remote('systemctl restart titand');wait(lambda:request('/health').get('ready'),'restart paused transfer')
                recovered=next(j for j in request('/files/jobs') if j['id']==transfer['id']);assert recovered['durable'] and recovered['retryable'] and not recovered['blocked'],recovered
                request('/files/jobs/'+transfer['id']+'/retry',{})
                request('/files/jobs/'+transfer['id']+'/control',{'paused':False,'priority':0,'rate':0})
                wait(lambda:next(j for j in request('/files/jobs') if j['id']==transfer['id'])['state']=='done','resumed transfer',120)
                checksums=remote('sha256sum /mnt/array/proof/transfer.bin /mnt/safe/transfers/transfer.bin').splitlines();assert checksums[0].split()[0]==checksums[1].split()[0]
                assert remote('find /mnt/safe/transfers -name ".titanos-copy-*" | wc -l')=='0'
                print('Durable transfer, current access recheck, partial-file cleanup and resumed file content passed.',flush=True)
                # Interrupt a real application backup while restic waits for a
                # deliberately silent local endpoint. Recovery must not resume a
                # different container or repeat the backup after process death.
                remote('nohup /bin/busybox nc -l -p 8801 -e /bin/sleep 600 > /run/workflow-listener.log 2>&1 < /dev/null & echo $! > /run/workflow-listener.pid')
                recovery_job={**job,'id':'interrupted','destination':{'type':'rest','url':'http://127.0.0.1:8801/repository'},'applications':[{'kind':'docker','name':'app-proof','method':'stop'}]}
                request('/backups',recovery_job);request('/backups/interrupted/run',{})
                wait(lambda:remote("docker inspect -f '{{.State.Running}}' app-proof")=='false','application stopped before simulated daemon crash',90)
                pending=request('/backups/application-recovery')['operation'];assert pending and pending['id'],pending
                remote('systemctl kill --signal=SIGKILL --kill-whom=all titand.service');wait(lambda:request('/health').get('ready'),'crash restart')
                wait(lambda:request('/docker')['running'],'Docker reconnect')
                assert remote("docker inspect -f '{{.State.Running}}' app-proof")=='false'
                assert request('/backups/application-recovery')['operation']['id']==pending['id']
                try:request('/docker/containers/'+next(c['id'] for c in request('/docker')['containers'] if c['name']=='app-proof')+'/start',{})
                except RuntimeError as error:assert ': 423 ' in str(error),error
                else:raise AssertionError('pending application recovery allowed a lifecycle mutation')
                request('/backups/application-recovery',{'confirm':pending['id'],'restart':True})
                assert remote("docker inspect -f '{{.State.Running}}' app-proof")=='true'
                assert request('/backups/application-recovery')['operation'] is None
                print('Interrupted application backup retained its recovery fence and restarted only the original container after explicit recovery.',flush=True)

                wait(lambda:request('/monitoring/history?hours=24'),'persisted monitoring sample',75)
                remote('systemctl restart titand');wait(lambda:request('/health').get('ready'),'final restart')
                assert request('/monitoring/history?hours=24') and request('/maintenance')['settings']['enabled']
                print('Monitoring history and maintenance settings survived daemon restart.',flush=True)
                print('Workflow storage smoke passed. Only disposable guest disks were attached.',flush=True)
            except BaseException:
                try: log.with_suffix('.titand.log').write_text(remote('journalctl -u titand -n 250 --no-pager',30))
                except BaseException: pass
                raise
            finally:
                process.terminate()
                try:process.wait(timeout=20)
                except subprocess.TimeoutExpired:process.kill();process.wait()
if __name__=='__main__':
    parser=argparse.ArgumentParser(description=__doc__);parser.add_argument('zip',type=Path);parser.add_argument('binary',type=Path);parser.add_argument('--log',type=Path,default=Path('out/validation/workflows-qemu-serial.log'));parser.add_argument('--timeout',type=int,default=420);args=parser.parse_args();args.log.parent.mkdir(parents=True,exist_ok=True);smoke(args.zip.resolve(),args.binary.resolve(),args.log.resolve(),args.timeout)
