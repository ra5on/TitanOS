"""Photo metadata and cached previews never bypass login, CSRF or current ACLs."""
from http.server import ThreadingHTTPServer
import io
import json
from pathlib import Path
import tempfile
import threading
import time
import unittest
import urllib.error
import urllib.request

from titan.core import Error
from titan.files import operate
from titan.server import Application, Handler


class PhotosHTTPTests(unittest.TestCase):
    def setUp(self):
        from PIL import Image
        self.temporary = tempfile.TemporaryDirectory()
        root = Path(self.temporary.name)
        self.files = root / 'pictures'
        self.files.mkdir()
        Image.new('RGB', (32, 20), '#349eff').save(self.files / 'Family.jpg')
        self.app = Application(root / 'web')
        for name, role in (('alice', 'user'), ('bob', 'user'), ('admin', 'admin')):
            self.app.store.create_user(name, name + '-long-test-password', role, name)
        self.allowed = True
        self.storage_uuid = 'original-filesystem-uuid'
        self.app.agent = self
        self.server = ThreadingHTTPServer(('127.0.0.1', 0), Handler)
        self.server.app = self.app
        self.thread = threading.Thread(target=self.server.serve_forever, kwargs={'poll_interval': .01}, daemon=True)
        self.thread.start()
        self.url = 'http://127.0.0.1:' + str(self.server.server_port)
        self.tokens = {name: self.app.store.login(name, name + '-long-test-password') for name in ('alice', 'bob', 'admin')}

    def tearDown(self):
        self.app.stop.set()
        if self.app.photos.worker:
            self.app.photos.worker.join(timeout=3)
        self.server.shutdown()
        self.server.server_close()
        self.thread.join()
        self.temporary.cleanup()

    def call(self, operation, **arguments):
        if operation == 'shares':
            return [{'name': 'pictures', 'path': str(self.files), 'readers': [], 'writers': ['alice'] if self.allowed else []}]
        if operation == 'storage_locations':
            return {'storage': [{'id': 'volume:photos', 'uuid': self.storage_uuid, 'label': 'Fotospeicher', 'path': str(self.files.parent), 'available': True, 'capabilities': ['files']}, {'id': 'system', 'label': 'Interner Speicher', 'path': '/var/srv/titan', 'available': True, 'capabilities': ['files']}]}
        if operation in ('file', 'admin_file'):
            user = arguments.pop('user', 'admin')
            if operation == 'file' and (not self.allowed or user != 'alice'):
                raise Error('Freigabe verweigert', 403)
            if arguments.pop('share') != 'pictures':
                raise Error('Unknown share', 404)
            from titan.storage_locations import StorageLocations
            StorageLocations.assert_identity({'id': 'volume:photos', 'uuid': self.storage_uuid},
                                             arguments.pop('expected_storage', None), arguments.pop('expected_uuid', None))
            try:
                return operate(self.files, arguments.pop('action'), arguments.pop('path', ''), **arguments)
            except OSError as exc:
                raise Error(str(exc)) from None  # Production file worker RPC contract.
        raise Error('Unexpected operation')

    def request(self, path, body=None, actor='alice', csrf=True):
        headers = {'Content-Type': 'application/json'}
        if actor:
            token, code = self.tokens[actor]
            headers['Cookie'] = 'titan_session=' + token
            if csrf:
                headers['X-CSRF-Token'] = code
        request = urllib.request.Request(self.url + path, data=json.dumps(body).encode() if body is not None else None, headers=headers)
        try:
            with urllib.request.urlopen(request) as response:
                return response.status, response.read(), dict(response.headers)
        except urllib.error.HTTPError as response:
            return response.code, response.read(), dict(response.headers)

    def library(self):
        status, raw, _ = self.request('/api/photos', {'action': 'library_add', 'source': 'share:pictures', 'path': '', 'name': 'Familie'})
        self.assertEqual(status, 200, raw)
        deadline = time.monotonic() + 5
        while time.monotonic() < deadline:
            status, raw, _ = self.request('/api/photos')
            gallery = json.loads(raw)
            if gallery['items'] and not gallery['indexing']:
                return gallery['items'][0], json.loads(raw)['libraries'][0]
            time.sleep(.02)
        self.fail('Background photo scan did not finish')

    def test_worker_produces_authenticated_private_preview_and_original(self):
        photo, _ = self.library()
        status, raw, headers = self.request('/api/photos/preview?photo=' + photo['id'])
        self.assertEqual(status, 200, raw)
        self.assertTrue(raw.startswith(b'\xff\xd8'))
        self.assertEqual(headers['Content-Type'], 'image/jpeg')
        self.assertEqual(headers['Cache-Control'], 'no-store')
        self.assertEqual(self.request('/api/photos/preview?photo=' + photo['id'], actor=None)[0], 401)
        self.assertEqual(self.request('/api/photos/preview?photo=' + photo['id'], actor='bob')[0], 404)
        status, raw, headers = self.request('/api/photos/original?photo=' + photo['id'] + '&preview=1')
        self.assertEqual(status, 200, raw)
        self.assertEqual(raw, (self.files / 'Family.jpg').read_bytes())
        self.assertEqual(headers['Content-Type'], 'image/jpeg')
        self.assertEqual(self.request('/api/photos/original?photo=' + photo['id'], actor='bob')[0], 404)

    def test_storage_replaced_under_same_name_blocks_preview_download_upload_and_trash(self):
        photo, library = self.library()
        self.storage_uuid = 'replacement-filesystem-uuid'
        for path in ('/api/photos/preview?photo=', '/api/photos/original?photo='):
            self.assertEqual(self.request(path + photo['id'])[0], 503)
        upload = {'library': library['id'], 'name': 'new.jpg', 'offset': 0, 'data': 'eA=='}
        self.assertEqual(self.request('/api/photos/upload', upload)[0], 503)
        self.assertEqual(self.request('/api/photos', {'action': 'trash', 'photo': photo['id']})[0], 503)
        status, raw, _ = self.request('/api/photos')
        self.assertEqual(status, 200)
        self.assertEqual(json.loads(raw)['items'], [])
        self.assertFalse((self.files / 'new.jpg').exists())
        self.assertTrue((self.files / 'Family.jpg').exists())

    def test_photo_upload_is_private_chunked_and_does_not_overwrite(self):
        import base64
        _, library = self.library()
        body = {'library': library['id'], 'name': 'new.png', 'offset': 0, 'data': base64.b64encode(b'first').decode()}
        self.assertEqual(self.request('/api/photos/upload', body, csrf=False)[0], 403)
        self.assertEqual(self.request('/api/photos/upload', body, actor='bob')[0], 404)
        status, raw, _ = self.request('/api/photos/upload', body)
        self.assertEqual(status, 200, raw)
        body.update(offset=5, data=base64.b64encode(b'second').decode())
        self.assertEqual(self.request('/api/photos/upload', body)[0], 200)
        self.assertEqual((self.files / 'new.png').read_bytes(), b'firstsecond')
        body['offset'] = 0
        self.assertEqual(self.request('/api/photos/upload', body)[0], 400)

    def test_scan_requested_while_initial_scan_finishes_runs_bounded_followup(self):
        import base64
        finished, release = threading.Event(), threading.Event()
        real_scan = self.app.photos.scan
        scans = []
        def held_scan(owner, library):
            scans.append(library)
            real_scan(owner, library)
            if len(scans) == 1:
                finished.set()
                release.wait(3)
        self.app.photos.scan = held_scan
        try:
            status, raw, _ = self.request('/api/photos', {'action':'library_add','name':'Familie','source':'share:pictures','path':''})
            self.assertEqual(status,200,raw)
            library = json.loads(raw)['library']
            self.assertTrue(finished.wait(3))
            image = (self.files / 'Family.jpg').read_bytes()
            self.assertEqual(self.request('/api/photos/upload', {'library':library,'name':'Second.jpg','offset':0,
                                                                  'data':base64.b64encode(image).decode()})[0],200)
            self.assertEqual(self.request('/api/photos', {'action':'scan','library':library})[0],200)
            release.set()
            deadline = time.monotonic() + 5
            while time.monotonic() < deadline:
                status, raw, _ = self.request('/api/photos')
                value = json.loads(raw)
                if value.get('total') == 2 and not value.get('indexing'):
                    break
                time.sleep(.02)
            self.assertEqual(value['total'],2)
            self.assertEqual(len(scans),2,'Repeated requests retain one bounded follow-up scan.')
        finally:
            release.set()

    def test_revoking_share_blocks_cached_preview_and_gallery(self):
        photo, _ = self.library()
        self.allowed = False
        self.assertEqual(self.request('/api/photos/preview?photo=' + photo['id'])[0], 403)
        status, raw, _ = self.request('/api/photos')
        self.assertEqual(status, 200, raw)
        self.assertEqual(json.loads(raw)['items'], [])
        self.assertFalse(json.loads(raw)['libraries'][0]['available'])

    def test_every_mutation_requires_csrf_and_current_file_application_permission(self):
        body = {'action': 'album_create', 'name': 'Test'}
        self.assertEqual(self.request('/api/photos', body, csrf=False)[0], 403)
        self.app.store.set_config('identity', {'schema': 1, 'groups': [], 'users': {'alice': {'applications': {'files': False}, 'shares': {}}}})
        self.assertEqual(self.request('/api/photos', body)[0], 403)
        self.assertEqual(self.request('/api/photos')[0], 403)

    def test_system_paths_and_unknown_options_are_rejected(self):
        body = {'action': 'library_add', 'name': 'Bad', 'source': 'storage:system', 'path': 'etc'}
        self.assertEqual(self.request('/api/photos', body, actor='admin')[0], 403)
        self.assertEqual(self.request('/api/photos', body, actor='alice')[0], 403)
        self.assertEqual(self.request('/api/photos?recursive=1')[0], 400)
        self.assertEqual(self.request('/api/photos', {'action': 'album_create', 'name': 'Test', 'owner': 'bob'})[0], 400)

    def test_original_revision_change_invalidates_cached_thumbnail(self):
        from PIL import Image
        photo, _ = self.library()
        Image.new('RGB', (15, 25), '#ee2211').save(self.files / 'Family.jpg')
        status, raw, _ = self.request('/api/photos/preview?photo=' + photo['id'])
        self.assertEqual(status, 202, raw)
        self.assertIn(b'<svg', raw)

    def test_disabling_account_revokes_preview_session_and_background_file_right(self):
        photo, _ = self.library()
        self.app.store.update_user('alice', enabled=False)
        self.assertEqual(self.request('/api/photos/preview?photo=' + photo['id'])[0], 401)
        with self.assertRaises(Error) as exc:
            self.app.photos_file_call('alice', share='pictures', path='Family.jpg', action='read', size=1)
        self.assertEqual(exc.exception.status, 403)


if __name__ == '__main__':
    unittest.main()
