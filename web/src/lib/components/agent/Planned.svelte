<script lang="ts">
	// "Geplant" of the KI-Agent (PLAN 49): the morning report each person
	// sets for themselves, then all planned tasks and reminders – once,
	// every few minutes/hours/days, on weekdays, weekly, monthly, by a cron
	// line or on a state in Home Assistant – with the next run, pause,
	// change, run now and delete. Times are those of the NAS.
	import Icon from '#lib/components/Icon.svelte';
	import Dialog from '#lib/components/Dialog.svelte';
	import { api } from '#lib/api.ts';
	import { formatDateTime } from '#lib/format.ts';
	import { apiErrorText, t, type MessageKey } from '#lib/i18n/index.svelte.ts';
	import type { Project, Report, Schedule } from '#lib/agent.ts';

	let {
		projects = [],
		onopenchat,
		oncount,
	}: {
		projects?: Project[];
		onopenchat: (id: string) => void;
		oncount?: (n: number) => void;
	} = $props();

	type View = Schedule & { next?: string; when?: string };
	let list = $state<View[]>([]);
	let error = $state('');
	let busy = $state(false);
	let editing = $state<string | null>(null);
	let removing = $state<View | null>(null);
	let draft = $state<Record<string, string>>({});
	let days = $state<number[]>([]);
	let haConns = $state<{ id: string; name: string }[]>([]);

	const weekdays = $derived(t('agent.weekdays').split(','));
	const short = (i: number) => weekdays[i]?.slice(0, 2) ?? '';
	// Monday first, as people read a week here.
	const order = [1, 2, 3, 4, 5, 6, 0];
	const morning = $derived(list.find((s) => s.kind === 'morning'));
	const others = $derived(
		list
			.filter((s) => s.kind !== 'morning')
			.sort((a, b) => (a.next ?? '9999').localeCompare(b.next ?? '9999')),
	);

	async function load() {
		error = '';
		try {
			list = (await api.get<{ schedules: View[] }>('/agent/schedules')).schedules;
			oncount?.(list.length);
		} catch (e) {
			error = apiErrorText(e);
		}
	}
	$effect(() => {
		void load();
		api
			.get<{ connections?: { id: string; name: string }[] }>('/homeassistant/connection-names')
			.then(
				(res) => (haConns = res.connections ?? []),
				() => (haConns = []),
			);
	});

	async function run(fn: () => Promise<unknown>, close = true) {
		error = '';
		busy = true;
		try {
			await fn();
			if (close) editing = null;
			await load();
		} catch (e) {
			error = apiErrorText(e);
		} finally {
			busy = false;
		}
	}
	const put = (s: Schedule) => api.put(`/agent/schedules/${s.id || 'new'}`, s);
	const toggle = (s: View) => run(() => put({ ...s, enabled: !s.enabled }), false);

	// ---- Morning report -------------------------------------------------
	let report = $state<Report>({ nas: true, backups: true, today: true });
	let mAt = $state('07:00');
	let mDays = $state<number[]>([1, 2, 3, 4, 5]);
	let mOn = $state(true);
	$effect(() => {
		if (morning) {
			report = { ...(morning.report ?? {}) };
			mAt = morning.at ?? '07:00';
			mDays =
				morning.every === 'weekdays'
					? [...(morning.weekdays ?? [])]
					: morning.every === 'daily'
						? [0, 1, 2, 3, 4, 5, 6]
						: [1, 2, 3, 4, 5];
			mOn = morning.enabled;
		}
	});
	function saveMorning() {
		const all = mDays.length === 7;
		const body: Schedule = {
			...(morning ?? ({} as Schedule)),
			id: morning?.id ?? '',
			kind: 'morning',
			name: t('agent.morning'),
			task: t('agent.morning'),
			every: all ? 'daily' : 'weekdays',
			weekdays: all ? [] : mDays,
			at: mAt,
			enabled: mOn,
			report,
		};
		void run(() => put(body), false);
	}
	const dayToggle = (arr: number[], d: number) =>
		arr.includes(d) ? arr.filter((x) => x !== d) : [...arr, d];

	// ---- Editor -----------------------------------------------------------
	const everyKeys = [
		'once',
		'minutes',
		'hours',
		'daily',
		'days',
		'weekdays',
		'weekly',
		'monthly',
		'cron',
		'trigger',
	] as const;
	const everyLabel: Record<string, MessageKey> = {
		once: 'agent.everyOnce',
		minutes: 'agent.everyMinutes',
		hours: 'agent.everyHours',
		hourly: 'agent.everyHourly',
		daily: 'agent.everyDaily',
		days: 'agent.everyDays',
		weekdays: 'agent.everyWeekdays',
		weekly: 'agent.everyWeekly',
		monthly: 'agent.everyMonthly',
		cron: 'agent.everyCron',
		trigger: 'agent.everyTrigger',
	};
	function today(offsetMinutes = 60): { date: string; at: string } {
		const d = new Date(Date.now() + offsetMinutes * 60000);
		const pad = (n: number) => String(n).padStart(2, '0');
		return {
			date: `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`,
			at: `${pad(d.getHours())}:00`,
		};
	}
	function edit(s?: View) {
		const soon = today();
		editing = s?.id ?? 'new';
		draft = {
			kind: s?.kind ?? '',
			name: s?.name ?? '',
			task: s?.task ?? '',
			every: s?.trigger ? 'trigger' : (s?.every ?? 'once'),
			at: s?.at ?? soon.at,
			date: s?.date ?? soon.date,
			interval: String(s?.interval ?? (s?.every === 'minutes' ? 15 : 2)),
			weekday: String(s?.weekday ?? 1),
			day: String(s?.day ?? 1),
			cron: s?.cron ?? '0 7 * * 1-5',
			entity: s?.trigger?.entity ?? '',
			conn: s?.trigger?.connection ?? '',
			state: s?.trigger?.state ?? '',
			// What the trigger waits for: a state, or a value above or below a threshold.
			cond:
				s?.trigger?.above !== undefined
					? 'above'
					: s?.trigger?.below !== undefined
						? 'below'
						: 'state',
			limit: String(s?.trigger?.above ?? s?.trigger?.below ?? ''),
			enabled: s ? String(s.enabled) : 'true',
			button: String(!!s?.button),
			wake: s?.wake ?? '',
			project: s?.project ?? '',
		};
		days = s?.weekdays?.length ? [...s.weekdays] : [1, 2, 3, 4, 5];
	}
	function body(): Schedule {
		const base = list.find((s) => s.id === editing);
		const trigger =
			draft.every === 'trigger'
				? {
						entity: draft.entity,
						state: draft.cond === 'state' ? draft.state : '',
						above: draft.cond === 'above' ? Number(draft.limit.replace(',', '.')) : undefined,
						below: draft.cond === 'below' ? Number(draft.limit.replace(',', '.')) : undefined,
						connection: draft.conn || undefined,
					}
				: null;
		return {
			...(base ?? ({} as Schedule)),
			id: editing === 'new' ? '' : (editing ?? ''),
			kind: draft.kind === 'reminder' ? 'reminder' : '',
			name: draft.name,
			task: draft.task,
			project: draft.project || undefined,
			every: draft.every === 'trigger' ? 'daily' : (draft.every as Schedule['every']),
			at: draft.at,
			date: draft.date,
			interval: Number(draft.interval),
			weekdays: days,
			weekday: Number(draft.weekday),
			day: Number(draft.day),
			cron: draft.cron,
			enabled: draft.enabled !== 'false',
			button: draft.button === 'true',
			wake: draft.kind === 'reminder' ? undefined : (draft.wake as Schedule['wake']) || undefined,
			trigger,
		};
	}
	const needsTime = $derived(
		['once', 'daily', 'days', 'weekdays', 'weekly', 'monthly'].includes(draft.every),
	);
	// A run before 2000 is the zero time older versions stored: never ran.
	const ran = (s: { lastRun?: string }) => !!s.lastRun && new Date(s.lastRun).getFullYear() >= 2000;
</script>

{#if error}<p class="notice-error" role="alert">{error}</p>{/if}

<!-- PLAN 90-4: groups of flat rows like a page of the control panel. -->
<h2 class="sgroup-title">{t('agent.plan.daily')}</h2>
<section class="card morning" aria-labelledby="morning-title">
	<header>
		<span class="ic" aria-hidden="true"><Icon name="bell" size={19} /></span>
		<div class="grow">
			<h3 id="morning-title">{t('agent.morning')}</h3>
			<p class="muted small">{t('agent.morningIntro')}</p>
		</div>
		{#if morning}
			<input
				type="checkbox"
				class="switch"
				role="switch"
				aria-label={t('agent.morning')}
				bind:checked={mOn}
				onchange={saveMorning}
			/>
		{/if}
	</header>
	{#if morning}
		<div class="grid">
			<span class="label">{t('agent.time')}</span>
			<input class="field small-in" type="time" bind:value={mAt} aria-label={t('agent.time')} />
			<span class="label">{t('agent.days')}</span>
			<div class="chips" role="group" aria-label={t('agent.days')}>
				{#each order as d (d)}
					<button
						type="button"
						class="chip-btn"
						aria-pressed={mDays.includes(d)}
						onclick={() => (mDays = dayToggle(mDays, d))}>{short(d)}</button
					>
				{/each}
			</div>
			<span class="label">{t('agent.morningWhat')}</span>
			<div class="checks">
				<label class="check"
					><input type="checkbox" bind:checked={report.nas} /><span>{t('agent.morningNAS')}</span
					></label
				>
				<label class="check"
					><input type="checkbox" bind:checked={report.backups} /><span
						>{t('agent.morningBackups')}</span
					></label
				>
				<label class="check"
					><input type="checkbox" bind:checked={report.today} /><span
						>{t('agent.morningToday')}</span
					></label
				>
				<label class="check"
					><input type="checkbox" bind:checked={report.home} /><span>{t('agent.morningHome')}</span
					></label
				>
			</div>
			<span class="label">{t('agent.morningExtra')}</span>
			<textarea
				class="field"
				rows="2"
				maxlength="1000"
				bind:value={report.extra}
				placeholder={t('agent.morningExtraHint')}></textarea>
		</div>
		<div class="row-actions">
			<button
				type="button"
				class="btn btn-primary"
				disabled={busy || mDays.length === 0}
				onclick={saveMorning}>{t('common.save')}</button
			>
			<button
				type="button"
				class="btn"
				disabled={busy}
				onclick={() => run(() => api.post(`/agent/schedules/${morning.id}/run`), false)}
				>{t('agent.morningNow')}</button
			>
			{#if morning.next && morning.enabled}<span class="muted small"
					>{t('agent.nextRun', { when: formatDateTime(new Date(morning.next)) })}</span
				>{/if}
		</div>
	{:else}
		<div class="row-actions">
			<button type="button" class="btn btn-primary" disabled={busy} onclick={saveMorning}
				>{t('agent.morningSetUp')}</button
			>
		</div>
	{/if}
</section>

<div class="group-head">
	<h2 class="sgroup-title">{t('agent.plan.tasks')}</h2>
	{#if !editing}<button type="button" class="btn" onclick={() => edit()}
			>{t('agent.scheduleAdd')}</button
		>{/if}
</div>
<ul class="rows">
	{#each others as s (s.id)}
		<li class:off={!s.enabled}>
			<span class="ic" aria-hidden="true"
				><Icon
					name={s.kind === 'reminder' ? 'bell' : s.trigger ? 'home' : 'play'}
					size={19}
				/></span
			>
			<span class="grow">
				<strong>{s.name}</strong>
				{#if !s.enabled}<span class="chip">{t('agent.off')}</span>{/if}
				{#if s.every === 'once' && ran(s)}<span class="chip">{t('agent.done')}</span>{/if}
				<span class="muted small"
					>{s.kind === 'reminder' ? t('agent.reminder') : t('agent.task')} · {s.when ??
						s.every}{s.next && s.enabled
						? ' · ' + t('agent.nextRun', { when: formatDateTime(new Date(s.next)) })
						: ''}{ran(s)
						? ' · ' + t('agent.lastRun', { when: formatDateTime(new Date(s.lastRun!)) })
						: ' · ' + t('agent.neverRun')}{ran(s) && s.kind !== 'reminder'
						? s.lastOk
							? ' ✓'
							: ' !'
						: ''}</span
				>
			</span>
			<span class="acts">
				<input
					type="checkbox"
					class="switch"
					role="switch"
					checked={s.enabled}
					disabled={busy}
					title={s.enabled ? t('agent.pause') : t('agent.resume')}
					aria-label={s.enabled ? t('agent.pause') : t('agent.resume')}
					onchange={() => toggle(s)}
				/>
				{#if s.chat}<button
						type="button"
						class="btn"
						onclick={() => onopenchat(s.chat!)}
						title={t('agent.lastResult')}
						aria-label={t('agent.lastResult')}><Icon name="chat" size={15} /></button
					>{/if}
				<button
					type="button"
					class="btn"
					disabled={busy}
					title={t('agent.runNow')}
					aria-label={t('agent.runNow')}
					onclick={() => run(() => api.post(`/agent/schedules/${s.id}/run`), false)}
					><Icon name="play" size={15} /></button
				>
				<button
					type="button"
					class="btn"
					title={t('agent.edit')}
					aria-label={t('agent.edit')}
					onclick={() => edit(s)}><Icon name="edit" size={15} /></button
				>
				<button
					type="button"
					class="btn btn-danger-outline"
					title={t('common.delete')}
					aria-label={t('common.delete')}
					onclick={() => (removing = s)}><Icon name="trash" size={15} /></button
				>
			</span>
		</li>
	{:else}
		<li class="muted">{t('agent.schedulesEmpty')}</li>
	{/each}
</ul>

{#if editing}
	<div class="card form">
		<div class="grid">
			<span class="label">{t('agent.planKind')}</span>
			<div class="seg" role="radiogroup" aria-label={t('agent.planKind')}>
				<button
					type="button"
					role="radio"
					aria-checked={draft.kind !== 'reminder'}
					onclick={() => (draft.kind = '')}>{t('agent.task')}</button
				>
				<button
					type="button"
					role="radio"
					aria-checked={draft.kind === 'reminder'}
					onclick={() => (draft.kind = 'reminder')}>{t('agent.reminder')}</button
				>
			</div>
			<label class="label" for="plan-name">{t('agent.scheduleName')}</label>
			<input id="plan-name" class="field" bind:value={draft.name} maxlength="80" />
			<label class="label" for="plan-task"
				>{draft.kind === 'reminder' ? t('agent.reminderText') : t('agent.scheduleTask')}</label
			>
			<textarea id="plan-task" class="field" rows="3" bind:value={draft.task}></textarea>
			<label class="label" for="plan-every">{t('agent.scheduleEvery')}</label>
			<div class="line">
				<select id="plan-every" class="field" bind:value={draft.every}>
					{#each everyKeys as e (e)}<option value={e}>{t(everyLabel[e])}</option>{/each}
				</select>
				{#if draft.every === 'minutes' || draft.every === 'hours' || draft.every === 'days'}
					<input
						class="field small-in"
						type="number"
						min={draft.every === 'minutes' ? 5 : 1}
						max={draft.every === 'minutes' ? 1440 : draft.every === 'hours' ? 168 : 365}
						bind:value={draft.interval}
						aria-label={t('agent.interval')}
					/>
					<span class="muted"
						>{t(
							draft.every === 'minutes'
								? 'agent.unitMinutes'
								: draft.every === 'hours'
									? 'agent.unitHours'
									: 'agent.unitDays',
						)}</span
					>
				{/if}
				{#if draft.every === 'once'}
					<input class="field" type="date" bind:value={draft.date} aria-label={t('agent.date')} />
				{/if}
				{#if draft.every === 'weekly'}
					<select class="field" bind:value={draft.weekday} aria-label={t('agent.weekday')}>
						{#each order as d (d)}<option value={String(d)}>{weekdays[d]}</option>{/each}
					</select>
				{/if}
				{#if draft.every === 'monthly'}
					<input
						class="field small-in"
						type="number"
						min="1"
						max="28"
						bind:value={draft.day}
						aria-label={t('agent.day')}
					/>
				{/if}
				{#if needsTime}
					<input
						class="field small-in"
						type="time"
						bind:value={draft.at}
						aria-label={t('agent.time')}
					/>
				{/if}
			</div>
			{#if draft.every === 'weekdays'}
				<span class="label">{t('agent.days')}</span>
				<div class="chips" role="group" aria-label={t('agent.days')}>
					{#each order as d (d)}
						<button
							type="button"
							class="chip-btn"
							aria-pressed={days.includes(d)}
							onclick={() => (days = dayToggle(days, d))}>{short(d)}</button
						>
					{/each}
				</div>
			{/if}
			{#if draft.every === 'cron'}
				<label class="label" for="plan-cron">{t('agent.cron')}</label>
				<div>
					<input
						id="plan-cron"
						class="field mono"
						bind:value={draft.cron}
						placeholder="0 7 * * 1-5"
					/>
					<p class="muted small">{t('agent.cronHint')}</p>
				</div>
			{/if}
			{#if draft.every === 'trigger'}
				<span class="label">{t('agent.everyTrigger')}</span>
				<div class="line">
					{#if haConns.length > 1}<select
							class="field"
							bind:value={draft.conn}
							aria-label={t('notify.haConnection')}
						>
							{#each haConns as c (c.id)}<option value={c.id === 'main' ? '' : c.id}
									>{c.name || t('ha.conn.first')}</option
								>{/each}
						</select>{/if}
					<input
						class="field"
						bind:value={draft.entity}
						placeholder="binary_sensor.haustuer"
						aria-label={t('agent.triggerEntity')}
					/>
					<select class="field" bind:value={draft.cond} aria-label={t('agent.triggerCond')}>
						<option value="state">{t('agent.triggerCondState')}</option>
						<option value="above">{t('agent.triggerCondAbove')}</option>
						<option value="below">{t('agent.triggerCondBelow')}</option>
					</select>
					{#if draft.cond === 'state'}
						<input
							class="field small-in"
							bind:value={draft.state}
							placeholder="on"
							aria-label={t('agent.triggerState')}
						/>
					{:else}
						<input
							class="field small-in"
							bind:value={draft.limit}
							inputmode="decimal"
							placeholder="28"
							aria-label={t('agent.triggerLimit')}
						/>
					{/if}
				</div>
			{/if}
			{#if draft.kind !== 'reminder'}
				<!-- PLAN 53/72: whether this task may wake a sleeping disk. -->
				<label class="label" for="plan-wake">{t('agent.scheduleWake')}</label>
				<div>
					<select id="plan-wake" class="field" bind:value={draft.wake}>
						<option value="">{t('agent.scheduleWakeAccount')}</option>
						<option value="wake">{t('agent.scheduleWakeYes')}</option>
						<option value="ask">{t('agent.scheduleWakeAsk')}</option>
						<option value="never">{t('agent.scheduleWakeNever')}</option>
					</select>
					<p class="muted small">{t('agent.scheduleWakeHint')}</p>
				</div>
			{/if}
			{#if projects.length && draft.kind !== 'reminder'}
				<label class="label" for="plan-project">{t('agent.project')}</label>
				<select id="plan-project" class="field" bind:value={draft.project}>
					<option value="">{t('agent.noProject')}</option>
					{#each projects as p (p.id)}<option value={p.id}>{p.icon ?? ''} {p.name}</option>{/each}
				</select>
			{/if}
		</div>
		<label class="check"
			><input
				type="checkbox"
				checked={draft.enabled !== 'false'}
				onchange={(e) => (draft.enabled = String(e.currentTarget.checked))}
			/><span>{t('agent.on')}</span></label
		>
		{#if draft.kind !== 'reminder'}
			<label class="check"
				><input
					type="checkbox"
					checked={draft.button === 'true'}
					onchange={(e) => (draft.button = String(e.currentTarget.checked))}
				/><span>{t('agent.scheduleButton')}</span></label
			>
		{/if}
		<div class="row-actions">
			<button
				type="button"
				class="btn btn-primary"
				disabled={busy || (draft.every === 'weekdays' && days.length === 0)}
				onclick={() => run(() => put(body()))}>{t('common.save')}</button
			>
			<button type="button" class="btn" onclick={() => (editing = null)}
				>{t('common.cancel')}</button
			>
		</div>
	</div>
{/if}

{#if removing}
	{@const s = removing}
	<Dialog title={t('agent.plan.delete')} onclose={() => (removing = null)}>
		<p>„{s.name}“</p>
		<div class="dialog-actions">
			<button type="button" class="btn" onclick={() => (removing = null)}
				>{t('common.cancel')}</button
			>
			<button
				type="button"
				class="btn btn-danger"
				onclick={() => {
					const id = s.id;
					removing = null;
					void run(() => api.delete(`/agent/schedules/${id}`), false);
				}}>{t('common.delete')}</button
			>
		</div>
	</Dialog>
{/if}

<style>
	.card {
		border: 1px solid var(--srow-line);
		border-radius: var(--srow-radius);
		padding: var(--space-3) var(--space-4);
		margin-bottom: var(--space-4);
		background: var(--srow-bg);
	}
	.sgroup-title {
		margin: 0 0 var(--space-2);
	}
	.group-head {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: var(--space-2);
		flex-wrap: wrap;
		margin-bottom: var(--space-2);
	}
	.group-head .sgroup-title {
		margin: 0;
	}
	.group-head .btn {
		gap: var(--space-1-5);
	}
	header {
		display: flex;
		align-items: center;
		gap: var(--space-3);
	}
	h3 {
		margin: 0;
		font-size: var(--text-md);
	}
	.ic {
		display: flex;
		color: var(--muted);
		flex: none;
	}
	.grow {
		flex: 1;
		min-width: 0;
		display: flex;
		flex-direction: column;
		gap: 2px;
	}
	.small {
		font-size: var(--text-sm);
		margin: 0;
	}
	.grid {
		display: grid;
		grid-template-columns: minmax(8rem, max-content) minmax(0, 1fr);
		align-items: center;
		gap: var(--space-2) var(--space-3);
		margin: var(--space-3) 0;
	}
	.label {
		color: var(--muted);
		font-size: var(--text-sm);
		text-align: right;
	}
	.chips,
	.line,
	.checks {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-1-5);
		align-items: center;
	}
	.checks {
		gap: var(--space-1) var(--space-4);
	}
	.chip-btn {
		min-width: 40px;
		min-height: 34px;
		padding: 0 var(--space-2-5);
		border-radius: 999px;
		border: 1px solid var(--border);
		background: none;
		color: var(--text);
		font: inherit;
		cursor: pointer;
	}
	.chip-btn[aria-pressed='true'] {
		background: var(--accent);
		border-color: var(--accent);
		color: var(--on-accent);
	}
	.seg {
		display: inline-flex;
		border: 1px solid var(--border);
		border-radius: 999px;
		padding: 2px;
		gap: 2px;
		justify-self: start;
	}
	.seg button {
		border: 0;
		background: none;
		color: var(--text);
		font: inherit;
		padding: var(--space-1-5) 14px;
		border-radius: 999px;
		cursor: pointer;
	}
	.seg button[aria-checked='true'] {
		background: var(--accent);
		color: var(--on-accent);
	}
	.small-in {
		width: 7.5rem;
	}
	.rows {
		list-style: none;
		margin: 0 0 var(--space-4);
		padding: 0;
		display: flex;
		flex-direction: column;
		gap: 3px;
	}
	.rows li {
		display: flex;
		align-items: center;
		gap: var(--space-3);
		min-height: 52px;
		padding: var(--space-2) var(--space-4);
		border: 1px solid var(--srow-line);
		border-radius: var(--srow-radius);
		background: var(--srow-bg);
	}
	.rows li.off .grow strong {
		color: var(--muted);
	}
	.acts {
		display: flex;
		align-items: center;
		gap: var(--space-1);
		flex: none;
	}
	.acts .btn {
		width: 34px;
		height: 34px;
		min-height: 34px;
		padding: 0;
		display: grid;
		place-items: center;
	}
	.acts .switch {
		margin-right: var(--space-1);
	}
	@media (max-width: 760px) {
		.grid {
			grid-template-columns: minmax(0, 1fr);
		}
		.label {
			text-align: left;
		}
		.rows li {
			flex-wrap: wrap;
		}
		.acts {
			width: 100%;
			justify-content: flex-end;
		}
		.acts .btn {
			width: 44px;
			height: 44px;
		}
	}
</style>
