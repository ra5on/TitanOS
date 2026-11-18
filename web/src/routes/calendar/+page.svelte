<script lang="ts">
	// 1.11: T-Kalender (docs/KALENDER.md, drafts 01–04, 07, 40–42). Asks
	// titand first whether the calendars can be read (GET /calendar/access):
	// without the data volume the page says "nicht verfügbar", never an
	// empty grid. Then the column (mini month, calendars with color and
	// name), the bar (Heute, back/forward, Monat · Woche · Tag · Liste in
	// `?view=`, the day in `?date=`) and the view. An event opens its
	// details; "Neuer Termin" and "Bearbeiten" open /calendar/event in a
	// window of its own (a page on the phone), which tells this page when it
	// saved. The phone has the list (with the week on top) and the month.
	import { onDestroy } from 'svelte';
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { api } from '#lib/api.ts';
	import Icon from '#lib/components/Icon.svelte';
	import PageHead from '#lib/components/PageHead.svelte';
	import Segments from '#lib/components/Segments.svelte';
	import { phoneView } from '#lib/desktop/mode.svelte.ts';
	import { apiErrorText, t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import { registerMenuCommands } from '#lib/menu-commands.svelte.ts';
	import {
		addDays,
		addMonths,
		clock,
		isoDate,
		localZone,
		monthGrid,
		validDate,
		wallOf,
		weekDays,
		weekStart,
	} from '#lib/pim/caldates.ts';
	import { BIRTHDAYS, eventsByDay, type CalEvent, type Calendar } from '#lib/pim/calendar.ts';
	import {
		eventHref,
		loadCalendars,
		loadEvent,
		loadEvents,
		onChange,
		openEventWindow,
		setHidden,
	} from '#lib/pim/calendar-api.ts';
	import { dayTitle, monthTitle, weekTitle, weekdayOf } from '#lib/pim/calendar-text.ts';
	import AgendaList from '#lib/pim/cal/AgendaList.svelte';
	import CalendarDialog from '#lib/pim/cal/CalendarDialog.svelte';
	import CalendarList from '#lib/pim/cal/CalendarList.svelte';
	import DeleteEventDialog from '#lib/pim/cal/DeleteEventDialog.svelte';
	import EventDetail from '#lib/pim/cal/EventDetail.svelte';
	import MiniMonth from '#lib/pim/cal/MiniMonth.svelte';
	import MonthView from '#lib/pim/cal/MonthView.svelte';
	import WeekView from '#lib/pim/cal/WeekView.svelte';
	import ProgramShell from '#lib/pim/ProgramShell.svelte';
	import { calendarShell, shellOfError, type ShellState } from '#lib/pim/shell.ts';

	type View = 'month' | 'week' | 'day' | 'list';

	const phone = $derived(phoneView());
	const zone = localZone();
	let clockNow = $state(new Date());
	const timer = setInterval(() => (clockNow = new Date()), 30_000);
	onDestroy(() => clearInterval(timer));
	const today = $derived(wallOf(clockNow, zone).date);
	const nowMinutes = $derived(wallOf(clockNow, zone).minutes);

	// The view and the day live in the address (the side bar's "Liste" is ?view=list).
	const view = $derived.by((): View => {
		const v = page.url.searchParams.get('view');
		if (phone) return v === 'month' ? 'month' : 'list';
		return v === 'week' || v === 'day' || v === 'list' ? v : 'month';
	});
	const anchor = $derived.by(() => {
		const d = page.url.searchParams.get('date') ?? '';
		return validDate(d) ? d : today;
	});
	function go(v: View, date: string) {
		const q = new URLSearchParams();
		q.set('view', v);
		if (date !== today) q.set('date', date);
		void goto(`/calendar?${q}`, { replace: true, reset: false });
	}

	let shown = $state<ShellState>({ kind: 'loading' });
	let calendars = $state<Calendar[]>([]);
	let events = $state<CalEvent[]>([]);
	let truncated = $state(false);
	let birthdaysUnavailable = $state(false);
	let loadError = $state('');
	let listLength = $state(30);
	let miniMonth = $state('');
	let picked = $state(''); // the chosen day of the phone's month

	let detail = $state<CalEvent | null>(null);
	let deleting = $state<CalEvent | null>(null);
	let calDialog = $state<{ calendar?: Calendar } | null>(null);

	const byId = $derived(new Map(calendars.map((c) => [c.id, c])));
	const calFor = (id: string) => byId.get(id);

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
	const byDay = $derived(eventsByDay(events, zone, range.first, range.last));

	async function start() {
		try {
			const access = await api.get<unknown>('/calendar/access');
			const state = calendarShell(access);
			if (state.kind !== 'ready') {
				shown = state;
				return;
			}
			calendars = await loadCalendars();
			shown = { kind: 'ready' };
		} catch (err) {
			shown = shellOfError(err);
		}
	}
	$effect(() => {
		void start();
	});

	// A request per shown range; an older answer never replaces a newer one.
	let asked = 0;
	async function reload() {
		if (shown.kind !== 'ready') return;
		const mine = ++asked;
		const { first, last } = range;
		try {
			// One day around: titand reads dates in the NAS zone, the views use the browser's.
			const got = await loadEvents(addDays(first, -1), addDays(last, 2));
			if (mine !== asked) return;
			events = got.events;
			truncated = got.truncated;
			birthdaysUnavailable = got.birthdaysUnavailable;
			loadError = '';
			void fillClipped(mine);
		} catch (err) {
			if (mine !== asked) return;
			const s = shellOfError(err);
			if (s.kind === 'unavailable') shown = s;
			else loadError = apiErrorText(err);
		}
	}
	$effect(() => {
		void range.first;
		void range.last;
		if (shown.kind === 'ready') void reload();
	});

	// Titles and places longer than the list keeps (clipped): the whole
	// event once, so no text is cut.
	async function fillClipped(mine: number) {
		const cut = events.filter((e) => e.clipped).slice(0, 40);
		for (const e of cut) {
			try {
				const full = await loadEvent(e.calendar, e.id, e.occurrence);
				if (mine !== asked) return;
				events = events.map((x) =>
					x === e ? { ...x, title: full.title, location: full.location, clipped: false } : x,
				);
			} catch {
				// Stays as the list had it.
			}
		}
	}

	async function refreshAll() {
		try {
			calendars = await loadCalendars();
		} catch (err) {
			const s = shellOfError(err);
			if (s.kind === 'unavailable') shown = s;
			return;
		}
		await reload();
	}
	const stop = onChange(() => void refreshAll());
	onDestroy(stop);

	async function toggle(c: Calendar) {
		const hidden = calendars.filter((x) => (x.id === c.id ? !x.hidden : x.hidden)).map((x) => x.id);
		calendars = calendars.map((x) => (x.id === c.id ? { ...x, hidden: !x.hidden } : x));
		try {
			await setHidden(hidden);
		} catch (err) {
			loadError = apiErrorText(err);
		}
		await refreshAll();
	}

	function step(dir: -1 | 1) {
		if (view === 'month') go(view, addMonths(anchor, dir));
		else if (view === 'week') go(view, addDays(anchor, 7 * dir));
		else if (view === 'day') go(view, addDays(anchor, dir));
		else go(view, addDays(anchor, 7 * dir));
	}

	const back = $derived(page.url.pathname + page.url.search);
	function newEvent(date = view === 'list' ? today : anchor, minutes?: number) {
		const time =
			minutes !== undefined
				? clock(minutes)
				: date === today
					? clock(Math.min(23 * 60, (Math.floor(nowMinutes / 60) + 1) * 60))
					: '09:00';
		openEventWindow(eventHref({ date, time, back }));
	}
	function edit(ev: CalEvent) {
		detail = null;
		openEventWindow(eventHref({ cal: ev.calendar, id: ev.id, occurrence: ev.occurrence, back }));
	}

	registerMenuCommands({
		'calendar.newEvent': {
			run: () => newEvent(),
			enabled: () => shown.kind === 'ready' && calendars.some((c) => c.kind === 'own'),
		},
		'calendar.newCalendar': {
			run: () => (calDialog = {}),
			enabled: () => shown.kind === 'ready',
		},
	});

	const heading = $derived(
		view === 'month'
			? monthTitle(anchor)
			: view === 'week'
				? weekTitle(weekStart(anchor))
				: view === 'day'
					? dayTitle(anchor)
					: monthTitle(anchor),
	);
	const viewOptions = $derived(
		phone
			? [
					{ value: 'list' as View, label: t('calendar.view.list') },
					{ value: 'month' as View, label: t('calendar.view.month') },
				]
			: [
					{ value: 'month' as View, label: t('calendar.view.month') },
					{ value: 'week' as View, label: t('calendar.view.week') },
					{ value: 'day' as View, label: t('calendar.view.day') },
					{ value: 'list' as View, label: t('calendar.view.list') },
				],
	);

	// The list from the chosen day: days with events, today and tomorrow always.
	const listDays = $derived.by(() => {
		const out: string[] = [];
		for (let i = 0; i < listLength; i++) {
			const d = addDays(range.first, i);
			if (byDay.has(d) || d === today || d === addDays(today, 1) || i === 0) out.push(d);
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
	const hiddenNames = $derived(calendars.filter((c) => c.hidden).length);
</script>

<!-- In a window the title bar names the view ("T-Kalender › Monat"); the phone's bar says it itself. -->
<PageHead
	title={phone && view === 'list'
		? t('calendar.view.agenda')
		: t(`calendar.view.${view}` as MessageKey)}
	path={phone ? undefined : []}
>
	{#snippet actions()}
		<button
			type="button"
			class="btn btn-primary"
			disabled={shown.kind !== 'ready' || !calendars.some((c) => c.kind === 'own')}
			onclick={() => newEvent()}><Icon name="plus" size={16} />{t('calendar.newEvent')}</button
		>
	{/snippet}
</PageHead>

<ProgramShell program="calendar" view={shown} own>
	<div class="cal" class:phone data-pim-ready data-view={view}>
		{#if !phone}
			<aside class="side">
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
				<CalendarList
					{calendars}
					ontoggle={toggle}
					onedit={(c) => (calDialog = { calendar: c })}
					onnew={() => (calDialog = {})}
				/>
			</aside>
		{/if}
		<section class="main">
			<div class="cbar">
				<span class="nav">
					<button type="button" class="ib" aria-label={t('calendar.prev')} onclick={() => step(-1)}
						><Icon name="chevronLeft" size={18} /></button
					>
					<button type="button" class="btn today-btn" onclick={() => go(view, today)}
						>{t('calendar.today')}</button
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
						value={view}
						options={viewOptions}
						onchange={(v) => go(v, anchor)}
					/>
				</span>
			</div>

			{#if loadError}<p class="notice-error msg" role="alert">{loadError}</p>{/if}
			{#if truncated}<p class="notice-warn msg" role="status">{t('calendar.truncated')}</p>{/if}
			{#if birthdaysUnavailable}<p class="muted msg" role="status">
					{t('calendar.birthdaysUnavailable')}
				</p>{/if}

			{#if view === 'month'}
				<MonthView
					month={anchor}
					{today}
					{byDay}
					{calFor}
					{phone}
					selected={phoneDay}
					onopen={(ev) => (detail = ev)}
					onday={(d) => (phone ? (picked = d) : go('day', d))}
				/>
				{#if phone}
					<AgendaList
						days={[phoneDay]}
						{today}
						{byDay}
						{calFor}
						phone
						onopen={(ev) => (detail = ev)}
					/>
				{/if}
			{:else if view === 'week' || view === 'day'}
				<WeekView
					days={view === 'week' ? weekDays(anchor) : [anchor]}
					{today}
					{byDay}
					{calFor}
					now={nowMinutes}
					onopen={(ev) => (detail = ev)}
					onslot={(d, m) => newEvent(d, m)}
				/>
			{:else}
				{#if phone}
					<div class="strip" role="group" aria-label={t('calendar.thisWeek')}>
						{#each weekDays(anchor) as d (d)}
							<button
								type="button"
								class="sd"
								class:today={d === today}
								class:sel={d === anchor}
								aria-pressed={d === anchor}
								onclick={() => go('list', d)}
								><small>{weekdayOf(d)}</small><b>{Number(d.slice(8))}</b><i
									class:has={busyDay(d)}
									aria-hidden="true"
								></i>{#if d === today}<span class="sr-only">{t('calendar.today')}</span
									>{/if}</button
							>
						{/each}
					</div>
				{/if}
				<AgendaList
					days={listDays}
					{today}
					{byDay}
					{calFor}
					{phone}
					onopen={(ev) => (detail = ev)}
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
			{/if}

			{#if phone}
				<div class="phone-cals">
					<CalendarList
						{calendars}
						phone
						ontoggle={toggle}
						onedit={(c) => (calDialog = { calendar: c })}
						onnew={() => (calDialog = {})}
					/>
				</div>
			{/if}
			{#if hiddenNames && !calendars.some((c) => !c.hidden && c.id !== BIRTHDAYS)}
				<p class="muted msg">{t('calendar.allHidden')}</p>
			{/if}
		</section>
	</div>
</ProgramShell>

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
			deleting = null;
			void reload();
		}}
	/>
{/if}
{#if calDialog}
	<CalendarDialog
		calendar={calDialog.calendar}
		onclose={() => (calDialog = null)}
		onchanged={() => void refreshAll()}
	/>
{/if}

<style>
	.cal {
		display: grid;
		grid-template-columns: minmax(220px, 250px) minmax(0, 1fr);
		border: 1px solid var(--border);
		border-radius: var(--radius-tile);
		background: var(--surface);
		min-height: 0;
	}
	.cal.phone {
		display: block;
		border: 0;
		background: none;
	}
	.side {
		display: flex;
		flex-direction: column;
		gap: var(--space-4);
		min-width: 0;
		padding: var(--space-3);
		border-right: 1px solid var(--border);
	}
	.main {
		min-width: 0;
		display: flex;
		flex-direction: column;
	}
	.cbar {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-2) var(--space-3);
		padding: var(--space-3);
		border-bottom: 1px solid var(--border);
	}
	.phone .cbar {
		padding: 0 0 var(--space-3);
		border: 0;
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
	.phone .ib,
	.phone .today-btn {
		min-width: 44px;
		min-height: 44px;
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
	.phone .cbar h2 {
		order: -1;
		flex-basis: 100%;
	}
	.phone .views :global(.segments button) {
		min-height: 44px;
	}
	.msg {
		margin: var(--space-2) var(--space-3);
	}
	.phone .msg {
		margin: 0 0 var(--space-2);
	}
	.list-more {
		padding: 0 var(--space-3) var(--space-3);
	}
	.phone .list-more {
		padding: var(--space-3) 0 0;
	}
	.list-more .btn {
		min-height: 44px;
	}
	.strip {
		display: grid;
		grid-template-columns: repeat(7, minmax(0, 1fr));
		gap: 3px;
		margin-bottom: var(--space-3);
	}
	.sd {
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: 1px;
		min-height: 56px;
		padding: 6px 0;
		border: 1px solid transparent;
		border-radius: var(--radius-ctl);
		background: var(--srow-bg);
		color: var(--text);
		font: inherit;
	}
	.sd small {
		color: var(--muted);
		font-size: var(--text-xs);
	}
	.sd b {
		font-size: var(--text-md);
	}
	.sd.sel {
		border-color: var(--accent);
	}
	.sd.today {
		background: var(--accent);
		color: var(--on-accent);
	}
	.sd.today small {
		color: inherit;
	}
	.sd i {
		width: 4px;
		height: 4px;
		border-radius: 50%;
	}
	.sd i.has {
		background: currentColor;
		opacity: 0.6;
	}
	.phone-cals {
		margin-top: var(--space-5);
	}
	/* A narrower window: the column below the view, so the days keep room
	   for their titles (a cell under 4 letters' width would break every word). */
	@media (max-width: 980px) {
		.cal:not(.phone) {
			grid-template-columns: minmax(0, 1fr);
		}
		.cal:not(.phone) .side {
			order: 2;
			border-right: 0;
			border-top: 1px solid var(--border);
		}
	}
</style>
