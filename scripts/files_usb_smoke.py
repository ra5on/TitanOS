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
