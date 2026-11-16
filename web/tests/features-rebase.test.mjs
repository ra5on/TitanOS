import assert from 'node:assert/strict';
import test from 'node:test';
import { rebaseFeatures } from '../src/lib/features-rebase.ts';

const base = { rev: 'a', guard: { enabled: false, mode: 'lock' }, buddy: { enabled: false } };

test('a change to another function is put on the new state, with its rev', () => {
	const mine = { ...base, guard: { enabled: true, mode: 'lock' } };
	const theirs = { ...base, rev: 'b', buddy: { enabled: true } };
	assert.deepEqual(rebaseFeatures(base, mine, theirs), {
		rev: 'b',
		guard: { enabled: true, mode: 'lock' },
		buddy: { enabled: true },
	});
});

test('the same function changed elsewhere: nothing is saved over it', () => {
	const mine = { ...base, guard: { enabled: true, mode: 'lock' } };
	const theirs = { ...base, rev: 'b', guard: { enabled: false, mode: 'report' } };
	assert.equal(rebaseFeatures(base, mine, theirs), null);
});

test('no change of its own: the new state as it is', () => {
	const theirs = { ...base, rev: 'b', buddy: { enabled: true } };
	assert.equal(rebaseFeatures(base, { ...base }, theirs), theirs);
});
