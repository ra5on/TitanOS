"""Photo indexing preserves originals and uses the scoped file service throughout."""
import base64
import io
from pathlib import Path
import tempfile
import threading
import unittest
from unittest.mock import patch

from titan.core import Error
from titan.files import operate
from titan.photos import Photos, capture_date, prepare_thumbnail, relative_path


class PhotosTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.root = Path(self.temporary.name)
        self.files = self.root / 'files'
        self.files.mkdir()
        self.stopped = threading.Event()
        self.allowed = True
        self.calls = []
        self.photos = Photos(self.root / 'state', self.call, self.stopped,
            processor=lambda file, modified: (b'private-preview', {'width': 20, 'height': 10, 'taken': modified}))
        # Deterministic tests invoke scans themselves; worker integration has HTTP tests.
        self.photos.schedule = lambda owner, key, photo=None: {'queued': True}

    def tearDown(self):
        self.stopped.set()
        self.temporary.cleanup()

    def call(self, owner, **arguments):
        self.calls.append((owner, dict(arguments)))
        if not self.allowed or owner != 'alice' or arguments.pop('share') != 'pictures':
            raise Error('No access', 403)
        return operate(self.files, arguments.pop('action'), arguments.pop('path', ''), **arguments)

    def library(self, name='Familie', path=''):
        return self.photos.add_library('alice', name, 'pictures', path)['library']

    def scan(self, key):
        self.photos.scan('alice', key)
        return self.photos.gallery('alice', self.photos.libraries('alice'))

    def test_nested_index_skips_links_hidden_and_nonimages_and_paginates(self):
        (self.files / 'Urlaub').mkdir()
        (self.files / '.private').mkdir()
        (self.files / '.private/hidden.jpg').write_bytes(b'private')
        (self.files / 'Urlaub/note.txt').write_text('not a photo')
        outside = self.root / 'outside.jpg'
        outside.write_bytes(b'secret')
        (self.files / 'linked.jpg').symlink_to(outside)
        for number in range(105):
            (self.files / f'Urlaub/{number:03d}.jpg').write_bytes(b'image')
        key = self.library()
        result = self.scan(key)
        self.assertEqual(result['total'], 105)
        self.assertEqual(len(result['items']), 80)
        self.assertTrue(result['has_more'])
        next_page = self.photos.gallery('alice', self.photos.libraries('alice'), offset=80)
        self.assertEqual(len(next_page['items']), 25)
        self.assertFalse(next_page['has_more'])
        self.assertTrue(all(call[1].get('path', '').startswith('Urlaub') or not call[1].get('path') for call in self.calls))

    def test_albums_and_favorites_persist_without_copying_original_files(self):
        original = self.files / 'portrait.jpg'
        original.write_bytes(b'original-image')
        key = self.library()
        photo = self.scan(key)['items'][0]
        album = self.photos.album_create('alice', 'Familienalbum')['album']
        self.photos.annotate('alice', photo['id'], favorite=True, album=album)
        restarted = Photos(self.root / 'state', self.call, self.stopped)
        result = restarted.gallery('alice', restarted.libraries('alice'), album=album, favorite=True)
        self.assertEqual(result['items'][0]['id'], photo['id'])
        self.assertEqual(original.read_bytes(), b'original-image')
        self.assertEqual(len(list(self.files.iterdir())), 1)
        self.photos.remove_library('alice', key)
        self.assertEqual(original.read_bytes(), b'original-image')
        self.assertFalse(self.photos.cache_path(photo['id']).exists())

    def test_trash_restore_keeps_original_content_and_favorite(self):
        original = self.files / 'moment.jpg'
        original.write_bytes(b'original')
        photo = self.scan(self.library())['items'][0]
        self.photos.annotate('alice', photo['id'], favorite=True)
        self.photos.trash('alice', photo['id'])
        self.assertFalse(original.exists())
        self.assertEqual(self.photos.gallery('alice', self.photos.libraries('alice'))['total'], 0)
        self.assertEqual(self.photos.gallery('alice', self.photos.libraries('alice'), trash=True)['total'], 1)
        self.photos.trash('alice', photo['id'], restore=True)
        self.assertEqual(original.read_bytes(), b'original')
        self.assertTrue(self.photos.photo('alice', photo['id'])['favorite'])

    def test_failed_scan_does_not_prune_existing_metadata_and_revoked_rights_block_actions(self):
        (self.files / 'a.jpg').write_bytes(b'image')
        key = self.library()
        photo = self.scan(key)['items'][0]
        self.allowed = False
        with self.assertRaises(Error):
            self.photos.scan('alice', key)
        self.assertEqual(self.photos.photo('alice', photo['id'])['name'], 'a.jpg')
        for operation in (lambda: self.photos.annotate('alice', photo['id'], favorite=True),
                          lambda: self.photos.trash('alice', photo['id'])):
            with self.assertRaises(Error) as exc:
                operation()
            self.assertEqual(exc.exception.status, 403)
        self.assertTrue((self.files / 'a.jpg').exists())

    def test_changed_original_or_replaced_tombstone_cannot_be_deleted_or_restored(self):
        original = self.files / 'a.jpg'
        original.write_bytes(b'original')
        key = self.library()
        photo = self.scan(key)['items'][0]
        original.write_bytes(b'changed original')
        with self.assertRaisesRegex(Error, 'verändert'):
            self.photos.trash('alice', photo['id'])
        self.scan(key)
        self.photos.trash('alice', photo['id'])
        record = self.photos.photo('alice', photo['id'])
        (self.files / record['trash_path']).write_bytes(b'substitute')
        with self.assertRaisesRegex(Error, 'ersetzt oder verändert'):
            self.photos.trash('alice', photo['id'], restore=True)
        self.assertFalse(original.exists())

    def test_user_cannot_read_another_users_photo_or_album(self):
        (self.files / 'a.jpg').write_bytes(b'image')
        photo = self.scan(self.library())['items'][0]
        with self.assertRaises(Error) as exc:
            self.photos.photo('bob', photo['id'])
        self.assertEqual(exc.exception.status, 404)
        album = self.photos.album_create('alice', 'Privat')['album']
        with self.assertRaises(Error):
            self.photos.gallery('bob', [{'id': photo['library']}], album=album)

    def test_oversized_original_is_not_decoded_and_remains_downloadable(self):
        (self.files / 'large.jpg').write_bytes(b'image')
        with patch('titan.photos.MAX_FILE_BYTES', 4):
            photo = self.scan(self.library())['items'][0]
        self.assertIn('64 MiB', photo['error'])
        self.assertFalse(self.photos.cache_path(photo['id']).exists())
        self.assertEqual((self.files / 'large.jpg').read_bytes(), b'image')

    def test_thumbnail_reads_are_chunked_with_revision_checks(self):
        (self.files / 'large.jpg').write_bytes(b'x' * (2 * 1024**2 + 31))
        photo = self.scan(self.library())['items'][0]
        reads = [args for _, args in self.calls if args['action'] == 'read']
        self.assertGreaterEqual(len(reads), 4)
        self.assertLessEqual(max(row['size'] for row in reads), 1024**2)
        self.assertEqual(self.photos.cache_path(photo['id']).read_bytes(), b'private-preview')
        self.assertTrue(photo['revision'])

    def test_revision_change_while_reading_never_publishes_preview(self):
        (self.files / 'a.jpg').write_bytes(b'image')
        real_call = self.photos.file_call
        count = 0
        def changed(owner, **arguments):
            nonlocal count
            result = real_call(owner, **arguments)
            if arguments['action'] == 'read':
                count += 1
                if count > 1:
                    result['revision'] = 'different'
            return result
        self.photos.file_call = changed
        photo = self.scan(self.library())['items'][0]
        self.assertIn('verändert', photo['error'])
        self.assertFalse(self.photos.cache_path(photo['id']).exists())

    def test_literal_search_and_invalid_storage_paths(self):
        for name in ('100%.jpg', '100x.jpg'):
            (self.files / name).write_bytes(b'image')
        self.scan(self.library())
        self.assertEqual(self.photos.gallery('alice', self.photos.libraries('alice'), search='%')['total'], 1)
        for path in ('../secret', '/etc', 'a/../../secret', '.titan-trash', 'folder\\file', 'folder\0file'):
            with self.assertRaises(Error):
                relative_path(path)


class ThumbnailTests(unittest.TestCase):
    def test_exif_orientation_and_capture_date_are_used_without_metadata_in_preview(self):
        from PIL import Image
        source = io.BytesIO()
        image = Image.new('RGB', (40, 20), '#249acc')
        exif = Image.Exif()
        exif[274] = 6
        exif[36867] = '2023:07:14 12:30:00'
        exif[36881] = '+02:00'
        image.save(source, 'JPEG', exif=exif)
        source.seek(0)
        content, metadata = prepare_thumbnail(source, 1700000000)
        preview = Image.open(io.BytesIO(content))
        self.assertEqual(preview.size, (20, 40))
        self.assertNotIn(274, preview.getexif())
        self.assertEqual((metadata['width'], metadata['height']), (40, 20))
        self.assertEqual(metadata['taken'], 1689330600)

    def test_pixel_bomb_limit_and_corrupt_images_leave_no_output(self):
        from PIL import Image
        source = io.BytesIO()
        Image.new('RGB', (40, 40)).save(source, 'PNG')
        source.seek(0)
        with patch('titan.photos.MAX_PIXELS', 100):
            with self.assertRaises(Error) as exc:
                prepare_thumbnail(source, 0)
            self.assertEqual(exc.exception.status, 413)
        with self.assertRaises(Error):
            prepare_thumbnail(io.BytesIO(b'not an image'), 0)

    def test_invalid_exif_date_and_timezone_use_fallback(self):
        self.assertEqual(capture_date({36867: 'not a date'}, 42), 42)
        self.assertEqual(capture_date({36867: '2023:07:14 12:30:00', 36881: '+99:99'}, 42), 42)


if __name__ == '__main__':
    unittest.main()
