// PLAN 79-7: the live view's own arrangement of the camera tiles. Like the
// desktop's widgets a tile has a place and a size on a grid; here the grid
// has a fixed number of columns that share the width of the view, so an
// arrangement keeps its shape in a smaller or larger window. Pure functions:
// the page and the desktop's settings (prefs.ts) both use them.

/** Columns of the live grid; a cell is as high as it is wide. */
export const LIVE_COLS = 24;
export const TILE_MIN_W = 4;
export const TILE_MIN_H = 3;
const MAX_ROWS = 400;
const MAX_TILES = 64;

export type Tile = { id: string; x: number; y: number; w: number; h: number };

export type NvrPrefs = {
	/** The account's own arrangement (empty or `custom` false: the standard grid). */
	tiles: Tile[];
	custom: boolean;
	/** The order of the stacked tiles at phone width; empty = the cameras' order. */
	phoneOrder: string[];
	/** Events beside the pictures. */
	events: boolean;
	/** The camera's name in its picture. */
	names: boolean;
	/** What is in the picture right now (person, car …) as a mark in the picture. */
	marks: boolean;
	/** The red dot of a camera that records. */
	recDot: boolean;
	/** Full screen hides the bar above the pictures. */
	hideBar: boolean;
};

export const defaultNvrPrefs = (): NvrPrefs => ({
	tiles: [],
	custom: false,
	phoneOrder: [],
	events: false,
	names: true,
	marks: true,
	recDot: true,
	hideBar: true,
});

const CAMERA_ID = /^[a-z][a-z0-9_]{0,31}$/;

function int(value: unknown, min: number, max: number, fallback: number): number {
	return typeof value === 'number' && Number.isFinite(value)
		? Math.min(max, Math.max(min, Math.round(value)))
		: fallback;
}

/** Keeps a tile inside the grid and above its smallest size. */
export function clampTile(tile: Tile): Tile {
	const w = int(tile.w, TILE_MIN_W, LIVE_COLS, 12);
	const h = int(tile.h, TILE_MIN_H, 60, 7);
	return {
		id: tile.id,
		w,
		h,
		x: int(tile.x, 0, LIVE_COLS - w, 0),
		y: int(tile.y, 0, MAX_ROWS, 0),
	};
}

/** Reads what the NAS stored; anything unknown or broken falls back to the standard. */
export function sanitizeNvrPrefs(input: unknown): NvrPrefs {
	const base = defaultNvrPrefs();
	if (typeof input !== 'object' || input === null || Array.isArray(input)) return base;
	const v = input as Record<string, unknown>;
	const seen = new Set<string>();
	const tiles = Array.isArray(v.tiles)
		? v.tiles
				.filter(
					(t): t is Record<string, unknown> =>
						typeof t === 'object' &&
						t !== null &&
						typeof (t as Record<string, unknown>).id === 'string' &&
						CAMERA_ID.test((t as Record<string, unknown>).id as string),
				)
				.filter((t) => !seen.has(t.id as string) && !!seen.add(t.id as string))
				.slice(0, MAX_TILES)
				.map((t) => clampTile(t as unknown as Tile))
		: [];
	const phoneOrder = Array.isArray(v.phoneOrder)
		? [
				...new Set(
					v.phoneOrder.filter((id): id is string => typeof id === 'string' && CAMERA_ID.test(id)),
				),
			].slice(0, MAX_TILES)
		: [];
	return {
		tiles,
		custom: v.custom === true && tiles.length > 0,
		phoneOrder,
		events: v.events === true,
		names: v.names !== false,
		marks: v.marks !== false,
		recDot: v.recDot !== false,
		hideBar: v.hideBar !== false,
	};
}

/** The standard arrangement: an even grid, one camera alone fills the width. */
export function gridTiles(ids: string[]): Tile[] {
	const cols = ids.length <= 1 ? 1 : ids.length <= 4 ? 2 : 3;
	const w = LIVE_COLS / cols;
	const h = Math.round((w * 9) / 16);
	return ids.map((id, i) => ({ id, x: (i % cols) * w, y: Math.floor(i / cols) * h, w, h }));
}

const overlaps = (a: Tile, b: Tile) =>
	a.x < b.x + b.w && b.x < a.x + a.w && a.y < b.y + b.h && b.y < a.y + a.h;

/** The first free place for a tile of this size (row by row). */
export function freePlace(tiles: Tile[], w: number, h: number): { x: number; y: number } {
	for (let y = 0; y < MAX_ROWS; y++)
		for (let x = 0; x + w <= LIVE_COLS; x++)
			if (!tiles.some((o) => overlaps(o, { id: '', x, y, w, h }))) return { x, y };
	return { x: 0, y: MAX_ROWS };
}

/**
 * The tiles to show for these cameras: the account's arrangement without
 * cameras that are gone, and a free place for every camera it does not know
 * yet. Without an own arrangement: the standard grid.
 */
export function tilesFor(prefs: NvrPrefs, ids: string[]): Tile[] {
	if (!prefs.custom) return gridTiles(ids);
	const known = new Set(ids);
	const tiles = prefs.tiles.filter((t) => known.has(t.id)).map(clampTile);
	for (const id of ids) {
		if (tiles.some((t) => t.id === id)) continue;
		const w = 8;
		const h = 5;
		tiles.push({ id, w, h, ...freePlace(tiles, w, h) });
	}
	return tiles;
}

/** The cameras in the order of the phone's stack; new cameras follow at the end. */
export function phoneOrderFor(prefs: NvrPrefs, ids: string[]): string[] {
	const known = new Set(ids);
	const first = prefs.phoneOrder.filter((id) => known.has(id));
	return [...first, ...ids.filter((id) => !first.includes(id))];
}

/** Moves or sizes a tile by whole cells; the result stays inside the grid. */
export function dragTile(start: Tile, mode: 'move' | 'size', dx: number, dy: number): Tile {
	if (mode === 'move')
		return clampTile({ ...start, x: start.x + dx, y: Math.max(0, start.y + dy) });
	return clampTile({
		...start,
		w: Math.max(TILE_MIN_W, Math.min(LIVE_COLS - start.x, start.w + dx)),
		h: Math.max(TILE_MIN_H, start.h + dy),
	});
}

/**
 * Tiles never lie on each other: the tile just moved or sized stays where it
 * was put, and every tile it would cover moves down, below what is already
 * placed (top to bottom, so a chain of tiles moves together).
 */
export function settle(tiles: Tile[], fixedId: string): Tile[] {
	const fixed = tiles.find((t) => t.id === fixedId);
	if (!fixed) return tiles;
	const placed: Tile[] = [fixed];
	const rest = tiles.filter((t) => t.id !== fixedId).sort((a, b) => a.y - b.y || a.x - b.x);
	for (const tile of rest) {
		let next = { ...tile };
		for (
			let hit = placed.find((p) => overlaps(p, next));
			hit;
			hit = placed.find((p) => overlaps(p, next))
		)
			next = { ...next, y: hit.y + hit.h };
		placed.push(next);
	}
	return tiles.map((t) => placed.find((p) => p.id === t.id) ?? t);
}

/** Rows the arrangement needs. */
export function layoutRows(tiles: Tile[]): number {
	return tiles.reduce((max, t) => Math.max(max, t.y + t.h), 0);
}
