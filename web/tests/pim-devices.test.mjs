import { test } from 'node:test';
import assert from 'node:assert/strict';
import { lastSync, passwordGroups } from '../src/lib/pim/devices.ts';

// TECHNIK 3.5: "zuletzt abgeglichen heute 14:05 (Kalender)", "noch nie",
// "vor dem Neustart: 12.11.".
test('last sync of a device in the words of the setup page', () => {
	const now = new Date(2026, 10, 17, 15, 0);
	const device = (lastSync, lastKind = 'caldav', before = false) => ({
		id: 'd1',
		name: 'iPhone',
		created: '2026-11-02T10:00:00Z',
		lastSync,
		lastKind,
		lastSyncBeforeRestart: before,
	});
	assert.deepEqual(lastSync(device(null), now), { when: 'never' });
	const today = new Date(2026, 10, 17, 14, 5);
	assert.deepEqual(lastSync(device(today.toISOString()), now), {
		when: 'today',
		at: today,
		kind: 'caldav',
	});
	const yesterday = new Date(2026, 10, 16, 23, 59);
	assert.equal(lastSync(device(yesterday.toISOString(), 'carddav'), now).when, 'yesterday');
	assert.equal(lastSync(device(yesterday.toISOString(), 'carddav'), now).kind, 'carddav');
	const older = new Date(2026, 10, 12, 9, 0);
	assert.equal(lastSync(device(older.toISOString(), 'other'), now).when, 'date');
	assert.equal(lastSync(device(older.toISOString(), 'other'), now).kind, '');
	assert.deepEqual(lastSync(device(older.toISOString(), 'caldav', true), now), {
		when: 'beforeRestart',
		at: older,
	});
});

test('a device password breaks only between its groups of four', () => {
	assert.deepEqual(passwordGroups('abcd-efgh-2345'), ['abcd-', 'efgh-', '2345']);
	assert.equal(passwordGroups('abcd-efgh-2345').join(''), 'abcd-efgh-2345');
});
