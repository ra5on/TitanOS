// 1.11 (step 15): the date math of T-Kalender – ISO weeks, the month grid
// (Monday first), where an event of the API stands in a zone, the columns
// of overlapping events in the week view. Pure functions (node:test in
// tests/calendar.test.mjs); dates are plain 'YYYY-MM-DD' strings.
//
// Times: titand sends timed events as RFC 3339 with the offset of the
// event's zone, all-day events as dates with an exclusive end (like
// DTEND). The views place them in one zone: the browser's (a normal account
// cannot read the NAS zone; for a NAS and browser in the same place it is
// the same).

const pad = (n: number) => String(n).padStart(2, '0');

/** The date of a Date's local fields. */
export function isoDate(d: Date): string {
	return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
}

/** A date at noon (no DST edge) in the local zone of the JS engine – only for calendar arithmetic. */
function noon(day: string): Date {
	const [y, m, d] = day.split('-').map(Number);
	return new Date(y, m - 1, d, 12);
}

export function validDate(day: string): boolean {
	if (!/^\d{4}-\d{2}-\d{2}$/.test(day)) return false;
	return isoDate(noon(day)) === day;
}

export function addDays(day: string, n: number): string {
	const d = noon(day);
	d.setDate(d.getDate() + n);
	return isoDate(d);
}

/** Months forward (or back) on the first of the month. */
export function addMonths(day: string, n: number): string {
	const [y, m] = day.split('-').map(Number);
	const d = new Date(y, m - 1 + n, 1, 12);
	return isoDate(d);
}

/** Whole days from a to b (b later = positive). */
export function daysBetween(a: string, b: string): number {
	return Math.round((noon(b).getTime() - noon(a).getTime()) / 864e5);
}

/** 0 = Monday … 6 = Sunday. */
export function weekday(day: string): number {
	return (noon(day).getDay() + 6) % 7;
}

export function dayOfMonth(day: string): number {
	return Number(day.slice(8, 10));
}

export function monthLength(day: string): number {
	const [y, m] = day.split('-').map(Number);
	return new Date(y, m, 0).getDate();
}

/** The Monday of the week of a day. */
export function weekStart(day: string): string {
	return addDays(day, -weekday(day));
}

/** The ISO 8601 week number (week 1 holds the first Thursday). */
export function isoWeek(day: string): number {
	const [y, m, d] = day.split('-').map(Number);
	const t = new Date(Date.UTC(y, m - 1, d));
	t.setUTCDate(t.getUTCDate() + 3 - ((t.getUTCDay() + 6) % 7));
	const first = new Date(Date.UTC(t.getUTCFullYear(), 0, 4));
	return (
		1 +
		Math.round(
			(t.getTime() - first.getTime()) / 864e5 / 7 - (3 - ((first.getUTCDay() + 6) % 7)) / 7,
		)
	);
}

/** The weeks (Monday to Sunday) that show the month of `day`: 4 to 6 rows of 7 dates. */
export function monthGrid(day: string): string[][] {
	const first = day.slice(0, 8) + '01';
	const last = first.slice(0, 8) + pad(monthLength(first));
	const weeks: string[][] = [];
	for (let start = weekStart(first); start <= last; start = addDays(start, 7))
		weeks.push(Array.from({ length: 7 }, (_, i) => addDays(start, i)));
	return weeks;
}

/** The seven dates of the week of a day. */
export function weekDays(day: string): string[] {
	const start = weekStart(day);
	return Array.from({ length: 7 }, (_, i) => addDays(start, i));
}

/** The browser's zone (IANA name). */
export function localZone(): string {
	try {
		return Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC';
	} catch {
		return 'UTC';
	}
}

/** Whether the browser knows a zone by this IANA name. */
export function knownZone(zone: string): boolean {
	if (!zone) return false;
	try {
		new Intl.DateTimeFormat('en', { timeZone: zone });
		return true;
	} catch {
		return false;
	}
}

// The NAS zone (GET /calendar/access `timezone`, TECHNIK 2.2 point 5):
// times show in it and new events are made in it. Unknown or missing:
// the browser's zone.
let nasZone = '';

/** Takes the NAS zone from titand; anything the browser does not know is dropped. */
export function setNasZone(zone: unknown): void {
	nasZone = typeof zone === 'string' && knownZone(zone) ? zone : '';
}

/** The zone the calendar shows: the NAS zone, else the browser's. */
export function viewZone(): string {
	return nasZone || localZone();
}

const wallFormats = new Map<string, Intl.DateTimeFormat>();
/** Date and minutes after midnight of an instant on the wall clock of a zone. */
export function wallOf(at: Date, zone: string): { date: string; minutes: number } {
	let f = wallFormats.get(zone);
	if (!f) {
		f = new Intl.DateTimeFormat('en-CA', {
			timeZone: zone,
			year: 'numeric',
			month: '2-digit',
			day: '2-digit',
			hour: '2-digit',
			minute: '2-digit',
			hourCycle: 'h23',
		});
		wallFormats.set(zone, f);
	}
	const p: Record<string, string> = {};
	for (const part of f.formatToParts(at)) p[part.type] = part.value;
	return { date: `${p.year}-${p.month}-${p.day}`, minutes: Number(p.hour) * 60 + Number(p.minute) };
}

/** "07:45" for minutes after midnight. */
export function clock(minutes: number): string {
	const m = ((minutes % 1440) + 1440) % 1440;
	return `${pad(Math.floor(m / 60))}:${pad(m % 60)}`;
}

/** What the views need of an event: dates or instants. */
export type Timed = { start: string; end: string; allDay: boolean };

/** One day of an event in a zone: minutes [from, to) of that day; all-day without minutes. */
export type DayPart = {
	date: string;
	allDay: boolean;
	from: number;
	to: number;
	/** The event begins on this day (its time is shown here). */
	first: boolean;
	/** The event ends on this day. */
	last: boolean;
};

/** The days an event covers in a zone. All-day: end exclusive; timed: an end at 00:00 does not take that day. */
export function dayParts(ev: Timed, zone: string): DayPart[] {
	const out: DayPart[] = [];
	if (ev.allDay) {
		const start = ev.start.slice(0, 10);
		let end = ev.end ? ev.end.slice(0, 10) : addDays(start, 1);
		if (!validDate(start)) return out;
		if (!validDate(end) || end <= start) end = addDays(start, 1);
		// At most a year of days (a broken end must not draw forever).
		for (let d = start, i = 0; d < end && i < 400; d = addDays(d, 1), i++)
			out.push({
				date: d,
				allDay: true,
				from: 0,
				to: 1440,
				first: d === start,
				last: addDays(d, 1) === end,
			});
		return out;
	}
	const s = new Date(ev.start);
	let e = new Date(ev.end || ev.start);
	if (Number.isNaN(s.getTime())) return out;
	if (Number.isNaN(e.getTime()) || e < s) e = s;
	const a = wallOf(s, zone);
	const b = wallOf(e, zone);
	let endDate = b.date;
	let endMin = b.minutes;
	if (endMin === 0 && endDate > a.date) {
		endDate = addDays(endDate, -1);
		endMin = 1440;
	}
	for (let d = a.date, i = 0; d <= endDate && i < 400; d = addDays(d, 1), i++)
		out.push({
			date: d,
			allDay: false,
			from: d === a.date ? a.minutes : 0,
			to: d === endDate ? Math.max(endMin, d === a.date ? a.minutes : 0) : 1440,
			first: d === a.date,
			last: d === endDate,
		});
	return out;
}

/** An event spanning several days, or timed over midnight: shown in the all-day row of the week. */
export function longEvent(ev: Timed, zone: string): boolean {
	return ev.allDay || dayParts(ev, zone).length > 1;
}

/** Placed in the week view: the lane of an event among the ones it overlaps. */
export type Lane = { lane: number; lanes: number };

/**
 * Columns for overlapping events of one day (from/to in minutes). An event
 * counts at least `minLength` minutes long (its block has a minimum height).
 * Returns one Lane per input, in the input's order.
 */
export function overlapLanes(items: { from: number; to: number }[], minLength = 30): Lane[] {
	const order = items
		.map((it, i) => ({ i, from: it.from, to: Math.max(it.to, it.from + minLength) }))
		.sort((x, y) => x.from - y.from || y.to - x.to || x.i - y.i);
	const out: Lane[] = items.map(() => ({ lane: 0, lanes: 1 }));
	let cluster: typeof order = [];
	let ends: number[] = [];
	let clusterEnd = -1;
	const close = () => {
		for (const c of cluster) out[c.i].lanes = ends.length;
		cluster = [];
		ends = [];
	};
	for (const it of order) {
		if (it.from >= clusterEnd) close();
		let lane = ends.findIndex((end) => end <= it.from);
		if (lane < 0) {
			lane = ends.length;
			ends.push(it.to);
		} else ends[lane] = it.to;
		out[it.i].lane = lane;
		cluster.push(it);
		clusterEnd = Math.max(clusterEnd, it.to);
	}
	close();
	return out;
}

/** All-day in the form (last day inclusive) → the API's exclusive end. */
export function exclusiveEnd(lastDay: string): string {
	return addDays(lastDay, 1);
}

/** The API's exclusive end → the last day the form shows. */
export function inclusiveEnd(start: string, end: string): string {
	const last = addDays(end, -1);
	return last < start ? start : last;
}

/** The wall clock of an API time as the form edits it: '2026-11-17T19:00:00+01:00' → date, time. */
export function wallParts(value: string): { date: string; time: string } {
	return { date: value.slice(0, 10), time: value.length >= 16 ? value.slice(11, 16) : '' };
}

/** Minutes between two wall times of the form (dates and HH:MM, same zone, DST ignored). */
export function wallMinutes(
	fromDate: string,
	fromTime: string,
	toDate: string,
	toTime: string,
): number {
	const m = (t: string) => Number(t.slice(0, 2)) * 60 + Number(t.slice(3, 5));
	return daysBetween(fromDate, toDate) * 1440 + m(toTime) - m(fromTime);
}
