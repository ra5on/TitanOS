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

test('console width is optional and bounded', async () => {
	const { vncWidth } = await import('../src/lib/vnc-size.ts');
	assert.equal(vncWidth({ height: 700 }), null);
	assert.equal(vncWidth({ width: 900 }), 900);
	assert.equal(vncWidth({ width: 10 }), 320);
	assert.equal(vncWidth({ width: '900' }), null);
	assert.equal(vncWidth(null), null);
});

test('console fits between its top and the bottom of the window', async () => {
	const { fitHeight } = await import('../src/lib/vnc-size.ts');
	assert.equal(fitHeight(900, 500), 500);
	assert.equal(fitHeight(400, 500), 400);
	// Never smaller than the least height, even in a tiny window.
	assert.equal(fitHeight(900, 100), 240);
	// Before the page is measured the stored height counts.
	assert.equal(fitHeight(700, 0), 700);
	assert.equal(fitHeight(700, 640.7), 640);
});
