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
	import { onDestroy, onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { ApiError } from '#lib/api.ts';
	import BottomBar from '#lib/components/BottomBar.svelte';
	import ColumnFrame, { type FrameLevel } from '#lib/components/ColumnFrame.svelte';
	import { compactView } from '#lib/components/compact.svelte.ts';
	import Dialog from '#lib/components/Dialog.svelte';
	import Icon from '#lib/components/Icon.svelte';
	import PageHead from '#lib/components/PageHead.svelte';
	import Segments from '#lib/components/Segments.svelte';
	import { phoneView } from '#lib/desktop/mode.svelte.ts';
	import { apiErrorText, t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import { registerMenuCommands } from '#lib/menu-commands.svelte.ts';
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
	import { BIRTHDAYS, eventsByDay, type CalEvent, type Calendar } from '#lib/pim/calendar.ts';
	import {
		eventHref,
		onChange,
		openEventWindow,
		updateEvent,
		type Scope,
	} from '#lib/pim/calendar-api.ts';
	import {
		dayTitle,
		eventTitle,
		longDay,
		monthTitle,
		weekTitle,
		whenText,
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
	import { TaskData } from '#lib/pim/cal/task-data.svelte.ts';
	import TaskDialog from '#lib/pim/cal/TaskDialog.svelte';
	import TaskSide from '#lib/pim/cal/TaskSide.svelte';
	import { tasksByDay, type DueTask, type TaskList } from '#lib/pim/cal/tasks.ts';
	import WeekView, { type TimedChange } from '#lib/pim/cal/WeekView.svelte';

	type View = 'month' | 'week' | 'day' | 'list';

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
	const view = $derived.by((): View => {
		const v = page.url.searchParams.get('view');
		if (compact) return v === 'list' ? 'list' : 'month';
		return v === 'week' || v === 'day' || v === 'list' ? v : 'month';
	});
	const anchor = $derived.by(() => {
		const d = page.url.searchParams.get('date') ?? '';
		return validDate(d) ? d : today;
	});
	const level = $derived.by((): FrameLevel => {
		const l = page.url.searchParams.get('level');
		return compact && l === 'cals' ? 'side' : compact && l === 'tasks' ? 'list' : 'content';
	});
	function go(v: View, date: string, lvl = '') {
		const q = new URLSearchParams();
		q.set('view', v);
		if (date !== today) q.set('date', date);
		if (lvl) q.set('level', lvl);
		void goto(`/calendar?${q}`, { replace: !lvl, reset: false });
	}
	function up() {
		if (history.length > 1) history.back();
		else go(view, anchor);
	}

	let listLength = $state(30);
	let miniMonth = $state('');
	let picked = $state(''); // the chosen day of the phone's month
	let detail = $state<CalEvent | null>(null);
	let deleting = $state<CalEvent | null>(null);
	let calDialog = $state<{ calendar?: Calendar | TaskList; tasks?: boolean } | null>(null);
	let taskDialog = $state<{ task?: DueTask; due?: string } | null>(null);
	let said = $state('');
	const announce = (text: string) => (said = said === text ? text + ' ' : text);

	// The calendars in the account's order (the birthdays too).
	const calendars = $derived(applyOrder(data.calendars, orderOf('calendar'), (c) => c.id));
	const byId = $derived(new Map(data.calendars.map((c) => [c.id, c])));
	const calFor = (id: string) => byId.get(id);
	const own = $derived(data.calendars.some((c) => c.kind === 'own' && !c.readOnly));

	/** The days the view shows. */
	const range = $derived.by((): { first: string; last: string } => {
		if (view === 'month') {
			const g = monthGrid(anchor);
			return { first: g[0][0], last: g[g.length - 1][6] };
		}
		if (view === 'week') return { first: weekStart(anchor), last: addDays(weekStart(anchor), 6) };
		if (view === 'day') return { first: anchor, last: anchor };
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
	function newEvent(date = view === 'list' ? today : anchor, minutes?: number, to?: number) {
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
		view === 'month' || view === 'list'
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
	const phoneDay = $derived(picked && picked.slice(0, 7) === anchor.slice(0, 7) ? picked : anchor);
	const allHidden = $derived(
		data.calendars.some((c) => c.hidden) &&
			!data.calendars.some((c) => !c.hidden && c.id !== BIRTHDAYS),
	);
	const title = $derived(
		compact ? t('desktop.app.calendar') : t(`calendar.view.${view}` as MessageKey),
	);
	const sortCalendars = (ids: string[]) => setOrder('calendar', ids);
	const taskHandlers = {
		ontask: (task: DueTask) => (taskDialog = { task }),
		ontaskdone: (task: DueTask) => void tasks.toggleDone(task),
	};
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

{#snippet backButton(label: string)}
	<button type="button" class="btn back" onclick={up}
		><Icon name="chevronLeft" size={16} />{label}</button
	>
{/snippet}

{#snippet side()}
	<div class="side" data-cal-side>
		{#if compact}{@render backButton(t('calendar.back'))}{/if}
		{#if !compact}
			<MiniMonth
				month={shownMonth}
				{today}
				selected={selectedDays}
				busy={busyDay}
				onpick={(d) => {
					miniMonth = '';
					go(view === 'month' ? 'day' : view, d);
				}}
				onmonth={(d) => (miniMonth = d)}
			/>
		{/if}
		{#if data.shown.kind === 'ready'}
			<CalendarList
				{calendars}
				phone={compact}
				adds={compact}
				ontoggle={(c) => void data.toggle(c)}
				onedit={(c) => (calDialog = { calendar: c })}
				onnew={() => (calDialog = {})}
				onsort={compact ? undefined : sortCalendars}
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
	<div class="cbar" class:compact>
		<span class="nav">
			<button type="button" class="ib" aria-label={t('calendar.prev')} onclick={() => step(-1)}
				><Icon name="chevronLeft" size={18} /></button
			>
			{#if !compact}
				<button type="button" class="btn today-btn" onclick={() => go(view, today)}
					>{t('calendar.today')}</button
				>
			{/if}
			<button type="button" class="ib" aria-label={t('calendar.next')} onclick={() => step(1)}
				><Icon name="chevronRight" size={18} /></button
			>
		</span>
		<h2>{heading}</h2>
		<span class="views">
			<Segments
				compact
				label={t('calendar.views')}
				value={view}
				options={viewOptions}
				onchange={(v) => go(v, anchor)}
			/>
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
{/snippet}

{#snippet taskLevel()}
	<div class="task-level">
		{@render backButton(t('calendar.back'))}
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
	foldBelow={800}
	sideResizeLabel={t('calendar.resize.side')}
	action={compact ? undefined : newButton}
	{side}
	{rail}
	sideFoot={compact ? undefined : foot}
	bar={!compact || (data.shown.kind === 'ready' && level === 'content') ? bar : undefined}
	notices={data.error || moveError || data.truncated || data.birthdaysUnavailable || allHidden
		? notices
		: undefined}
	list={compact && data.shown.kind === 'ready' ? taskLevel : undefined}
>
	<div
		class="calview"
		class:compact
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
		{:else if view === 'month'}
			<MonthView
				month={anchor}
				{today}
				{byDay}
				{calFor}
				phone={compact}
				selected={phoneDay}
				{canEdit}
				onopen={(ev) => (detail = ev)}
				onday={(d) => (compact ? (picked = d) : go('day', d))}
				onshift={compact ? undefined : shift}
				tasks={dueByDay}
				{...taskHandlers}
			/>
			{#if compact}
				<div class="phone-day">
					<AgendaList
						days={[phoneDay]}
						{today}
						{byDay}
						{calFor}
						phone
						onopen={(ev) => (detail = ev)}
						tasks={dueByDay}
						{...taskHandlers}
					/>
				</div>
			{/if}
		{:else if view === 'week' || view === 'day'}
			<WeekView
				days={view === 'week' ? weekDays(anchor) : [anchor]}
				{today}
				{byDay}
				{calFor}
				now={nowMinutes}
				loaded={data.loaded}
				{canEdit}
				onopen={(ev) => (detail = ev)}
				onslot={(d, m, to) => own && newEvent(d, m, to)}
				onchange={timedChange}
				onshift={shift}
				onannounce={announce}
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
	</div>
	{#if compact && level === 'content'}
		<BottomBar label={t('calendar.actions')}>
			<button
				type="button"
				class="bar-btn"
				title={t('calendar.today')}
				onclick={() => {
					picked = '';
					go(view, today);
				}}>{t('calendar.today')}</button
			>
			<button
				type="button"
				class="bar-btn"
				aria-label={t('calendar.phoneCalendars')}
				title={t('calendar.phoneCalendars')}
				disabled={data.shown.kind !== 'ready'}
				onclick={() => go(view, anchor, 'cals')}><Icon name="calendar" size={20} /></button
			>
			<button
				type="button"
				class="bar-btn"
				aria-label={t('calendar.tasks.heading')}
				title={t('calendar.tasks.heading')}
				disabled={data.shown.kind !== 'ready'}
				onclick={() => go(view, anchor, 'tasks')}><Icon name="check" size={20} /></button
			>
			<button
				type="button"
				class="bar-btn bar-new"
				aria-label={t('calendar.newEvent')}
				title={t('calendar.newEvent')}
				disabled={data.shown.kind !== 'ready' || !own}
				onclick={() => newEvent(view === 'month' ? phoneDay : today)}
				><Icon name="plus" size={20} /></button
			>
		</BottomBar>
	{/if}
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
		lists={tasks.lists}
		list={tasks.chosen}
		onclose={() => (taskDialog = null)}
		onchanged={() => tasks.refresh()}
	/>
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
	.calview {
		position: relative;
		display: flex;
		flex-direction: column;
		height: 100%;
		min-height: 0;
	}
	.calview.compact {
		height: auto;
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
	.cbar {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-2) var(--space-3);
		flex: 1;
		min-width: 0;
	}
	.nav {
		display: flex;
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
	.compact .ib {
		min-width: 44px;
		min-height: 44px;
		border-color: transparent;
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
	.cbar.compact h2 {
		order: -1;
		flex-basis: 100%;
		font-size: var(--text-xl);
	}
	.compact .views :global(.segments button) {
		min-height: 40px;
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
		overflow: visible;
		padding: 0 var(--space-3);
	}
	.list-more {
		padding: 0 var(--space-3) var(--space-3);
	}
	.list-more .btn {
		min-height: 44px;
	}
	.phone-day {
		padding: var(--space-3);
		border-top: 1px solid var(--border);
	}
	.back {
		align-self: flex-start;
		display: inline-flex;
		align-items: center;
		gap: var(--space-1);
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
</style>
