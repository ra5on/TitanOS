// The tiles of the dashboard and their layout in the 12-column grid.
// Individual VMs and containers use stable names in their saved tile IDs.

import type { MessageKey } from '#lib/i18n/index.svelte.ts';

export const COLUMNS = 12;
/** Height of one grid row and the gap between tiles, in px. */
export const ROW_HEIGHT = 72;
export const GAP = 16;

export const TILE_IDS = ['system', 'volumes', 'network', 'disks', 'igpu', 'npu', 'nvidia'] as const;
export type StaticTileId = (typeof TILE_IDS)[number];
export type TileId = StaticTileId | `vm:${string}` | `container:${string}`;

export type TileSpec = { title: MessageKey; minW: number; minH: number };

export const TILES: Record<StaticTileId, TileSpec> = {
	system: { title: 'tile.system', minW: 3, minH: 4 },
	volumes: { title: 'tile.volumes', minW: 4, minH: 2 },
	network: { title: 'tile.network', minW: 3, minH: 3 },
	disks: { title: 'tile.disks', minW: 3, minH: 2 },
	igpu: { title: 'tile.igpu', minW: 3, minH: 4 },
	npu: { title: 'tile.npu', minW: 3, minH: 4 },
	nvidia: { title: 'tile.nvidia', minW: 3, minH: 4 }
};

export function workload(id: TileId): { kind: 'vm' | 'container'; name: string } | undefined {
	const match = /^(vm|container):([a-zA-Z0-9][a-zA-Z0-9_.-]{0,127})$/.exec(id);
	return match ? { kind: match[1] as 'vm' | 'container', name: match[2] } : undefined;
}

export function tileSpec(id: TileId): TileSpec {
	const target = workload(id);
	return target ? { title: target.kind === 'vm' ? 'tile.vm' : 'tile.container', minW: 3, minH: 3 } : TILES[id as StaticTileId];
}

/** Position and size of a tile in grid cells. Without x/y it goes to the first free place. */
export type TilePlacement = { id: TileId; x?: number; y?: number; w: number; h: number };

export function defaultLayout(): TilePlacement[] {
	return [
		{ id: 'system', x: 0, y: 0, w: 4, h: 4 },
		{ id: 'volumes', x: 4, y: 0, w: 8, h: 4 },
		{ id: 'network', x: 0, y: 4, w: 4, h: 4 },
		{ id: 'disks', x: 4, y: 4, w: 4, h: 4 }
	];
}

/** Size a tile gets when it is shown again. */
export function defaultPlacement(id: TileId): TilePlacement {
	const { w, h } = defaultLayout().find((tile) => tile.id === id) ?? { w: 4, h: workload(id) ? 4 : 6 };
	return { id, w, h };
}

function cells(value: unknown, min: number, max: number): number | undefined {
	if (typeof value !== 'number' || !Number.isFinite(value)) return undefined;
	return Math.min(max, Math.max(min, Math.round(value)));
}

/**
 * Reads the layout stored for an account. Anything unknown or broken is
 * dropped; without a usable layout the default is used.
 */
export function sanitizeLayout(input: unknown): TilePlacement[] {
	const tiles = (input as { tiles?: unknown } | null)?.tiles;
	if (!Array.isArray(tiles)) return defaultLayout();

	const seen = new Set<TileId>();
	const layout: TilePlacement[] = [];
	for (const raw of tiles) {
		if (typeof raw !== 'object' || raw === null) continue;
		const id = (raw as { id?: unknown }).id as TileId;
		if (typeof id !== 'string' || (!TILE_IDS.includes(id as StaticTileId) && !workload(id)) || seen.has(id)) continue;
		seen.add(id);
		const fallback = defaultPlacement(id);
		const v = raw as Record<string, unknown>;
		const w = cells(v.w, tileSpec(id).minW, COLUMNS) ?? fallback.w;
		layout.push({
			id,
			x: cells(v.x, 0, COLUMNS - w),
			y: cells(v.y, 0, 1000),
			w,
			h: cells(v.h, tileSpec(id).minH, 40) ?? fallback.h
		});
	}
	return layout;
}
