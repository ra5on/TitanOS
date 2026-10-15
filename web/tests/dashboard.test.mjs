import { test } from 'node:test';
import assert from 'node:assert/strict';
import { sanitizeLayout, defaultPlacement, workload } from '../src/lib/dashboard/tiles.ts';

test('dashboard retains separate VM and container selections through saving and rearranging', () => {
	const tiles = ['system', 'vm:home-assistant', 'vm:debian', 'container:immich_server'].map(defaultPlacement);
	const saved = sanitizeLayout({ version: 2, tiles });
	assert.deepEqual(saved.map(t => t.id), tiles.map(t => t.id));
	assert.deepEqual(workload(saved[3].id), { kind: 'container', name: 'immich_server' });
	assert.equal(sanitizeLayout({ tiles: [...tiles, { id: 'vm:../../secret' }, { id: 'unknown' }, tiles[1]] }).length, 4);
});

test('dashboard preserves explicit hiding and constrains saved geometry', () => {
	assert.deepEqual(sanitizeLayout({ tiles: [] }), []);
	const [tile] = sanitizeLayout({ tiles: [{ id: 'vm:debian', w: -1, h: 9999, x: -5 }] });
	assert.equal(tile.w, 3);
	assert.equal(tile.h, 40);
	assert.equal(tile.x, 0);
});
