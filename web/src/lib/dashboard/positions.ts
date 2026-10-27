/** Pixel positions with independent lane heights; DOM order remains the reading order. */
export type Position = { id: string; span?: number; column?: number; offset?: number };
export type Rect = { column: number; span: number; top: number; height: number };
export function positions(
	items: Position[],
	columns: number,
	heights: Record<string, number>,
	explicit = true,
) {
	const ends = Array<number>(columns).fill(0),
		rects: Record<string, Rect> = {};
	let cursor = 0;
	for (const item of items) {
		const span = Math.min(columns, item.span ?? 1);
		let column =
			explicit && item.column !== undefined ? Math.min(item.column, columns - span) : cursor;
		if (column + span > columns) column = 0;
		const top =
			Math.max(...ends.slice(column, column + span)) + (explicit ? (item.offset ?? 0) : 0);
		const height = Math.max(1, heights[item.id] ?? 80);
		rects[item.id] = { column, span, top, height };
		for (let lane = column; lane < column + span; lane++) ends[lane] = top + height + 12;
		cursor = (column + span) % columns;
	}
	return { rects, height: Math.max(0, ...ends) ? Math.max(...ends) - 12 : 0 };
}

/**
 * Reading order after dropping `id` at the drop point `at` (pixels, column
 * index already snapped). With `explicit` columns the tile goes behind the last
 * tile of its target lanes that starts above the drop point, so tiles above it
 * in that lane keep their place and tiles in other lanes keep their column; the
 * gap above becomes its offset. Without explicit columns (fewer visible columns
 * than wished for) only the reading order changes.
 */
export function dropLayout<T extends Position>(
	items: T[],
	id: string,
	rects: Record<string, Rect>,
	at: { column: number; top: number },
	columns: number,
	explicit: boolean,
): T[] {
	const current = items.find((p) => p.id === id);
	if (!current) return items;
	const span = rects[id]?.span ?? 1,
		col = at.column;
	const overlaps = (r: Rect) => r.column < col + span && r.column + r.span > col;
	const rest = items
		.filter((p) => p.id !== id)
		.map((p) => (explicit ? { ...p, column: rects[p.id].column } : p));
	if (!explicit) {
		let index = rest.findIndex(
			(p) =>
				rects[p.id].top > at.top + 8 ||
				(Math.abs(rects[p.id].top - at.top) <= 8 && rects[p.id].column >= col),
		);
		if (index < 0) index = rest.length;
		rest.splice(index, 0, current);
		return rest;
	}
	let index = 0;
	rest.forEach((p, i) => {
		if (overlaps(rects[p.id]) && rects[p.id].top <= at.top + 8) index = i + 1;
	});
	const before = rest.slice(0, index);
	const packed = positions(
		before,
		columns,
		Object.fromEntries(Object.entries(rects).map(([id, r]) => [id, r.height])),
	);
	const end = Math.max(
		0,
		...before
			.filter((p) => overlaps(packed.rects[p.id]))
			.map((p) => packed.rects[p.id].top + packed.rects[p.id].height + 12),
	);
	rest.splice(index, 0, {
		...current,
		column: col,
		offset: Math.min(8000, Math.max(0, at.top - end)),
	});
	return rest;
}
