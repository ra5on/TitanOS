import { test } from 'node:test';
import assert from 'node:assert/strict';
import { chartSegments, newMaintenanceJob } from '../src/lib/operations.ts';
test('monitoring lines keep explicit gaps after server downsampling', () => {
	const at = '2026-10-23T10:00:00Z';
	const rows = [0, 1, 2, 3].map((n) => ({
		at: new Date(Date.parse(at) + n * 900000).toISOString(),
		gap: n === 2,
		cpu: 10 + n,
	}));
	const segments = chartSegments(rows, (s) => s.cpu);
	assert.equal(segments.length, 2);
	assert.equal(segments[0].length, 2);
	assert.equal(segments[1].length, 2);
	assert.equal(
		chartSegments(
			[
				{ at, cpu: 0 },
				{ at: '2026-10-23T10:10:00Z', cpu: 1 },
			],
			(s) => s.cpu,
		).length,
		2,
	);
	assert.equal(chartSegments(rows, (s) => (s.cpu === 11 ? null : s.cpu)).length, 2);
});
test('new maintenance jobs require explicit snapshot pruning consent', () => {
	const first = newMaintenanceJob(),
		second = newMaintenanceJob();
	assert.equal(first.allowPrune, false);
	assert.equal(first.maxFiles, 20);
	first.weekdays.push(3);
	assert.deepEqual(second.weekdays, [0]);
});
