import { test } from 'node:test';
import assert from 'node:assert/strict';
import { sanitizeNav } from '../src/lib/navigation.ts';
import { defaultAppearance, sanitizeAppearance } from '../src/lib/theme/themes.ts';

// PLAN 65: the classic navigation is gone; its stored settings stay readable.
const standard = { order: [], hidden: [], layout: 'top', magnify: true, button: 'tl' };

test('sanitizing keeps unknown but valid ids and drops junk', () => {
	assert.deepEqual(sanitizeNav({ order: ['fleet', 'fleet', 'Bad Id', 7, 'docker'], hidden: 'x' }), {
		...standard,
		order: ['fleet', 'docker'],
	});
	assert.deepEqual(sanitizeNav(null), standard);
	assert.deepEqual(defaultAppearance().nav, standard);
	assert.deepEqual(sanitizeAppearance({ nav: { order: ['tools'] } }).nav, {
		...standard,
		order: ['tools'],
	});
	assert.deepEqual(sanitizeNav({ hidden: ['settings', 'docker'] }).hidden, ['docker']);
	assert.equal(sanitizeNav({ button: 'br' }).button, 'br');
	assert.equal(sanitizeNav({ button: 'middle' }).button, 'tl');
});

test('stored layout and magnify survive for older versions', () => {
	assert.equal(sanitizeNav({ layout: 'side' }).layout, 'side');
	assert.equal(sanitizeNav({ layout: 'left' }).layout, 'top');
	assert.equal(
		sanitizeAppearance({ nav: { layout: 'side', hidden: ['apps'] } }).nav.layout,
		'side',
	);
	assert.equal(sanitizeNav({}).magnify, true);
	assert.equal(sanitizeNav({ magnify: false }).magnify, false);
	assert.equal(sanitizeNav({ magnify: 'no' }).magnify, true);
});
