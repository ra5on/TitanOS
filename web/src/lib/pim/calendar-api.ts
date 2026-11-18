// 1.11 (step 15): the requests of T-Kalender (BAU-API "Schritt 14a"). Ids
// are file names chosen by devices: always encoded in the path. A change
// made in the event window tells the calendar page (another frame of the
// same origin) over a BroadcastChannel, so it reloads.
import { api, apiFetch, dispositionName, responseError, saveBlob, ApiError } from '../api.ts';
import { goto } from '$app/navigation';
import { inWindow, type FrameMessage } from '../desktop/frame.ts';
import { setNasZone, viewZone } from './caldates.ts';
import {
	sanitizeCalendars,
	sanitizeEvent,
	sanitizeEventList,
	type CalEvent,
	type Calendar,
	type Simple,
} from './calendar.ts';

const enc = encodeURIComponent;
const bad = () => new ApiError(0, 'invalid_response', 'unexpected answer');

export type EventInput = {
	title?: string;
	start?: string;
	end?: string;
	allDay?: boolean;
	timezone?: string;
	location?: string;
	notes?: string;
	repeat?: Simple | null;
	alarms?: number[] | null;
};
export type Scope = 'this' | 'following' | 'all';
export type SaveResult = {
	id: string;
	etag: string;
	calendar: string;
	scope: Scope;
	droppedExceptions: number;
};

let zoneAsked: Promise<string> | null = null;
/**
 * The NAS zone once per page (GET /calendar/access `timezone`), set for
 * viewZone(); a failed request leaves the browser's zone.
 */
export function loadNasZone(): Promise<string> {
	zoneAsked ??= api
		.get<unknown>('/calendar/access')
		.then((a) => setNasZone((a as { timezone?: unknown } | null)?.timezone))
		.catch(() => setNasZone(''))
		.then(() => viewZone());
	return zoneAsked;
}

export async function loadCalendars(): Promise<Calendar[]> {
	const list = sanitizeCalendars(await api.get<unknown>('/calendar/calendars'));
	if (!list) throw bad();
	return list;
}

export type EventList = NonNullable<ReturnType<typeof sanitizeEventList>>;

// 1.13: the last answers of GET /calendar/events with their ETag, by query.
// The answer itself is no-store: the page sends the ETag (If-None-Match) and
// keeps its list on 304 (docs/KALENDER.md "Leistung und Live-Aktualisierung").
const answers = new Map<string, { etag: string; list: EventList }>();
const KEPT_ANSWERS = 16;

/**
 * Events of [from, to) – dates in the NAS zone – with whole titles
 * (`full=1`, no request per clipped event). `calendars`: only these (the
 * birthdays are `geburtstage`); without, all that are not hidden. An
 * answer read before with the same query and unchanged on the NAS comes
 * back as 304 and the kept list is returned.
 */
export async function loadEvents(
	from: string,
	to: string,
	calendars?: string[],
): Promise<EventList> {
	const q = new URLSearchParams({ from, to, full: '1' });
	if (calendars) q.set('calendars', calendars.join(','));
	const path = `/calendar/events?${q}`;
	const kept = answers.get(path);
	let res: Response;
	try {
		res = await apiFetch(path, kept ? { headers: { 'If-None-Match': kept.etag } } : {});
	} catch {
		throw new ApiError(0, 'unreachable', 'titand is not reachable');
	}
	if (res.status === 304 && kept) {
		answers.delete(path);
		answers.set(path, kept);
		return kept.list;
	}
	if (!res.ok) throw await responseError(res);
	const list = sanitizeEventList(await res.json().catch(() => null));
	if (!list) throw bad();
	const etag = res.headers.get('ETag');
	answers.delete(path);
	if (etag) {
		answers.set(path, { etag, list });
		while (answers.size > KEPT_ANSWERS) answers.delete(answers.keys().next().value as string);
	}
	return list;
}

export async function loadEvent(cal: string, id: string, occurrence = ''): Promise<CalEvent> {
	const q = occurrence ? `?occurrence=${enc(occurrence)}` : '';
	const ev = sanitizeEvent(await api.get<unknown>(`/calendar/events/${enc(cal)}/${enc(id)}${q}`));
	if (!ev) throw bad();
	return ev;
}

export function createEvent(calendar: string, event: EventInput) {
	return api.post<SaveResult>('/calendar/events', { calendar, event });
}

export function updateEvent(
	cal: string,
	id: string,
	body: { etag: string; scope: Scope; occurrence: string; event: EventInput; calendar?: string },
) {
	return api.put<SaveResult>(`/calendar/events/${enc(cal)}/${enc(id)}`, body);
}

export function deleteEvent(
	cal: string,
	id: string,
	p: { scope: Scope; occurrence: string; etag: string },
) {
	const q = `scope=${enc(p.scope)}&occurrence=${enc(p.occurrence)}&etag=${enc(p.etag)}`;
	return api.delete<{ removed: boolean; etag?: string; scope: Scope }>(
		`/calendar/events/${enc(cal)}/${enc(id)}?${q}`,
	);
}

export type Preview = {
	occurrences: { start: string; end: string }[];
	text: { de?: string; en?: string } | null;
	total: number | null;
	more: boolean;
};
export function previewRepeat(body: {
	start: string;
	end?: string;
	timezone?: string;
	allDay?: boolean;
	repeat: Simple | null;
}) {
	return api.post<Preview>('/calendar/preview', body);
}

export function createCalendar(name: string, color: string) {
	return api.post<unknown>('/calendar/calendars', { name, color });
}
export function updateCalendar(id: string, change: { name?: string; color?: string }) {
	return api.put<unknown>(`/calendar/calendars/${enc(id)}`, change);
}
export function removeCalendar(id: string) {
	return api.delete(`/calendar/calendars/${enc(id)}`);
}
export function setHidden(hidden: string[]) {
	return api.put('/preferences/calendar', { hidden });
}

export type ImportResult = {
	added: number;
	updated: number;
	skipped: number;
	failed: { uid: string; reason: string }[];
};
export function importICS(id: string, file: File, onDuplicate: 'skip' | 'replace') {
	const form = new FormData();
	form.append('file', file);
	return api.post<ImportResult>(
		`/calendar/calendars/${enc(id)}/import?onDuplicate=${onDuplicate}`,
		form,
	);
}

/** Saves the calendar as .ics under the name titand gives. */
export async function exportICS(id: string): Promise<void> {
	let res: Response;
	try {
		res = await apiFetch(`/calendar/calendars/${enc(id)}/export`);
	} catch {
		throw new ApiError(0, 'unreachable', 'titand is not reachable');
	}
	if (!res.ok) throw await responseError(res);
	saveBlob(
		await res.blob(),
		dispositionName(res.headers.get('Content-Disposition')) || `${id}.ics`,
	);
}

// ---- Between the event window and the calendar ----

const CHANNEL = 'titanos-calendar';

/** Tells every calendar page of this browser that events changed. */
export function announceChange() {
	try {
		const ch = new BroadcastChannel(CHANNEL);
		ch.postMessage('changed');
		ch.close();
	} catch {
		// Without BroadcastChannel the calendar shows the change on its next load.
	}
}

/** Calls `fn` when another page changed events; returns the unsubscribe. */
export function onChange(fn: () => void): () => void {
	try {
		const ch = new BroadcastChannel(CHANNEL);
		ch.onmessage = () => fn();
		return () => ch.close();
	} catch {
		return () => {};
	}
}

/** Where "Neuer Termin" or "Bearbeiten" opens: a window of its own on the desktop, a page on the phone. */
export function eventHref(p: {
	cal?: string;
	id?: string;
	occurrence?: string;
	date?: string;
	time?: string;
	/** A first title of a new event (the name of a contact). */
	title?: string;
	back?: string;
}): string {
	const q = new URLSearchParams();
	for (const [k, v] of Object.entries(p)) if (v) q.set(k, v);
	const s = q.toString();
	return '/calendar/event' + (s ? '?' + s : '');
}

export function openEventWindow(href: string) {
	if (inWindow) {
		window.parent.postMessage(
			{ type: 'titanos-open-task', href } satisfies FrameMessage,
			location.origin,
		);
		return;
	}
	void goto(href);
}

/** The event window is done: closes it, or goes back on the phone. */
export function closeEventWindow(back: string) {
	if (inWindow) {
		window.parent.postMessage({ type: 'titanos-close' } satisfies FrameMessage, location.origin);
		return;
	}
	void goto(back, { replace: true });
}

/** A `back` of the URL: only a place of T-Kalender or Kontakte ("Termin" of a contact). */
export function safeBack(raw: string | null): string {
	const b = raw ?? '';
	return /^\/(calendar|contacts)(\?|$)/.test(b) ? b : '/calendar';
}
