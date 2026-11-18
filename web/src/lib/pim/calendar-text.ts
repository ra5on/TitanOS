// 1.11 (step 15): T-Kalender in words – dates, weekdays, ranges, the
// repetition presets, reminders, durations – in the language of the UI.
import { i18n, t } from '../i18n/index.svelte.ts';
import { addDays, dayOfMonth, isoWeek, weekday, type DayPart, clock } from './caldates.ts';
import {
	alarmWords,
	nthOfMonth,
	type CalEvent,
	type Calendar,
	type Repeat,
	type RepeatKind,
} from './calendar.ts';

const tag = () => (i18n.locale === 'en' ? 'en-GB' : 'de-DE');
const at = (day: string) => {
	const [y, m, d] = day.split('-').map(Number);
	return new Date(y, m - 1, d, 12);
};
// 1.13: a format is made once per language and options (making one costs
// far more than using it; the month asked for hundreds per drawing).
const formats = new Map<string, Intl.DateTimeFormat>();
function format(locale: string, o: Intl.DateTimeFormatOptions): Intl.DateTimeFormat {
	const key = locale + JSON.stringify(o);
	let f = formats.get(key);
	if (!f) {
		f = new Intl.DateTimeFormat(locale, o);
		formats.set(key, f);
	}
	return f;
}
const fmt = (day: string, o: Intl.DateTimeFormatOptions) =>
	format(i18n.locale === 'en' ? 'en-US' : 'de-DE', o).format(at(day));
const CLOCK: Intl.DateTimeFormatOptions = { hour: '2-digit', minute: '2-digit', hourCycle: 'h23' };

/** "Mo" … "So" (Monday first). */
export function weekdayShort(i: number): string {
	return (t('calendar.wd').split(',')[i] ?? '').trim();
}
export function weekdayLong(day: string): string {
	return fmt(day, { weekday: 'long' });
}
/** "November 2026". */
export function monthTitle(day: string): string {
	return fmt(day, { month: 'long', year: 'numeric' });
}
/** "Dienstag, 17. November" (with the year when it is not this year's). */
export function longDay(day: string, today?: string): string {
	const year = today && today.slice(0, 4) !== day.slice(0, 4) ? 'numeric' : undefined;
	return fmt(day, { weekday: 'long', day: 'numeric', month: 'long', year });
}
/** "17. November 2026". */
export function dateText(day: string): string {
	return fmt(day, { day: 'numeric', month: 'long', year: 'numeric' });
}
/** "15. Dezember" / "December 15". */
export function shortDate(day: string): string {
	return fmt(day, { day: 'numeric', month: 'long' });
}
/** "16. – 22. November 2026 · KW 47". */
export function weekTitle(start: string): string {
	const end = addDays(start, 6);
	const f = format(i18n.locale === 'en' ? 'en-US' : 'de-DE', {
		day: 'numeric',
		month: 'long',
		year: 'numeric',
	});
	return `${f.formatRange(at(start), at(end))} · ${t('calendar.weekN', { n: isoWeek(start) })}`;
}
/** "Dienstag, 17. November 2026 · KW 47". */
export function dayTitle(day: string): string {
	return `${fmt(day, { weekday: 'long', day: 'numeric', month: 'long', year: 'numeric' })} · ${t('calendar.weekN', { n: isoWeek(day) })}`;
}

/** The name of a calendar: the birthdays' in the UI language. */
export function calName(c: Pick<Calendar, 'name' | 'kind'> | undefined): string {
	if (!c) return '';
	return c.kind === 'birthdays' ? t('calendar.birthdays') : c.name;
}

/** The title of an event as shown: birthdays worded, an empty title named. */
export function eventTitle(e: Pick<CalEvent, 'kind' | 'title' | 'birthday'>): string {
	if (e.kind === 'birthday') {
		// Draft 01/03: "Anna Beispiel (35)" – the calendar "Geburtstage" says what it is.
		const name = e.birthday?.name || e.title;
		return typeof e.birthday?.age === 'number' ? `${name} (${e.birthday.age})` : name;
	}
	return e.title.trim() || t('calendar.untitled');
}

/** "19:00–20:30", "ab 22:00", "bis 02:00" or "ganztägig" for one day of an event. */
export function partTime(p: DayPart): string {
	if (p.allDay) return t('calendar.allDay');
	if (p.first && p.last) return `${clock(p.from)}–${clock(p.to)}`;
	if (p.first) return t('calendar.from', { time: clock(p.from) });
	if (p.last) return t('calendar.until', { time: clock(p.to) });
	return t('calendar.allDay');
}

/** The rule of a series as the server wrote it, in the UI language. */
export function ruleText(e: Pick<CalEvent, 'rule'>): string {
	const text = e.rule?.text;
	if (!text) return '';
	return (i18n.locale === 'en' ? text.en : text.de) || text.de || text.en || '';
}

/** A preset of the form for a start date in words. */
export function repeatLabel(kind: RepeatKind, start: string, custom = ''): string {
	const day = weekdayLong(start);
	switch (kind) {
		case 'none':
			return t('calendar.rep.none');
		case 'daily':
			return t('calendar.rep.daily');
		case 'workdays':
			return t('calendar.rep.workdays');
		case 'weekly':
			return t('calendar.rep.weekly', { day });
		case 'monthlyDay':
			return t('calendar.rep.monthlyDay', { n: dayOfMonth(start) });
		case 'monthlyNth':
			return t('calendar.rep.monthlyNth', {
				nth: t(`calendar.nth.${Math.min(nthOfMonth(start), 4)}` as 'calendar.nth.1'),
				day,
			});
		case 'monthlyLast':
			return t('calendar.rep.monthlyLast', { day });
		case 'yearly':
			return t('calendar.rep.yearly', { date: shortDate(start) });
		case 'custom':
			return custom ? t('calendar.rep.custom', { text: custom }) : t('calendar.rep.customPlain');
	}
}

/** The unit after "Alle n …". */
export function intervalWord(unit: 'day' | 'week' | 'month' | 'year', n: number): string {
	return t(`calendar.unit.${unit}${n === 1 ? '' : 's'}` as 'calendar.unit.day');
}

/** A reminder in words: "1 Stunde vorher", "Am Vortag um 18:00". */
export function alarmText(minutes: number, allDay: boolean, start?: number): string {
	const w = alarmWords(minutes, allDay, start);
	switch (w.kind) {
		case 'start':
			return t('calendar.alarm.start');
		case 'minutes':
			return t('calendar.alarm.minutes', { n: w.n });
		case 'hours':
			return w.n === 1 ? t('calendar.alarm.hour') : t('calendar.alarm.hours', { n: w.n });
		case 'days':
			return w.n === 1 ? t('calendar.alarm.day') : t('calendar.alarm.days', { n: w.n });
		case 'weeks':
			return w.n === 1 ? t('calendar.alarm.week') : t('calendar.alarm.weeks', { n: w.n });
		case 'mixed':
			return t('calendar.alarm.mixed', { h: w.hours, m: w.minutes });
		case 'dayAt':
			if (w.days === 0) return t('calendar.alarm.dayOf', { time: w.time });
			if (w.days === 1) return t('calendar.alarm.dayBefore', { time: w.time });
			return t('calendar.alarm.daysBefore', { n: w.days, time: w.time });
	}
}

/** "1 Stunde 30 Minuten", "2 Tage". */
export function durationText(minutes: number): string {
	const parts: string[] = [];
	const days = Math.floor(minutes / 1440);
	const hours = Math.floor((minutes % 1440) / 60);
	const mins = minutes % 60;
	if (days) parts.push(days === 1 ? t('calendar.dur.day') : t('calendar.dur.days', { n: days }));
	if (hours)
		parts.push(hours === 1 ? t('calendar.dur.hour') : t('calendar.dur.hours', { n: hours }));
	if (mins || !parts.length) parts.push(t('calendar.dur.minutes', { n: mins }));
	return parts.join(' ');
}

/** "Berlin" of "Europe/Berlin"; "UTC" stays. */
export function zoneName(zone: string): string {
	if (!zone) return '';
	const city = zone.split('/').pop() ?? zone;
	return city.replace(/_/g, ' ');
}

/** When an event is, for a dialog: "Dienstag, 17. November, 19:00–20:30". */
export function whenText(e: Pick<CalEvent, 'start' | 'end' | 'allDay'>): string {
	if (e.allDay) {
		const start = e.start.slice(0, 10);
		const last = e.end ? addDays(e.end.slice(0, 10), -1) : start;
		return last > start
			? `${longDay(start)} – ${longDay(last)}`
			: `${longDay(start)}, ${t('calendar.allDay')}`;
	}
	const s = new Date(e.start);
	const en = new Date(e.end || e.start);
	const f = format(tag(), CLOCK);
	const day = (d: Date) =>
		`${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`;
	if (day(s) === day(en)) return `${longDay(day(s))}, ${f.format(s)}–${f.format(en)}`;
	return `${longDay(day(s))}, ${f.format(s)} – ${longDay(day(en))}, ${f.format(en)}`;
}

/** Weekday of a date for the strip and the grid head. */
export function weekdayOf(day: string): string {
	return weekdayShort(weekday(day));
}

export type { Repeat };

/** When an occurrence begins: "Montag, 16. November, 09:00" (all-day: the day). */
export function startText(e: Pick<CalEvent, 'start' | 'allDay'>): string {
	if (e.allDay) return longDay(e.start.slice(0, 10));
	const s = new Date(e.start);
	const day = `${s.getFullYear()}-${String(s.getMonth() + 1).padStart(2, '0')}-${String(s.getDate()).padStart(2, '0')}`;
	const f = format(tag(), CLOCK);
	return `${longDay(day)}, ${f.format(s)}`;
}

/** The title of the detail dialog: a birthday says so ("Geburtstag: Anna (35)"). */
export function detailTitle(e: Pick<CalEvent, 'kind' | 'title' | 'birthday'>): string {
	if (e.kind !== 'birthday') return eventTitle(e);
	const name = e.birthday?.name || e.title;
	return typeof e.birthday?.age === 'number'
		? t('calendar.birthdayAge', { name, age: e.birthday.age })
		: t('calendar.birthdayOf', { name });
}
