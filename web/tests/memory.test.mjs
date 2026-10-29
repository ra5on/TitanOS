import { test } from 'node:test';
import assert from 'node:assert/strict';
import { memoryParts } from '../src/lib/memory.ts';

test('occupied, write cache and free partition the total', () => {
	const ram = memoryParts({
		totalBytes: 16,
		usedBytes: 15,
		cacheBytes: 12,
		freeBytes: 1,
		availableBytes: 12,
		dirtyBytes: 2,
		writebackBytes: 1,
	});
	assert.equal(ram.occupied, 4);
	assert.equal(ram.write, 3);
	assert.equal(ram.free, 9);
	assert.equal(ram.occupied + ram.write + ram.free, ram.total);
	assert.equal(ram.available, 12);
	assert.equal(memoryParts({ totalBytes: 16, usedBytes: 15, availableBytes: 8 }).write, 0);
	const clamped = memoryParts({
		totalBytes: 16,
		usedBytes: 20,
		availableBytes: 10,
		dirtyBytes: 30,
	});
	assert.equal(clamped.write, 10);
	assert.equal(clamped.occupied + clamped.write + clamped.free, 16);
	assert.equal(memoryParts({ totalBytes: 16, usedBytes: 0, availableBytes: 40 }).occupied, 0);
});
