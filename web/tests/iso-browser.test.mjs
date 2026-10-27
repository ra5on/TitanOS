import { test } from 'node:test';
import assert from 'node:assert/strict';
import { isISO, isoLocation, isoPath, matchMedia } from '../src/lib/iso-browser.ts';

test('ISO browser builds volume paths and opens where a medium lies', () => {
	assert.equal(isoPath('data', '', 'debian.iso'), '/mnt/data/debian.iso');
	assert.equal(isoPath('data', 'iso/linux', 'debian.iso'), '/mnt/data/iso/linux/debian.iso');
	assert.deepEqual(isoLocation('/mnt/data/iso/linux/debian.iso', ['data']), {
		volume: 'data',
		dir: 'iso/linux',
	});
	assert.deepEqual(isoLocation('/mnt/data/debian.iso', ['data']), { volume: 'data', dir: '' });
	assert.equal(isoLocation('/mnt/gone/debian.iso', ['data']), null, 'unmounted volume');
	assert.equal(isoLocation('/mnt/data/../etc/x.iso', ['data']), null);
	assert.equal(isoLocation('/srv/data/x.iso', ['data']), null);
	assert.equal(isoLocation('', ['data']), null);
	assert.equal(isoLocation('/mnt/data', ['data']), null);
	assert.ok(isISO('Win11.ISO') && !isISO('notes.txt') && !isISO('iso'));
});

test('ISO search matches every word anywhere in the path, ignoring case', () => {
	const media = [
		{ path: '/mnt/data/iso/debian-13-amd64.iso', sizeBytes: 1 },
		{ path: '/mnt/data/iso/Win11_German.iso', sizeBytes: 2 },
		{ path: '/mnt/backup/old/debian-12.iso', sizeBytes: 3 },
	];
	assert.deepEqual(matchMedia(media, 'debian').map((m) => m.sizeBytes), [1, 3]);
	assert.deepEqual(matchMedia(media, 'WIN german').map((m) => m.sizeBytes), [2]);
	assert.deepEqual(matchMedia(media, 'backup debian').map((m) => m.sizeBytes), [3]);
	assert.equal(matchMedia(media, '  ').length, 3);
});
