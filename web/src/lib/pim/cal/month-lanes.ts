// 1.15 (PLAN 115-5, drafts 36/37): the month as Apple draws it – one bar
// per event, a multi-day event one bar across its days (one piece per
// week), stacked in lanes; the phone shows at most a few lanes and "+N"
// for the rest of a day. Pure: the views place the bars in a CSS grid
// (grid-column from `start`/`span`, grid-row from `lane`), nothing is
// measured.
import type { DayItem } from '../calendar.ts';
import type { DueTask } from './tasks.ts';

/** One bar of a week: an event over the days [start, start + span) of the week (0 = Monday). */
export type WeekBar = {
	/** The event's key and the bar's first day (unique in a week). */
	key: string;
	item: DayItem;
	start: number;
	span: number;
	lane: number;
	/** The event began before this week (the bar runs on from the left). */
	before: boolean;
	/** The event goes on after this week. */
	after: boolean;
};

/**
 * Lanes for pieces of a row of columns, in the given order: each takes the
 * lowest lane free over all its columns. Returns the lane of each piece.
 */
export function packLanes(
	pieces: { start: number; span: number }[],
	taken: boolean[][] = [],
): number[] {
	return pieces.map(({ start, span }) => {
		for (let lane = 0; ; lane++) {
			const row = (taken[lane] ??= []);
			let free = true;
			for (let c = start; c < start + span; c++) if (row[c]) free = false;
			if (!free) continue;
			for (let c = start; c < start + span; c++) row[c] = true;
			return lane;
		}
	});
}

/**
 * The bars of one week (its seven days, Monday first) from the events per
 * day: longer bars first (they keep the top lanes, as at Apple), then by
 * the first day, all-day before timed, by time and title (eventsByDay's
 * order of a day).
 */
export function weekBars(
	week: string[],
	byDay: Map<string, DayItem[]>,
	taken: boolean[][] = [],
): WeekBar[] {
	const found = new Map<
		string,
		{ item: DayItem; first: number; last: number; order: number; open: boolean }
	>();
	/** The bar an event's next day continues (only a part that runs on from the day before). */
	const running = new Map<string, string>();
	let order = 0;
	week.forEach((d, col) => {
		for (const it of byDay.get(d) ?? []) {
			const event = it.key.slice(0, it.key.lastIndexOf('\u0000'));
			const bar = running.get(event);
			const had = bar && !it.part.first ? found.get(bar) : undefined;
			if (had && had.open && had.last === col - 1) {
				had.last = col;
				had.open = !it.part.last;
				continue;
			}
			const key = `${event}\u0000${d}`;
			found.set(key, { item: it, first: col, last: col, order: order++, open: !it.part.last });
			running.set(event, key);
		}
	});
	const list = [...found.entries()]
		.map(([key, f]) => ({ key, ...f, span: f.last - f.first + 1 }))
		.sort((a, b) => b.span - a.span || a.first - b.first || a.order - b.order);
	const lanes = packLanes(
		list.map((b) => ({ start: b.first, span: b.span })),
		taken,
	);
	return list.map((b, i) => ({
		key: b.key,
		item: b.item,
		start: b.first,
		span: b.span,
		lane: lanes[i],
		before: !b.item.part.first,
		after: b.open,
	}));
}

/** How many bars of a week are hidden on each of its days with only `max` lanes shown. */
export function hiddenPerDay(bars: WeekBar[], max: number): number[] {
	const out = [0, 0, 0, 0, 0, 0, 0];
	for (const b of bars)
		if (b.lane >= max) for (let c = b.start; c < b.start + b.span; c++) out[c]++;
	return out;
}

/** The number of lanes a week uses. */
export function laneCount(bars: WeekBar[]): number {
	return bars.reduce((n, b) => Math.max(n, b.lane + 1), 0);
}

/** A reminder due on a day of the week, in a lane below or between the bars. */
export type WeekTask = { task: DueTask; start: number; lane: number };

/** The bars and the reminders of one week, packed into the same lanes (reminders after the events). */
export function weekLayout(
	week: string[],
	byDay: Map<string, DayItem[]>,
	due?: Map<string, DueTask[]>,
): { bars: WeekBar[]; tasks: WeekTask[]; lanes: number } {
	const taken: boolean[][] = [];
	const bars = weekBars(week, byDay, taken);
	const pieces: { task: DueTask; start: number }[] = [];
	week.forEach((d, col) => {
		for (const task of due?.get(d) ?? []) pieces.push({ task, start: col });
	});
	const lanes = packLanes(
		pieces.map((p) => ({ start: p.start, span: 1 })),
		taken,
	);
	const tasks = pieces.map((p, i) => ({ ...p, lane: lanes[i] }));
	return {
		bars,
		tasks,
		lanes: Math.max(laneCount(bars), ...tasks.map((t) => t.lane + 1), 0),
	};
}

/** Hidden bars and reminders per day of a week with only `max` lanes shown. */
export function hiddenOf(layout: { bars: WeekBar[]; tasks: WeekTask[] }, max: number): number[] {
	const out = hiddenPerDay(layout.bars, max);
	for (const t of layout.tasks) if (t.lane >= max) out[t.start]++;
	return out;
}

/** One day of a week on the desktop: its single-day events and reminders, in eventsByDay's order. */
export type DayStack = { items: DayItem[]; tasks: DueTask[] };

/**
 * The desktop's week (1.15): events over several days (and the pieces of
 * such events) as bars in lanes on top, below them each day's own events
 * and reminders as a stack – a long title then only makes its own day
 * taller, and "+N" is counted per day.
 */
export function weekSplit(
	week: string[],
	byDay: Map<string, DayItem[]>,
	due?: Map<string, DueTask[]>,
): { bars: WeekBar[]; lanes: number; days: DayStack[] } {
	const longOnly = new Map<string, DayItem[]>();
	for (const d of week)
		longOnly.set(
			d,
			(byDay.get(d) ?? []).filter((it) => it.long),
		);
	const bars = weekBars(week, longOnly);
	return {
		bars,
		lanes: laneCount(bars),
		days: week.map((d) => ({
			items: (byDay.get(d) ?? []).filter((it) => !it.long),
			tasks: due?.get(d) ?? [],
		})),
	};
}
