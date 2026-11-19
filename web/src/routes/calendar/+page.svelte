<script lang="ts">
	// 1.11: T-Kalender (docs/KALENDER.md, drafts 01–04, 07, 40–42). Without
	// the data volume the page says "nicht verfügbar", never an empty grid.
	// An event opens its details; "Neuer Termin" and "Bearbeiten" open
	// /calendar/event in a window of its own (a page on the phone).
	//
	// 1.13 (owner 18 Nov 2026, docs/KALENDER.md "1.13"): the shared
	// ColumnFrame, edge to edge and as high as the window, from the first
	// frame on (loading and "nicht verfügbar" stand in the content: nothing
	// jumps). The side bar: "Neuer Termin" on top, the small month, "Meine
	// Kalender" (sorted per account on the NAS), the reminders (TaskSide),
	// at its foot "Neuer Kalender" and "Am iPhone einrichten". Over the
	// content one bar: ‹ Heute › and the month, Monat · Woche · Tag · Liste.
	// The title bar of the window names only the place. The data: CalendarData
	// (three first requests at once, full=1, ETag, the live stream, hiding
	// without reading everything again) and TaskData.
	// Changing by dragging (week, day, month; desktop): a moved or longer
	// event is saved at once, a series asks first which events are meant
	// (only this one, this and the following, all); what happened is said
	// in the status line.
	// Compact (the phone or under 620 px): levels like Mail – the month with
	// dots and the chosen day's events below (or the list), the calendars
	// and the reminders each a level of their own with "‹ Zurück"; "Heute",
	// "Kalender", "Erinnerungen" and "Neuer Termin" (+, at the right) in the
	// bar at the bottom. No dragging there; an event opens to be edited.
	// The KI-Agent (1.13, with session.services.agent): its panel right of
	// the content (a column of the frame) or, without room and compact, a
	// sheet from below; open or not kept per device. The day shown (and an
	// event asked about from its details) goes with each message as a
	// reference, never its content. A draft of a reminder (?agentDraft=,
	// "Öffnen und bearbeiten") opens the reminder dialog filled in, ticking
	// one off asks first; nothing is saved without the person.
	//
	// 1.15 (PLAN 115-5, drafts 33–39, owner 19 Nov 2026 "wie Apple Kalender";
	// docs/KALENDER.md "1.15"): compact, the months run on while scrolling
	// (MonthScroll) with a bar per event and "+N", today a red circle; a tap
	// on a day opens it – its week moves up and becomes the week strip, the
	// hours come in from below (motion.ts, only with "Animationen: An" and
	// without reduced motion). The day: week strip, "Mittwoch – 18. Nov.
	// 2026", the row "Ganztägig" with chips that wrap, the hours with the
	// red line of now; a swipe goes to the next day or week. Floating opaque
	// pills: on top "‹ 2026" (the year) or "‹ November" (back to the month)
	// and views · search · new; below "Heute" and Kalender · Erinnerungen.
	// The KI-Agent is in the ⋯ menu of the head (the command calendar.agent;
	// a narrow browser tab without that menu has it in the views' menu). Wide (from 620 px) the
	// same language: the month's bars with titles fill the height, the day
	// has the week strip, the week red circles and the red line of now.
	import { onDestroy, onMount, tick, untrack } from 'svelte';
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { ApiError } from '#lib/api.ts';
	import Dropdown from '#lib/components/Dropdown.svelte';
	import ColumnFrame, { type FrameLevel } from '#lib/components/ColumnFrame.svelte';
	import { compactView } from '#lib/components/compact.svelte.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import Icon from '#lib/components/Icon.svelte';
	import PageHead from '#lib/components/PageHead.svelte';
	import Segments from '#lib/components/Segments.svelte';
	import { phoneView } from '#lib/desktop/mode.svelte.ts';
	import { apiErrorText, t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import { registerMenuCommands } from '#lib/menu-commands.svelte.ts';
	import { entries, menusFor } from '#lib/desktop/menubar/menus.ts';
	import { inWindow } from '#lib/desktop/frame.ts';
	import { applyOrder, orderOf, setOrder } from '#lib/side-order.svelte.ts';
	import {
		addDays,
		addMonths,
		clock,
		instantAt,
		monthGrid,
		rfc3339,
		validDate,
		wallOf,
		weekDays,
		weekStart,
	} from '#lib/pim/caldates.ts';
	import CalIcon from '#lib/pim/cal/CalIcon.svelte';
	import DayStrip from '#lib/pim/cal/DayStrip.svelte';
	import MonthScroll from '#lib/pim/cal/MonthScroll.svelte';
	import SearchPanel from '#lib/pim/cal/SearchPanel.svelte';
	import YearView from '#lib/pim/cal/YearView.svelte';
	import { glideFrom, motionOn, play } from '#lib/pim/cal/motion.ts';
	import { BIRTHDAYS, eventsByDay, type CalEvent, type Calendar } from '#lib/pim/calendar.ts';
	import {
		eventHref,
		onChange,
		openEventWindow,
		updateEvent,
		type Scope,
	} from '#lib/pim/calendar-api.ts';
	import {
		dayLine,
		dayTitle,
		eventTitle,
		longDay,
		monthName,
		monthTitle,
		weekTitle,
		whenText,
		yearText,
	} from '#lib/pim/calendar-text.ts';
	import { watchPim } from '#lib/pim/live.ts';
	import AgendaList from '#lib/pim/cal/AgendaList.svelte';
	import { CalendarData } from '#lib/pim/cal/calendar-data.svelte.ts';
	import CalendarDialog from '#lib/pim/cal/CalendarDialog.svelte';
	import CalendarList from '#lib/pim/cal/CalendarList.svelte';
	import DeleteEventDialog from '#lib/pim/cal/DeleteEventDialog.svelte';
	import EventDetail from '#lib/pim/cal/EventDetail.svelte';
	import MiniMonth from '#lib/pim/cal/MiniMonth.svelte';
	import MonthView from '#lib/pim/cal/MonthView.svelte';
	import ScopeChoices from '#lib/pim/cal/ScopeChoices.svelte';
	import ReminderSettings from '#lib/pim/cal/ReminderSettings.svelte';
	import { TaskData } from '#lib/pim/cal/task-data.svelte.ts';
	import TaskDialog from '#lib/pim/cal/TaskDialog.svelte';
	import TaskSide from '#lib/pim/cal/TaskSide.svelte';
	import { tasksByDay, type DueTask, type TaskList } from '#lib/pim/cal/tasks.ts';
	import WeekView, { type TimedChange } from '#lib/pim/cal/WeekView.svelte';
	import { loadTask, setTaskDone, type Task } from '#lib/pim/cal/tasks.ts';
	import AgentPanel from '#lib/components/agent/AgentPanel.svelte';
	import AgentSheet from '#lib/components/agent/BottomSheet.svelte';
	import type { AgentContext } from '#lib/agent.ts';
	import { loadAgentDraft, type AgentDraft } from '#lib/agent/drafts.ts';
	import { calendarPanelActions } from '#lib/agent/panel-actions.ts';
	import { session } from '#lib/session.svelte.ts';

	type View = 'month' | 'week' | 'day' | 'list' | 'year' | 'search';

	const phone = $derived(phoneView());
	const compact = $derived(compactView());
	const data = new CalendarData();
	const tasks = new TaskData();
	let clockNow = $state(new Date());
	const timer = setInterval(() => (clockNow = new Date()), 30_000);
	onDestroy(() => {
		clearInterval(timer);
		data.stop();
		tasks.stop();
	});
	const zone = $derived(data.zone);
	const today = $derived(wallOf(clockNow, zone).date);
	const nowMinutes = $derived(wallOf(clockNow, zone).minutes);

	// The view, the day and the phone's level live in the address.
	// Compact: month, day, list, the year and the search; the week is the day there.
	const view = $derived.by((): View => {
		const v = page.url.searchParams.get('view');
		if (v === 'day' || v === 'list' || v === 'search') return v;
		if (v === 'week') return compact ? 'day' : 'week';
		if (v === 'year') return compact ? 'year' : 'month';
		return 'month';
	});
	const anchor = $derived.by(() => {
		const d = page.url.searchParams.get('date') ?? '';
		return validDate(d) ? d : today;
	});
	const level = $derived.by((): FrameLevel => {
		const l = page.url.searchParams.get('level');
		return compact && l === 'cals' ? 'side' : compact && l === 'tasks' ? 'list' : 'content';
	});
	/** `push`: a step the way back returns from (a day opened from the month, the year). */
	function go(v: View, date: string, lvl = '', push = false) {
		const q = new URLSearchParams();
		q.set('view', v);
		if (date !== today) q.set('date', date);
		if (lvl) q.set('level', lvl);
		return goto(`/calendar?${q}`, { replace: !lvl && !push, reset: false });
	}
	function up() {
		if (history.length > 1) history.back();
		else go(view, anchor);
	}

	let listLength = $state(30);
	let miniMonth = $state('');
	/** The month at the top of the phone's continuous months (its events are read around it). */
	let seenMonth = $state('');
	/** Where the continuous months scroll to; `jump` asks them to. */
	let monthTarget = $state('');
	let jump = $state(0);
	/** The day just left for the month (its circle there, as at Apple). */
	let lastDay = $state('');
	let detail = $state<CalEvent | null>(null);
	let deleting = $state<CalEvent | null>(null);
	let calDialog = $state<{ calendar?: Calendar | TaskList; tasks?: boolean } | null>(null);
	let taskDialog = $state<{
		task?: Pick<DueTask, 'list' | 'id'>;
		due?: string;
		draft?: Record<string, unknown>;
	} | null>(null);
	let said = $state('');
	const announce = (text: string) => (said = said === text ? text + ' ' : text);

	// The calendars in the account's order (the birthdays too).
	const calendars = $derived(applyOrder(data.calendars, orderOf('calendar'), (c) => c.id));
	const byId = $derived(new Map(data.calendars.map((c) => [c.id, c])));
	const calFor = (id: string) => byId.get(id);
	const own = $derived(data.calendars.some((c) => c.kind === 'own' && !c.readOnly));

	/** The days the view shows. */
	const range = $derived.by((): { first: string; last: string } => {
		if (compact && (view === 'month' || view === 'year' || view === 'search')) {
			// The month at the top, the one before and two after (the phone scrolls on).
			const m = (seenMonth || monthTarget || anchor).slice(0, 7) + '-01';
			return { first: addMonths(m, -1), last: addDays(addMonths(m, 3), -1) };
		}
		if (view === 'month' || view === 'year' || view === 'search') {
			const g = monthGrid(anchor);
			return { first: g[0][0], last: g[g.length - 1][6] };
		}
		if (view === 'week') return { first: weekStart(anchor), last: addDays(weekStart(anchor), 6) };
		// The day with its week and the weeks around (the strip, a swipe on).
		if (view === 'day')
			return { first: addDays(weekStart(anchor), -7), last: addDays(weekStart(anchor), 13) };
		return { first: anchor, last: addDays(anchor, listLength - 1) };
	});
	const byDay = $derived(eventsByDay(data.events, zone, range.first, range.last));
	const dueByDay = $derived(tasksByDay(data.shown.kind === 'ready' ? tasks.due : [], zone));

	let started = false;
	$effect(() => {
		const { first, last } = range;
		if (!started) {
			started = true;
			void data.start(first, last);
			void tasks.start();
		} else void data.show(first, last);
		void tasks.showDays(first, last);
	});

	// Live: the stream names the collection that changed; another window of
	// this browser (the event window) says so over the BroadcastChannel.
	onMount(() =>
		watchPim((e) => {
			if (e.type === 'pim' && e.kind === 'tasks') tasks.refresh();
			else if (e.type === 'resync') {
				tasks.refresh();
				data.live(e);
			} else data.live(e);
		}),
	);
	const stopChange = onChange(() => data.announced());
	onDestroy(stopChange);

	function step(dir: -1 | 1) {
		if (view === 'month') go(view, addMonths(anchor, dir));
		else if (view === 'week') go(view, addDays(anchor, 7 * dir));
		else if (view === 'day') go(view, addDays(anchor, dir));
		else go(view, addDays(anchor, 7 * dir));
	}

	const back = $derived(page.url.pathname + page.url.search);
	function newEvent(
		date = view === 'list' || view === 'search' || view === 'year' ? today : anchor,
		minutes?: number,
		to?: number,
	) {
		const time =
			minutes !== undefined
				? clock(minutes)
				: date === today
					? clock(Math.min(23 * 60, (Math.floor(nowMinutes / 60) + 1) * 60))
					: '09:00';
		const end =
			to === undefined
				? {}
				: to >= 1440
					? { endDate: addDays(date, 1), end: clock(to - 1440) }
					: { end: clock(to) };
		openEventWindow(eventHref({ date, time, back, ...end }));
	}
	function edit(ev: CalEvent) {
		detail = null;
		openEventWindow(eventHref({ cal: ev.calendar, id: ev.id, occurrence: ev.occurrence, back }));
	}

	// ---- Moving by dragging (or the keys of the week) ----
	const canEdit = (ev: CalEvent) =>
		!compact &&
		ev.kind === 'event' &&
		!ev.readOnly &&
		!ev.recurUnsupported &&
		calFor(ev.calendar)?.kind === 'own' &&
		!calFor(ev.calendar)?.readOnly;
	type Move = { ev: CalEvent; start: string; end: string; allDay: boolean; what: string };
	let moving = $state<Move | null>(null);
	let moveScope = $state<Scope>('this');
	let moveError = $state('');

	function timedChange(ev: CalEvent, c: TimedChange) {
		const start = rfc3339(instantAt(c.date, c.from, zone));
		const end = rfc3339(instantAt(c.date, c.to, zone));
		const what = `${longDay(c.date)}, ${clock(c.from)}–${clock(c.to % 1440)}`;
		ask({ ev, start, end, allDay: false, what });
	}
	function shift(ev: CalEvent, n: number) {
		if (ev.allDay) {
			const start = addDays(ev.start.slice(0, 10), n);
			const end = addDays((ev.end || ev.start).slice(0, 10), n);
			ask({ ev, start, end, allDay: true, what: whenText({ start, end, allDay: true }) });
			return;
		}
		const s = wallOf(new Date(ev.start), zone);
		const e = wallOf(new Date(ev.end || ev.start), zone);
		const start = rfc3339(instantAt(addDays(s.date, n), s.minutes, zone));
		const end = rfc3339(instantAt(addDays(e.date, n), e.minutes, zone));
		ask({
			ev,
			start,
			end,
			allDay: false,
			what: `${longDay(addDays(s.date, n))}, ${clock(s.minutes)}`,
		});
	}
	function ask(m: Move) {
		moveError = '';
		if (m.ev.recurring) {
			moveScope = 'this';
			moving = m;
			return;
		}
		void save(m, 'all');
	}
	async function save(m: Move, scope: Scope) {
		moving = null;
		const title = eventTitle(m.ev);
		// Shown at once where it goes; the NAS answers and the stream reads it again.
		const changed = { ...m.ev, start: m.start, end: m.end, allDay: m.allDay };
		if (scope === 'this' || !m.ev.recurring) data.replace(m.ev, changed);
		try {
			await updateEvent(m.ev.calendar, m.ev.id, {
				etag: m.ev.etag,
				scope,
				occurrence: m.ev.occurrence,
				event: { start: m.start, end: m.end, allDay: m.allDay, timezone: '' },
			});
			announce(t('calendar.drag.saved', { title, when: m.what }));
		} catch (err) {
			moveError =
				err instanceof ApiError && err.code === 'calendar_conflict'
					? t('calendar.conflict')
					: apiErrorText(err);
			announce(t('calendar.drag.failed', { title, reason: moveError }));
		}
		void data.refresh([m.ev.calendar]);
	}

	registerMenuCommands({
		'calendar.newEvent': {
			run: () => newEvent(),
			enabled: () => data.shown.kind === 'ready' && own,
		},
		'calendar.newCalendar': {
			run: () => (calDialog = {}),
			enabled: () => data.shown.kind === 'ready',
		},
	});

	const heading = $derived(
		view === 'search'
			? t('calendar.search.label')
			: view === 'month' || view === 'list' || view === 'year'
				? monthTitle(anchor)
				: view === 'week'
					? weekTitle(weekStart(anchor))
					: dayTitle(anchor),
	);
	const viewOptions = $derived(
		compact
			? [
					{ value: 'month' as View, label: t('calendar.view.month') },
					{ value: 'list' as View, label: t('calendar.view.list') },
				]
			: [
					{ value: 'month' as View, label: t('calendar.view.month') },
					{ value: 'week' as View, label: t('calendar.view.week') },
					{ value: 'day' as View, label: t('calendar.view.day') },
					{ value: 'list' as View, label: t('calendar.view.list') },
				],
	);

	// The list from the chosen day: days with events or reminders, today and tomorrow always.
	const listDays = $derived.by(() => {
		const out: string[] = [];
		for (let i = 0; i < listLength; i++) {
			const d = addDays(range.first, i);
			if (byDay.has(d) || dueByDay.has(d) || d === today || d === addDays(today, 1) || i === 0)
				out.push(d);
		}
		return out;
	});
	const shownMonth = $derived(
		miniMonth && miniMonth.slice(0, 7) !== anchor.slice(0, 7) ? miniMonth : anchor,
	);
	const selectedDays = $derived(
		new Set(view === 'week' ? weekDays(anchor) : view === 'day' ? [anchor] : []),
	);
	const busyDay = (d: string) => (byDay.get(d)?.length ?? 0) > 0;
	const phoneDay = $derived(anchor);
	const allHidden = $derived(
		data.calendars.some((c) => c.hidden) &&
			!data.calendars.some((c) => !c.hidden && c.id !== BIRTHDAYS),
	);
	const title = $derived(
		compact || view === 'search' || view === 'year'
			? t('desktop.app.calendar')
			: t(`calendar.view.${view}` as MessageKey),
	);
	const sortCalendars = (ids: string[]) => setOrder('calendar', ids);
	const taskHandlers = {
		ontask: (task: DueTask) => (taskDialog = { task }),
		ontaskdone: (task: DueTask) => void tasks.toggleDone(task),
	};

	// ---- The KI-Agent's panel (1.13) ----
	const AGENT_KEY = 'titanos.calendar.agentPanel';
	const withAgent = $derived(!!session.services.agent);
	let agentOpen = $state(readAgentOpen());
	/** The panel stands as a column (room next to the content); else a sheet. */
	let agentColumn = $state(false);
	function readAgentOpen(): boolean {
		try {
			return localStorage.getItem(AGENT_KEY) === '1';
		} catch {
			return false;
		}
	}
	function setAgent(on: boolean) {
		agentOpen = on;
		try {
			if (on) localStorage.setItem(AGENT_KEY, '1');
			else localStorage.removeItem(AGENT_KEY);
		} catch {
			// Kept for this visit only.
		}
	}
	/** The event asked about from its details; gone with another day or view. */
	let agentEvent = $state<CalEvent | null>(null);
	$effect(() => {
		void anchor;
		void view;
		untrack(() => (agentEvent = null));
	});
	const agentContext = $derived<AgentContext>({
		app: 'calendar',
		date: compact && view === 'month' ? phoneDay : anchor,
		...(agentEvent && agentEvent.kind === 'event'
			? {
					event: {
						cal: agentEvent.calendar,
						id: agentEvent.id,
						...(agentEvent.occurrence ? { occurrence: agentEvent.occurrence } : {}),
					},
				}
			: {}),
	});
	const agentActions = $derived(calendarPanelActions((k) => t(k)));
	// 1.15: the agent in the ⋯ menu of the head (and the desktop's menu bar,
	// "Ansicht"; menus.ts names calendar.agent).
	registerMenuCommands({
		'calendar.agent': {
			run: () => setAgent(!agentOpen),
			enabled: () => withAgent,
			checked: () => agentOpen,
		},
	});
	// The ⋯ menu is the phone's head bar; a window of the desktop has its menu
	// bar. A narrow browser tab has neither: there the views' menu has it.
	const menuHasAgent = $derived(
		(phone || inWindow) &&
			entries(menusFor('app:calendar', session.data?.user?.role ?? 'user')).some(
				(e) => e.id === 'calendar.agent',
			),
	);
	function askAgent(ev: CalEvent) {
		detail = null;
		agentEvent = ev;
		setAgent(true);
	}

	// ---- Compact: the month → a day and back, with their movement (motion.ts) ----
	let glide: { to: 'day' | 'month'; rect: DOMRect; day: string } | null = null;
	/** The day came from the month (the way back goes there). */
	let fromMonth = false;
	function openDay(d: string, cell?: HTMLElement) {
		const week = cell?.closest('.mweek');
		glide = motionOn() && week ? { to: 'day', rect: week.getBoundingClientRect(), day: d } : null;
		fromMonth = view === 'month';
		void go('day', d, '', true);
	}
	function backToMonth() {
		const strip = document.querySelector('[data-strip] .days');
		const d = anchor;
		glide =
			motionOn() && strip ? { to: 'month', rect: strip.getBoundingClientRect(), day: d } : null;
		monthTarget = d;
		lastDay = d;
		jump++;
		if (fromMonth && history.length > 1) {
			fromMonth = false;
			history.back();
		} else void go('month', d);
	}
	function toToday() {
		lastDay = '';
		monthTarget = today;
		jump++;
		void go(view === 'year' || view === 'search' ? 'month' : view, today);
	}
	function showMonth(m: string) {
		lastDay = '';
		monthTarget = m;
		jump++;
		void go('month', m);
	}
	$effect(() => {
		const v = view;
		if (data.shown.kind !== 'ready' || !glide || glide.to !== (v === 'day' ? 'day' : 'month'))
			return;
		const g = glide;
		glide = null;
		untrack(() => {
			if (g.to === 'day')
				void tick().then(() => {
					glideFrom(document.querySelector('[data-strip] .days'), g.rect, 380);
					play(
						document.querySelector('.calview .week'),
						[
							{ transform: 'translateY(45%)', opacity: 0 },
							{ transform: 'none', opacity: 1 },
						],
						{ duration: 420 },
					);
					play(document.querySelector('.calview .dayline'), [{ opacity: 0 }, { opacity: 1 }], {
						duration: 380,
					});
				});
			else
				// The months scroll to the day first (MonthScroll), then its week glides home.
				requestAnimationFrame(() =>
					requestAnimationFrame(() => {
						const week = document
							.querySelector(`.calview .mweek [data-day="${g.day}"]`)
							?.closest('.mweek');
						glideFrom(week, g.rect, 380);
						play(document.querySelector('.calview .months'), [{ opacity: 0.3 }, { opacity: 1 }], {
							duration: 300,
						});
					}),
				);
		});
	});

	// Compact: the view is as high as what is left of the window (the pills float over it).
	let viewEl = $state<HTMLElement>();
	let viewTop = $state(0);
	function measure() {
		if (viewEl) viewTop = Math.max(0, Math.round(viewEl.getBoundingClientRect().top + scrollY));
	}
	$effect(() => {
		if (!compact || !viewEl) return;
		void level;
		void data.error;
		void moveError;
		void draftNote;
		void data.shown.kind;
		measure();
		addEventListener('resize', measure);
		return () => removeEventListener('resize', measure);
	});

	// ---- Drafts of the KI-Agent (?agentDraft=, also while the page is open) ----
	let draftSeen = '';
	let draftNote = $state('');
	let taskDraft = $state<AgentDraft | null>(null);
	let doneAsk = $state<{ task: Task; done: boolean } | null>(null);
	let doneBusy = $state(false);
	let doneError = $state('');
	$effect(() => {
		const param = page.url.searchParams.get('agentDraft') ?? '';
		if (!param || param === draftSeen) return;
		draftSeen = param;
		untrack(() => void takeDraft(param));
	});
	async function takeDraft(param: string) {
		draftNote = '';
		const d = await loadAgentDraft(param);
		// Out of the address: a reload or the way back does not open it again.
		const q = new URLSearchParams(page.url.search);
		q.delete('agentDraft');
		await goto(`/calendar${q.size ? '?' + q : ''}`, { replace: true, reset: false });
		draftSeen = '';
		if (d?.app === 'calendar' && d.kind === 'event') {
			openEventWindow(`/calendar/event?agentDraft=${encodeURIComponent(param)}&back=/calendar`);
			return;
		}
		if (d?.app !== 'tasks' || (d.kind !== 'task' && d.kind !== 'complete')) {
			draftNote = d ? t('agentpim.draft.foreign') : t('agentpim.draft.gone');
			return;
		}
		taskDraft = d;
	}
	// The dialog once the lists are there (its list is chosen from them).
	$effect(() => {
		const d = taskDraft;
		if (!d || !tasks.ready) return;
		untrack(() => {
			taskDraft = null;
			void showTaskDraft(d);
		});
	});
	async function showTaskDraft(d: AgentDraft) {
		const f = d.fields;
		const list = typeof f.list === 'string' ? f.list : '';
		const id = typeof f.id === 'string' ? f.id : '';
		if (d.kind === 'task') {
			detail = null;
			taskDialog = list && id ? { task: { list, id }, draft: f } : { draft: f };
			return;
		}
		if (!list || !id) {
			draftNote = t('agentpim.draft.gone');
			return;
		}
		try {
			doneError = '';
			doneAsk = { task: await loadTask(list, id), done: f.done !== false };
		} catch (err) {
			draftNote = apiErrorText(err);
		}
	}
	async function confirmDone() {
		if (!doneAsk) return;
		const { task, done } = doneAsk;
		doneBusy = true;
		try {
			await setTaskDone(task, done);
			doneAsk = null;
			announce(
				done
					? t('agentpim.draft.doneSaid', { title: task.title })
					: t('agentpim.draft.openSaid', { title: task.title }),
			);
			tasks.refresh();
		} catch (err) {
			doneError = apiErrorText(err);
		} finally {
			doneBusy = false;
		}
	}
</script>

<PageHead {title} />
<p class="sr-only" role="status" aria-live="polite">{said}</p>

{#snippet newButton(folded: boolean)}
	<button
		type="button"
		class="btn btn-primary new-event"
		class:icon={folded}
		disabled={data.shown.kind !== 'ready' || !own}
		title={folded ? t('calendar.newEvent') : undefined}
		aria-label={folded ? t('calendar.newEvent') : undefined}
		onclick={() => newEvent()}
		><Icon name="plus" size={16} />{#if !folded}{t('calendar.newEvent')}{/if}</button
	>
{/snippet}

{#snippet doneHead(label: string)}
	<div class="sheet-head">
		<h2>{label}</h2>
		<button type="button" class="link-btn done" onclick={up}>{t('calendar.done')}</button>
	</div>
{/snippet}

{#snippet side()}
	<div class="side" data-cal-side>
		{#if compact}{@render doneHead(t('calendar.phoneCalendars'))}{/if}
		{#if !compact}
			<MiniMonth
				month={shownMonth}
				{today}
				selected={selectedDays}
				busy={busyDay}
				onpick={(d) => {
					miniMonth = '';
					go(view === 'month' || view === 'search' ? 'day' : view, d);
				}}
				onmonth={(d) => (miniMonth = d)}
			/>
		{/if}
		{#if data.shown.kind === 'ready'}
			<CalendarList
				{calendars}
				phone={compact}
				adds={compact}
				account={session.data?.user?.name ?? ''}
				ontoggle={(c) => void data.toggle(c)}
				onedit={(c) => (calDialog = { calendar: c })}
				onnew={() => (calDialog = {})}
				onsort={compact ? undefined : sortCalendars}
				onall={compact ? (hide) => void data.setAllHidden(hide) : undefined}
			/>
			{#if !compact}
				<!-- The reminders below the calendars (Baustand step 11). -->
				<TaskSide
					data={tasks}
					{today}
					onopen={(task) => (taskDialog = { task })}
					onedit={(l) => (calDialog = { calendar: l, tasks: true })}
					onnewlist={() => (calDialog = { tasks: true })}
				/>
			{/if}
			<ReminderSettings phone={compact} />
		{/if}
	</div>
{/snippet}

{#snippet rail()}
	<button
		type="button"
		class="rail-btn"
		title={t('calendar.today')}
		aria-label={t('calendar.today')}
		onclick={() => go(view, today)}><Icon name="calendar" size={18} /></button
	>
{/snippet}

{#snippet foot(folded: boolean)}
	{#if !compact && withAgent}
		<button
			type="button"
			class="side-link agent-toggle"
			class:icon={folded}
			aria-pressed={agentOpen}
			title={folded ? t('agentpim.panel.toggle') : undefined}
			aria-label={folded ? t('agentpim.panel.toggle') : undefined}
			onclick={() => setAgent(!agentOpen)}
			><Icon name="sparkle" size={17} />{#if !folded}<span>{t('agentpim.panel.toggle')}</span
				>{/if}</button
		>
	{/if}
	{#if !compact}
		<button
			type="button"
			class="side-link"
			class:icon={folded}
			title={folded ? t('calendar.newCalendar') : undefined}
			aria-label={folded ? t('calendar.newCalendar') : undefined}
			disabled={data.shown.kind !== 'ready'}
			onclick={() => (calDialog = {})}
			><Icon name="plus" size={17} />{#if !folded}<span>{t('calendar.newCalendar')}</span
				>{/if}</button
		>
		<a
			class="side-link"
			class:icon={folded}
			href="/calendar/iphone"
			title={folded ? t('calendar.side.iphone') : undefined}
			aria-label={folded ? t('calendar.side.iphone') : undefined}
			><Icon name="device" size={17} />{#if !folded}<span>{t('calendar.side.iphone')}</span>{/if}</a
		>
	{/if}
{/snippet}

{#snippet bar()}
	<div class="cbar">
		<span class="nav">
			<button type="button" class="ib" aria-label={t('calendar.prev')} onclick={() => step(-1)}
				><Icon name="chevronLeft" size={18} /></button
			>
			<button
				type="button"
				class="btn today-btn"
				onclick={() => go(view === 'search' ? 'month' : view, today)}>{t('calendar.today')}</button
			>
			<button type="button" class="ib" aria-label={t('calendar.next')} onclick={() => step(1)}
				><Icon name="chevronRight" size={18} /></button
			>
		</span>
		<h2>{heading}</h2>
		<span class="views">
			<Segments
				compact
				label={t('calendar.views')}
				value={view === 'search' || view === 'year' ? 'month' : view}
				options={viewOptions}
				onchange={(v) => go(v, anchor)}
			/>
			<button
				type="button"
				class="ib"
				aria-pressed={view === 'search'}
				aria-label={t('calendar.search.label')}
				title={t('calendar.search.label')}
				onclick={() => (view === 'search' ? go('month', anchor) : go('search', anchor, '', true))}
				><Icon name="search" size={17} /></button
			>
		</span>
	</div>
{/snippet}

{#snippet notices()}
	{#if data.error}<p class="notice-error msg" role="alert">{data.error}</p>{/if}
	{#if moveError}<p class="notice-error msg" role="alert">{moveError}</p>{/if}
	{#if data.truncated}<p class="notice-warn msg" role="status">{t('calendar.truncated')}</p>{/if}
	{#if data.birthdaysUnavailable}<p class="muted msg" role="status">
			{t('calendar.birthdaysUnavailable')}
		</p>{/if}
	{#if allHidden}<p class="muted msg">{t('calendar.allHidden')}</p>{/if}
	{#if draftNote}
		<p class="muted msg draft-note" role="status">
			{draftNote}
			<button type="button" class="btn btn-sm" onclick={() => (draftNote = '')}
				>{t('common.close')}</button
			>
		</p>
	{/if}
{/snippet}

{#snippet taskLevel()}
	<div class="task-level">
		{@render doneHead(t('calendar.tasks.heading'))}
		<TaskSide
			data={tasks}
			{today}
			phone
			onopen={(task) => (taskDialog = { task })}
			onedit={(l) => (calDialog = { calendar: l, tasks: true })}
			onnewlist={() => (calDialog = { tasks: true })}
		/>
	</div>
{/snippet}

{#snippet viewItems(close: () => void)}
	{#each [['month', 'calendar.view.month'], ['day', 'calendar.view.day'], ['list', 'calendar.view.list'], ['year', 'calendar.view.year']] as const as [v, key] (v)}
		<button
			type="button"
			role="menuitemradio"
			aria-checked={view === v}
			onclick={() => {
				close();
				if (v === 'month') showMonth(anchor);
				else void go(v, v === 'list' ? (view === 'month' ? seenMonth || anchor : anchor) : anchor);
			}}>{t(key)}</button
		>
	{/each}
	{#if withAgent && !menuHasAgent}
		<button
			type="button"
			role="menuitemcheckbox"
			aria-checked={agentOpen}
			onclick={() => {
				close();
				setAgent(!agentOpen);
			}}>{t('agentpim.panel.toggle')}</button
		>
	{/if}
{/snippet}

<!-- Compact: the floating pills on top (drafts 36–39). -->
{#snippet topPills()}
	<div class="pills top">
		{#if view === 'month'}
			<button
				type="button"
				class="pill back-pill"
				aria-label={t('calendar.toYear', { year: yearText(seenMonth || anchor) })}
				onclick={() => go('year', seenMonth || anchor, '', true)}
				><Icon name="chevronLeft" size={20} /><span>{yearText(seenMonth || anchor)}</span></button
			>
		{:else if view === 'day' || view === 'list'}
			<button
				type="button"
				class="pill back-pill"
				aria-label={t('calendar.toMonth', { month: monthTitle(anchor) })}
				onclick={backToMonth}
				><Icon name="chevronLeft" size={20} /><span>{monthName(anchor)}</span></button
			>
		{:else}
			<span></span>
		{/if}
		<span class="pill icons">
			<Dropdown label={t('calendar.views')} icon="list" align="right">
				{#snippet children(close)}{@render viewItems(close)}{/snippet}
			</Dropdown>
			<button
				type="button"
				class="pi"
				aria-pressed={view === 'search'}
				aria-label={t('calendar.search.label')}
				title={t('calendar.search.label')}
				onclick={() => (view === 'search' ? up() : go('search', anchor, '', true))}
				><Icon name="search" size={22} /></button
			>
			<button
				type="button"
				class="pi"
				aria-label={t('calendar.newEvent')}
				title={t('calendar.newEvent')}
				disabled={!own}
				onclick={() => newEvent(view === 'month' || view === 'day' ? anchor : today)}
				><Icon name="plus" size={24} /></button
			>
		</span>
	</div>
{/snippet}

{#snippet bottomPills()}
	<div class="pills bottom" role="toolbar" aria-label={t('calendar.actions')}>
		<button type="button" class="pill today-pill" onclick={toToday}>{t('calendar.today')}</button>
		<span class="pill icons">
			<button
				type="button"
				class="pi"
				aria-label={t('calendar.phoneCalendars')}
				title={t('calendar.phoneCalendars')}
				onclick={() => go(view, anchor, 'cals')}><CalIcon name="month" size={24} /></button
			>
			<button
				type="button"
				class="pi"
				aria-label={t('calendar.tasks.heading')}
				title={t('calendar.tasks.heading')}
				onclick={() => go(view, anchor, 'tasks')}><CalIcon name="todo" size={24} /></button
			>
		</span>
	</div>
{/snippet}

<ColumnFrame
	storage="titanos.calendar"
	columns={!compact}
	{level}
	class={compact ? 'calendar levels' : 'calendar wide'}
	sideClass="col-cal-side"
	listClass="col-cal-tasks"
	contentClass="col-cal-main"
	sideWidth={{ min: 230, max: 400, fallback: 260 }}
	contentMin={560}
	foldBelow={view === 'week' ? 960 : 800}
	sideResizeLabel={t('calendar.resize.side')}
	action={compact ? undefined : newButton}
	{side}
	{rail}
	sideFoot={compact ? undefined : foot}
	bar={compact ? undefined : bar}
	notices={data.error ||
	moveError ||
	data.truncated ||
	data.birthdaysUnavailable ||
	allHidden ||
	draftNote
		? notices
		: undefined}
	list={compact && data.shown.kind === 'ready' ? taskLevel : undefined}
	aside={withAgent && agentOpen && !compact ? agentAside : undefined}
	asideKey="titanos.agentPanel.width"
	asideResizeLabel={t('agentpim.panel.resize')}
	bind:asideShown={agentColumn}
>
	<div
		class="calview"
		class:compact
		bind:this={viewEl}
		style:height={compact ? `calc(100dvh - ${viewTop}px)` : undefined}
		data-pim-shell="calendar"
		data-pim-state={data.shown.kind}
		data-pim-ready={data.shown.kind === 'ready' ? '' : undefined}
		data-view={view}
	>
		{#if data.shown.kind === 'loading'}
			<p class="muted state" role="status">{t('common.loading')}</p>
		{:else if data.shown.kind === 'unavailable'}
			<div class="notice state" role="status">
				<Icon name="disk" size={22} />
				<div>
					<p class="strong">{t('calendar.unavailable.title')}</p>
					<p>{t('calendar.unavailable.text')}</p>
				</div>
			</div>
		{:else if data.shown.kind === 'error'}
			<p class="notice-error state" role="alert">{apiErrorText(data.shown.error)}</p>
		{:else}
			{#if compact}{@render topPills()}{/if}
			{#if view === 'search'}
				<div class="list-scroll">
					<SearchPanel
						{today}
						{zone}
						{calFor}
						phone={compact}
						onopen={(ev) => (detail = ev)}
						onclose={compact ? undefined : () => go('month', anchor)}
					/>
				</div>
			{:else if view === 'year'}
				<div class="list-scroll">
					<YearView year={anchor} {today} onmonth={showMonth} onyear={(d) => go('year', d)} />
				</div>
			{:else if view === 'month' && compact}
				<MonthScroll
					target={monthTarget || anchor}
					{jump}
					{today}
					{byDay}
					{calFor}
					selected={lastDay}
					tasks={dueByDay}
					onopen={(ev) => (detail = ev)}
					onday={openDay}
					onvisible={(m) => (seenMonth = m)}
				/>
			{:else if view === 'month'}
				<MonthView
					month={anchor}
					{today}
					{byDay}
					{calFor}
					{canEdit}
					onopen={(ev) => (detail = ev)}
					onday={(d) => go('day', d)}
					onshift={shift}
					tasks={dueByDay}
					{...taskHandlers}
				/>
			{:else if view === 'week' || view === 'day'}
				{#if view === 'day'}
					<div class="dayhead">
						<DayStrip
							day={anchor}
							{today}
							busy={compact ? undefined : busyDay}
							onpick={(d) => go('day', d)}
						/>
						<p class="dayline">{dayLine(anchor)}</p>
					</div>
				{/if}
				<WeekView
					days={view === 'week' ? weekDays(anchor) : [anchor]}
					{today}
					{byDay}
					{calFor}
					now={nowMinutes}
					loaded={data.loaded}
					strip={view === 'day'}
					hour={compact ? 64 : 52}
					{canEdit}
					onopen={(ev) => (detail = ev)}
					onslot={(d, m, to) => own && newEvent(d, m, to)}
					onchange={timedChange}
					onshift={shift}
					onannounce={announce}
					onswipe={(dir) => go(view, addDays(anchor, view === 'week' ? 7 * dir : dir))}
					tasks={dueByDay}
					{...taskHandlers}
				/>
			{:else}
				<div class="list-scroll">
					<AgendaList
						days={listDays}
						{today}
						{byDay}
						{calFor}
						phone={compact}
						onopen={(ev) => (detail = ev)}
						tasks={dueByDay}
						{...taskHandlers}
					/>
					<!-- The API reads at most 400 days (with a day around): the list stops at 390. -->
					{#if listLength < 390}
						<div class="list-more">
							<button
								type="button"
								class="btn"
								onclick={() => (listLength = Math.min(390, listLength + 30))}
								>{t('calendar.moreDays')}</button
							>
						</div>
					{/if}
				</div>
			{/if}
			{#if compact}{@render bottomPills()}{/if}
		{/if}
	</div>
</ColumnFrame>

{#if detail}
	<EventDetail
		event={detail}
		{calFor}
		onclose={() => (detail = null)}
		onedit={edit}
		ondelete={(ev) => {
			detail = null;
			deleting = ev;
		}}
		onagent={withAgent ? askAgent : undefined}
	/>
{/if}
{#if deleting}
	<DeleteEventDialog
		event={deleting}
		onclose={() => (deleting = null)}
		ondone={() => {
			const cal = deleting?.calendar ?? '';
			deleting = null;
			void data.refresh([cal]);
		}}
	/>
{/if}
{#if calDialog}
	<CalendarDialog
		calendar={calDialog.calendar}
		tasks={calDialog.tasks}
		onclose={() => (calDialog = null)}
		onchanged={() => {
			if (calDialog?.tasks) tasks.refresh();
			else void data.reloadCalendars().then(() => data.reload());
		}}
	/>
{/if}
{#if taskDialog}
	<TaskDialog
		task={taskDialog.task}
		due={taskDialog.due}
		draft={taskDialog.draft}
		lists={tasks.lists}
		list={tasks.chosen}
		onclose={() => (taskDialog = null)}
		onchanged={() => tasks.refresh()}
	/>
{/if}
{#if doneAsk}
	{@const d = doneAsk}
	<Dialog
		title={d.done
			? t('agentpim.draft.doneTitle', { title: d.task.title })
			: t('agentpim.draft.openTitle', { title: d.task.title })}
		locked={doneBusy}
		onclose={() => (doneAsk = null)}
	>
		<p class="lead">{t('agentpim.draft.doneText')}</p>
		{#if doneError}<p class="notice-error" role="alert">{doneError}</p>{/if}
		<div class="dialog-actions">
			<button type="button" class="btn" disabled={doneBusy} onclick={() => (doneAsk = null)}
				>{t('common.cancel')}</button
			>
			<button
				type="button"
				class="btn btn-primary"
				disabled={doneBusy}
				data-done-confirm
				onclick={() => void confirmDone()}
				><Icon name="check" size={15} />{d.done
					? t('agentpim.draft.doneConfirm')
					: t('agentpim.draft.openConfirm')}</button
			>
		</div>
	</Dialog>
{/if}
{#snippet agentAside()}
	<AgentPanel
		app="calendar"
		context={agentContext}
		actions={agentActions}
		onclose={() => setAgent(false)}
	/>
{/snippet}
{#if withAgent && agentOpen && !(!compact && agentColumn)}
	<AgentSheet label={t('agentpim.panel.title')} onclose={() => setAgent(false)}>
		<AgentPanel
			app="calendar"
			phone
			context={agentContext}
			actions={agentActions}
			onclose={() => setAgent(false)}
		/>
	</AgentSheet>
{/if}
{#if moving}
	{@const m = moving}
	<Dialog
		title={t('calendar.drag.seriesTitle', { title: eventTitle(m.ev) })}
		onclose={() => (moving = null)}
	>
		<p class="lead">{t('calendar.drag.seriesText', { when: m.what })}</p>
		<ScopeChoices bind:value={moveScope} mode="save" label={t('calendar.save.which')} />
		<div class="dialog-actions">
			<button type="button" class="btn" onclick={() => (moving = null)}>{t('common.cancel')}</button
			>
			<button type="button" class="btn btn-primary" onclick={() => void save(m, moveScope)}
				>{t('calendar.drag.move')}</button
			>
		</div>
	</Dialog>
{/if}

<style>
	/* 1.15: Kalender's own tokens. Today's red: white on --cal-today
	   reaches 5.4:1; --cal-today-text reaches 4.5:1 on the theme's
	   background (darker on light themes, lighter on dark ones). */
	:global(.cf.calendar),
	:global(.calview) {
		--cal-today: #d70015;
		--cal-on-today: #fff;
		--cal-today-text: #c00012;
		--cal-pill-shadow: 0 2px 10px rgb(0 0 0 / 0.18);
	}
	:global([data-mode='dark'] .cf.calendar),
	:global([data-mode='dark'] .calview) {
		--cal-today-text: #ff6b60;
		--cal-pill-shadow: 0 2px 10px rgb(0 0 0 / 0.45);
	}
	.calview {
		position: relative;
		display: flex;
		flex-direction: column;
		height: 100%;
		min-height: 0;
	}
	.calview.compact {
		overflow: hidden;
	}
	/* Compact, the view takes the rest of the window and scrolls inside:
	   the page around it does not (local to Kalender; ColumnFrame does the
	   same for its columns). */
	:global(body main:has(.calview.compact):has(.cf.levels.calendar)) {
		min-height: 0;
		padding-bottom: 0;
	}
	.state {
		margin: var(--space-4);
	}
	.notice {
		display: flex;
		gap: var(--space-3);
		align-items: flex-start;
	}
	.notice p {
		margin: 0 0 var(--space-1);
	}
	.strong {
		font-weight: 600;
	}
	.side {
		position: relative;
		display: flex;
		flex-direction: column;
		gap: var(--space-4);
		padding: 0 var(--space-1-5) var(--space-2);
		min-width: 0;
	}
	.new-event {
		justify-content: center;
		min-height: 36px;
	}
	.new-event.icon {
		width: 44px;
		padding: 0;
	}
	.rail-btn,
	:global(.calendar) .side-link {
		display: flex;
		align-items: center;
		gap: var(--space-2-5);
		min-height: 36px;
		padding: var(--space-1-5) var(--space-2-5);
		border: 0;
		border-radius: calc(9px * var(--radius-scale, 1));
		background: none;
		color: var(--muted);
		font: inherit;
		text-align: left;
		text-decoration: none;
		cursor: pointer;
	}
	.rail-btn {
		justify-content: center;
		width: 44px;
		padding: 0;
	}
	:global(.calendar) .side-link.icon {
		justify-content: center;
		width: 44px;
		padding: 0;
	}
	.rail-btn:hover,
	:global(.calendar) .side-link:hover {
		background: color-mix(in srgb, var(--text) 7%, transparent);
		color: var(--text);
	}
	:global(.calendar) .agent-toggle[aria-pressed='true'] {
		background: color-mix(in srgb, var(--accent) 12%, transparent);
		color: var(--text);
	}
	.draft-note {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-2);
	}
	.cbar {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-2) var(--space-3);
		flex: 1;
		min-width: 0;
	}
	.nav,
	.views {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-1);
	}
	.ib {
		display: inline-grid;
		place-items: center;
		min-width: 34px;
		min-height: 34px;
		padding: 0;
		border: 1px solid var(--border);
		border-radius: var(--radius-ctl);
		background: none;
		color: var(--text);
		cursor: pointer;
	}
	.ib[aria-pressed='true'] {
		background: color-mix(in srgb, var(--accent) 14%, transparent);
	}
	.ib:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: 1px;
	}
	.cbar h2 {
		flex: 1 1 auto;
		margin: 0;
		font-size: var(--text-lg);
		font-weight: 600;
		overflow-wrap: break-word;
	}
	.msg {
		margin: var(--space-2) var(--space-3);
	}
	.list-scroll {
		position: relative;
		flex: 1 1 auto;
		min-height: 0;
		overflow-y: auto;
	}
	.compact .list-scroll {
		padding: 0 var(--space-3) calc(80px + env(safe-area-inset-bottom, 0px));
		overscroll-behavior: contain;
	}
	.list-more {
		padding: 0 var(--space-3) var(--space-3);
	}
	.list-more .btn {
		min-height: 44px;
	}
	.task-level {
		display: flex;
		flex-direction: column;
		gap: var(--space-3);
		padding: var(--space-3);
	}
	:global(.calendar.levels) .side {
		padding: var(--space-3);
	}

	/* The day: week strip and its date (draft 38). */
	.dayhead {
		flex: none;
		padding-top: var(--space-1);
		border-bottom: 1px solid var(--border);
	}
	.dayline {
		margin: 0;
		padding: var(--space-2) var(--space-3);
		border-top: 1px solid var(--border);
		text-align: center;
		font-size: var(--text-md);
		font-weight: 600;
		overflow-wrap: break-word;
	}
	.calview:not(.compact) .dayhead {
		padding-inline: var(--space-2);
	}

	/* ---- Compact: floating opaque pills (drafts 36–39; never glass) ---- */
	.pills {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: var(--space-2);
		padding: var(--space-2) calc(var(--space-3) + env(safe-area-inset-right, 0px)) var(--space-2)
			calc(var(--space-3) + env(safe-area-inset-left, 0px));
		flex-wrap: wrap;
	}
	.pills.top {
		flex: none;
	}
	.pills.bottom {
		position: absolute;
		left: 0;
		right: 0;
		bottom: 0;
		z-index: 5;
		padding-bottom: calc(var(--space-3) + env(safe-area-inset-bottom, 0px));
		pointer-events: none;
	}
	.pills.bottom > * {
		pointer-events: auto;
	}
	.pill {
		display: inline-flex;
		align-items: center;
		gap: 2px;
		min-height: 44px;
		padding: 0 var(--space-3);
		border: 1px solid var(--border);
		border-radius: 22px;
		background: var(--surface);
		color: var(--text);
		font: inherit;
		font-size: var(--text-md);
		box-shadow: var(--cal-pill-shadow);
		cursor: pointer;
	}
	.pill span {
		overflow-wrap: anywhere;
	}
	.back-pill {
		padding-left: var(--space-2);
	}
	.today-pill {
		font-weight: 600;
		padding-inline: var(--space-4);
	}
	.pill.icons {
		gap: var(--space-1);
		padding: 0 var(--space-1);
		cursor: default;
	}
	.pi,
	.pill.icons :global(.dropdown > .btn) {
		display: inline-grid;
		place-items: center;
		min-width: 44px;
		min-height: 44px;
		padding: 0;
		border: 0;
		border-radius: 22px;
		background: none;
		color: var(--text);
		cursor: pointer;
	}
	.pill.icons :global(.dropdown > .btn svg) {
		width: 22px;
		height: 22px;
	}
	.pi:disabled {
		opacity: 0.5;
		cursor: default;
	}
	.pi[aria-pressed='true'] {
		background: color-mix(in srgb, var(--accent) 16%, transparent);
	}
	.pill:focus-visible,
	.pi:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: 1px;
	}
	/* Room under the hours for the pills at the bottom. */
	.compact :global(.wk-grid) {
		margin-bottom: calc(80px + env(safe-area-inset-bottom, 0px));
	}

	/* The levels "Kalender" and "Erinnerungen" as sheets (draft 34). */
	.sheet-head {
		display: grid;
		grid-template-columns: 1fr auto 1fr;
		align-items: center;
		gap: var(--space-2);
	}
	.sheet-head h2 {
		grid-column: 2;
		margin: 0;
		font-size: var(--text-md);
		font-weight: 700;
		text-align: center;
		overflow-wrap: anywhere;
	}
	.link-btn {
		justify-self: end;
		min-height: 44px;
		padding: 0 var(--space-1);
		border: 0;
		background: none;
		color: var(--accent);
		font: inherit;
		font-size: var(--text-md);
		font-weight: 600;
		cursor: pointer;
	}
</style>
