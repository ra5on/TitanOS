"""Exercise complete VM state through real libvirt/QEMU in a disposable NAS.

The harness passes an authenticated API request, a root shell in that NAS,
and a file transfer function. Never call this against a user's NAS.
"""
import base64,gzip,json,lzma,os,shlex,subprocess,time,xml.etree.ElementTree as ET
from pathlib import Path

def check_vm_snapshots(request,remote,put,directory,volume='testdata'):
    directory=Path(directory);tools=f'/mnt/{volume}/snapshot-tools';remote(f'mkdir -p {tools}/modules; chmod 755 {tools}')
    root=Path(__file__).resolve().parents[1]
    binary=directory/'snapshot-guest'
    subprocess.run(['gcc','-static','-O2','-Wall','-Wextra',str(root/'scripts/snapshot-guest.c'),'-o',str(binary)],check=True)
    put(binary,tools+'/guest');remote(f'chmod 755 {tools}/guest')
    modules=remote('modprobe --show-depends virtio_pci; modprobe --show-depends virtio_blk')
    for path in sorted(set(line.split()[1] for line in modules.splitlines() if line.startswith('insmod '))):
        dest=tools+'/modules/'+Path(path).name.removesuffix('.zst').removesuffix('.xz')
        if path.endswith('.zst'):remote('zstd -dc '+shlex.quote(path)+' > '+shlex.quote(dest))
        elif path.endswith('.xz'):
            module=directory/Path(dest).name;module.write_bytes(lzma.decompress(base64.b64decode(remote('base64 -w0 '+shlex.quote(path)))));put(module,dest)
        else:remote('cp '+shlex.quote(path)+' '+shlex.quote(dest))
    remote(f'cp {tools}/guest {tools}/init; cd {tools}; find init modules -print | cpio --quiet -o -H newc | gzip -1 > initrd.img')
    # The active NAS slot provides the matching kernel; /efi is read only here.
    kernel=remote("find /efi/titanos -maxdepth 2 -name vmlinuz | sort | head -n 1")
    remote(f'cp {shlex.quote(kernel)} {tools}/vmlinuz; chmod -R a+rX {tools}')
    name='snapshot-smoke'
    request('/vms',{'name':name,'displayName':'Snapshot Test','template':'linux','memoryMiB':512,'vcpus':1,'volume':volume,'tpm':True,'disks':[{'format':'qcow2','sizeBytes':1024**3,'volume':volume}]})
    xml=ET.fromstring(request(f'/vms/{name}/xml')['xml']);xml.set('type','qemu')
    cpu=xml.find('cpu');xml.remove(cpu)
    cpu=ET.SubElement(xml,'cpu',{'mode':'custom','match':'exact'});ET.SubElement(cpu,'model',{'fallback':'allow'}).text='qemu64'
    osnode=xml.find('os');ET.SubElement(osnode,'kernel').text=tools+'/vmlinuz';ET.SubElement(osnode,'initrd').text=tools+'/initrd.img';ET.SubElement(osnode,'cmdline').text='console=tty0 console=ttyS0 panic=-1'
    ET.SubElement(xml.find('.//serial'),'log',{'file':tools+'/guest-serial.log','append':'on'})
    for romdev in xml.findall('.//interface'):ET.SubElement(romdev,'rom',{'bar':'off'})
    vmroot=f'/mnt/{volume}/vms/{name}'
    remote(f'mkdir -p /run/titanos-vms {vmroot}/tpm; chown libvirt-qemu:kvm {vmroot}/tpm; chmod 700 {vmroot}/tpm; chown root:kvm /run/titanos-vms; chmod 770 /run/titanos-vms; cp /usr/share/OVMF/OVMF_VARS_4M.fd {vmroot}/nvram.fd; chgrp users {vmroot}/nvram.fd; chmod 660 {vmroot}/nvram.fd')
    domain=directory/'snapshot-domain.xml';domain.write_text(ET.tostring(xml,encoding='unicode'));put(domain,tools+'/domain.xml');remote(f'virsh create {tools}/domain.xml')
    return check_snapshot_state(request,remote,tools,name)


def check_snapshot_state(request,remote,tools,name):
    def guest(command):return remote(f'{tools}/guest /run/titanos-vms/{name}.serial '+shlex.quote(command),30)
    deadline=time.monotonic()+240
    while True:
        try:
            if 'SNAPSHOT-OK' in guest('get'):break
        except subprocess.SubprocessError:pass
        if time.monotonic()>deadline:raise TimeoutError('nested guest')
        time.sleep(3)
    guest('ram original-memory');guest('disk original-disk')
    request(f'/vms/{name}/snapshots',{'name':'Before change','memory':True,'allowPause':True})
    def finish():
        end=time.monotonic()+600
        while time.monotonic()<end:
            status=request(f'/vms/{name}/snapshots')
            if status['job'] and status['job']['state']=='failed':raise AssertionError(status)
            if not status['job']:
                assert not status['pending'],status
                return status
            time.sleep(2)
        raise TimeoutError('snapshot job')
    snapshot=finish()['snapshots'][0];assert snapshot['memory']
    assert 'RAM=original-memory DISK=original-disk' in guest('get')
    guest('ram changed-memory');guest('disk changed-disk')
    request(f'/vms/{name}/poweroff',{})
    request(f'/vms/{name}/snapshots/{snapshot["id"]}/restore',{'confirm':'Snapshot Test','memory':True})
    finish();assert 'RAM=original-memory DISK=original-disk' in guest('get')
    print('Live RAM, qcow2 disk, UEFI and TPM snapshot/resume/restore passed',flush=True)
    # Snapshot metadata is on the data volume; a daemon restart must retain it.
    remote('systemctl restart titand')
    return name,snapshot
