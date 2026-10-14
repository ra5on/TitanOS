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
