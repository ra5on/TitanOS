"""A re-registered, same-name pool never becomes an old photo library's data."""
import unittest
import tempfile
from pathlib import Path
from unittest.mock import patch

from titan.core import Error
from titan.storage_locations import StorageLocations
import tests.test_storage_locations as storage_fixture


class PhotoStorageIdentityTests(unittest.TestCase):
    def test_demo_uses_same_pinned_library_upload_read_trash_restore_contract(self):
        from PIL import Image
        from titan.server import Application
        with tempfile.TemporaryDirectory() as directory:
            app = Application(Path(directory) / 'web', demo=True)
            self.addCleanup(app.stop.set)
            self.addCleanup(app.agent._temporary.cleanup)
            app.store.create_user('photo-admin', 'photos-long-test-password', 'admin', 'titan-files')
            actor = app.store.user_record('photo-admin')
            app.photos.schedule = lambda *_args: {'queued': True}
            source = next(row for row in app.photos_sources(actor) if row['id'] == 'share:dokumente')
            self.assertEqual(source['storage_uuid'], 'demo:' + source['storage_id'])
            Image.new('RGB', (24, 16), '#339aff').save(app.agent._share_paths['dokumente'] / 'Foto.jpg')
            key = app.photos.add_library(actor['name'], 'Familie', source['share'], '', source['storage_id'], source['storage_uuid'])['library']
            app.photos.scan(actor['name'], key)
            photo = app.photos.gallery(actor['name'], app.photos.libraries(actor['name']))['items'][0]
            self.assertTrue(app.photos.cache_path(photo['id']).is_file())
            app.photos.trash(actor['name'], photo['id'])
            app.photos.trash(actor['name'], photo['id'], restore=True)
            self.assertTrue((app.agent._share_paths['dokumente'] / 'Foto.jpg').is_file())

    def test_persisted_identity_is_checked_for_both_privileged_file_boundaries(self):
        fixture = storage_fixture.NamedStorageTests('test_shared_ids_labels_and_programs_do_not_expose_os_root')
        fixture.setUp()
        self.addCleanup(fixture.temp.cleanup)
        pool = fixture.pool()
        share = pool / 'pictures'
        share.mkdir()
        (share / 'a.jpg').write_bytes(b'old original')
        fixture.host._storage_locations = fixture.model
        original = fixture.model.validate_path(share)
        self.assertEqual(original['uuid'], 'zfs:' + fixture.guid)
        fixture.host.save('shares', [{'name': 'pictures', 'path': str(share), 'readers': [], 'writers': []}])
        # The administrator deliberately unregisters the old pool and registers
        # a replacement under exactly the same path and friendly name.
        fixture.guid = '987654321'
        fixture.host.save('pools', [{'name': 'photos', 'guid': fixture.guid}])
        current = fixture.model.validate_path(share)
        self.assertNotEqual(original['uuid'], current['uuid'])
        with patch('titan.host.subprocess.run') as worker:
            for operation in (
                    lambda: fixture.host.op_admin_file('pictures', 'read', 'a.jpg', expected_storage=original['id'], expected_uuid=original['uuid']),
                    lambda: fixture.host.op_system_file('read', str(share / 'a.jpg').lstrip('/'), expected_storage=original['id'], expected_uuid=original['uuid'])):
                with self.assertRaises(Error) as caught:
                    operation()
                self.assertEqual(caught.exception.status, 503)
            worker.assert_not_called()
        self.assertEqual((share / 'a.jpg').read_bytes(), b'old original')

    def test_external_identity_requires_uuid_and_internal_data_alias_is_exact(self):
        for resource, storage, uuid in (({'id': 'volume:photos', 'uuid': 'new'}, 'volume:photos', ''),
                                        ({'id': 'system'}, 'volume:photos', 'known'),
                                        ({'id': 'volume:photos', 'uuid': 'new'}, 'volume:photos', 'old')):
            with self.assertRaises(Error):
                StorageLocations.assert_identity(resource, storage, uuid)
        StorageLocations.assert_identity({'id': 'system'}, 'system', '')


if __name__ == '__main__':
    unittest.main()
