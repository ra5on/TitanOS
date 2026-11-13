import { test } from 'node:test';
import assert from 'node:assert/strict';
import { shrinks } from '../src/lib/nvr/shrink.ts';

// The same rule as nvr.shrinks in titand (manager.go): the dialog names
// what the NAS asked about.
test('a narrower recording mode or fewer days delete recordings', () => {
	const r = (mode, retainDays = 14) => ({ mode, retainDays });
	assert.deepEqual(shrinks(r('continuous'), r('motion')), ['motion']);
	assert.deepEqual(shrinks(r('continuous'), r('events')), ['events']);
	assert.deepEqual(shrinks(r('motion'), r('events')), ['events']);
	assert.deepEqual(shrinks(r('off'), r('events')), ['events']);
	assert.deepEqual(shrinks(r('off'), r('motion')), ['motion']);
	assert.deepEqual(shrinks(r('continuous'), r('continuous', 7)), ['days']);
	assert.deepEqual(shrinks(r('continuous', 30), r('events', 7)), ['events', 'days']);
	for (const [from, to] of [
		['continuous', 'off'],
		['events', 'off'],
		['motion', 'off'],
		['off', 'continuous'],
		['events', 'motion'],
		['motion', 'continuous'],
	])
		assert.deepEqual(shrinks(r(from), r(to)), [], `${from} → ${to}`);
	assert.deepEqual(shrinks(r('events'), r('events', 30)), []);
	assert.deepEqual(shrinks(undefined, r('events')), []);
});
