"""Exercise the image-smoke Photos flow against real authenticated HTTP routes."""
from http.server import ThreadingHTTPServer
import importlib.util
import json
from pathlib import Path
import tempfile
import threading
from types import SimpleNamespace
import unittest
import urllib.error
import urllib.request
from unittest.mock import Mock, patch

from titan.server import Application, Handler

spec = importlib.util.spec_from_file_location('photos_runtime_smoke', Path(__file__).resolve().parents[1] / 'scripts/smoke-runtime.py')
smoke = importlib.util.module_from_spec(spec)
spec.loader.exec_module(smoke)


class LocalClient:
    def __init__(self, url, session=None):
        self.url, self.session = url, session
        self.original_substitution = False

    def unauthenticated_client(self):
        return LocalClient(self.url)

    def request(self, path, body=None, expected_status=None, raw=False):
        headers = {'Content-Type':'application/json'}
        if self.session:
            headers.update(Cookie='titan_session=' + self.session[0], **{'X-CSRF-Token':self.session[1]})
        request = urllib.request.Request(self.url + path, data=json.dumps(body).encode() if body is not None else None, headers=headers)
        try:
            response = urllib.request.urlopen(request, timeout=10)
        except urllib.error.HTTPError as response_error:
            response = response_error
        with response:
            status, content, metadata = response.status, response.read(1024**2 + 1), response.headers
        if expected_status is not None and status != expected_status or expected_status is None and not 200 <= status < 300:
            raise smoke.SmokeFailure('Test API returned HTTP ' + str(status) + '.')
        if raw:
            return {'status':status,'data':b'private substituted bytes' if self.original_substitution and '/original?' in path else content,
                    'content_type':metadata.get('Content-Type'),'cache_control':metadata.get('Cache-Control')}
        return json.loads(content)


class PhotosRuntimeSmokeTests(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.app = Application(Path(self.directory.name) / 'web', demo=True)
        self.demo_agent = self.app.agent
        # Use real sessions/CSRF and the same HTTP Photos code; only the
        # privileged file service is confined to a disposable DATA fixture.
        self.app.demo = False
        def service(operation, **arguments):
            from titan.storage_locations import StorageLocations
            storage = arguments.pop('expected_storage', None)
            uuid = arguments.pop('expected_uuid', None)
            if storage is not None or uuid is not None:
                StorageLocations.assert_identity({'id':'system'}, storage, uuid)
            return self.demo_agent.call(operation, **arguments)
        self.app.agent = SimpleNamespace(call=service)
        self.app.store.create_user('admin', 'photos-long-test-password', 'admin', 'titan-files')
        self.server = ThreadingHTTPServer(('127.0.0.1', 0), Handler)
        self.server.app = self.app
        self.thread = threading.Thread(target=self.server.serve_forever, kwargs={'poll_interval':.01}, daemon=True)
        self.thread.start()
        session = self.app.store.login('admin','photos-long-test-password')
        self.client = LocalClient('http://127.0.0.1:' + str(self.server.server_port), session)

    def tearDown(self):
        self.app.stop.set()
        if self.app.photos.worker:
            self.app.photos.worker.join(timeout=3)
        self.server.shutdown(); self.server.server_close(); self.thread.join()
        self.demo_agent._temporary.cleanup(); self.directory.cleanup()

    def test_actual_http_upload_index_previews_original_album_favorite_trash_restore_cleanup(self):
        values = smoke.RuntimeSmoke(self.client).photos()
        self.assertEqual(len(values),12)
        self.assertTrue(all(value is True for value in values.values()))
        self.assertEqual(self.app.photos.libraries('admin'), [])
        self.assertEqual(self.app.photos.albums('admin'), [])
        data = self.demo_agent._system_path / 'var/srv/titan'
        self.assertFalse(list(data.glob('titan-smoke-photos-*')))
        self.assertNotIn(str(data), json.dumps(values))

    def test_wrong_original_bytes_fail_and_cleanup_without_publishing_content(self):
        self.client.original_substitution = True
        with self.assertRaises(smoke.SmokeFailure) as caught:
            smoke.RuntimeSmoke(self.client).photos()
        self.assertIn('does not match', str(caught.exception))
        self.assertNotIn('private', str(caught.exception))
        self.assertEqual(self.app.photos.libraries('admin'), [])


class InstallResponsivenessTests(unittest.TestCase):
    def test_running_install_job_probes_authenticated_listing_and_status_before_completion(self):
        client = smoke.GuestClient()
        client.measure_install_responsiveness = True
        client.request = Mock(side_effect=[{'job':'job'}, [{'id':'job','status':'running'}],
            {'path':'var/srv/titan','entries':[]}, {'demo':False,'memory_total':8*1024**3-1024**2,'memory_available':1024**3},
            [{'id':'job','status':'completed','result':{'ok':True}}]])
        with patch.object(smoke.time, 'sleep'):
            self.assertEqual(client.action('app_install',{'app':'heimdall'}), {'ok':True})
        client.request.assert_any_call('/api/files?share=%40system&path=', timeout=8)
        client.request.assert_any_call('/api/status', timeout=8)
        values = smoke.RuntimeSmoke(client).installation_responsiveness()
        self.assertEqual(values['active_install_samples'],1)
        self.assertNotIn('job',json.dumps(values))

    def test_completed_without_running_sample_and_bad_status_cannot_pass(self):
        client = smoke.GuestClient()
        with self.assertRaises(smoke.SmokeFailure):
            smoke.RuntimeSmoke(client).installation_responsiveness()
        for changed in ({'demo':True},{'memory_total':16*1024**3},{'memory_available':0}):
            status = {'demo':False,'memory_total':8*1024**3-1024**2,'memory_available':1024**3,**changed}
            client.request = Mock(side_effect=[{'path':'var/srv/titan','entries':[]},status])
            with self.subTest(changed=changed),self.assertRaises(smoke.SmokeFailure):
                client.probe_install_responsiveness()


if __name__ == '__main__':
    unittest.main()
