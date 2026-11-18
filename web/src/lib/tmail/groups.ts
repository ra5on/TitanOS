// 1.12 (owner 18 Nov 2026, "Gruppiert nach Datum"): the list in four
// groups – Heute, Gestern, Diese Woche, Älter – in the browser's time zone,
// the week starting on the language's first day. The list comes from titand
// newest uid first; a mail moved into the folder has a high uid and an old
// date, so the groups stand in their fixed order and each is sorted by date
// (never "Heute … Älter … Heute").

export type DayGroup = 'today' | 'yesterday' | 'week' | 'older';

export const GROUP_ORDER: DayGroup[] = ['today', 'yesterday', 'week', 'older'];

/** The first day of the week of a language (1 = Monday … 7 = Sunday). */
export function weekStart(locale: string): number {
	try {
		const loc = new Intl.Locale(locale) as Intl.Locale & {
			getWeekInfo?: () => { firstDay: number };
			weekInfo?: { firstDay: number };
		};
		const info = loc.getWeekInfo?.() ?? loc.weekInfo;
		if (info && info.firstDay >= 1 && info.firstDay <= 7) return info.firstDay;
	} catch {
		// An unknown language: the fallback below.
	}
	return /^en(-US)?$/i.test(locale) ? 7 : 1;
}

function startOfDay(d: Date): Date {
	return new Date(d.getFullYear(), d.getMonth(), d.getDate());
}

/** The group of a date; a date in the future (a wrong clock) counts as today. */
export function dayGroup(iso: string, now: Date, locale: string): DayGroup {
	const date = new Date(iso);
	if (Number.isNaN(date.getTime())) return 'older';
	const today = startOfDay(now);
	if (date >= today) return 'today';
	const yesterday = new Date(today.getFullYear(), today.getMonth(), today.getDate() - 1);
	if (date >= yesterday) return 'yesterday';
	// JavaScript counts Sunday as 0; the week info counts it as 7.
	const first = weekStart(locale) % 7;
	const back = (today.getDay() - first + 7) % 7;
	const week = new Date(today.getFullYear(), today.getMonth(), today.getDate() - back);
	return date >= week ? 'week' : 'older';
}

export type Grouped<T> = { group: DayGroup; items: T[] };

/** Items in the four groups (empty ones left out), each newest first by date. */
export function groupByDay<T>(
	items: T[],
	dateOf: (item: T) => string,
	now: Date,
	locale: string,
): Grouped<T>[] {
	const by = new Map<DayGroup, { item: T; time: number; at: number }[]>();
	items.forEach((item, at) => {
		const iso = dateOf(item);
		const time = new Date(iso).getTime();
		const g = dayGroup(iso, now, locale);
		const list = by.get(g) ?? [];
		list.push({ item, time: Number.isNaN(time) ? 0 : time, at });
		by.set(g, list);
	});
	return GROUP_ORDER.filter((g) => by.has(g)).map((group) => ({
		group,
		items: by
			.get(group)!
			.sort((a, b) => b.time - a.time || a.at - b.at)
			.map((x) => x.item),
	}));
}
