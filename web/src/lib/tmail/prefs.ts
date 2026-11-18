// 1.12: what T-Mail keeps per device – the widths of the columns, the
// favourite folders, the folded folders and whether conversations are
// shown. Only in this browser's localStorage (nothing on the stick); a
// browser that keeps nothing (private window, blocked storage) works with
// the defaults.

const PREFIX = 'titanos.mail.';

export function readPref<T>(name: string, fallback: T, check: (v: unknown) => v is T): T {
	try {
		const raw = localStorage.getItem(PREFIX + name);
		if (raw === null) return fallback;
		const v: unknown = JSON.parse(raw);
		return check(v) ? v : fallback;
	} catch {
		return fallback;
	}
}

export function writePref(name: string, value: unknown, fallback?: unknown) {
	try {
		if (value === undefined || JSON.stringify(value) === JSON.stringify(fallback))
			localStorage.removeItem(PREFIX + name);
		else localStorage.setItem(PREFIX + name, JSON.stringify(value));
	} catch {
		// Not kept in this browser; it applies now.
	}
}

export const isStrings = (v: unknown): v is string[] =>
	Array.isArray(v) && v.length <= 500 && v.every((x) => typeof x === 'string' && x.length <= 2100);
export const isBool = (v: unknown): v is boolean => typeof v === 'boolean';
export const isNumber = (v: unknown): v is number => typeof v === 'number' && Number.isFinite(v);

/** A folder of a mailbox as one string (favourites, folded folders). */
export const folderRef = (account: string, path: string) => `${account}\u0000${path}`;

/** The keys between an anchor and a target in the shown order (Shift+click). */
export function rangeOf(order: string[], anchor: string, target: string): string[] {
	const a = order.indexOf(anchor);
	const b = order.indexOf(target);
	if (a < 0 || b < 0) return b < 0 ? [] : [target];
	return order.slice(Math.min(a, b), Math.max(a, b) + 1);
}
