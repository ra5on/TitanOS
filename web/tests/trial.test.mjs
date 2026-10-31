import { test } from 'node:test';
import assert from 'node:assert/strict';
import { countdown } from '../src/lib/trial.ts';

test('countdown: minutes and two-digit seconds', () => {
	assert.equal(countdown(0), '0:00');
	assert.equal(countdown(9), '0:09');
	assert.equal(countdown(60), '1:00');
	assert.equal(countdown(125.9), '2:05');
});

test('countdown: never negative', () => {
	assert.equal(countdown(-5), '0:00');
});
