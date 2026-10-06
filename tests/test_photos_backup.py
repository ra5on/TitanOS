"""Photo annotations participate in real NAS archives and config rollback."""
import copy
import contextlib
import json
from pathlib import Path
import sqlite3
import tarfile
import threading
import unittest
from unittest.mock import Mock, patch

from titan.config_restore import restore
from titan.core import Error
from titan.files import operate
from titan.photos import Photos
from titan.photos_backup import export_metadata, import_metadata, validate_metadata
import tests.test_backups as backup_fixture


class PhotosBackupTests(unittest.TestCase):
    def setUp(self):
        self.fixture = backup_fixture.BackupTests('test_config_snapshot_is_consistent_and_revoke_sessions')
        self.fixture.setUp()
        f = self.fixture
        self.stop = threading.Event()
        self.photos = Photos(f.store.directory, self.file_call, self.stop,
            processor=lambda file, modified: (b'private-preview', {'width': 4, 'height': 3, 'taken': modified}))
        self.photos.schedule = lambda *_args: {'queued': True}
        (f.host.share_root / 'data/favorite.jpg').write_bytes(b'precious photo')
        (f.host.share_root / 'data/ordinary.jpg').write_bytes(b'ordinary photo')
        self.library = self.photos.add_library('admin', 'Familie', 'data', storage_id='system')['library']
        self.photos.scan('admin', self.library)
        self.photo = next(row for row in self.photos.gallery('admin', self.photos.libraries('admin'))['items'] if row['name'] == 'favorite.jpg')
        self.album = self.photos.album_create('admin', 'Urlaub')['album']
        self.photos.annotate('admin', self.photo['id'], favorite=True, album=self.album)

    def tearDown(self):
        self.stop.set()
        self.fixture.tearDown()

    def file_call(self, owner, **args):
        self.assertEqual(owner, 'admin')
        self.assertEqual(args.pop('share'), 'data')
        self.assertEqual(args.pop('expected_storage', None), 'system')
        self.assertEqual(args.pop('expected_uuid', None), '')
        return operate(self.fixture.host.share_root / 'data', args.pop('action'), args.pop('path', ''), **args)

    def metadata(self):
        f = self.fixture
        return export_metadata(f.store.directory, f.store.users(), f.host.op_shares(), f.host)

    def test_real_archive_roundtrip_preserves_albums_favorites_and_originals(self):
        f = self.fixture
        backup = f.backups.create()
        settings = f.backups.read_config(backup['id'])
        self.assertEqual(settings['photos']['libraries'][0]['name'], 'Familie')
        self.assertEqual(len(settings['photos']['photos']), 1, 'Ordinary image index rows are regenerated, not backed up.')
        archive = f.backups.namespace() / backup['id'] / 'archive.tar.gz'
        with tarfile.open(archive, 'r:gz') as file:
            self.assertFalse(any('thumbnail' in name or 'photos.sqlite' in name for name in file.getnames()))
        self.photos.annotate('admin', self.photo['id'], favorite=False)
        self.photos.album_remove('admin', self.album)
        self.assertTrue(restore(f.host, settings, f.store.path, Mock())['ok'])
        self.assertEqual(self.metadata(), settings['photos'])
        self.assertEqual((f.host.share_root / 'data/favorite.jpg').read_bytes(), b'precious photo')
        self.assertFalse(self.photos.cache_path(self.photo['id']).exists())
        self.assertEqual(self.photos.libraries('admin')[0]['state'], 'restored')
        self.photos.scan('admin', self.library)
        favorites = self.photos.gallery('admin', self.photos.libraries('admin'), album=self.album, favorite=True)
        self.assertEqual(favorites['items'][0]['id'], self.photo['id'])
        self.assertEqual(favorites['items'][0]['size'], len(b'precious photo'))
        self.assertEqual(self.photos.gallery('admin', self.photos.libraries('admin'))['total'], 2)

    def test_failure_after_photo_import_rolls_back_previous_photo_settings(self):
        f = self.fixture
        settings = f.backups.read_config(f.backups.create()['id'])
        self.photos.annotate('admin', self.photo['id'], favorite=False)
        kept_album = self.photos.album_create('admin', 'Behalten')['album']
        previous = self.metadata()
        count = 0
        def fail_once(directory, value, owner):
            nonlocal count
            import_metadata(directory, value, owner)
            count += 1
            if count == 1:
                raise Error('Simulated interruption after photo import')
        with patch('titan.photos_backup.import_metadata', side_effect=fail_once), self.assertRaises(Error):
            restore(f.host, settings, f.store.path, Mock())
        self.assertEqual(self.metadata(), previous)
        self.assertTrue(any(row['id'] == kept_album for row in self.photos.albums('admin')))
        self.assertFalse(self.photos.photo('admin', self.photo['id'])['favorite'])

    def test_restored_tombstone_uses_content_identity_and_rejects_substituted_file(self):
        f = self.fixture
        self.photos.trash('admin', self.photo['id'])
        value = self.metadata()
        photo = value['photos'][0]
        self.assertEqual(len(photo['content_sha256']), 64)
        saved_trash = f.host.share_root / 'data' / photo['trash_path']
        original = saved_trash.read_bytes()
        import_metadata(f.store.directory, value, f.store.path.stat())
        saved_trash.write_bytes(b'a substituted image')
        with self.assertRaisesRegex(Error, 'ersetzt oder verändert'):
            self.photos.trash('admin', photo['id'], restore=True)
        self.assertFalse((f.host.share_root / 'data/favorite.jpg').exists())
        saved_trash.write_bytes(original)  # changed inode/revision after file backup is fine
        self.photos.trash('admin', photo['id'], restore=True)
        self.assertEqual((f.host.share_root / 'data/favorite.jpg').read_bytes(), original)

    def test_forged_paths_owner_membership_and_schema_are_rejected(self):
        value = self.metadata()
        f = self.fixture
        for change in ('path', 'owner', 'member', 'unknown'):
            broken = copy.deepcopy(value)
            if change == 'path':
                broken['photos'][0]['path'] = '../etc/shadow'
            elif change == 'owner':
                broken['libraries'][0]['owner'] = 'root'
            elif change == 'member':
                broken['members'][0]['photo'] = 'f' * 64
            else:
                broken['execute'] = 'shell'
            with self.subTest(change=change), self.assertRaises(Error):
                validate_metadata(broken, f.store.users(), f.host.op_shares(), f.host)
        with self.photos.connection() as db:
            db.execute('CREATE TRIGGER unexpected AFTER UPDATE ON libraries BEGIN SELECT 1; END')
        with self.assertRaises(Error):
            self.metadata()

    def test_same_storage_path_with_replacement_uuid_is_rejected_before_restore(self):
        f = self.fixture
        value = self.metadata()
        value['libraries'][0].update(storage_id='volume:photos', storage_uuid='original-uuid')
        resource = {'id': 'volume:photos', 'path': str(f.host.share_root), 'uuid': 'original-uuid', 'capabilities': ['files']}
        with patch.object(f.host, 'op_storage_locations', return_value={'storage': [resource]}):
            validate_metadata(value, f.store.users(), f.host.op_shares(), f.host)
            resource['uuid'] = 'replacement-uuid'
            with self.assertRaisesRegex(Error, 'Speicherkennung'):
                validate_metadata(value, f.store.users(), f.host.op_shares(), f.host)
            value['libraries'][0]['storage_uuid'] = ''
            with self.assertRaises(Error):
                validate_metadata(value, f.store.users(), f.host.op_shares(), f.host)
    def test_snapshot_rejects_symlink_and_record_limit_without_changing_originals(self):
        f = self.fixture
        metadata = self.photos.directory / 'metadata.sqlite3'
        moved = metadata.with_name('private.sqlite3')
        metadata.rename(moved)
        metadata.symlink_to(moved)
        with self.assertRaises(Error):
            self.metadata()
        metadata.unlink()
        moved.rename(metadata)
        with patch('titan.photos_backup.MAX_ANNOTATIONS', 0), self.assertRaises(Error):
            self.metadata()
        self.assertEqual((f.host.share_root / 'data/favorite.jpg').read_bytes(), b'precious photo')


if __name__ == '__main__':
    unittest.main()
