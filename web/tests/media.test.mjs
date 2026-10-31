import { test, afterEach } from 'node:test';
import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import { mediaActive, verifyMediaUpload } from '../src/lib/media.ts';

const realFetch = globalThis.fetch;
afterEach(() => (globalThis.fetch = realFetch));

test('mediaActive: only phases that are still working', () => {
	for (const phase of ['downloading', 'extracting', 'verifying', 'publishing', 'pausing'])
		assert.equal(mediaActive({ phase }), true, phase);
	for (const phase of ['done', 'failed', 'paused', 'uploading', ''])
		assert.equal(mediaActive({ phase }), false, phase);
});

test('verifyMediaUpload: SHA-256 of each chunk, sent in groups of 32', async () => {
	const bytes = new Uint8Array(70).map((_, i) => i);
	const file = new File([bytes], 'x.iso');
	const sent = [];
	globalThis.fetch = async (url, init) => {
		sent.push({ url, body: JSON.parse(init.body) });
		return new Response(null, { status: 204 });
	};
	const progress = [];
	await verifyMediaUpload({ id: 'j1', bytes: 66 }, file, 2, (done) => progress.push(done));
	// 33 chunks of 2 bytes: one request with 32, one with the last.
	assert.equal(sent.length, 2);
	assert.equal(sent[0].url, '/api/v1/vms/imports/j1/verify');
	assert.equal(sent[0].body.chunks.length, 32);
	assert.equal(sent[1].body.chunks.length, 1);
	assert.deepEqual(progress, [64, 66]);
	const last = sent[1].body.chunks[0];
	assert.deepEqual({ offset: last.offset, size: last.size }, { offset: 64, size: 2 });
	assert.equal(last.sha256, createHash('sha256').update(bytes.slice(64, 66)).digest('hex'));
});
