import { test } from 'node:test';
import assert from 'node:assert/strict';
import { defaultLayout, defaultPlacement, sanitizeLayout, TILE_IDS, TILES } from '../src/lib/dashboard/tiles.ts';

test('new optional accelerator tiles can be added to existing dashboards', () => {
	const existing = defaultLayout();
	assert.deepEqual(sanitizeLayout({ tiles: existing }), existing);
	for (const id of ['igpu', 'npu', 'nvidia']) {
		assert.ok(TILE_IDS.includes(id));
		assert.ok(!existing.some((tile) => tile.id === id));
		const tile = defaultPlacement(id);
		assert.ok(tile.w >= TILES[id].minW && tile.h >= TILES[id].minH);
		const restored = sanitizeLayout({ tiles: [...existing, tile] });
		assert.equal(restored.at(-1).id, id);
		assert.equal(restored.at(-1).w, tile.w);
		assert.equal(restored.at(-1).h, tile.h);
	}
});

test('saved accelerator layouts reject duplicates and clamp corrupt sizes', () => {
	const tiles = sanitizeLayout({ tiles: [{ id: 'nvidia', w: 99, h: -5, x: 30 }, { id: 'nvidia' }, { id: 'unknown' }, { id: 'npu' }] });
	assert.deepEqual(tiles.map((tile) => tile.id), ['nvidia', 'npu']);
	assert.equal(tiles[0].w, 12);
	assert.equal(tiles[0].x, 0);
	assert.equal(tiles[0].h, TILES.nvidia.minH);
	assert.equal(tiles[1].h, defaultPlacement('npu').h);
});

test('the add list offers no tile that would repeat another one', async () => {
	const { addableTiles } = await import('../src/lib/dashboard/tiles.ts');
	const admin = { admin: true, hardware: [] };
	const none = addableTiles([], admin);
	for (const id of ['accelerators', 'igpu', 'npu', 'nvidia']) assert.ok(!none.includes(id), id);
	assert.ok(none.includes('tailscale'), 'unknown plugin state stays addable');
	assert.ok(!addableTiles([], { ...admin, tailscale: false, netbird: false }).includes('netbird'));

	// One kind of device: the all-devices tile is the same as the kind tile.
	const one = addableTiles([], { ...admin, hardware: ['igpu'] });
	assert.ok(one.includes('accelerators'));
	assert.ok(!one.includes('igpu'));
	assert.ok(!addableTiles([{ id: 'igpu' }], { ...admin, hardware: ['igpu'] }).includes('accelerators'));

	// Two kinds: each kind on its own, only kinds that exist.
	const two = addableTiles([{ id: 'accelerators' }], { ...admin, hardware: ['igpu', 'npu'] });
	assert.deepEqual(two.filter((id) => ['accelerators', 'igpu', 'npu', 'nvidia'].includes(id)), ['igpu', 'npu']);

	// Kinds already shown, also as copies, are duplicated in the editor instead.
	const shown = addableTiles([{ id: 'cpu@1a2b3c4d' }, { id: 'vm:debian' }], admin);
	assert.ok(!shown.includes('cpu'));
	assert.ok(!addableTiles([], { admin: false, hardware: [] }).includes('docker'));
});
