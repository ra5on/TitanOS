import type { FileEntry } from './api.ts';
export type FileSort = 'name' | 'size' | 'modified';

/** Filter only the loaded directory; never rewrite names or mutate the API data. */
export function fileList(
	entries: FileEntry[],
	query: string,
	sort: FileSort,
	descending: boolean,
	locale: string,
): FileEntry[] {
	const needle = query.normalize('NFC').toLocaleLowerCase(locale);
	const collator = new Intl.Collator(locale, { numeric: true, sensitivity: 'base' });
	const nameOrder = (a: FileEntry, b: FileEntry) =>
		collator.compare(a.name, b.name) || a.name.localeCompare(b.name, locale);
	return entries
		.filter((entry) => entry.name.normalize('NFC').toLocaleLowerCase(locale).includes(needle))
		.sort((a, b) => {
			if (a.dir !== b.dir) return a.dir ? -1 : 1;
			let order = 0;
			if (sort === 'size' && !a.dir) order = a.size - b.size;
			if (sort === 'modified')
				order = (Date.parse(a.modified) || 0) - (Date.parse(b.modified) || 0);
			return (order || nameOrder(a, b)) * (descending ? -1 : 1);
		});
}
