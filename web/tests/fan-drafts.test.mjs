import assert from 'node:assert/strict';
import test from 'node:test';
import { takeFans } from '../src/lib/fan-drafts.ts';

const fixed = (percent) => ({ id: 'f1', mode: 'fixed', percent, min: 20 });

test('a changed fan keeps its draft while the NAS is asked again', () => {
	let s = takeFans({ drafts: {}, base: {} }, [{ id: 'f1', control: fixed(40) }]);
	assert.deepEqual(s.drafts.f1, fixed(40));
	s.drafts.f1 = fixed(70);
	const next = takeFans(s, [{ id: 'f1', control: fixed(40) }]);
	assert.equal(next.drafts.f1.percent, 70);
	assert.equal(next.edited, true);
});

test('after a save, later changes are kept too (they sprang back before)', () => {
	let s = takeFans({ drafts: {}, base: {} }, [{ id: 'f1', control: fixed(40) }]);
	s.drafts.f1 = fixed(70);
	// The answer to the save replaces the drafts.
	s = takeFans(s, [{ id: 'f1', control: fixed(70) }], true);
	assert.equal(s.edited, false);
	s.drafts.f1 = fixed(90);
	s = takeFans(s, [{ id: 'f1', control: fixed(70) }]);
	assert.equal(s.drafts.f1.percent, 90);
});

test('an untouched fan follows the NAS', () => {
	let s = takeFans({ drafts: {}, base: {} }, [{ id: 'f1', control: fixed(40) }]);
	s = takeFans(s, [
		{ id: 'f1', control: fixed(55) },
		{ id: 'f2', control: { id: 'f2', mode: 'bios', min: 0 } },
	]);
	assert.equal(s.drafts.f1.percent, 55);
	assert.equal(s.drafts.f2.mode, 'bios');
	assert.equal(s.edited, false);
});
