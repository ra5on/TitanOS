import test from 'node:test';
import assert from 'node:assert/strict';
import { vncHeight } from '../src/lib/vnc-size.ts';
test('console height is bounded and survives old or damaged settings', () => {
	assert.equal(vncHeight({ height: 700 }), 700);
	for (const value of [null, {}, { width: 75 }, { height: '700' }, { height: NaN }, { height: Infinity }]) assert.equal(vncHeight(value), 620);
	assert.equal(vncHeight({ height: 10 }), 240);
	assert.equal(vncHeight({ height: 9000 }), 2000);
	assert.equal(vncHeight({ height: 700.6 }), 701);
});
