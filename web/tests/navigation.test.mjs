import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
	arrangeNav,
	moveNav,
	NAV,
	navForRole,
	sanitizeNav,
	visibleNav,
} from '../src/lib/navigation.ts';
import { defaultAppearance, sanitizeAppearance } from '../src/lib/theme/themes.ts';

const ids = (items) => items.map((item) => item.id);

test('standard order without preferences', () => {
	assert.deepEqual(ids(visibleNav(NAV, { order: [], hidden: [] })), ids(NAV));
	assert.deepEqual(ids(navForRole('user')), ['dashboard', 'files', 'settings']);
});

test('saved order wins and missing tabs keep their standard neighbour', () => {
	const order = ['tools', 'dashboard', 'storage', 'settings'];
	const out = ids(arrangeNav(NAV, { order, hidden: [] }));
	assert.equal(out[0], 'tools');
	assert.equal(out.indexOf('shares'), out.indexOf('storage') + 1);
	assert.equal(out.length, NAV.length);
	assert.deepEqual([...out].sort(), ids(NAV).sort());
});

test('hidden tabs disappear, settings never', () => {
	const out = ids(visibleNav(NAV, { order: [], hidden: ['docker', 'settings', 'vms'] }));
	assert.ok(!out.includes('docker') && !out.includes('vms'));
	assert.ok(out.includes('settings'));
	assert.deepEqual(sanitizeNav({ hidden: ['settings', 'docker'] }).hidden, ['docker']);
});

test('moving swaps neighbours and keeps ids of other roles', () => {
	const users = navForRole('user');
	const order = moveNav(users, { order: ['tools'], hidden: [] }, 'files', -1);
	assert.deepEqual(order, ['files', 'dashboard', 'settings', 'tools']);
	assert.deepEqual(moveNav(users, { order: ['tools'], hidden: [] }, 'dashboard', -1), [
		'dashboard',
		'files',
		'settings',
		'tools',
	]);
});

test('sanitizing keeps unknown but valid ids and drops junk', () => {
	assert.deepEqual(sanitizeNav({ order: ['fleet', 'fleet', 'Bad Id', 7, 'docker'], hidden: 'x' }), {
		order: ['fleet', 'docker'],
		hidden: [],
		layout: 'top',
	});
	assert.deepEqual(sanitizeNav(null), { order: [], hidden: [], layout: 'top' });
	assert.deepEqual(defaultAppearance().nav, { order: [], hidden: [], layout: 'top' });
	assert.deepEqual(sanitizeAppearance({ nav: { order: ['tools'] } }).nav, {
		order: ['tools'],
		hidden: [],
		layout: 'top',
	});
});

test('the sidebar is kept, anything else means the header', () => {
	assert.equal(sanitizeNav({ layout: 'side' }).layout, 'side');
	assert.equal(sanitizeNav({ layout: 'left' }).layout, 'top');
	assert.equal(
		sanitizeAppearance({ nav: { layout: 'side', hidden: ['apps'] } }).nav.layout,
		'side',
	);
});

test('every tab has an icon for the sidebar', () => {
	for (const item of NAV) assert.ok(item.icon, item.id);
});
