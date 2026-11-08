import { test } from 'node:test';
import assert from 'node:assert/strict';
import { treemap } from '../src/routes/files/treemap.ts';

test('treemap areas match sizes and fill the box', () => {
	const items = [{ v: 6 }, { v: 6 }, { v: 4 }, { v: 3 }, { v: 2 }, { v: 2 }, { v: 1 }, { v: 0 }];
	const out = treemap(items, 600, 400, (i) => i.v);
	assert.equal(out.length, 7);
	const total = 24;
	for (const a of out) {
		assert.ok(Math.abs(a.w * a.h - (a.item.v / total) * 600 * 400) < 1e-6);
		assert.ok(a.x >= -1e-9 && a.y >= -1e-9 && a.x + a.w <= 600 + 1e-6 && a.y + a.h <= 400 + 1e-6);
	}
	const sum = out.reduce((s, a) => s + a.w * a.h, 0);
	assert.ok(Math.abs(sum - 600 * 400) < 1e-6);
});

test('treemap handles empty input and zero box', () => {
	assert.deepEqual(
		treemap([], 100, 100, () => 1),
		[],
	);
	assert.deepEqual(
		treemap([{}], 0, 100, () => 1),
		[],
	);
	assert.deepEqual(
		treemap([{}], 100, 100, () => 0),
		[],
	);
});
