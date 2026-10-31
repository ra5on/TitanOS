import { test } from 'node:test';
import assert from 'node:assert/strict';
import { rule321, placeOf } from '../src/lib/backup-321.ts';

const job = (id, destination, enabled = true) => ({ job: { id, enabled, destination } });

test('without jobs only the original counts', () => {
	const r = rule321([]);
	assert.deepEqual(r, {
		copies: 1,
		media: ['nas'],
		offsite: 0,
		jobs: 0,
		three: false,
		two: false,
		one: false,
	});
});

test('another volume is the same kind of storage, a USB drive and the cloud are not', () => {
	let r = rule321([job('a', { type: 'local', volume: 'backup', folder: 'x' })]);
	assert.equal(r.copies, 2);
	assert.equal(r.two, false);
	r = rule321([
		job('a', { type: 'local', volume: 'backup', folder: 'x' }),
		job('b', { type: 'usb-copy', deviceId: 'USB1' }),
		job('c', { type: 's3', endpoint: 'https://s3.example', bucket: 'b' }),
	]);
	assert.deepEqual(r.media, ['nas', 'usb', 'cloud']);
	assert.equal(r.copies, 4);
	assert.equal(r.offsite, 1);
	assert.ok(r.three && r.two && r.one);
});

test('two jobs to one place are one copy; switched off jobs do not count', () => {
	const r = rule321([
		job('a', { type: 'sftp', host: 'Example.org', port: 22, user: 'nas', path: '/b' }),
		job('b', { type: 'sftp', host: 'example.org', port: 22, user: 'nas', path: '/b' }),
		job('c', { type: 'titanos', targetId: 't1' }, false),
		job('d', { type: 'unknown-later' }),
	]);
	assert.equal(r.copies, 2);
	assert.equal(r.jobs, 2);
	assert.equal(r.offsite, 1);
	assert.deepEqual(r.media, ['nas', 'server']);
});

test('places name the kind and the target', () => {
	assert.equal(placeOf({ type: 'rest', url: 'https://R.example/x' }), 'rest:https://r.example/x');
	assert.notEqual(
		placeOf({ type: 'smb', targetId: 'a' }),
		placeOf({ type: 'smb-copy', targetId: 'a' }),
	);
});
