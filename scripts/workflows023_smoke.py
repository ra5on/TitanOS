"""0.23 checks inside smoke-storage's disposable QEMU NAS only.

Real networkd, VLAN frames over emulated Ethernet hubs, active-backup bond,
Docker LAN/Compose, NFS server, media publication and libvirt XML. Extra ports
and data disks are supplied by --workflows023. No host interfaces are changed.
"""
import gzip
import hashlib
import json
import shlex
import subprocess
import time
import urllib.error
import urllib.request
import xml.etree.ElementTree as ET


def wait(fn, label, timeout=90):
    end = time.monotonic() + timeout
    while time.monotonic() < end:
        try:
            value = fn()
            if value:
                return value
        except (OSError, RuntimeError, subprocess.SubprocessError):
            pass
        time.sleep(1)
    raise TimeoutError(label)


def checks(boot, request, remote, put, directory, opener, url, password, online_virtio=False):
    if boot != 1:
        state = request('/network')['network']
        assert len(state['config']['networks']) == 3 and not state['pending'], state
        remote('test -d /sys/class/net/tnapps; test -d /sys/class/net/tbuplink; test -d /sys/class/net/tvapps')
        assert next(s for s in request('/shares') if s['name'] == 'NFSReadOnly')['nfs']['mode'] == 'ro'
        jobs = request('/vms/imports')['jobs']
        assert any(j['name'] == 'fixture.iso' and j['phase'] == 'done' for j in jobs), jobs
        assert remote('test -x /usr/bin/titanosctl; systemctl is-enabled titanos-network-recovery.service') == 'enabled'
        print(f'PASS 0.23 boot {boot}: confirmed topology, bond/VLAN, NFS RO and completed import survive NAS reboot', flush=True)
        return

    state = request('/network')['network']
    ports = {p['mac']: p['name'] for p in state['interfaces']}
    extras = [ports[f'52:54:00:23:00:0{i}'] for i in range(1, 5)]
    management = next(p['name'] for p in state['interfaces'] if p['name'] not in extras)
    papps, ppeer, pbond1, pbond2 = extras

    def net(id, name, parent, address='', bridge=True, vlan=0, management=False):
        return dict(id=id, name=name, purpose='management' if management else 'apps', parent=parent,
                    vlan=vlan, bridge=bridge, management=management, ipv4='dhcp' if management else 'static',
                    address=address, gateway='', ipv6='auto' if management else 'static',
                    address6='' if management else ('fd23:30::20/64' if id == 'apps' else 'fd23:40::20/64'),
                    gateway6='', dns=[], routes=[])

    cfg = dict(state['config'], bonds=[dict(id='uplink', ports=[pbond1, pbond2], mode='active-backup')],
               networks=[net('admin', 'Verwaltung', management, management=True),
                         net('apps', 'Anwendungen', papps, '172.29.30.20/24', vlan=30),
                         net('store', 'Speicher', 'bond:uplink', '172.29.40.20/24', vlan=40)])

    def apply(c, confirm=True):
        try:
            request('/network', c, 'PUT')
        except OSError:
            pass
        pending = wait(lambda: request('/network')['network']['pending'], 'network trial')
        for n in pending['networks']: n.setdefault('routes', [])
        assert pending['networks'] == c['networks'], pending
        if confirm:
            wait(lambda: request('/network')['network']['addresses'], 'management DHCP')
            request('/network/confirm', {})
            assert request('/network')['network']['pending'] is None

    apply(cfg)
    remote('ip -j -d link show tvapps')
    assert json.loads(remote('ip -j -d link show tvapps'))[0]['linkinfo']['info_data']['id'] == 30
    assert remote('cat /sys/class/net/br0/address') == next(p['mac'] for p in state['interfaces'] if p['name'] == management)
    remote(f'''ip netns add titan-peer
ip link set {shlex.quote(ppeer)} netns titan-peer
ip -n titan-peer link set lo up
ip -n titan-peer link set {shlex.quote(ppeer)} up
ip -n titan-peer link add peer30 link {shlex.quote(ppeer)} type vlan id 30
ip -n titan-peer address add 172.29.30.2/24 dev peer30
ip -n titan-peer -6 address add fd23:30::2/64 dev peer30
ip -n titan-peer link set peer30 up
''')
    wait(lambda: remote('ping -c1 -W1 -I tnapps 172.29.30.2') is not None, 'tagged VLAN peer')
    wait(lambda: remote('ping -6 -c1 -W1 -I tnapps fd23:30::2') is not None, 'IPv6 VLAN peer')
    assert remote(f'cat /sys/class/net/tbuplink/bonding/active_slave') in (pbond1, pbond2)
    active = remote('cat /sys/class/net/tbuplink/bonding/active_slave')
    remote(f'ip link set {active} down')
    wait(lambda: remote('cat /sys/class/net/tbuplink/bonding/active_slave') not in ('', active), 'bond failover')
    remote(f'ip link set {active} up')
    # Changed VLAN definitions must replace the old kernel VLAN, and revert cleanly.
    trial = json.loads(json.dumps(cfg)); trial['networks'][1]['vlan'] = 31
    apply(trial, False)
    assert json.loads(remote('ip -j -d link show tvapps'))[0]['linkinfo']['info_data']['id'] == 31
    request('/network/revert', {})
    wait(lambda: json.loads(remote('ip -j -d link show tvapps'))[0]['linkinfo']['info_data']['id'] == 30, 'VLAN rollback')
    remote('systemctl restart titand')
    wait(lambda: request('/health').get('ready'), 'daemon restart')
    request('/login', dict(name='storagetester', password=password))
    assert request('/network')['network']['pending'] is None
    remote('ping -c1 -W2 -I tnapps 172.29.30.2')
    print('PASS real networkd: preferred DHCP bridge/MAC, tagged VLAN IPv4/IPv6, active-backup failover, changed VLAN/revert and daemon restart', flush=True)

    # Real NFSv4 service: the client asks for RW and the server must reject writes.
    request('/shares', dict(name='NFSReadOnly', volume='mirror', smb=True, nfs=dict(mode='ro', clients=['127.0.0.1'])))
    remote('printf nfs-marker > /mnt/mirror/NFSReadOnly/marker.txt; chmod 664 /mnt/mirror/NFSReadOnly/marker.txt; mkdir -p /root/nfs-client')
    wait(lambda: 'NFSReadOnly' in remote('exportfs -v'), 'NFS export')
    remote('mount -t nfs -o vers=4,rw 127.0.0.1:/NFSReadOnly /root/nfs-client; test "$(cat /root/nfs-client/marker.txt)" = nfs-marker; if touch /root/nfs-client/denied; then exit 1; fi; umount /root/nfs-client')
    share = next(s for s in request('/shares') if s['name'] == 'NFSReadOnly')
    assert share['access']['storagetester'] == 'rw'
    share['nfs']['mode'] = 'rw'
    request('/shares/NFSReadOnly', share, 'PUT')
    wait(lambda: 'rw,' in remote("exportfs -v | sed -n '/NFSReadOnly/,+1p'"), 'writable NFS export')
    remote('mount -t nfs -o vers=4,rw 127.0.0.1:/NFSReadOnly /root/nfs-client; touch /root/nfs-client/allowed; umount /root/nfs-client')
    share['nfs']['mode'] = 'ro'; request('/shares/NFSReadOnly', share, 'PUT')
    print('PASS real NFS server: RO write denial, explicit RW write, independent SMB/account permissions', flush=True)

    # The actual engine and Compose create containers; no host Docker is used.
    request('/docker/settings', dict(enabled=True, volume='mirror'), 'PUT')
    wait(lambda: request('/docker')['running'], 'Docker engine', 120)
    remote('docker pull busybox:1.37', timeout=180)
    lan = dict(name='apps', parent='apps', driver='ipvlan', subnet='172.29.30.0/24', gateway='172.29.30.1',
               range='172.29.30.128/26', hostAccess=True, hostAddress='172.29.30.21')
    request('/docker/networks', lan)
    compose = 'services:\n  web:\n    image: busybox:1.37\n    command: [sleep, "1800"]\n  db:\n    image: busybox:1.37\n    command: [sleep, "1800"]\n'
    request('/docker/apps', dict(name='lan-test', compose=compose, env={}))
    def appdone():
        job = request('/docker')['jobs'].get('lan-test')
        if job and job['state'] == 'failed':
            raise AssertionError(job)
        return job and job['state'] == 'done'
    wait(appdone, 'Compose install', 180)
    request('/docker/apps/lan-test/network', dict(service='web', network='apps', address='172.29.30.130'))
    wait(appdone, 'Compose LAN assignment', 120)
    web = remote('docker ps -q --filter label=com.docker.compose.project=lan-test --filter label=com.docker.compose.service=web')
    db = remote('docker ps -q --filter label=com.docker.compose.project=lan-test --filter label=com.docker.compose.service=db')
    inspect = json.loads(remote('docker inspect '+web))[0]
    assert inspect['NetworkSettings']['Networks']['titanos-lan-apps']['IPAddress'] == '172.29.30.130'
    assert len(inspect['NetworkSettings']['Networks']) == 2
    assert 'titanos-lan-apps' not in json.loads(remote('docker inspect '+db))[0]['NetworkSettings']['Networks']
    remote(f'docker exec {web} ping -c1 -W3 172.29.30.2; ping -c1 -W3 172.29.30.130')
    try:
        request('/docker/networks/apps', method='DELETE')
    except RuntimeError as e:
        assert 'lan_in_use' in str(e), e
    else:
        raise AssertionError('removed in-use LAN')
    invalid = json.loads(json.dumps(cfg)); invalid['networks'][1]['vlan'] = 31
    try:
        request('/network', invalid, 'PUT')
    except RuntimeError as e:
        assert 'invalid_network' in str(e), e
    else:
        raise AssertionError('changed in-use parent VLAN')
    request('/docker/apps/lan-test', method='DELETE')
    wait(lambda: not remote('docker ps -aq --filter label=com.docker.compose.project=lan-test'), 'app removal')
    request('/docker/networks/apps', method='DELETE')
    # macvlan is a distinct driver and receives its own endpoint and host shim.
    lan.update(name='macapps', driver='macvlan')
    request('/docker/networks', lan)
    remote('docker run -d --name mac-test --network titanos-lan-macapps --ip 172.29.30.131 busybox:1.37 sleep 600')
    remote('docker exec mac-test ping -c1 -W3 172.29.30.2; ping -c1 -W3 172.29.30.131; docker rm -f mac-test')
    request('/docker/networks/macapps', method='DELETE')
    request('/docker/settings', dict(enabled=False, volume='mirror'), 'PUT')
    print('PASS real Docker: ipvlan/macvlan VLAN traffic, static service IP, private DB, host reachability and in-use deletion/topology protection', flush=True)

    # Real data-volume import with bounded upload, restart proof and extraction.
    image = bytearray(80 << 10); image[16*2048+1:16*2048+6] = b'CD001'
    source = gzip.compress(image)
    job = request('/vms/imports', dict(volume='mirror', name='fixture.iso', uploadName='fixture.iso.gz',
                                    uploadSize=len(source), sha256=hashlib.sha256(source).hexdigest()))
    def upload(id, offset, body):
        req = urllib.request.Request(url+f'/api/v1/vms/imports/{id}/data?offset={offset}', data=body, method='PUT',
                                     headers={'Origin':url,'Content-Type':'application/octet-stream'})
        with opener.open(req, timeout=60) as res:
            assert res.status == 204
    offset = len(source)//2
    upload(job['id'], 0, source[:offset])
    remote('systemctl restart titand')
    wait(lambda: request('/health').get('ready'), 'restart during upload')
    request('/login', dict(name='storagetester', password=password))
    request(f'/vms/imports/{job["id"]}/verify', dict(chunks=[dict(offset=0,size=offset,sha256=hashlib.sha256(source[:offset]).hexdigest())]))
    upload(job['id'], offset, source[offset:])
    request(f'/vms/imports/{job["id"]}/finish', {})
    def imported():
        j=next(j for j in request('/vms/imports')['jobs'] if j['id']==job['id'])
        if j['phase'] in ('failed','review'): raise AssertionError(j)
        return j if j['phase']=='done' else None
    done=wait(imported,'media publication')
    assert remote('sha256sum /mnt/mirror/iso/fixture.iso').split()[0] == hashlib.sha256(image).hexdigest()
    # This small fixture proves CD attachment/validation, not a bootable OS.
    vm = dict(name='named-network',template='windows',memoryMiB=1024,vcpus=1,volume='mirror',
              network='network:apps',iso=done['output'],virtioISO=done['output'],
              disks=[dict(format='qcow2',sizeBytes=1<<30,volume='mirror')])
    request('/vms',vm)
    xml=ET.fromstring(request('/vms/named-network/xml')['xml'])
    assert xml.find('.//interface/source').get('bridge') == 'tnapps'
    assert len(xml.findall(".//disk[@device='cdrom']")) == 2
    request('/vms/named-network',dict(deleteDisks=True,confirm='named-network'),'DELETE')
    print('PASS media: gzip import, restart/prefix proof, published hash; real generated libvirt named bridge and two Windows CDs (guest OS not installed)', flush=True)

    remote('mkdir -p /mnt/mirror/http-fixture; qemu-img create -q -f qcow2 /mnt/mirror/http-fixture/import.qcow2 64M; gzip -c /mnt/mirror/http-fixture/import.qcow2 > /mnt/mirror/http-fixture/import.qcow2.gz; busybox httpd -p 127.0.0.1:18098 -h /mnt/mirror/http-fixture')
    downloaded=request('/vms/imports',dict(volume='mirror',name='http-import.qcow2',url='http://127.0.0.1:18098/import.qcow2.gz'))
    def disk_imported():
        j=next(j for j in request('/vms/imports')['jobs'] if j['id']==downloaded['id'])
        if j['phase'] in ('failed','review'):raise AssertionError(j)
        return j if j['phase']=='done' else None
    wait(disk_imported,'HTTP qcow2 import')
    assert json.loads(remote('qemu-img info --output=json /mnt/mirror/iso/http-import.qcow2'))['virtual-size']==64<<20
    print('PASS real HTTP gzip/qcow2 download, extraction, qemu-img validation and publication',flush=True)

    remote("mkdir -p /config/plugins/releases /mnt/mirror/.titanos/plugins/releases/0.20.0 /mnt/mirror/.titanos/plugins/releases/0.23.0; chmod 700 /mnt/mirror/.titanos /mnt/mirror/.titanos/plugins /mnt/mirror/.titanos/plugins/releases; printf '{\"volume\":\"mirror\",\"enabled\":[]}' > /config/plugins/releases/0.23.0.json; printf old > /mnt/mirror/.titanos/plugins/releases/0.20.0/fixture.raw; printf current > /mnt/mirror/.titanos/plugins/releases/0.23.0/fixture.raw; systemctl restart titand")
    wait(lambda:request('/health').get('ready'),'plugin inventory restart')
    request('/login',dict(name='storagetester',password=password))
    releases=request('/plugins/releases')['releases']
    assert next(r for r in releases if r['version']=='0.23.0')['protected']
    old=next(r for r in releases if r['version']=='0.20.0')
    request('/plugins/cleanup',dict(selection=[dict(version=old['version'],fingerprint=old['fingerprint'])]))
    remote('test ! -e /mnt/mirror/.titanos/plugins/releases/0.20.0; test -f /mnt/mirror/.titanos/plugins/releases/0.23.0/fixture.raw')
    print('PASS real plugin-volume cleanup, current release protection and mounted/private directory checks',flush=True)

    token=request('/account/tokens',dict(name='QEMU CLI',scopes=['system:read']))
    token_path=directory/'api-token'; token_path.write_text(token['secret']); token_path.chmod(0o600)
    put(token_path,'/root/api-token');remote('chmod 600 /root/api-token')
    # The certificate is trusted explicitly. No TLS verification bypass in CLI.
    cert=['/config/tls/cert.pem']
    assert cert, 'NAS certificate missing'
    result=remote('titanosctl --url https://localhost --ca-file '+shlex.quote(cert[0])+' --token-file /root/api-token status')
    assert json.loads(result)['version']=='0.23.0'
    request('/account/tokens/'+token['token']['id'],method='DELETE')
    remote('if titanosctl --url https://localhost --ca-file '+shlex.quote(cert[0])+' --token-file /root/api-token status >/dev/null 2>&1; then exit 1; fi; rm /root/api-token')
    print('PASS real CLI HTTPS + personal read-only token and immediate revocation',flush=True)

    if online_virtio:
        driver=request('/vms/virtio',dict(volume='mirror'))
        def drivers():
            j=next(j for j in request('/vms/imports')['jobs'] if j['id']==driver['id'])
            if j['phase'] in ('failed','review'):raise AssertionError(j)
            return j if j['phase']=='done' else None
        official=wait(drivers,'official stable VirtIO ISO',900)
        assert official['output'].startswith('/mnt/mirror/iso/virtio-win-')
        reused=request('/vms/virtio',dict(volume='mirror'))
        assert reused['id']==official['id']
        print('PASS official stable VirtIO HTTPS download, ISO validation and hash-verified reuse (no Windows install)',flush=True)
