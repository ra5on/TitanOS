<script lang="ts">
	// 1.11: a new or changed event of T-Kalender (drafts 05, 06, 43, 44). On
	// the desktop a window of its own like writing in T-Mail (taskPage), on
	// the phone a page with the fields one below the other. Title, calendar
	// (color, name, a check mark), all-day, start and end with duration and
	// zone, repetition (presets of the start date, every n, ends never / on
	// a date / after a number, the next dates from POST /calendar/preview; a
	// rule from another device is shown as text and kept), reminders,
	// place, notes. Changing one event of a series asks which ones are meant
	// when saving; the danger zone deletes. A conflict (changed elsewhere)
	// says so and reads the event again.
	// 1.13: a draft of the KI-Agent (?agentDraft=<chat>:<entry>, "Öffnen und
	// bearbeiten") fills the form – a new event, or a change of the event the
	// draft names (cal, id, occurrence; only the fields that change). It is
	// text to check: saved only with "Sichern", a series asks as always.
	import { beforeNavigate } from '$app/navigation';
	import { page } from '$app/state';
	import { ApiError } from '#lib/api.ts';
	import DangerZone from '#lib/components/DangerZone.svelte';
	import Dialog from '#lib/components/Dialog.svelte';
	import Dropdown from '#lib/components/Dropdown.svelte';
	import Icon from '#lib/components/Icon.svelte';
	import PageHead from '#lib/components/PageHead.svelte';
	import Segments from '#lib/components/Segments.svelte';
	import { phoneView } from '#lib/desktop/mode.svelte.ts';
	import { guardWindowClose } from '#lib/desktop/unsaved.svelte.ts';
	import { apiErrorText, t } from '#lib/i18n/index.svelte.ts';
	import {
		addDays,
		clock,
		daysBetween,
		inclusiveEnd,
		viewZone,
		validDate,
		wallMinutes,
		wallOf,
		wallParts,
	} from '#lib/pim/caldates.ts';
	import {
		ALLDAY_ALARMS,
		TIMED_ALARMS,
		apiTimes,
		calColor,
		fromRule,
		intervalUnit,
		repeatKinds,
		toSimple,
		type CalEvent,
		type Calendar,
		type Repeat,
		type RepeatEnd,
		type RepeatKind,
	} from '#lib/pim/calendar.ts';
	import {
		announceChange,
		closeEventWindow,
		createEvent,
		loadCalendars,
		loadNasZone,
		loadEvent,
		previewRepeat,
		safeBack,
		updateEvent,
		type EventInput,
		type Preview,
		type Scope,
	} from '#lib/pim/calendar-api.ts';
	import {
		alarmText,
		calName,
		dateText,
		durationText,
		intervalWord,
		repeatLabel,
		ruleText,
		shortDate,
		zoneName,
	} from '#lib/pim/calendar-text.ts';
	import DeleteEventDialog from '#lib/pim/cal/DeleteEventDialog.svelte';
	import ScopeChoices from '#lib/pim/cal/ScopeChoices.svelte';
	import { shellOfError } from '#lib/pim/shell.ts';
	import { eventDraftForm, loadAgentDraft, type AgentDraft } from '#lib/agent/drafts.ts';
	import type { Simple } from '#lib/pim/calendar.ts';

	const phone = $derived(phoneView());
	const params = page.url.searchParams;
	// A draft of a change names its event itself (load() sets these).
	let editCal = $state(params.get('cal') ?? '');
	let editId = $state(params.get('id') ?? '');
	let editOcc = $state(params.get('occurrence') ?? '');
	const editing = $derived(!!(editCal && editId));
	const draftParam = params.get('agentDraft');
	/** A draft was put in ('applied') or could not be opened ('gone'). */
	let fromAgent = $state<'' | 'applied' | 'gone'>('');
	let draftRule: Simple | null = null;
	let draftScope: Scope | '' = '';
	let draftZone = $state('');
	const back = safeBack(params.get('back'));
	// Without ?date= today in the NAS zone (known once loadNasZone answered).
	const startDayIn = (zone: string) => {
		const d = params.get('date') ?? '';
		return validDate(d) ? d : wallOf(new Date(), zone).date;
	};
	// A first title, e.g. the name of a contact ("Termin" in Kontakte); text only.
	const startTitle = (params.get('title') ?? '').replace(/\s+/g, ' ').trim().slice(0, 200);
	const startClock = (() => {
		const v = params.get('time') ?? '';
		return /^\d{2}:\d{2}$/.test(v) ? v : '09:00';
	})();
	// 1.13 (dragging in the week, later the agent): more of a new event in
	// the address – its end (`end` HH:MM on `endDate` or the start's day) or
	// length (`duration` in minutes, 1 … 10 080), all day (`allDay=1`, to
	// `endDate`), place, notes and calendar. Each bounded; anything else is
	// left out. Only a draft: nothing is saved before "Sichern".
	const oneLine = (v: string | null, max: number) =>
		(v ?? '').replace(/\s+/g, ' ').trim().slice(0, max);
	const startAllDay = params.get('allDay') === '1';
	const startPlace = oneLine(params.get('location'), 500);
	const startNotes = (params.get('notes') ?? '').replace(/\r\n?/g, '\n').slice(0, 4000);
	const startCalendar = oneLine(params.get('calendar'), 100);
	const startEnd = (() => {
		const v = params.get('end') ?? '';
		return /^\d{2}:\d{2}$/.test(v) ? v : '';
	})();
	const startEndDate = (() => {
		const v = params.get('endDate') ?? '';
		return validDate(v) ? v : '';
	})();
	const startDuration = (() => {
		const n = Number(params.get('duration') ?? '');
		return Number.isInteger(n) && n > 0 && n <= 10080 ? n : 0;
	})();

	type Form = {
		title: string;
		calendar: string;
		allDay: boolean;
		startDate: string;
		startTime: string;
		endDate: string;
		endTime: string;
		repeat: Repeat;
		alarms: number[];
		location: string;
		notes: string;
	};
	const plusHour = (time: string) => {
		const m = Number(time.slice(0, 2)) * 60 + Number(time.slice(3, 5)) + 60;
		return m >= 1440 ? { day: 1, time: clock(m - 1440) } : { day: 0, time: clock(m) };
	};
	/** The end of a new event: from the address, else an hour after the start. */
	function endOf(startDay: string): { date: string; time: string } {
		const startMin = Number(startClock.slice(0, 2)) * 60 + Number(startClock.slice(3, 5));
		if (startDuration) {
			const total = startMin + startDuration;
			return { date: addDays(startDay, Math.floor(total / 1440)), time: clock(total % 1440) };
		}
		if (startEnd || startEndDate) {
			const date = startEndDate && startEndDate >= startDay ? startEndDate : startDay;
			const time = startEnd || startClock;
			if (startAllDay || wallMinutes(startDay, startClock, date, time) > 0) return { date, time };
		}
		const end = plusHour(startClock);
		return { date: addDays(startDay, end.day), time: end.time };
	}
	function blank(zone = viewZone()): Form {
		const startDay = startDayIn(zone);
		const end = endOf(startDay);
		return {
			title: startTitle,
			calendar: '',
			allDay: startAllDay,
			startDate: startDay,
			startTime: startClock,
			endDate: end.date,
			endTime: end.time,
			repeat: { kind: 'none', interval: 1, end: 'never', until: '', count: 10 },
			alarms: [],
			location: startPlace,
			notes: startNotes,
		};
	}
	function formOf(ev: CalEvent): Form {
		const s = wallParts(ev.start);
		const e = wallParts(ev.end || ev.start);
		return {
			title: ev.title,
			calendar: ev.calendar,
			allDay: ev.allDay,
			startDate: s.date,
			startTime: ev.allDay ? '09:00' : s.time,
			endDate: ev.allDay ? inclusiveEnd(s.date, e.date) : e.date,
			endTime: ev.allDay ? '10:00' : e.time,
			repeat: fromRule(ev.rule, ev.recurring, s.date),
			alarms: [...ev.alarms].sort((a, b) => a - b),
			location: ev.location,
			notes: ev.notes ?? '',
		};
	}

	let calendars = $state<Calendar[]>([]);
	let event = $state<CalEvent | null>(null);
	let form = $state<Form>(blank());
	let initial = $state(JSON.stringify(blank()));
	let loading = $state(true);
	let loadError = $state('');
	let unavailable = $state(false);
	let busy = $state(false);
	let error = $state('');
	let conflict = $state(false);
	let note = $state('');
	let askScope = $state(false);
	let scope = $state<Scope>('this');
	let deleting = $state(false);
	let askDiscard = $state(false);
	let done = $state(false);

	const own = $derived(calendars.filter((c) => c.kind === 'own' && !c.readOnly));
	// New events in the NAS zone (/calendar/access), the browser's without one.
	let nasZone = $state(viewZone());
	const zone = $derived(editing ? (event?.timezone ?? '') : draftZone || nasZone);
	const dirty = $derived(JSON.stringify(form) !== initial);

	async function load() {
		loading = true;
		loadError = '';
		try {
			let got: AgentDraft | null;
			[calendars, nasZone, got] = await Promise.all([
				loadCalendars(),
				loadNasZone(),
				draftParam ? loadAgentDraft(draftParam) : Promise.resolve(null),
			]);
			const draft = got?.app === 'calendar' && got.kind === 'event' ? got : null;
			if (draftParam && !draft) fromAgent = 'gone';
			const f = draft?.fields ?? {};
			if (draft && !editing && typeof f.cal === 'string' && typeof f.id === 'string') {
				editCal = f.cal;
				editId = f.id;
				editOcc = typeof f.occurrence === 'string' ? f.occurrence : '';
			}
			if (editing) {
				event = await loadEvent(editCal, editId, editOcc);
				form = formOf(event);
			} else {
				const first =
					calendars.find((c) => c.id === startCalendar && c.kind === 'own' && !c.readOnly) ??
					calendars.find((c) => c.kind === 'own' && !c.readOnly && !c.hidden) ??
					calendars.find((c) => c.kind === 'own' && !c.readOnly);
				form = { ...blank(nasZone), calendar: first?.id ?? '' };
			}
			// What was there before the draft: the change is what differs from it.
			initial = JSON.stringify(form);
			if (draft) {
				const ids = calendars.filter((c) => c.kind === 'own' && !c.readOnly).map((c) => c.id);
				const put = eventDraftForm(form, f, ids);
				form = put.form;
				draftRule = put.rule;
				draftScope = put.scope;
				if (!editing) draftZone = put.zone;
				fromAgent = 'applied';
			}
			takeDuration();
		} catch (err) {
			unavailable = shellOfError(err).kind === 'unavailable';
			loadError = apiErrorText(err);
		} finally {
			loading = false;
		}
	}
	$effect(() => {
		void load();
	});

	guardWindowClose(() => (busy ? 'busy' : dirty && !done ? 'edits' : ''));
	beforeNavigate((nav) => {
		if (done || !dirty || nav.willUnload || !nav.to) return;
		nav.cancel();
		askDiscard = true;
	});

	// Start moved: the end keeps the duration (taken when the end changes).
	let duration = 60;
	function takeDuration() {
		const m = wallMinutes(form.startDate, form.startTime, form.endDate, form.endTime);
		if (m >= 0) duration = m;
	}
	function moveStart() {
		if (form.allDay) {
			if (form.endDate < form.startDate) form.endDate = form.startDate;
			return;
		}
		const [h, m] = form.startTime.split(':').map(Number);
		const total = h * 60 + m + duration;
		form.endDate = addDays(form.startDate, Math.floor(total / 1440));
		form.endTime = clock(total % 1440);
	}

	const minutes = $derived(
		form.allDay ? 0 : wallMinutes(form.startDate, form.startTime, form.endDate, form.endTime),
	);
	const kinds = $derived.by(() => {
		const list: RepeatKind[] = repeatKinds(form.startDate);
		if (form.repeat.kind === 'custom' || (editing && event?.recurring && !event.rule))
			list.push('custom');
		else if (!list.includes(form.repeat.kind)) list.push(form.repeat.kind);
		return list;
	});
	const unit = $derived(intervalUnit(form.repeat.kind));
	const customText = $derived(event ? ruleText(event) : '');

	function problem(): string {
		if (!form.calendar) return t('calendar.form.noCalendar');
		if (!validDate(form.startDate) || !validDate(form.endDate)) return t('calendar.form.badDate');
		if (
			!form.allDay &&
			!(/^\d{2}:\d{2}$/.test(form.startTime) && /^\d{2}:\d{2}$/.test(form.endTime))
		)
			return t('calendar.form.badDate');
		if (form.allDay ? form.endDate < form.startDate : minutes < 0)
			return t('calendar.form.endBeforeStart');
		if (
			form.repeat.end === 'date' &&
			form.repeat.kind !== 'none' &&
			form.repeat.kind !== 'custom'
		) {
			if (!validDate(form.repeat.until) || form.repeat.until < form.startDate)
				return t('calendar.form.untilBeforeStart');
		}
		return '';
	}

	// The next dates of a repetition, from titand (it knows the rules).
	let preview = $state<Preview | null>(null);
	let previewTimer: ReturnType<typeof setTimeout> | undefined;
	$effect(() => {
		const r = form.repeat;
		const simple = r.kind === 'none' || r.kind === 'custom' ? null : toSimple(r, form.startDate);
		const times = apiTimes(form);
		const body = JSON.stringify({ ...times, timezone: zone, repeat: simple });
		clearTimeout(previewTimer);
		if (!simple || problem()) {
			preview = null;
			return;
		}
		previewTimer = setTimeout(() => {
			previewRepeat(JSON.parse(body))
				.then((p) => (preview = p))
				.catch(() => (preview = null));
		}, 300);
	});
	const previewText = $derived.by(() => {
		if (!preview) return '';
		const dates = preview.occurrences
			.map((o) => o.start.slice(0, 10))
			.filter((d) => d !== form.startDate)
			.slice(0, 3)
			.map(shortDate);
		if (!dates.length) return '';
		let s = t('calendar.form.next', { list: dates.join(', ') + (preview.more ? ' …' : '') });
		if (preview.total)
			s +=
				' · ' +
				(form.repeat.end === 'date' && form.repeat.until
					? t('calendar.form.totalUntil', { n: preview.total, date: dateText(form.repeat.until) })
					: t('calendar.form.total', { n: preview.total }));
		return s;
	});

	function input(): EventInput {
		const base = JSON.parse(initial) as Form;
		const out: EventInput = {};
		const times = apiTimes(form);
		const timesChanged =
			!editing ||
			form.allDay !== base.allDay ||
			form.startDate !== base.startDate ||
			form.endDate !== base.endDate ||
			(!form.allDay && (form.startTime !== base.startTime || form.endTime !== base.endTime));
		if (timesChanged) {
			Object.assign(out, times);
			// New: the NAS zone (else the browser's); changed: '' keeps the event's own zone.
			out.timezone = editing ? '' : zone;
		}
		if (!editing || form.title !== base.title) out.title = form.title;
		if (!editing || form.location !== base.location) out.location = form.location;
		if (!editing || form.notes !== base.notes) out.notes = form.notes;
		if (!editing || JSON.stringify(form.alarms) !== JSON.stringify(base.alarms))
			out.alarms = form.alarms;
		if (!editing || JSON.stringify(form.repeat) !== JSON.stringify(base.repeat) || timesChanged)
			// A rule of the agent's draft the form shows only as "custom" goes as it came.
			out.repeat =
				form.repeat.kind === 'custom' && draftRule
					? draftRule
					: toSimple(form.repeat, form.startDate);
		return out;
	}

	function save() {
		error = problem();
		conflict = false;
		if (error) return;
		if (editing && event?.recurring) {
			scope = moved ? 'all' : draftScope || 'this';
			askScope = true;
			return;
		}
		void send('all');
	}
	const moved = $derived(editing && !!event && form.calendar !== event.calendar);

	async function send(chosen: Scope) {
		askScope = false;
		busy = true;
		error = '';
		try {
			let dropped = 0;
			if (editing && event) {
				const res = await updateEvent(event.calendar, event.id, {
					etag: event.etag,
					scope: chosen,
					occurrence: event.occurrence,
					event: input(),
					...(moved ? { calendar: form.calendar } : {}),
				});
				dropped = res?.droppedExceptions ?? 0;
			} else {
				const res = await createEvent(form.calendar, input());
				dropped = res?.droppedExceptions ?? 0;
			}
			announceChange();
			initial = JSON.stringify(form);
			if (dropped > 0) {
				// Stays open: the person reads what went with it.
				note = t('calendar.form.dropped', { n: dropped });
				done = true;
				return;
			}
			finish();
		} catch (err) {
			conflict = err instanceof ApiError && err.code === 'calendar_conflict';
			error = conflict ? t('calendar.conflict') : apiErrorText(err);
		} finally {
			busy = false;
		}
	}

	function finish() {
		done = true;
		closeEventWindow(back);
	}
	function discard() {
		if (!dirty || done) return finish();
		askDiscard = true;
	}

	async function reloadEvent() {
		if (!event) return;
		busy = true;
		try {
			event = await loadEvent(event.calendar, event.id, event.occurrence);
			form = formOf(event);
			initial = JSON.stringify(form);
			takeDuration();
			error = '';
			conflict = false;
		} catch (err) {
			error = apiErrorText(err);
		} finally {
			busy = false;
		}
	}

	const startMin = $derived(
		Number(form.startTime.slice(0, 2)) * 60 + Number(form.startTime.slice(3, 5)) || 0,
	);
	const alarmChoices = $derived(
		(form.allDay ? ALLDAY_ALARMS : TIMED_ALARMS).filter((m) => !form.alarms.includes(m)),
	);

	const title = $derived(
		editing ? form.title.trim() || event?.title || t('calendar.untitled') : t('calendar.form.new'),
	);
	const endOptions = $derived([
		{ value: 'never' as RepeatEnd, label: t('calendar.form.endNever') },
		{ value: 'date' as RepeatEnd, label: t('calendar.form.endDate') },
		{ value: 'count' as RepeatEnd, label: t('calendar.form.endCount') },
	]);
</script>

<PageHead {title} path={phone ? undefined : [{ label: t('desktop.app.calendar'), href: back }]}>
	{#snippet actions()}
		<button type="button" class="btn" disabled={busy} onclick={discard}
			><Icon name="close" size={16} />{done
				? t('calendar.detail.close')
				: t('calendar.form.discard')}</button
		>
		{#if !done}
			<button
				type="button"
				class="btn btn-primary"
				disabled={busy || loading || !!loadError || !own.length}
				onclick={save}><Icon name="check" size={16} />{t('calendar.form.save')}</button
			>
		{/if}
	{/snippet}
</PageHead>

{#if loading}
	<p class="muted" role="status">{t('common.loading')}</p>
{:else if loadError}
	{#if unavailable}
		<div class="notice-warn" role="status">
			<p class="strong">{t('calendar.unavailable.title')}</p>
			<p>{t('calendar.unavailable.text')}</p>
		</div>
	{:else}
		<p class="notice-error" role="alert">{loadError}</p>
	{/if}
{:else}
	<form class="ev" class:phone onsubmit={(e) => (e.preventDefault(), save())} data-event-form>
		{#if fromAgent === 'applied'}<p class="notice-warn" role="note" data-agent-draft>
				{t('agentpim.draft.eventNote')}
			</p>{:else if fromAgent === 'gone'}<p class="muted" role="status">
				{t('agentpim.draft.gone')}
			</p>{/if}
		{#if note}<p class="notice-warn" role="status">{note}</p>{/if}
		<div class="row">
			<label class="lbl" for="t-title">{t('calendar.form.title')}</label>
			<input
				id="t-title"
				class="field big"
				type="text"
				bind:value={form.title}
				maxlength="2000"
				placeholder={t('calendar.form.titlePlaceholder')}
			/>
		</div>

		<div class="row top">
			<span class="lbl" id="t-cal">{t('calendar.form.calendar')}</span>
			<div class="calpick" role="radiogroup" aria-labelledby="t-cal">
				{#each own as c (c.id)}
					<label class="cp" class:on={form.calendar === c.id} style:--c={calColor(c.color)}>
						<input type="radio" name="cal" value={c.id} bind:group={form.calendar} />
						<span class="sw" aria-hidden="true"></span><span class="cpn">{calName(c)}</span>
						{#if form.calendar === c.id}<Icon name="check" size={15} />{/if}
					</label>
				{/each}
				{#if !own.length}<p class="muted">{t('calendar.form.noCalendar')}</p>{/if}
			</div>
		</div>
		{#if moved && event?.recurring}
			<p class="hint-row muted">{t('calendar.scope.moveOnlyAll')}</p>
		{/if}

		<div class="row">
			<label class="lbl" for="t-all">{t('calendar.form.allDay')}</label>
			<label class="swwrap"
				><input
					id="t-all"
					type="checkbox"
					class="switch"
					role="switch"
					bind:checked={form.allDay}
				/></label
			>
		</div>

		<div class="row">
			<label class="lbl" for="t-from">{t('calendar.form.start')}</label>
			<span class="when">
				<input
					id="t-from"
					class="field date"
					type="date"
					bind:value={form.startDate}
					onchange={moveStart}
				/>
				{#if !form.allDay}<input
						class="field time"
						type="time"
						bind:value={form.startTime}
						onchange={moveStart}
						aria-label={t('calendar.form.startTime')}
					/>{/if}
			</span>
		</div>
		<div class="row">
			<label class="lbl" for="t-to">{t('calendar.form.end')}</label>
			<span class="when">
				<input
					id="t-to"
					class="field date"
					type="date"
					bind:value={form.endDate}
					onchange={takeDuration}
				/>
				{#if !form.allDay}<input
						class="field time"
						type="time"
						bind:value={form.endTime}
						onchange={takeDuration}
						aria-label={t('calendar.form.endTime')}
					/>{/if}
				<small class="muted" data-duration
					>{#if form.allDay}{durationText(
							(Math.max(0, daysBetween(form.startDate, form.endDate)) + 1) * 1440,
						)}{:else if minutes >= 0}{durationText(
							minutes,
						)}{/if}{#if !form.allDay && zone}{' · '}{t('calendar.zone', {
							zone: zoneName(zone),
						})}{:else if !form.allDay && editing}{' · '}{t('calendar.form.floating')}{/if}</small
				>
			</span>
		</div>
		{#if event?.tzGuess}
			<p class="hint-row muted">{t('calendar.form.tzGuess', { zone: zoneName(zone) })}</p>
		{/if}

		<div class="row top">
			<label class="lbl" for="t-rep">{t('calendar.form.repeat')}</label>
			<span class="stack">
				<select id="t-rep" class="field sel" bind:value={form.repeat.kind}>
					{#each kinds as k (k)}<option value={k}
							>{repeatLabel(k, form.startDate, customText)}</option
						>{/each}
				</select>
				{#if form.repeat.kind === 'custom'}
					<small class="muted">{t('calendar.form.customHint')}</small>
				{:else if form.repeat.kind !== 'none'}
					{#if unit}
						<span class="endrow">
							<label class="muted" for="t-int">{t('calendar.form.every')}</label>
							<input
								id="t-int"
								class="field num"
								type="number"
								min="1"
								max="999"
								bind:value={form.repeat.interval}
							/>
							<span>{intervalWord(unit, form.repeat.interval)}</span>
						</span>
					{/if}
					<span class="endrow">
						<span class="muted">{t('calendar.form.ends')}</span>
						<Segments
							compact
							label={t('calendar.form.ends')}
							bind:value={form.repeat.end}
							options={endOptions}
						/>
						{#if form.repeat.end === 'date'}<input
								class="field date"
								type="date"
								bind:value={form.repeat.until}
								min={form.startDate}
								aria-label={t('calendar.form.until')}
							/>{:else if form.repeat.end === 'count'}<input
								class="field num"
								type="number"
								min="1"
								max="999"
								bind:value={form.repeat.count}
								aria-label={t('calendar.form.count')}
							/><span>{t('calendar.form.countUnit')}</span>{/if}
					</span>
					{#if previewText}<small class="muted" data-preview>{previewText}</small>{/if}
				{/if}
				{#if event?.recurUnsupported}<small class="muted"
						>{t('calendar.form.recurUnsupported')}</small
					>{/if}
			</span>
		</div>

		<div class="row top">
			<span class="lbl" id="t-alarm">{t('calendar.form.alarm')}</span>
			<span class="stack" role="group" aria-labelledby="t-alarm">
				{#each form.alarms as a (a)}
					<span class="alarm"
						><Icon name="bell" size={16} /><span class="grow"
							>{alarmText(a, form.allDay, startMin)}</span
						><button
							type="button"
							class="ib"
							aria-label={t('calendar.form.removeAlarm', {
								text: alarmText(a, form.allDay, startMin),
							})}
							onclick={() => (form.alarms = form.alarms.filter((x) => x !== a))}
							><Icon name="close" size={15} /></button
						></span
					>
				{/each}
				{#if alarmChoices.length && form.alarms.length < 20}
					<!-- Draft 05/43: "+ Erinnerung" opens the choices. -->
					<Dropdown label={t('calendar.form.addAlarm')} text={t('calendar.form.alarm')} icon="plus">
						{#snippet children(close)}
							{#each alarmChoices as m (m)}
								<button
									type="button"
									role="menuitem"
									onclick={() => {
										close();
										if (!form.alarms.includes(m))
											form.alarms = [...form.alarms, m].sort((x, y) => x - y);
									}}><span>{alarmText(m, form.allDay, startMin)}</span></button
								>
							{/each}
						{/snippet}
					</Dropdown>
				{/if}
				<small class="muted">{t('calendar.form.alarmHint')}</small>
			</span>
		</div>

		<div class="row">
			<label class="lbl" for="t-place">{t('calendar.form.place')}</label>
			<input
				id="t-place"
				class="field"
				type="text"
				bind:value={form.location}
				maxlength="2000"
				placeholder={t('calendar.form.placePlaceholder')}
			/>
		</div>

		<div class="row top">
			<label class="lbl" for="t-note">{t('calendar.form.notes')}</label>
			<textarea id="t-note" class="field note" rows={phone ? 3 : 4} bind:value={form.notes}
			></textarea>
		</div>

		{#if error}
			<p class="notice-error" role="alert">
				{error}
				{#if conflict}<button type="button" class="btn" onclick={reloadEvent}
						>{t('calendar.reload')}</button
					>{/if}
			</p>
		{/if}

		{#if editing && event && !done}
			<DangerZone
				hint={event.recurring ? t('calendar.form.dangerSeries') : t('calendar.form.dangerSingle')}
			>
				<button
					type="button"
					class="btn btn-danger"
					disabled={busy}
					onclick={() => (deleting = true)}
					><Icon name="trash" size={15} />{t('calendar.form.delete')}</button
				>
			</DangerZone>
		{/if}
	</form>
{/if}

{#if askScope && event}
	<Dialog title={t('calendar.save.title', { title: title })} onclose={() => (askScope = false)}>
		<p class="lead">{t('calendar.save.text')}</p>
		<ScopeChoices
			bind:value={scope}
			mode="save"
			only={moved ? ['all'] : undefined}
			label={t('calendar.save.which')}
		/>
		{#if moved}<p class="muted">{t('calendar.scope.moveOnlyAll')}</p>{/if}
		<div class="dialog-actions">
			<button type="button" class="btn" onclick={() => (askScope = false)}
				>{t('common.cancel')}</button
			>
			<button type="button" class="btn btn-primary" onclick={() => send(scope)}
				>{t('calendar.form.save')}</button
			>
		</div>
	</Dialog>
{/if}

{#if deleting && event}
	<DeleteEventDialog
		{event}
		onclose={() => (deleting = false)}
		ondone={() => {
			deleting = false;
			announceChange();
			finish();
		}}
	/>
{/if}

{#if askDiscard}
	<Dialog title={t('calendar.form.discardTitle')} onclose={() => (askDiscard = false)}>
		<p class="lead">{t('calendar.form.discardText')}</p>
		<div class="dialog-actions">
			<button type="button" class="btn" onclick={() => (askDiscard = false)}
				>{t('common.cancel')}</button
			>
			<button
				type="button"
				class="btn btn-danger"
				onclick={() => {
					askDiscard = false;
					finish();
				}}>{t('calendar.form.discard')}</button
			>
		</div>
	</Dialog>
{/if}

<style>
	.ev {
		display: flex;
		flex-direction: column;
		gap: var(--space-3);
		max-width: 52rem;
		min-width: 0;
	}
	.row {
		display: grid;
		grid-template-columns: 7.5rem minmax(0, 1fr);
		align-items: center;
		gap: var(--space-2);
	}
	.row.top {
		align-items: start;
	}
	.row.top .lbl {
		padding-top: 9px;
	}
	.phone .row {
		grid-template-columns: minmax(0, 1fr);
		gap: var(--space-1);
	}
	.phone .row.top .lbl {
		padding-top: 0;
	}
	/* The switch's whole line is its target (44 px on the phone). */
	.swwrap {
		display: inline-flex;
		align-items: center;
		min-height: 36px;
		cursor: pointer;
	}
	.phone .swwrap {
		min-height: 44px;
		min-width: 44px;
	}
	.hint-row {
		margin: 0 0 0 calc(7.5rem + var(--space-2));
		font-size: var(--text-sm);
	}
	.phone .hint-row {
		margin: 0;
	}
	.lbl {
		color: var(--muted);
		font-size: var(--text-md);
		overflow-wrap: break-word;
	}
	.row > .field {
		width: 100%;
		min-width: 0;
	}
	.big {
		font-size: var(--text-lg);
		font-weight: 600;
	}
	.calpick {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-1-5);
		min-width: 0;
	}
	.cp {
		position: relative;
		display: inline-flex;
		align-items: center;
		gap: var(--space-2);
		min-height: 44px;
		max-width: 100%;
		padding: 0 var(--space-3);
		border: 1px solid var(--border);
		border-radius: var(--radius-ctl);
		background: var(--srow-bg);
		cursor: pointer;
	}
	.cpn {
		min-width: 0;
		overflow-wrap: break-word;
	}
	.cp input {
		position: absolute;
		opacity: 0;
		pointer-events: none;
	}
	.cp.on {
		border-color: var(--accent);
		box-shadow: inset 0 0 0 1px var(--accent);
		font-weight: 600;
	}
	.cp:has(input:focus-visible) {
		outline: 2px solid var(--accent);
		outline-offset: 2px;
	}
	.sw {
		flex: none;
		width: 12px;
		height: 12px;
		border-radius: 3px;
		background: var(--c);
	}
	.when {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-2);
		min-width: 0;
	}
	.date,
	.time {
		width: auto;
	}
	.num {
		width: 4.5rem;
	}
	.stack {
		display: flex;
		flex-direction: column;
		align-items: flex-start;
		gap: var(--space-2);
		min-width: 0;
	}
	.sel {
		width: auto;
		max-width: 100%;
	}
	.endrow {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-2);
	}
	.alarm {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		min-height: 44px;
		padding: 0 var(--space-1) 0 var(--space-3);
		border-radius: var(--radius-ctl);
		background: var(--srow-bg);
		width: min(26rem, 100%);
	}
	.phone .stack :global(.dropdown .btn) {
		min-height: 44px;
	}
	.grow {
		flex: 1;
		min-width: 0;
		overflow-wrap: break-word;
	}
	.ib {
		display: inline-grid;
		place-items: center;
		min-width: 36px;
		min-height: 36px;
		padding: 0;
		border: 0;
		background: none;
		color: var(--muted);
		cursor: pointer;
	}
	.ib:focus-visible {
		outline: 2px solid var(--accent);
	}
	.phone .ib {
		min-width: 44px;
		min-height: 44px;
	}
	.phone .field {
		min-height: 44px;
	}
	.note {
		width: 100%;
		resize: vertical;
		font: inherit;
		line-height: 1.5;
	}
	small {
		font-size: var(--text-sm);
	}
	.notice-error {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-2);
	}
	.notice-warn p {
		margin: 0;
	}
	.strong {
		font-weight: 600;
	}
	.lead {
		margin: 0;
		overflow-wrap: break-word;
	}
	@media (max-width: 560px) {
		.ev:not(.phone) .row {
			grid-template-columns: minmax(0, 1fr);
			gap: var(--space-1);
		}
		.ev:not(.phone) .hint-row {
			margin: 0;
		}
	}
</style>
