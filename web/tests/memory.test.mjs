import { test } from 'node:test';
import assert from 'node:assert/strict';
import { memoryParts } from '../src/lib/memory.ts';

test('physical cache accounting excludes available from the partition sum', () => {
	const ram = memoryParts({
		totalBytes: 16,
		usedBytes: 15,
		cacheBytes: 12,
		freeBytes: 1,
		availableBytes: 12,
	});
	assert.equal(ram.other, 3);
	assert.equal(ram.other + ram.cache + ram.free, ram.total);
	assert.equal(ram.available, 12);
	assert.equal(memoryParts({ totalBytes: 16, usedBytes: 15, availableBytes: 8 }).cache, null);
	const clamped = memoryParts({
		totalBytes: 16,
		usedBytes: 20,
		cacheBytes: 30,
		availableBytes: 40,
	});
	assert.equal(clamped.other + clamped.cache + clamped.free, 16);
	assert.equal(clamped.available, 16);
});
