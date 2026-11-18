// 1.11 (step 15): what T-Kalender reads from titand (BAU-API "Schritt
// 14a"), checked, and the pure rules of its views and form: the colors of
// the calendars (the fixed --icon-square-* colors), the repetition presets
// of the form and their mapping to the API's simple rule, reminders in
// words, links in notes. No Svelte, no DOM (node:test in
// tests/calendar.test.mjs).
import {
	addDays,
	dayOfMonth,
	dayParts,
	longEvent,
	monthLength,
	weekday,
	type DayPart,
} from './caldates.ts';

export type Calendar = {
	id: string;
	name: string;
	color: string;
	order: number;
	readOnly: boolean;
	kind: 'own' | 'birthdays';
	count: number;
	hidden: boolean;
	unavailable: boolean;
};

export type Rule = {
	freq: 'daily' | 'weekly' | 'monthly' | 'yearly';
	interval: number;
	byDay?: string[];
	byMonthDay?: number[];
	until?: string;
	count?: number;
	custom?: boolean;
	text?: { de?: string; en?: string };
};

export type CalEvent = {
	kind: 'event' | 'birthday';
	id: string;
	occurrence: string;
	calendar: string;
	uid: string;
	title: string;
	start: string;
	end: string;
	allDay: boolean;
	timezone: string;
	floating: boolean;
	location: string;
	clipped: boolean;
	notes?: string;
	recurring: boolean;
	rule: Rule | null;
	alarms: number[];
	exception: boolean;
	etag: string;
	tzGuess: boolean;
	recurUnsupported: boolean;
	readOnly: boolean;
	occurrences?: number;
	birthday?: { name: string; year?: number; age?: number };
};

export const BIRTHDAYS = 'geburtstage';

const str = (v: unknown) => (typeof v === 'string' ? v : '');
const num = (v: unknown) => (typeof v === 'number' && Number.isFinite(v) ? v : 0);

/** GET /calendar/calendars, checked; null when it is no list. */
export function sanitizeCalendars(raw: unknown): Calendar[] | null {
	if (!Array.isArray(raw)) return null;
	return raw
		.filter((c): c is Record<string, unknown> => !!c && typeof c === 'object')
		.map((c) => ({
			id: str(c.id),
			name: str(c.name),
			color: /^#[0-9a-fA-F]{6}$/.test(str(c.color)) ? str(c.color).toLowerCase() : '#7c3aed',
			order: num(c.order),
			readOnly: c.readOnly === true,
			kind: (c.kind === 'birthdays' || c.id === BIRTHDAYS
				? 'birthdays'
				: 'own') as Calendar['kind'],
			count: Math.max(0, Math.floor(num(c.count))),
			hidden: c.hidden === true,
			unavailable: c.unavailable === true,
		}))
		.filter((c) => c.id !== '');
}

function sanitizeRule(raw: unknown): Rule | null {
	if (!raw || typeof raw !== 'object') return null;
	const r = raw as Record<string, unknown>;
	const freq = ['daily', 'weekly', 'monthly', 'yearly'].includes(str(r.freq))
		? (str(r.freq) as Rule['freq'])
		: 'daily';
	const out: Rule = { freq, interval: Math.max(1, Math.floor(num(r.interval)) || 1) };
	if (Array.isArray(r.byDay)) out.byDay = r.byDay.filter((d) => typeof d === 'string');
	if (Array.isArray(r.byMonthDay))
		out.byMonthDay = r.byMonthDay.filter((d) => typeof d === 'number');
	if (str(r.until)) out.until = str(r.until);
	if (num(r.count) > 0) out.count = Math.floor(num(r.count));
	if (r.custom === true || !['daily', 'weekly', 'monthly', 'yearly'].includes(str(r.freq)))
		out.custom = true;
	if (r.text && typeof r.text === 'object') {
		const t = r.text as Record<string, unknown>;
		out.text = { de: str(t.de), en: str(t.en) };
	}
	return out;
}

/** One View of the API (list, single event, birthday), checked; null when unusable. */
export function sanitizeEvent(raw: unknown): CalEvent | null {
	if (!raw || typeof raw !== 'object') return null;
	const e = raw as Record<string, unknown>;
	if (!str(e.id) || !str(e.start)) return null;
	const out: CalEvent = {
		kind: e.kind === 'birthday' ? 'birthday' : 'event',
		id: str(e.id),
		occurrence: str(e.occurrence),
		calendar: str(e.calendar),
		uid: str(e.uid),
		title: str(e.title),
		start: str(e.start),
		end: str(e.end),
		allDay: e.allDay === true,
		timezone: str(e.timezone),
		floating: e.floating === true,
		location: str(e.location),
		clipped: e.clipped === true,
		recurring: e.recurring === true,
		rule: sanitizeRule(e.rule),
		alarms: Array.isArray(e.alarms)
			? e.alarms.filter((m): m is number => typeof m === 'number' && m > -1440)
			: [],
		exception: e.exception === true,
		etag: str(e.etag),
		tzGuess: e.tzGuess === true,
		recurUnsupported: e.recurUnsupported === true,
		readOnly: e.readOnly === true || e.kind === 'birthday',
	};
	if (typeof e.notes === 'string') out.notes = e.notes;
	if (typeof e.occurrences === 'number' && e.occurrences > 0) out.occurrences = e.occurrences;
	if (e.birthday && typeof e.birthday === 'object') {
		const b = e.birthday as Record<string, unknown>;
		out.birthday = { name: str(b.name) };
		if (num(b.year) > 0) out.birthday.year = num(b.year);
		if (typeof b.age === 'number') out.birthday.age = b.age;
	}
	return out;
}

/** GET /calendar/events: {events, truncated, birthdaysUnavailable?}. */
export function sanitizeEventList(raw: unknown): {
	events: CalEvent[];
	truncated: boolean;
	birthdaysUnavailable: boolean;
} | null {
	if (!raw || typeof raw !== 'object' || !Array.isArray((raw as { events?: unknown }).events))
		return null;
	const r = raw as { events: unknown[]; truncated?: unknown; birthdaysUnavailable?: unknown };
	return {
		events: r.events.map(sanitizeEvent).filter((e): e is CalEvent => e !== null),
		truncated: r.truncated === true,
		birthdaysUnavailable: r.birthdaysUnavailable === true,
	};
}

/** The key of an occurrence (keyed lists, the opened detail). */
export function eventKey(e: Pick<CalEvent, 'calendar' | 'id' | 'occurrence'>): string {
	return `${e.calendar}\u0000${e.id}\u0000${e.occurrence}`;
}

// ---- Colors ----

/** The fixed colors of the program squares (app.css), the same in every theme. */
export const SQUARES = [
	['blue', '#2563eb'],
	['violet', '#7c3aed'],
	['teal', '#0f766e'],
	['cyan', '#0e7490'],
	['green', '#15803d'],
	['orange', '#c2410c'],
	['amber', '#b45309'],
	['red', '#dc2626'],
	['pink', '#be185d'],
	['slate', '#475569'],
] as const;

export type SquareName = (typeof SQUARES)[number][0];

const rgb = (hex: string) => [1, 3, 5].map((i) => parseInt(hex.slice(i, i + 2), 16));

/** The square closest to a calendar's color (a device may have chosen any color). */
export function squareOf(hex: string): SquareName {
	if (!/^#[0-9a-fA-F]{6}$/.test(hex)) return 'violet';
	const [r, g, b] = rgb(hex.toLowerCase());
	let best: SquareName = 'violet';
	let dist = Infinity;
	for (const [name, value] of SQUARES) {
		const [r2, g2, b2] = rgb(value);
		const d = (r - r2) ** 2 + (g - g2) ** 2 + (b - b2) ** 2;
		if (d < dist) {
			dist = d;
			best = name;
		}
	}
	return best;
}

/** The CSS color of a calendar: always one of the theme's square variables. */
export function calColor(hex: string): string {
	return `var(--icon-square-${squareOf(hex)})`;
}

export function squareHex(name: SquareName): string {
	return SQUARES.find(([n]) => n === name)?.[1] ?? '#7c3aed';
}

// ---- Repetition ----

export type RepeatKind =
	| 'none'
	| 'daily'
	| 'workdays'
	| 'weekly'
	| 'monthlyDay'
	| 'monthlyNth'
	| 'monthlyLast'
	| 'yearly'
	| 'custom';
export type RepeatEnd = 'never' | 'date' | 'count';

/** The form's repetition: a preset of the start date, every `interval`, and how it ends. */
export type Repeat = {
	kind: RepeatKind;
	interval: number;
	end: RepeatEnd;
	until: string;
	count: number;
};

/** The simple rule the API takes (recur.Simple). */
export type Simple = {
	freq: Rule['freq'];
	interval: number;
	byDay?: string[];
	byMonthDay?: number[];
	until?: string;
	count?: number;
	custom?: boolean;
};

export const DAY_CODES = ['MO', 'TU', 'WE', 'TH', 'FR', 'SA', 'SU'];
const WORKDAYS = ['MO', 'TU', 'WE', 'TH', 'FR'];

/** The week of the month of a date: 1 … 5. */
export function nthOfMonth(day: string): number {
	return Math.ceil(dayOfMonth(day) / 7);
}

/** The date falls in the last seven days of its month ("am letzten Dienstag"). */
export function lastWeekOfMonth(day: string): boolean {
	return dayOfMonth(day) + 7 > monthLength(day);
}

/** The presets the form offers for a start date. */
export function repeatKinds(start: string): RepeatKind[] {
	const kinds: RepeatKind[] = ['none', 'daily', 'workdays', 'weekly', 'monthlyDay'];
	if (nthOfMonth(start) <= 4) kinds.push('monthlyNth');
	if (lastWeekOfMonth(start)) kinds.push('monthlyLast');
	kinds.push('yearly');
	return kinds;
}

/** The form's repetition as the API's `repeat` (null = none; {custom:true} keeps the rule). */
export function toSimple(r: Repeat, start: string): Simple | null {
	const interval = Math.max(1, Math.floor(r.interval) || 1);
	const code = DAY_CODES[weekday(start)];
	let out: Simple;
	switch (r.kind) {
		case 'none':
			return null;
		case 'custom':
			return { freq: 'daily', interval: 1, custom: true };
		case 'daily':
			out = { freq: 'daily', interval };
			break;
		case 'workdays':
			out = { freq: 'weekly', interval: 1, byDay: [...WORKDAYS] };
			break;
		case 'weekly':
			out = { freq: 'weekly', interval, byDay: [code] };
			break;
		case 'monthlyDay':
			out = { freq: 'monthly', interval, byMonthDay: [dayOfMonth(start)] };
			break;
		case 'monthlyNth':
			out = { freq: 'monthly', interval, byDay: [`${Math.min(nthOfMonth(start), 4)}${code}`] };
			break;
		case 'monthlyLast':
			out = { freq: 'monthly', interval, byDay: [`-1${code}`] };
			break;
		case 'yearly':
			out = { freq: 'yearly', interval };
			break;
	}
	if (r.end === 'date' && r.until) out.until = r.until;
	if (r.end === 'count' && r.count > 0) out.count = Math.floor(r.count);
	return out;
}

const same = (a: string[] | undefined, b: string[]) =>
	(a ?? []).length === b.length && b.every((x) => (a ?? []).includes(x));

/** The form's repetition for a rule of the API; a rule that is no preset of the start stays 'custom'. */
export function fromRule(rule: Rule | null, recurring: boolean, start: string): Repeat {
	const base: Repeat = { kind: 'none', interval: 1, end: 'never', until: '', count: 10 };
	if (!rule) return recurring ? { ...base, kind: 'custom' } : base;
	const out: Repeat = {
		...base,
		interval: rule.interval || 1,
		end: rule.until ? 'date' : rule.count ? 'count' : 'never',
		until: rule.until ?? '',
		count: rule.count ?? 10,
	};
	const code = DAY_CODES[weekday(start)];
	const byDay = rule.byDay ?? [];
	const byMonthDay = rule.byMonthDay ?? [];
	let kind: RepeatKind = 'custom';
	if (!rule.custom) {
		if (rule.freq === 'daily' && !byDay.length && !byMonthDay.length) kind = 'daily';
		else if (rule.freq === 'weekly' && !byMonthDay.length) {
			if (same(byDay, WORKDAYS) && out.interval === 1 && WORKDAYS.includes(code)) kind = 'workdays';
			else if (!byDay.length || same(byDay, [code])) kind = 'weekly';
		} else if (rule.freq === 'monthly') {
			if (
				!byDay.length &&
				(!byMonthDay.length || same(byMonthDay.map(String), [String(dayOfMonth(start))]))
			)
				kind = 'monthlyDay';
			else if (!byMonthDay.length && byDay.length === 1) {
				const m = /^(-?\d)([A-Z]{2})$/.exec(byDay[0]);
				if (m && m[2] === code) {
					if (m[1] === '-1' && lastWeekOfMonth(start)) kind = 'monthlyLast';
					else if (Number(m[1]) === nthOfMonth(start) && nthOfMonth(start) <= 4)
						kind = 'monthlyNth';
				}
			}
		} else if (rule.freq === 'yearly' && !byDay.length && !byMonthDay.length) kind = 'yearly';
	}
	out.kind = kind;
	return out;
}

/** The interval unit of a preset ('' = none shown). */
export function intervalUnit(kind: RepeatKind): 'day' | 'week' | 'month' | 'year' | '' {
	if (kind === 'daily') return 'day';
	if (kind === 'weekly') return 'week';
	if (kind === 'monthlyDay' || kind === 'monthlyNth' || kind === 'monthlyLast') return 'month';
	if (kind === 'yearly') return 'year';
	return '';
}

// ---- Reminders ----

/** Reminders the form offers: minutes before the start. */
export const TIMED_ALARMS = [0, 5, 10, 15, 30, 60, 120, 1440, 2880, 10080];
/**
 * All-day: on the day at 09:00 (1.13, after the start: −540), the day
 * before at 09:00 / 18:00, two days before 09:00, a week before 09:00.
 */
export const ALLDAY_ALARMS = [-540, 900, 360, 2340, 9540];

/** A reminder in parts for the words: relative ("1 Stunde vorher") or at a time of a day before (all-day). */
export type AlarmWords =
	| { kind: 'start' }
	| { kind: 'minutes' | 'hours' | 'days' | 'weeks'; n: number }
	| { kind: 'mixed'; hours: number; minutes: number }
	| { kind: 'dayAt'; days: number; time: string };

/**
 * `start`: minutes after midnight of a timed event's start; a reminder of a
 * day or more that is no whole number of days is then a time of a day
 * before ("Am Vortag um 18:00" for 25 hours before 19:00).
 */
export function alarmWords(minutes: number, allDay: boolean, start?: number): AlarmWords {
	// After the start only for all-day events (on the day at a time).
	const m = allDay ? Math.max(-1439, Math.floor(minutes)) : Math.max(0, Math.floor(minutes));
	const timedDayAt = !allDay && start !== undefined && m >= 1440 && m % 1440 !== 0;
	if (allDay || timedDayAt) {
		const from = allDay ? 0 : (start as number);
		const days = Math.ceil((m - from) / 1440);
		const at = from + days * 1440 - m;
		const time = `${String(Math.floor(at / 60)).padStart(2, '0')}:${String(at % 60).padStart(2, '0')}`;
		return { kind: 'dayAt', days, time };
	}
	if (m === 0) return { kind: 'start' };
	if (m % 10080 === 0) return { kind: 'weeks', n: m / 10080 };
	if (m % 1440 === 0) return { kind: 'days', n: m / 1440 };
	if (m % 60 === 0) return { kind: 'hours', n: m / 60 };
	if (m < 60) return { kind: 'minutes', n: m };
	return { kind: 'mixed', hours: Math.floor(m / 60), minutes: m % 60 };
}

// ---- Links in notes ----

export type TextPiece = { text: string; href?: string };

/**
 * A note as text and links: only http(s), mailto and tel become links, and
 * the link's text is its real target. Everything else stays text.
 */
export function linkPieces(text: string): TextPiece[] {
	const out: TextPiece[] = [];
	const re = /\b(?:https?:\/\/[^\s<>"']+|mailto:[^\s<>"']+|tel:\+?[\d][\d\-/().]*\d)/gi;
	let last = 0;
	for (const m of text.matchAll(re)) {
		let link = m[0];
		// Punctuation that ends a sentence is no part of the address.
		const trail = /[.,;:!?)\]]+$/.exec(link);
		if (trail && !(trail[0].startsWith(')') && link.includes('(')))
			link = link.slice(0, -trail[0].length);
		const at = m.index ?? 0;
		if (at > last) out.push({ text: text.slice(last, at) });
		out.push({ text: link, href: link });
		last = at + link.length;
	}
	if (last < text.length) out.push({ text: text.slice(last) });
	return out;
}

/** An address of the event's URL field or a note may only be one of these. */
export function safeHref(href: string): boolean {
	return /^(https?:\/\/|mailto:|tel:)/i.test(href);
}

// ---- Event times for the API ----

/** The `event` of POST/PUT from the form's fields. */
export type FormTimes = {
	allDay: boolean;
	startDate: string;
	startTime: string;
	endDate: string;
	endTime: string;
};

/** start/end for the API: wall clock 'YYYY-MM-DDTHH:MM' or dates with the end exclusive. */
export function apiTimes(f: FormTimes): { start: string; end: string; allDay: boolean } {
	if (f.allDay) {
		const end = f.endDate && f.endDate >= f.startDate ? f.endDate : f.startDate;
		return { start: f.startDate, end: addDays(end, 1), allDay: true };
	}
	return {
		start: `${f.startDate}T${f.startTime}`,
		end: `${f.endDate}T${f.endTime}`,
		allDay: false,
	};
}

/** Query of a list window: [from, to) as dates (to = the day after the last). */
export function windowQuery(from: string, lastDay: string): string {
	return `from=${encodeURIComponent(from)}&to=${encodeURIComponent(addDays(lastDay, 1))}`;
}

// ---- Events per day ----

/** One event on one day of a view. */
export type DayItem = { ev: CalEvent; part: DayPart; key: string; long: boolean };

/**
 * The events of a list per day in a zone: all-day and multi-day ones first,
 * then by time, then by title. Days outside [from, to] are left out.
 */
export function eventsByDay(
	events: CalEvent[],
	zone: string,
	from: string,
	to: string,
): Map<string, DayItem[]> {
	const out = new Map<string, DayItem[]>();
	for (const ev of events) {
		const long = longEvent(ev, zone);
		for (const part of dayParts(ev, zone)) {
			if (part.date < from || part.date > to) continue;
			const list = out.get(part.date) ?? [];
			list.push({ ev, part, key: eventKey(ev) + '\u0000' + part.date, long });
			out.set(part.date, list);
		}
	}
	for (const list of out.values())
		list.sort(
			(a, b) =>
				Number(b.part.allDay) - Number(a.part.allDay) ||
				Number(b.long) - Number(a.long) ||
				a.part.from - b.part.from ||
				a.ev.title.localeCompare(b.ev.title),
		);
	return out;
}
