"""Validated photo settings backups; the large image index is disposable.

Take a SQLite online snapshot, export only libraries/albums/annotations, and
import records into our schema. Never execute SQL obtained from an archive.
"""
import contextlib
import hashlib
import os
from pathlib import Path, PurePosixPath
import re
import sqlite3
import tempfile
import threading
import time

from .core import Error
from .photos import Photos, relative_path, title

MAX_DATABASE = 128 * 1024**2
MAX_ANNOTATIONS = 25_000
MAX_COLLECTIONS = 8192
HEX32 = re.compile(r'^[a-f0-9]{32}$')


def _path(value, trash=False):
    if not trash:
        return relative_path(value)
    if (not isinstance(value, str) or len(value) > 4096 or '\0' in value or '\\' in value
            or PurePosixPath(value).is_absolute() or '..' in PurePosixPath(value).parts):
        raise Error('Ungültiger gesicherter Foto-Papierkorb.')
    return value


def validate_metadata(value, users, shares, host=None):
    if (not isinstance(value, dict) or set(value) != {'schema', 'libraries', 'photos', 'albums', 'members'}
            or value['schema'] != 1):
        raise Error('Ungültige gesicherte Fotoeinstellungen.')
    for name, maximum in (('libraries', MAX_COLLECTIONS), ('photos', MAX_ANNOTATIONS), ('albums', MAX_COLLECTIONS), ('members', MAX_ANNOTATIONS * 8)):
        if not isinstance(value[name], list) or len(value[name]) > maximum:
            raise Error('Gesicherte Fotoeinstellungen überschreiten die Sicherheitsgrenze.')
    users = {row['name']: row for row in users}
    shares = {row['name']: row for row in shares}
    libraries, counts, locations = {}, {}, None
    for library in value['libraries']:
        if (not isinstance(library, dict) or set(library) != {'id', 'owner', 'name', 'share', 'path', 'storage_id', 'storage_uuid'}
                or not isinstance(library['id'], str) or not HEX32.fullmatch(library['id']) or library['id'] in libraries
                or library['owner'] not in users or title(library['name']) != library['name']
                or _path(library['path']) != library['path'] or not isinstance(library['storage_id'], str)
                or not library['storage_id'] or len(library['storage_id']) > 128
                or not isinstance(library['storage_uuid'], str) or len(library['storage_uuid']) > 128
                or library['storage_id'] != 'system' and not library['storage_uuid']):
            raise Error('Ungültige gesicherte Fotobibliothek.')
        owner = library['owner']
        counts[owner] = counts.get(owner, 0) + 1
        if counts[owner] > 32:
            raise Error('Zu viele gesicherte Fotobibliotheken für ein Konto.')
        if library['share'] == '@system':
            if users[owner]['role'] != 'admin' or not library['storage_id']:
                raise Error('Gesicherter Foto-Systemspeicher benötigt Administratorrechte.')
        elif library['share'] not in shares:
            raise Error('Gesicherte Fotobibliothek verweist auf eine unbekannte Freigabe.')
        if host is not None:
            if locations is None:
                data = host.op_storage_locations()
                locations = {row['id']: row for row in data.get('storage', data.get('resources', []))}
            resource = locations.get(library['storage_id'])
            full_path = PurePosixPath('/' + library['path']) if library['share'] == '@system' else PurePosixPath(shares[library['share']]['path']) / library['path']
            if (not resource or not isinstance(resource.get('path'), str) or resource['path'] == '/'
                    or not full_path.is_relative_to(PurePosixPath(resource['path']))
                    or 'files' not in resource.get('capabilities', [])
                    or (resource.get('uuid') or '') != library['storage_uuid']):
                raise Error('Gesicherter Fotoordner passt nicht zur ursprünglichen Speicherkennung.')
            # A nested managed mount cannot inherit its parent's identity.
            containing = [row for row in locations.values() if isinstance(row.get('path'), str)
                          and row['path'] != '/' and full_path.is_relative_to(PurePosixPath(row['path']))]
            if not containing or max(containing, key=lambda row: len(row['path']))['id'] != library['storage_id']:
                raise Error('Gesicherter Fotoordner verweist auf einen anderen Speicherbereich.')
        libraries[library['id']] = library
    photos = {}
    for photo in value['photos']:
        if (not isinstance(photo, dict) or set(photo) != {'id', 'library', 'path', 'favorite', 'trashed', 'trash_path', 'content_sha256'}
                or photo['library'] not in libraries or not isinstance(photo['id'], str) or photo['id'] in photos
                or type(photo['favorite']) is not int or photo['favorite'] not in (0, 1)
                or type(photo['trashed']) is not int or photo['trashed'] not in (0, 1)
                or _path(photo['path']) != photo['path']
                or not isinstance(photo['content_sha256'], str)
                or photo['content_sha256'] and not re.fullmatch(r'[a-f0-9]{64}', photo['content_sha256'])):
            raise Error('Ungültige gesicherte Fotozuordnung.')
        library = libraries[photo['library']]
        if (not PurePosixPath(photo['path']).is_relative_to(PurePosixPath(library['path']))
                or photo['path'] == library['path']
                or photo['id'] != hashlib.sha256((photo['library'] + '\0' + photo['path']).encode()).hexdigest()):
            raise Error('Gesicherte Fotozuordnung liegt außerhalb der Bibliothek.')
        if photo['trashed']:
            path = _path(photo['trash_path'], trash=True)
            expected = PurePosixPath(library['path']) / '.titan-trash' if library['share'] == '@system' else PurePosixPath('.titan-trash')
            if PurePosixPath(path).parent != expected or not PurePosixPath(path).name or not photo['content_sha256']:
                raise Error('Gesicherter Foto-Papierkorb liegt außerhalb der Bibliothek.')
        elif photo['trash_path'] != '':
            raise Error('Unzulässiger gesicherter Foto-Papierkorb.')
        photos[photo['id']] = photo
    albums, counts = {}, {}
    for album in value['albums']:
        if (not isinstance(album, dict) or set(album) != {'id', 'owner', 'name'}
                or not isinstance(album['id'], str) or not HEX32.fullmatch(album['id']) or album['id'] in albums
                or album['owner'] not in users or title(album['name']) != album['name']):
            raise Error('Ungültiges gesichertes Fotoalbum.')
        counts[album['owner']] = counts.get(album['owner'], 0) + 1
        if counts[album['owner']] > 256:
            raise Error('Zu viele gesicherte Fotoalben für ein Konto.')
        albums[album['id']] = album
    seen = set()
    for member in value['members']:
        if (not isinstance(member, dict) or set(member) != {'album', 'photo'} or member['album'] not in albums
                or member['photo'] not in photos or (member['album'], member['photo']) in seen
                or albums[member['album']]['owner'] != libraries[photos[member['photo']]['library']]['owner']):
            raise Error('Ungültige gesicherte Albumzuordnung.')
        seen.add((member['album'], member['photo']))
    return value


def export_metadata(directory, users, shares, host=None):
    source = Path(directory) / 'photos/metadata.sqlite3'
    if source.is_symlink():
        raise Error('Fotoeinstellungen dürfen kein symbolischer Link sein.')
    if not source.exists():
        return None
    from .backups import directory_fd
    with directory_fd(source.parent):
        if source.is_symlink() or not source.is_file() or source.stat().st_size > MAX_DATABASE:
            raise Error('Fotoeinstellungen sind unsicher oder zu groß für die Konfigurationssicherung.')
        with tempfile.TemporaryDirectory(prefix='titan-photo-snapshot-') as temporary:
            snapshot = Path(temporary) / 'photos.sqlite3'
            backup_deadline = time.monotonic() + 10
            def progress(_status, _remaining, _total):
                if time.monotonic() > backup_deadline:
                    raise Error('Fotoeinstellungen sind gerade ausgelastet. Sicherung erneut versuchen.', 503)
            with contextlib.closing(sqlite3.connect(source.as_uri() + '?mode=ro', uri=True, timeout=10)) as original:
                with contextlib.closing(sqlite3.connect(snapshot)) as copied:
                    original.backup(copied, pages=128, sleep=.01, progress=progress)
            if snapshot.stat().st_size > MAX_DATABASE:
                raise Error('Fotoeinstellungen sind zu groß für die Konfigurationssicherung.')
            with contextlib.closing(sqlite3.connect(snapshot)) as db:
                db.row_factory = sqlite3.Row
                db.execute('PRAGMA trusted_schema=OFF')
                db.execute('PRAGMA query_only=ON')
                deadline = time.monotonic() + 10
                db.set_progress_handler(lambda: int(time.monotonic() > deadline), 10000)
                try:
                    schema = db.execute("SELECT type,name FROM sqlite_master WHERE name NOT LIKE 'sqlite_%'").fetchall()
                    allowed = {('table', name) for name in ('libraries', 'photos', 'albums', 'album_photos')} | {('index', 'library_owner'), ('index', 'photo_library_date')}
                    if {tuple(row) for row in schema} != allowed or db.execute('PRAGMA quick_check').fetchone()[0] != 'ok':
                        raise Error('Fotoeinstellungen enthalten ein unerwartetes Datenbankschema.')
                    owners = {row['name'] for row in users}
                    libraries = [dict(row) for row in db.execute('SELECT id,owner,name,share,path,storage_id,storage_uuid FROM libraries LIMIT ?', (MAX_COLLECTIONS + 1,)) if row['owner'] in owners]
                    library_ids = {row['id'] for row in libraries}
                    photos = [dict(row) for row in db.execute('''SELECT id,library,path,favorite,trashed,trash_path,content_sha256 FROM photos
                        WHERE favorite=1 OR trashed=1 OR EXISTS(SELECT 1 FROM album_photos WHERE photo=photos.id)
                        LIMIT ?''', (MAX_ANNOTATIONS + 1,)) if row['library'] in library_ids]
                    photo_ids = {row['id'] for row in photos}
                    albums = [dict(row) for row in db.execute('SELECT id,owner,name FROM albums LIMIT ?', (MAX_COLLECTIONS + 1,)) if row['owner'] in owners]
                    album_ids = {row['id'] for row in albums}
                    members = [dict(row) for row in db.execute('SELECT album,photo FROM album_photos LIMIT ?', (MAX_ANNOTATIONS * 8 + 1,)) if row['album'] in album_ids and row['photo'] in photo_ids]
                except sqlite3.DatabaseError:
                    raise Error('Fotoeinstellungen konnten nicht sicher gelesen werden.') from None
    return validate_metadata({'schema': 1, 'libraries': libraries, 'photos': photos, 'albums': albums, 'members': members}, users, shares, host)


def import_metadata(directory, value, owner):
    """Caller validates against restored accounts/storage before stopping services."""
    directory = Path(directory)
    metadata = directory / 'photos/metadata.sqlite3'
    from .backups import directory_fd
    with directory_fd(directory):
        if metadata.parent.exists():
            with directory_fd(metadata.parent):
                if metadata.is_symlink() or metadata.exists() and not metadata.is_file():
                    raise Error('Unsicherer Zielpfad für Fotoeinstellungen.')
            cache = metadata.parent / 'thumbnails'
            if cache.exists() or cache.is_symlink():
                with directory_fd(cache):
                    pass
        photos = Photos(directory, lambda *_args, **_kwargs: None, threading.Event())
        with photos.connection() as db:
            db.execute('DELETE FROM album_photos')
            db.execute('DELETE FROM albums')
            db.execute('DELETE FROM photos')
            db.execute('DELETE FROM libraries')
            if value is not None:
                for library in value['libraries']:
                    db.execute("INSERT INTO libraries(id,owner,name,share,path,storage_id,storage_uuid,state) VALUES(?,?,?,?,?,?,?,'restored')",
                               tuple(library[key] for key in ('id', 'owner', 'name', 'share', 'path', 'storage_id', 'storage_uuid')))
                for photo in value['photos']:
                    db.execute('''INSERT INTO photos(id,library,path,name,modified,size,taken,scan,favorite,trashed,trash_path,content_sha256)
                        VALUES(?,?,?,?,0,0,0,'restored',?,?,?,?)''',
                        (photo['id'], photo['library'], photo['path'], PurePosixPath(photo['path']).name,
                         photo['favorite'], photo['trashed'], photo['trash_path'], photo['content_sha256']))
                db.executemany('INSERT INTO albums(id,owner,name) VALUES(?,?,?)',
                               [tuple(row[key] for key in ('id', 'owner', 'name')) for row in value['albums']])
                db.executemany('INSERT INTO album_photos(album,photo) VALUES(?,?)',
                               [(row['album'], row['photo']) for row in value['members']])
        for path in photos.cache.iterdir():
            if re.fullmatch(r'[a-f0-9]{64}\.jpg', path.name) and path.is_file() and not path.is_symlink():
                path.unlink()
        for path in (photos.directory, photos.cache, metadata):
            os.chown(path, owner.st_uid, owner.st_gid)
            os.chmod(path, 0o600 if path == metadata else 0o700)
