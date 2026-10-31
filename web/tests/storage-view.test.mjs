import { test } from 'node:test';
import assert from 'node:assert/strict';
import { storageNavigation } from '../src/lib/storage-view.ts';

test('storageNavigation: starts on the volumes with defaults', () => {
	const nav = storageNavigation('alex');
	assert.deepEqual(nav, {
		account: 'alex',
		tab: 'volumes',
		query: '',
		filter: 'all',
		volume: '',
		sort: 'name',
		selected: '',
		disk: '',
		scroll: 0,
	});
});

test('storageNavigation: kept for the same account, fresh for another', () => {
	const nav = storageNavigation('alex');
	nav.tab = 'disks';
	nav.scroll = 300;
	assert.equal(storageNavigation('alex'), nav);
	assert.equal(storageNavigation('alex').tab, 'disks');
	const other = storageNavigation('sam');
	assert.notEqual(other, nav);
	assert.equal(other.tab, 'volumes');
	assert.equal(other.scroll, 0);
});
