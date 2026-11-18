// 1.11 (step 11): what the pages of T-Kalender and Kontakte show before
// their views are built (steps 15/16 fill them). Never an empty list for
// data that cannot be read: without the system data volume titand answers
// 503 (calendar_unavailable, contacts_unavailable) or `volume:false`, and
// the page says "nicht verfügbar".
import { ApiError } from '../api.ts';

export type ShellState =
	| { kind: 'loading' }
	| { kind: 'unavailable' }
	| { kind: 'error'; error: unknown }
	| { kind: 'ready' };

/** GET /calendar/access: {enabled, volume}; data only with the volume. */
export function calendarShell(access: unknown): ShellState {
	const a = (access ?? {}) as { volume?: unknown };
	return a.volume === true ? { kind: 'ready' } : { kind: 'unavailable' };
}

export type Book = { id: string; name: string; count: number };

/** GET /contacts/books: the address books, checked; null for anything else. */
export function sanitizeBooks(raw: unknown): Book[] | null {
	if (!Array.isArray(raw)) return null;
	return raw
		.filter((b): b is Record<string, unknown> => !!b && typeof b === 'object')
		.map((b) => ({
			id: typeof b.id === 'string' ? b.id : '',
			name: typeof b.name === 'string' ? b.name : '',
			count: typeof b.count === 'number' && b.count > 0 ? Math.floor(b.count) : 0,
		}))
		.filter((b) => b.id !== '');
}

/** A failed request: 503 calendar_/contacts_unavailable is "not available" (no volume), anything else an error with its text. */
export function shellOfError(err: unknown): ShellState {
	if (
		err instanceof ApiError &&
		(err.code === 'calendar_unavailable' || err.code === 'contacts_unavailable')
	)
		return { kind: 'unavailable' };
	return { kind: 'error', error: err };
}
