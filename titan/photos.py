"""Own photo library: bounded indexing, private metadata and disposable thumbnails.

Original files are always read by the existing UID-scoped file service. This
module never opens a NAS data path itself, and cached images confer no access.
"""
import base64
import contextlib
from datetime import datetime, timezone, timedelta
import hashlib
import io
import os
from pathlib import Path, PurePosixPath
import queue
import re
import secrets
import sqlite3
import tempfile
import threading
import time
import warnings

from .core import Error, integer

FORMATS = {'.jpg', '.jpeg', '.png', '.webp', '.gif'}
MAX_FILE_BYTES = 64 * 1024**2
MAX_PIXELS = 20_000_000
MAX_CACHE_BYTES = 512 * 1024**2
MAX_FILES = 100_000
MAX_DIRECTORIES = 4096
MEMORY_RESERVE_BYTES = 512 * 1024**2
ID = re.compile(r'^[a-f0-9]{64}$')


def relative_path(value):
    if not isinstance(value, str) or len(value) > 4096 or '\0' in value or '\\' in value:
        raise Error('Ungültiger Fotoordner.')
    path = PurePosixPath(value)
    if path.is_absolute() or '..' in path.parts or '.titan-trash' in path.parts:
        raise Error('Fotoordner muss innerhalb des gewählten Speichers liegen.', 403)
    return '/'.join(part for part in path.parts if part != '.')


def title(value):
    if not isinstance(value, str) or not value.strip() or len(value.strip()) > 80 or any(ord(c) < 32 for c in value):
        raise Error('Einen Namen mit höchstens 80 Zeichen angeben.')
    return value.strip()


def capture_date(exif, fallback):
    for tag in (36867, 36868, 306):
        value = exif.get(tag)
        if not isinstance(value, str):
            continue
        try:
            zone = timezone.utc
            offset = exif.get(36881)
            if isinstance(offset, str) and re.fullmatch(r'[+-]\d{2}:\d{2}', offset):
                minutes = int(offset[1:3]) * 60 + int(offset[4:6])
                if minutes > 14 * 60 or int(offset[4:6]) > 59:
                    raise ValueError()
                zone = timezone(timedelta(minutes=minutes if offset[0] == '+' else -minutes))
            return datetime.strptime(value[:19], '%Y:%m:%d %H:%M:%S').replace(tzinfo=zone).timestamp()
        except (ValueError, OverflowError, OSError):
            pass
    return fallback


def memory_available():
    try:
        with open('/proc/meminfo', encoding='ascii') as stream:
            for line in stream:
                if line.startswith('MemAvailable:'):
                    return int(line.split()[1]) * 1024
    except (OSError, ValueError, IndexError):
        pass
    return None


def prepare_thumbnail(source, modified):
    """One image at a time, with decoder bombs rejected before pixel allocation."""
    try:
        from PIL import Image, ImageOps, UnidentifiedImageError
    except ImportError:
        raise Error('Foto-Komponente fehlt im Systemimage (Pillow).', 503) from None
    try:
        with warnings.catch_warnings():
            warnings.simplefilter('error', Image.DecompressionBombWarning)
            with Image.open(source) as image:
                if image.width * image.height > MAX_PIXELS:
                    raise Error('Foto überschreitet die Vorschaugrenze von 20 Megapixeln.', 413)
                width, height = image.size
                exif = image.getexif()
                taken = capture_date(exif, modified)
                if image.format == 'JPEG':
                    image.draft('RGB', (640, 640))
                image.seek(0)
                normalized = ImageOps.exif_transpose(image)
                normalized.thumbnail((640, 640), Image.Resampling.LANCZOS)
                if normalized.mode not in ('RGB', 'L'):
                    if 'A' in normalized.getbands():
                        background = Image.new('RGB', normalized.size, 'white')
                        background.paste(normalized, mask=normalized.getchannel('A'))
                        normalized = background
                    else:
                        normalized = normalized.convert('RGB')
                output = io.BytesIO()
                normalized.save(output, format='JPEG', quality=78, optimize=False)
                return output.getvalue(), {'width': width, 'height': height, 'taken': taken}
    except Error:
        raise
    except (UnidentifiedImageError, Image.DecompressionBombError, Image.DecompressionBombWarning, OSError, ValueError, SyntaxError):
        raise Error('Dieses Bild kann nicht als Vorschau gelesen werden.', 422) from None


class Photos:
    def __init__(self, directory, file_call, stop, processor=prepare_thumbnail):
        self.directory = Path(directory) / 'photos'
        self.directory.mkdir(mode=0o700, parents=True, exist_ok=True)
        self.cache = self.directory / 'thumbnails'
        self.cache.mkdir(mode=0o700, exist_ok=True)
        self.cache_bytes = sum(path.stat().st_size for path in self.cache.iterdir() if path.is_file())
        self.file_call, self.stop, self.processor = file_call, stop, processor
        self.lock = threading.RLock()
        self.queue = queue.Queue(maxsize=16)
        self.pending = set()
        self.repeat = set()
        self.worker = None
        with self.connection() as db:
            db.executescript('''
                CREATE TABLE IF NOT EXISTS libraries (
                    id TEXT PRIMARY KEY, owner TEXT NOT NULL, name TEXT NOT NULL,
                    share TEXT NOT NULL, path TEXT NOT NULL, storage_id TEXT NOT NULL DEFAULT '',
                    storage_uuid TEXT NOT NULL DEFAULT '',
                    state TEXT NOT NULL DEFAULT 'idle', error TEXT NOT NULL DEFAULT '',
                    indexed INTEGER NOT NULL DEFAULT 0, checked REAL NOT NULL DEFAULT 0);
                CREATE INDEX IF NOT EXISTS library_owner ON libraries(owner);
                CREATE TABLE IF NOT EXISTS photos (
                    id TEXT PRIMARY KEY, library TEXT NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
                    path TEXT NOT NULL, name TEXT NOT NULL, modified REAL NOT NULL, size INTEGER NOT NULL,
                    revision TEXT NOT NULL DEFAULT '', taken REAL NOT NULL, width INTEGER NOT NULL DEFAULT 0,
                    height INTEGER NOT NULL DEFAULT 0, favorite INTEGER NOT NULL DEFAULT 0,
                    error TEXT NOT NULL DEFAULT '', scan TEXT NOT NULL, trashed INTEGER NOT NULL DEFAULT 0,
                    trash_path TEXT NOT NULL DEFAULT '', content_sha256 TEXT NOT NULL DEFAULT '', UNIQUE(library,path));
                CREATE INDEX IF NOT EXISTS photo_library_date ON photos(library,trashed,taken DESC);
                CREATE TABLE IF NOT EXISTS albums(id TEXT PRIMARY KEY, owner TEXT NOT NULL,name TEXT NOT NULL);
                CREATE TABLE IF NOT EXISTS album_photos (
                    album TEXT NOT NULL REFERENCES albums(id) ON DELETE CASCADE,
                    photo TEXT NOT NULL REFERENCES photos(id) ON DELETE CASCADE, PRIMARY KEY(album,photo));
            ''')
            for table, column in (('libraries', 'storage_uuid'), ('photos', 'content_sha256')):
                if column not in {row['name'] for row in db.execute('PRAGMA table_info(' + table + ')')}:
                    db.execute('ALTER TABLE ' + table + ' ADD COLUMN ' + column + " TEXT NOT NULL DEFAULT ''")
            db.execute("UPDATE libraries SET state='idle',error='Indexierung wurde unterbrochen. Erneut einlesen.' WHERE state IN ('queued','scanning')")

    @contextlib.contextmanager
    def connection(self):
        with self.lock:
            db = sqlite3.connect(self.directory / 'metadata.sqlite3', timeout=10)
            db.row_factory = sqlite3.Row
            db.execute('PRAGMA foreign_keys=ON')
            try:
                with db:
                    yield db
            finally:
                db.close()

    def libraries(self, owner):
        with self.connection() as db:
            return [dict(row) for row in db.execute('SELECT * FROM libraries WHERE owner=? ORDER BY name COLLATE NOCASE,id', (owner,))]

    def library(self, owner, key):
        with self.connection() as db:
            row = db.execute('SELECT * FROM libraries WHERE owner=? AND id=?', (owner, key)).fetchone()
        if not row:
            raise Error('Fotobibliothek nicht gefunden.', 404)
        return dict(row)

    def file(self, owner, record, **arguments):
        """Keep the registered library attached to its original storage identity."""
        arguments.setdefault('share', record['share'])
        arguments.setdefault('path', record['path'])
        if record.get('storage_id'):
            arguments['expected_storage'] = record['storage_id']
            arguments['expected_uuid'] = record.get('storage_uuid', '')
        return self.file_call(owner, **arguments)

    def add_library(self, owner, name, share, path='', storage_id='', storage_uuid=''):
        name, path = title(name), relative_path(path)
        if not isinstance(share, str) or not share or len(share) > 128:
            raise Error('Speicher für die Fotobibliothek auswählen.')
        if (not isinstance(storage_id, str) or len(storage_id) > 128 or not isinstance(storage_uuid, str)
                or len(storage_uuid) > 128 or storage_id and storage_id != 'system' and not storage_uuid):
            raise Error('Speicherkennung fehlt. Speicher neu auswählen.')
        self.file(owner, {'share': share, 'path': path, 'storage_id': storage_id, 'storage_uuid': storage_uuid}, action='list', offset=0, limit=1)
        key = secrets.token_hex(16)
        with self.connection() as db:
            if db.execute('SELECT COUNT(*) FROM libraries WHERE owner=?', (owner,)).fetchone()[0] >= 32:
                raise Error('Höchstens 32 Fotobibliotheken pro Konto.', 409)
            if db.execute('SELECT 1 FROM libraries WHERE owner=? AND share=? AND path=?', (owner, share, path)).fetchone():
                raise Error('Dieser Fotoordner ist bereits hinzugefügt.', 409)
            db.execute('INSERT INTO libraries(id,owner,name,share,path,storage_id,storage_uuid) VALUES(?,?,?,?,?,?,?)', (key, owner, name, share, path, storage_id, storage_uuid))
        self.schedule(owner, key)
        return {'library': key}

    def remove_library(self, owner, key):
        self.library(owner, key)
        with self.connection() as db:
            rows = db.execute('SELECT id FROM photos WHERE library=?', (key,)).fetchall()
            db.execute('DELETE FROM libraries WHERE id=? AND owner=?', (key, owner))
        for row in rows:
            self.remove_cache(row['id'])
        return {'ok': True, 'files_retained': True}

    def schedule(self, owner, key, photo=None):
        self.library(owner, key)
        job = (owner, key, photo)
        with self.lock:
            if job in self.pending:
                if photo is None:
                    self.repeat.add(job)
                return {'queued': True}
            try:
                self.queue.put_nowait(job)
            except queue.Full:
                raise Error('Fotoverarbeitung ist ausgelastet. Bitte kurz warten.', 429) from None
            self.pending.add(job)
            if photo is None:
                with self.connection() as db:
                    db.execute("UPDATE libraries SET state='queued',error='' WHERE id=?", (key,))
            if self.worker is None or not self.worker.is_alive():
                self.worker = threading.Thread(target=self.run, name='titan-photo-index', daemon=True)
                self.worker.start()
        return {'queued': True}

    def run(self):
        try:
            # Linux applies nice priority per thread; web request threads keep
            # their own priority while background image decoding yields sooner.
            os.setpriority(os.PRIO_PROCESS, threading.get_native_id(), 10)
        except (AttributeError, OSError):
            pass
        while not self.stop.is_set():
            try:
                job = self.queue.get(timeout=.5)
            except queue.Empty:
                with self.connection() as db:
                    restored = db.execute("SELECT owner,id FROM libraries WHERE state='restored' LIMIT 1").fetchone()
                if restored:
                    self.schedule(restored['owner'], restored['id'])
                continue
            owner, key, photo = job
            try:
                if photo is None:
                    self.scan(owner, key)
                else:
                    record = self.photo(owner, photo)
                    if not record['trashed']:
                        self.thumbnail(owner, record)
            except Exception as exc:
                # No raw filenames/tracebacks are exposed to another user's UI.
                message = str(exc)[:240] if isinstance(exc, Error) else 'Fotoordner nicht vollständig lesbar. Rechte und Speicher prüfen.'
                if photo is None:
                    with self.connection() as db:
                        db.execute("UPDATE libraries SET state='failed',error=?,checked=? WHERE id=? AND owner=?", (message, time.time(), key, owner))
            finally:
                with self.lock:
                    self.pending.discard(job)
                    repeat = job in self.repeat
                    self.repeat.discard(job)
                self.queue.task_done()
                if repeat and not self.stop.is_set():
                    try:
                        self.schedule(owner, key)
                    except Error:
                        pass  # Library may have been removed while scanning.

    def check_library(self, owner, library):
        self.file(owner, library, action='list', limit=1, offset=0)

    def scan(self, owner, key):
        library = self.library(owner, key)
        self.check_library(owner, library)
        scan = secrets.token_hex(16)
        stack, directories, files, indexed = [library['path']], 0, 0, 0
        with self.connection() as db:
            db.execute("UPDATE libraries SET state='scanning',indexed=0,error='' WHERE id=?", (key,))
        while stack:
            if self.stop.is_set():
                raise Error('Indexierung wurde unterbrochen.')
            self.library(owner, key)  # Removal cancels an active scan.
            folder = stack.pop()
            directories += 1
            if directories > MAX_DIRECTORIES:
                raise Error('Die Bibliothek enthält zu viele Ordner. Kleinere Fotoordner auswählen.')
            offset = 0
            while True:
                listing = self.file(owner, library, path=folder, action='list', offset=offset, limit=200)
                entries = listing['entries']
                if not entries and listing.get('has_more'):
                    raise Error('Fotoordner hat sich geändert. Erneut einlesen.', 409)
                for entry in entries:
                    files += 1
                    if files > MAX_FILES:
                        raise Error('Die Bibliothek enthält mehr als 100.000 Einträge. Kleinere Fotoordner auswählen.')
                    name = entry.get('name', '')
                    if not isinstance(name, str) or '/' in name or name in ('.', '..') or name.startswith('.') or entry.get('symlink'):
                        continue
                    path = relative_path('/'.join(filter(None, (folder, name))))
                    if entry.get('directory'):
                        if len(stack) + directories >= MAX_DIRECTORIES or len(PurePosixPath(path).parts) > 64:
                            raise Error('Fotoordner zu tief oder zu umfangreich. Kleineren Ordner auswählen.')
                        stack.append(path)
                        continue
                    if PurePosixPath(name).suffix.lower() not in FORMATS:
                        continue
                    photo_id = hashlib.sha256((key + '\0' + path).encode()).hexdigest()
                    size, modified = int(entry['size']), float(entry['modified'])
                    with self.connection() as db:
                        old = db.execute('SELECT * FROM photos WHERE id=?', (photo_id,)).fetchone()
                        changed = old is None or old['size'] != size or old['modified'] != modified or old['trashed']
                        db.execute('''INSERT INTO photos(id,library,path,name,size,modified,taken,scan)
                            VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET size=excluded.size,
                            modified=excluded.modified,scan=excluded.scan,trashed=0,trash_path='' ''',
                            (photo_id, key, path, name, size, modified, modified, scan))
                    if changed or not self.cache_path(photo_id).is_file():
                        self.thumbnail(owner, self.photo(owner, photo_id))
                    indexed += 1
                    with self.connection() as db:
                        db.execute('UPDATE libraries SET indexed=? WHERE id=?', (indexed, key))
                    if self.stop.wait(.01):
                        raise Error('Indexierung wurde unterbrochen.')
                if not listing.get('has_more'):
                    break
                offset += len(entries)
        with self.connection() as db:
            obsolete = db.execute('SELECT id FROM photos WHERE library=? AND scan<>? AND trashed=0', (key, scan)).fetchall()
            db.execute('DELETE FROM photos WHERE library=? AND scan<>? AND trashed=0', (key, scan))
            errors = db.execute("SELECT COUNT(*) FROM photos WHERE library=? AND trashed=0 AND error<>''", (key,)).fetchone()[0]
            db.execute("UPDATE libraries SET state='ready',checked=?,error=? WHERE id=?", (time.time(), f'{errors} Bilder ohne Vorschau. Originale bleiben erhalten.' if errors else '', key))
        for row in obsolete:
            self.remove_cache(row['id'])

    def photo(self, owner, key):
        if not isinstance(key, str) or not ID.fullmatch(key):
            raise Error('Ungültige Fotoauswahl.')
        with self.connection() as db:
            record = db.execute('''SELECT p.*,l.owner,l.share,l.storage_id,l.storage_uuid,l.path AS library_path
                FROM photos p JOIN libraries l ON l.id=p.library WHERE p.id=? AND l.owner=?''', (key, owner)).fetchone()
        if not record:
            raise Error('Foto nicht gefunden.', 404)
        return dict(record)

    def cache_path(self, key):
        if not isinstance(key, str) or not ID.fullmatch(key):
            raise Error('Ungültige Fotoauswahl.')
        return self.cache / (key + '.jpg')

    def remove_cache(self, key):
        path = self.cache_path(key)
        with self.lock:
            try:
                size = path.stat().st_size
            except FileNotFoundError:
                return
            path.unlink(missing_ok=True)
            self.cache_bytes = max(0, self.cache_bytes - size)

    def thumbnail(self, owner, record):
        try:
            while True:
                available = memory_available()
                if available is None or available >= MEMORY_RESERVE_BYTES:
                    break
                if self.stop.wait(2):
                    raise Error('Indexierung wurde unterbrochen.')
            with tempfile.TemporaryFile(dir=self.directory) as original:
                offset, expected, checksum = 0, None, hashlib.sha256()
                while True:
                    if self.stop.is_set():
                        raise Error('Indexierung wurde unterbrochen.')
                    result = self.file(owner, record, action='read', offset=offset, size=1024**2)
                    if result['total'] > MAX_FILE_BYTES:
                        raise Error('Foto ist größer als 64 MiB. Original bleibt verfügbar.', 413)
                    if expected is None:
                        expected = result['revision']
                    if result['revision'] != expected:
                        raise Error('Foto wurde während der Indexierung verändert.', 409)
                    chunk = base64.b64decode(result['data'], validate=True)
                    if not chunk and offset < result['total']:
                        raise Error('Foto konnte nicht vollständig gelesen werden.', 503)
                    original.write(chunk)
                    checksum.update(chunk)
                    offset += len(chunk)
                    if offset >= result['total']:
                        break
                with self.connection() as db:
                    db.execute('UPDATE photos SET revision=?,content_sha256=? WHERE id=?', (expected, checksum.hexdigest(), record['id']))
                original.seek(0)
                preview, metadata = self.processor(original, record['modified'])
            if len(preview) > 1024**2:
                raise Error('Foto-Vorschau ist zu groß.', 413)
            temporary = self.cache / (record['id'] + '-' + secrets.token_hex(8) + '.tmp')
            try:
                with temporary.open('xb') as output:
                    os.chmod(temporary, 0o600)
                    output.write(preview)
                # Recheck access/revision after decode, before publishing the cache.
                latest = self.file(owner, record, action='read', size=1)
                if latest['revision'] != expected:
                    raise Error('Foto wurde während der Indexierung verändert.', 409)
                with self.connection() as db:
                    self.photo(owner, record['id'])
                    self.remove_cache(record['id'])
                    os.replace(temporary, self.cache_path(record['id']))
                    self.cache_bytes += len(preview)
                    db.execute('UPDATE photos SET revision=?,taken=?,width=?,height=?,error=? WHERE id=?',
                               (expected, metadata['taken'], metadata['width'], metadata['height'], '', record['id']))
            finally:
                temporary.unlink(missing_ok=True)
            self.trim_cache()
        except Exception as exc:
            self.remove_cache(record['id'])
            message = str(exc)[:200] if isinstance(exc, Error) else 'Vorschau konnte nicht erstellt werden.'
            with self.connection() as db:
                db.execute('UPDATE photos SET error=? WHERE id=?', (message, record['id']))

    def trim_cache(self):
        if self.cache_bytes <= MAX_CACHE_BYTES:
            return
        files, total = [], 0
        for path in self.cache.iterdir():
            if not path.is_file() or not re.fullmatch(r'[a-f0-9]{64}\.jpg', path.name):
                continue
            try:
                value = path.stat()
            except FileNotFoundError:
                continue
            files.append((value.st_mtime, path, value.st_size))
            total += value.st_size
        for _, path, size in sorted(files):
            if total <= MAX_CACHE_BYTES:
                break
            path.unlink(missing_ok=True)
            total -= size
        self.cache_bytes = total

    def gallery(self, owner, libraries, *, library='', album='', favorite=False, trash=False, search='', offset=0, limit=80):
        offset, limit = integer(offset, 0, MAX_FILES), integer(limit, 1, 100)
        if not isinstance(search, str) or len(search) > 200:
            raise Error('Fotosuche ist zu lang.')
        allowed = {row['id'] for row in libraries}
        if library:
            if library not in allowed:
                raise Error('Kein Zugriff auf diese Fotobibliothek.', 403)
            allowed = {library}
        if not allowed:
            return {'items': [], 'total': 0, 'offset': offset, 'limit': limit, 'has_more': False}
        params = sorted(allowed) + [int(trash)]
        where = 'p.library IN (' + ','.join('?' for _ in allowed) + ') AND p.trashed=?'
        if favorite:
            where += ' AND p.favorite=1'
        if search:
            where += " AND p.name LIKE ? ESCAPE '\\'"
            params.append('%' + search.replace('\\', '\\\\').replace('%', '\\%').replace('_', '\\_') + '%')
        if album:
            with self.connection() as db:
                if not db.execute('SELECT 1 FROM albums WHERE id=? AND owner=?', (album, owner)).fetchone():
                    raise Error('Album nicht gefunden.', 404)
            where += ' AND EXISTS(SELECT 1 FROM album_photos WHERE album=? AND photo=p.id)'
            params.append(album)
        with self.connection() as db:
            total = db.execute('SELECT COUNT(*) FROM photos p WHERE ' + where, params).fetchone()[0]
            rows = db.execute('''SELECT p.id,p.library,p.path,p.name,p.size,p.modified,p.taken,p.width,p.height,
                    p.favorite,p.error,p.revision,p.trashed,l.share,l.name AS library_name
                    FROM photos p JOIN libraries l ON l.id=p.library WHERE ''' + where +
                    ' ORDER BY p.taken DESC,p.id LIMIT ? OFFSET ?', params + [limit, offset]).fetchall()
        return {'items': [dict(row) for row in rows], 'total': total, 'offset': offset, 'limit': limit, 'has_more': offset + limit < total}

    def albums(self, owner):
        with self.connection() as db:
            return [dict(row) for row in db.execute('SELECT id,name FROM albums WHERE owner=? ORDER BY name COLLATE NOCASE,id', (owner,))]

    def album_create(self, owner, name):
        name = title(name)
        key = secrets.token_hex(16)
        with self.connection() as db:
            if db.execute('SELECT COUNT(*) FROM albums WHERE owner=?', (owner,)).fetchone()[0] >= 256:
                raise Error('Höchstens 256 Alben pro Konto.', 409)
            db.execute('INSERT INTO albums VALUES(?,?,?)', (key, owner, name))
        return {'album': key}

    def album_remove(self, owner, key):
        with self.connection() as db:
            if not db.execute('DELETE FROM albums WHERE id=? AND owner=?', (key, owner)).rowcount:
                raise Error('Album nicht gefunden.', 404)
        return {'ok': True, 'files_retained': True}

    def annotate(self, owner, key, *, favorite=None, album=None, remove=False):
        record = self.photo(owner, key)
        self.file(owner, record, action='read', size=1)
        with self.connection() as db:
            if favorite is not None:
                if type(favorite) is not bool:
                    raise Error('Favorit muss Ja oder Nein sein.')
                db.execute('UPDATE photos SET favorite=? WHERE id=?', (int(favorite), key))
            if album is not None:
                if not db.execute('SELECT 1 FROM albums WHERE id=? AND owner=?', (album, owner)).fetchone():
                    raise Error('Album nicht gefunden.', 404)
                if remove:
                    db.execute('DELETE FROM album_photos WHERE album=? AND photo=?', (album, key))
                else:
                    db.execute('INSERT OR IGNORE INTO album_photos VALUES(?,?)', (album, key))
        return {'ok': True}

    def content_identity(self, owner, record, path=None):
        """Bounded reads verify a tombstone even after a backup changed its inode."""
        offset, expected, checksum = 0, None, hashlib.sha256()
        while True:
            result = self.file(owner, record, path=path if path is not None else record['path'],
                               action='read', offset=offset, size=1024**2)
            if result['total'] > MAX_FILE_BYTES:
                raise Error('Fotos über 64 MiB bitte im Dateimanager verwalten.', 413)
            if expected is None:
                expected = result['revision']
            if result['revision'] != expected:
                raise Error('Foto wurde während der Prüfung verändert.', 409)
            chunk = base64.b64decode(result['data'], validate=True)
            if not chunk and offset < result['total']:
                raise Error('Foto konnte nicht vollständig geprüft werden.', 503)
            checksum.update(chunk)
            offset += len(chunk)
            if offset >= result['total']:
                return expected, checksum.hexdigest()

    def trash(self, owner, key, restore=False):
        record = self.photo(owner, key)
        if bool(record['trashed']) == (not restore):
            raise Error('Foto ist bereits im Papierkorb.' if not restore else 'Foto ist nicht im Papierkorb.', 409)
        if restore:
            revision, checksum = self.content_identity(owner, record, record['trash_path'])
            if not record['content_sha256'] or checksum != record['content_sha256']:
                raise Error('Datei im Papierkorb wurde ersetzt oder verändert. Wiederherstellung abgebrochen.', 409)
            self.file(owner, record, action='move', path=record['trash_path'], destination=record['path'], revision=revision)
            with self.connection() as db:
                db.execute("UPDATE photos SET trashed=0,trash_path='' WHERE id=?", (key,))
        else:
            revision, checksum = self.content_identity(owner, record)
            if record['revision'] and revision != record['revision']:
                raise Error('Foto wurde verändert. Bibliothek zuerst aktualisieren.', 409)
            if record['share'] == '@system':
                folder = record['library_path'] + '/.titan-trash'
                try:
                    self.file(owner, record, action='list', path=folder, limit=1)
                except (FileNotFoundError, Error) as exc:
                    if isinstance(exc, Error) and exc.status not in (400, 404):
                        raise
                    try:
                        self.file(owner, record, action='mkdir', path=folder)
                    except (FileExistsError, Error):
                        # RPC translates an EEXIST race into a normal Error.
                        # Only proceed if the file boundary confirms a directory.
                        self.file(owner, record, action='list', path=folder, limit=1)
                target = folder + '/' + secrets.token_hex(16) + '-' + record['name']
                self.file(owner, record, action='move', destination=target, revision=revision)
            else:
                result = self.file(owner, record, action='trash', revision=revision)
                target = '.titan-trash/' + result['trash_name']
            with self.connection() as db:
                db.execute('UPDATE photos SET trashed=1,trash_path=?,content_sha256=? WHERE id=?', (target, checksum, key))
            self.remove_cache(key)
        return {'ok': True}
