"""Authenticated photo routes reusing Titan's existing file authority boundary."""
from pathlib import PurePosixPath

from .core import Error
from .identity import require_application
from .photos import Photos, relative_path, FORMATS


class PhotosApplicationMixin:
    def initialize_photos(self):
        self.photos = Photos(self.store.directory, self.photos_file_call, self.stop)
        with self.photos.connection() as db:
            restored = db.execute("SELECT owner,id FROM libraries WHERE state='restored' LIMIT 16").fetchall()
        for library in restored:
            self.photos.schedule(library['owner'], library['id'])

    def photos_file_call(self, owner, **arguments):
        # A scan can outlive both an HTTP request and a permission grant.
        current = self.store.user_record(owner)
        require_application(self.store, current, 'files')
        if self.demo and arguments.get('expected_storage'):
            data = self.agent.call('storage_locations')
            resource = next((row for row in data.get('storage', []) if row['id'] == arguments['expected_storage']), None)
            expected = arguments.pop('expected_storage')
            uuid = arguments.pop('expected_uuid', '')
            if (not resource or resource.get('available') is False
                    or uuid != (resource.get('uuid') or ('' if expected == 'system' else 'demo:' + expected))):
                raise Error('Der ursprüngliche Fotospeicher ist nicht verfügbar.', 503)
        if arguments.get('share') == '@system':
            if current['role'] != 'admin':
                raise Error('Dieser Speicher benötigt Administratorrechte.', 403)
            arguments.pop('share')
            return self.agent.call('system_file', **arguments)
        if current['role'] == 'admin':
            return self.agent.call('admin_file', **arguments)
        return self.agent.call('file', user=current['system_user'], **arguments)

    def photos_sources(self, user):
        shares = self.agent.call('shares')
        data = self.agent.call('storage_locations')
        resources = sorted(data.get('storage', data.get('resources', [])), key=lambda row: len(row.get('path', '')), reverse=True)
        sources = []
        def identity(resource):
            return {'storage_id': resource['id'], 'storage_uuid': resource.get('uuid') or ('' if resource['id'] == 'system' else 'demo:' + resource['id'] if self.demo else '')}
        for row in shares:
            if user['role'] != 'admin' and user['system_user'] not in row['readers'] + row['writers']:
                continue
            resource = next((item for item in resources if isinstance(item.get('path'), str) and item['path'] != '/'
                             and PurePosixPath(row['path']).is_relative_to(PurePosixPath(item['path']))), None)
            if not resource or resource.get('available') is False:
                continue
            pin = identity(resource)
            if pin['storage_id'] != 'system' and not pin['storage_uuid']:
                continue
            sources.append({'id': 'share:' + row['name'], 'name': row.get('label') or row['name'], 'share': row['name'],
                            'path': '', **pin, 'writable': user['role'] == 'admin' or user['system_user'] in row['writers']})
        if user['role'] == 'admin':
            for row in resources:
                if (row.get('available') is not False and 'files' in row.get('capabilities', [])
                        and isinstance(row.get('path'), str) and row['path'].startswith('/') and row['path'] != '/'):
                    pin = identity(row)
                    if pin['storage_id'] != 'system' and not pin['storage_uuid']:
                        continue
                    sources.append({'id': 'storage:' + row['id'], **pin,
                                    'name': row.get('label') or row['id'], 'share': '@system',
                                    'path': row['path'].strip('/'), 'writable': row.get('status') != 'full'})
        return sources

    def photos_accessible_libraries(self, owner):
        result = []
        for library in self.photos.libraries(owner):
            try:
                self.photos.check_library(owner, library)
                library['available'] = True
            except Exception:
                library['available'] = False
            result.append(library)
        return result


class PhotosHTTPMixin:
    def photos_get(self, path, user, query):
        if path not in ('/api/photos', '/api/photos/preview', '/api/photos/original'):
            return False
        require_application(self.app.store, user, 'files')
        owner = user['name']
        if path in ('/api/photos/preview', '/api/photos/original'):
            if set(query) - {'photo', 'v', 'preview'} or 'photo' not in query:
                raise Error('Foto auswählen.')
            record = self.app.photos.photo(owner, query['photo'])
            if record['trashed']:
                raise Error('Foto liegt im Papierkorb.', 404)
            if path == '/api/photos/original':
                self.download(user, query, file_reader=lambda **args: self.app.photos.file(owner, record, **args))
                return True
            first = self.app.photos.file(owner, record, action='read', size=1)
            cache = self.app.photos.cache_path(record['id'])
            if record['revision'] != first['revision'] or not cache.is_file():
                library = self.app.photos.library(owner, record['library'])
                if library['state'] not in ('queued', 'scanning'):
                    try:
                        self.app.photos.schedule(owner, record['library'], record['id'])
                    except Error as exc:
                        if exc.status != 429:
                            raise
                image = b'<svg xmlns="http://www.w3.org/2000/svg" width="160" height="160" viewBox="0 0 160 160"><rect width="160" height="160" fill="#e8edf4"/><path d="M40 110l28-30 18 18 18-24 20 36z" fill="#8193ac"/><circle cx="57" cy="55" r="10" fill="#8193ac"/></svg>'
                self.send_headers(202, len(image), 'image/svg+xml', {'Retry-After': '3'})
                self.wfile.write(image)
                return True
            image = cache.read_bytes()
            self.send_headers(200, len(image), 'image/jpeg')
            self.wfile.write(image)
            return True
        if set(query) - {'library', 'album', 'favorite', 'trash', 'search', 'offset', 'limit'}:
            raise Error('Ungültige Fotoansicht.')
        for flag in ('favorite', 'trash'):
            if query.get(flag, '0') not in ('0', '1'):
                raise Error('Ungültiger Fotofilter.')
        libraries = self.app.photos_accessible_libraries(owner)
        sources = self.app.photos_sources(user)
        for library in libraries:
            source = next((row for row in sources if row['share'] == library['share']
                           and row.get('storage_id', '') == library['storage_id']
                           and row.get('storage_uuid', '') == library['storage_uuid']), None)
            library['writable'] = bool(source and source['writable'])
        gallery = self.app.photos.gallery(owner, [row for row in libraries if row['available']],
                    library=query.get('library', ''), album=query.get('album', ''),
                    favorite=query.get('favorite') == '1', trash=query.get('trash') == '1',
                    search=query.get('search', ''), offset=query.get('offset', 0), limit=query.get('limit', 80))
        # Regenerate only visible evicted previews, bounded by the shared worker
        # queue. The gallery polls until they are ready; image requests never
        # decode originals in HTTP threads or fill an unbounded executor.
        missing = [row for row in gallery['items'] if not row['trashed'] and not row['error']
                   and not self.app.photos.cache_path(row['id']).is_file()]
        library_states = {row['id']: row['state'] for row in libraries}
        for row in missing[:8]:
            if library_states.get(row['library']) in ('queued', 'scanning'):
                continue
            try:
                self.app.photos.schedule(owner, row['library'], row['id'])
            except Error as exc:
                if exc.status != 429:
                    raise
        self.reply({**gallery, 'libraries': libraries, 'albums': self.app.photos.albums(owner),
                    'sources': sources,
                    'formats': ['JPEG', 'PNG', 'WebP', 'GIF'], 'max_preview_megapixels': 20,
                    'preview_processing': bool(missing),
                    'indexing': bool(missing) or any(row['state'] in ('queued', 'scanning') for row in libraries)})
        return True

    def photos_post(self, path, user, body):
        if path not in ('/api/photos', '/api/photos/upload'):
            return False
        require_application(self.app.store, user, 'files')
        if path == '/api/photos/upload':
            if set(body) != {'library', 'name', 'offset', 'data'}:
                raise Error('Ungültiger Foto-Upload.')
            name = relative_path(body['name'])
            if not name or '/' in name or name.startswith('.') or PurePosixPath(name).suffix.lower() not in FORMATS:
                raise Error('Wähle eine JPEG-, PNG-, WebP- oder GIF-Datei.')
            if not isinstance(body['data'], str) or len(body['data']) > 1400000:
                raise Error('Foto-Uploadblock ist zu groß.', 413)
            library = self.app.photos.library(user['name'], body['library'])
            target = '/'.join(filter(None, (library['path'], name)))
            self.reply(self.app.photos.file(user['name'], library, path=target, action='upload', offset=body['offset'], data=body['data']))
            return True
        action, owner = body.get('action'), user['name']
        fields = {
            'library_add': {'action', 'name', 'source', 'path'},
            'library_remove': {'action', 'library'}, 'scan': {'action', 'library'},
            'favorite': {'action', 'photo', 'value'}, 'album_create': {'action', 'name'},
            'album_remove': {'action', 'album'}, 'album_add': {'action', 'album', 'photo'},
            'album_unlink': {'action', 'album', 'photo'}, 'trash': {'action', 'photo'},
            'restore': {'action', 'photo'},
        }
        if action not in fields or set(body) != fields[action]:
            raise Error('Ungültige Fotoaktion.')
        if action == 'library_add':
            source = next((row for row in self.app.photos_sources(user) if row['id'] == body['source']), None)
            if not source:
                raise Error('Der gewählte Speicher ist nicht verfügbar.', 403)
            path = relative_path(body['path'])
            if source['path']:
                root = PurePosixPath(source['path'])
                # Browsers pass a path picked by the existing file browser.
                selected = PurePosixPath(path)
                if not selected.is_relative_to(root):
                    raise Error('Fotoordner liegt außerhalb des gewählten Speichers.', 403)
            result = self.app.photos.add_library(owner, body['name'], source['share'], path, source['storage_id'], source['storage_uuid'])
        elif action == 'library_remove':
            result = self.app.photos.remove_library(owner, body['library'])
        elif action == 'scan':
            library = self.app.photos.library(owner, body['library'])
            self.app.photos.check_library(owner, library)
            result = self.app.photos.schedule(owner, body['library'])
        elif action == 'favorite':
            result = self.app.photos.annotate(owner, body['photo'], favorite=body['value'])
        elif action == 'album_create':
            result = self.app.photos.album_create(owner, body['name'])
        elif action == 'album_remove':
            result = self.app.photos.album_remove(owner, body['album'])
        elif action in ('album_add', 'album_unlink'):
            result = self.app.photos.annotate(owner, body['photo'], album=body['album'], remove=action == 'album_unlink')
        else:
            result = self.app.photos.trash(owner, body['photo'], restore=action == 'restore')
        self.app.store.audit(owner, 'photos_' + action, str(body.get('library', body.get('photo', body.get('album', '')))))
        self.reply(result)
        return True
