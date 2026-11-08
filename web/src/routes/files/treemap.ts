// Squarified treemap (Bruls, Huizing, van Wijk 2000): lays items out as
// rectangles whose areas match their sizes and whose sides stay close to
// squares. PLAN 72, folders as areas in the storage analysis.

export type Area<T> = { item: T; x: number; y: number; w: number; h: number };

/** Lays out items (largest first is best) in a w × h box; zero sizes are left out. */
export function treemap<T>(items: T[], w: number, h: number, size: (item: T) => number): Area<T>[] {
	const list = items
		.map((item) => ({ item, value: Math.max(0, size(item)) }))
		.filter((entry) => entry.value > 0)
		.sort((a, b) => b.value - a.value);
	const total = list.reduce((sum, entry) => sum + entry.value, 0);
	if (!list.length || w <= 0 || h <= 0 || total <= 0) return [];
	const scale = (w * h) / total;
	const out: Area<T>[] = [];
	let box = { x: 0, y: 0, w, h };
	let row: { item: T; area: number }[] = [];

	const worst = (r: { area: number }[], side: number) => {
		const sum = r.reduce((s, e) => s + e.area, 0);
		let max = 0,
			min = Infinity;
		for (const e of r) {
			max = Math.max(max, e.area);
			min = Math.min(min, e.area);
		}
		const s2 = sum * sum;
		return Math.max((side * side * max) / s2, s2 / (side * side * min));
	};
	const place = (r: { item: T; area: number }[]) => {
		const sum = r.reduce((s, e) => s + e.area, 0);
		if (box.w >= box.h) {
			// A column on the left side.
			const cw = sum / box.h;
			let y = box.y;
			for (const e of r) {
				const eh = e.area / cw;
				out.push({ item: e.item, x: box.x, y, w: cw, h: eh });
				y += eh;
			}
			box = { x: box.x + cw, y: box.y, w: box.w - cw, h: box.h };
		} else {
			// A row on the top side.
			const rh = sum / box.w;
			let x = box.x;
			for (const e of r) {
				const ew = e.area / rh;
				out.push({ item: e.item, x, y: box.y, w: ew, h: rh });
				x += ew;
			}
			box = { x: box.x, y: box.y + rh, w: box.w, h: box.h - rh };
		}
	};
	for (const entry of list) {
		const next = { item: entry.item, area: entry.value * scale };
		const side = Math.min(box.w, box.h);
		if (row.length && worst([...row, next], side) > worst(row, side)) {
			place(row);
			row = [];
		}
		row.push(next);
	}
	if (row.length) place(row);
	return out;
}
