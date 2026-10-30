import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
	thumbnailKind,
	thumbnailURL,
	sortPhotos,
	swipeDirection,
	streamableVideo,
} from '../src/lib/file-actions.ts';
import { indexSettings, indexStateLabel } from '../src/lib/file-index.ts';
test('thumbnails cover the same types as titand', () => {
	assert.equal(thumbnailKind('Urlaub.JPG'), 'image');
	assert.equal(thumbnailKind('scan.pdf'), 'pdf');
	assert.equal(thumbnailKind('clip.MOV'), 'video');
	assert.equal(thumbnailKind('vector.svg'), null);
	assert.equal(thumbnailKind('foto.heic'), null);
});
test('thumbnail addresses carry the version and escape paths', () => {
	const url = new URL(
		thumbnailURL('share/Fotos', '2026/a b&c.jpg', 320, '2026-01-02T03:04:05Z'),
		'https://nas',
	);
	assert.equal(url.pathname, '/api/v1/files/thumb');
	assert.equal(url.searchParams.get('path'), '2026/a b&c.jpg');
	assert.equal(url.searchParams.get('v'), '2026-01-02T03:04:05Z');
	assert.equal(url.searchParams.get('size'), '320');
});
test('photos sort by date taken, falling back to the modification time', () => {
	const list = [
		{ name: 'b.jpg', size: 1, modified: '2026-03-01T00:00:00Z', kind: 'image' },
		{
			name: 'a.jpg',
			size: 1,
			modified: '2026-05-01T00:00:00Z',
			taken: '2020-01-01T00:00:00Z',
			kind: 'image',
		},
		{ name: 'c10.jpg', size: 1, modified: '2026-04-01T00:00:00Z', kind: 'image' },
		{ name: 'c9.jpg', size: 1, modified: '2026-04-01T00:00:00Z', kind: 'image' },
	];
	assert.deepEqual(
		sortPhotos(list, 'taken').map((p) => p.name),
		['c9.jpg', 'c10.jpg', 'b.jpg', 'a.jpg'],
	);
	assert.deepEqual(
		sortPhotos(list, 'name').map((p) => p.name),
		['a.jpg', 'b.jpg', 'c9.jpg', 'c10.jpg'],
	);
});
test('swipes need a clear horizontal movement', () => {
	assert.equal(swipeDirection(-80, 5), -1);
	assert.equal(swipeDirection(80, -10), 1);
	assert.equal(swipeDirection(30, 0), 0);
	assert.equal(swipeDirection(-80, 70), 0);
	assert.equal(streamableVideo('a.mp4'), true);
	assert.equal(streamableVideo('a.mkv'), false);
});
test('index settings default to off and drop malformed values', () => {
	assert.deepEqual(indexSettings(null), {
		enabled: false,
		volumes: [],
		shares: [],
		ocr: false,
		time: '',
	});
	assert.deepEqual(
		indexSettings({
			enabled: true,
			volumes: ['data', 3],
			shares: ['fotos'],
			ocr: true,
			time: '24:00',
		}),
		{ enabled: true, volumes: ['data'], shares: ['fotos'], ocr: true, time: '' },
	);
	assert.equal(indexSettings({ time: '03:15' }).time, '03:15');
	assert.equal(indexStateLabel('running'), 'fileIndex.state.running');
});
