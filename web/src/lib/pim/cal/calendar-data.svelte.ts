// 1.13 (Baustand step 9, docs/KALENDER.md "1.13"): what the page of Kalender
// reads, kept apart from its views. Measured before (scripts/
// test-ui-measure-kalender.mjs): the page asked /calendar/access, then the
// calendars, then the events one after the other, read every clipped event
// again on its own (up to 40, each drawing the views anew), read
// everything twice after hiding a calendar and again for every change of
// another window. Now:
//
// - the three first requests go out together (the window in the browser's
//   zone; the NAS zone moves it at most by the day kept around it);
// - the events come with whole titles (full=1) and their ETag: the same
//   window again is a 304 (calendar-api.ts loadEvents);
// - hiding a calendar takes its events off at once, showing it reads only
//   it; the NAS keeps the choice (PUT /preferences/calendar);
// - the live stream (watchPim) reads only the collection that changed
//   (the birthdays for a change of the contacts); a change announced by
//   another window of this browser (the event window saved) is read once
//   unless the stream told it already.
import { api } from '#lib/api.ts';
import { apiErrorText } from '#lib/i18n/index.svelte.ts';
import { addDays, setNasZone, viewZone } from '../caldates.ts';
import { BIRTHDAYS, type CalEvent, type Calendar } from '../calendar.ts';
import { loadCalendars, loadEvents, setHidden, type EventList } from '../calendar-api.ts';
import type { PimEvent } from '../live.ts';
import { calendarShell, shellOfError, type ShellState } from '../shell.ts';

/** How long a change announced by another window waits for the live stream to name it. */
const ANNOUNCED_WAIT = 1200;
/** Changes of a collection within this time are read once. */
const GATHER = 200;

export class CalendarData {
	shown = $state<ShellState>({ kind: 'loading' });
	calendars = $state<Calendar[]>([]);
	events = $state<CalEvent[]>([]);
	truncated = $state(false);
	birthdaysUnavailable = $state(false);
	/** An error of a later read (the views stay). */
	error = $state('');
	/** The zone the views show: the NAS zone once /calendar/access named it. */
	zone = $state(viewZone());
	/** Bumped when the events of the shown window came (the views keep their place). */
	loaded = $state(0);

	/** The window asked for: [from, to) as dates. */
	#from = '';
	#to = '';
	#asked = 0;
	#generations = new Map<string, string>();
	#dirty = new Set<string>();
	#gatherTimer: ReturnType<typeof setTimeout> | undefined;
	#announcedTimer: ReturnType<typeof setTimeout> | undefined;

	/** The days shown, first and last: the window read is a day wider on each side. */
	#windowOf(first: string, last: string) {
		return { from: addDays(first, -1), to: addDays(last, 2) };
	}

	/**
	 * The first reading: access, calendars and the events of the shown days
	 * at once. Returns the zone titand named (the page may then show other
	 * days and asks again through `show`).
	 */
	async start(first: string, last: string): Promise<void> {
		const { from, to } = this.#windowOf(first, last);
		this.#from = from;
		this.#to = to;
		const mine = ++this.#asked;
		const access = api.get<unknown>('/calendar/access');
		const calendars = loadCalendars();
		const events = loadEvents(from, to);
		// Errors of the later two count only when access said the data is there.
		calendars.catch(() => {});
		events.catch(() => {});
		try {
			const a = await access;
			setNasZone((a as { timezone?: unknown } | null)?.timezone);
			this.zone = viewZone();
			const state = calendarShell(a);
			if (state.kind !== 'ready') {
				this.shown = state;
				return;
			}
			this.calendars = await calendars;
			const got = await events;
			if (mine === this.#asked) this.#take(got);
			this.shown = { kind: 'ready' };
		} catch (err) {
			this.shown = shellOfError(err);
		}
	}

	#take(got: EventList) {
		this.events = got.events;
		this.truncated = got.truncated;
		this.birthdaysUnavailable = got.birthdaysUnavailable;
		this.error = '';
		this.loaded++;
	}

	#failed(err: unknown) {
		const s = shellOfError(err);
		if (s.kind === 'unavailable') this.shown = s;
		else this.error = apiErrorText(err);
	}

	/** The view shows these days: reads them unless they are the window read already. */
	async show(first: string, last: string, again = false): Promise<void> {
		const { from, to } = this.#windowOf(first, last);
		if (!again && from === this.#from && to === this.#to) return;
		this.#from = from;
		this.#to = to;
		await this.reload();
	}

	/** The shown window again (an unchanged one is a 304). */
	async reload(): Promise<void> {
		if (this.shown.kind !== 'ready' || !this.#from) return;
		const mine = ++this.#asked;
		try {
			const got = await loadEvents(this.#from, this.#to);
			if (mine === this.#asked) this.#take(got);
		} catch (err) {
			if (mine === this.#asked) this.#failed(err);
		}
	}

	/** Reads only these collections of the shown window and puts them in place of theirs. */
	async refresh(ids: string[]): Promise<void> {
		const shown = ids.filter((id) => this.calendars.some((c) => c.id === id && !c.hidden));
		if (this.shown.kind !== 'ready' || !shown.length || !this.#from) return;
		const window = this.#from + '/' + this.#to;
		const asked = this.#asked;
		try {
			const got = await loadEvents(this.#from, this.#to, shown);
			// A newer window (or a whole reading) came meanwhile: that one counts.
			if (window !== this.#from + '/' + this.#to || asked !== this.#asked) return;
			this.events = [
				...this.events.filter((e) => !shown.includes(e.calendar)),
				...got.events.filter((e) => shown.includes(e.calendar)),
			];
			this.truncated = this.truncated || got.truncated;
			if (shown.includes(BIRTHDAYS)) this.birthdaysUnavailable = false;
			this.loaded++;
		} catch (err) {
			if (shown.includes(BIRTHDAYS) && shown.length === 1) this.birthdaysUnavailable = true;
			else this.#failed(err);
		}
	}

	/** The calendars again (a new or removed one, a name or color). */
	async reloadCalendars(): Promise<void> {
		try {
			this.calendars = await loadCalendars();
		} catch (err) {
			this.#failed(err);
		}
	}

	/** Shows or hides a calendar: at once here, the NAS keeps it. */
	async toggle(c: Calendar): Promise<void> {
		const hide = !c.hidden;
		this.calendars = this.calendars.map((x) => (x.id === c.id ? { ...x, hidden: hide } : x));
		const hidden = this.calendars.filter((x) => x.hidden).map((x) => x.id);
		if (hide) this.events = this.events.filter((e) => e.calendar !== c.id);
		const saved = setHidden(hidden);
		if (!hide) void this.refresh([c.id]);
		try {
			await saved;
			this.error = '';
		} catch (err) {
			// Not kept: as it was.
			this.calendars = this.calendars.map((x) => (x.id === c.id ? { ...x, hidden: !hide } : x));
			this.error = apiErrorText(err);
			if (hide) void this.refresh([c.id]);
			else this.events = this.events.filter((e) => e.calendar !== c.id);
		}
	}

	/** An event of the views changed here (moved): shown at once, the NAS answers later. */
	replace(old: CalEvent, next: CalEvent) {
		this.events = this.events.map((e) => (e === old ? next : e));
	}

	#gather(id: string) {
		this.#dirty.add(id);
		clearTimeout(this.#gatherTimer);
		this.#gatherTimer = setTimeout(() => {
			const ids = [...this.#dirty];
			this.#dirty.clear();
			if (ids.includes('*')) {
				void this.reloadCalendars().then(() => this.reload());
				return;
			}
			void this.refresh(ids);
		}, GATHER);
	}

	/** An event of the live stream (watchPim). */
	live(e: PimEvent) {
		if (this.shown.kind !== 'ready') return;
		if (e.type === 'resync') {
			this.#gather('*');
			return;
		}
		if (e.type === 'ready') {
			for (const [k, v] of Object.entries(e.generations)) this.#generations.set(k, v);
			return;
		}
		const key = `${e.kind}/${e.collection}`;
		if (this.#generations.get(key) === e.generation) return;
		this.#generations.set(key, e.generation);
		clearTimeout(this.#announcedTimer);
		if (e.kind === 'contacts') {
			this.#gather(BIRTHDAYS);
			return;
		}
		// A calendar this page does not know, or one that is gone: all of it.
		if (!e.generation || !this.calendars.some((c) => c.id === e.collection)) this.#gather('*');
		else this.#gather(e.collection);
	}

	/**
	 * Another window of this browser changed events (BroadcastChannel): the
	 * live stream names the collection a moment later; without it, the
	 * window is read again.
	 */
	announced() {
		clearTimeout(this.#announcedTimer);
		this.#announcedTimer = setTimeout(() => void this.reload(), ANNOUNCED_WAIT);
	}

	stop() {
		clearTimeout(this.#gatherTimer);
		clearTimeout(this.#announcedTimer);
	}
}
