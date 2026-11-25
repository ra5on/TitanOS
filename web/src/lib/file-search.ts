// Search preferences carry only the query. Results always come from the server
// with the current account's rights, including when returning through history.
export type SearchFilters = {
	root: string;
	dir: string;
	query: string;
	extension: string;
	content: boolean;
	minimum?: number;
	maximum?: number;
	after: string;
	before: string;
};
export type SavedSearch = SearchFilters & { name: string };
export type SearchPosition = SearchFilters & { scroll: number };

export function savedSearches(value: unknown): SavedSearch[] {
	if (!Array.isArray(value)) return [];
	const validNumber = (n: unknown) =>
		n === undefined || (typeof n === 'number' && Number.isFinite(n) && n >= 0);
	return value
		.filter(
			(s): s is SavedSearch =>
				s &&
				typeof s.name === 'string' &&
				s.name.trim() &&
				s.name.length <= 80 &&
				['root', 'dir', 'query', 'extension', 'after', 'before'].every(
					(key) => typeof s[key] === 'string' && s[key].length <= 4096,
				) &&
				typeof s.content === 'boolean' &&
				validNumber(s.minimum) &&
				validNumber(s.maximum),
		)
		.slice(0, 40);
}
