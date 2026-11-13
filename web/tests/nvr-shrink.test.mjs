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
	// One number for both days (0.80): fewer of it shortens both.
	assert.deepEqual(shrinks(r('continuous'), r('continuous', 7)), ['days', 'eventDays']);
	assert.deepEqual(shrinks(r('continuous', 30), r('events', 7)), ['events', 'eventDays']);
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

// PLAN 88: two days – recordings without an event and events – each asks
// where it counts; events keep no recordings without an event.
test('fewer record days or event days delete recordings', () => {
	const r = (mode, recordDays, eventDays) => ({
		mode,
		retainDays: Math.max(recordDays, eventDays),
		recordDays,
		eventDays,
	});
	assert.deepEqual(shrinks(r('continuous', 14, 14), r('continuous', 7, 14)), ['days']);
	assert.deepEqual(shrinks(r('continuous', 14, 30), r('continuous', 14, 7)), ['eventDays']);
	assert.deepEqual(shrinks(r('continuous', 7, 30), r('continuous', 14, 60)), []);
	assert.deepEqual(shrinks(r('off', 14, 30), r('off', 10, 30)), ['days']);
	assert.deepEqual(shrinks(r('events', 30, 30), r('continuous', 7, 30)), []);
	assert.deepEqual(shrinks(r('events', 30, 30), r('continuous', 7, 20)), ['eventDays']);
	assert.deepEqual(shrinks(r('continuous', 7, 30), r('events', 30, 30)), ['events']);
	// A camera of 0.80 has one number for both.
	assert.deepEqual(shrinks({ mode: 'continuous', retainDays: 14 }, r('continuous', 14, 10)), [
		'eventDays',
	]);
	assert.deepEqual(shrinks({ mode: 'continuous', retainDays: 14 }, r('continuous', 14, 14)), []);
});
