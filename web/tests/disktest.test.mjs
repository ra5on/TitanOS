import { test } from 'node:test';
import assert from 'node:assert/strict';
import { resumeKind, resumeBody, restartHint } from '../src/lib/disktest.ts';

test('an overwriting test is continued only after asking, a reading one directly', () => {
	assert.equal(resumeKind({ disk: 'sdb', mode: 'write', result: 'interrupted' }), 'confirm');
	assert.deepEqual(resumeBody({ disk: 'sdb', mode: 'write', result: 'interrupted' }), {
		confirm: 'sdb',
	});
	assert.equal(resumeKind({ disk: 'sdb', mode: 'read', result: 'interrupted' }), 'direct');
	assert.deepEqual(resumeBody({ disk: 'sdb', mode: 'read', result: 'interrupted' }), {
		confirm: '',
	});
	for (const result of ['', 'passed', 'failed', 'cancelled'])
		assert.equal(resumeKind({ disk: 'sdb', mode: 'read', result }), null);
});

test('running tests say whether a restart ends them', () => {
	assert.equal(restartHint({ disk: 'sda', mode: 'read', result: '', persistent: true }), 'kept');
	assert.equal(
		restartHint({
			disk: 'sda',
			mode: 'read',
			result: '',
			persistent: false,
			notPersistent: 'no_volume',
		}),
		'no_volume',
	);
	assert.equal(
		restartHint({ disk: 'sda', mode: 'read', result: '', notPersistent: 'something new' }),
		null,
	);
	assert.equal(
		restartHint({ disk: 'sda', mode: 'read', result: 'passed', persistent: true }),
		null,
	);
});
