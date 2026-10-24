// The tiles of the dashboard and their layout in the 12-column grid.
// Individual VMs and containers use stable names in their saved tile IDs.

import type { MessageKey } from '#lib/i18n/index.svelte.ts';

export const COLUMNS = 12;
/** Height of one grid row and the gap between tiles, in px. */
export const ROW_HEIGHT = 72;
export const GAP = 16;

export const TILE_IDS = [
	'cpu',
	'memory',
	'system',
	'volumes',
	'network',
	'disks',
	'accelerators',
	'igpu',
	'npu',
	'nvidia',
	'temperatures',
	'assistant',
	'docker',
	'vms',
	'shares',
] as const;
export type StaticTileId = (typeof TILE_IDS)[number];
export type SourceId = StaticTileId | `vm:${string}` | `container:${string}` | `app:${string}`;
export type TileId = SourceId | `${SourceId}@${string}`;
export const sourceId = (id: TileId): SourceId => id.replace(/@[a-f0-9]{8}$/, '') as SourceId;

export type TileSpec = { title: MessageKey; minW: number; minH: number };

export const TILES: Record<StaticTileId, TileSpec> = {
	cpu: { title: 'system.cpu', minW: 2, minH: 2 },
	memory: { title: 'system.memory', minW: 2, minH: 2 },
	system: { title: 'tile.system', minW: 2, minH: 2 },
	volumes: { title: 'tile.volumes', minW: 2, minH: 2 },
	network: { title: 'tile.network', minW: 2, minH: 2 },
	disks: { title: 'tile.disks', minW: 3, minH: 2 },
	temperatures: { title: 'temperatures.title', minW: 2, minH: 2 },
	accelerators: { title: 'dashboard.accelerators', minW: 2, minH: 2 },
	igpu: { title: 'tile.igpu', minW: 2, minH: 2 },
	npu: { title: 'tile.npu', minW: 2, minH: 2 },
	nvidia: { title: 'tile.nvidia', minW: 2, minH: 2 },
	assistant: { title: 'tile.assistant', minW: 3, minH: 3 },
	docker: { title: 'nav.docker', minW: 3, minH: 2 },
	vms: { title: 'nav.vms', minW: 3, minH: 2 },
	shares: { title: 'nav.shares', minW: 3, minH: 2 },
};

export function workload(
	id: TileId,
): { kind: 'vm' | 'container' | 'app'; name: string } | undefined {
	const match = /^(vm|container|app):([a-zA-Z0-9][a-zA-Z0-9_.-]{0,127})$/.exec(sourceId(id));
	return match ? { kind: match[1] as 'vm' | 'container' | 'app', name: match[2] } : undefined;
}

export function tileSpec(id: TileId): TileSpec {
	const target = workload(id);
	return target
		? {
				title:
					target.kind === 'vm'
						? 'tile.vm'
						: target.kind === 'app'
							? 'nav.docker'
							: 'tile.container',
				minW: 2,
				minH: 2,
			}
		: TILES[sourceId(id) as StaticTileId];
}

/** Position and size of a tile in grid cells. Without x/y it goes to the first free place. */
export type TilePlacement = {
	id: TileId;
	x?: number;
	y?: number;
	w: number;
	h: number;
	span?: number;
	autoHeight?: boolean;
};

export function defaultLayout(): TilePlacement[] {
	return [
		{ id: 'system', x: 0, y: 0, w: 3, h: 2 },
		{ id: 'cpu', x: 3, y: 0, w: 3, h: 2 },
		{ id: 'memory', x: 6, y: 0, w: 3, h: 2 },
		{ id: 'network', x: 9, y: 0, w: 3, h: 2 },
		{ id: 'volumes', x: 0, y: 2, w: 6, h: 4 },
		{ id: 'docker', x: 6, y: 2, w: 6, h: 4 },
		{ id: 'vms', x: 6, y: 6, w: 6, h: 3 },
		{ id: 'temperatures', x: 0, y: 6, w: 6, h: 2 },
		{ id: 'shares', x: 0, y: 8, w: 6, h: 3 },
	];
}
export function defaultWithHardware(kinds: StaticTileId[]): TilePlacement[] {
	const hardware = [...new Set(kinds)].filter((id) => ['igpu', 'npu', 'nvidia'].includes(id));
	if (!hardware.length) return defaultLayout();
	return [
		...defaultLayout().map((t) =>
			['volumes', 'temperatures', 'shares'].includes(t.id) ? { ...t, y: (t.y ?? 0) + 3 } : t,
		),
		...hardware.map((id, index) => ({
			id,
			x: index * (6 / hardware.length),
			y: 2,
			w: 6 / hardware.length,
			h: 3,
		})),
	];
}
export function duplicateId(id: TileId): TileId {
	return `${sourceId(id)}@${crypto.randomUUID().replaceAll('-', '').slice(0, 8)}` as TileId;
}
/** Size a tile gets when it is shown again. */
export function defaultPlacement(id: TileId): TilePlacement {
	const { w, h } = defaultLayout().find((tile) => tile.id === id) ?? {
		w: 4,
		h: workload(id) ? 2 : 4,
	};
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
	for (const raw of tiles.slice(0, 100)) {
		if (typeof raw !== 'object' || raw === null) continue;
		const id = (raw as { id?: unknown }).id as TileId;
		if (
			typeof id !== 'string' ||
			(id.includes('@') && !/@[a-f0-9]{8}$/.test(id)) ||
			(!TILE_IDS.includes(sourceId(id) as StaticTileId) && !workload(id)) ||
			seen.has(id)
		)
			continue;
		seen.add(id);
		const fallback = defaultPlacement(id);
		const v = raw as Record<string, unknown>;
		const w = cells(v.w, tileSpec(id).minW, COLUMNS) ?? fallback.w;
		layout.push({
			id,
			x: cells(v.x, 0, COLUMNS - w),
			y: cells(v.y, 0, 1000),
			w,
			h: cells(v.h, tileSpec(id).minH, 40) ?? fallback.h,
			...(v.span !== undefined ? { span: cells(v.span, 1, 4) ?? 1 } : {}),
			...(typeof v.autoHeight === 'boolean' ? { autoHeight: v.autoHeight } : {}),
		});
	}
	return layout;
}
