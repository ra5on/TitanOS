import { test } from 'node:test';
import assert from 'node:assert/strict';
import { historyRows, isSmartHistory, rose, temperaturePoints } from '../src/lib/smart-history.ts';

const samples = [
	{ day: '2026-10-01', readAt: '', temperatureC: 30, reallocatedSectors: 0, crcErrors: 2 },
	{ day: '2026-10-02', readAt: '', temperatureC: 34, reallocatedSectors: 0, crcErrors: 2 },
	{ day: '2026-10-03', readAt: '', temperatureC: 33, reallocatedSectors: 8, crcErrors: 2 },
];

test('rows name first and last value and the day a counter rose', () => {
	const rows = historyRows(samples);
	assert.deepEqual(
		rows.map((r) => r.field),
		['reallocatedSectors', 'crcErrors', 'temperatureC'],
	);
	const realloc = rows[0];
	assert.equal(realloc.first, 0);
	assert.equal(realloc.last, 8);
	assert.equal(realloc.roseOn, '2026-10-03');
	assert.ok(rose(realloc));
	assert.ok(!rose(rows[1]));
	assert.equal(rows[2].max, 34);
	assert.ok(!rose(rows[2]), 'temperature never counts as rising');
	assert.deepEqual(historyRows([]), []);
});

test('the temperature line spans the box with at least ten degrees', () => {
	const { points, min, max } = temperaturePoints(samples, 100, 50);
	assert.equal(points.length, 3);
	assert.equal(points[0].x, 0);
	assert.equal(points[2].x, 100);
	assert.ok(max - min >= 10);
	for (const p of points) assert.ok(p.y >= 0 && p.y <= 50);
	assert.deepEqual(temperaturePoints([{ day: 'x', readAt: '' }], 100, 50).points, []);
	assert.equal(temperaturePoints([samples[0]], 100, 50).points[0].x, 50);
});

test('answers are checked', () => {
	assert.ok(isSmartHistory({ disk: 'sda', volume: 'data', samples }));
	assert.ok(!isSmartHistory({ disk: 'sda', samples }));
	assert.ok(!isSmartHistory(null));
});
