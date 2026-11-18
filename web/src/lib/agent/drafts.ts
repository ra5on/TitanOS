// 1.13: drafts of the KI-Agent ("Nur Vorschläge") on their way into the
// programs. A card "Öffnen und bearbeiten" opens the program with
// ?agentDraft=<chat>:<entry>; the program fetches the draft from the
// account's own chat (GET /agent/chats/{chat}/drafts/{entry}) and puts the
// fields into its editor as text the person checks – nothing is saved or
// sent without the person. No runes and no navigation here (node --test).
//
// Fields per program (backend: api/agent_mail.go, agent_pim_more.go,
// agent_tasks.go):
// - mail (new, reply): a, to[], cc[], bcc[], subject, text; a reply also
//   f, uid (the mail it answers). move: a, f, uids[], to (a folder).
// - calendar (event): title, start, end, allDay, timezone, location, notes,
//   calendar, alarms (minutes before), rrule (the repetition in the form of
//   the web API: {freq, interval, byDay, byMonthDay, until, count}); a
//   change also cal, id, occurrence and only the fields that change.
// - contacts (contact): name, first, last, emails[{type,value}],
//   phones[{type,value}], org, addresses[{type,street,city,postcode,region,
//   country}], notes, birthday; a change also book, id.
// - tasks (task, complete): title, notes, due, priority, list; a change
//   also id (and done).

import { api } from '../api.ts';
import type { AgentDraft } from '../agent.ts';
import {
	addDays,
	clock,
	daysBetween,
	inclusiveEnd,
	validDate,
	wallMinutes,
} from '../pim/caldates.ts';
import { fromRule, type Repeat, type Simple } from '../pim/calendar.ts';

export type { AgentDraft };

const ID = /^[A-Za-z0-9_-]{1,64}$/;

/** Reads "<chat>:<entry>"; null for anything else. */
export function parseDraftParam(
	param: string | null | undefined,
): { chat: string; entry: string } | null {
	if (!param) return null;
	const at = param.indexOf(':');
	if (at < 1) return null;
	const chat = param.slice(0, at);
	const entry = param.slice(at + 1);
	return ID.test(chat) && ID.test(entry) ? { chat, entry } : null;
}

/**
 * The draft a program was opened with (the value of ?agentDraft=), or null
 * when there is none or it is gone. Only the account's own chats answer.
 */
export async function loadAgentDraft(param: string | null | undefined): Promise<AgentDraft | null> {
	const ref = parseDraftParam(param);
	if (!ref) return null;
	try {
		const d = await api.get<AgentDraft>(
			`/agent/chats/${encodeURIComponent(ref.chat)}/drafts/${encodeURIComponent(ref.entry)}`,
		);
		return d && typeof d === 'object' && d.fields && typeof d.fields === 'object' ? d : null;
	} catch {
		return null;
	}
}

/** Where a draft opens: the editor of its program with ?agentDraft=. */
export function draftHref(
	draft: Pick<AgentDraft, 'app' | 'kind' | 'fields'>,
	chat: string,
	entry: string,
): string {
	const ref = encodeURIComponent(`${chat}:${entry}`);
	switch (draft.app) {
		case 'mail': {
			if (draft.kind === 'move') {
				const f = draft.fields;
				const uids = Array.isArray(f.uids) ? f.uids : [];
				const q = new URLSearchParams();
				if (typeof f.a === 'string') q.set('a', f.a);
				if (typeof f.f === 'string') q.set('f', f.f);
				if (typeof uids[0] === 'number') q.set('m', String(uids[0]));
				return '/mail?' + q.toString();
			}
			return `/mail/compose?agentDraft=${ref}`;
		}
		case 'calendar':
			return `/calendar/event?agentDraft=${ref}`;
		case 'contacts':
			return `/contacts?agentDraft=${ref}`;
		case 'tasks':
			return `/calendar?agentDraft=${ref}`;
	}
	return '/agent';
}

/** Which steps of a waiting approval can be opened as a draft instead. */
export const DRAFT_TOOLS = new Set([
	'mail_send',
	'mail_draft',
	'mail_move',
	'calendar_create',
	'calendar_update',
	'contacts_create',
	'contacts_update',
	'tasks_create',
	'tasks_update',
	'tasks_complete',
]);

/** The program a tool's draft opens in, for the href of a waiting step. */
export function draftAppOf(tool: string): AgentDraft['app'] {
	if (tool.startsWith('mail_')) return 'mail';
	if (tool.startsWith('contacts_')) return 'contacts';
	if (tool.startsWith('tasks_')) return 'tasks';
	return 'calendar';
}

/** Recipients of a draft as one line each (to, cc, bcc are lists). */
export function draftList(v: unknown): string[] {
	return Array.isArray(v)
		? v.filter((x): x is string => typeof x === 'string' && x.trim() !== '')
		: [];
}

type DraftRow = { type: string; value: string };
type DraftAddress = {
	type: string;
	street: string;
	postcode: string;
	city: string;
	region: string;
	country: string;
};
/** The parts of the form of Kontakte a draft fills (lib/pim/contacts.ts Form). */
type ContactFormLike = {
	first: string;
	last: string;
	org: string;
	phones: DraftRow[];
	emails: DraftRow[];
	addresses: DraftAddress[];
	birthdayDate: string;
	birthdayNoYear: boolean;
	notes: string;
};

const str = (v: unknown) => (typeof v === 'string' ? v : '');

/**
 * The form of Kontakte with a contact draft put in: the fields the draft
 * names replace those of the form (a change names only what changes);
 * lists replace the whole list, as contacts_update does.
 */
export function contactDraftForm<F extends ContactFormLike>(
	form: F,
	fields: Record<string, unknown>,
): F {
	const out = { ...form };
	if ('first' in fields) out.first = str(fields.first);
	if ('last' in fields) out.last = str(fields.last);
	if (!('first' in fields) && !('last' in fields) && str(fields.name)) {
		const parts = str(fields.name).trim().split(/\s+/);
		out.last = parts.length > 1 ? (parts.pop() ?? '') : '';
		out.first = parts.join(' ');
	}
	if ('org' in fields) out.org = str(fields.org);
	if ('notes' in fields) out.notes = str(fields.notes);
	const rows = (v: unknown, fallback: string): DraftRow[] =>
		Array.isArray(v)
			? v
					.filter((r): r is Record<string, unknown> => !!r && typeof r === 'object')
					.map((r) => ({ type: str(r.type) || fallback, value: str(r.value) }))
					.filter((r) => r.value.trim() !== '')
			: [];
	if ('phones' in fields) out.phones = rows(fields.phones, 'mobile');
	if ('emails' in fields) out.emails = rows(fields.emails, 'home');
	if ('addresses' in fields && Array.isArray(fields.addresses))
		out.addresses = fields.addresses
			.filter((r): r is Record<string, unknown> => !!r && typeof r === 'object')
			.map((r) => ({
				type: str(r.type) || 'home',
				street: str(r.street),
				postcode: str(r.postcode),
				city: str(r.city),
				region: str(r.region),
				country: str(r.country),
			}));
	if ('birthday' in fields) {
		const b = str(fields.birthday);
		const noYear = /^--\d{2}-\d{2}$/.test(b);
		out.birthdayNoYear = noYear;
		out.birthdayDate = noYear ? '2000' + b.slice(1) : /^\d{4}-\d{2}-\d{2}$/.test(b) ? b : '';
	}
	return out;
}

// ---------- Kalender: events and reminders ----------

/** The parts of the form of the event window a draft fills
 *  (routes/calendar/event/+page.svelte Form). */
type EventFormLike = {
	title: string;
	calendar: string;
	allDay: boolean;
	startDate: string;
	startTime: string;
	endDate: string;
	endTime: string;
	repeat: Repeat;
	alarms: number[];
	location: string;
	notes: string;
};

export type EventDraftResult<F> = {
	form: F;
	/** A repetition the form shows only as "custom": sent as it is. */
	rule: Simple | null;
	/** The zone a new event is meant in ('' = the NAS zone). */
	zone: string;
	/** Which events of a series the change means ('' = ask as always). */
	scope: '' | 'this' | 'following' | 'all';
};

const WHEN = /^(\d{4}-\d{2}-\d{2})(?:T(\d{2}):(\d{2}))?/;
const ZONE = /^(UTC|[A-Za-z]+(?:\/[A-Za-z0-9_+-]+){1,2})$/;
const FREQS = new Set(['daily', 'weekly', 'monthly', 'yearly']);
const line = (v: unknown, max: number) => str(v).replace(/\s+/g, ' ').trim().slice(0, max);

function when(v: unknown): { date: string; time: string } | null {
	const m = WHEN.exec(str(v).trim());
	if (!m || !validDate(m[1])) return null;
	if (m[2] === undefined) return { date: m[1], time: '' };
	if (Number(m[2]) > 23 || Number(m[3]) > 59) return null;
	return { date: m[1], time: `${m[2]}:${m[3]}` };
}

/** The repetition of a draft (recur.Simple of the web API), checked. */
function simpleOf(v: Record<string, unknown>): Simple | null {
	if (!FREQS.has(str(v.freq))) return null;
	const out: Simple = {
		freq: str(v.freq) as Simple['freq'],
		interval:
			typeof v.interval === 'number' && Number.isInteger(v.interval) && v.interval > 0
				? Math.min(v.interval, 999)
				: 1,
	};
	if (Array.isArray(v.byDay)) {
		const days = v.byDay.filter(
			(d): d is string => typeof d === 'string' && /^(-?[1-5])?(MO|TU|WE|TH|FR|SA|SU)$/.test(d),
		);
		if (days.length) out.byDay = days.slice(0, 7 * 5);
	}
	if (Array.isArray(v.byMonthDay)) {
		const days = v.byMonthDay.filter(
			(d): d is number =>
				typeof d === 'number' && Number.isInteger(d) && d !== 0 && d >= -31 && d <= 31,
		);
		if (days.length) out.byMonthDay = days.slice(0, 31);
	}
	if (validDate(str(v.until))) out.until = str(v.until);
	else if (typeof v.count === 'number' && Number.isInteger(v.count) && v.count > 0)
		out.count = Math.min(v.count, 999);
	return out;
}

/**
 * The form of the event window with an event draft put in (calendar_create:
 * every field; calendar_update: only what changes). Fields that do not fit
 * (a day that is none, a calendar that is not one of `calendars`, the
 * person's own and writable) are left as the form has them. A start
 * without an end keeps the event's length (or takes `duration`); an
 * all-day end is the day after the last day, as the API has it.
 */
export function eventDraftForm<F extends EventFormLike>(
	form: F,
	fields: Record<string, unknown>,
	calendars: string[],
): EventDraftResult<F> {
	const out = { ...form, repeat: { ...form.repeat }, alarms: [...form.alarms] };
	let rule: Simple | null = null;
	if ('title' in fields) out.title = line(fields.title, 2000);
	if ('location' in fields) out.location = line(fields.location, 2000);
	if ('notes' in fields) out.notes = str(fields.notes).replace(/\r\n?/g, '\n').slice(0, 10000);
	if (typeof fields.calendar === 'string' && calendars.includes(fields.calendar))
		out.calendar = fields.calendar;

	// The length before: kept when only the start moves.
	const spanDays = Math.max(0, daysBetween(form.startDate, form.endDate));
	const spanMinutes = Math.max(
		0,
		wallMinutes(form.startDate, form.startTime, form.endDate, form.endTime),
	);
	const start = when(fields.start);
	const end = when(fields.end);
	if (typeof fields.allDay === 'boolean') out.allDay = fields.allDay;
	else if (start) out.allDay = start.time === '';
	if (start) {
		out.startDate = start.date;
		if (start.time) out.startTime = start.time;
	}
	if (end && (end.date >= out.startDate || !out.allDay)) {
		if (out.allDay) out.endDate = inclusiveEnd(out.startDate, end.date);
		else {
			out.endDate = end.date;
			if (end.time) out.endTime = end.time;
		}
	} else if (start || 'allDay' in fields) {
		const d = Number(fields.duration);
		const minutes =
			Number.isInteger(d) && d > 0 && d <= 10080 ? d : out.allDay ? 0 : spanMinutes || 60;
		if (out.allDay) out.endDate = addDays(out.startDate, spanDays);
		else {
			const total = Number(out.startTime.slice(0, 2)) * 60 + Number(out.startTime.slice(3, 5));
			out.endDate = addDays(out.startDate, Math.floor((total + minutes) / 1440));
			out.endTime = clock((total + minutes) % 1440);
		}
	}
	// An end before the start (a wrong end in the draft): an hour after it.
	if (!out.allDay && wallMinutes(out.startDate, out.startTime, out.endDate, out.endTime) < 0) {
		const total = Number(out.startTime.slice(0, 2)) * 60 + Number(out.startTime.slice(3, 5)) + 60;
		out.endDate = addDays(out.startDate, Math.floor(total / 1440));
		out.endTime = clock(total % 1440);
	}
	if (out.allDay && out.endDate < out.startDate) out.endDate = out.startDate;

	if (Array.isArray(fields.alarms))
		out.alarms = [
			...new Set(
				fields.alarms.filter(
					(m): m is number =>
						typeof m === 'number' && Number.isInteger(m) && m >= -1440 && m <= 40320,
				),
			),
		]
			.sort((a, b) => a - b)
			.slice(0, 20);

	if ('rrule' in fields) {
		const r = fields.rrule;
		const simple = r && typeof r === 'object' ? simpleOf(r as Record<string, unknown>) : null;
		if (!simple) out.repeat = { kind: 'none', interval: 1, end: 'never', until: '', count: 10 };
		else {
			out.repeat = fromRule(simple, true, out.startDate);
			if (out.repeat.kind === 'custom') rule = simple;
		}
	}
	const zone =
		typeof fields.timezone === 'string' && ZONE.test(fields.timezone) ? fields.timezone : '';
	const s = str(fields.scope);
	const scope = s === 'this' || s === 'following' || s === 'all' ? s : '';
	return { form: out, rule, zone, scope };
}

/** The parts of the form of the reminder dialog a draft fills
 *  (lib/pim/cal/TaskDialog.svelte Form). */
type TaskFormLike = {
	title: string;
	list: string;
	hasDue: boolean;
	date: string;
	hasTime: boolean;
	time: string;
	priority: number;
	notes: string;
};

/**
 * The form of the reminder dialog with a draft of tasks_create or
 * tasks_update put in: title, notes, due (a day, a day with a time, null
 * removes it), priority (0–9) and the list (only one of `lists`).
 */
export function taskDraftForm<F extends TaskFormLike>(
	form: F,
	fields: Record<string, unknown>,
	lists: string[],
): F {
	const out = { ...form };
	if ('title' in fields) out.title = line(fields.title, 2000);
	if ('notes' in fields) out.notes = str(fields.notes).replace(/\r\n?/g, '\n').slice(0, 10000);
	if (fields.due === null) out.hasDue = false;
	else if ('due' in fields) {
		const d = when(fields.due);
		if (d) {
			out.hasDue = true;
			out.date = d.date;
			out.hasTime = d.time !== '';
			if (d.time) out.time = d.time;
		}
	}
	const p = fields.priority;
	if (typeof p === 'number' && Number.isInteger(p) && p >= 0 && p <= 9) out.priority = p;
	if (typeof fields.list === 'string' && lists.includes(fields.list)) out.list = fields.list;
	return out;
}
