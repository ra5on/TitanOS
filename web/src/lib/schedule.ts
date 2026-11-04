// A simple schedule that nobody needs cron for (owner request of 4 Nov
// 2026): every N minutes, every hour, daily, on chosen weekdays or monthly.
// Stored as a five-field cron expression; "L" in the day of the month is
// the last day (titand, package scripts). Expressions that do not fit the
// simple choice stay as they are and are shown under "Advanced".

export type SimpleSchedule =
	| { kind: 'minutes'; every: number }
	| { kind: 'hourly'; minute: number }
	| { kind: 'daily'; time: string }
	| { kind: 'weekdays'; days: number[]; time: string }
	| { kind: 'monthly'; day: number | 'last'; time: string };

export type ScheduleKind = SimpleSchedule['kind'];

export const MINUTE_STEPS = [5, 10, 15, 30];
/** Monday first, as people read a week here; 0 is Sunday. */
export const WEEK = [1, 2, 3, 4, 5, 6, 0];

const pad = (n: number) => String(n).padStart(2, '0');

function clock(time: string): [number, number] | null {
	const m = /^(\d{1,2}):(\d{2})$/.exec(time);
	if (!m) return null;
	const h = Number(m[1]),
		mi = Number(m[2]);
	return h < 24 && mi < 60 ? [h, mi] : null;
}

/** The cron expression of a simple schedule, or null when it is incomplete. */
export function toCron(s: SimpleSchedule): string | null {
	switch (s.kind) {
		case 'minutes':
			return MINUTE_STEPS.includes(s.every) ? `*/${s.every} * * * *` : null;
		case 'hourly':
			return Number.isInteger(s.minute) && s.minute >= 0 && s.minute < 60
				? `${s.minute} * * * *`
				: null;
	}
	const at = clock(s.time);
	if (!at) return null;
	const [h, m] = at;
	switch (s.kind) {
		case 'daily':
			return `${m} ${h} * * *`;
		case 'weekdays': {
			const days = WEEK.filter((d) => s.days.includes(d));
			if (days.length === 0) return null;
			if (days.length === 7) return `${m} ${h} * * *`;
			return `${m} ${h} * * ${[...days].sort((a, b) => a - b).join(',')}`;
		}
		case 'monthly':
			if (s.day !== 'last' && !(Number.isInteger(s.day) && s.day >= 1 && s.day <= 28)) return null;
			return `${m} ${h} ${s.day === 'last' ? 'L' : s.day} * *`;
	}
}

const num = (v: string, low: number, high: number) =>
	/^\d{1,2}$/.test(v) && Number(v) >= low && Number(v) <= high ? Number(v) : null;

/** Weekdays of a list such as "1,4" or "1-5" (7 = Sunday), or null. */
function weekdays(field: string): number[] | null {
	const days = new Set<number>();
	for (const part of field.split(',')) {
		const range = /^(\d)-(\d)$/.exec(part);
		if (range) {
			const a = Number(range[1]),
				b = Number(range[2]);
			if (a > b || b > 7) return null;
			for (let d = a; d <= b; d++) days.add(d % 7);
		} else {
			const d = num(part, 0, 7);
			if (d === null) return null;
			days.add(d % 7);
		}
	}
	return WEEK.filter((d) => days.has(d));
}

/** The simple schedule a cron expression stands for, or null if it has none. */
export function fromCron(expr: string): SimpleSchedule | null {
	const f = expr.trim().split(/\s+/);
	if (f.length !== 5) return null;
	const [mi, h, dom, month, dow] = f;
	if (month !== '*') return null;
	const step = /^\*\/(\d+)$/.exec(mi);
	if (step && h === '*' && dom === '*' && dow === '*') {
		const every = Number(step[1]);
		return MINUTE_STEPS.includes(every) ? { kind: 'minutes', every } : null;
	}
	const minute = num(mi, 0, 59);
	if (minute === null) return null;
	if (h === '*') return dom === '*' && dow === '*' ? { kind: 'hourly', minute } : null;
	const hour = num(h, 0, 23);
	if (hour === null) return null;
	const time = `${pad(hour)}:${pad(minute)}`;
	if (dom === '*' && dow === '*') return { kind: 'daily', time };
	if (dom === '*') {
		const days = weekdays(dow);
		if (!days?.length) return null;
		return days.length === 7 ? { kind: 'daily', time } : { kind: 'weekdays', days, time };
	}
	if (dow !== '*') return null;
	if (dom === 'L') return { kind: 'monthly', day: 'last', time };
	const day = num(dom, 1, 28);
	return day === null ? null : { kind: 'monthly', day, time };
}

/** The default of a kind, keeping the time of the one before where it has one. */
export function simpleOf(kind: ScheduleKind, before?: SimpleSchedule | null): SimpleSchedule {
	const time = before && 'time' in before ? before.time : '03:00';
	switch (kind) {
		case 'minutes':
			return { kind, every: 15 };
		case 'hourly':
			return { kind, minute: 0 };
		case 'daily':
			return { kind, time };
		case 'weekdays':
			return { kind, days: before?.kind === 'weekdays' ? before.days : [1], time };
		case 'monthly':
			return { kind, day: 1, time };
	}
}

// ---- Next runs ---------------------------------------------------------

type Cron = {
	minute: Set<number>;
	hour: Set<number>;
	dom: Set<number>;
	lastDay: boolean;
	month: Set<number>;
	dow: Set<number>;
	domAny: boolean;
	dowAny: boolean;
};

function field(text: string, low: number, high: number): Set<number> | null {
	const out = new Set<number>();
	for (let part of text.split(',')) {
		let step = 1;
		const slash = part.indexOf('/');
		if (slash >= 0) {
			step = Number(part.slice(slash + 1));
			part = part.slice(0, slash);
			if (!/^\d+$/.test(String(step)) || step < 1 || step > high) return null;
		}
		let from = low,
			to = high;
		if (part !== '*') {
			const range = /^(\d+)-(\d+)$/.exec(part);
			if (range) {
				from = Number(range[1]);
				to = Number(range[2]);
				if (from > to) return null;
			} else if (/^\d+$/.test(part)) {
				from = Number(part);
				to = slash >= 0 ? high : from;
			} else return null;
		}
		if (from < low || to > high) return null;
		for (let v = from; v <= to; v += step) out.add(v);
	}
	return out;
}

/** Parses a cron expression as titand does; null when titand refuses it. */
export function parseCron(expr: string): Cron | null {
	const f = expr.trim().split(/\s+/);
	if (f.length !== 5) return null;
	let days = f[2].split(',');
	const lastDay = days.includes('L');
	days = days.filter((d) => d !== 'L');
	const minute = field(f[0], 0, 59),
		hour = field(f[1], 0, 23),
		dom = days.length ? field(days.join(','), 1, 31) : new Set<number>(),
		month = field(f[3], 1, 12),
		dow = field(f[4], 0, 7);
	if (!minute || !hour || !dom || !month || !dow) return null;
	if (dow.has(7)) dow.add(0);
	return { minute, hour, dom, lastDay, month, dow, domAny: f[2] === '*', dowAny: f[4] === '*' };
}

/** Wall-clock fields of an instant in a time zone. */
const formats = new Map<string, Intl.DateTimeFormat>();
function wall(at: number, timeZone: string) {
	let format = formats.get(timeZone);
	if (!format) {
		format = new Intl.DateTimeFormat('en-US', {
			timeZone,
			hourCycle: 'h23',
			year: 'numeric',
			month: 'numeric',
			day: 'numeric',
			hour: 'numeric',
			minute: 'numeric',
		});
		formats.set(timeZone, format);
	}
	const parts = format.formatToParts(new Date(at));
	const get = (type: string) => Number(parts.find((p) => p.type === type)?.value);
	return { y: get('year'), mo: get('month'), d: get('day'), h: get('hour'), mi: get('minute') };
}

/** The instant of a wall-clock time in a zone; null when it does not exist (DST gap). */
function instant(y: number, mo: number, d: number, h: number, mi: number, timeZone: string) {
	const guess = Date.UTC(y, mo - 1, d, h, mi);
	for (const probe of [guess, guess - 12 * 3600_000, guess + 12 * 3600_000]) {
		const w = wall(probe, timeZone);
		const offset = Date.UTC(w.y, w.mo - 1, w.d, w.h, w.mi) - probe;
		const at = guess - offset;
		const back = wall(at, timeZone);
		if (back.y === y && back.mo === mo && back.d === d && back.h === h && back.mi === mi) return at;
	}
	return null;
}

function dayMatches(c: Cron, y: number, mo: number, d: number): boolean {
	if (!c.month.has(mo)) return false;
	const date = new Date(Date.UTC(y, mo - 1, d));
	const last = new Date(Date.UTC(y, mo, 0)).getUTCDate() === d;
	const dom = c.dom.has(d) || (c.lastDay && last);
	const dow = c.dow.has(date.getUTCDay());
	if (c.domAny && c.dowAny) return true;
	if (c.domAny) return dow;
	if (c.dowAny) return dom;
	return dom || dow;
}

/** The next runs after `from` in the zone of the NAS (at most a year ahead). */
export function nextRuns(expr: string, count = 3, timeZone = 'UTC', from = Date.now()): Date[] {
	const c = parseCron(expr);
	if (!c) return [];
	let zone = timeZone;
	try {
		new Intl.DateTimeFormat('en-US', { timeZone: zone });
	} catch {
		zone = 'UTC';
	}
	const start = wall(from, zone);
	const hours = [...c.hour].sort((a, b) => a - b);
	const minutes = [...c.minute].sort((a, b) => a - b);
	const out: Date[] = [];
	const day = new Date(Date.UTC(start.y, start.mo - 1, start.d));
	for (let i = 0; i < 400 && out.length < count; i++, day.setUTCDate(day.getUTCDate() + 1)) {
		const y = day.getUTCFullYear(),
			mo = day.getUTCMonth() + 1,
			d = day.getUTCDate();
		if (!dayMatches(c, y, mo, d)) continue;
		const today = i === 0;
		for (const h of hours) {
			if (today && h < start.h) continue;
			for (const mi of minutes) {
				if (today && h === start.h && mi <= start.mi) continue;
				const at = instant(y, mo, d, h, mi, zone);
				if (at !== null && at > from) out.push(new Date(at));
				if (out.length >= count) return out;
			}
		}
	}
	return out;
}

// ---- Words -------------------------------------------------------------

export type Translate = (key: string, values?: Record<string, string | number>) => string;

/**
 * The schedule in words. `when` is short, for a list ("Täglich um 03:00 Uhr"),
 * `say` a sentence ("Läuft montags und donnerstags um 03:00 Uhr.").
 * `join` makes "a, b und c" in the language of the page.
 */
export function describe(
	expr: string,
	tr: Translate,
	join: (items: string[]) => string,
	form: 'when' | 'say' = 'when',
): string {
	const s = fromCron(expr);
	const key = (name: string) => `schedule.${form}.${name}`;
	if (!s) return tr(key('custom'), { cron: expr.trim() });
	let text: string;
	switch (s.kind) {
		case 'minutes':
			text = tr(key('minutes'), { n: s.every });
			break;
		case 'hourly':
			text = tr(key('hourly'), { minute: pad(s.minute) });
			break;
		case 'daily':
			text = tr(key('daily'), { time: s.time });
			break;
		case 'weekdays': {
			const workdays = s.days.length === 5 && [1, 2, 3, 4, 5].every((d) => s.days.includes(d));
			const days = workdays
				? tr('schedule.days.workdays')
				: join(s.days.map((d) => tr(`schedule.days.${d}`)));
			text = tr(key('weekdays'), { days, time: s.time });
			break;
		}
		case 'monthly':
			text =
				s.day === 'last'
					? tr(key('monthlyLast'), { time: s.time })
					: tr(key('monthly'), { day: s.day, time: s.time });
	}
	return text.charAt(0).toLocaleUpperCase() + text.slice(1);
}

/** The cron expression of a schedule stored in the older preset form (scripts). */
export function cronOfPreset(s: {
	every: string;
	minute?: number;
	time?: string;
	weekday?: number;
	monthDay?: number;
	cron?: string;
}): string {
	const [h, m] = clock(s.time ?? '') ?? [3, 0];
	switch (s.every) {
		case 'hourly':
			return `${s.minute ?? 0} * * * *`;
		case 'daily':
			return `${m} ${h} * * *`;
		case 'weekly':
			return `${m} ${h} * * ${s.weekday ?? 1}`;
		case 'monthly':
			return `${m} ${h} ${s.monthDay ?? 1} * *`;
		default:
			return (s.cron ?? '').trim();
	}
}
