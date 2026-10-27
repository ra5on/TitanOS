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
