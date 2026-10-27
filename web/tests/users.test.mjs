import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { filterAccounts, mayLock, sanitizeStatusFilter } from '../src/lib/users-view.ts';

const accounts = [
	{ name: 'alex', role: 'owner', disabled: false },
	{ name: 'chris', role: 'owner', disabled: true },
	{ name: 'bea', role: 'admin', disabled: false },
	{ name: 'gast', role: 'user', disabled: true },
];

test('the status filter shows active or locked accounts and refuses unknown values', () => {
	assert.deepEqual(
		filterAccounts(accounts, 'all').map((a) => a.name),
		['alex', 'chris', 'bea', 'gast'],
	);
	assert.deepEqual(
		filterAccounts(accounts, 'active').map((a) => a.name),
		['alex', 'bea'],
	);
	assert.deepEqual(
		filterAccounts(accounts, 'locked').map((a) => a.name),
		['chris', 'gast'],
	);
	assert.equal(sanitizeStatusFilter('locked'), 'locked');
	assert.equal(sanitizeStatusFilter('../x'), 'all');
	assert.equal(sanitizeStatusFilter(undefined), 'all');
});

test('lock is offered following the role rules of the server', () => {
	const owner = { name: 'alex', role: 'owner' };
	const admin = { name: 'bea', role: 'admin' };
	const byName = (name) => accounts.find((a) => a.name === name);
	assert.equal(mayLock(owner, byName('alex'), accounts), false, 'not oneself');
	assert.equal(mayLock(admin, byName('bea'), accounts), false, 'not oneself');
	assert.equal(mayLock(admin, byName('chris'), accounts), false, 'owners only by owners');
	assert.equal(mayLock(admin, byName('gast'), accounts), true);
	assert.equal(mayLock(owner, byName('chris'), accounts), true, 'unlocking an owner');
	assert.equal(mayLock({ name: 'gast', role: 'user' }, byName('bea'), accounts), false);
	assert.equal(mayLock(undefined, byName('bea'), accounts), false);
	// alex is the only owner who is not locked: another owner may not lock alex.
	assert.equal(mayLock({ name: 'dana', role: 'owner' }, byName('alex'), accounts), false);
	const two = [...accounts, { name: 'dana', role: 'owner', disabled: false }];
	assert.equal(mayLock({ name: 'dana', role: 'owner' }, two[0], two), true);
});

test('lock texts exist in both languages', () => {
	for (const lang of ['de', 'en']) {
		const texts = JSON.parse(
			readFileSync(new URL(`../src/lib/i18n/${lang}.json`, import.meta.url), 'utf8'),
		);
		for (const key of [
			'users.lock',
			'users.unlock',
			'users.lockTitle',
			'users.lockText',
			'users.unlockTitle',
			'users.unlockText',
			'users.statusLocked',
			'users.filterLocked',
			'error.account_disabled',
			'error.cannot_lock_self',
			'error.last_active_owner',
		]) {
			assert.ok(texts[key], `${lang}: ${key}`);
		}
	}
});
