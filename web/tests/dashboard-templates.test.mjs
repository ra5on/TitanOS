import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
	ADMIN_TILES,
	DASHBOARD_TEMPLATES,
	TILES,
	templateLayout,
} from '../src/lib/dashboard/tiles.ts';

test('templates use known tiles and respect the account', () => {
	for (const [name, ids] of Object.entries(DASHBOARD_TEMPLATES)) {
		for (const id of ids) assert.ok(TILES[id], `${name}: ${id}`);
		const admin = templateLayout(name, true);
		assert.equal(admin.length, ids.length);
		const user = templateLayout(name, false);
		assert.ok(
			user.every((p) => !ADMIN_TILES.includes(p.id)),
			name,
		);
	}
	assert.equal(templateLayout('storage', true)[0].span, 2);
	assert.equal(templateLayout('minimal', true)[0].span, 1);
	assert.deepEqual(
		templateLayout('remote', false).map((p) => p.id),
		['network', 'system', 'cpu', 'memory'],
	);
});
