import { sanitizeLayout, type TilePlacement } from './tiles.ts';

export function columnCount(value: unknown): number {
	return typeof value === 'number' && Number.isFinite(value)
		? Math.max(1, Math.min(4, Math.round(value)))
		: 2;
}
export function columnWidth(value: unknown): number {
	return typeof value === 'number' && Number.isFinite(value)
		? Math.max(280, Math.min(700, Math.round(value)))
		: 350;
}
export function visibleColumns(width: number, wanted: number, minimum: number): number {
	return Math.min(
		columnCount(wanted),
		Math.max(1, Math.floor((width + 12) / (columnWidth(minimum) + 12))),
	);
}
/** Reading order stays independent of viewport; v6 additionally retains optional lane positions. */
export function flowLayout(input: unknown): TilePlacement[] {
	const v = input as { version?: number } | null;
	const layout = sanitizeLayout(input);
	if ((v?.version ?? 0) < 5)
		layout.sort((a, b) => (a.y ?? 1000) - (b.y ?? 1000) || (a.x ?? 0) - (b.x ?? 0));
	return layout.map((p) => ({
		...p,
		span: p.span ?? Math.max(1, Math.ceil(p.w / 6)),
		autoHeight: p.autoHeight ?? true,
	}));
}
/** Keep a useful 12-column representation for the preceding app on rollback. */
export function legacyPositions(layout: TilePlacement[], columns: number): TilePlacement[] {
	let x = 0,
		y = 0,
		rowHeight = 0;
	return layout.map((p) => {
		const span = Math.min(p.span ?? 1, columns),
			w = span * (12 / columns);
		if (x + w > 12) {
			x = 0;
			y += rowHeight;
			rowHeight = 0;
		}
		const next = { ...p, x, y, w };
		x += w;
		rowHeight = Math.max(rowHeight, p.h);
		return next;
	});
}
