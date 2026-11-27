// Only coordinates cross the editor boundary. Root, path, version and account
// come from the TitanOS window that handed the file to this exact frame.
export type OfficeRegion =
	| { paragraph: number }
	| { sheet: string; range: string }
	| { page: number }
	| { slide: number; paragraph?: number };
export type EditorSelection =
	| { status: 'selected'; items: OfficeRegion[] }
	| { status: 'none' | 'dirty' | 'unsupported' | 'unbound' | 'legacy' | 'changed' };
export const selectionId = () =>
	Array.from(crypto.getRandomValues(new Uint8Array(16)), (b) =>
		b.toString(16).padStart(2, '0'),
	).join('');
const number = (n: unknown): n is number =>
	typeof n === 'number' && Number.isInteger(n) && n >= 1 && n <= 100000;
function rectangle(value: unknown): value is string {
	if (
		typeof value !== 'string' ||
		!/^[A-Z]{1,3}[1-9]\d{0,6}(?::[A-Z]{1,3}[1-9]\d{0,6})?$/.test(value)
	)
		return false;
	const points = value.split(':').map((cell) => {
		const letters = /^[A-Z]+/.exec(cell)![0];
		const col = [...letters].reduce((n, c) => n * 26 + c.charCodeAt(0) - 64, 0);
		return { col, row: Number(cell.slice(letters.length)) };
	});
	const first = points[0],
		last = points.at(-1)!;
	return (
		last.row <= 1048576 &&
		last.col <= 16384 &&
		first.row <= last.row &&
		first.col <= last.col &&
		(last.row - first.row + 1) * (last.col - first.col + 1) <= 200
	);
}
export function editorSelection(app: string, value: Record<string, unknown>): EditorSelection {
	if (
		value.status === 'none' ||
		value.status === 'dirty' ||
		value.status === 'unbound' ||
		value.status === 'unsupported'
	)
		return { status: value.status };
	if (
		value.status !== 'selected' ||
		!Array.isArray(value.items) ||
		!value.items.length ||
		value.items.length > 16
	)
		return { status: 'unsupported' };
	const items: OfficeRegion[] = [];
	for (const item of value.items) {
		if (!item || typeof item !== 'object' || Array.isArray(item)) return { status: 'unsupported' };
		const keys = Object.keys(item).sort().join(',');
		if (app === 't-word' && keys === 'paragraph' && number(item.paragraph))
			items.push({ paragraph: item.paragraph });
		else if (
			app === 't-exel' &&
			keys === 'range,sheet' &&
			typeof item.sheet === 'string' &&
			item.sheet.trim() &&
			item.sheet.length <= 128 &&
			!/[\p{Cc}]/u.test(item.sheet) &&
			rectangle(item.range)
		)
			items.push({ sheet: item.sheet, range: item.range });
		else if (app === 't-pdf' && keys === 'page' && number(item.page))
			items.push({ page: item.page });
		else if (
			app === 't-deck' &&
			number(item.slide) &&
			(keys === 'slide' || (keys === 'paragraph,slide' && number(item.paragraph)))
		)
			items.push(
				keys === 'slide' ? { slide: item.slide } : { slide: item.slide, paragraph: item.paragraph },
			);
		else return { status: 'unsupported' };
	}
	return { status: 'selected', items };
}
export function regionTitle(r: OfficeRegion, de: boolean): string {
	if ('sheet' in r) return r.sheet + ' · ' + r.range;
	if ('page' in r) return (de ? 'Seite ' : 'Page ') + r.page;
	if ('slide' in r)
		return (
			(de ? 'Folie ' : 'Slide ') +
			r.slide +
			(r.paragraph ? ' · ' + (de ? 'Absatz ' : 'Paragraph ') + r.paragraph : '')
		);
	return (de ? 'Absatz ' : 'Paragraph ') + r.paragraph;
}
