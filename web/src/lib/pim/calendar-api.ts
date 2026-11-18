// 1.11 (step 15): the requests of T-Kalender (BAU-API "Schritt 14a"). Ids
// are file names chosen by devices: always encoded in the path. A change
// made in the event window tells the calendar page (another frame of the
// same origin) over a BroadcastChannel, so it reloads.
import { api, apiFetch, dispositionName, responseError, saveBlob, ApiError } from '../api.ts';
import { goto } from '$app/navigation';
import { inWindow, type FrameMessage } from '../desktop/frame.ts';
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

export async function loadCalendars(): Promise<Calendar[]> {
	const list = sanitizeCalendars(await api.get<unknown>('/calendar/calendars'));
	if (!list) throw bad();
	return list;
}

/** Events of [from, to) – dates in the NAS zone. */
export async function loadEvents(from: string, to: string) {
	const list = sanitizeEventList(
		await api.get<unknown>(`/calendar/events?from=${enc(from)}&to=${enc(to)}`),
	);
	if (!list) throw bad();
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

/** A `back` of the URL: only a place of T-Kalender. */
export function safeBack(raw: string | null): string {
	const b = raw ?? '';
	return /^\/calendar(\?|$)/.test(b) ? b : '/calendar';
}
