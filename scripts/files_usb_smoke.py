"""Exercise USB uploads and text edits on a disposable, real exFAT filesystem."""
import json
import urllib.parse
import urllib.request


def check_usb_files(request, opener, url):
    drive = next(d for d in request('/usb') if d['label'] == 'EXFAT-TEST')
    assert drive['fsType'] == 'exfat' and drive['supported'], drive
    mounted = request('/usb/' + drive['id'] + '/mount', {})
    root = next(d['root'] for d in mounted if d['id'] == drive['id'])
    query = urllib.parse.urlencode({'root': root, 'path': 'settings.json'})
    assert request('/files/uploads?' + urllib.parse.urlencode({'root': root, 'path': ''})) == []
    content = json.dumps({'text': 'a' * 70000}).encode()
    part = request('/files/uploads', {'root': root, 'dir': '', 'name': 'settings.json', 'size': len(content)})
    chunk = urllib.request.Request(url + '/api/v1/files/uploads/' + part['id'] + '?offset=0', data=content,
                                  method='PATCH', headers={'Origin': url, 'Content-Type': 'application/octet-stream'})
    with opener.open(chunk, timeout=90) as response:
        assert json.load(response)['offset'] == len(content)
    request('/files/uploads/' + part['id'] + '/finish', {})
    original = request('/files/text?' + query)
    assert original['content'].encode() == content
    edited = json.dumps({'text': 'b' * 80000})
    result = request('/files/text', {'root': root, 'path': 'settings.json', 'content': edited, 'version': original['version']}, 'PUT')
    assert result['version'] != original['version']
    assert request('/files/text?' + query)['content'] == edited
    try:
        request('/files/text', {'root': root, 'path': 'settings.json', 'content': '{}', 'version': original['version']}, 'PUT')
    except RuntimeError as error:
        assert 'text_conflict' in str(error), error
    else:
        raise AssertionError('stale text overwrite accepted')
    with opener.open(url + '/api/v1/files/download?' + query.replace('settings.json', 'settings.json.prev'), timeout=30) as response:
        assert response.read() == content
    request('/usb/' + drive['id'] + '/eject', {})
    assert not next(d for d in request('/usb') if d['id'] == drive['id'])['mounted']
    print('Real exFAT: mount, upload >64 KiB, atomic finish, text edit, conflict rejection, backup and eject passed', flush=True)


def _format(request, disk, body, time):
    # A storage action of titand (e.g. a share quota round) can hold the lock
    # for a moment; the drive manager then answers usb_busy. Try again briefly.
    for attempt in range(30):
        try:
            request('/usb/disks/' + disk['id'] + '/format', body)
            break
        except RuntimeError as error:
            if 'usb_busy' not in str(error) or attempt == 29:
                raise
            time.sleep(1)
    for _ in range(240):
        job = request('/usb/disks')['job']
        if job and job.get('finished'):
            return job
        time.sleep(0.5)
    raise AssertionError('format did not end')


def check_drive_format(request, remote, volume_members):
    """Format the disposable exFAT drive with the real tools of the image."""
    import time
    listing = request('/usb/disks')
    disks = listing['disks']
    # The boot stick and the disks of the mirror volume are never offered.
    assert not any(d['device'] in volume_members for d in disks), disks
    assert not any(p['label'] == 'TITANOS' for d in disks for p in d['partitions']), disks
    disk = next(d for d in disks if any(p['label'] == 'EXFAT-TEST' for p in d['partitions']))
    assert disk['formattable'] and disk['transport'] == 'usb' and disk['kind'] == 'removable', disk
    device = disk['device']
    job = _format(request, disk, {'filesystem': 'vfat', 'label': 'fmt test', 'scheme': 'auto', 'mount': True}, time)
    assert job['phase'] == 'done' and job['scheme'] == 'mbr' and job['label'] == 'FMT TEST', job
    layout = json.loads(remote(f'lsblk -J -b -o NAME,PTTYPE,FSTYPE,LABEL,PARTTYPE,START {device}'))['blockdevices'][0]
    part = layout['children'][0]
    assert layout['pttype'] == 'dos' and len(layout['children']) == 1, layout
    assert part['fstype'] == 'vfat' and part['label'] == 'FMT TEST' and part['parttype'] == '0xe' and int(part['start']) == 2048, layout
    drive = next(d for d in request('/usb') if d['id'] == job['id'])
    assert drive['mounted'] and not drive['readOnly'], drive
    request('/usb/' + drive['id'] + '/eject', {})
    disk = next(d for d in request('/usb/disks')['disks'] if d['device'] == device)
    job = _format(request, disk, {'filesystem': 'ext4', 'label': 'linux', 'mount': False}, time)
    assert job['phase'] == 'done' and job['scheme'] == 'gpt', job
    layout = json.loads(remote(f'lsblk -J -o NAME,PTTYPE,FSTYPE,LABEL {device}'))['blockdevices'][0]
    assert layout['pttype'] == 'gpt' and layout['children'][0]['fstype'] == 'ext4', layout
    assert not next(d for d in request('/usb') if d['id'] == job['id'])['mounted']
    print('Real drive format: FAT on MBR at 1 MiB (FAT16 type on a 128 MiB stick), mount, eject, ext4 on GPT passed', flush=True)
